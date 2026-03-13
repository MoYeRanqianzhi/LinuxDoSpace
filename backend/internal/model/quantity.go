package model

import "time"

// QuantityRecord stores one append-only quantity delta granted to or removed
// from a user for a specific resource key and optional scope.
//
// This model is intentionally generic so later billing, redeem codes, manual
// grants, subscription renewals, and promotional campaigns can all write into
// the same auditable ledger instead of inventing separate balance tables.
type QuantityRecord struct {
	ID                int64      `json:"id"`
	UserID            int64      `json:"user_id"`
	Username          string     `json:"username"`
	DisplayName       string     `json:"display_name"`
	ResourceKey       string     `json:"resource_key"`
	Scope             string     `json:"scope"`
	Delta             int        `json:"delta"`
	Source            string     `json:"source"`
	Reason            string     `json:"reason"`
	ReferenceType     string     `json:"reference_type"`
	ReferenceID       string     `json:"reference_id"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
	CreatedByUserID   *int64     `json:"created_by_user_id,omitempty"`
	CreatedByUsername string     `json:"created_by_username,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

// QuantityBalance stores the current non-expired summed quantity for one user,
// resource key, and optional scope.
type QuantityBalance struct {
	UserID          int64  `json:"user_id"`
	Username        string `json:"username"`
	DisplayName     string `json:"display_name"`
	ResourceKey     string `json:"resource_key"`
	Scope           string `json:"scope"`
	CurrentQuantity int    `json:"current_quantity"`
}
