package model

import "time"

// UserControl stores security-sensitive moderation controls that are maintained by application administrators.
type UserControl struct {
	UserID    int64     `json:"user_id"`
	IsBanned  bool      `json:"is_banned"`
	Note      string    `json:"note"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// AdminPasswordAttempt stores one persisted limiter bucket for the
// administrator second-factor password endpoint.
type AdminPasswordAttempt struct {
	BucketType   string     `json:"bucket_type"`
	BucketKey    string     `json:"bucket_key"`
	FailureCount int        `json:"failure_count"`
	BlockedUntil *time.Time `json:"blocked_until,omitempty"`
	LastSeenAt   time.Time  `json:"last_seen_at"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

// AdminUserSummary represents the compact user row rendered by the admin console user table.
type AdminUserSummary struct {
	ID              int64     `json:"id"`
	LinuxDOUserID   int64     `json:"linuxdo_user_id"`
	Username        string    `json:"username"`
	DisplayName     string    `json:"display_name"`
	AvatarURL       string    `json:"avatar_url"`
	TrustLevel      int       `json:"trust_level"`
	IsLinuxDOAdmin  bool      `json:"is_linuxdo_admin"`
	IsAppAdmin      bool      `json:"is_app_admin"`
	IsBanned        bool      `json:"is_banned"`
	AllocationCount int       `json:"allocation_count"`
	CreatedAt       time.Time `json:"created_at"`
	LastLoginAt     time.Time `json:"last_login_at"`
}

// AdminAllocationSummary represents an allocation row with owner information for administrator workflows.
type AdminAllocationSummary struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	OwnerUsername    string    `json:"owner_username"`
	OwnerDisplayName string    `json:"owner_display_name"`
	ManagedDomainID  int64     `json:"managed_domain_id"`
	RootDomain       string    `json:"root_domain"`
	Prefix           string    `json:"prefix"`
	NormalizedPrefix string    `json:"normalized_prefix"`
	FQDN             string    `json:"fqdn"`
	IsPrimary        bool      `json:"is_primary"`
	Source           string    `json:"source"`
	Status           string    `json:"status"`
	CloudflareZoneID string    `json:"cloudflare_zone_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// AdminDNSRecord represents a DNS record row shown to administrators across all managed allocations.
type AdminDNSRecord struct {
	AllocationID     int64  `json:"allocation_id"`
	OwnerUserID      int64  `json:"owner_user_id"`
	OwnerUsername    string `json:"owner_username"`
	OwnerDisplayName string `json:"owner_display_name"`
	RootDomain       string `json:"root_domain"`
	NamespaceFQDN    string `json:"namespace_fqdn"`
	ID               string `json:"id"`
	Type             string `json:"type"`
	Name             string `json:"name"`
	RelativeName     string `json:"relative_name"`
	Content          string `json:"content"`
	TTL              int    `json:"ttl"`
	Proxied          bool   `json:"proxied"`
	Comment          string `json:"comment"`
	Priority         *int   `json:"priority,omitempty"`
}

// EmailRoute stores one administrator-managed forwarding rule for a managed domain.
type EmailRoute struct {
	ID                  int64     `json:"id"`
	OwnerUserID         int64     `json:"owner_user_id"`
	OwnerUsername       string    `json:"owner_username"`
	OwnerDisplayName    string    `json:"owner_display_name"`
	RootDomain          string    `json:"root_domain"`
	Prefix              string    `json:"prefix"`
	TargetEmail         string    `json:"target_email"`
	TargetKind          string    `json:"target_kind"`
	TargetTokenPublicID string    `json:"target_token_public_id"`
	Enabled             bool      `json:"enabled"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// AdminApplication stores a moderation request awaiting administrative review.
type AdminApplication struct {
	ID                int64      `json:"id"`
	ApplicantUserID   int64      `json:"applicant_user_id"`
	ApplicantUsername string     `json:"applicant_username"`
	ApplicantName     string     `json:"applicant_name"`
	Type              string     `json:"type"`
	Target            string     `json:"target"`
	Reason            string     `json:"reason"`
	Status            string     `json:"status"`
	ReviewNote        string     `json:"review_note"`
	ReviewedByUserID  *int64     `json:"reviewed_by_user_id,omitempty"`
	ReviewedAt        *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// RedeemCode stores one generated administrative code that can later be redeemed by a user-facing flow.
type RedeemCode struct {
	ID                int64      `json:"id"`
	Code              string     `json:"code"`
	Type              string     `json:"type"`
	Target            string     `json:"target"`
	Note              string     `json:"note"`
	CreatedByUserID   int64      `json:"created_by_user_id"`
	CreatedByUsername string     `json:"created_by_username"`
	UsedByUserID      *int64     `json:"used_by_user_id,omitempty"`
	UsedByUsername    string     `json:"used_by_username,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	UsedAt            *time.Time `json:"used_at,omitempty"`
}
