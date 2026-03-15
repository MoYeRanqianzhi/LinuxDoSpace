package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type EnqueueMailDeliveryBatchInput = storage.EnqueueMailDeliveryBatchInput
type ClaimMailDeliveryJobsInput = storage.ClaimMailDeliveryJobsInput
type MarkMailDeliveryJobDeliveredInput = storage.MarkMailDeliveryJobDeliveredInput
type MarkMailDeliveryJobRetryInput = storage.MarkMailDeliveryJobRetryInput
type MarkMailDeliveryJobFailedInput = storage.MarkMailDeliveryJobFailedInput
type CleanupMailDeliveryJobsInput = storage.CleanupMailDeliveryJobsInput

const mailQueueCatchAllPolicyKey = "email_catch_all"

// EnqueueMailDeliveryBatch atomically reserves any required catch-all quota and
// persists every outbound delivery job derived from one accepted SMTP message.
func (s *Store) EnqueueMailDeliveryBatch(ctx context.Context, input EnqueueMailDeliveryBatchInput) ([]model.MailDeliveryJob, error) {
	if len(input.RawMessage) == 0 {
		return nil, fmt.Errorf("mail delivery batch raw message is required")
	}
	if len(input.Groups) == 0 {
		return nil, fmt.Errorf("mail delivery batch requires at least one group")
	}
	if input.MaxAttempts < 1 {
		return nil, fmt.Errorf("mail delivery batch max attempts must be at least 1")
	}

	now := input.QueuedAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	messageRow := tx.QueryRowContext(ctx, `
INSERT INTO mail_messages (
    original_envelope_from,
    raw_message,
    message_size_bytes,
    created_at
) VALUES (?, ?, ?, ?)
RETURNING id
`,
		strings.TrimSpace(input.OriginalEnvelopeFrom),
		input.RawMessage,
		len(input.RawMessage),
		formatTime(now),
	)

	var messageID int64
	if err := messageRow.Scan(&messageID); err != nil {
		return nil, err
	}

	defaultDailyLimit, err := loadMailQueueDefaultDailyLimitTx(ctx, tx)
	if err != nil {
		return nil, err
	}

	jobIDs := make([]int64, 0, len(input.Groups))
	for _, group := range input.Groups {
		originalRecipients := normalizeMailAddressSlice(group.OriginalRecipients)
		targetRecipients := normalizeMailAddressSlice(group.TargetRecipients)
		ownerUserIDs := uniqueInt64Values(group.CatchAllOwnerUserIDs)

		if len(originalRecipients) == 0 {
			return nil, fmt.Errorf("mail delivery group original recipients are required")
		}
		if len(targetRecipients) == 0 {
			return nil, fmt.Errorf("mail delivery group target recipients are required")
		}

		reservations := make([]model.MailDeliveryReservation, 0, len(ownerUserIDs))
		for _, ownerUserID := range ownerUserIDs {
			reservation, reservationErr := consumeMailQueueCatchAllReservationTx(ctx, tx, ownerUserID, 1, defaultDailyLimit, now)
			if reservationErr != nil {
				return nil, reservationErr
			}
			reservations = append(reservations, reservation)
		}

		originalRecipientsJSON, err := marshalStringSliceJSON(originalRecipients)
		if err != nil {
			return nil, err
		}
		targetRecipientsJSON, err := marshalStringSliceJSON(targetRecipients)
		if err != nil {
			return nil, err
		}
		reservationsJSON, err := marshalMailDeliveryReservationsJSON(reservations)
		if err != nil {
			return nil, err
		}

		jobRow := tx.QueryRowContext(ctx, `
INSERT INTO mail_delivery_jobs (
    message_id,
    original_recipients_json,
    target_recipients_json,
    reservations_json,
    status,
    attempt_count,
    max_attempts,
    next_attempt_at,
    processing_started_at,
    last_attempt_at,
    last_error,
    delivered_at,
    failed_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, 0, ?, ?, NULL, NULL, '', NULL, NULL, ?, ?)
RETURNING id
`,
			messageID,
			originalRecipientsJSON,
			targetRecipientsJSON,
			reservationsJSON,
			model.MailDeliveryJobStatusQueued,
			input.MaxAttempts,
			formatTime(now),
			formatTime(now),
			formatTime(now),
		)

		var jobID int64
		if err := jobRow.Scan(&jobID); err != nil {
			return nil, err
		}
		jobIDs = append(jobIDs, jobID)
	}

	jobs := make([]model.MailDeliveryJob, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		job, getErr := getMailDeliveryJobTx(ctx, tx, jobID, false)
		if getErr != nil {
			return nil, getErr
		}
		jobs = append(jobs, job)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// ClaimMailDeliveryJobs leases ready queued jobs and stale processing jobs for
// one worker so network delivery can happen outside the database transaction.
func (s *Store) ClaimMailDeliveryJobs(ctx context.Context, input ClaimMailDeliveryJobsInput) ([]model.MailDeliveryJob, error) {
	if input.Limit < 1 {
		return nil, fmt.Errorf("mail delivery claim limit must be at least 1")
	}
	if input.LeaseDuration <= 0 {
		return nil, fmt.Errorf("mail delivery lease duration must be greater than 0")
	}

	now := input.Now.UTC()
	staleBefore := now.Add(-input.LeaseDuration)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	rows, err := tx.QueryContext(ctx, `
SELECT id
FROM mail_delivery_jobs
WHERE (
    status = ? AND next_attempt_at <= ?
) OR (
    status = ? AND processing_started_at IS NOT NULL AND processing_started_at <= ?
)
ORDER BY next_attempt_at ASC, id ASC
LIMIT ?
FOR UPDATE SKIP LOCKED
`,
		model.MailDeliveryJobStatusQueued,
		formatTime(now),
		model.MailDeliveryJobStatusProcessing,
		formatTime(staleBefore),
		input.Limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	jobIDs, err := scanMailDeliveryJobIDs(rows)
	if err != nil {
		return nil, err
	}
	if len(jobIDs) == 0 {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}

	jobs := make([]model.MailDeliveryJob, 0, len(jobIDs))
	for _, jobID := range jobIDs {
		if _, err := tx.ExecContext(ctx, `
UPDATE mail_delivery_jobs
SET
    status = ?,
    attempt_count = attempt_count + 1,
    processing_started_at = ?,
    last_attempt_at = ?,
    updated_at = ?
WHERE id = ?
`,
			model.MailDeliveryJobStatusProcessing,
			formatTime(now),
			formatTime(now),
			formatTime(now),
			jobID,
		); err != nil {
			return nil, err
		}

		job, getErr := getMailDeliveryJobTx(ctx, tx, jobID, false)
		if getErr != nil {
			return nil, getErr
		}
		jobs = append(jobs, job)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return jobs, nil
}

// MarkMailDeliveryJobDelivered persists the terminal success state for one
// outbound delivery job after the remote SMTP side accepted the message.
func (s *Store) MarkMailDeliveryJobDelivered(ctx context.Context, input MarkMailDeliveryJobDeliveredInput) (model.MailDeliveryJob, error) {
	now := input.DeliveredAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	defer tx.Rollback()

	job, err := getMailDeliveryJobTx(ctx, tx, input.ID, true)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if job.Status == model.MailDeliveryJobStatusDelivered {
		if err := tx.Commit(); err != nil {
			return model.MailDeliveryJob{}, err
		}
		return job, nil
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE mail_delivery_jobs
SET
    status = ?,
    processing_started_at = NULL,
    last_error = '',
    delivered_at = ?,
    updated_at = ?
WHERE id = ?
`,
		model.MailDeliveryJobStatusDelivered,
		formatTime(now),
		formatTime(now),
		input.ID,
	); err != nil {
		return model.MailDeliveryJob{}, err
	}

	job, err = getMailDeliveryJobTx(ctx, tx, input.ID, false)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MailDeliveryJob{}, err
	}
	return job, nil
}

// MarkMailDeliveryJobRetry returns one transiently failed job back to the queue
// with its next retry time already calculated by the worker.
func (s *Store) MarkMailDeliveryJobRetry(ctx context.Context, input MarkMailDeliveryJobRetryInput) (model.MailDeliveryJob, error) {
	now := input.UpdatedAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	defer tx.Rollback()

	job, err := getMailDeliveryJobTx(ctx, tx, input.ID, true)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if job.Status == model.MailDeliveryJobStatusDelivered || job.Status == model.MailDeliveryJobStatusFailed {
		if err := tx.Commit(); err != nil {
			return model.MailDeliveryJob{}, err
		}
		return job, nil
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE mail_delivery_jobs
SET
    status = ?,
    processing_started_at = NULL,
    next_attempt_at = ?,
    last_error = ?,
    updated_at = ?
WHERE id = ?
`,
		model.MailDeliveryJobStatusQueued,
		formatTime(input.NextAttemptAt.UTC()),
		strings.TrimSpace(input.LastError),
		formatTime(now),
		input.ID,
	); err != nil {
		return model.MailDeliveryJob{}, err
	}

	job, err = getMailDeliveryJobTx(ctx, tx, input.ID, false)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MailDeliveryJob{}, err
	}
	return job, nil
}

// MarkMailDeliveryJobFailed persists one terminal failure and refunds any
// catch-all quota reservations in the same transaction.
func (s *Store) MarkMailDeliveryJobFailed(ctx context.Context, input MarkMailDeliveryJobFailedInput) (model.MailDeliveryJob, error) {
	now := input.FailedAt.UTC()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	defer tx.Rollback()

	job, err := getMailDeliveryJobTx(ctx, tx, input.ID, true)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if job.Status == model.MailDeliveryJobStatusFailed || job.Status == model.MailDeliveryJobStatusDelivered {
		if err := tx.Commit(); err != nil {
			return model.MailDeliveryJob{}, err
		}
		return job, nil
	}

	for _, reservation := range job.Reservations {
		if refundErr := refundMailQueueCatchAllReservationTx(ctx, tx, reservation, now); refundErr != nil {
			return model.MailDeliveryJob{}, refundErr
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE mail_delivery_jobs
SET
    status = ?,
    processing_started_at = NULL,
    last_error = ?,
    failed_at = ?,
    updated_at = ?
WHERE id = ?
`,
		model.MailDeliveryJobStatusFailed,
		strings.TrimSpace(input.LastError),
		formatTime(now),
		formatTime(now),
		input.ID,
	); err != nil {
		return model.MailDeliveryJob{}, err
	}

	job, err = getMailDeliveryJobTx(ctx, tx, input.ID, false)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.MailDeliveryJob{}, err
	}
	return job, nil
}

// CleanupMailDeliveryJobs deletes terminal jobs beyond the configured
// retention windows and then removes any now-orphaned raw message rows.
func (s *Store) CleanupMailDeliveryJobs(ctx context.Context, input CleanupMailDeliveryJobsInput) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	result, err := tx.ExecContext(ctx, `
DELETE FROM mail_delivery_jobs
WHERE (
    status = ? AND delivered_at IS NOT NULL AND delivered_at <= ?
) OR (
    status = ? AND failed_at IS NOT NULL AND failed_at <= ?
)
`,
		model.MailDeliveryJobStatusDelivered,
		formatTime(input.DeliveredBefore.UTC()),
		model.MailDeliveryJobStatusFailed,
		formatTime(input.FailedBefore.UTC()),
	)
	if err != nil {
		return 0, err
	}

	deletedJobs, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}

	if _, err := tx.ExecContext(ctx, `
DELETE FROM mail_messages
WHERE NOT EXISTS (
    SELECT 1
    FROM mail_delivery_jobs
    WHERE mail_delivery_jobs.message_id = mail_messages.id
)
`); err != nil {
		return 0, err
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deletedJobs, nil
}

// getMailDeliveryJobTx loads one durable delivery job and its raw message
// payload from the current transaction. The caller may request a row lock when
// a subsequent update must stay race-free.
func getMailDeliveryJobTx(ctx context.Context, tx *queryTx, jobID int64, forUpdate bool) (model.MailDeliveryJob, error) {
	query := `
SELECT
    j.id,
    j.message_id,
    m.original_envelope_from,
    j.original_recipients_json,
    j.target_recipients_json,
    m.raw_message,
    m.message_size_bytes,
    j.reservations_json,
    j.status,
    j.attempt_count,
    j.max_attempts,
    j.next_attempt_at,
    j.processing_started_at,
    j.last_attempt_at,
    j.last_error,
    j.delivered_at,
    j.failed_at,
    j.created_at,
    j.updated_at
FROM mail_delivery_jobs j
JOIN mail_messages m ON m.id = j.message_id
WHERE j.id = ?
`
	if forUpdate {
		query += " FOR UPDATE"
	}
	row := tx.QueryRowContext(ctx, query, jobID)
	return scanMailDeliveryJob(row)
}

// scanMailDeliveryJobIDs reads one result set of candidate job IDs into memory
// so the claim transaction can update them deterministically afterward.
func scanMailDeliveryJobIDs(rows *sql.Rows) ([]int64, error) {
	ids := make([]int64, 0, 8)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

// scanMailDeliveryJob maps one joined queue row into the model package.
func scanMailDeliveryJob(scanner interface{ Scan(dest ...any) error }) (model.MailDeliveryJob, error) {
	var item model.MailDeliveryJob
	var originalRecipientsJSON string
	var targetRecipientsJSON string
	var reservationsJSON string
	var processingStartedAt sql.NullString
	var lastAttemptAt sql.NullString
	var deliveredAt sql.NullString
	var failedAt sql.NullString
	var createdAt string
	var updatedAt string
	var nextAttemptAt string

	err := scanner.Scan(
		&item.ID,
		&item.MessageID,
		&item.OriginalEnvelopeFrom,
		&originalRecipientsJSON,
		&targetRecipientsJSON,
		&item.RawMessage,
		&item.MessageSizeBytes,
		&reservationsJSON,
		&item.Status,
		&item.AttemptCount,
		&item.MaxAttempts,
		&nextAttemptAt,
		&processingStartedAt,
		&lastAttemptAt,
		&item.LastError,
		&deliveredAt,
		&failedAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.MailDeliveryJob{}, err
	}

	if item.OriginalRecipients, err = unmarshalStringSliceJSON(originalRecipientsJSON); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.TargetRecipients, err = unmarshalStringSliceJSON(targetRecipientsJSON); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.Reservations, err = unmarshalMailDeliveryReservationsJSON(reservationsJSON); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.NextAttemptAt, err = parseTime(nextAttemptAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.ProcessingStartedAt, err = parseNullableTime(processingStartedAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.LastAttemptAt, err = parseNullableTime(lastAttemptAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.DeliveredAt, err = parseNullableTime(deliveredAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.FailedAt, err = parseNullableTime(failedAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.MailDeliveryJob{}, err
	}
	return item, nil
}

// loadMailQueueDefaultDailyLimitTx reads the currently configured catch-all
// default daily limit inside the enqueue transaction so quota checks stay
// aligned with the same commit boundary as the queued jobs.
func loadMailQueueDefaultDailyLimitTx(ctx context.Context, tx *queryTx) (int64, error) {
	row := tx.QueryRowContext(ctx, `
SELECT default_daily_limit
FROM permission_policies
WHERE key = ?
`,
		mailQueueCatchAllPolicyKey,
	)

	var defaultDailyLimit int64
	if err := row.Scan(&defaultDailyLimit); err != nil {
		if storage.IsNotFound(err) {
			return 1_000_000, nil
		}
		return 0, err
	}
	if defaultDailyLimit <= 0 {
		return 1_000_000, nil
	}
	return defaultDailyLimit, nil
}

// consumeMailQueueCatchAllReservationTx uses the existing catch-all access
// tables inside the current queue transaction so quota and enqueue commit as a
// single atomic unit.
func consumeMailQueueCatchAllReservationTx(ctx context.Context, tx *queryTx, userID int64, count int64, defaultDailyLimit int64, now time.Time) (model.MailDeliveryReservation, error) {
	usageDate := now.Format("2006-01-02")

	access, accessExists, err := getEmailCatchAllAccessTx(ctx, tx, userID)
	if err != nil {
		return model.MailDeliveryReservation{}, err
	}
	if !accessExists {
		access = model.EmailCatchAllAccess{
			UserID:         userID,
			RemainingCount: 0,
		}
	}

	usage, usageExists, err := getEmailCatchAllDailyUsageTx(ctx, tx, userID, usageDate)
	if err != nil {
		return model.MailDeliveryReservation{}, err
	}
	if !usageExists {
		usage = model.EmailCatchAllDailyUsage{
			UserID:    userID,
			UsageDate: usageDate,
			UsedCount: 0,
		}
	}

	effectiveDailyLimit := defaultDailyLimit
	if access.DailyLimitOverride != nil {
		effectiveDailyLimit = *access.DailyLimitOverride
	}
	if effectiveDailyLimit <= 0 {
		effectiveDailyLimit = 1_000_000
	}
	if usage.UsedCount+count > effectiveDailyLimit {
		return model.MailDeliveryReservation{}, storage.ErrEmailCatchAllDailyLimitExceeded
	}

	consumedMode := "subscription"
	subscriptionActive := access.SubscriptionExpiresAt != nil && access.SubscriptionExpiresAt.After(now)
	if !subscriptionActive {
		if access.RemainingCount < count {
			return model.MailDeliveryReservation{}, storage.ErrEmailCatchAllInsufficientRemainingCount
		}
		access.RemainingCount -= count
		consumedMode = "quantity"
		if _, err := tx.ExecContext(ctx, `
UPDATE email_catch_all_access
SET remaining_count = ?, updated_at = ?
WHERE user_id = ?
`,
			access.RemainingCount,
			formatTime(now),
			userID,
		); err != nil {
			return model.MailDeliveryReservation{}, err
		}
	}

	if usageExists {
		if _, err := tx.ExecContext(ctx, `
UPDATE email_catch_all_daily_usage
SET used_count = used_count + ?, updated_at = ?
WHERE user_id = ? AND usage_date = ?
`,
			count,
			formatTime(now),
			userID,
			usageDate,
		); err != nil {
			return model.MailDeliveryReservation{}, err
		}
	} else {
		if _, err := tx.ExecContext(ctx, `
INSERT INTO email_catch_all_daily_usage (
    user_id,
    usage_date,
    used_count,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?)
`,
			userID,
			usageDate,
			count,
			formatTime(now),
			formatTime(now),
		); err != nil {
			return model.MailDeliveryReservation{}, err
		}
	}

	return model.MailDeliveryReservation{
		UserID:       userID,
		Count:        count,
		ConsumedMode: consumedMode,
		UsageDate:    usageDate,
	}, nil
}

// refundMailQueueCatchAllReservationTx reverses one reservation within the same
// transaction that marks the delivery job terminally failed.
func refundMailQueueCatchAllReservationTx(ctx context.Context, tx *queryTx, reservation model.MailDeliveryReservation, now time.Time) error {
	access, accessExists, err := getEmailCatchAllAccessTx(ctx, tx, reservation.UserID)
	if err != nil {
		return err
	}
	if !accessExists {
		return fmt.Errorf("catch-all access state does not exist")
	}

	usage, usageExists, err := getEmailCatchAllDailyUsageTx(ctx, tx, reservation.UserID, reservation.UsageDate)
	if err != nil {
		return err
	}
	if !usageExists || usage.UsedCount < reservation.Count {
		return fmt.Errorf("catch-all daily usage cannot be refunded")
	}

	if reservation.ConsumedMode == "quantity" {
		access.RemainingCount += reservation.Count
		if _, err := tx.ExecContext(ctx, `
UPDATE email_catch_all_access
SET remaining_count = ?, updated_at = ?
WHERE user_id = ?
`,
			access.RemainingCount,
			formatTime(now),
			reservation.UserID,
		); err != nil {
			return err
		}
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE email_catch_all_daily_usage
SET used_count = used_count - ?, updated_at = ?
WHERE user_id = ? AND usage_date = ?
`,
		reservation.Count,
		formatTime(now),
		reservation.UserID,
		reservation.UsageDate,
	); err != nil {
		return err
	}

	return nil
}

// marshalStringSliceJSON stores address lists as stable JSON arrays so both
// database backends can parse them uniformly later.
func marshalStringSliceJSON(values []string) (string, error) {
	normalized := append([]string(nil), values...)
	if len(normalized) == 0 {
		normalized = []string{}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal string slice json: %w", err)
	}
	return string(encoded), nil
}

// unmarshalStringSliceJSON restores one JSON string-array column.
func unmarshalStringSliceJSON(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("unmarshal string slice json: %w", err)
	}
	if values == nil {
		return []string{}, nil
	}
	return values, nil
}

// marshalMailDeliveryReservationsJSON stores the reservation metadata needed to
// refund catch-all quota if the job later fails permanently.
func marshalMailDeliveryReservationsJSON(values []model.MailDeliveryReservation) (string, error) {
	normalized := append([]model.MailDeliveryReservation(nil), values...)
	if len(normalized) == 0 {
		normalized = []model.MailDeliveryReservation{}
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("marshal mail reservations json: %w", err)
	}
	return string(encoded), nil
}

// unmarshalMailDeliveryReservationsJSON restores one JSON reservation array.
func unmarshalMailDeliveryReservationsJSON(raw string) ([]model.MailDeliveryReservation, error) {
	if strings.TrimSpace(raw) == "" {
		return []model.MailDeliveryReservation{}, nil
	}
	var values []model.MailDeliveryReservation
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, fmt.Errorf("unmarshal mail reservations json: %w", err)
	}
	if values == nil {
		return []model.MailDeliveryReservation{}, nil
	}
	return values, nil
}

// normalizeMailAddressSlice trims, lowercases, and removes empty values so one
// queued job never carries SMTP addresses that differ only by formatting.
func normalizeMailAddressSlice(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		normalized := strings.ToLower(strings.TrimSpace(value))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		result = append(result, normalized)
	}
	return result
}

// uniqueInt64Values removes duplicate owner IDs while keeping first-seen
// ordering stable so tests and audit output stay deterministic.
func uniqueInt64Values(values []int64) []int64 {
	result := make([]int64, 0, len(values))
	seen := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
