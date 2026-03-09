package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

const (
	// PermissionKeyEmailCatchAll is the stable identifier used across the
	// database, HTTP API, and frontends for the catch-all mailbox permission.
	PermissionKeyEmailCatchAll = "email_catch_all"

	// emailCatchAllPrefix is the fixed local-part prefix granted by this
	// permission. The address always resolves to catch-all@<username>.<root>.
	emailCatchAllPrefix = "catch-all"
)

// EmailCatchAllPledgeText is the canonical server-side pledge text recorded on
// every application. The backend stores this value directly so the audit trail
// does not depend on client-side wording.
const EmailCatchAllPledgeText = "我承诺仅将此邮箱泛解析权限用于合法、正当且合理的用途，不实施违法违纪行为，不滥用平台资源；如因本人使用导致任何后果，均由本人自行承担，与开发者无关；若因此获得收益，我也愿意无私回馈 Linux Do 社区。"

// PermissionService owns user-facing permission application flows together with
// the administrator-configurable policy rules that govern those flows.
type PermissionService struct {
	cfg config.Config
	db  Store
}

// PermissionApplicationSummary is the normalized subset of one application row
// exposed to the user-facing frontend.
type PermissionApplicationSummary struct {
	ID         int64      `json:"id"`
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
	Application        *PermissionApplicationSummary `json:"application,omitempty"`
}

// UserEmailRouteView describes the one catch-all mailbox route that can be
// configured by a user after the permission has been approved.
type UserEmailRouteView struct {
	ID               int64      `json:"id,omitempty"`
	PermissionKey    string     `json:"permission_key"`
	Address          string     `json:"address"`
	Prefix           string     `json:"prefix"`
	RootDomain       string     `json:"root_domain"`
	TargetEmail      string     `json:"target_email"`
	Enabled          bool       `json:"enabled"`
	Configured       bool       `json:"configured"`
	PermissionStatus string     `json:"permission_status"`
	CanManage        bool       `json:"can_manage"`
	UpdatedAt        *time.Time `json:"updated_at,omitempty"`
}

// SubmitPermissionApplicationRequest describes the single user-side mutation
// currently supported by the permissions page.
type SubmitPermissionApplicationRequest struct {
	Key string `json:"key"`
}

// UpsertMyCatchAllEmailRouteRequest describes the forwarding target configured
// by the user after the catch-all mailbox permission has been approved.
type UpsertMyCatchAllEmailRouteRequest struct {
	TargetEmail string `json:"target_email"`
	Enabled     bool   `json:"enabled"`
}

// UpdatePermissionPolicyRequest describes the administrator-editable subset of
// one permission policy row. Pointer fields let PATCH keep existing values.
type UpdatePermissionPolicyRequest struct {
	Enabled       *bool `json:"enabled,omitempty"`
	AutoApprove   *bool `json:"auto_approve,omitempty"`
	MinTrustLevel *int  `json:"min_trust_level,omitempty"`
}

// AdminSetUserPermissionRequest describes one administrator-authored permission
// decision written directly against a target user.
type AdminSetUserPermissionRequest struct {
	Status     string `json:"status"`
	ReviewNote string `json:"review_note"`
	Reason     string `json:"reason"`
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
func NewPermissionService(cfg config.Config, db Store) *PermissionService {
	return &PermissionService{cfg: cfg, db: db}
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

	item, err := s.db.UpsertAdminApplication(ctx, sqlite.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          permission.Target,
		Reason:          EmailCatchAllPledgeText,
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
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "permission.application.submit",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserPermissionView{}, InternalError("failed to write permission application audit log", err)
	}

	return s.loadEmailCatchAllPermission(ctx, user)
}

// ListMyEmailRoutes returns the single catch-all mailbox row shown on the user
// email page. A placeholder row is returned even when no target has been saved.
func (s *PermissionService) ListMyEmailRoutes(ctx context.Context, user model.User) ([]UserEmailRouteView, error) {
	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return nil, err
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return nil, err
	}

	item, err := s.buildCatchAllEmailRouteView(ctx, permission, namespace)
	if err != nil {
		return nil, err
	}
	return []UserEmailRouteView{item}, nil
}

