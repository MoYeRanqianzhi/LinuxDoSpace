package service

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

const (
	// EmailTargetVerificationPending means Cloudflare has not yet reported the
	// destination address as verified, so it cannot safely be used for routing.
	EmailTargetVerificationPending = "pending"

	// EmailTargetVerificationVerified means Cloudflare has confirmed ownership
	// of the external inbox and the address can be selected for forwarding.
	EmailTargetVerificationVerified = "verified"
)

// UserEmailTargetView describes one user-owned forwarding destination email
// shown on the public email page.
type UserEmailTargetView struct {
	ID                     int64      `json:"id"`
	Email                  string     `json:"email"`
	CloudflareAddressID    string     `json:"cloudflare_address_id,omitempty"`
	VerificationStatus     string     `json:"verification_status"`
	Verified               bool       `json:"verified"`
	VerifiedAt             *time.Time `json:"verified_at,omitempty"`
	LastVerificationSentAt *time.Time `json:"last_verification_sent_at,omitempty"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

// CreateMyEmailTargetRequest describes one user-authored request to add a new
// forwarding destination address under the current account.
type CreateMyEmailTargetRequest struct {
	Email string `json:"email"`
}

// cloudflareEmailTargetSnapshot is the narrow subset of Cloudflare destination
// address fields needed by the ownership-binding workflow.
type cloudflareEmailTargetSnapshot struct {
	AddressID  string
	VerifiedAt *time.Time
}

// ListMyEmailTargets returns all forwarding destination emails currently bound
// to the authenticated user. The service opportunistically syncs Cloudflare
// verification status on every read so the frontend can refresh after the user
// clicks the confirmation email.
func (s *PermissionService) ListMyEmailTargets(ctx context.Context, user model.User) ([]UserEmailTargetView, error) {
	if err := s.backfillOwnedEmailTargetsFromRoutes(ctx, user); err != nil {
		return nil, err
	}

	items, err := s.db.ListEmailTargetsByOwner(ctx, user.ID)
	if err != nil {
		return nil, InternalError("failed to load user email targets", err)
	}

	syncedItems, err := s.syncEmailTargetsWithCloudflare(ctx, items, false)
	if err != nil {
		return nil, err
	}

	views := make([]UserEmailTargetView, 0, len(syncedItems))
	for _, item := range syncedItems {
		views = append(views, userEmailTargetViewFromModel(item))
	}
	return views, nil
}

// CreateMyEmailTarget binds one forwarding destination email to the current
// user. When the destination is new, Cloudflare sends the verification email
// immediately and the returned row remains pending until the owner confirms it.
func (s *PermissionService) CreateMyEmailTarget(ctx context.Context, user model.User, request CreateMyEmailTargetRequest) (UserEmailTargetView, error) {
	targetEmail, err := normalizeTargetEmail(request.Email, false)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	item, err := s.ensureOwnedEmailTarget(ctx, user, targetEmail, true)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_target_id":       item.ID,
		"email":                 item.Email,
		"verification_status":   emailTargetVerificationStatus(item),
		"cloudflare_address_id": item.CloudflareAddressID,
	})
	logAuditWriteFailure("email_target.create_or_sync", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_target.create_or_sync",
		ResourceType: "email_target",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return userEmailTargetViewFromModel(item), nil
}

// ResendMyEmailTargetVerification retriggers the Cloudflare verification email
// for one still-pending target mailbox owned by the current user.
func (s *PermissionService) ResendMyEmailTargetVerification(ctx context.Context, user model.User, targetID int64) (UserEmailTargetView, error) {
	if targetID <= 0 {
		return UserEmailTargetView{}, ValidationError("targetID is required")
	}

	items, err := s.db.ListEmailTargetsByOwner(ctx, user.ID)
	if err != nil {
		return UserEmailTargetView{}, InternalError("failed to load user email targets", err)
	}

	var item *model.EmailTarget
	for index := range items {
		if items[index].ID == targetID {
			item = &items[index]
			break
		}
	}
	if item == nil {
		return UserEmailTargetView{}, NotFoundError("email target not found")
	}

	refreshed, err := s.syncSingleEmailTargetWithCloudflare(ctx, *item, false)
	if err != nil {
		return UserEmailTargetView{}, err
	}
	if refreshed.VerifiedAt != nil {
		return UserEmailTargetView{}, ConflictError("该目标邮箱已完成验证，无需重新发送验证邮件")
	}

	accountID, err := s.resolveEmailRoutingAccountID(ctx)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return UserEmailTargetView{}, err
	}
	if snapshot, found := snapshots[strings.ToLower(strings.TrimSpace(refreshed.Email))]; found && strings.TrimSpace(snapshot.AddressID) != "" {
		if deleteErr := s.cf.DeleteEmailRoutingDestinationAddress(ctx, accountID, snapshot.AddressID); deleteErr != nil {
			return UserEmailTargetView{}, wrapEmailRoutingUnavailable("failed to delete cloudflare email routing destination address before resending verification", deleteErr)
		}
	}

	created, err := s.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, refreshed.Email)
	if err != nil {
		return UserEmailTargetView{}, wrapEmailRoutingUnavailable("failed to recreate cloudflare email routing destination address", err)
	}

	var sentAt *time.Time
	if created.Verified == nil {
		now := time.Now().UTC()
		sentAt = &now
	}

	updated, err := s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     refreshed.ID,
		CloudflareAddressID:    strings.TrimSpace(created.ID),
		VerifiedAt:             created.Verified,
		LastVerificationSentAt: sentAt,
	})
	if err != nil {
		return UserEmailTargetView{}, InternalError("failed to update email target after resending verification", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_target_id":       updated.ID,
		"email":                 updated.Email,
		"verification_status":   emailTargetVerificationStatus(updated),
		"cloudflare_address_id": updated.CloudflareAddressID,
	})
	logAuditWriteFailure("email_target.resend_verification", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_target.resend_verification",
		ResourceType: "email_target",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return userEmailTargetViewFromModel(updated), nil
}

// requireVerifiedOwnedEmailTarget enforces that one forwarding target email is
// already bound to the current user and verified by Cloudflare.
func (s *PermissionService) requireVerifiedOwnedEmailTarget(ctx context.Context, user model.User, targetEmail string) (model.EmailTarget, error) {
	item, err := s.ensureOwnedEmailTarget(ctx, user, targetEmail, false)
	if err != nil {
		return model.EmailTarget{}, err
	}
	if item.VerifiedAt == nil {
		return model.EmailTarget{}, ConflictError("该转发目标邮箱尚未完成 Cloudflare 验证，请先在“我的目标邮箱列表”中完成验证")
	}
	return item, nil
}

// ensureOwnedEmailTarget loads one target email binding for the current user
// and optionally creates or recreates the Cloudflare destination when the
// caller explicitly requests that behavior.
func (s *PermissionService) ensureOwnedEmailTarget(ctx context.Context, user model.User, targetEmail string, allowCreate bool) (model.EmailTarget, error) {
	item, err := s.db.GetEmailTargetByEmail(ctx, targetEmail)
	switch {
	case err == nil:
		if item.OwnerUserID != user.ID {
			return model.EmailTarget{}, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
		}
		return s.syncSingleEmailTargetWithCloudflare(ctx, item, allowCreate)
	case storage.IsNotFound(err):
		if !allowCreate {
			return model.EmailTarget{}, ConflictError("该转发目标邮箱尚未绑定到你的账号，请先在“我的目标邮箱列表”中添加并完成验证")
		}
		return s.createOwnedEmailTarget(ctx, user, targetEmail)
	default:
		return model.EmailTarget{}, InternalError("failed to load email target binding", err)
	}
}

// backfillOwnedEmailTargetsFromRoutes migrates legacy route-only target emails
// into the dedicated ownership table so existing users do not lose access when
// the new binding rules are introduced.
func (s *PermissionService) backfillOwnedEmailTargetsFromRoutes(ctx context.Context, user model.User) error {
	routes, err := s.db.ListEmailRoutesByOwner(ctx, user.ID)
	if err != nil {
		return InternalError("failed to load user email routes for target backfill", err)
	}

	existingTargets, err := s.db.ListEmailTargetsByOwner(ctx, user.ID)
	if err != nil {
		return InternalError("failed to load user email targets for backfill", err)
	}

	existingEmails := make(map[string]struct{}, len(existingTargets))
	for _, item := range existingTargets {
		existingEmails[strings.ToLower(strings.TrimSpace(item.Email))] = struct{}{}
	}

	legacyTargets := make(map[string]struct{})
	for _, route := range routes {
		targetEmail := strings.ToLower(strings.TrimSpace(route.TargetEmail))
		if targetEmail == "" {
			continue
		}
		if _, alreadyKnown := existingEmails[targetEmail]; alreadyKnown {
			continue
		}
		legacyTargets[targetEmail] = struct{}{}
	}
	if len(legacyTargets) == 0 {
		return nil
	}

	cloudflareSnapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return err
	}

	for email := range legacyTargets {
		existing, existingErr := s.db.GetEmailTargetByEmail(ctx, email)
		switch {
		case existingErr == nil:
			if existing.OwnerUserID != user.ID {
				continue
			}
		case storage.IsNotFound(existingErr):
			snapshot := cloudflareSnapshots[email]
			created, createErr := s.db.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
				OwnerUserID:            user.ID,
				Email:                  email,
				CloudflareAddressID:    snapshot.AddressID,
				VerifiedAt:             snapshot.VerifiedAt,
				LastVerificationSentAt: nil,
			})
			if createErr != nil {
				return InternalError("failed to backfill legacy email target binding", createErr)
			}

			metadata, _ := json.Marshal(map[string]any{
				"email_target_id": created.ID,
				"email":           created.Email,
				"source":          "legacy_email_route_backfill",
			})
			logAuditWriteFailure("email_target.backfill", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
				ActorUserID:  &user.ID,
				Action:       "email_target.backfill",
				ResourceType: "email_target",
				ResourceID:   strconv.FormatInt(created.ID, 10),
				MetadataJSON: string(metadata),
			}))
		default:
			return InternalError("failed to inspect legacy email target ownership", existingErr)
		}
	}

	return nil
}

// createOwnedEmailTarget creates a new local ownership row and, when necessary,
// asks Cloudflare to send the verification email for the destination address.
func (s *PermissionService) createOwnedEmailTarget(ctx context.Context, user model.User, targetEmail string) (model.EmailTarget, error) {
	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return model.EmailTarget{}, err
	}

	snapshot, found := snapshots[targetEmail]
	if !found {
		accountID, accountErr := s.resolveEmailRoutingAccountID(ctx)
		if accountErr != nil {
			return model.EmailTarget{}, accountErr
		}

		created, createErr := s.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, targetEmail)
		if createErr != nil {
			return model.EmailTarget{}, wrapEmailRoutingUnavailable("failed to create cloudflare email routing destination address", createErr)
		}
		snapshot = cloudflareEmailTargetSnapshot{
			AddressID:  strings.TrimSpace(created.ID),
			VerifiedAt: created.Verified,
		}
		found = true
	}

	var sentAt *time.Time
	if snapshot.VerifiedAt == nil && found {
		now := time.Now().UTC()
		sentAt = &now
	}

	item, err := s.db.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
		OwnerUserID:            user.ID,
		Email:                  targetEmail,
		CloudflareAddressID:    snapshot.AddressID,
		VerifiedAt:             snapshot.VerifiedAt,
		LastVerificationSentAt: sentAt,
	})
	if err != nil {
		if isEmailTargetUniqueConflict(err) {
			existing, existingErr := s.db.GetEmailTargetByEmail(ctx, targetEmail)
			switch {
			case existingErr == nil:
				if existing.OwnerUserID != user.ID {
					return model.EmailTarget{}, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
				}
				return s.syncSingleEmailTargetWithCloudflare(ctx, existing, false)
			case storage.IsNotFound(existingErr):
				return model.EmailTarget{}, ConflictError("该转发目标邮箱已被其他请求占用，请刷新后重试")
			default:
				return model.EmailTarget{}, InternalError("failed to recover email target binding after unique conflict", existingErr)
			}
		}
		return model.EmailTarget{}, InternalError("failed to save email target binding", err)
	}
	return item, nil
}

// syncEmailTargetsWithCloudflare refreshes a batch of local email target rows
// from the current Cloudflare destination-address list.
func (s *PermissionService) syncEmailTargetsWithCloudflare(ctx context.Context, items []model.EmailTarget, allowCreate bool) ([]model.EmailTarget, error) {
	if len(items) == 0 {
		return items, nil
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return items, nil
	}

	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return nil, err
	}

	updatedItems := make([]model.EmailTarget, 0, len(items))
	for _, item := range items {
		updated, updateErr := s.syncEmailTargetAgainstSnapshots(ctx, item, snapshots, allowCreate)
		if updateErr != nil {
			return nil, updateErr
		}
		updatedItems = append(updatedItems, updated)
	}
	return updatedItems, nil
}

// syncSingleEmailTargetWithCloudflare refreshes one local email target row and
// optionally recreates the missing Cloudflare destination address.
func (s *PermissionService) syncSingleEmailTargetWithCloudflare(ctx context.Context, item model.EmailTarget, allowCreate bool) (model.EmailTarget, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return item, nil
	}

	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return model.EmailTarget{}, err
	}
	return s.syncEmailTargetAgainstSnapshots(ctx, item, snapshots, allowCreate)
}

// syncEmailTargetAgainstSnapshots reconciles one local target row with the
// latest Cloudflare destination-address snapshot map.
func (s *PermissionService) syncEmailTargetAgainstSnapshots(ctx context.Context, item model.EmailTarget, snapshots map[string]cloudflareEmailTargetSnapshot, allowCreate bool) (model.EmailTarget, error) {
	snapshot, found := snapshots[strings.ToLower(strings.TrimSpace(item.Email))]
	if !found {
		if !allowCreate {
			if strings.TrimSpace(item.CloudflareAddressID) == "" && item.VerifiedAt == nil {
				return item, nil
			}
			return s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
				ID:                     item.ID,
				CloudflareAddressID:    "",
				VerifiedAt:             nil,
				LastVerificationSentAt: item.LastVerificationSentAt,
			})
		}

		accountID, err := s.resolveEmailRoutingAccountID(ctx)
		if err != nil {
			return model.EmailTarget{}, err
		}

		created, err := s.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, item.Email)
		if err != nil {
			return model.EmailTarget{}, wrapEmailRoutingUnavailable("failed to create cloudflare email routing destination address", err)
		}

		var sentAt *time.Time
		if created.Verified == nil {
			now := time.Now().UTC()
			sentAt = &now
		}
		return s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
			ID:                     item.ID,
			CloudflareAddressID:    strings.TrimSpace(created.ID),
			VerifiedAt:             created.Verified,
			LastVerificationSentAt: sentAt,
		})
	}

	if strings.TrimSpace(item.CloudflareAddressID) == snapshot.AddressID && equalOptionalTimes(item.VerifiedAt, snapshot.VerifiedAt) {
		return item, nil
	}

	return s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     item.ID,
		CloudflareAddressID:    snapshot.AddressID,
		VerifiedAt:             snapshot.VerifiedAt,
		LastVerificationSentAt: item.LastVerificationSentAt,
	})
}

// listCloudflareEmailTargetSnapshots returns the current Cloudflare destination
// address list keyed by normalized email address.
func (s *PermissionService) listCloudflareEmailTargetSnapshots(ctx context.Context) (map[string]cloudflareEmailTargetSnapshot, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return map[string]cloudflareEmailTargetSnapshot{}, nil
	}

	accountID, err := s.resolveEmailRoutingAccountID(ctx)
	if err != nil {
		return nil, err
	}

	addresses, err := s.cf.ListEmailRoutingDestinationAddresses(ctx, accountID)
	if err != nil {
		return nil, wrapEmailRoutingUnavailable("failed to list cloudflare email routing destination addresses", err)
	}

	items := make(map[string]cloudflareEmailTargetSnapshot, len(addresses))
	for _, item := range addresses {
		email := strings.ToLower(strings.TrimSpace(item.Email))
		if email == "" {
			continue
		}
		items[email] = cloudflareEmailTargetSnapshot{
			AddressID:  strings.TrimSpace(item.ID),
			VerifiedAt: item.Verified,
		}
	}
	return items, nil
}

// resolveEmailRoutingAccountID derives the Cloudflare account id used for the
// shared destination-address API from the default email root configuration.
func (s *PermissionService) resolveEmailRoutingAccountID(ctx context.Context) (string, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)
	if err := routing.ensureConfigured(); err != nil {
		return "", err
	}

	managedDomain, err := s.resolveAvailableEmailRootDomain(ctx, s.cfg.Cloudflare.DefaultRootDomain)
	if err != nil {
		return "", err
	}

	zoneID, err := routing.resolveZoneID(ctx, managedDomain.RootDomain)
	if err != nil {
		return "", err
	}
	return routing.resolveAccountID(ctx, zoneID)
}

// userEmailTargetViewFromModel converts one persisted ownership row into the
// public JSON payload consumed by the email page.
func userEmailTargetViewFromModel(item model.EmailTarget) UserEmailTargetView {
	return UserEmailTargetView{
		ID:                     item.ID,
		Email:                  item.Email,
		CloudflareAddressID:    item.CloudflareAddressID,
		VerificationStatus:     emailTargetVerificationStatus(item),
		Verified:               item.VerifiedAt != nil,
		VerifiedAt:             item.VerifiedAt,
		LastVerificationSentAt: item.LastVerificationSentAt,
		CreatedAt:              item.CreatedAt,
		UpdatedAt:              item.UpdatedAt,
	}
}

// emailTargetVerificationStatus centralizes the UI-facing status string for one
// forwarding destination.
func emailTargetVerificationStatus(item model.EmailTarget) string {
	if item.VerifiedAt != nil {
		return EmailTargetVerificationVerified
	}
	return EmailTargetVerificationPending
}

// equalOptionalTimes keeps Cloudflare sync writes idempotent.
func equalOptionalTimes(left *time.Time, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.UTC().Equal(right.UTC())
	}
}

// isEmailTargetUniqueConflict recognizes the SQLite unique-constraint message
// for the globally unique email_targets.email column so the service can turn a
// race condition into a deterministic ownership decision.
func isEmailTargetUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(normalized, "unique constraint failed: email_targets.email")
}
