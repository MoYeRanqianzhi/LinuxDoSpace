package postgres

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type UpsertPOWGlobalSettingsInput = storage.UpsertPOWGlobalSettingsInput
type UpsertPOWBenefitSettingsInput = storage.UpsertPOWBenefitSettingsInput
type UpsertPOWDifficultySettingsInput = storage.UpsertPOWDifficultySettingsInput
type UpsertPOWUserSettingsInput = storage.UpsertPOWUserSettingsInput

// GetPOWGlobalSettings loads the singleton proof-of-work global settings row.
func (s *Store) GetPOWGlobalSettings(ctx context.Context) (model.POWGlobalSettings, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    enabled,
    default_daily_completion_limit,
    base_reward_min,
    base_reward_max,
    created_at,
    updated_at
FROM pow_global_settings
WHERE id = 1
`)
	return scanPOWGlobalSettings(row)
}

// UpsertPOWGlobalSettings inserts or updates the singleton global PoW settings row.
func (s *Store) UpsertPOWGlobalSettings(ctx context.Context, input UpsertPOWGlobalSettingsInput) (model.POWGlobalSettings, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO pow_global_settings (
    id,
    enabled,
    default_daily_completion_limit,
    base_reward_min,
    base_reward_max,
    created_at,
    updated_at
) VALUES (1, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    enabled = excluded.enabled,
    default_daily_completion_limit = excluded.default_daily_completion_limit,
    base_reward_min = excluded.base_reward_min,
    base_reward_max = excluded.base_reward_max,
    updated_at = excluded.updated_at
RETURNING
    id,
    enabled,
    default_daily_completion_limit,
    base_reward_min,
    base_reward_max,
    created_at,
    updated_at
`,
		boolToInt(input.Enabled),
		input.DefaultDailyCompletionLimit,
		input.BaseRewardMin,
		input.BaseRewardMax,
		formatTime(now),
		formatTime(now),
	)
	return scanPOWGlobalSettings(row)
}

// ListPOWBenefitSettings returns all administrator-visible PoW benefit toggle rows.
func (s *Store) ListPOWBenefitSettings(ctx context.Context) ([]model.POWBenefitSettings, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    key,
    enabled,
    created_at,
    updated_at
FROM pow_benefit_settings
ORDER BY key ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.POWBenefitSettings, 0, 4)
	for rows.Next() {
		item, scanErr := scanPOWBenefitSettings(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// UpsertPOWBenefitSettings inserts or updates one PoW benefit toggle row.
func (s *Store) UpsertPOWBenefitSettings(ctx context.Context, input UpsertPOWBenefitSettingsInput) (model.POWBenefitSettings, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO pow_benefit_settings (
    key,
    enabled,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    enabled = excluded.enabled,
    updated_at = excluded.updated_at
RETURNING
    key,
    enabled,
    created_at,
    updated_at
`,
		strings.TrimSpace(input.Key),
		boolToInt(input.Enabled),
		formatTime(now),
		formatTime(now),
	)
	return scanPOWBenefitSettings(row)
}

// ListPOWDifficultySettings returns all administrator-visible PoW difficulty toggle rows.
func (s *Store) ListPOWDifficultySettings(ctx context.Context) ([]model.POWDifficultySettings, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    difficulty,
    enabled,
    created_at,
    updated_at
FROM pow_difficulty_settings
ORDER BY difficulty ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.POWDifficultySettings, 0, 8)
	for rows.Next() {
		item, scanErr := scanPOWDifficultySettings(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// UpsertPOWDifficultySettings inserts or updates one PoW difficulty toggle row.
func (s *Store) UpsertPOWDifficultySettings(ctx context.Context, input UpsertPOWDifficultySettingsInput) (model.POWDifficultySettings, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO pow_difficulty_settings (
    difficulty,
    enabled,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(difficulty) DO UPDATE SET
    enabled = excluded.enabled,
    updated_at = excluded.updated_at
RETURNING
    difficulty,
    enabled,
    created_at,
    updated_at
`,
		input.Difficulty,
		boolToInt(input.Enabled),
		formatTime(now),
		formatTime(now),
	)
	return scanPOWDifficultySettings(row)
}

// GetPOWUserSettings loads one user's optional PoW per-day completion override.
func (s *Store) GetPOWUserSettings(ctx context.Context, userID int64) (model.POWUserSettings, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    user_id,
    daily_completion_limit_override,
    created_at,
    updated_at
FROM pow_user_settings
WHERE user_id = ?
`, userID)
	return scanPOWUserSettings(row)
}

// UpsertPOWUserSettings inserts or updates one user's daily PoW completion override.
func (s *Store) UpsertPOWUserSettings(ctx context.Context, input UpsertPOWUserSettingsInput) (model.POWUserSettings, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO pow_user_settings (
    user_id,
    daily_completion_limit_override,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    daily_completion_limit_override = excluded.daily_completion_limit_override,
    updated_at = excluded.updated_at
RETURNING
    user_id,
    daily_completion_limit_override,
    created_at,
    updated_at
`,
		input.UserID,
		nullableInt(input.DailyCompletionLimitOverride),
		formatTime(now),
		formatTime(now),
	)
	return scanPOWUserSettings(row)
}

func scanPOWGlobalSettings(scanner interface{ Scan(dest ...any) error }) (model.POWGlobalSettings, error) {
	var item model.POWGlobalSettings
	var enabled int
	var createdAt string
	var updatedAt string
	err := scanner.Scan(
		&item.ID,
		&enabled,
		&item.DefaultDailyCompletionLimit,
		&item.BaseRewardMin,
		&item.BaseRewardMax,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.POWGlobalSettings{}, err
	}
	item.Enabled = enabled == 1
	var parseErr error
	if item.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return model.POWGlobalSettings{}, parseErr
	}
	if item.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return model.POWGlobalSettings{}, parseErr
	}
	return item, nil
}

