package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/timeutil"
)

type CreateOrReplacePOWChallengeInput = storage.CreateOrReplacePOWChallengeInput
type ClaimPOWChallengeRewardInput = storage.ClaimPOWChallengeRewardInput

const (
	powQuantitySource        = "pow_reward"
	powQuantityReferenceType = "pow_challenge"
)

// GetActivePOWChallengeByUser loads the newest still-active challenge for one
// authenticated user.
func (s *Store) GetActivePOWChallengeByUser(ctx context.Context, userID int64) (model.POWChallenge, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    user_id,
    benefit_key,
    resource_key,
    scope,
    difficulty,
    base_reward,
    reward_quantity,
    reward_unit,
    challenge_token,
    salt_hex,
    argon2_variant,
    argon2_memory_kib,
    argon2_iterations,
    argon2_parallelism,
    argon2_hash_length,
    status,
    solution_nonce,
    solution_hash_hex,
    claimed_at,
    superseded_at,
    created_at,
    updated_at
FROM pow_challenges
WHERE user_id = ? AND status = ?
ORDER BY created_at DESC, id DESC
LIMIT 1
`, userID, model.POWChallengeStatusActive)
	return scanPOWChallenge(row)
}

// CreateOrReplacePOWChallenge supersedes any older active challenge for the
// user and inserts the newly generated challenge row.
func (s *Store) CreateOrReplacePOWChallenge(ctx context.Context, input CreateOrReplacePOWChallengeInput) (model.POWChallenge, error) {
	now := input.CreatedAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.POWChallenge{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
UPDATE pow_challenges
SET
    status = ?,
    superseded_at = ?,
    updated_at = ?
WHERE user_id = ? AND status = ?
`,
		model.POWChallengeStatusSuperseded,
		formatTime(now),
		formatTime(now),
		input.UserID,
		model.POWChallengeStatusActive,
	); err != nil {
		return model.POWChallenge{}, err
	}

	row := tx.QueryRowContext(ctx, `
INSERT INTO pow_challenges (
    user_id,
    benefit_key,
    resource_key,
    scope,
    difficulty,
    base_reward,
    reward_quantity,
    reward_unit,
    challenge_token,
    salt_hex,
    argon2_variant,
    argon2_memory_kib,
    argon2_iterations,
    argon2_parallelism,
    argon2_hash_length,
    status,
    solution_nonce,
    solution_hash_hex,
    claimed_at,
    superseded_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', '', NULL, NULL, ?, ?)
RETURNING
    id,
    user_id,
    benefit_key,
    resource_key,
    scope,
    difficulty,
    base_reward,
    reward_quantity,
    reward_unit,
    challenge_token,
    salt_hex,
    argon2_variant,
    argon2_memory_kib,
    argon2_iterations,
    argon2_parallelism,
    argon2_hash_length,
    status,
    solution_nonce,
    solution_hash_hex,
    claimed_at,
    superseded_at,
    created_at,
    updated_at
`,
		input.UserID,
		strings.TrimSpace(input.BenefitKey),
		strings.TrimSpace(input.ResourceKey),
		strings.TrimSpace(input.Scope),
		input.Difficulty,
		input.BaseReward,
		input.RewardQuantity,
		strings.TrimSpace(input.RewardUnit),
		strings.TrimSpace(input.ChallengeToken),
		strings.TrimSpace(input.SaltHex),
		strings.TrimSpace(input.Argon2Variant),
		int64(input.Argon2MemoryKiB),
		int64(input.Argon2Iterations),
		int64(input.Argon2Parallelism),
		int64(input.Argon2HashLength),
		model.POWChallengeStatusActive,
		formatTime(now),
		formatTime(now),
	)

	item, err := scanPOWChallenge(row)
	if err != nil {
		return model.POWChallenge{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.POWChallenge{}, err
	}
	return item, nil
}

// CountClaimedPOWChallengesByUser counts how many PoW rewards one user already
// claimed inside the requested UTC window.
func (s *Store) CountClaimedPOWChallengesByUser(ctx context.Context, userID int64, start time.Time, end time.Time) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COUNT(*)
FROM pow_challenges
WHERE user_id = ?
  AND status = ?
  AND claimed_at IS NOT NULL
  AND claimed_at >= ?
  AND claimed_at < ?
