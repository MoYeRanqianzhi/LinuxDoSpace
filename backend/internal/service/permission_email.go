package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
	"strings"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// reservedPublicEmailPrefixes contains the local-parts that are intentionally
// blocked from the public mailbox search flow because they are infrastructural,
// ambiguous, or already reserved by the platform itself.
var reservedPublicEmailPrefixes = map[string]struct{}{
	"abuse":         {},
	"admin":         {},
	"catch-all":     {},
	"hostmaster":    {},
	"mailer-daemon": {},
	"no-reply":      {},
	"noreply":       {},
	"postmaster":    {},
	"root":          {},
	"security":      {},
	"support":       {},
	"webmaster":     {},
}

// defaultEmailRouteSpec describes the mailbox that every authenticated user
// should implicitly own on the primary public email domain.
type defaultEmailRouteSpec struct {
	RootDomain string
	Prefix     string
	Address    string
}

// forwardingRuleSnapshot is the small Cloudflare-facing view used to reconcile
// the public mailbox UI with the actual Email Routing rule when the local
// database is missing or stale.
type forwardingRuleSnapshot struct {
	Found       bool
	TargetEmail string
	Enabled     bool
}

// CheckPublicEmailAvailability powers the public email search box without
// leaking mailbox targets or requiring authentication.
func (s *PermissionService) CheckPublicEmailAvailability(ctx context.Context, rootDomain string, prefix string) (EmailRouteAvailabilityResult, error) {
	managedDomain, err := s.resolveAvailableEmailRootDomain(ctx, rootDomain)
	if err != nil {
		return EmailRouteAvailabilityResult{}, err
	}

	normalizedPrefix, err := NormalizePrefix(prefix)
	if err != nil {
		return EmailRouteAvailabilityResult{}, ValidationError(err.Error())
	}

	result := EmailRouteAvailabilityResult{
		RootDomain:       managedDomain.RootDomain,
		Prefix:           strings.TrimSpace(prefix),
		NormalizedPrefix: normalizedPrefix,
		Address:          normalizedPrefix + "@" + managedDomain.RootDomain,
		Available:        true,
		Reasons:          make([]string, 0, 3),
	}

	if isSystemReservedEmailPrefix(normalizedPrefix) {
		result.Available = false
		result.Reasons = append(result.Reasons, "reserved_system_prefix")
	}

	reservedByKnownUser, err := s.isEmailPrefixReservedByKnownUser(ctx, normalizedPrefix)
	if err != nil {
		return EmailRouteAvailabilityResult{}, err
	}
	if reservedByKnownUser {
		result.Available = false
		result.Reasons = append(result.Reasons, "reserved_by_existing_user")
	}

	if _, err := s.db.GetEmailRouteByAddress(ctx, managedDomain.RootDomain, normalizedPrefix); err == nil {
		result.Available = false
		result.Reasons = append(result.Reasons, "existing_email_route")
	} else if !storage.IsNotFound(err) {
		return EmailRouteAvailabilityResult{}, InternalError("failed to check existing email route conflicts", err)
	}

	// When the local database is missing or stale, Cloudflare remains the source
	// of truth for whether the mailbox is already forwarding somewhere. This
	// second check prevents the public search UI from advertising an address that
	// still exists remotely after a partial failure.
	if result.Available {
		snapshot, snapshotErr := s.lookupCloudflareForwardingSnapshot(ctx, managedDomain.RootDomain, normalizedPrefix)
		if snapshotErr != nil {
			return EmailRouteAvailabilityResult{}, snapshotErr
		}
		if snapshot.Found {
			result.Available = false
			result.Reasons = append(result.Reasons, "existing_email_route")
		}
	}

	return result, nil
}

