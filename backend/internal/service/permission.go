package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

const (
	// PermissionKeyEmailCatchAll is the stable identifier used across the
	// database, HTTP API, and frontends for the catch-all mailbox permission.
	PermissionKeyEmailCatchAll = "email_catch_all"

	// UserEmailRouteKindDefault marks the per-user default mailbox that always
	// maps to <username>@linuxdo.space (or the configured default email root).
	UserEmailRouteKindDefault = "default"

	// UserEmailRouteKindCustom marks one extra mailbox alias already assigned to
	// the current user and loaded from the database.
	UserEmailRouteKindCustom = "custom"

	// UserEmailRouteKindCatchAll marks the permission-gated catch-all mailbox
	// that routes catch-all@<username>.<root> to one target inbox.
	UserEmailRouteKindCatchAll = "catch_all"

	// emailCatchAllPrefix is the fixed local-part prefix granted by this
	// permission. The address always resolves to catch-all@<username>.<root>.
	emailCatchAllPrefix = "catch-all"
)

// EmailCatchAllPledgeText is the canonical server-side pledge text recorded on
// every application. The backend stores this value directly so the audit trail
// does not depend on client-side wording.
const EmailCatchAllPledgeText = "I promise to use this email catch-all permission only for lawful and reasonable purposes, not to abuse platform resources, and to accept full responsibility for any consequences caused by my own actions. If I benefit from it, I am willing to give back to the Linux Do community."

// PermissionService owns user-facing permission application flows together with
// the administrator-configurable policy rules that govern those flows.
type PermissionService struct {
	cfg config.Config
	db  Store
	cf  CloudflareClient
}

const (
	emailCatchAllPermissionDisplayName = "Catch-all forwarding"
	emailCatchAllPermissionDescription = "Enable one catch-all forwarding entry for the default subdomain that matches your username."
	emailCatchAllPledgeTextClean       = "I promise to use this email catch-all permission only for lawful and reasonable purposes, not to abuse platform resources, and to accept full responsibility for any consequences caused by my own actions. If I benefit from it, I am willing to give back to the Linux Do community."
	defaultEmailRouteDisplayName       = "Default mailbox"
	defaultEmailRouteDescription       = "Each user automatically owns one mailbox that matches the Linux Do username."
	customEmailRouteDisplayName        = "Additional mailbox"
	customEmailRouteDescription        = "This mailbox was already assigned to your account and is currently shown as read-only on the public page."
	catchAllEmailRouteDisplayName      = "Catch-all forwarding"
	catchAllEmailRouteDescription      = "Receives mail sent to catch-all@<username>.linuxdo.space."
)

