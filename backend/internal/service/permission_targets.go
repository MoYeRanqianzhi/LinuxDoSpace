package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage"
)

const (
	// EmailTargetVerificationPending means LinuxDoSpace has not yet seen the
	// owner open the platform-issued verification link for this target inbox.
	EmailTargetVerificationPending = "pending"

	// EmailTargetVerificationVerified means the owner completed LinuxDoSpace's
	// own verification link flow and the inbox can now be used for forwarding.
	EmailTargetVerificationVerified = "verified"

	// emailTargetVerificationResendCooldown prevents the resend endpoint from
	// being hammered so one user cannot force repeated outbound verification
	// messages to the same external inbox in a tight loop.
	emailTargetVerificationResendCooldown = time.Minute

	// emailTargetVerificationOwnerShortLimit caps how many verification sends one
	// account can reserve within the short anti-spam burst window.
	emailTargetVerificationOwnerShortLimit = 5

	// emailTargetVerificationOwnerDailyLimit caps how many verification sends one
	// account can reserve within a rolling 24-hour period.
	emailTargetVerificationOwnerDailyLimit = 20

	// emailTargetVerificationTargetShortLimit caps short-burst sends to the same
	// external inbox, even if the caller retries from multiple sessions.
	emailTargetVerificationTargetShortLimit = 3

	// emailTargetVerificationTargetDailyLimit caps rolling 24-hour sends to the
	// same external inbox to keep LinuxDoSpace from becoming an outbound spammer.
	emailTargetVerificationTargetDailyLimit = 10
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

// EmailTargetVerificationResult is the small public outcome returned after one
// verification link is consumed.
type EmailTargetVerificationResult struct {
	Email      string
	VerifiedAt time.Time
}

// CreateMyEmailTargetRequest describes one user-authored request to add a new
// forwarding destination address under the current account.
type CreateMyEmailTargetRequest struct {
	Email string `json:"email"`
}

// ListMyEmailTargets returns all forwarding destination emails currently bound
// to the authenticated user. Verification state is now entirely local, so the
// database is the only source of truth and no Cloudflare reconciliation is
// required during read paths anymore.
func (s *PermissionService) ListMyEmailTargets(ctx context.Context, user model.User) ([]UserEmailTargetView, error) {
	if err := s.backfillOwnedEmailTargetsFromRoutes(ctx, user); err != nil {
		return nil, err
	}

	items, err := s.db.ListEmailTargetsByOwner(ctx, user.ID)
	if err != nil {
		return nil, InternalError("failed to load user email targets", err)
	}

	views := make([]UserEmailTargetView, 0, len(items))
	for _, item := range items {
		views = append(views, userEmailTargetViewFromModel(item))
	}
	return views, nil
}

// CreateMyEmailTarget binds one forwarding destination email to the current
// user. The mailbox stays pending until the owner opens LinuxDoSpace's own
// verification link sent directly to that inbox.
func (s *PermissionService) CreateMyEmailTarget(ctx context.Context, user model.User, request CreateMyEmailTargetRequest) (UserEmailTargetView, error) {
	targetEmail, err := normalizeTargetEmail(request.Email, false)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	unlock := s.lockEmailTargetCreate(targetEmail)
	defer unlock()

	item, created, err := s.loadOrCreateOwnedEmailTarget(ctx, user, targetEmail)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	now := time.Now().UTC()
	if item.VerifiedAt == nil && (created || shouldIssueVerificationOnCreate(item, now)) {
		item, err = s.issueEmailTargetVerification(ctx, item)
		if err != nil {
			return UserEmailTargetView{}, err
		}
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_target_id":     item.ID,
		"email":               item.Email,
		"verification_status": emailTargetVerificationStatus(item),
		"delivery_mode":       "platform_mailer",
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

// ResendMyEmailTargetVerification triggers a fresh LinuxDoSpace verification
// email for one still-pending target mailbox owned by the current user.
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
	if item.VerifiedAt != nil {
		return UserEmailTargetView{}, ConflictError("该目标邮箱已完成验证，无需重新发送验证邮件")
	}
	if wait := remainingEmailTargetVerificationResendCooldown(item, time.Now().UTC()); wait > 0 {
		return UserEmailTargetView{}, TooManyRequestsError(fmt.Sprintf("验证邮件发送过于频繁，请在 %s 后再试", formatEmailTargetResendWait(wait)))
	}

	item, err = s.issueEmailTargetVerification(ctx, item)
	if err != nil {
		return UserEmailTargetView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_target_id":     item.ID,
		"email":               item.Email,
		"verification_status": emailTargetVerificationStatus(item),
		"delivery_mode":       "platform_mailer",
	})
	logAuditWriteFailure("email_target.resend_verification", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_target.resend_verification",
		ResourceType: "email_target",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return userEmailTargetViewFromModel(item), nil
}

// VerifyEmailTarget consumes one public verification token, marks the matching
// target inbox as verified, and clears the single-use token immediately.
func (s *PermissionService) VerifyEmailTarget(ctx context.Context, token string) (EmailTargetVerificationResult, error) {
	normalizedToken := strings.TrimSpace(token)
	if normalizedToken == "" {
		return EmailTargetVerificationResult{}, ValidationError("verification token is required")
	}

	now := time.Now().UTC()
	verifiedItem, err := s.db.ConsumeEmailTargetVerificationToken(ctx, hashEmailTargetVerificationToken(normalizedToken), now)
	if err != nil {
		switch {
		case storage.IsNotFound(err):
			return EmailTargetVerificationResult{}, NotFoundError("verification token is invalid or already used")
		case err == storage.ErrEmailTargetVerificationExpired:
			return EmailTargetVerificationResult{}, ValidationError("verification token has expired")
		default:
			return EmailTargetVerificationResult{}, InternalError("failed to verify email target", err)
		}
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_target_id":     verifiedItem.ID,
		"email":               verifiedItem.Email,
		"verification_status": emailTargetVerificationStatus(verifiedItem),
	})
	logAuditWriteFailure("email_target.verify", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &verifiedItem.OwnerUserID,
		Action:       "email_target.verify",
		ResourceType: "email_target",
		ResourceID:   strconv.FormatInt(verifiedItem.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return EmailTargetVerificationResult{
		Email:      verifiedItem.Email,
		VerifiedAt: now,
	}, nil
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

// requireVerifiedOwnedEmailTarget enforces that one forwarding target email is
// already bound to the current user and verified by LinuxDoSpace itself.
func (s *PermissionService) requireVerifiedOwnedEmailTarget(ctx context.Context, user model.User, targetEmail string) (model.EmailTarget, error) {
	item, err := s.db.GetEmailTargetByEmail(ctx, targetEmail)
	switch {
	case err == nil:
		if item.OwnerUserID != user.ID {
			return model.EmailTarget{}, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
		}
	case storage.IsNotFound(err):
		return model.EmailTarget{}, ConflictError("该转发目标邮箱尚未绑定到你的账号，请先在“我的目标邮箱列表”中添加并完成验证")
	default:
		return model.EmailTarget{}, InternalError("failed to load email target binding", err)
	}

	if item.VerifiedAt == nil {
		return model.EmailTarget{}, ConflictError("该转发目标邮箱尚未完成平台验证，请先在“我的目标邮箱列表”中完成验证")
	}
	return item, nil
}

// loadOrCreateOwnedEmailTarget returns the existing ownership row or creates a
// new pending row when the mailbox is still unknown locally.
func (s *PermissionService) loadOrCreateOwnedEmailTarget(ctx context.Context, user model.User, targetEmail string) (model.EmailTarget, bool, error) {
	item, err := s.db.GetEmailTargetByEmail(ctx, targetEmail)
	switch {
	case err == nil:
		if item.OwnerUserID != user.ID {
			return model.EmailTarget{}, false, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
		}
		return item, false, nil
	case storage.IsNotFound(err):
		item, createErr := s.db.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
			OwnerUserID:            user.ID,
			Email:                  targetEmail,
			CloudflareAddressID:    "",
			VerificationTokenHash:  "",
			VerificationExpiresAt:  nil,
			VerifiedAt:             nil,
			LastVerificationSentAt: nil,
		})
		if createErr != nil {
			if isEmailTargetUniqueConflict(createErr) {
				existing, existingErr := s.db.GetEmailTargetByEmail(ctx, targetEmail)
				switch {
				case existingErr == nil:
					if existing.OwnerUserID != user.ID {
						return model.EmailTarget{}, false, ForbiddenError("该转发目标邮箱已经绑定到其他账号，不能重复使用")
					}
					return existing, false, nil
				case storage.IsNotFound(existingErr):
					return model.EmailTarget{}, false, ConflictError("该转发目标邮箱已被其他请求占用，请刷新后重试")
				default:
					return model.EmailTarget{}, false, InternalError("failed to recover email target binding after unique conflict", existingErr)
				}
			}
			return model.EmailTarget{}, false, InternalError("failed to save email target binding", createErr)
		}
		return item, true, nil
	default:
		return model.EmailTarget{}, false, InternalError("failed to load email target binding", err)
	}
}

// issueEmailTargetVerification mints one new single-use verification token,
// persists it, sends the email, and finally records the successful send time.
func (s *PermissionService) issueEmailTargetVerification(ctx context.Context, item model.EmailTarget) (model.EmailTarget, error) {
	if s.targetVerificationMailer == nil {
		return model.EmailTarget{}, UnavailableError("email target verification mailer is not configured", nil)
	}

	token, tokenHash, err := generateEmailTargetVerificationToken()
	if err != nil {
		return model.EmailTarget{}, InternalError("failed to generate email target verification token", err)
	}

	now := time.Now().UTC()
	expiresAt := now.Add(emailTargetVerificationTokenLifetime)
	prepared, err := s.db.PrepareEmailTargetVerificationSend(ctx, storage.PrepareEmailTargetVerificationSendInput{
		ID:                    item.ID,
		OwnerUserID:           item.OwnerUserID,
		Email:                 item.Email,
		VerificationTokenHash: tokenHash,
		VerificationExpiresAt: &expiresAt,
		PreparedAt:            now,
		ShortWindowStart:      now.Add(-10 * time.Minute),
		DailyWindowStart:      now.Add(-24 * time.Hour),
		OwnerShortLimit:       emailTargetVerificationOwnerShortLimit,
		OwnerDailyLimit:       emailTargetVerificationOwnerDailyLimit,
		TargetShortLimit:      emailTargetVerificationTargetShortLimit,
		TargetDailyLimit:      emailTargetVerificationTargetDailyLimit,
	})
	if err != nil {
		if err == storage.ErrEmailTargetVerificationRateLimited {
			return model.EmailTarget{}, TooManyRequestsError("验证邮件发送过于频繁，请稍后再试")
		}
		return model.EmailTarget{}, InternalError("failed to prepare email target verification state", err)
	}

	verificationURL, err := s.buildEmailTargetVerificationURL(token)
	if err != nil {
		return model.EmailTarget{}, err
	}

	if err := s.targetVerificationMailer.SendVerification(ctx, EmailTargetVerificationMailInput{
		TargetEmail:      prepared.Email,
		VerificationURL:  verificationURL,
		ExpiresAt:        expiresAt,
		AppDisplayName:   firstNonEmpty(strings.TrimSpace(s.cfg.App.Name), "LinuxDoSpace"),
		ForwardFrom:      strings.TrimSpace(s.cfg.Mail.ForwardFrom),
		FrontendEmailURL: strings.TrimRight(strings.TrimSpace(s.cfg.App.FrontendURL), "/") + "/emails",
	}); err != nil {
		// Restore the previous verification material so a transient SMTP outage
		// does not invalidate an older still-working verification link or strand
		// the target in a permanently pending state.
		if _, rollbackErr := s.db.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
			ID:                     item.ID,
			CloudflareAddressID:    item.CloudflareAddressID,
			VerificationTokenHash:  item.VerificationTokenHash,
			VerificationExpiresAt:  item.VerificationExpiresAt,
			VerifiedAt:             item.VerifiedAt,
			LastVerificationSentAt: item.LastVerificationSentAt,
		}); rollbackErr != nil {
			logPostMutationFailure("email_target.verification_prepare_rollback", rollbackErr)
		}
		return model.EmailTarget{}, UnavailableError("目标邮箱已保存，但平台暂时无法发出验证邮件，请稍后点击“重新发送验证”重试", err)
	}
	return prepared, nil
}