// UpsertMyDefaultEmailRoute creates, updates, or clears the forwarding target
// for the always-owned default mailbox <username>@linuxdo.space.
func (s *PermissionService) UpsertMyDefaultEmailRoute(ctx context.Context, user model.User, request UpsertMyDefaultEmailRouteRequest) (UserEmailRouteView, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	spec, err := s.resolveDefaultEmailRouteSpec(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	targetEmail, err := normalizeTargetEmail(request.TargetEmail, true)
	if err != nil {
		return UserEmailRouteView{}, err
	}
	if targetEmail != "" {
		target, targetErr := s.requireVerifiedOwnedEmailTarget(ctx, user, targetEmail)
		if targetErr != nil {
			return UserEmailRouteView{}, targetErr
		}
		targetEmail = target.Email
	}

	beforeState, existingRoute, err := s.resolveDefaultEmailRouteBeforeState(ctx, user, spec)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	if targetEmail == "" {
		if beforeState.Exists {
			if err := routing.SyncForwardingState(ctx, beforeState, newDeletedEmailRouteSyncState(spec.RootDomain, spec.Prefix), func() error {
				if existingRoute == nil {
					return nil
				}
				if deleteErr := s.db.DeleteEmailRoute(ctx, existingRoute.ID); deleteErr != nil {
					return InternalError("failed to clear default email route", deleteErr)
				}
				return nil
			}); err != nil {
				return UserEmailRouteView{}, err
			}

			if existingRoute != nil {
				metadata, _ := json.Marshal(map[string]any{
					"email_route_id": existingRoute.ID,
					"address":        spec.Address,
				})
				logAuditWriteFailure("email_route.default.clear", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
					ActorUserID:  &user.ID,
					Action:       "email_route.default.clear",
					ResourceType: "email_route",
					ResourceID:   strconv.FormatInt(existingRoute.ID, 10),
					MetadataJSON: string(metadata),
				}))
			}
		}

		return s.buildDefaultEmailRouteView(ctx, user)
	}

	desiredState := newForwardingEmailRouteSyncState(spec.RootDomain, spec.Prefix, targetEmail, request.Enabled)
	var item model.EmailRoute
	if err := routing.SyncForwardingState(ctx, beforeState, desiredState, func() error {
		var persistErr error
		item, persistErr = s.db.UpsertEmailRouteByAddress(ctx, storage.UpsertEmailRouteByAddressInput{
			OwnerUserID: user.ID,
			RootDomain:  spec.RootDomain,
			Prefix:      spec.Prefix,
			TargetEmail: targetEmail,
			Enabled:     request.Enabled,
		})
		if persistErr != nil {
			return InternalError("failed to save default email route", persistErr)
		}
		return nil
	}); err != nil {
		return UserEmailRouteView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": item.ID,
		"address":        item.Prefix + "@" + item.RootDomain,
	})
	logAuditWriteFailure("email_route.default.upsert", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_route.default.upsert",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return userEmailRouteFromModel(
		item,
		UserEmailRouteKindDefault,
		defaultEmailRouteDisplayName,
		defaultEmailRouteDescription,
		true,
		false,
		"",
	), nil
}

// resolveDefaultEmailRouteBeforeState loads the exact Cloudflare-facing state
// that should be considered the "before" snapshot for one user's implicit
// default mailbox. The database remains authoritative for ownership checks, but
// Cloudflare becomes the fallback truth when the local row is missing after a
// partial failure.
func (s *PermissionService) resolveDefaultEmailRouteBeforeState(ctx context.Context, user model.User, spec defaultEmailRouteSpec) (emailRouteSyncState, *model.EmailRoute, error) {
	beforeState := newDeletedEmailRouteSyncState(spec.RootDomain, spec.Prefix)

	existingRoute, err := s.db.GetEmailRouteByAddress(ctx, spec.RootDomain, spec.Prefix)
	switch {
	case err == nil:
		if existingRoute.OwnerUserID != user.ID {
			return emailRouteSyncState{}, nil, UnavailableError("default mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", existingRoute.ID, existingRoute.OwnerUserID))
		}
		beforeState = newForwardingEmailRouteSyncState(existingRoute.RootDomain, existingRoute.Prefix, existingRoute.TargetEmail, existingRoute.Enabled)
		return beforeState, &existingRoute, nil
	case storage.IsNotFound(err):
		snapshot, snapshotErr := s.lookupCloudflareForwardingSnapshot(ctx, spec.RootDomain, spec.Prefix)
		if snapshotErr != nil {
			return emailRouteSyncState{}, nil, snapshotErr
		}
		if snapshot.Found {
			beforeState = newForwardingEmailRouteSyncState(spec.RootDomain, spec.Prefix, snapshot.TargetEmail, snapshot.Enabled)
		}
		return beforeState, nil, nil
	default:
		return emailRouteSyncState{}, nil, InternalError("failed to load default email route", err)
	}
}