// PermissionApplicationSummary is the normalized subset of one application row
// exposed to the user-facing frontend.
type PermissionApplicationSummary struct {
	ID         int64      `json:"id"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason"`
	ReviewNote string     `json:"review_note"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// UserPermissionView describes the current visible state of one user-facing
// permission, including the active policy gate and the latest application state.
type UserPermissionView struct {
	Key                string                        `json:"key"`
	DisplayName        string                        `json:"display_name"`
	Description        string                        `json:"description"`
	Target             string                        `json:"target"`
	PledgeText         string                        `json:"pledge_text"`
	PolicyEnabled      bool                          `json:"policy_enabled"`
	AutoApprove        bool                          `json:"auto_approve"`
	MinTrustLevel      int                           `json:"min_trust_level"`
	Eligible           bool                          `json:"eligible"`
	EligibilityReasons []string                      `json:"eligibility_reasons"`
	Status             string                        `json:"status"`
	CanApply           bool                          `json:"can_apply"`
	CanManageRoute     bool                          `json:"can_manage_route"`
	Application        *PermissionApplicationSummary `json:"application,omitempty"`
}

// UserEmailRouteView describes one user-visible email forwarding row shown on
// the public email page.
type UserEmailRouteView struct {
	ID               int64      `json:"id,omitempty"`
	Kind             string     `json:"kind"`
	PermissionKey    string     `json:"permission_key,omitempty"`
	DisplayName      string     `json:"display_name"`
	Description      string     `json:"description"`
	Address          string     `json:"address"`
	Prefix           string     `json:"prefix"`
	RootDomain       string     `json:"root_domain"`
	TargetEmail      string     `json:"target_email"`
	Enabled          bool       `json:"enabled"`
	Configured       bool       `json:"configured"`
	PermissionStatus string     `json:"permission_status,omitempty"`
	CanManage        bool       `json:"can_manage"`
	CanDelete        bool       `json:"can_delete"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

// EmailRouteAvailabilityResult mirrors the public email-prefix search result
// rendered by the frontend search box.
type EmailRouteAvailabilityResult struct {
	RootDomain       string   `json:"root_domain"`
	Prefix           string   `json:"prefix"`
	NormalizedPrefix string   `json:"normalized_prefix"`
	Address          string   `json:"address"`
	Available        bool     `json:"available"`
	Reasons          []string `json:"reasons"`
}

// SubmitPermissionApplicationRequest describes the single user-side mutation
// currently supported by the permissions page.
type SubmitPermissionApplicationRequest struct {
	Key string `json:"key"`
}

// UpsertMyDefaultEmailRouteRequest describes the forwarding target saved for
// the always-owned default mailbox <username>@linuxdo.space.
type UpsertMyDefaultEmailRouteRequest struct {
	TargetEmail string `json:"target_email"`
	Enabled     bool   `json:"enabled"`
}

// UpsertMyCatchAllEmailRouteRequest describes the forwarding target configured
// by the user after the catch-all mailbox permission has been approved.
type UpsertMyCatchAllEmailRouteRequest struct {
	TargetEmail string `json:"target_email"`
	Enabled     bool   `json:"enabled"`
}

// UpdatePermissionPolicyRequest describes the administrator-editable subset of
// one permission policy row. Pointer fields let PATCH keep existing values.
type UpdatePermissionPolicyRequest struct {
	Enabled       *bool `json:"enabled,omitempty"`
	AutoApprove   *bool `json:"auto_approve,omitempty"`
	MinTrustLevel *int  `json:"min_trust_level,omitempty"`
}

// AdminSetUserPermissionRequest describes one administrator-authored permission
// decision written directly against a target user.
type AdminSetUserPermissionRequest struct {
	Status     string `json:"status"`
	ReviewNote string `json:"review_note"`
	Reason     string `json:"reason"`
}

// catchAllNamespace describes the routed namespace derived from the current
// user account and the configured default root domain.
type catchAllNamespace struct {
	RootDomain         string
	Address            string
	HasOwnedAllocation bool
}

// NewPermissionService constructs the service instance responsible for the new
// user-side permission and email-routing flows.
func NewPermissionService(cfg config.Config, db Store, cf CloudflareClient) *PermissionService {
	return &PermissionService{cfg: cfg, db: db, cf: cf}
}

// ListMyPermissions returns the single currently supported permission card for
// the authenticated user.
func (s *PermissionService) ListMyPermissions(ctx context.Context, user model.User) ([]UserPermissionView, error) {
	item, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return nil, err
	}
	return []UserPermissionView{item}, nil
}

// SubmitPermissionApplication stores or refreshes the user's latest permission
// application and automatically approves it when the current policy allows that.
func (s *PermissionService) SubmitPermissionApplication(ctx context.Context, user model.User, request SubmitPermissionApplicationRequest) (UserPermissionView, error) {
	if strings.TrimSpace(request.Key) != PermissionKeyEmailCatchAll {
		return UserPermissionView{}, ValidationError("unsupported permission key")
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}
	if !permission.Eligible {
		return UserPermissionView{}, ForbiddenError(firstNonEmpty(permission.EligibilityReasons...))
	}

	switch permission.Status {
	case "approved":
		return UserPermissionView{}, ConflictError("the catch-all email permission has already been granted")
	case "pending":
		return UserPermissionView{}, ConflictError("the catch-all email permission application is already pending review")
	}

	status := "pending"
	if permission.AutoApprove {
		status = "approved"
	}

	item, err := s.db.UpsertAdminApplication(ctx, sqlite.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          permission.Target,
		Reason:          emailCatchAllPledgeTextClean,
		Status:          status,
	})
	if err != nil {
		return UserPermissionView{}, InternalError("failed to submit permission application", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"application_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"target":         item.Target,
		"status":         item.Status,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "permission.application.submit",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserPermissionView{}, InternalError("failed to write permission application audit log", err)
	}

	return s.loadEmailCatchAllPermission(ctx, user)
}

