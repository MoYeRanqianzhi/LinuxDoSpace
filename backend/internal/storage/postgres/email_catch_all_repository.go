package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type UpsertEmailCatchAllAccessInput = storage.UpsertEmailCatchAllAccessInput
type ConsumeEmailCatchAllInput = storage.ConsumeEmailCatchAllInput

// GetEmailCatchAllAccessByUser loads the mutable catch-all delivery state for
// one user.
func (s *Store) GetEmailCatchAllAccessByUser(ctx context.Context, userID int64) (model.EmailCatchAllAccess, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    user_id,
    subscription_expires_at,
    remaining_count,
    daily_limit_override,
    created_at,
    updated_at
FROM email_catch_all_access
WHERE user_id = ?
`, userID)
	return scanEmailCatchAllAccess(row)
}

// UpsertEmailCatchAllAccess inserts or updates one user's mutable catch-all
// delivery state.
func (s *Store) UpsertEmailCatchAllAccess(ctx context.Context, input UpsertEmailCatchAllAccessInput) (model.EmailCatchAllAccess, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO email_catch_all_access (
    user_id,
    subscription_expires_at,
    remaining_count,
    daily_limit_override,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    subscription_expires_at = excluded.subscription_expires_at,
    remaining_count = excluded.remaining_count,
    daily_limit_override = excluded.daily_limit_override,
    updated_at = excluded.updated_at
RETURNING
    user_id,
    subscription_expires_at,
    remaining_count,
    daily_limit_override,
    created_at,
    updated_at
`,
		input.UserID,
		formatNullableTime(input.SubscriptionExpiresAt),
		input.RemainingCount,
		input.DailyLimitOverride,
		formatTime(now),
		formatTime(now),
	)
	return scanEmailCatchAllAccess(row)
}