// resolveAvailableEmailRootDomain validates one public email root domain. When
// the caller leaves it empty, the service falls back to the configured default
// root or the first enabled managed domain.
func (s *PermissionService) resolveAvailableEmailRootDomain(ctx context.Context, rootDomain string) (model.ManagedDomain, error) {
	trimmedRootDomain := strings.ToLower(strings.TrimSpace(rootDomain))
	if trimmedRootDomain != "" {
		managedDomain, err := s.db.GetManagedDomainByRoot(ctx, trimmedRootDomain)
		if err != nil {
			if storage.IsNotFound(err) {
				return model.ManagedDomain{}, NotFoundError("managed domain not found")
			}
			return model.ManagedDomain{}, InternalError("failed to load managed domain", err)
		}
		if !managedDomain.Enabled {
			return model.ManagedDomain{}, NotFoundError("managed domain not found")
		}
		return managedDomain, nil
	}

	configuredRootDomain := strings.ToLower(strings.TrimSpace(s.cfg.Cloudflare.DefaultRootDomain))
	if configuredRootDomain != "" {
		managedDomain, err := s.db.GetManagedDomainByRoot(ctx, configuredRootDomain)
		if err == nil && managedDomain.Enabled {
			return managedDomain, nil
		}
		if err != nil && !storage.IsNotFound(err) {
			return model.ManagedDomain{}, InternalError("failed to load default managed domain", err)
		}
	}

	managedDomains, err := s.db.ListManagedDomains(ctx, false)
	if err != nil {
		return model.ManagedDomain{}, InternalError("failed to list managed domains", err)
	}
	if len(managedDomains) == 0 {
		return model.ManagedDomain{}, UnavailableError("no managed email domains are available", fmt.Errorf("no enabled managed domains"))
	}

	selected := managedDomains[0]
	for _, item := range managedDomains {
		if item.IsDefault {
			selected = item
			break
		}
	}
	return selected, nil
}

// resolveDefaultEmailRouteSpec converts the current username into the default
// mailbox address that should be reserved for the authenticated user.
func (s *PermissionService) resolveDefaultEmailRouteSpec(ctx context.Context, user model.User) (defaultEmailRouteSpec, error) {
	normalizedPrefix, err := normalizedUserPrefix(user.Username)
	if err != nil {
		return defaultEmailRouteSpec{}, ValidationError("current username cannot be used as a mailbox prefix")
	}

	managedDomain, err := s.resolveAvailableEmailRootDomain(ctx, s.cfg.Cloudflare.DefaultRootDomain)
	if err != nil {
		return defaultEmailRouteSpec{}, err
	}

	return defaultEmailRouteSpec{
		RootDomain: managedDomain.RootDomain,
		Prefix:     normalizedPrefix,
		Address:    normalizedPrefix + "@" + managedDomain.RootDomain,
	}, nil
}

