package model

import "time"

// PermissionPolicy stores one administrator-configurable rule set that decides
// whether a specific user-facing permission can currently be requested and
// whether matching requests should be auto-approved.
type PermissionPolicy struct {
	Key               string    `json:"key"`
	DisplayName       string    `json:"display_name"`
	Description       string    `json:"description"`
	Enabled           bool      `json:"enabled"`
	AutoApprove       bool      `json:"auto_approve"`
	MinTrustLevel     int       `json:"min_trust_level"`
	DefaultDailyLimit int64     `json:"default_daily_limit"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}
