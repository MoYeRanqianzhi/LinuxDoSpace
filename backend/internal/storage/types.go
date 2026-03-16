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
	RootDomain         string
	CloudflareZoneID   string
	DefaultQuota       int
	AutoProvision      bool
	IsDefault          bool
	Enabled            bool
	SaleEnabled        bool
	SaleBasePriceCents int64
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
	UserID                   int64
	SubscriptionExpiresAt    *time.Time
	RemainingCount           int64
	TemporaryRewardCount     int64
	TemporaryRewardExpiresAt *time.Time
	DailyLimitOverride       *int64
}

// ConsumeEmailCatchAllInput describes one atomic usage-reservation request
// issued by the SMTP relay for catch-all mail.
type ConsumeEmailCatchAllInput struct {
	UserID            int64
	Count             int64
	DefaultDailyLimit int64
	Now               time.Time
}

// RefundEmailCatchAllInput describes one compensating rollback of a previous
// catch-all usage reservation after SMTP forwarding fails.
type RefundEmailCatchAllInput struct {
	UserID                       int64
	Count                        int64
	ConsumedMode                 string
	ConsumedPermanentCount       int64
	ConsumedTemporaryRewardCount int64
	TemporaryRewardExpiresAt     *time.Time
	UsageDate                    string
	Now                          time.Time
}

// EnqueueMailDeliveryGroupInput describes one final outbound delivery action
// derived from a set of inbound SMTP recipients that all resolve to the same
// verified target mailbox.
type EnqueueMailDeliveryGroupInput struct {
	OriginalRecipients   []string
	TargetRecipients     []string
	CatchAllOwnerUserIDs []int64
}

// EnqueueMailDeliveryBatchInput describes one atomic "accept SMTP mail and
// persist delivery jobs" transaction. The storage layer must either reserve
// any required catch-all quota and enqueue every group, or commit nothing.
type EnqueueMailDeliveryBatchInput struct {
	OriginalEnvelopeFrom string
	RawMessage           []byte
	Groups               []EnqueueMailDeliveryGroupInput
	MaxAttempts          int
	QueuedAt             time.Time
}

// ClaimMailDeliveryJobsInput describes one worker lease request for ready
// queued jobs and stale processing jobs whose previous worker lease expired.
type ClaimMailDeliveryJobsInput struct {
	Limit         int
	LeaseDuration time.Duration
	Now           time.Time
}

// MarkMailDeliveryJobDeliveredInput describes one terminal success update after
// the remote SMTP side accepted the message.
type MarkMailDeliveryJobDeliveredInput struct {
	ID          int64
	DeliveredAt time.Time
}

// MarkMailDeliveryJobRetryInput describes one transient failure update that
// should return the job to the queue with a later retry time.
type MarkMailDeliveryJobRetryInput struct {
	ID            int64
	LastError     string
	NextAttemptAt time.Time
	UpdatedAt     time.Time
}

// MarkMailDeliveryJobFailedInput describes one terminal failure update that
// must also refund any reserved catch-all quota in the same transaction.
type MarkMailDeliveryJobFailedInput struct {
	ID        int64
	LastError string
	FailedAt  time.Time
}

// CleanupMailDeliveryJobsInput describes the retention cutoffs used to delete
// old terminal mail-delivery jobs and any orphaned raw messages they leave
// behind.
type CleanupMailDeliveryJobsInput struct {
	DeliveredBefore time.Time
	FailedBefore    time.Time
}

// UpsertPaymentProductInput describes one administrator-managed purchasable
// Linux Do Credit product row.
type UpsertPaymentProductInput struct {
	Key            string
	DisplayName    string
	Description    string
	Enabled        bool
	UnitPriceCents int64
	GrantQuantity  int64
	GrantUnit      string
	EffectType     string
	SortOrder      int
}

// CreatePaymentOrderInput describes one locally persisted LDC order reserved
// before the backend talks to the upstream payment gateway.
type CreatePaymentOrderInput struct {
	UserID                   int64
	ProductKey               string
	ProductName              string
	Title                    string
	GatewayType              string
	OutTradeNo               string
	Status                   string
	Units                    int64
	GrantQuantity            int64
	GrantedTotal             int64
	GrantUnit                string
	UnitPriceCents           int64
	TotalPriceCents          int64
	EffectType               string
	PurchaseRootDomain       string
	PurchaseMode             string
	PurchasePrefix           string
	PurchaseNormalizedPrefix string
	PurchaseRequestedLength  int
	PurchaseAssignedPrefix   string
	PurchaseAssignedFQDN     string
	PaymentURL               string
}

// UpdatePaymentOrderGatewayStateInput describes the mutable gateway-facing
// portion of one local order, including checkout URL, upstream IDs, and raw
// notification payload.
type UpdatePaymentOrderGatewayStateInput struct {
	OutTradeNo       string
	Status           string
	ProviderTradeNo  string
	PaymentURL       string
	NotifyPayloadRaw string
	PaidAt           *time.Time
	LastCheckedAt    *time.Time
}

// ApplyPaymentOrderEntitlementInput describes one idempotent request to turn a
// paid order into local entitlements and immutable quantity ledger records.
type ApplyPaymentOrderEntitlementInput struct {
	OutTradeNo string
	AppliedAt  time.Time
}

// CreateOrReplacePOWChallengeInput describes one user-bound proof-of-work
// puzzle generated by the backend. The storage layer supersedes any older
// active challenge for the same user before inserting the new row.
type CreateOrReplacePOWChallengeInput struct {
	UserID            int64
	BenefitKey        string
	ResourceKey       string
	Scope             string
	Difficulty        int
	BaseReward        int
	RewardQuantity    int
	RewardUnit        string
	ChallengeToken    string
	SaltHex           string
	Argon2Variant     string
	Argon2MemoryKiB   uint32
	Argon2Iterations  uint32
	Argon2Parallelism uint8
	Argon2HashLength  uint32
	CreatedAt         time.Time
}

// ClaimPOWChallengeRewardInput describes one verified proof-of-work solution
// that should be atomically turned into a user-visible reward.
type ClaimPOWChallengeRewardInput struct {
	UserID               int64
	ChallengeID          int64
	BaseReward           int
	RewardQuantity       int
	RewardExpiresAt      time.Time
	SolutionNonce        string
	SolutionHashHex      string
	ClaimedAt            time.Time
	DailyWindowStart     time.Time
	DailyWindowEnd       time.Time
	MaxDailyCompletions  int
	QuantityRecordReason string
}

// UpsertPOWGlobalSettingsInput describes one administrator-authored update to
// the global proof-of-work feature configuration.
type UpsertPOWGlobalSettingsInput struct {
	Enabled                     bool
	DefaultDailyCompletionLimit int
	BaseRewardMin               int
	BaseRewardMax               int
}

// UpsertPOWBenefitSettingsInput describes one administrator-authored toggle for
// a single proof-of-work benefit target.
type UpsertPOWBenefitSettingsInput struct {
	Key     string
	Enabled bool
}

// UpsertPOWDifficultySettingsInput describes one administrator-authored toggle
// for a single proof-of-work difficulty level.
type UpsertPOWDifficultySettingsInput struct {
	Difficulty int
	Enabled    bool
}

// UpsertPOWUserSettingsInput describes one per-user daily completion limit
// override managed by the administrator console.
type UpsertPOWUserSettingsInput struct {
	UserID                       int64
	DailyCompletionLimitOverride *int
}