// buildDefaultEmailRouteView returns either the persisted default forwarding
// rule or the placeholder row shown before the user saves a target inbox.
func (s *PermissionService) buildDefaultEmailRouteView(ctx context.Context, user model.User) (UserEmailRouteView, error) {
	spec, err := s.resolveDefaultEmailRouteSpec(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	snapshot, snapshotErr := s.lookupCloudflareForwardingSnapshot(ctx, spec.RootDomain, spec.Prefix)
	placeholder := UserEmailRouteView{
		Kind:        UserEmailRouteKindDefault,
		DisplayName: defaultEmailRouteDisplayName,
		Description: defaultEmailRouteDescription,
		Address:     spec.Address,
		Prefix:      spec.Prefix,
		RootDomain:  spec.RootDomain,
		TargetEmail: "",
		Enabled:     false,
		Configured:  false,
		CanManage:   true,
		CanDelete:   false,
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, spec.RootDomain, spec.Prefix)
	if err != nil {
		if storage.IsNotFound(err) {
			if snapshotErr == nil && snapshot.Found {
				placeholder.TargetEmail = snapshot.TargetEmail
				placeholder.Enabled = snapshot.Enabled
				placeholder.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
			}
			return normalizeUserEmailRouteCopy(placeholder), nil
		}
		return UserEmailRouteView{}, InternalError("failed to load default email route", err)
	}
	if route.OwnerUserID != user.ID {
		return UserEmailRouteView{}, UnavailableError("default mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", route.ID, route.OwnerUserID))
	}

	view := userEmailRouteFromModel(
		route,
		UserEmailRouteKindDefault,
		defaultEmailRouteDisplayName,
		defaultEmailRouteDescription,
		true,
		false,
		"",
	)
	if snapshotErr == nil && snapshot.Found {
		view.TargetEmail = snapshot.TargetEmail
		view.Enabled = snapshot.Enabled
		view.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
	}
	return normalizeUserEmailRouteCopy(view), nil
}

// buildCustomEmailRouteView converts one persisted extra mailbox alias into the
// user-facing view model. These rows stay read-only for now because the current
// public recovery work focuses on the default mailbox and catch-all flows.
func buildCustomEmailRouteView(route model.EmailRoute) UserEmailRouteView {
	return userEmailRouteFromModel(
		route,
		UserEmailRouteKindCustom,
		customEmailRouteDisplayName,
		customEmailRouteDescription,
		false,
		false,
		"",
	)
}

// lookupCloudflareForwardingSnapshot loads the exact Cloudflare Email Routing
// rule for one visible mailbox address when Email Routing integration is
// configured. The public page uses this to avoid showing stale local state.
func (s *PermissionService) lookupCloudflareForwardingSnapshot(ctx context.Context, rootDomain string, prefix string) (forwardingRuleSnapshot, error) {
	if usesDatabaseRelayNamespaceRoot(s.cfg, rootDomain) {
		return forwardingRuleSnapshot{}, nil
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return forwardingRuleSnapshot{}, nil
	}

	routing := newEmailRoutingProvisioner(s.cfg, s.cf)
	zoneID, err := routing.resolveZoneID(ctx, rootDomain)
	if err != nil {
		return forwardingRuleSnapshot{}, err
	}

	rules, err := s.cf.ListEmailRoutingRules(ctx, zoneID)
	if err != nil {
		return forwardingRuleSnapshot{}, wrapEmailRoutingUnavailable("failed to list cloudflare email routing rules", err)
	}

	rule, found := findEmailRoutingRuleByAddress(rules, buildEmailRouteAddress(prefix, rootDomain))
	if !found {
		return forwardingRuleSnapshot{}, nil
	}

	return forwardingRuleSnapshot{
		Found:       true,
		TargetEmail: extractForwardTargetEmail(rule),
		Enabled:     rule.Enabled,
	}, nil
}