// shouldIssueVerificationOnCreate decides whether pressing "添加目标邮箱"
// again should trigger a fresh verification email instead of silently returning
// the stale pending row.
func shouldIssueVerificationOnCreate(item model.EmailTarget, now time.Time) bool {
	if item.VerifiedAt != nil {
		return false
	}
	if remainingEmailTargetVerificationResendCooldown(item, now) > 0 {
		return false
	}
	if strings.TrimSpace(item.VerificationTokenHash) == "" {
		return true
	}
	if item.VerificationExpiresAt == nil {
		return true
	}
	return !item.VerificationExpiresAt.After(now.UTC())
}

// buildEmailTargetVerificationURL turns the random token into the public URL
// included in the verification email.
func (s *PermissionService) buildEmailTargetVerificationURL(token string) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(s.cfg.App.BaseURL), "/")
	if baseURL == "" {
		return "", UnavailableError("email target verification callback base url is not configured", nil)
	}
	return baseURL + "/v1/public/email-targets/verify?token=" + token, nil
}

// backfillOwnedEmailTargetsFromRoutes migrates legacy route-only target emails
// into the dedicated ownership table so existing users do not lose access when
// verification moved away from Cloudflare destination addresses.
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

	verifiedAt := time.Now().UTC()
	for _, route := range routes {
		targetEmail := strings.ToLower(strings.TrimSpace(route.TargetEmail))
		if targetEmail == "" {
			continue
		}
		if _, alreadyKnown := existingEmails[targetEmail]; alreadyKnown {
			continue
		}

		created, createErr := s.db.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
			OwnerUserID:            user.ID,
			Email:                  targetEmail,
			CloudflareAddressID:    "",
			VerificationTokenHash:  "",
			VerificationExpiresAt:  nil,
			VerifiedAt:             &verifiedAt,
			LastVerificationSentAt: nil,
		})
		if createErr != nil {
			if isEmailTargetUniqueConflict(createErr) {
				continue
			}
			return InternalError("failed to backfill legacy email target binding", createErr)
		}
		existingEmails[targetEmail] = struct{}{}

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
	}

	return nil
}

// userEmailTargetViewFromModel converts one persisted ownership row into the
// public JSON payload consumed by the email page.
func userEmailTargetViewFromModel(item model.EmailTarget) UserEmailTargetView {
	return UserEmailTargetView{
		ID:                     item.ID,
		Email:                  item.Email,
		CloudflareAddressID:    "",
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

// generateEmailTargetVerificationToken creates one opaque public token together
// with the SHA-256 hash persisted in the database.
func generateEmailTargetVerificationToken() (string, string, error) {
	token, err := security.RandomToken(32)
	if err != nil {
		return "", "", err
	}
	return token, hashEmailTargetVerificationToken(token), nil
}

// hashEmailTargetVerificationToken derives the stored lookup key so the
// database never keeps the raw public token in clear text.
func hashEmailTargetVerificationToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// isEmailTargetUniqueConflict recognizes the SQLite/PostgreSQL unique
// constraint message for the globally unique email_targets.email column so the
// service can turn a race condition into a deterministic ownership decision.
func isEmailTargetUniqueConflict(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(normalized, "unique constraint failed: email_targets.email") ||
		(strings.Contains(normalized, "duplicate key") && strings.Contains(normalized, "email_targets"))
}
