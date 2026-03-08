package service

import (
	"context"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/linuxdo"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

// Store abstracts the persistence capabilities used by the service layer.
// The interface keeps service code testable and decoupled from the concrete SQLite implementation.
type Store interface {
	UpsertUser(ctx context.Context, input sqlite.UpsertUserInput) (model.User, error)
	GetUserByID(ctx context.Context, userID int64) (model.User, error)
	GetUserByUsername(ctx context.Context, username string) (model.User, error)
	CreateSession(ctx context.Context, input sqlite.CreateSessionInput) (model.Session, error)
	GetSessionWithUserByID(ctx context.Context, sessionID string) (model.Session, model.User, error)
	MarkSessionAdminVerified(ctx context.Context, sessionID string, verifiedAt time.Time) error
	TouchSession(ctx context.Context, sessionID string) error
	DeleteSession(ctx context.Context, sessionID string) error
	SaveOAuthState(ctx context.Context, state model.OAuthState) error
	ConsumeOAuthState(ctx context.Context, stateID string) (model.OAuthState, error)
	GetUserControlByUserID(ctx context.Context, userID int64) (model.UserControl, error)
	UpsertUserControl(ctx context.Context, input sqlite.UpsertUserControlInput) (model.UserControl, error)
	ListAdminUsers(ctx context.Context) ([]model.AdminUserSummary, error)
	ListManagedDomains(ctx context.Context, includeDisabled bool) ([]model.ManagedDomain, error)
	GetManagedDomainByID(ctx context.Context, id int64) (model.ManagedDomain, error)
	GetManagedDomainByRoot(ctx context.Context, rootDomain string) (model.ManagedDomain, error)
	UpsertManagedDomain(ctx context.Context, input sqlite.UpsertManagedDomainInput) (model.ManagedDomain, error)
	SetUserQuota(ctx context.Context, input sqlite.SetUserQuotaInput) (model.UserDomainQuota, error)
	GetEffectiveQuota(ctx context.Context, userID int64, managedDomainID int64) (int, error)
	CountAllocationsByUserAndDomain(ctx context.Context, userID int64, managedDomainID int64) (int, error)
	FindAllocationByNormalizedPrefix(ctx context.Context, managedDomainID int64, normalizedPrefix string) (model.Allocation, error)
	CreateAllocation(ctx context.Context, input sqlite.CreateAllocationInput) (model.Allocation, error)
	ListAllocationsByUser(ctx context.Context, userID int64) ([]model.Allocation, error)
	ListAdminAllocations(ctx context.Context) ([]model.AdminAllocationSummary, error)
	ListPublicAllocationOwnerships(ctx context.Context) ([]model.PublicAllocationOwnership, error)
	GetAllocationByID(ctx context.Context, allocationID int64) (model.Allocation, error)
	GetAllocationByIDForUser(ctx context.Context, allocationID int64, userID int64) (model.Allocation, error)
	ListEmailRoutes(ctx context.Context) ([]model.EmailRoute, error)
	CreateEmailRoute(ctx context.Context, input sqlite.CreateEmailRouteInput) (model.EmailRoute, error)
	UpdateEmailRoute(ctx context.Context, input sqlite.UpdateEmailRouteInput) (model.EmailRoute, error)
	DeleteEmailRoute(ctx context.Context, id int64) error
	ListAdminApplications(ctx context.Context) ([]model.AdminApplication, error)
	UpdateAdminApplication(ctx context.Context, input sqlite.UpdateAdminApplicationInput) (model.AdminApplication, error)
	ListRedeemCodes(ctx context.Context) ([]model.RedeemCode, error)
	CreateRedeemCode(ctx context.Context, input sqlite.CreateRedeemCodeInput) (model.RedeemCode, error)
	DeleteRedeemCode(ctx context.Context, id int64) error
	WriteAuditLog(ctx context.Context, input sqlite.AuditLogInput) error
}

// OAuthClient abstracts Linux Do OAuth operations.
type OAuthClient interface {
	Configured() bool
	BuildAuthorizationURL(state string, codeChallenge string) string
	ExchangeCode(ctx context.Context, code string, codeVerifier string) (linuxdo.TokenResponse, error)
	GetCurrentUser(ctx context.Context, accessToken string) (model.LinuxDOProfile, error)
}

// CloudflareClient abstracts Cloudflare DNS operations.
type CloudflareClient interface {
	ResolveZoneID(ctx context.Context, rootDomain string) (string, error)
	ListAllDNSRecords(ctx context.Context, zoneID string) ([]cloudflare.DNSRecord, error)
	GetDNSRecord(ctx context.Context, zoneID string, recordID string) (cloudflare.DNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneID string, input cloudflare.CreateDNSRecordInput) (cloudflare.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, input cloudflare.UpdateDNSRecordInput) (cloudflare.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error
}