// ListMyEmailRoutes returns the full set of user-visible mailbox rows rendered
// by the public email page: the default mailbox, any extra stored aliases, and
// the permission-gated catch-all mailbox.
func (s *PermissionService) ListMyEmailRoutes(ctx context.Context, user model.User) ([]UserEmailRouteView, error) {
	defaultRoute, err := s.buildDefaultEmailRouteView(ctx, user)
	if err != nil {
		return nil, err
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return nil, err
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return nil, err
	}

	catchAllRoute, err := s.buildCatchAllEmailRouteView(ctx, user, permission, namespace)
	if err != nil {
		return nil, err
	}

	persistedRoutes, err := s.db.ListEmailRoutesByOwner(ctx, user.ID)
	if err != nil {
		return nil, InternalError("failed to load user email routes", err)
	}

	items := make([]UserEmailRouteView, 0, len(persistedRoutes)+2)
	items = append(items, defaultRoute)
	for _, route := range persistedRoutes {
		if route.Prefix == defaultRoute.Prefix && strings.EqualFold(route.RootDomain, defaultRoute.RootDomain) {
			continue
		}
		if route.Prefix == emailCatchAllPrefix && strings.EqualFold(route.RootDomain, namespace.RootDomain) {
			continue
		}
		items = append(items, buildCustomEmailRouteView(route))
	}
	items = append(items, catchAllRoute)
	return items, nil
}

// UpsertMyCatchAllEmailRoute creates or updates the user's forwarding target
// after the catch-all permission has been approved.
func (s *PermissionService) UpsertMyCatchAllEmailRoute(ctx context.Context, user model.User, request UpsertMyCatchAllEmailRouteRequest) (UserEmailRouteView, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}
	if permission.Status != "approved" {
		return UserEmailRouteView{}, ForbiddenError("the catch-all email permission has not been approved")
	}

	targetEmail, err := normalizeTargetEmail(request.TargetEmail, false)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	beforeState := newDeletedEmailRouteSyncState(namespace.RootDomain, emailCatchAllPrefix)
	existingRoute, routeErr := s.db.GetEmailRouteByAddress(ctx, namespace.RootDomain, emailCatchAllPrefix)
	switch {
	case routeErr == nil:
		if existingRoute.OwnerUserID != user.ID {
			return UserEmailRouteView{}, UnavailableError("catch-all mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", existingRoute.ID, existingRoute.OwnerUserID))
		}
		beforeState = newForwardingEmailRouteSyncState(existingRoute.RootDomain, existingRoute.Prefix, existingRoute.TargetEmail, existingRoute.Enabled)
	case sqlite.IsNotFound(routeErr):
		// No existing catch-all route is stored yet.
	default:
		return UserEmailRouteView{}, InternalError("failed to load catch-all email route before update", routeErr)
	}

	desiredState := newForwardingEmailRouteSyncState(namespace.RootDomain, emailCatchAllPrefix, targetEmail, request.Enabled)
	var item model.EmailRoute
	if err := routing.SyncForwardingState(ctx, beforeState, desiredState, func() error {
		var persistErr error
		item, persistErr = s.db.UpsertEmailRouteByAddress(ctx, sqlite.UpsertEmailRouteByAddressInput{
			OwnerUserID: user.ID,
			RootDomain:  namespace.RootDomain,
			Prefix:      emailCatchAllPrefix,
			TargetEmail: targetEmail,
			Enabled:     request.Enabled,
		})
		if persistErr != nil {
			return InternalError("failed to save catch-all email route", persistErr)
		}
		return nil
	}); err != nil {
		return UserEmailRouteView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"address":        item.Prefix + "@" + item.RootDomain,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_route.catch_all.upsert",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserEmailRouteView{}, InternalError("failed to write catch-all email route audit log", err)
	}

	updatedAt := item.UpdatedAt
	return normalizeUserEmailRouteCopy(UserEmailRouteView{
		ID:               item.ID,
		Kind:             UserEmailRouteKindCatchAll,
		PermissionKey:    PermissionKeyEmailCatchAll,
		DisplayName:      "邮箱泛解析",
		Description:      "用于接收 catch-all@<username>.linuxdo.space 的泛解析邮件转发。",
		Address:          emailCatchAllPrefix + "@" + namespace.RootDomain,
		Prefix:           emailCatchAllPrefix,
		RootDomain:       namespace.RootDomain,
		TargetEmail:      item.TargetEmail,
		Enabled:          item.Enabled,
		Configured:       strings.TrimSpace(item.TargetEmail) != "",
		PermissionStatus: permission.Status,
		CanManage:        true,
		CanDelete:        false,
		UpdatedAt:        &updatedAt,
	}), nil
}

