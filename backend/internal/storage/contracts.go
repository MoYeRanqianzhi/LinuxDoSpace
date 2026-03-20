package storage

import (
	"context"
	"time"

	"linuxdospace/backend/internal/model"
)

// Store describes the persistence capabilities the business services require.
//
// The interface is intentionally storage-backend agnostic so SQLite and
// PostgreSQL can both satisfy it without leaking driver-specific DTOs upward.
type Store interface {
	UpsertUser(ctx context.Context, input UpsertUserInput) (model.User, error)
	GetUserByID(ctx context.Context, userID int64) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	CreateSession(ctx context.Context, input CreateSessionInput) (model.Session, error)
	CreateSessionFromOAuthState(ctx context.Context, stateID string, input CreateSessionInput) (model.Session, error)
	GetSessionWithUserByID(ctx context.Context, sessionID string) (model.Session, model.User, error)
	MarkSessionAdminVerified(ctx context.Context, sessionID string, verifiedAt time.Time) error
	TouchSession(ctx context.Context, sessionID string) error
	DeleteSession(ctx context.Context, sessionID string) error
	SaveOAuthState(ctx context.Context, state model.OAuthState) error
	GetOAuthState(ctx context.Context, stateID string) (model.OAuthState, error)
	ConsumeOAuthState(ctx context.Context, stateID string) (model.OAuthState, error)
	DeleteOAuthState(ctx context.Context, stateID string) error
	GetUserControlByUserID(ctx context.Context, userID int64) (model.UserControl, error)
	UpsertUserControl(ctx context.Context, input UpsertUserControlInput) (model.UserControl, error)
	GetAdminPasswordAttempt(ctx context.Context, bucketType string, bucketKey string) (model.AdminPasswordAttempt, error)
	RegisterAdminPasswordFailure(ctx context.Context, bucketType string, bucketKey string, maxFailures int, blockDuration time.Duration, now time.Time) (model.AdminPasswordAttempt, error)
	DeleteAdminPasswordAttempt(ctx context.Context, bucketType string, bucketKey string) error
	DeleteStaleAdminPasswordAttempts(ctx context.Context, cutoff time.Time, now time.Time) error
	ListAdminUsers(ctx context.Context) ([]model.AdminUserSummary, error)
	ListManagedDomains(ctx context.Context, includeDisabled bool) ([]model.ManagedDomain, error)
	GetManagedDomainByID(ctx context.Context, id int64) (model.ManagedDomain, error)
	GetManagedDomainByRoot(ctx context.Context, rootDomain string) (model.ManagedDomain, error)
	UpsertManagedDomain(ctx context.Context, input UpsertManagedDomainInput) (model.ManagedDomain, error)
	SetUserQuota(ctx context.Context, input SetUserQuotaInput) (model.UserDomainQuota, error)
	GetEffectiveQuota(ctx context.Context, userID int64, managedDomainID int64) (int, error)
	CountAllocationsByUserAndDomain(ctx context.Context, userID int64, managedDomainID int64) (int, error)
	FindAllocationByNormalizedPrefix(ctx context.Context, managedDomainID int64, normalizedPrefix string) (model.Allocation, error)
	CreateAllocation(ctx context.Context, input CreateAllocationInput) (model.Allocation, error)
	UpdateAllocation(ctx context.Context, input UpdateAllocationInput) (model.Allocation, error)
	ListAllocationsByUser(ctx context.Context, userID int64) ([]model.Allocation, error)
	ListAdminAllocations(ctx context.Context) ([]model.AdminAllocationSummary, error)
	ListPublicAllocationOwnerships(ctx context.Context) ([]model.PublicAllocationOwnership, error)
	GetAllocationByID(ctx context.Context, allocationID int64) (model.Allocation, error)
	GetAllocationByIDForUser(ctx context.Context, allocationID int64, userID int64) (model.Allocation, error)
	ListEmailRoutes(ctx context.Context) ([]model.EmailRoute, error)
	ListEmailRoutesByOwner(ctx context.Context, ownerUserID int64) ([]model.EmailRoute, error)
	GetEmailRouteByAddress(ctx context.Context, rootDomain string, prefix string) (model.EmailRoute, error)
	CreateEmailRoute(ctx context.Context, input CreateEmailRouteInput) (model.EmailRoute, error)
	UpsertEmailRouteByAddress(ctx context.Context, input UpsertEmailRouteByAddressInput) (model.EmailRoute, error)
	UpdateEmailRoute(ctx context.Context, input UpdateEmailRouteInput) (model.EmailRoute, error)
	DeleteEmailRoute(ctx context.Context, id int64) error
	ListAPITokensByOwner(ctx context.Context, ownerUserID int64) ([]model.APIToken, error)
	GetAPITokenByPublicID(ctx context.Context, publicID string) (model.APIToken, error)
	GetAPITokenByTokenHash(ctx context.Context, tokenHash string) (model.APIToken, error)
	CreateAPIToken(ctx context.Context, input CreateAPITokenInput) (model.APIToken, error)
	UpdateAPIToken(ctx context.Context, input UpdateAPITokenInput) (model.APIToken, error)
	ListEmailTargetsByOwner(ctx context.Context, ownerUserID int64) ([]model.EmailTarget, error)
	GetEmailTargetByEmail(ctx context.Context, email string) (model.EmailTarget, error)
	GetEmailTargetByVerificationTokenHash(ctx context.Context, tokenHash string) (model.EmailTarget, error)
	CreateEmailTarget(ctx context.Context, input CreateEmailTargetInput) (model.EmailTarget, error)
	UpdateEmailTarget(ctx context.Context, input UpdateEmailTargetInput) (model.EmailTarget, error)
	ListAdminApplications(ctx context.Context) ([]model.AdminApplication, error)
	ListAdminApplicationsByApplicant(ctx context.Context, applicantUserID int64) ([]model.AdminApplication, error)
	UpsertAdminApplication(ctx context.Context, input UpsertAdminApplicationInput) (model.AdminApplication, error)
	UpdateAdminApplication(ctx context.Context, input UpdateAdminApplicationInput) (model.AdminApplication, error)
	ListPermissionPolicies(ctx context.Context) ([]model.PermissionPolicy, error)
	GetPermissionPolicy(ctx context.Context, key string) (model.PermissionPolicy, error)
	UpsertPermissionPolicy(ctx context.Context, input UpsertPermissionPolicyInput) (model.PermissionPolicy, error)
	ListPaymentProducts(ctx context.Context, includeDisabled bool) ([]model.PaymentProduct, error)
	GetPaymentProduct(ctx context.Context, key string) (model.PaymentProduct, error)
	UpsertPaymentProduct(ctx context.Context, input UpsertPaymentProductInput) (model.PaymentProduct, error)
	CreatePaymentOrder(ctx context.Context, input CreatePaymentOrderInput) (model.PaymentOrder, error)
	ListPaymentOrders(ctx context.Context, limit int) ([]model.PaymentOrder, error)
	ListPaymentOrdersByUser(ctx context.Context, userID int64, limit int) ([]model.PaymentOrder, error)
	GetPaymentOrderByOutTradeNo(ctx context.Context, outTradeNo string) (model.PaymentOrder, error)
	UpdatePaymentOrderGatewayState(ctx context.Context, input UpdatePaymentOrderGatewayStateInput) (model.PaymentOrder, error)
	ApplyPaymentOrderEntitlement(ctx context.Context, input ApplyPaymentOrderEntitlementInput) (model.PaymentOrder, bool, error)
	GetEmailCatchAllAccessByUser(ctx context.Context, userID int64) (model.EmailCatchAllAccess, error)
	UpsertEmailCatchAllAccess(ctx context.Context, input UpsertEmailCatchAllAccessInput) (model.EmailCatchAllAccess, error)
	GetEmailCatchAllDailyUsage(ctx context.Context, userID int64, usageDate string) (model.EmailCatchAllDailyUsage, error)
	ConsumeEmailCatchAll(ctx context.Context, input ConsumeEmailCatchAllInput) (model.EmailCatchAllConsumeResult, error)
	RefundEmailCatchAll(ctx context.Context, input RefundEmailCatchAllInput) error
	EnqueueMailDeliveryBatch(ctx context.Context, input EnqueueMailDeliveryBatchInput) ([]model.MailDeliveryJob, error)
	ClaimMailDeliveryJobs(ctx context.Context, input ClaimMailDeliveryJobsInput) ([]model.MailDeliveryJob, error)
	MarkMailDeliveryJobDelivered(ctx context.Context, input MarkMailDeliveryJobDeliveredInput) (model.MailDeliveryJob, error)
	MarkMailDeliveryJobRetry(ctx context.Context, input MarkMailDeliveryJobRetryInput) (model.MailDeliveryJob, error)
	MarkMailDeliveryJobFailed(ctx context.Context, input MarkMailDeliveryJobFailedInput) (model.MailDeliveryJob, error)
	CleanupMailDeliveryJobs(ctx context.Context, input CleanupMailDeliveryJobsInput) (int64, error)
	GetActivePOWChallengeByUser(ctx context.Context, userID int64) (model.POWChallenge, error)
	CreateOrReplacePOWChallenge(ctx context.Context, input CreateOrReplacePOWChallengeInput) (model.POWChallenge, error)
	CountClaimedPOWChallengesByUser(ctx context.Context, userID int64, start time.Time, end time.Time) (int, error)
	ClaimPOWChallengeReward(ctx context.Context, input ClaimPOWChallengeRewardInput) (model.POWChallenge, model.EmailCatchAllAccess, error)
	GetPOWGlobalSettings(ctx context.Context) (model.POWGlobalSettings, error)
	UpsertPOWGlobalSettings(ctx context.Context, input UpsertPOWGlobalSettingsInput) (model.POWGlobalSettings, error)
	ListPOWBenefitSettings(ctx context.Context) ([]model.POWBenefitSettings, error)
	UpsertPOWBenefitSettings(ctx context.Context, input UpsertPOWBenefitSettingsInput) (model.POWBenefitSettings, error)
	ListPOWDifficultySettings(ctx context.Context) ([]model.POWDifficultySettings, error)
	UpsertPOWDifficultySettings(ctx context.Context, input UpsertPOWDifficultySettingsInput) (model.POWDifficultySettings, error)
	GetPOWUserSettings(ctx context.Context, userID int64) (model.POWUserSettings, error)
	UpsertPOWUserSettings(ctx context.Context, input UpsertPOWUserSettingsInput) (model.POWUserSettings, error)
	ListRedeemCodes(ctx context.Context) ([]model.RedeemCode, error)
	CreateRedeemCode(ctx context.Context, input CreateRedeemCodeInput) (model.RedeemCode, error)
	DeleteRedeemCode(ctx context.Context, id int64) error
	ListQuantityRecordsByUser(ctx context.Context, userID int64) ([]model.QuantityRecord, error)
	ListQuantityBalancesByUser(ctx context.Context, userID int64, now time.Time) ([]model.QuantityBalance, error)
	CreateQuantityRecord(ctx context.Context, input CreateQuantityRecordInput) (model.QuantityRecord, error)
	WriteAuditLog(ctx context.Context, input AuditLogInput) error
}

// Backend describes one concrete storage backend that can be opened, migrated,
// and then used through the Store interface.
type Backend interface {
	Store
	Close() error
	Migrate(ctx context.Context) error
}
