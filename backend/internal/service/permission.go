package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

const (
	// PermissionKeyEmailCatchAll is the stable identifier used across the
	// database, HTTP API, and frontends for the catch-all mailbox permission.
	PermissionKeyEmailCatchAll = "email_catch_all"

	// UserEmailRouteKindDefault marks the per-user default mailbox that always
	// maps to <username>@linuxdo.space (or the configured default email root).
	UserEmailRouteKindDefault = "default"

	// UserEmailRouteKindCustom marks one extra mailbox alias already assigned to
	// the current user and loaded from the database.
	UserEmailRouteKindCustom = "custom"

	// UserEmailRouteKindCatchAll marks the permission-gated catch-all mailbox
	// that routes *@<username>.<root> to one target inbox.
	UserEmailRouteKindCatchAll = "catch_all"

	// emailCatchAllPrefix is the stable database key used to store the
	// permission-gated catch-all route without requiring a schema migration. The
	// public address is no longer exposed as catch-all@... and instead renders as
	// *@<username>.<root>.
	emailCatchAllPrefix = "catch-all"

	// QuantityResourceEmailCatchAllSubscriptionDays records administrator grants
	// that extend the paid catch-all subscription window.
	QuantityResourceEmailCatchAllSubscriptionDays = "email_catch_all_subscription_days"

	// QuantityResourceEmailCatchAllRemainingCount records administrator grants or
	// deductions applied to the mutable prepaid remaining-count pool.
	QuantityResourceEmailCatchAllRemainingCount = "email_catch_all_remaining_count"
)

// EmailCatchAllPledgeText is the canonical server-side pledge text recorded on
// every application. The backend stores this value directly so the audit trail
// does not depend on client-side wording.
const EmailCatchAllPledgeText = "我承诺仅将此邮箱泛解析权限用于合法、正当且合理的用途，不实施违法违纪行为，不滥用平台资源；如因本人使用导致任何后果，均由本人自行承担，与开发者无关；若因此获得收益，我也愿意无私反馈 Linux Do 社区。"

// PermissionService owns user-facing permission application flows together with
// the administrator-configurable policy rules that govern those flows.
type PermissionService struct {
	cfg                      config.Config
	db                       Store
	cf                       CloudflareClient
	targetVerificationMailer EmailTargetVerificationMailer

	// emailTargetCreateLocks serializes create-or-bind operations per concrete
	// target email so one process does not race local ownership binding and
	// verification-token issuance for the same mailbox across rapid retries,
	// duplicate clicks, or two tabs.
	emailTargetCreateLocks sync.Map

	// emailTargetResendLocks serializes resend-verification operations per local
	// target row so one process cannot concurrently mint multiple fresh
	// verification tokens for the same mailbox.
	emailTargetResendLocks sync.Map
}

const (
	emailCatchAllPermissionDisplayName = "邮箱泛解析"
	emailCatchAllPermissionDescription = "为与你用户名同名的默认二级域名开启整个邮件命名空间的泛解析转发。"
	emailCatchAllPledgeTextClean       = "我承诺仅将此邮箱泛解析权限用于合法、正当且合理的用途，不实施违法违纪行为，不滥用平台资源；如因本人使用导致任何后果，均由本人自行承担，与开发者无关；若因此获得收益，我也愿意无私反馈 Linux Do 社区。"
	defaultEmailRouteDisplayName       = "默认邮箱"
	defaultEmailRouteDescription       = "每位用户默认拥有一个与 Linux Do 用户名同名的邮箱转发地址。"
	customEmailRouteDisplayName        = "附加邮箱"
	customEmailRouteDescription        = "这是已经分配到你名下的额外邮箱地址，当前页面先以只读方式展示。"
	catchAllEmailRouteDisplayName      = "邮箱泛解析"
	catchAllEmailRouteDescription      = "用于接收 *@<username>.linuxdo.space 的整段邮件泛解析转发。"
)