// GetEmailCatchAllDailyUsage loads the UTC-day catch-all consumption row for
// one user.
func (s *Store) GetEmailCatchAllDailyUsage(ctx context.Context, userID int64, usageDate string) (model.EmailCatchAllDailyUsage, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
FROM email_catch_all_daily_usage
WHERE user_id = ? AND usage_date = ?
`, userID, usageDate)
	return scanEmailCatchAllDailyUsage(row)
}

// ConsumeEmailCatchAll atomically reserves catch-all delivery usage. Active
// subscriptions win first; otherwise the remaining prepaid count is decremented.
func (s *Store) ConsumeEmailCatchAll(ctx context.Context, input ConsumeEmailCatchAllInput) (model.EmailCatchAllConsumeResult, error) {
	if input.Count <= 0 {
		return model.EmailCatchAllConsumeResult{}, fmt.Errorf("catch-all consume count must be positive")
	}

	now := input.Now.UTC()
	usageDate := now.Format("2006-01-02")
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.EmailCatchAllConsumeResult{}, err
	}
	defer tx.Rollback()

	access, accessExists, err := getEmailCatchAllAccessTx(ctx, tx, input.UserID)
	if err != nil {
		return model.EmailCatchAllConsumeResult{}, err
	}
	if !accessExists {
		access = model.EmailCatchAllAccess{
			UserID:         input.UserID,
			RemainingCount: 0,
		}
	}

	usage, usageExists, err := getEmailCatchAllDailyUsageTx(ctx, tx, input.UserID, usageDate)
	if err != nil {
		return model.EmailCatchAllConsumeResult{}, err
	}
	if !usageExists {
		usage = model.EmailCatchAllDailyUsage{
			UserID:    input.UserID,
			UsageDate: usageDate,
			UsedCount: 0,
		}
	}

	effectiveDailyLimit := input.DefaultDailyLimit
	if access.DailyLimitOverride != nil {
		effectiveDailyLimit = *access.DailyLimitOverride
	}
	if effectiveDailyLimit <= 0 {
		effectiveDailyLimit = 1_000_000
	}
	if usage.UsedCount+input.Count > effectiveDailyLimit {
		return model.EmailCatchAllConsumeResult{}, storage.ErrEmailCatchAllDailyLimitExceeded
	}

	consumedMode := "subscription"
	subscriptionActive := access.SubscriptionExpiresAt != nil && access.SubscriptionExpiresAt.After(now)
	if !subscriptionActive {
		if access.RemainingCount < input.Count {
			return model.EmailCatchAllConsumeResult{}, storage.ErrEmailCatchAllInsufficientRemainingCount
		}
		access.RemainingCount -= input.Count
		consumedMode = "quantity"
		if _, err := tx.ExecContext(ctx, `
UPDATE email_catch_all_access
SET remaining_count = ?, updated_at = ?
WHERE user_id = ?
`, access.RemainingCount, formatTime(now), input.UserID); err != nil {
			return model.EmailCatchAllConsumeResult{}, err
		}
		access.UpdatedAt = now
	}

	if usageExists {
		row := tx.QueryRowContext(ctx, `
UPDATE email_catch_all_daily_usage
SET used_count = used_count + ?, updated_at = ?
WHERE user_id = ? AND usage_date = ?
RETURNING
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
`, input.Count, formatTime(now), input.UserID, usageDate)
		usage, err = scanEmailCatchAllDailyUsage(row)
		if err != nil {
			return model.EmailCatchAllConsumeResult{}, err
		}
	} else {
		row := tx.QueryRowContext(ctx, `
INSERT INTO email_catch_all_daily_usage (
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?)
RETURNING
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
`, input.UserID, usageDate, input.Count, formatTime(now), formatTime(now))
		usage, err = scanEmailCatchAllDailyUsage(row)
		if err != nil {
			return model.EmailCatchAllConsumeResult{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return model.EmailCatchAllConsumeResult{}, err
	}

	return model.EmailCatchAllConsumeResult{
		Access:              access,
		DailyUsage:          usage,
		EffectiveDailyLimit: effectiveDailyLimit,
		ConsumedMode:        consumedMode,
	}, nil
}

// getEmailCatchAllAccessTx loads and locks one access row inside an open
// PostgreSQL transaction.
func getEmailCatchAllAccessTx(ctx context.Context, tx *queryTx, userID int64) (model.EmailCatchAllAccess, bool, error) {
	row := tx.QueryRowContext(ctx, `
SELECT
    user_id,
    subscription_expires_at,
    remaining_count,
    daily_limit_override,
    created_at,
    updated_at
FROM email_catch_all_access
WHERE user_id = ?
FOR UPDATE
`, userID)

	item, err := scanEmailCatchAllAccess(row)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.EmailCatchAllAccess{}, false, nil
		}
		return model.EmailCatchAllAccess{}, false, err
	}
	return item, true, nil
}

// getEmailCatchAllDailyUsageTx loads and locks one per-day usage row inside an
// open PostgreSQL transaction.
func getEmailCatchAllDailyUsageTx(ctx context.Context, tx *queryTx, userID int64, usageDate string) (model.EmailCatchAllDailyUsage, bool, error) {
	row := tx.QueryRowContext(ctx, `
SELECT
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
FROM email_catch_all_daily_usage
WHERE user_id = ? AND usage_date = ?
FOR UPDATE
`, userID, usageDate)

	item, err := scanEmailCatchAllDailyUsage(row)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.EmailCatchAllDailyUsage{}, false, nil
		}
		return model.EmailCatchAllDailyUsage{}, false, err
	}
	return item, true, nil
}

// scanEmailCatchAllAccess maps one access row into the model package.
func scanEmailCatchAllAccess(scanner interface{ Scan(dest ...any) error }) (model.EmailCatchAllAccess, error) {
	var item model.EmailCatchAllAccess
	var subscriptionExpiresAt sql.NullString
	var dailyLimitOverride sql.NullInt64
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&item.UserID,
		&subscriptionExpiresAt,
		&item.RemainingCount,
		&dailyLimitOverride,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.EmailCatchAllAccess{}, err
	}

	if dailyLimitOverride.Valid {
		value := dailyLimitOverride.Int64
		item.DailyLimitOverride = &value
	}

	item.SubscriptionExpiresAt, err = parseNullableTime(subscriptionExpiresAt)
	if err != nil {
		return model.EmailCatchAllAccess{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.EmailCatchAllAccess{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.EmailCatchAllAccess{}, err
	}
	return item, nil
}

// scanEmailCatchAllDailyUsage maps one per-day usage row into the model
// package.
func scanEmailCatchAllDailyUsage(scanner interface{ Scan(dest ...any) error }) (model.EmailCatchAllDailyUsage, error) {
	var item model.EmailCatchAllDailyUsage
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&item.UserID,
		&item.UsageDate,
		&item.UsedCount,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.EmailCatchAllDailyUsage{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.EmailCatchAllDailyUsage{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.EmailCatchAllDailyUsage{}, err
	}
	return item, nil
}
