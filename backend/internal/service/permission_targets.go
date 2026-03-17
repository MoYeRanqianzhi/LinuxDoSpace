package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

	// emailTargetVerificationResendCooldown prevents users from hammering the
	// resend endpoint and repeatedly burning Cloudflare destination-address quota.
	emailTargetVerificationResendCooldown = time.Minute

	// emailTargetCloudflarePropagationGrace keeps a newly created pending target
	// stable for a short window even when Cloudflare's list endpoint has not yet
	// reflected the just-created destination address. Without this grace period,
	// an immediate page refresh could clear the local binding and trigger a
	// second create attempt against the same mailbox.
	emailTargetCloudflarePropagationGrace = 10 * time.Minute
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

	unlock := s.lockEmailTargetCreate(targetEmail)
	defer unlock()

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

	unlock := s.lockEmailTargetResend(targetID)
	defer unlock()

	item, err := s.loadOwnedEmailTargetByID(ctx, user.ID, targetID)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	refreshed, err := s.syncSingleEmailTargetWithCloudflare(ctx, item, false)
	if err != nil {
		return UserEmailTargetView{}, err
	}
	if refreshed.VerifiedAt != nil {
		return UserEmailTargetView{}, ConflictError("该目标邮箱已完成验证，无需重新发送验证邮件")
	}
	if wait := remainingEmailTargetVerificationResendCooldown(refreshed, time.Now().UTC()); wait > 0 {
		return UserEmailTargetView{}, TooManyRequestsError(fmt.Sprintf("验证邮件发送过于频繁，请在 %s 后再试", formatEmailTargetResendWait(wait)))
	}

	accountID, err := s.resolveEmailRoutingAccountID(ctx)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return UserEmailTargetView{}, err
	}
	if err := s.deleteCloudflareEmailTargetBeforeResend(ctx, accountID, refreshed, snapshots); err != nil {
		return UserEmailTargetView{}, err
	}

	now := time.Now().UTC()
	created, err := s.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, refreshed.Email)
	if err != nil {
		repaired, repairedOK, repairErr := s.repairEmailTargetAfterFailedResendCreate(ctx, refreshed, now)
		if repairErr != nil {
			return UserEmailTargetView{}, repairErr
		}
		if repairedOK {
			return userEmailTargetViewFromModel(repaired), nil
		}
		return UserEmailTargetView{}, wrapEmailRoutingUnavailable("failed to recreate cloudflare email routing destination address", err)
	}

	var sentAt *time.Time
	if created.Verified == nil {
		sentAt = &now
	}

	updated, err := s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     refreshed.ID,
		CloudflareAddressID:    strings.TrimSpace(created.ID),
		VerifiedAt:             created.Verified,
		LastVerificationSentAt: sentAt,
	})
	if err != nil {
		return UserEmailTargetView{}, s.rollbackEmailTargetAfterFailedResendUpdate(ctx, accountID, refreshed, strings.TrimSpace(created.ID), err)
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

// loadOwnedEmailTargetByID loads one email-target row owned by the specified
// user. The current storage contract does not expose a direct lookup by id, so
// the service narrows the owned set in memory.
func (s *PermissionService) loadOwnedEmailTargetByID(ctx context.Context, ownerUserID int64, targetID int64) (model.EmailTarget, error) {
	items, err := s.db.ListEmailTargetsByOwner(ctx, ownerUserID)
	if err != nil {
		return model.EmailTarget{}, InternalError("failed to load user email targets", err)
	}
	for _, item := range items {
		if item.ID == targetID {
			return item, nil
		}
	}
	return model.EmailTarget{}, NotFoundError("email target not found")
}

// remainingEmailTargetVerificationResendCooldown reports how long the caller
// still has to wait before another resend becomes allowed.
func remainingEmailTargetVerificationResendCooldown(item model.EmailTarget, now time.Time) time.Duration {
	if item.LastVerificationSentAt == nil {
		return 0
	}
	readyAt := item.LastVerificationSentAt.UTC().Add(emailTargetVerificationResendCooldown)
	if !readyAt.After(now.UTC()) {
		return 0
	}
	return readyAt.Sub(now.UTC())
}

// formatEmailTargetResendWait renders the cooldown into a compact Chinese
// string so the API can tell the frontend exactly when another resend is safe.
func formatEmailTargetResendWait(wait time.Duration) string {
	if wait <= 0 {
		return "稍后"
	}

	wait = wait.Round(time.Second)
	if wait < time.Second {
		wait = time.Second
	}

	if wait < time.Minute {
		return fmt.Sprintf("%d 秒", int(wait/time.Second))
	}

	minutes := int(wait / time.Minute)
	seconds := int((wait % time.Minute) / time.Second)
	if seconds == 0 {
		return fmt.Sprintf("%d 分钟", minutes)
	}
	return fmt.Sprintf("%d 分 %d 秒", minutes, seconds)
}

// deleteCloudflareEmailTargetBeforeResend prefers the locally bound
// CloudflareAddressID and only falls back to the latest Cloudflare snapshot id
// when the local row is already stale. This narrows the chance of deleting an
// unrelated remote address after prior drift.
func (s *PermissionService) deleteCloudflareEmailTargetBeforeResend(ctx context.Context, accountID string, item model.EmailTarget, snapshots map[string]cloudflareEmailTargetSnapshot) error {
	emailKey := strings.ToLower(strings.TrimSpace(item.Email))
	snapshot := snapshots[emailKey]

	candidates := make([]string, 0, 2)
	if currentID := strings.TrimSpace(item.CloudflareAddressID); currentID != "" {
		candidates = append(candidates, currentID)
	}
	if snapshotID := strings.TrimSpace(snapshot.AddressID); snapshotID != "" && (len(candidates) == 0 || candidates[0] != snapshotID) {
		candidates = append(candidates, snapshotID)
	}
	if len(candidates) == 0 {
		return nil
	}

	var deleteErr error
	for _, candidateID := range candidates {
		deleteErr = s.cf.DeleteEmailRoutingDestinationAddress(ctx, accountID, candidateID)
		if deleteErr == nil {
			return nil
		}
	}
	return wrapEmailRoutingUnavailable("failed to delete cloudflare email routing destination address before resending verification", deleteErr)
}

// repairEmailTargetAfterFailedResendCreate reconciles the local database after
// Cloudflare recreate returned an error. When the remote create actually
// succeeded but the response failed, the follow-up snapshot repair turns the
// operation into a successful resend. Otherwise the stale local id is cleared.
func (s *PermissionService) repairEmailTargetAfterFailedResendCreate(ctx context.Context, item model.EmailTarget, resendSentAt time.Time) (model.EmailTarget, bool, error) {
	repaired, err := s.forceSyncEmailTargetWithCloudflare(ctx, item)
	if err != nil {
		return model.EmailTarget{}, false, err
	}
	if strings.TrimSpace(repaired.CloudflareAddressID) == "" {
		return repaired, false, nil
	}
	if repaired.VerifiedAt != nil {
		return repaired, true, nil
	}
	if equalOptionalTimes(repaired.LastVerificationSentAt, &resendSentAt) {
		return repaired, true, nil
	}

	repaired, err = s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     repaired.ID,
		CloudflareAddressID:    repaired.CloudflareAddressID,
		VerifiedAt:             repaired.VerifiedAt,
		LastVerificationSentAt: &resendSentAt,
	})
	if err != nil {
		return model.EmailTarget{}, false, InternalError("failed to repair email target after recreate response loss", err)
	}
	return repaired, true, nil
}

