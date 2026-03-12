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

// loadEmailCatchAllAccessView resolves the effective catch-all runtime
// allowance for one user using only server-side UTC time.
func (s *PermissionService) loadEmailCatchAllAccessView(ctx context.Context, userID int64, policy model.PermissionPolicy, permissionApproved bool) (*EmailCatchAllAccessView, error) {
	now := time.Now().UTC()
	usageDate := now.Format("2006-01-02")

	access, err := s.db.GetEmailCatchAllAccessByUser(ctx, userID)
	if err != nil {
		if !storage.IsNotFound(err) {
			return nil, InternalError("failed to load catch-all access state", err)
		}
		access = model.EmailCatchAllAccess{UserID: userID}
	}

	usage, err := s.db.GetEmailCatchAllDailyUsage(ctx, userID, usageDate)
	if err != nil {
		if !storage.IsNotFound(err) {
			return nil, InternalError("failed to load catch-all daily usage", err)
		}
		usage = model.EmailCatchAllDailyUsage{
			UserID:    userID,
			UsageDate: usageDate,
			UsedCount: 0,
		}
	}

	effectiveDailyLimit := policy.DefaultDailyLimit
	if effectiveDailyLimit <= 0 {
		effectiveDailyLimit = 1_000_000
	}
	if access.DailyLimitOverride != nil && *access.DailyLimitOverride > 0 {
		effectiveDailyLimit = *access.DailyLimitOverride
	}

	subscriptionActive := access.SubscriptionExpiresAt != nil && access.SubscriptionExpiresAt.After(now)
	accessMode := "none"
	hasAccess := false
	if subscriptionActive {
		accessMode = "subscription"
		hasAccess = true
	} else if access.RemainingCount > 0 {
		accessMode = "quantity"
		hasAccess = true
	}

	dailyRemainingCount := effectiveDailyLimit - usage.UsedCount
	if dailyRemainingCount < 0 {
		dailyRemainingCount = 0
	}

	return &EmailCatchAllAccessView{
		AccessMode:            accessMode,
		SubscriptionActive:    subscriptionActive,
		SubscriptionExpiresAt: access.SubscriptionExpiresAt,
		RemainingCount:        access.RemainingCount,
		DailyUsageDate:        usageDate,
		DailyUsedCount:        usage.UsedCount,
		DailyRemainingCount:   dailyRemainingCount,
		EffectiveDailyLimit:   effectiveDailyLimit,
		HasAccess:             hasAccess,
		DeliveryAvailable:     permissionApproved && hasAccess && dailyRemainingCount > 0,
	}, nil
}

