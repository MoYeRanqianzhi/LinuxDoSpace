package storage

import "time"

// UpsertUserInput describes the user fields that must be written during Linux
// Do login synchronization.
type UpsertUserInput struct {
	LinuxDOUserID  int64
	Username       string
	DisplayName    string
	AvatarURL      string
	TrustLevel     int
	IsLinuxDOAdmin bool
	IsAppAdmin     bool
}

// CreateSessionInput describes one server-side session row that should be
// created after a successful login.
type CreateSessionInput struct {
	ID                   string
	UserID               int64
	CSRFToken            string
	UserAgentFingerprint string
	AdminVerifiedAt      *time.Time
	ExpiresAt            time.Time
}

// UpsertManagedDomainInput describes one distributable root domain row.
type UpsertManagedDomainInput struct {
	RootDomain       string
	CloudflareZoneID string
	DefaultQuota     int
	AutoProvision    bool
	IsDefault        bool
	Enabled          bool
}

// SetUserQuotaInput describes one user-specific quota override for one root
// domain.
type SetUserQuotaInput struct {
	UserID          int64
	ManagedDomainID int64
	MaxAllocations  int
	Reason          string
}

// CreateAllocationInput describes one new namespace allocation.
type CreateAllocationInput struct {
	UserID           int64
	ManagedDomainID  int64
	Prefix           string
	NormalizedPrefix string
	FQDN             string
	IsPrimary        bool
	Source           string
	Status           string
}

// UpdateAllocationInput describes the mutable administrator-managed portion of
// one namespace allocation row.
type UpdateAllocationInput struct {
	ID        int64
	UserID    int64
	IsPrimary bool
	Source    string
	Status    string
}

// AuditLogInput describes one auditable backend action.
type AuditLogInput struct {
	ActorUserID  *int64
	Action       string
	ResourceType string
	ResourceID   string
	MetadataJSON string
}

// UpsertUserControlInput describes a moderation update for one local user.
type UpsertUserControlInput struct {
	UserID   int64
	IsBanned bool
	Note     string
}

// CreateEmailRouteInput describes one new email forwarding rule.
type CreateEmailRouteInput struct {
	OwnerUserID int64
	RootDomain  string
	Prefix      string
	TargetEmail string
	Enabled     bool
}

// UpsertEmailRouteByAddressInput describes an idempotent email-route write
// keyed by root domain and local prefix.
type UpsertEmailRouteByAddressInput struct {
	OwnerUserID int64
	RootDomain  string
	Prefix      string
	TargetEmail string
	Enabled     bool
}

// UpdateEmailRouteInput describes the mutable portion of one email forwarding
// rule.
type UpdateEmailRouteInput struct {
	ID          int64
	TargetEmail string
	Enabled     bool
}

// CreateEmailTargetInput describes one new user-owned forwarding destination
// mailbox.
type CreateEmailTargetInput struct {
	OwnerUserID            int64
	Email                  string
	CloudflareAddressID    string
	VerifiedAt             *time.Time
	LastVerificationSentAt *time.Time
}

// UpdateEmailTargetInput describes the mutable synchronization fields for one
// forwarding destination mailbox.
type UpdateEmailTargetInput struct {
	ID                     int64
	CloudflareAddressID    string
	VerifiedAt             *time.Time
	LastVerificationSentAt *time.Time
}

// UpsertAdminApplicationInput describes one user-side permission application.
type UpsertAdminApplicationInput struct {
	ApplicantUserID  int64
	Type             string
	Target           string
	Reason           string
	Status           string
	ReviewNote       string
	ReviewedByUserID *int64
	ReviewedAt       *time.Time
}

// UpdateAdminApplicationInput describes one administrator review decision.
type UpdateAdminApplicationInput struct {
	ID               int64
	Status           string
	ReviewNote       string
	ReviewedByUserID int64
}

// UpsertPermissionPolicyInput describes the mutable fields of one permission
// policy row.
type UpsertPermissionPolicyInput struct {
	Key               string
	DisplayName       string
	Description       string
	Enabled           bool
	AutoApprove       bool
	MinTrustLevel     int
	DefaultDailyLimit int64
}

// CreateRedeemCodeInput describes one administrator-issued redeem code.
type CreateRedeemCodeInput struct {
	Code            string
	Type            string
	Target          string
	Note            string
	CreatedByUserID int64
}

// CreateQuantityRecordInput describes one append-only quantity delta written to
// the generic user resource ledger.
type CreateQuantityRecordInput struct {
	UserID          int64
	ResourceKey     string
	Scope           string
	Delta           int
	Source          string
	Reason          string
	ReferenceType   string
	ReferenceID     string
	ExpiresAt       *time.Time
	CreatedByUserID *int64
}

// UpsertEmailCatchAllAccessInput describes the mutable runtime access state
// for one user's catch-all mailbox delivery allowance.
type UpsertEmailCatchAllAccessInput struct {
	UserID                int64
	SubscriptionExpiresAt *time.Time
	RemainingCount        int64
	DailyLimitOverride    *int64
}

// ConsumeEmailCatchAllInput describes one atomic usage-reservation request
// issued by the SMTP relay for catch-all mail.
type ConsumeEmailCatchAllInput struct {
	UserID            int64
	Count             int64
	DefaultDailyLimit int64
	Now               time.Time
}