// rollbackEmailTargetAfterFailedResendUpdate compensates for the case where the
// new Cloudflare destination was created but the local database update failed.
// The rollback removes the freshly created remote address and then re-syncs the
// local row so the user does not remain stuck with a dangling Cloudflare id.
func (s *PermissionService) rollbackEmailTargetAfterFailedResendUpdate(ctx context.Context, accountID string, previous model.EmailTarget, createdID string, updateErr error) error {
	var rollbackErrs []error

	if createdID = strings.TrimSpace(createdID); createdID != "" {
		if deleteErr := s.cf.DeleteEmailRoutingDestinationAddress(ctx, accountID, createdID); deleteErr != nil {
			rollbackErrs = append(rollbackErrs, wrapEmailRoutingUnavailable("failed to roll back recreated cloudflare email routing destination address", deleteErr))
		}
	}

	if _, repairErr := s.forceSyncEmailTargetWithCloudflare(ctx, previous); repairErr != nil {
		rollbackErrs = append(rollbackErrs, repairErr)
	}

	rollbackErrs = append([]error{InternalError("failed to update email target after resending verification", updateErr)}, rollbackErrs...)
	return errors.Join(rollbackErrs...)
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
			repaired, repairedOK, repairErr := s.repairCreatedEmailTargetAfterFailedCreate(ctx, user, targetEmail)
			if repairErr != nil {
				return model.EmailTarget{}, repairErr
			}
			if repairedOK {
				return repaired, nil
			}
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
		if shouldKeepPendingEmailTargetWithoutSnapshot(item, time.Now().UTC()) {
			return item, nil
		}
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

		now := time.Now().UTC()
		created, err := s.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, item.Email)
		if err != nil {
			repaired, repairedOK, repairErr := s.repairEmailTargetAfterFailedCreate(ctx, item, now)
			if repairErr != nil {
				return model.EmailTarget{}, repairErr
			}
			if repairedOK {
				return repaired, nil
			}
			return model.EmailTarget{}, wrapEmailRoutingUnavailable("failed to create cloudflare email routing destination address", err)
		}

		var sentAt *time.Time
		if created.Verified == nil {
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

// shouldKeepPendingEmailTargetWithoutSnapshot preserves a fresh pending target
// when Cloudflare's destination-address list still lags behind the create or
// resend request. The local row is intentionally left untouched during this
// grace window so repeated page loads stay idempotent instead of recreating the
// same target mailbox over and over.
func shouldKeepPendingEmailTargetWithoutSnapshot(item model.EmailTarget, now time.Time) bool {
	if item.VerifiedAt != nil {
		return false
	}
	if strings.TrimSpace(item.CloudflareAddressID) == "" {
		return false
	}
	if item.LastVerificationSentAt == nil {
		return false
	}
	return item.LastVerificationSentAt.UTC().Add(emailTargetCloudflarePropagationGrace).After(now.UTC())
}

// repairCreatedEmailTargetAfterFailedCreate checks whether the remote create
// actually succeeded despite returning an error and, if so, materializes the
// corresponding local ownership row instead of surfacing a false failure.
func (s *PermissionService) repairCreatedEmailTargetAfterFailedCreate(ctx context.Context, user model.User, targetEmail string) (model.EmailTarget, bool, error) {
	snapshot, found, err := s.lookupCloudflareEmailTargetSnapshot(ctx, targetEmail)
	if err != nil {
		return model.EmailTarget{}, false, err
	}
	if !found || strings.TrimSpace(snapshot.AddressID) == "" {
		return model.EmailTarget{}, false, nil
	}

	var sentAt *time.Time
	if snapshot.VerifiedAt == nil {
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
					return model.EmailTarget{}, false, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
				}
				repaired, repairErr := s.syncSingleEmailTargetWithCloudflare(ctx, existing, false)
				if repairErr != nil {
					return model.EmailTarget{}, false, repairErr
				}
				return repaired, true, nil
			case storage.IsNotFound(existingErr):
				return model.EmailTarget{}, false, ConflictError("该转发目标邮箱已被其他请求占用，请刷新后重试")
			default:
				return model.EmailTarget{}, false, InternalError("failed to recover repaired email target binding after unique conflict", existingErr)
			}
		}
		return model.EmailTarget{}, false, InternalError("failed to save repaired email target binding", err)
	}
	return item, true, nil
}