func scanPOWBenefitSettings(scanner interface{ Scan(dest ...any) error }) (model.POWBenefitSettings, error) {
	var item model.POWBenefitSettings
	var enabled int
	var createdAt string
	var updatedAt string
	err := scanner.Scan(&item.Key, &enabled, &createdAt, &updatedAt)
	if err != nil {
		return model.POWBenefitSettings{}, err
	}
	item.Enabled = enabled == 1
	var parseErr error
	if item.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return model.POWBenefitSettings{}, parseErr
	}
	if item.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return model.POWBenefitSettings{}, parseErr
	}
	return item, nil
}

func scanPOWDifficultySettings(scanner interface{ Scan(dest ...any) error }) (model.POWDifficultySettings, error) {
	var item model.POWDifficultySettings
	var enabled int
	var createdAt string
	var updatedAt string
	err := scanner.Scan(&item.Difficulty, &enabled, &createdAt, &updatedAt)
	if err != nil {
		return model.POWDifficultySettings{}, err
	}
	item.Enabled = enabled == 1
	var parseErr error
	if item.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return model.POWDifficultySettings{}, parseErr
	}
	if item.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return model.POWDifficultySettings{}, parseErr
	}
	return item, nil
}

func scanPOWUserSettings(scanner interface{ Scan(dest ...any) error }) (model.POWUserSettings, error) {
	var item model.POWUserSettings
	var override sql.NullInt64
	var createdAt string
	var updatedAt string
	err := scanner.Scan(&item.UserID, &override, &createdAt, &updatedAt)
	if err != nil {
		return model.POWUserSettings{}, err
	}
	if override.Valid {
		value := int(override.Int64)
		item.DailyCompletionLimitOverride = &value
	}
	var parseErr error
	if item.CreatedAt, parseErr = parseTime(createdAt); parseErr != nil {
		return model.POWUserSettings{}, parseErr
	}
	if item.UpdatedAt, parseErr = parseTime(updatedAt); parseErr != nil {
		return model.POWUserSettings{}, parseErr
	}
	return item, nil
}

func nullableInt(value *int) any {
	if value == nil {
		return nil
	}
	return *value
}
