package postgres

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type CreateAPITokenInput = storage.CreateAPITokenInput
type UpdateAPITokenInput = storage.UpdateAPITokenInput

// ListAPITokensByOwner returns every API token created by one local user.
func (s *Store) ListAPITokensByOwner(ctx context.Context, ownerUserID int64) ([]model.APIToken, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    id,
    owner_user_id,
    name,
    public_id,
    token_hash,
    scopes_json,
    last_used_at,
    created_at,
    updated_at,
    revoked_at
FROM api_tokens
WHERE owner_user_id = ?
ORDER BY created_at DESC, id DESC
`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.APIToken, 0, 8)
	for rows.Next() {
		item, scanErr := scanAPIToken(rows)
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

// GetAPITokenByPublicID loads one persisted API token by its public identifier.
func (s *Store) GetAPITokenByPublicID(ctx context.Context, publicID string) (model.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    owner_user_id,
    name,
    public_id,
    token_hash,
    scopes_json,
    last_used_at,
    created_at,
    updated_at,
    revoked_at
FROM api_tokens
WHERE public_id = ?
`, strings.TrimSpace(publicID))
	return scanAPIToken(row)
}

// GetAPITokenByTokenHash authenticates one bearer token via its stored hash.
func (s *Store) GetAPITokenByTokenHash(ctx context.Context, tokenHash string) (model.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    owner_user_id,
    name,
    public_id,
    token_hash,
    scopes_json,
    last_used_at,
    created_at,
    updated_at,
    revoked_at
FROM api_tokens
WHERE token_hash = ?
`, strings.TrimSpace(tokenHash))
	return scanAPIToken(row)
}

// CreateAPIToken inserts one newly issued user-managed API token row.
func (s *Store) CreateAPIToken(ctx context.Context, input CreateAPITokenInput) (model.APIToken, error) {
	now := time.Now().UTC()
	scopesJSON, err := marshalStringSliceJSON(input.Scopes)
	if err != nil {
		return model.APIToken{}, err
	}

	row := s.db.QueryRowContext(ctx, `
INSERT INTO api_tokens (
    owner_user_id,
    name,
    public_id,
    token_hash,
    scopes_json,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id
`,
		input.OwnerUserID,
		strings.TrimSpace(input.Name),
		strings.TrimSpace(input.PublicID),
		strings.TrimSpace(input.TokenHash),
		scopesJSON,
		formatTime(now),
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.APIToken{}, err
	}
	return s.getAPITokenByID(ctx, id)
}

// UpdateAPIToken refreshes one persisted API token's mutable metadata.
func (s *Store) UpdateAPIToken(ctx context.Context, input UpdateAPITokenInput) (model.APIToken, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
UPDATE api_tokens
SET
    last_used_at = COALESCE(?, last_used_at),
    revoked_at = COALESCE(?, revoked_at),
    updated_at = ?
WHERE id = ?
RETURNING id
`,
		formatNullableTime(input.LastUsedAt),
		formatNullableTime(input.RevokedAt),
		formatTime(now),
		input.ID,
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.APIToken{}, err
	}
	return s.getAPITokenByID(ctx, id)
}

func (s *Store) getAPITokenByID(ctx context.Context, id int64) (model.APIToken, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    owner_user_id,
    name,
    public_id,
    token_hash,
    scopes_json,
    last_used_at,
    created_at,
    updated_at,
    revoked_at
FROM api_tokens
WHERE id = ?
`, id)
	return scanAPIToken(row)
}

func scanAPIToken(scanner interface{ Scan(dest ...any) error }) (model.APIToken, error) {
	var item model.APIToken
	var scopesJSON string
	var lastUsedAt sql.NullString
	var createdAt string
	var updatedAt string
	var revokedAt sql.NullString

	err := scanner.Scan(
		&item.ID,
		&item.OwnerUserID,
		&item.Name,
		&item.PublicID,
		&item.TokenHash,
		&scopesJSON,
		&lastUsedAt,
		&createdAt,
		&updatedAt,
		&revokedAt,
	)
	if err != nil {
		return model.APIToken{}, err
	}

	if item.Scopes, err = unmarshalStringSliceJSON(scopesJSON); err != nil {
		return model.APIToken{}, err
	}
	if item.LastUsedAt, err = parseNullableTime(lastUsedAt); err != nil {
		return model.APIToken{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.APIToken{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.APIToken{}, err
	}
	if item.RevokedAt, err = parseNullableTime(revokedAt); err != nil {
		return model.APIToken{}, err
	}
	return item, nil
}
