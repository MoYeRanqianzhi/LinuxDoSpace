package model

import "time"

// EmailCatchAllAccess stores the mutable runtime state that governs whether one
// approved catch-all mailbox can still receive mail.
type EmailCatchAllAccess struct {
	UserID                int64      `json:"user_id"`
	SubscriptionExpiresAt *time.Time `json:"subscription_expires_at,omitempty"`
	RemainingCount        int64      `json:"remaining_count"`
	DailyLimitOverride    *int64     `json:"daily_limit_override,omitempty"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

// EmailCatchAllDailyUsage stores one user's already-consumed catch-all traffic
// for one canonical UTC day.
type EmailCatchAllDailyUsage struct {
	UserID    int64     `json:"user_id"`
	UsageDate string    `json:"usage_date"`
	UsedCount int64     `json:"used_count"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// EmailCatchAllConsumeResult returns the post-consumption state after the
// relay reserves one or more catch-all deliveries.
type EmailCatchAllConsumeResult struct {
	Access              EmailCatchAllAccess     `json:"access"`
	DailyUsage          EmailCatchAllDailyUsage `json:"daily_usage"`
	EffectiveDailyLimit int64                   `json:"effective_daily_limit"`
	ConsumedMode        string                  `json:"consumed_mode"`
}