// lookupCloudflareCatchAllSnapshot loads the Cloudflare catch-all rule for one
// routed namespace so the public page can stay aligned with the provider even
// when SQLite is stale or missing.
func (s *PermissionService) lookupCloudflareCatchAllSnapshot(ctx context.Context, rootDomain string) (forwardingRuleSnapshot, error) {
	if s.cfg.UsesDatabaseMailRelay() {
		return forwardingRuleSnapshot{}, nil
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return forwardingRuleSnapshot{}, nil
	}

	routing := newEmailRoutingProvisioner(s.cfg, s.cf)
	zoneID, err := routing.resolveZoneID(ctx, rootDomain)
	if err != nil {
		return forwardingRuleSnapshot{}, err
	}
	zoneRoot, err := routing.resolveZoneRootDomain(ctx, zoneID, rootDomain)
	if err != nil {
		return forwardingRuleSnapshot{}, err
	}
	subdomain, err := cloudflareEmailRoutingScopedDomain(rootDomain, zoneRoot)
	if err != nil {
		return forwardingRuleSnapshot{}, err
	}

	rule, err := s.cf.GetEmailRoutingCatchAllRule(ctx, zoneID, subdomain)
	if err != nil {
		return forwardingRuleSnapshot{}, wrapEmailRoutingUnavailable("failed to load cloudflare catch-all email routing rule", err)
	}
	if !isCatchAllForwardingRule(rule) {
		return forwardingRuleSnapshot{}, nil
	}

	return forwardingRuleSnapshot{
		Found:       true,
		TargetEmail: extractForwardTargetEmail(rule),
		Enabled:     rule.Enabled,
	}, nil
}

// extractForwardTargetEmail returns the first forwarded destination email from
// one Cloudflare Email Routing rule. LinuxDoSpace only writes one destination
// target today, so the first entry is the effective forwarding inbox.
func extractForwardTargetEmail(rule cloudflare.EmailRoutingRule) string {
	for _, action := range rule.Actions {
		if !strings.EqualFold(strings.TrimSpace(action.Type), "forward") {
			continue
		}
		if len(action.Value) == 0 {
			return ""
		}
		return strings.ToLower(strings.TrimSpace(action.Value[0]))
	}
	return ""
}

// userEmailRouteFromModel centralizes the translation from one stored email
// route row into the public user-facing API model.
func userEmailRouteFromModel(route model.EmailRoute, kind string, displayName string, description string, canManage bool, canDelete bool, permissionStatus string) UserEmailRouteView {
	updatedAt := route.UpdatedAt
	return normalizeUserEmailRouteCopy(UserEmailRouteView{
		ID:               route.ID,
		Kind:             kind,
		DisplayName:      displayName,
		Description:      description,
		Address:          route.Prefix + "@" + route.RootDomain,
		Prefix:           route.Prefix,
		RootDomain:       route.RootDomain,
		TargetEmail:      route.TargetEmail,
		Enabled:          route.Enabled,
		Configured:       strings.TrimSpace(route.TargetEmail) != "",
		PermissionStatus: permissionStatus,
		CanManage:        canManage,
		CanDelete:        canDelete,
		UpdatedAt:        &updatedAt,
	})
}

// normalizeTargetEmail validates one forwarding target. When allowEmpty is
// true, the caller may deliberately clear the saved target to restore the
// placeholder state without writing a broken route.
func normalizeTargetEmail(raw string, allowEmpty bool) (string, error) {
	targetEmail := strings.ToLower(strings.TrimSpace(raw))
	if targetEmail == "" {
		if allowEmpty {
			return "", nil
		}
		return "", ValidationError("target_email must not be empty")
	}
	if _, err := mail.ParseAddress(targetEmail); err != nil {
		return "", ValidationError("target_email must be a valid email address")
	}
	return targetEmail, nil
}

// isSystemReservedEmailPrefix checks whether one local-part belongs to the
// platform-maintained reserved set.
func isSystemReservedEmailPrefix(normalizedPrefix string) bool {
	_, reserved := reservedPublicEmailPrefixes[strings.ToLower(strings.TrimSpace(normalizedPrefix))]
	return reserved
}

// isEmailPrefixReservedByKnownUser prevents a visitor from claiming another
// member's implicit default mailbox when that member already exists locally.
func (s *PermissionService) isEmailPrefixReservedByKnownUser(ctx context.Context, normalizedPrefix string) (bool, error) {
	_, err := s.db.GetUserByUsername(ctx, normalizedPrefix)
	if err == nil {
		return true, nil
	}
	if storage.IsNotFound(err) {
		return false, nil
	}
	return false, InternalError("failed to load known users for email availability", err)
}
