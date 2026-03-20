package model

import "time"

const (
	// APITokenScopeEmail grants access to the email NDJSON stream.
	APITokenScopeEmail = "email"

	// EmailRouteTargetKindEmail means one route forwards to a verified email target.
	EmailRouteTargetKindEmail = "email"

	// EmailRouteTargetKindAPIToken means one route forwards to one API token's live stream.
	EmailRouteTargetKindAPIToken = "api_token"
)

// APIToken stores one user-generated bearer token used by the SDK/API clients.
// The raw bearer secret is never persisted; only its hash is stored.
type APIToken struct {
	ID          int64      `json:"id"`
	OwnerUserID int64      `json:"owner_user_id"`
	Name        string     `json:"name"`
	PublicID    string     `json:"public_id"`
	TokenHash   string     `json:"-"`
	Scopes      []string   `json:"scopes"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}