// ListPermissionPolicies returns the administrator-visible configuration rows
// that decide who can request each supported permission.
func (s *PermissionService) ListPermissionPolicies(ctx context.Context) ([]model.PermissionPolicy, error) {
	items, err := s.db.ListPermissionPolicies(ctx)
	if err != nil {
		return nil, InternalError("failed to list permission policies", err)
	}
	return items, nil
}

// UpdatePermissionPolicy updates the mutable gates that control one permission.
func (s *PermissionService) UpdatePermissionPolicy(ctx context.Context, actor model.User, key string, request UpdatePermissionPolicyRequest) (model.PermissionPolicy, error) {
	item, err := s.db.GetPermissionPolicy(ctx, strings.TrimSpace(key))
	if err != nil {
		if sqlite.IsNotFound(err) {
			return model.PermissionPolicy{}, NotFoundError("permission policy not found")
		}
		return model.PermissionPolicy{}, InternalError("failed to load permission policy", err)
	}

	if request.Enabled != nil {
		item.Enabled = *request.Enabled
	}
	if request.AutoApprove != nil {
		item.AutoApprove = *request.AutoApprove
	}
	if request.MinTrustLevel != nil {
		if *request.MinTrustLevel < 0 || *request.MinTrustLevel > 4 {
			return model.PermissionPolicy{}, ValidationError("min_trust_level must be between 0 and 4")
		}
		item.MinTrustLevel = *request.MinTrustLevel
	}

	updated, err := s.db.UpsertPermissionPolicy(ctx, sqlite.UpsertPermissionPolicyInput{
		Key:           item.Key,
		DisplayName:   item.DisplayName,
		Description:   item.Description,
		Enabled:       item.Enabled,
		AutoApprove:   item.AutoApprove,
		MinTrustLevel: item.MinTrustLevel,
	})
	if err != nil {
		return model.PermissionPolicy{}, InternalError("failed to update permission policy", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"policy_key":      updated.Key,
		"enabled":         updated.Enabled,
		"auto_approve":    updated.AutoApprove,
		"min_trust_level": updated.MinTrustLevel,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.permission_policy.update",
		ResourceType: "permission_policy",
		ResourceID:   updated.Key,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.PermissionPolicy{}, InternalError("failed to write permission policy audit log", err)
	}
	return updated, nil
}

// ListPermissionsForUser returns the current permission card set for one target
// user so administrators can inspect and control it from the user editor.
func (s *PermissionService) ListPermissionsForUser(ctx context.Context, userID int64) ([]UserPermissionView, error) {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return nil, NotFoundError("target user not found")
		}
		return nil, InternalError("failed to load target user", err)
	}
	return s.ListMyPermissions(ctx, user)
}