`, userID, model.POWChallengeStatusClaimed, formatTime(start.UTC()), formatTime(end.UTC()))

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// ClaimPOWChallengeReward marks one active challenge as claimed, enforces the
// per-day claim cap, grants the configured benefit, and records one immutable
// quantity-ledger entry inside one transaction.
func (s *Store) ClaimPOWChallengeReward(ctx context.Context, input ClaimPOWChallengeRewardInput) (model.POWChallenge, model.EmailCatchAllAccess, error) {
	if input.MaxDailyCompletions <= 0 {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, fmt.Errorf("max daily pow completions must be positive")
	}
	if input.BaseReward < 1 || input.RewardQuantity < 1 {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, fmt.Errorf("pow reward must be positive")
	}

	now := input.ClaimedAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	defer tx.Rollback()

	challenge, err := getPOWChallengeByIDForUserTx(ctx, tx, input.ChallengeID, input.UserID)
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	if challenge.Status != model.POWChallengeStatusActive {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, storage.ErrPOWChallengeNotActive
	}

	if err := reservePOWDailyClaimTx(ctx, tx, input.UserID, timeutil.ShanghaiDayKey(now), input.MaxDailyCompletions, now); err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}

	updateResult, err := tx.ExecContext(ctx, `
UPDATE pow_challenges
SET
    status = ?,
    base_reward = ?,
    reward_quantity = ?,
    solution_nonce = ?,
    solution_hash_hex = ?,
    claimed_at = ?,
    updated_at = ?
WHERE id = ? AND user_id = ? AND status = ?
`,
		model.POWChallengeStatusClaimed,
		input.BaseReward,
		input.RewardQuantity,
		strings.TrimSpace(input.SolutionNonce),
		strings.TrimSpace(input.SolutionHashHex),
		formatTime(now),
		formatTime(now),
		input.ChallengeID,
		input.UserID,
		model.POWChallengeStatusActive,
	)
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	affectedRows, err := updateResult.RowsAffected()
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	if affectedRows == 0 {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, storage.ErrPOWChallengeNotActive
	}
	finalChallenge, err := getPOWChallengeByIDForUserTx(ctx, tx, input.ChallengeID, input.UserID)
	if err != nil {
		if err == sql.ErrNoRows {
			return model.POWChallenge{}, model.EmailCatchAllAccess{}, storage.ErrPOWChallengeNotActive
		}
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}

	access, found, err := getEmailCatchAllAccessTx(ctx, tx, input.UserID)
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	if !found {
		access = model.EmailCatchAllAccess{UserID: input.UserID}
	}
	access = access.NormalizeTemporaryReward(now)

	switch challenge.ResourceKey {
	case model.POWBenefitEmailCatchAllRemainingCount:
		nextTemporaryRewardCount := access.ActiveTemporaryRewardCount(now) + int64(input.RewardQuantity)
		rewardExpiresAt := input.RewardExpiresAt.UTC()
		if err := upsertEmailCatchAllAccessTx(ctx, tx, input.UserID, access.SubscriptionExpiresAt, access.RemainingCount, nextTemporaryRewardCount, &rewardExpiresAt, access.DailyLimitOverride, now); err != nil {
			return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
		}
		access, _, err = getEmailCatchAllAccessTx(ctx, tx, input.UserID)
		if err != nil {
			return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
		}
	default:
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, fmt.Errorf("unsupported pow challenge resource %q", challenge.ResourceKey)
	}

	_, err = tx.ExecContext(ctx, `
