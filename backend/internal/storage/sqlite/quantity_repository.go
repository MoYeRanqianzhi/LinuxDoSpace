package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type CreateQuantityRecordInput = storage.CreateQuantityRecordInput

// ListQuantityRecordsByUser returns the append-only quantity ledger for one
// user, ordered from newest record to oldest so billing history UIs can stream
// the most recent changes first.
func (s *Store) ListQuantityRecordsByUser(ctx context.Context, userID int64) ([]model.QuantityRecord, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    qr.id,
    qr.user_id,
    u.username,
    u.display_name,
    qr.resource_key,
    qr.scope,
    qr.delta,
    qr.source,
    qr.reason,
    qr.reference_type,
    qr.reference_id,
    qr.expires_at,
    qr.created_by_user_id,
    COALESCE(creator.username, ''),
    qr.created_at
FROM quantity_records qr
INNER JOIN users u ON u.id = qr.user_id
LEFT JOIN users creator ON creator.id = qr.created_by_user_id
WHERE qr.user_id = ?
ORDER BY qr.created_at DESC, qr.id DESC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.QuantityRecord, 0, 8)
	for rows.Next() {
		item, scanErr := scanQuantityRecord(rows)
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

// ListQuantityBalancesByUser returns the current non-expired balances grouped
// by resource key and scope for one user.
func (s *Store) ListQuantityBalancesByUser(ctx context.Context, userID int64, now time.Time) ([]model.QuantityBalance, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    qr.user_id,
    u.username,
    u.display_name,
    qr.resource_key,
    qr.scope,
    COALESCE(SUM(CASE
        WHEN qr.expires_at IS NULL OR qr.expires_at = '' OR qr.expires_at > ? THEN qr.delta
        ELSE 0
    END), 0) AS current_quantity
FROM quantity_records qr
INNER JOIN users u ON u.id = qr.user_id
WHERE qr.user_id = ?
GROUP BY qr.user_id, u.username, u.display_name, qr.resource_key, qr.scope
HAVING COALESCE(SUM(CASE
    WHEN qr.expires_at IS NULL OR qr.expires_at = '' OR qr.expires_at > ? THEN qr.delta
    ELSE 0
END), 0) <> 0
ORDER BY qr.resource_key ASC, qr.scope ASC
`, formatTime(now.UTC()), userID, formatTime(now.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.QuantityBalance, 0, 8)
	for rows.Next() {
		item, scanErr := scanQuantityBalance(rows)
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

// CreateQuantityRecord appends one immutable quantity delta row to the user
// ledger and returns the persisted record with joined user metadata.
func (s *Store) CreateQuantityRecord(ctx context.Context, input CreateQuantityRecordInput) (model.QuantityRecord, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
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
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id
`,
		input.UserID,
		strings.TrimSpace(input.ResourceKey),
		strings.TrimSpace(input.Scope),
		input.Delta,
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.Reason),
		strings.TrimSpace(input.ReferenceType),
		strings.TrimSpace(input.ReferenceID),
		formatNullableTime(input.ExpiresAt),
		input.CreatedByUserID,
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.QuantityRecord{}, err
	}
	return s.getQuantityRecordByID(ctx, id)
}

// getQuantityRecordByID loads one stored quantity record by its local primary
// key so create paths can return the joined usernames immediately.
func (s *Store) getQuantityRecordByID(ctx context.Context, id int64) (model.QuantityRecord, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    qr.id,
    qr.user_id,
    u.username,
    u.display_name,
    qr.resource_key,
    qr.scope,
    qr.delta,
    qr.source,
    qr.reason,
    qr.reference_type,
    qr.reference_id,
    qr.expires_at,
    qr.created_by_user_id,
    COALESCE(creator.username, ''),
    qr.created_at
FROM quantity_records qr
INNER JOIN users u ON u.id = qr.user_id
LEFT JOIN users creator ON creator.id = qr.created_by_user_id
WHERE qr.id = ?
`, id)
	return scanQuantityRecord(row)
}

// scanQuantityRecord maps one joined quantity-record row into the model package.
func scanQuantityRecord(scanner interface{ Scan(dest ...any) error }) (model.QuantityRecord, error) {
	var item model.QuantityRecord
	var expiresAt sql.NullString
	var createdByUserID sql.NullInt64
	var createdAt string

	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Username,
		&item.DisplayName,
		&item.ResourceKey,
		&item.Scope,
		&item.Delta,
		&item.Source,
		&item.Reason,
		&item.ReferenceType,
		&item.ReferenceID,
		&expiresAt,
		&createdByUserID,
		&item.CreatedByUsername,
		&createdAt,
	); err != nil {
		return model.QuantityRecord{}, err
	}

	if createdByUserID.Valid {
		value := createdByUserID.Int64
		item.CreatedByUserID = &value
	}

	var err error
	item.ExpiresAt, err = parseNullableTime(expiresAt)
	if err != nil {
		return model.QuantityRecord{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.QuantityRecord{}, err
	}

	return item, nil
}

// scanQuantityBalance maps one grouped balance row into the model package.
func scanQuantityBalance(scanner interface{ Scan(dest ...any) error }) (model.QuantityBalance, error) {
	var item model.QuantityBalance
	if err := scanner.Scan(
		&item.UserID,
		&item.Username,
		&item.DisplayName,
		&item.ResourceKey,
		&item.Scope,
		&item.CurrentQuantity,
	); err != nil {
		return model.QuantityBalance{}, err
	}
	return item, nil
}