// SetPermissionForUser lets an administrator directly create or override one
// permission state for a target user, even without a prior user-side request.
func (s *PermissionService) SetPermissionForUser(ctx context.Context, actor model.User, userID int64, permissionKey string, request AdminSetUserPermissionRequest) (UserPermissionView, error) {
	if strings.TrimSpace(permissionKey) != PermissionKeyEmailCatchAll {
		return UserPermissionView{}, ValidationError("unsupported permission key")
	}

	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return UserPermissionView{}, NotFoundError("target user not found")
		}
		return UserPermissionView{}, InternalError("failed to load target user", err)
	}

	status := normalizeAdminApplicationStatus(request.Status)
	if status == "" {
		return UserPermissionView{}, ValidationError("status must be pending, approved, or rejected")
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		if permission.Application != nil && strings.TrimSpace(permission.Application.Reason) != "" {
			reason = permission.Application.Reason
		} else {
			reason = "Administrator manually set this permission state."
		}
	}

	now := time.Now().UTC()
	item, err := s.db.UpsertAdminApplication(ctx, sqlite.UpsertAdminApplicationInput{
		ApplicantUserID:  user.ID,
		Type:             PermissionKeyEmailCatchAll,
		Target:           permission.Target,
		Reason:           reason,
		Status:           status,
		ReviewNote:       strings.TrimSpace(request.ReviewNote),
		ReviewedByUserID: &actor.ID,
		ReviewedAt:       &now,
	})
	if err != nil {
		return UserPermissionView{}, InternalError("failed to set target user permission", err)
	}

	if err := s.disableCatchAllEmailRouteForApplication(ctx, actor, item); err != nil {
		return UserPermissionView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"application_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"target_user_id": user.ID,
		"status":         item.Status,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.permission.user_set",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserPermissionView{}, InternalError("failed to write admin permission audit log", err)
	}

	return s.loadEmailCatchAllPermission(ctx, user)
}

// loadEmailCatchAllPermission resolves the current policy, eligibility, and
// application state into the single card rendered by the public frontend.
func (s *PermissionService) loadEmailCatchAllPermission(ctx context.Context, user model.User) (UserPermissionView, error) {
	policy, err := s.db.GetPermissionPolicy(ctx, PermissionKeyEmailCatchAll)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return UserPermissionView{}, UnavailableError("catch-all email permission policy is not configured", fmt.Errorf("missing policy %s", PermissionKeyEmailCatchAll))
		}
		return UserPermissionView{}, InternalError("failed to load catch-all email permission policy", err)
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}

	applications, err := s.db.ListAdminApplicationsByApplicant(ctx, user.ID)
	if err != nil {
		return UserPermissionView{}, InternalError("failed to load permission applications", err)
	}

	application := findPermissionApplication(applications, PermissionKeyEmailCatchAll, namespace.Address)
	reasons := buildCatchAllEligibilityReasons(user, policy, namespace)
	status := "not_requested"
	var summary *PermissionApplicationSummary
	if application != nil {
		status = application.Status
		summary = permissionApplicationSummaryFromModel(*application)
	}

	canApply := len(reasons) == 0 && status != "pending" && status != "approved"
	canManageRoute := status == "approved"

	return normalizeCatchAllPermissionCopy(UserPermissionView{
		Key:                PermissionKeyEmailCatchAll,
		DisplayName:        itemDisplayName(policy.DisplayName, emailCatchAllPermissionDisplayName),
		Description:        itemDisplayName(policy.Description, emailCatchAllPermissionDescription),
		Target:             namespace.Address,
		PledgeText:         emailCatchAllPledgeTextClean,
		PolicyEnabled:      policy.Enabled,
		AutoApprove:        policy.AutoApprove,
		MinTrustLevel:      policy.MinTrustLevel,
		Eligible:           len(reasons) == 0,
		EligibilityReasons: reasons,
		Status:             status,
		CanApply:           canApply,
		CanManageRoute:     canManageRoute,
		Application:        summary,
	}), nil
}