// repairEmailTargetAfterFailedCreate reconciles an existing local target row
// after Cloudflare create returned an error. This covers both response-loss
// scenarios and duplicate-create races where the remote destination now exists
// even though the immediate API call failed.
func (s *PermissionService) repairEmailTargetAfterFailedCreate(ctx context.Context, item model.EmailTarget, sentAt time.Time) (model.EmailTarget, bool, error) {
	repaired, err := s.forceSyncEmailTargetWithCloudflare(ctx, item)
	if err != nil {
		return model.EmailTarget{}, false, err
	}
	if strings.TrimSpace(repaired.CloudflareAddressID) == "" {
		return repaired, false, nil
	}
	if repaired.VerifiedAt != nil {
		return repaired, true, nil
	}
	if equalOptionalTimes(repaired.LastVerificationSentAt, &sentAt) {
		return repaired, true, nil
	}

	repaired, err = s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     repaired.ID,
		CloudflareAddressID:    repaired.CloudflareAddressID,
		VerifiedAt:             repaired.VerifiedAt,
		LastVerificationSentAt: &sentAt,
	})
	if err != nil {
		return model.EmailTarget{}, false, InternalError("failed to repair email target after create response loss", err)
	}
	return repaired, true, nil
}

// lookupCloudflareEmailTargetSnapshot returns the latest destination-address
// snapshot for one normalized email address.
func (s *PermissionService) lookupCloudflareEmailTargetSnapshot(ctx context.Context, targetEmail string) (cloudflareEmailTargetSnapshot, bool, error) {
	snapshots, err := s.listCloudflareEmailTargetSnapshots(ctx)
	if err != nil {
		return cloudflareEmailTargetSnapshot{}, false, err
	}
	snapshot, found := snapshots[strings.ToLower(strings.TrimSpace(targetEmail))]
	if !found {
		return cloudflareEmailTargetSnapshot{}, false, nil
	}
	return snapshot, true, nil
}

// forceSyncEmailTargetWithCloudflare reconciles one local row against the
// current Cloudflare destination-address snapshot without applying the normal
// propagation grace window. Repair and rollback paths use this stricter helper
// because they must clear stale ids immediately after an explicit failure.
func (s *PermissionService) forceSyncEmailTargetWithCloudflare(ctx context.Context, item model.EmailTarget) (model.EmailTarget, error) {
	snapshot, found, err := s.lookupCloudflareEmailTargetSnapshot(ctx, item.Email)
	if err != nil {
		return model.EmailTarget{}, err
	}
	if !found {
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
	return strings.Contains(normalized, "unique constraint failed: email_targets.email") ||
		(strings.Contains(normalized, "duplicate key") && strings.Contains(normalized, "email_targets"))
}