INSERT INTO quantity_records (
    user_id,
    resource_key,
    scope,
    delta,
    source,
    reason,
    reference_type,
    reference_id,
    expires_at,
    created_by_user_id,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, ?)
	`,
		input.UserID,
		challenge.ResourceKey,
		challenge.Scope,
		input.RewardQuantity,
		powQuantitySource,
		strings.TrimSpace(input.QuantityRecordReason),
		powQuantityReferenceType,
		strconv.FormatInt(challenge.ID, 10),
		formatTime(input.RewardExpiresAt.UTC()),
		formatTime(now),
	)
	if err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.POWChallenge{}, model.EmailCatchAllAccess{}, err
	}
	return finalChallenge, access, nil
}

// getPOWChallengeByIDForUserTx reloads one user-owned challenge row inside the
// active transaction so claim logic can stay atomic.
func getPOWChallengeByIDForUserTx(ctx context.Context, tx *sql.Tx, challengeID int64, userID int64) (model.POWChallenge, error) {
	row := tx.QueryRowContext(ctx, `
SELECT
    id,
    user_id,
    benefit_key,
    resource_key,
    scope,
    difficulty,
    base_reward,
    reward_quantity,
    reward_unit,
    challenge_token,
    salt_hex,
    argon2_variant,
    argon2_memory_kib,
    argon2_iterations,
    argon2_parallelism,
    argon2_hash_length,
    status,
    solution_nonce,
    solution_hash_hex,
    claimed_at,
    superseded_at,
    created_at,
    updated_at
FROM pow_challenges
WHERE id = ? AND user_id = ?
`, challengeID, userID)
	return scanPOWChallenge(row)
}

// reservePOWDailyClaimTx atomically consumes one claim slot in the Shanghai
// day-scoped usage table. This closes the race where two concurrent challenge
// submissions both saw the same historical count and both succeeded.
func reservePOWDailyClaimTx(ctx context.Context, tx *sql.Tx, userID int64, usageDate string, maxDailyCompletions int, now time.Time) error {
	if maxDailyCompletions <= 0 {
		return storage.ErrPOWChallengeDailyLimitExceeded
	}

	result, err := tx.ExecContext(ctx, `
INSERT INTO pow_challenge_daily_usage (
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
) VALUES (?, ?, 1, ?, ?)
ON CONFLICT(user_id, usage_date) DO UPDATE SET
    used_count = pow_challenge_daily_usage.used_count + 1,
    updated_at = excluded.updated_at
WHERE pow_challenge_daily_usage.used_count + 1 <= ?
`,
		userID,
		usageDate,
		formatTime(now),
		formatTime(now),
		maxDailyCompletions,
	)
	if err != nil {
		return err
	}
	affectedRows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affectedRows == 0 {
		return storage.ErrPOWChallengeDailyLimitExceeded
	}
	return nil
}

// scanPOWChallenge maps one persisted challenge row into the shared model.
func scanPOWChallenge(scanner interface{ Scan(dest ...any) error }) (model.POWChallenge, error) {
	var item model.POWChallenge
	var claimedAt sql.NullString
	var supersededAt sql.NullString
	var createdAt string
	var updatedAt string
	var argon2MemoryKiB int64
	var argon2Iterations int64
	var argon2Parallelism int64
	var argon2HashLength int64

	err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.BenefitKey,
		&item.ResourceKey,
		&item.Scope,
		&item.Difficulty,
		&item.BaseReward,
		&item.RewardQuantity,
		&item.RewardUnit,
		&item.ChallengeToken,
		&item.SaltHex,
		&item.Argon2Variant,
		&argon2MemoryKiB,
		&argon2Iterations,
		&argon2Parallelism,
		&argon2HashLength,
		&item.Status,
		&item.SolutionNonce,
		&item.SolutionHashHex,
		&claimedAt,
		&supersededAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.POWChallenge{}, err
	}

	item.Argon2MemoryKiB = uint32(argon2MemoryKiB)
	item.Argon2Iterations = uint32(argon2Iterations)
	item.Argon2Parallelism = uint8(argon2Parallelism)
	item.Argon2HashLength = uint32(argon2HashLength)

	item.ClaimedAt, err = parseNullableTime(claimedAt)
	if err != nil {
		return model.POWChallenge{}, err
	}
	item.SupersededAt, err = parseNullableTime(supersededAt)
	if err != nil {
		return model.POWChallenge{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.POWChallenge{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.POWChallenge{}, err
	}
	return item, nil
}