// buildCatchAllEmailRouteView loads the persisted catch-all route when it
// exists and otherwise returns the placeholder row required by the public page.
func (s *PermissionService) buildCatchAllEmailRouteView(ctx context.Context, user model.User, permission UserPermissionView, namespace catchAllNamespace) (UserEmailRouteView, error) {
	snapshot, snapshotErr := s.lookupCloudflareForwardingSnapshot(ctx, namespace.RootDomain, emailCatchAllPrefix)
	item := UserEmailRouteView{
		Kind:             UserEmailRouteKindCatchAll,
		PermissionKey:    PermissionKeyEmailCatchAll,
		DisplayName:      catchAllEmailRouteDisplayName,
		Description:      catchAllEmailRouteDescription,
		Address:          namespace.Address,
		Prefix:           emailCatchAllPrefix,
		RootDomain:       namespace.RootDomain,
		TargetEmail:      "",
		Enabled:          false,
		Configured:       false,
		PermissionStatus: permission.Status,
		CanManage:        permission.CanManageRoute,
		CanDelete:        false,
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, namespace.RootDomain, emailCatchAllPrefix)
	if err != nil {
		if sqlite.IsNotFound(err) {
			if snapshotErr == nil && snapshot.Found {
				item.TargetEmail = snapshot.TargetEmail
				item.Enabled = snapshot.Enabled
				item.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
			}
			return normalizeUserEmailRouteCopy(item), nil
		}
		return UserEmailRouteView{}, InternalError("failed to load catch-all email route", err)
	}
	if route.OwnerUserID != user.ID {
		return UserEmailRouteView{}, UnavailableError("catch-all mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", route.ID, route.OwnerUserID))
	}

	updatedAt := route.UpdatedAt
	item.ID = route.ID
	item.TargetEmail = route.TargetEmail
	item.Enabled = route.Enabled
	item.Configured = strings.TrimSpace(route.TargetEmail) != ""
	item.UpdatedAt = &updatedAt
	if snapshotErr == nil && snapshot.Found {
		item.TargetEmail = snapshot.TargetEmail
		item.Enabled = snapshot.Enabled
		item.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
	}
	return normalizeUserEmailRouteCopy(item), nil
}

// resolveCatchAllNamespace derives the fixed catch-all mailbox address from the
// current username and the configured default root domain.
func (s *PermissionService) resolveCatchAllNamespace(ctx context.Context, user model.User) (catchAllNamespace, error) {
	normalizedPrefix, err := normalizedUserPrefix(user.Username)
	if err != nil {
		return catchAllNamespace{}, ValidationError("current username cannot be used as a namespace")
	}

	defaultRootDomain := strings.ToLower(strings.TrimSpace(s.cfg.Cloudflare.DefaultRootDomain))
	if defaultRootDomain == "" {
		allocations, listErr := s.db.ListAllocationsByUser(ctx, user.ID)
		if listErr != nil {
			return catchAllNamespace{}, InternalError("failed to load user allocations", listErr)
		}
		for _, allocation := range allocations {
			if allocation.NormalizedPrefix != normalizedPrefix {
				continue
			}
			return catchAllNamespace{
				RootDomain:         allocation.FQDN,
				Address:            emailCatchAllPrefix + "@" + allocation.FQDN,
				HasOwnedAllocation: true,
			}, nil
		}
		return catchAllNamespace{}, UnavailableError("default root domain is not configured for catch-all email permission", fmt.Errorf("default root domain is empty"))
	}

	namespaceRoot := normalizedPrefix + "." + defaultRootDomain
	allocations, err := s.db.ListAllocationsByUser(ctx, user.ID)
	if err != nil {
		return catchAllNamespace{}, InternalError("failed to load user allocations", err)
	}

	hasOwnedAllocation := false
	for _, allocation := range allocations {
		if allocation.NormalizedPrefix == normalizedPrefix && strings.EqualFold(allocation.RootDomain, defaultRootDomain) {
			hasOwnedAllocation = true
			break
		}
	}

	return catchAllNamespace{
		RootDomain:         namespaceRoot,
		Address:            emailCatchAllPrefix + "@" + namespaceRoot,
		HasOwnedAllocation: hasOwnedAllocation,
	}, nil
}

// findPermissionApplication picks the latest application row that matches the
// requested permission key and routed target address.
func findPermissionApplication(applications []model.AdminApplication, key string, target string) *model.AdminApplication {
	for index := range applications {
		item := applications[index]
		if item.Type == key && strings.EqualFold(item.Target, target) {
			return &item
		}
	}
	return nil
}

// permissionApplicationSummaryFromModel trims the admin-facing application row
// into the smaller shape consumed by the public frontend.
func permissionApplicationSummaryFromModel(item model.AdminApplication) *PermissionApplicationSummary {
	return &PermissionApplicationSummary{
		ID:         item.ID,
		Status:     item.Status,
		Reason:     item.Reason,
		ReviewNote: item.ReviewNote,
		ReviewedAt: item.ReviewedAt,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

// parseCatchAllTargetAddress validates and splits one catch-all mailbox target
// recorded on an application row.
func parseCatchAllTargetAddress(target string) (string, string, error) {
	localPart, rootDomain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(target)), "@")
	if !ok || strings.TrimSpace(rootDomain) == "" {
		return "", "", ValidationError("invalid catch-all application target")
	}
	if localPart != emailCatchAllPrefix {
		return "", "", ValidationError("unsupported catch-all application target")
	}
	return localPart, rootDomain, nil
}