// UpdateEmailCatchAllAccessForUser lets an administrator grant subscription
// days, adjust prepaid remaining count, and override the effective daily cap.
func (s *PermissionService) UpdateEmailCatchAllAccessForUser(ctx context.Context, actor model.User, userID int64, request AdminUpdateEmailCatchAllAccessRequest) (UserPermissionView, error) {
	targetUser, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return UserPermissionView{}, NotFoundError("target user not found")
		}
		return UserPermissionView{}, InternalError("failed to load target user", err)
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		return UserPermissionView{}, ValidationError("reason is required")
	}
	if request.AddSubscriptionDays < 0 {
		return UserPermissionView{}, ValidationError("add_subscription_days must be 0 or greater")
	}
	if request.DailyLimitOverride != nil && *request.DailyLimitOverride <= 0 {
		return UserPermissionView{}, ValidationError("daily_limit_override must be greater than 0")
	}
	if request.AddSubscriptionDays == 0 &&
		!request.ClearSubscription &&
		request.RemainingCountDelta == 0 &&
		request.DailyLimitOverride == nil &&
		!request.ClearDailyLimitOverride {
		return UserPermissionView{}, ValidationError("at least one access-state change is required")
	}

	currentAccess, err := s.db.GetEmailCatchAllAccessByUser(ctx, userID)
	if err != nil {
		if !storage.IsNotFound(err) {
			return UserPermissionView{}, InternalError("failed to load current catch-all access state", err)
		}
		currentAccess = model.EmailCatchAllAccess{UserID: userID}
	}

	now := time.Now().UTC()
	nextSubscriptionExpiresAt := currentAccess.SubscriptionExpiresAt
	if request.ClearSubscription {
		nextSubscriptionExpiresAt = nil
	}
	if request.AddSubscriptionDays > 0 {
		base := now
		if nextSubscriptionExpiresAt != nil && nextSubscriptionExpiresAt.After(now) {
			base = nextSubscriptionExpiresAt.UTC()
		}
		expiresAt := base.AddDate(0, 0, request.AddSubscriptionDays)
		nextSubscriptionExpiresAt = &expiresAt
	}

	nextRemainingCount := currentAccess.RemainingCount + request.RemainingCountDelta
	if nextRemainingCount < 0 {
		return UserPermissionView{}, ValidationError("remaining_count_delta would make remaining_count negative")
	}

	nextDailyLimitOverride := currentAccess.DailyLimitOverride
	if request.ClearDailyLimitOverride {
		nextDailyLimitOverride = nil
	}
	if request.DailyLimitOverride != nil {
		overrideValue := *request.DailyLimitOverride
		nextDailyLimitOverride = &overrideValue
	}

	if _, err := s.db.UpsertEmailCatchAllAccess(ctx, storage.UpsertEmailCatchAllAccessInput{
		UserID:                userID,
		SubscriptionExpiresAt: nextSubscriptionExpiresAt,
		RemainingCount:        nextRemainingCount,
		DailyLimitOverride:    nextDailyLimitOverride,
	}); err != nil {
		return UserPermissionView{}, InternalError("failed to update catch-all access state", err)
	}

	if request.AddSubscriptionDays > 0 {
		_, quantityRecordErr := s.db.CreateQuantityRecord(ctx, storage.CreateQuantityRecordInput{
			UserID:          userID,
			ResourceKey:     QuantityResourceEmailCatchAllSubscriptionDays,
			Scope:           PermissionKeyEmailCatchAll,
			Delta:           request.AddSubscriptionDays,
			Source:          QuantitySourceAdminManual,
			Reason:          reason,
			ReferenceType:   "permission",
			ReferenceID:     PermissionKeyEmailCatchAll,
			CreatedByUserID: &actor.ID,
		})
		logPostMutationFailure("admin.email_catch_all_access.quantity_record.subscription_days", quantityRecordErr)
	}
	if request.RemainingCountDelta != 0 {
		_, quantityRecordErr := s.db.CreateQuantityRecord(ctx, storage.CreateQuantityRecordInput{
			UserID:          userID,
			ResourceKey:     QuantityResourceEmailCatchAllRemainingCount,
			Scope:           PermissionKeyEmailCatchAll,
			Delta:           int(request.RemainingCountDelta),
			Source:          QuantitySourceAdminManual,
			Reason:          reason,
			ReferenceType:   "permission",
			ReferenceID:     PermissionKeyEmailCatchAll,
			CreatedByUserID: &actor.ID,
		})
		logPostMutationFailure("admin.email_catch_all_access.quantity_record.remaining_count", quantityRecordErr)
	}

	metadata, _ := json.Marshal(map[string]any{
		"target_user_id":             userID,
		"add_subscription_days":      request.AddSubscriptionDays,
		"clear_subscription":         request.ClearSubscription,
		"remaining_count_delta":      request.RemainingCountDelta,
		"daily_limit_override":       nextDailyLimitOverride,
		"clear_daily_limit_override": request.ClearDailyLimitOverride,
	})
	logAuditWriteFailure("admin.email_catch_all_access.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_catch_all_access.update",
		ResourceType: "email_catch_all_access",
		ResourceID:   strconv.FormatInt(userID, 10),
		MetadataJSON: string(metadata),
	}))

	return s.loadEmailCatchAllPermission(ctx, targetUser)
}