// UpsertMyCatchAllEmailRoute creates or updates the user's forwarding target
// after the catch-all permission has been approved.
func (s *PermissionService) UpsertMyCatchAllEmailRoute(ctx context.Context, user model.User, request UpsertMyCatchAllEmailRouteRequest) (UserEmailRouteView, error) {
	permission, err := s.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}
	if permission.Status != "approved" {
		return UserEmailRouteView{}, ForbiddenError("the catch-all email permission has not been approved")
	}

	targetEmail := strings.ToLower(strings.TrimSpace(request.TargetEmail))
	if _, err := mail.ParseAddress(targetEmail); err != nil {
		return UserEmailRouteView{}, ValidationError("target_email must be a valid email address")
	}

	namespace, err := s.resolveCatchAllNamespace(ctx, user)
	if err != nil {
		return UserEmailRouteView{}, err
	}

	item, err := s.db.UpsertEmailRouteByAddress(ctx, sqlite.UpsertEmailRouteByAddressInput{
		OwnerUserID: user.ID,
		RootDomain:  namespace.RootDomain,
		Prefix:      emailCatchAllPrefix,
		TargetEmail: targetEmail,
		Enabled:     request.Enabled,
	})
	if err != nil {
		return UserEmailRouteView{}, InternalError("failed to save catch-all email route", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"address":        item.Prefix + "@" + item.RootDomain,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "email_route.catch_all.upsert",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserEmailRouteView{}, InternalError("failed to write catch-all email route audit log", err)
	}

	updatedAt := item.UpdatedAt
	return UserEmailRouteView{
		ID:               item.ID,
		PermissionKey:    PermissionKeyEmailCatchAll,
		Address:          emailCatchAllPrefix + "@" + namespace.RootDomain,
		Prefix:           emailCatchAllPrefix,
		RootDomain:       namespace.RootDomain,
		TargetEmail:      item.TargetEmail,
		Enabled:          item.Enabled,
		Configured:       strings.TrimSpace(item.TargetEmail) != "",
		PermissionStatus: permission.Status,
		CanManage:        true,
		UpdatedAt:        &updatedAt,
	}, nil
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
		if sqlite.IsNotFound(err) {
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

	updated, err := s.db.UpsertPermissionPolicy(ctx, sqlite.UpsertPermissionPolicyInput{
		Key:           item.Key,
		DisplayName:   item.DisplayName,
		Description:   item.Description,
		Enabled:       item.Enabled,
		AutoApprove:   item.AutoApprove,
		MinTrustLevel: item.MinTrustLevel,
	})
	if err != nil {
		return model.PermissionPolicy{}, InternalError("failed to update permission policy", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"policy_key":      updated.Key,
		"enabled":         updated.Enabled,
		"auto_approve":    updated.AutoApprove,
		"min_trust_level": updated.MinTrustLevel,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
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
		if sqlite.IsNotFound(err) {
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
		if sqlite.IsNotFound(err) {
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
	item, err := s.db.UpsertAdminApplication(ctx, sqlite.UpsertAdminApplicationInput{
		ApplicantUserID:  user.ID,
		Type:             PermissionKeyEmailCatchAll,
		Target:           permission.Target,
		Reason:           reason,
		Status:           status,
		ReviewNote:       strings.TrimSpace(request.ReviewNote),
		ReviewedByUserID: &actor.ID,
		ReviewedAt:       &now,
	})
	if err != nil {
		return UserPermissionView{}, InternalError("failed to set target user permission", err)
	}

	if err := s.disableCatchAllEmailRouteForApplication(ctx, actor, item); err != nil {
		return UserPermissionView{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"application_id": item.ID,
		"permission_key": PermissionKeyEmailCatchAll,
		"target_user_id": user.ID,
		"status":         item.Status,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.permission.user_set",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return UserPermissionView{}, InternalError("failed to write admin permission audit log", err)
	}

	return s.loadEmailCatchAllPermission(ctx, user)
}

// loadEmailCatchAllPermission resolves the current policy, eligibility, and
// application state into the single card rendered by the public frontend.
func (s *PermissionService) loadEmailCatchAllPermission(ctx context.Context, user model.User) (UserPermissionView, error) {
	policy, err := s.db.GetPermissionPolicy(ctx, PermissionKeyEmailCatchAll)
	if err != nil {
		if sqlite.IsNotFound(err) {
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

	application := findPermissionApplication(applications, PermissionKeyEmailCatchAll, namespace.Address)
	reasons := evaluateCatchAllEligibility(user, policy, namespace)
	status := "not_requested"
	var summary *PermissionApplicationSummary
	if application != nil {
		status = application.Status
		summary = permissionApplicationSummaryFromModel(*application)
	}

	canApply := len(reasons) == 0 && status != "pending" && status != "approved"
	canManageRoute := status == "approved"

	return UserPermissionView{
		Key:                PermissionKeyEmailCatchAll,
		DisplayName:        itemDisplayName(policy.DisplayName, "catch-all@<username>.linuxdo.space"),
		Description:        itemDisplayName(policy.Description, "为与你用户名同名的默认二级域名开启一个 catch-all 邮箱转发入口。"),
		Target:             namespace.Address,
		PledgeText:         EmailCatchAllPledgeText,
		PolicyEnabled:      policy.Enabled,
		AutoApprove:        policy.AutoApprove,
		MinTrustLevel:      policy.MinTrustLevel,
		Eligible:           len(reasons) == 0,
		EligibilityReasons: reasons,
		Status:             status,
		CanApply:           canApply,
		CanManageRoute:     canManageRoute,
		Application:        summary,
	}, nil
}

// buildCatchAllEmailRouteView loads the persisted route when it exists and
// otherwise returns the placeholder row required by the user-facing email page.
func (s *PermissionService) buildCatchAllEmailRouteView(ctx context.Context, permission UserPermissionView, namespace catchAllNamespace) (UserEmailRouteView, error) {
	item := UserEmailRouteView{
		PermissionKey:    PermissionKeyEmailCatchAll,
		Address:          namespace.Address,
		Prefix:           emailCatchAllPrefix,
		RootDomain:       namespace.RootDomain,
		TargetEmail:      "",
		Enabled:          false,
		Configured:       false,
		PermissionStatus: permission.Status,
		CanManage:        permission.CanManageRoute,
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, namespace.RootDomain, emailCatchAllPrefix)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return item, nil
		}
		return UserEmailRouteView{}, InternalError("failed to load catch-all email route", err)
	}

	updatedAt := route.UpdatedAt
	item.ID = route.ID
	item.TargetEmail = route.TargetEmail
	item.Enabled = route.Enabled
	item.Configured = strings.TrimSpace(route.TargetEmail) != ""
	item.UpdatedAt = &updatedAt
	return item, nil
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
				Address:            emailCatchAllPrefix + "@" + allocation.FQDN,
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
		Address:            emailCatchAllPrefix + "@" + namespaceRoot,
		HasOwnedAllocation: hasOwnedAllocation,
	}, nil
}

// evaluateCatchAllEligibility converts the current policy and user profile into
// human-readable reasons that explain why the permission can or cannot be
// requested right now.
func evaluateCatchAllEligibility(user model.User, policy model.PermissionPolicy, namespace catchAllNamespace) []string {
	reasons := make([]string, 0, 3)
	if !policy.Enabled {
		reasons = append(reasons, "管理员当前已暂时关闭该权限申请。")
	}
	if !namespace.HasOwnedAllocation {
		reasons = append(reasons, "你当前尚未持有与用户名同名的默认子域名，暂时无法申请该邮箱泛解析权限。")
	}
	if user.TrustLevel < policy.MinTrustLevel {
		reasons = append(reasons, fmt.Sprintf("你的 Linux Do 信任等级需要至少达到 %d，当前为 %d。", policy.MinTrustLevel, user.TrustLevel))
	}
	return reasons
}

// findPermissionApplication picks the latest application row that matches the
// requested permission key and routed target address.
func findPermissionApplication(applications []model.AdminApplication, key string, target string) *model.AdminApplication {
	for index := range applications {
		item := applications[index]
		if item.Type == key && strings.EqualFold(item.Target, target) {
			return &item
		}
	}
	return nil
}

// permissionApplicationSummaryFromModel trims the admin-facing application row
// into the smaller shape consumed by the public frontend.
func permissionApplicationSummaryFromModel(item model.AdminApplication) *PermissionApplicationSummary {
	return &PermissionApplicationSummary{
		ID:         item.ID,
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
	if localPart != emailCatchAllPrefix {
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
		if sqlite.IsNotFound(err) {
			return nil
		}
		return InternalError("failed to load catch-all email route", err)
	}
	if !route.Enabled {
		return nil
	}

	updated, err := s.db.UpdateEmailRoute(ctx, sqlite.UpdateEmailRouteInput{
		ID:          route.ID,
		TargetEmail: route.TargetEmail,
		Enabled:     false,
	})
	if err != nil {
		return InternalError("failed to disable catch-all email route", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"email_route_id": updated.ID,
		"application_id": application.ID,
		"address":        updated.Prefix + "@" + updated.RootDomain,
		"status":         application.Status,
	})
	if err := s.db.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.disable_on_permission_update",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write catch-all disable audit log", err)
	}
	return nil
}