// itemDisplayName returns the stored text when available and otherwise falls
// back to the built-in description so legacy databases still expose sane copy.
func itemDisplayName(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

// normalizeCatchAllPermissionCopy replaces any damaged legacy copy with the
// canonical public wording used by the current frontend.
func normalizeCatchAllPermissionCopy(item UserPermissionView) UserPermissionView {
	if item.Key != PermissionKeyEmailCatchAll {
		return item
	}
	item.DisplayName = itemDisplayName(item.DisplayName, emailCatchAllPermissionDisplayName)
	item.Description = emailCatchAllPermissionDescription
	item.PledgeText = emailCatchAllPledgeTextClean
	return item
}

// normalizeUserEmailRouteCopy keeps user-visible mailbox labels and
// descriptions stable even when older database rows or source strings contain
// damaged text.
func normalizeUserEmailRouteCopy(item UserEmailRouteView) UserEmailRouteView {
	switch item.Kind {
	case UserEmailRouteKindDefault:
		item.DisplayName = defaultEmailRouteDisplayName
		item.Description = defaultEmailRouteDescription
	case UserEmailRouteKindCustom:
		item.DisplayName = customEmailRouteDisplayName
		item.Description = customEmailRouteDescription
	case UserEmailRouteKindCatchAll:
		item.DisplayName = catchAllEmailRouteDisplayName
		item.Description = catchAllEmailRouteDescription
	}
	return item
}

// buildCatchAllEligibilityReasons renders the current policy gate into the
// exact copy returned by the public permission API.
func buildCatchAllEligibilityReasons(user model.User, policy model.PermissionPolicy, namespace catchAllNamespace) []string {
	reasons := make([]string, 0, 3)
	if !policy.Enabled {
		reasons = append(reasons, "This permission is currently disabled by the administrator.")
	}
	if !namespace.HasOwnedAllocation {
		reasons = append(reasons, "You do not currently own the default subdomain that matches your username, so the catch-all permission cannot be requested yet.")
	}
	if user.TrustLevel < policy.MinTrustLevel {
		reasons = append(reasons, fmt.Sprintf("Your Linux Do trust level must be at least %d, but the current level is %d.", policy.MinTrustLevel, user.TrustLevel))
	}
	return reasons
}

// disableCatchAllEmailRouteForApplication keeps the effective route in sync when
// an administrator moves a catch-all permission away from approved.
func (s *PermissionService) disableCatchAllEmailRouteForApplication(ctx context.Context, actor model.User, application model.AdminApplication) error {
	if application.Type != PermissionKeyEmailCatchAll || application.Status == "approved" {
		return nil
	}

	_, rootDomain, err := parseCatchAllTargetAddress(application.Target)
	if err != nil {
		return InternalError("failed to parse catch-all permission target", err)
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, rootDomain, emailCatchAllPrefix)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return nil
		}
		return InternalError("failed to load catch-all email route", err)
	}
	if !route.Enabled {
		return nil
	}

	beforeState := newForwardingEmailRouteSyncState(route.RootDomain, route.Prefix, route.TargetEmail, route.Enabled)
	afterState := newForwardingEmailRouteSyncState(route.RootDomain, route.Prefix, route.TargetEmail, false)
	updated := route
	if err := newEmailRoutingProvisioner(s.cfg, s.cf).SyncForwardingState(ctx, beforeState, afterState, func() error {
		var persistErr error
		updated, persistErr = s.db.UpdateEmailRoute(ctx, sqlite.UpdateEmailRouteInput{
			ID:          route.ID,
			TargetEmail: route.TargetEmail,
			Enabled:     false,
		})
		if persistErr != nil {
			return InternalError("failed to disable catch-all email route", persistErr)
		}
		return nil
	}); err != nil {
		return err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": updated.ID,
		"application_id": application.ID,
		"address":        updated.Prefix + "@" + updated.RootDomain,
		"status":         application.Status,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.disable_on_permission_update",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write catch-all disable audit log", err)
	}
	return nil
}