// PermissionApplicationSummary is the normalized subset of one application row
// exposed to the user-facing frontend.
type PermissionApplicationSummary struct {
	ID         int64      `json:"id"`
	Target     string     `json:"target,omitempty"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason"`
	ReviewNote string     `json:"review_note"`
	ReviewedAt *time.Time `json:"reviewed_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

// UserPermissionView describes the current visible state of one user-facing
// permission, including the active policy gate and the latest application state.
type UserPermissionView struct {
	Key                string                        `json:"key"`
	DisplayName        string                        `json:"display_name"`
	Description        string                        `json:"description"`
	Target             string                        `json:"target"`
	PledgeText         string                        `json:"pledge_text"`
	PolicyEnabled      bool                          `json:"policy_enabled"`
	AutoApprove        bool                          `json:"auto_approve"`
	MinTrustLevel      int                           `json:"min_trust_level"`
	Eligible           bool                          `json:"eligible"`
	EligibilityReasons []string                      `json:"eligibility_reasons"`
	Status             string                        `json:"status"`
	CanApply           bool                          `json:"can_apply"`
	CanManageRoute     bool                          `json:"can_manage_route"`
	CatchAllAccess     *EmailCatchAllAccessView      `json:"catch_all_access,omitempty"`
	Application        *PermissionApplicationSummary `json:"application,omitempty"`
}

// EmailCatchAllAccessView describes the currently effective runtime allowance
// for one catch-all mailbox, independent of the immutable quantity ledger.
type EmailCatchAllAccessView struct {
	AccessMode               string     `json:"access_mode"`
	SubscriptionActive       bool       `json:"subscription_active"`
	SubscriptionExpiresAt    *time.Time `json:"subscription_expires_at,omitempty"`
	RemainingCount           int64      `json:"remaining_count"`
	PermanentRemainingCount  int64      `json:"permanent_remaining_count"`
	TemporaryRewardCount     int64      `json:"temporary_reward_count"`
	TemporaryRewardExpiresAt *time.Time `json:"temporary_reward_expires_at,omitempty"`
	DailyUsageDate           string     `json:"daily_usage_date"`
	DailyUsedCount           int64      `json:"daily_used_count"`
	DailyRemainingCount      int64      `json:"daily_remaining_count"`
	EffectiveDailyLimit      int64      `json:"effective_daily_limit"`
	HasAccess                bool       `json:"has_access"`
	DeliveryAvailable        bool       `json:"delivery_available"`
}

// UserEmailRouteView describes one user-visible email forwarding row shown on
// the public email page.
type UserEmailRouteView struct {
	ID                  int64                    `json:"id,omitempty"`
	Kind                string                   `json:"kind"`
	PermissionKey       string                   `json:"permission_key,omitempty"`
	DisplayName         string                   `json:"display_name"`
	Description         string                   `json:"description"`
	Address             string                   `json:"address"`
	Prefix              string                   `json:"prefix"`
	RootDomain          string                   `json:"root_domain"`
	TargetType          string                   `json:"target_type"`
	TargetEmail         string                   `json:"target_email"`
	TargetTokenPublicID string                   `json:"target_token_public_id,omitempty"`
	TargetTokenName     string                   `json:"target_token_name,omitempty"`
	TargetDisplay       string                   `json:"target_display"`
	Enabled             bool                     `json:"enabled"`
	Configured          bool                     `json:"configured"`
	PermissionStatus    string                   `json:"permission_status,omitempty"`
	CatchAllAccess      *EmailCatchAllAccessView `json:"catch_all_access,omitempty"`
	CanManage           bool                     `json:"can_manage"`
	CanDelete           bool                     `json:"can_delete"`
	UpdatedAt           *time.Time               `json:"updated_at,omitempty"`
}

// EmailRouteAvailabilityResult mirrors the public email-prefix search result
// rendered by the frontend search box.
type EmailRouteAvailabilityResult struct {
	RootDomain       string   `json:"root_domain"`
	Prefix           string   `json:"prefix"`
	NormalizedPrefix string   `json:"normalized_prefix"`
	Address          string   `json:"address"`
	Available        bool     `json:"available"`
	Reasons          []string `json:"reasons"`
}

// SubmitPermissionApplicationRequest describes the single user-side mutation
// currently supported by the permissions page.
type SubmitPermissionApplicationRequest struct {
	Key string `json:"key"`
}

// UpsertMyDefaultEmailRouteRequest describes the forwarding target saved for
// the always-owned default mailbox <username>@linuxdo.space.
type UpsertMyDefaultEmailRouteRequest struct {
	TargetType          string `json:"target_type"`
	TargetEmail         string `json:"target_email"`
	TargetTokenPublicID string `json:"target_token_public_id"`
	Enabled             bool   `json:"enabled"`
}

// UpsertMyCatchAllEmailRouteRequest describes the forwarding target configured
// by the user after the catch-all mailbox permission has been approved.
type UpsertMyCatchAllEmailRouteRequest struct {
	TargetType          string `json:"target_type"`
	TargetEmail         string `json:"target_email"`
	TargetTokenPublicID string `json:"target_token_public_id"`
	Enabled             bool   `json:"enabled"`
}

// UpdatePermissionPolicyRequest describes the administrator-editable subset of
// one permission policy row. Pointer fields let PATCH keep existing values.
type UpdatePermissionPolicyRequest struct {
	Enabled           *bool  `json:"enabled,omitempty"`
	AutoApprove       *bool  `json:"auto_approve,omitempty"`
	MinTrustLevel     *int   `json:"min_trust_level,omitempty"`
	DefaultDailyLimit *int64 `json:"default_daily_limit,omitempty"`
}

// AdminSetUserPermissionRequest describes one administrator-authored permission
// decision written directly against a target user.
type AdminSetUserPermissionRequest struct {
	Status     string `json:"status"`
	ReviewNote string `json:"review_note"`
	Reason     string `json:"reason"`
}

// AdminUpdateEmailCatchAllAccessRequest describes one administrator-managed
// adjustment to the mutable catch-all entitlement state.
type AdminUpdateEmailCatchAllAccessRequest struct {
	AddSubscriptionDays     int    `json:"add_subscription_days"`
	ClearSubscription       bool   `json:"clear_subscription"`
	RemainingCountDelta     int64  `json:"remaining_count_delta"`
	DailyLimitOverride      *int64 `json:"daily_limit_override,omitempty"`
	ClearDailyLimitOverride bool   `json:"clear_daily_limit_override"`
	Reason                  string `json:"reason"`
}

// catchAllNamespace describes the routed namespace derived from the current
// user account and the configured default root domain.
type catchAllNamespace struct {
	RootDomain         string
	Address            string
	HasOwnedAllocation bool
}

// NewPermissionService constructs the service instance responsible for the new
// user-side permission and email-routing flows.
func NewPermissionService(cfg config.Config, db Store, cf CloudflareClient) *PermissionService {
	return &PermissionService{
		cfg:                      cfg,
		db:                       db,
		cf:                       cf,
		targetVerificationMailer: newEmailTargetVerificationMailer(cfg),
	}
}

// lockEmailTargetResend acquires the in-process mutex protecting one concrete
// email-target resend flow and returns the matching unlock function.
func (s *PermissionService) lockEmailTargetResend(targetID int64) func() {
	lockValue, _ := s.emailTargetResendLocks.LoadOrStore(targetID, &sync.Mutex{})
	locker := lockValue.(*sync.Mutex)
	locker.Lock()
	return locker.Unlock
}

// lockEmailTargetCreate acquires the in-process mutex protecting one concrete
// target-email bind/create flow and returns the matching unlock function.
func (s *PermissionService) lockEmailTargetCreate(targetEmail string) func() {
	lockKey := strings.ToLower(strings.TrimSpace(targetEmail))
	lockValue, _ := s.emailTargetCreateLocks.LoadOrStore(lockKey, &sync.Mutex{})
	locker := lockValue.(*sync.Mutex)
	locker.Lock()
	return locker.Unlock
}

// ListMyPermissions returns the single currently supported permission card for
// the authenticated user.
func (s *PermissionService) ListMyPermissions(ctx context.Context, user model.User) ([]UserPermissionView, error) {
	item, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return nil, err
	}
	return []UserPermissionView{item}, nil
}

// SubmitPermissionApplication stores or refreshes the user's latest permission
// application and automatically approves it when the current policy allows that.
func (s *PermissionService) SubmitPermissionApplication(ctx context.Context, user model.User, request SubmitPermissionApplicationRequest) (UserPermissionView, error) {
	if strings.TrimSpace(request.Key) != PermissionKeyEmailCatchAll {
		return UserPermissionView{}, ValidationError("unsupported permission key")
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}
	if !permission.Eligible {
		return UserPermissionView{}, ForbiddenError(firstNonEmpty(permission.EligibilityReasons...))
	}

	switch permission.Status {
	case "approved":
		return UserPermissionView{}, ConflictError("the catch-all email permission has already been granted")
	case "pending":
		return UserPermissionView{}, ConflictError("the catch-all email permission application is already pending review")
	}

	status := "pending"
	if permission.AutoApprove {
		status = "approved"
	}

	applicationTarget := permission.Target
	if permission.Application != nil && strings.TrimSpace(permission.Application.Target) != "" {
		applicationTarget = permission.Application.Target
	}
	item, err := s.db.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          applicationTarget,
		Reason:          emailCatchAllPledgeTextClean,
		Status:          status,
	})
	if err != nil {
		return UserPermissionView{}, InternalError("failed to submit permission application", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"application_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"target":         item.Target,
		"status":         item.Status,
	})
	logAuditWriteFailure("permission.application.submit", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "permission.application.submit",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return s.loadEmailCatchAllPermission(ctx, user)
}

// ListMyEmailRoutes returns the full set of user-visible mailbox rows rendered
// by the public email page: the default mailbox, any extra stored aliases, and
// the permission-gated catch-all mailbox.
func (s *PermissionService) ListMyEmailRoutes(ctx context.Context, user model.User) ([]UserEmailRouteView, error) {
	defaultRoute, err := s.buildDefaultEmailRouteView(ctx, user)
	if err != nil {
		return nil, err
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return nil, err
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return nil, err
	}

	catchAllRoute, err := s.buildCatchAllEmailRouteView(ctx, user, permission, namespace)
	if err != nil {
		return nil, err
	}

	persistedRoutes, err := s.db.ListEmailRoutesByOwner(ctx, user.ID)
	if err != nil {
		return nil, InternalError("failed to load user email routes", err)
	}

	items := make([]UserEmailRouteView, 0, len(persistedRoutes)+2)
	items = append(items, defaultRoute)
	for _, route := range persistedRoutes {
		if route.Prefix == defaultRoute.Prefix && strings.EqualFold(route.RootDomain, defaultRoute.RootDomain) {
			continue
		}
		if route.Prefix == emailCatchAllPrefix && strings.EqualFold(route.RootDomain, namespace.RootDomain) {
			continue
		}
		items = append(items, buildCustomEmailRouteView(ctx, s.db, route))
	}
	items = append(items, catchAllRoute)
	return items, nil
}

// UpsertMyCatchAllEmailRoute creates or updates the user's forwarding target
// after the catch-all permission has been approved.
func (s *PermissionService) UpsertMyCatchAllEmailRoute(ctx context.Context, user model.User, request UpsertMyCatchAllEmailRouteRequest) (UserEmailRouteView, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}
	if permission.Status != "approved" {
		return UserEmailRouteView{}, ForbiddenError("the catch-all email permission has not been approved")
	}

	target, err := s.resolveOwnedRouteTarget(
		ctx,
		user,
		request.TargetType,
		request.TargetEmail,
		request.TargetTokenPublicID,
		true,
	)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}
	if target.Configured {
		if err := ensureDatabaseRelayIngressDNSForRootDomain(ctx, s.cfg, s.cf, namespace.RootDomain); err != nil {
			return UserEmailRouteView{}, err
		}
	}

	beforeState := newDeletedCatchAllEmailRouteSyncState(namespace.RootDomain)
	var existingRoute *model.EmailRoute
	storedRoute, routeErr := s.db.GetEmailRouteByAddress(ctx, namespace.RootDomain, emailCatchAllPrefix)
	switch {
	case routeErr == nil:
		if storedRoute.OwnerUserID != user.ID {
			return UserEmailRouteView{}, UnavailableError("catch-all mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", storedRoute.ID, storedRoute.OwnerUserID))
		}
		beforeState = newCatchAllEmailRouteSyncState(storedRoute.RootDomain, storedRoute.TargetEmail, storedRoute.Enabled)
		existingRoute = &storedRoute
	case storage.IsNotFound(routeErr):
		snapshot, snapshotErr := s.lookupCloudflareCatchAllSnapshot(ctx, namespace.RootDomain)
		if snapshotErr != nil {
			return UserEmailRouteView{}, snapshotErr
		}
		if snapshot.Found {
			beforeState = newCatchAllEmailRouteSyncState(namespace.RootDomain, snapshot.TargetEmail, snapshot.Enabled)
		}
	default:
		return UserEmailRouteView{}, InternalError("failed to load catch-all email route before update", routeErr)
	}

	if !target.Configured {
		if existingRoute != nil || beforeState.Exists {
			if err := routing.SyncForwardingState(ctx, beforeState, newDeletedCatchAllEmailRouteSyncState(namespace.RootDomain), func() error {
				if existingRoute == nil {
					return nil
				}
				if deleteErr := s.db.DeleteEmailRoute(ctx, existingRoute.ID); deleteErr != nil {
					return InternalError("failed to clear catch-all email route", deleteErr)
				}
				return nil
			}); err != nil {
				return UserEmailRouteView{}, err
			}

			if existingRoute != nil {
				metadata, _ := json.Marshal(map[string]any{
					"email_route_id": existingRoute.ID,
					"permission_key": PermissionKeyEmailCatchAll,
					"address":        namespace.Address,
				})
				logAuditWriteFailure("email_route.catch_all.clear", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
					ActorUserID:  &user.ID,
					Action:       "email_route.catch_all.clear",
					ResourceType: "email_route",
					ResourceID:   strconv.FormatInt(existingRoute.ID, 10),
					MetadataJSON: string(metadata),
				}))
			}
		}

		return s.buildCatchAllEmailRouteView(ctx, user, permission, namespace)
	}

	desiredState := newCatchAllEmailRouteSyncState(namespace.RootDomain, target.TargetEmail, request.Enabled)
	var item model.EmailRoute
	if err := routing.SyncForwardingState(ctx, beforeState, desiredState, func() error {
		var persistErr error
		item, persistErr = s.db.UpsertEmailRouteByAddress(ctx, storage.UpsertEmailRouteByAddressInput{
			OwnerUserID:         user.ID,
			RootDomain:          namespace.RootDomain,
			Prefix:              emailCatchAllPrefix,
			TargetEmail:         target.TargetEmail,
			TargetKind:          target.TargetType,
			TargetTokenPublicID: target.TargetTokenPublicID,
			Enabled:             request.Enabled,
		})
		if persistErr != nil {
			return InternalError("failed to save catch-all email route", persistErr)
		}
		return nil
	}); err != nil {
		return UserEmailRouteView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"address":        namespace.Address,
	})
	logAuditWriteFailure("email_route.catch_all.upsert", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_route.catch_all.upsert",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	updatedAt := item.UpdatedAt
	return normalizeUserEmailRouteCopy(UserEmailRouteView{
		ID:                  item.ID,
		Kind:                UserEmailRouteKindCatchAll,
		PermissionKey:       PermissionKeyEmailCatchAll,
		DisplayName:         catchAllEmailRouteDisplayName,
		Description:         catchAllEmailRouteDescription,
		Address:             namespace.Address,
		Prefix:              "*",
		RootDomain:          namespace.RootDomain,
		TargetType:          normalizeRouteTargetType(item.TargetKind, item.TargetTokenPublicID),
		TargetEmail:         item.TargetEmail,
		TargetTokenPublicID: item.TargetTokenPublicID,
		TargetTokenName:     target.TargetTokenName,
		TargetDisplay:       target.TargetDisplay,
		Enabled:             item.Enabled,
		Configured:          target.Configured,
		PermissionStatus:    permission.Status,
		CatchAllAccess:      permission.CatchAllAccess,
		CanManage:           true,
		CanDelete:           false,
		UpdatedAt:           &updatedAt,
	}), nil
}

// ListPermissionPolicies returns the administrator-visible configuration rows
// that decide who can request each supported permission.
func (s *PermissionService) ListPermissionPolicies(ctx context.Context) ([]model.PermissionPolicy, error) {
	items, err := s.db.ListPermissionPolicies(ctx)
	if err != nil {
		return nil, InternalError("failed to list permission policies", err)
	}
	return items, nil
}

// UpdatePermissionPolicy updates the mutable gates that control one permission.
func (s *PermissionService) UpdatePermissionPolicy(ctx context.Context, actor model.User, key string, request UpdatePermissionPolicyRequest) (model.PermissionPolicy, error) {
	item, err := s.db.GetPermissionPolicy(ctx, strings.TrimSpace(key))
	if err != nil {
		if storage.IsNotFound(err) {
			return model.PermissionPolicy{}, NotFoundError("permission policy not found")
		}
		return model.PermissionPolicy{}, InternalError("failed to load permission policy", err)
	}

	if request.Enabled != nil {
		item.Enabled = *request.Enabled
	}
	if request.AutoApprove != nil {
		item.AutoApprove = *request.AutoApprove
	}
	if request.MinTrustLevel != nil {
		if *request.MinTrustLevel < 0 || *request.MinTrustLevel > 4 {
			return model.PermissionPolicy{}, ValidationError("min_trust_level must be between 0 and 4")
		}
		item.MinTrustLevel = *request.MinTrustLevel
	}
	if request.DefaultDailyLimit != nil {
		if *request.DefaultDailyLimit <= 0 {
			return model.PermissionPolicy{}, ValidationError("default_daily_limit must be greater than 0")
		}
		item.DefaultDailyLimit = *request.DefaultDailyLimit
	}

	updated, err := s.db.UpsertPermissionPolicy(ctx, storage.UpsertPermissionPolicyInput{
		Key:               item.Key,
		DisplayName:       item.DisplayName,
		Description:       item.Description,
		Enabled:           item.Enabled,
		AutoApprove:       item.AutoApprove,
		MinTrustLevel:     item.MinTrustLevel,
		DefaultDailyLimit: item.DefaultDailyLimit,
	})
	if err != nil {
		return model.PermissionPolicy{}, InternalError("failed to update permission policy", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"policy_key":          updated.Key,
		"enabled":             updated.Enabled,
		"auto_approve":        updated.AutoApprove,
		"min_trust_level":     updated.MinTrustLevel,
		"default_daily_limit": updated.DefaultDailyLimit,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.permission_policy.update",
		ResourceType: "permission_policy",
		ResourceID:   updated.Key,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.PermissionPolicy{}, InternalError("failed to write permission policy audit log", err)
	}
	return updated, nil
}

// ListPermissionsForUser returns the current permission card set for one target
// user so administrators can inspect and control it from the user editor.
func (s *PermissionService) ListPermissionsForUser(ctx context.Context, userID int64) ([]UserPermissionView, error) {
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil, NotFoundError("target user not found")
		}
		return nil, InternalError("failed to load target user", err)
	}
	return s.ListMyPermissions(ctx, user)
}

// SetPermissionForUser lets an administrator directly create or override one
// permission state for a target user, even without a prior user-side request.
func (s *PermissionService) SetPermissionForUser(ctx context.Context, actor model.User, userID int64, permissionKey string, request AdminSetUserPermissionRequest) (UserPermissionView, error) {
	if strings.TrimSpace(permissionKey) != PermissionKeyEmailCatchAll {
		return UserPermissionView{}, ValidationError("unsupported permission key")
	}

	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return UserPermissionView{}, NotFoundError("target user not found")
		}
		return UserPermissionView{}, InternalError("failed to load target user", err)
	}

	status := normalizeAdminApplicationStatus(request.Status)
	if status == "" {
		return UserPermissionView{}, ValidationError("status must be pending, approved, or rejected")
	}

	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		if permission.Application != nil && strings.TrimSpace(permission.Application.Reason) != "" {
			reason = permission.Application.Reason
		} else {
			reason = "管理员手动设置该权限状态。"
		}
	}

	now := time.Now().UTC()
	nextApplication := model.AdminApplication{
		ID:     0,
		Type:   PermissionKeyEmailCatchAll,
		Target: permission.Target,
		Status: status,
	}
	if permission.Application != nil {
		nextApplication.ID = permission.Application.ID
		if strings.TrimSpace(permission.Application.Target) != "" {
			nextApplication.Target = permission.Application.Target
		}
	}
	if err := s.disableCatchAllEmailRouteForApplication(ctx, actor, nextApplication); err != nil {
		return UserPermissionView{}, err
	}

	applicationTarget := permission.Target
	if permission.Application != nil && strings.TrimSpace(permission.Application.Target) != "" {
		applicationTarget = permission.Application.Target
	}

	item, err := s.db.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
		ApplicantUserID:  user.ID,
		Type:             PermissionKeyEmailCatchAll,
		Target:           applicationTarget,
		Reason:           reason,
		Status:           status,
		ReviewNote:       strings.TrimSpace(request.ReviewNote),
		ReviewedByUserID: &actor.ID,
		ReviewedAt:       &now,
	})
	if err != nil {
		return UserPermissionView{}, InternalError("failed to set target user permission", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"application_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"target_user_id": user.ID,
		"status":         item.Status,
	})
	logAuditWriteFailure("admin.permission.user_set", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.permission.user_set",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))

	return s.loadEmailCatchAllPermission(ctx, user)
}

// loadEmailCatchAllPermission resolves the current policy, eligibility, and
// application state into the single card rendered by the public frontend.
func (s *PermissionService) loadEmailCatchAllPermission(ctx context.Context, user model.User) (UserPermissionView, error) {
	policy, err := s.db.GetPermissionPolicy(ctx, PermissionKeyEmailCatchAll)
	if err != nil {
		if storage.IsNotFound(err) {
			return UserPermissionView{}, UnavailableError("catch-all email permission policy is not configured", fmt.Errorf("missing policy %s", PermissionKeyEmailCatchAll))
		}
		return UserPermissionView{}, InternalError("failed to load catch-all email permission policy", err)
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return UserPermissionView{}, err
	}

	applications, err := s.db.ListAdminApplicationsByApplicant(ctx, user.ID)
	if err != nil {
		return UserPermissionView{}, InternalError("failed to load permission applications", err)
	}

	application := findPermissionApplication(applications, PermissionKeyEmailCatchAll, namespace.Address, buildLegacyCatchAllApplicationTarget(namespace.RootDomain))
	reasons := buildCatchAllEligibilityReasons(user, policy, namespace)
	status := "not_requested"
	var summary *PermissionApplicationSummary
	if application != nil {
		status = application.Status
		summary = permissionApplicationSummaryFromModel(*application)
	}

	canApply := len(reasons) == 0 && status != "pending" && status != "approved"
	canManageRoute := status == "approved"
	catchAllAccess, err := s.loadEmailCatchAllAccessView(ctx, user.ID, policy, canManageRoute)
	if err != nil {
		return UserPermissionView{}, err
	}

	return normalizeCatchAllPermissionCopy(UserPermissionView{
		Key:                PermissionKeyEmailCatchAll,
		DisplayName:        itemDisplayName(policy.DisplayName, emailCatchAllPermissionDisplayName),
		Description:        itemDisplayName(policy.Description, emailCatchAllPermissionDescription),
		Target:             namespace.Address,
		PledgeText:         emailCatchAllPledgeTextClean,
		PolicyEnabled:      policy.Enabled,
		AutoApprove:        policy.AutoApprove,
		MinTrustLevel:      policy.MinTrustLevel,
		Eligible:           len(reasons) == 0,
		EligibilityReasons: reasons,
		Status:             status,
		CanApply:           canApply,
		CanManageRoute:     canManageRoute,
		CatchAllAccess:     catchAllAccess,
		Application:        summary,
	}), nil
}

// buildCatchAllEmailRouteView loads the persisted catch-all route when it
// exists and otherwise returns the placeholder row required by the public page.
func (s *PermissionService) buildCatchAllEmailRouteView(ctx context.Context, user model.User, permission UserPermissionView, namespace catchAllNamespace) (UserEmailRouteView, error) {
	snapshot, snapshotErr := s.lookupCloudflareCatchAllSnapshot(ctx, namespace.RootDomain)
	item := UserEmailRouteView{
		Kind:             UserEmailRouteKindCatchAll,
		PermissionKey:    PermissionKeyEmailCatchAll,
		DisplayName:      catchAllEmailRouteDisplayName,
		Description:      catchAllEmailRouteDescription,
		Address:          namespace.Address,
		Prefix:           "*",
		RootDomain:       namespace.RootDomain,
		TargetType:       model.EmailRouteTargetKindEmail,
		TargetEmail:      "",
		TargetDisplay:    "",
		Enabled:          false,
		Configured:       false,
		PermissionStatus: permission.Status,
		CatchAllAccess:   permission.CatchAllAccess,
		CanManage:        permission.CanManageRoute,
		CanDelete:        false,
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, namespace.RootDomain, emailCatchAllPrefix)
	if err != nil {
		if storage.IsNotFound(err) {
			if snapshotErr == nil && snapshot.Found {
				item.TargetEmail = snapshot.TargetEmail
				item.Enabled = snapshot.Enabled
				item.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
			}
			return normalizeUserEmailRouteCopy(item), nil
		}
		return UserEmailRouteView{}, InternalError("failed to load catch-all email route", err)
	}
	if route.OwnerUserID != user.ID {
		return UserEmailRouteView{}, UnavailableError("catch-all mailbox is assigned to another user", fmt.Errorf("route %d belongs to user %d", route.ID, route.OwnerUserID))
	}

	updatedAt := route.UpdatedAt
	item.ID = route.ID
	item.TargetType = normalizeRouteTargetType(route.TargetKind, route.TargetTokenPublicID)
	item.TargetEmail = route.TargetEmail
	item.TargetTokenPublicID = route.TargetTokenPublicID
	item.Enabled = route.Enabled
	item.Configured = strings.TrimSpace(route.TargetEmail) != "" || strings.TrimSpace(route.TargetTokenPublicID) != ""
	item.UpdatedAt = &updatedAt
	item.CatchAllAccess = permission.CatchAllAccess
	if item.TargetType == model.EmailRouteTargetKindAPIToken && strings.TrimSpace(route.TargetTokenPublicID) != "" {
		token, tokenErr := s.db.GetAPITokenByPublicID(ctx, route.TargetTokenPublicID)
		if tokenErr == nil {
			item.TargetTokenName = token.Name
			item.TargetDisplay = routeTargetDisplayFromModel(route, &token)
		} else {
			item.TargetDisplay = routeTargetDisplayFromModel(route, nil)
		}
	} else {
		item.TargetDisplay = strings.TrimSpace(route.TargetEmail)
	}
	if snapshotErr == nil && snapshot.Found {
		item.TargetEmail = snapshot.TargetEmail
		item.TargetType = model.EmailRouteTargetKindEmail
		item.TargetTokenPublicID = ""
		item.TargetTokenName = ""
		item.TargetDisplay = snapshot.TargetEmail
		item.Enabled = snapshot.Enabled
		item.Configured = strings.TrimSpace(snapshot.TargetEmail) != ""
	}
	return normalizeUserEmailRouteCopy(item), nil
}

// resolveCatchAllNamespace derives the fixed catch-all mailbox address from the
// current username and the configured default root domain.
func (s *PermissionService) resolveCatchAllNamespace(ctx context.Context, user model.User) (catchAllNamespace, error) {
	normalizedPrefix, err := normalizedUserPrefix(user.Username)
	if err != nil {
		return catchAllNamespace{}, ValidationError("current username cannot be used as a namespace")
	}

	defaultRootDomain := strings.ToLower(strings.TrimSpace(s.cfg.Cloudflare.DefaultRootDomain))
	if defaultRootDomain == "" {
		allocations, listErr := s.db.ListAllocationsByUser(ctx, user.ID)
		if listErr != nil {
			return catchAllNamespace{}, InternalError("failed to load user allocations", listErr)
		}
		for _, allocation := range allocations {
			if allocation.NormalizedPrefix != normalizedPrefix {
				continue
			}
			return catchAllNamespace{
				RootDomain:         allocation.FQDN,
				Address:            buildCatchAllEmailRouteAddress(allocation.FQDN),
				HasOwnedAllocation: true,
			}, nil
		}
		return catchAllNamespace{}, UnavailableError("default root domain is not configured for catch-all email permission", fmt.Errorf("default root domain is empty"))
	}

	namespaceRoot := normalizedPrefix + "." + defaultRootDomain
	allocations, err := s.db.ListAllocationsByUser(ctx, user.ID)
	if err != nil {
		return catchAllNamespace{}, InternalError("failed to load user allocations", err)
	}

	hasOwnedAllocation := false
	for _, allocation := range allocations {
		if allocation.NormalizedPrefix == normalizedPrefix && strings.EqualFold(allocation.RootDomain, defaultRootDomain) {
			hasOwnedAllocation = true
			break
		}
	}

	return catchAllNamespace{
		RootDomain:         namespaceRoot,
		Address:            buildCatchAllEmailRouteAddress(namespaceRoot),
		HasOwnedAllocation: hasOwnedAllocation,
	}, nil
}

// findPermissionApplication picks the latest application row that matches the
// requested permission key and routed target address.
func findPermissionApplication(applications []model.AdminApplication, key string, targets ...string) *model.AdminApplication {
	for index := range applications {
		item := applications[index]
		if item.Type != key {
			continue
		}
		for _, target := range targets {
			if strings.EqualFold(item.Target, target) {
				return &item
			}
		}
	}
	return nil
}

// permissionApplicationSummaryFromModel trims the admin-facing application row
// into the smaller shape consumed by the public frontend.
func permissionApplicationSummaryFromModel(item model.AdminApplication) *PermissionApplicationSummary {
	return &PermissionApplicationSummary{
		ID:         item.ID,
		Target:     item.Target,
		Status:     item.Status,
		Reason:     item.Reason,
		ReviewNote: item.ReviewNote,
		ReviewedAt: item.ReviewedAt,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

// parseCatchAllTargetAddress validates and splits one catch-all mailbox target
// recorded on an application row.
func parseCatchAllTargetAddress(target string) (string, string, error) {
	localPart, rootDomain, ok := strings.Cut(strings.ToLower(strings.TrimSpace(target)), "@")
	if !ok || strings.TrimSpace(rootDomain) == "" {
		return "", "", ValidationError("invalid catch-all application target")
	}
	if localPart != emailCatchAllPrefix && localPart != "*" {
		return "", "", ValidationError("unsupported catch-all application target")
	}
	return localPart, rootDomain, nil
}

// itemDisplayName returns the stored text when available and otherwise falls
// back to the built-in description so legacy databases still expose sane copy.
func itemDisplayName(value string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback
	}
	return trimmed
}

// normalizeCatchAllPermissionCopy replaces any damaged legacy copy with the
// canonical public wording used by the current frontend.
func normalizeCatchAllPermissionCopy(item UserPermissionView) UserPermissionView {
	if item.Key != PermissionKeyEmailCatchAll {
		return item
	}
	item.DisplayName = itemDisplayName(item.DisplayName, emailCatchAllPermissionDisplayName)
	item.Description = emailCatchAllPermissionDescription
	item.PledgeText = emailCatchAllPledgeTextClean
	if _, rootDomain, err := parseCatchAllTargetAddress(item.Target); err == nil {
		item.Target = buildCatchAllEmailRouteAddress(rootDomain)
	}
	return item
}

// normalizeUserEmailRouteCopy keeps user-visible mailbox labels and
// descriptions stable even when older database rows or source strings contain
// damaged text.
func normalizeUserEmailRouteCopy(item UserEmailRouteView) UserEmailRouteView {
	switch item.Kind {
	case UserEmailRouteKindDefault:
		item.DisplayName = defaultEmailRouteDisplayName
		item.Description = defaultEmailRouteDescription
	case UserEmailRouteKindCustom:
		item.DisplayName = customEmailRouteDisplayName
		item.Description = customEmailRouteDescription
	case UserEmailRouteKindCatchAll:
		item.DisplayName = catchAllEmailRouteDisplayName
		item.Description = catchAllEmailRouteDescription
	}
	return item
}

// buildCatchAllEligibilityReasons renders the current policy gate into the
// exact copy returned by the public permission API.
func buildCatchAllEligibilityReasons(user model.User, policy model.PermissionPolicy, namespace catchAllNamespace) []string {
	reasons := make([]string, 0, 3)
	if !policy.Enabled {
		reasons = append(reasons, "管理员当前已暂时关闭该权限申请。")
	}
	if !namespace.HasOwnedAllocation {
		reasons = append(reasons, "你当前尚未持有与你用户名同名的默认子域名，暂时无法申请该邮箱泛解析权限。")
	}
	if user.TrustLevel < policy.MinTrustLevel {
		reasons = append(reasons, fmt.Sprintf("你的 Linux Do 信任等级需要至少达到 %d，当前为 %d。", policy.MinTrustLevel, user.TrustLevel))
	}
	return reasons
}

// disableCatchAllEmailRouteForApplication keeps the effective route in sync when
// an administrator moves a catch-all permission away from approved.
func (s *PermissionService) disableCatchAllEmailRouteForApplication(ctx context.Context, actor model.User, application model.AdminApplication) error {
	if application.Type != PermissionKeyEmailCatchAll || application.Status == "approved" {
		return nil
	}

	_, rootDomain, err := parseCatchAllTargetAddress(application.Target)
	if err != nil {
		return InternalError("failed to parse catch-all permission target", err)
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, rootDomain, emailCatchAllPrefix)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil
		}
		return InternalError("failed to load catch-all email route", err)
	}
	if !route.Enabled {
		return nil
	}

	beforeState := newCatchAllEmailRouteSyncState(route.RootDomain, route.TargetEmail, route.Enabled)
	afterState := newCatchAllEmailRouteSyncState(route.RootDomain, route.TargetEmail, false)
	updated := route
	if err := newEmailRoutingProvisioner(s.cfg, s.cf).SyncForwardingState(ctx, beforeState, afterState, func() error {
		var persistErr error
		updated, persistErr = s.db.UpdateEmailRoute(ctx, storage.UpdateEmailRouteInput{
			ID:                  route.ID,
			TargetEmail:         route.TargetEmail,
			TargetKind:          route.TargetKind,
			TargetTokenPublicID: route.TargetTokenPublicID,
			Enabled:             false,
		})
		if persistErr != nil {
			return InternalError("failed to disable catch-all email route", persistErr)
		}
		return nil
	}); err != nil {
		return err
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": updated.ID,
		"application_id": application.ID,
		"address":        buildCatchAllEmailRouteAddress(updated.RootDomain),
		"status":         application.Status,
	})
	logAuditWriteFailure("admin.email_route.disable_on_permission_update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.disable_on_permission_update",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}))
	return nil
}

// buildLegacyCatchAllApplicationTarget preserves compatibility with early
// stored application rows that used `catch-all@<namespace>` instead of the
// current public `*@<namespace>` representation.
func buildLegacyCatchAllApplicationTarget(rootDomain string) string {
	return emailCatchAllPrefix + "@" + strings.ToLower(strings.TrimSpace(rootDomain))
}
