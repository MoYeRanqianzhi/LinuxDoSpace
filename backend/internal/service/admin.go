package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/mail"
	"sort"
	"strconv"
	"strings"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage"
)

// AdminService groups the administrator-only workflows used by the standalone admin frontend.
type AdminService struct {
	cfg config.Config
	db  Store
	cf  CloudflareClient
}

// AdminUserQuotaView describes one managed domain quota row shown inside the user edit dialog.
type AdminUserQuotaView struct {
	ManagedDomainID int64  `json:"managed_domain_id"`
	RootDomain      string `json:"root_domain"`
	DefaultQuota    int    `json:"default_quota"`
	EffectiveQuota  int    `json:"effective_quota"`
	AllocationCount int    `json:"allocation_count"`
}

// AdminUserDetail is the expanded payload required by the admin user edit dialog.
type AdminUserDetail struct {
	User    model.AdminUserSummary `json:"user"`
	BanNote string                 `json:"ban_note"`
	Quotas  []AdminUserQuotaView   `json:"quotas"`
}

// UpdateAdminUserRequest describes the moderation controls that can be changed for one user.
type UpdateAdminUserRequest struct {
	IsBanned bool   `json:"is_banned"`
	BanNote  string `json:"ban_note"`
}

// UpsertAdminRecordRequest describes the mutable DNS record payload used by administrators.
type UpsertAdminRecordRequest struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Comment  string `json:"comment"`
	Priority *int   `json:"priority,omitempty"`
}

// CreateAdminAllocationRequest describes the administrator-only fields needed to
// manually create one allocation namespace.
type CreateAdminAllocationRequest struct {
	OwnerUserID int64  `json:"owner_user_id"`
	RootDomain  string `json:"root_domain"`
	Prefix      string `json:"prefix"`
	IsPrimary   bool   `json:"is_primary"`
	Source      string `json:"source"`
	Status      string `json:"status"`
}

// UpdateAdminAllocationRequest describes the mutable lifecycle controls for one
// existing allocation namespace.
type UpdateAdminAllocationRequest struct {
	OwnerUserID *int64 `json:"owner_user_id,omitempty"`
	IsPrimary   *bool  `json:"is_primary,omitempty"`
	Source      string `json:"source"`
	Status      string `json:"status"`
}

// UpsertEmailRouteRequest describes the input accepted by the email routes page.
type UpsertEmailRouteRequest struct {
	OwnerUserID int64  `json:"owner_user_id"`
	RootDomain  string `json:"root_domain"`
	Prefix      string `json:"prefix"`
	TargetEmail string `json:"target_email"`
	Enabled     bool   `json:"enabled"`
}

// UpdateEmailRouteRequest describes the actually mutable fields supported by
// the administrator email-route PATCH endpoint.
type UpdateEmailRouteRequest struct {
	TargetEmail string `json:"target_email"`
	Enabled     bool   `json:"enabled"`
}

// UpdateApplicationRequest describes the moderation action performed on one request row.
type UpdateApplicationRequest struct {
	Status     string `json:"status"`
	ReviewNote string `json:"review_note"`
}

// GenerateRedeemCodesRequest describes one batch of generated redeem codes.
type GenerateRedeemCodesRequest struct {
	Amount int    `json:"amount"`
	Type   string `json:"type"`
	Target string `json:"target"`
	Note   string `json:"note"`
}

// NewAdminService creates a new administrator service instance.
func NewAdminService(cfg config.Config, db Store, cf CloudflareClient) *AdminService {
	return &AdminService{cfg: cfg, db: db, cf: cf}
}

// ListUsers returns the compact user list required by the admin console user page.
func (s *AdminService) ListUsers(ctx context.Context) ([]model.AdminUserSummary, error) {
	items, err := s.db.ListAdminUsers(ctx)
	if err != nil {
		return nil, InternalError("failed to list admin users", err)
	}
	return items, nil
}

// GetUserDetail loads the expanded moderation and quota view for one user.
func (s *AdminService) GetUserDetail(ctx context.Context, userID int64) (AdminUserDetail, error) {
	baseUser, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return AdminUserDetail{}, NotFoundError("target user not found")
		}
		return AdminUserDetail{}, InternalError("failed to load target user", err)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		return AdminUserDetail{}, err
	}

	var summary *model.AdminUserSummary
	for index := range users {
		if users[index].ID == userID {
			summary = &users[index]
			break
		}
	}
	if summary == nil {
		summary = &model.AdminUserSummary{
			ID:              baseUser.ID,
			LinuxDOUserID:   baseUser.LinuxDOUserID,
			Username:        baseUser.Username,
			DisplayName:     baseUser.DisplayName,
			AvatarURL:       baseUser.AvatarURL,
			TrustLevel:      baseUser.TrustLevel,
			IsLinuxDOAdmin:  baseUser.IsLinuxDOAdmin,
			IsAppAdmin:      baseUser.IsAppAdmin,
			CreatedAt:       baseUser.CreatedAt,
			LastLoginAt:     baseUser.LastLoginAt,
			AllocationCount: 0,
		}
	}

	control, err := s.db.GetUserControlByUserID(ctx, userID)
	if err != nil {
		return AdminUserDetail{}, InternalError("failed to load user moderation state", err)
	}

	managedDomains, err := s.db.ListManagedDomains(ctx, true)
	if err != nil {
		return AdminUserDetail{}, InternalError("failed to load managed domains", err)
	}

	quotas := make([]AdminUserQuotaView, 0, len(managedDomains))
	for _, managedDomain := range managedDomains {
		effectiveQuota, quotaErr := s.db.GetEffectiveQuota(ctx, userID, managedDomain.ID)
		if quotaErr != nil {
			return AdminUserDetail{}, InternalError("failed to load effective quota", quotaErr)
		}
		allocationCount, countErr := s.db.CountAllocationsByUserAndDomain(ctx, userID, managedDomain.ID)
		if countErr != nil {
			return AdminUserDetail{}, InternalError("failed to count user allocations", countErr)
		}
		quotas = append(quotas, AdminUserQuotaView{
			ManagedDomainID: managedDomain.ID,
			RootDomain:      managedDomain.RootDomain,
			DefaultQuota:    managedDomain.DefaultQuota,
			EffectiveQuota:  effectiveQuota,
			AllocationCount: allocationCount,
		})
	}

	sort.Slice(quotas, func(i int, j int) bool {
		if quotas[i].RootDomain == quotas[j].RootDomain {
			return quotas[i].ManagedDomainID < quotas[j].ManagedDomainID
		}
		return quotas[i].RootDomain < quotas[j].RootDomain
	})

	summary.IsBanned = control.IsBanned
	return AdminUserDetail{User: *summary, BanNote: control.Note, Quotas: quotas}, nil
}

// UpdateUser applies one moderation update to the target user.
func (s *AdminService) UpdateUser(ctx context.Context, actor model.User, userID int64, request UpdateAdminUserRequest) (AdminUserDetail, error) {
	if actor.ID == userID && request.IsBanned {
		return AdminUserDetail{}, ForbiddenError("you cannot ban your own administrator account")
	}

	targetUser, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		if storage.IsNotFound(err) {
			return AdminUserDetail{}, NotFoundError("target user not found")
		}
		return AdminUserDetail{}, InternalError("failed to load target user", err)
	}

	control, err := s.db.UpsertUserControl(ctx, storage.UpsertUserControlInput{
		UserID:   targetUser.ID,
		IsBanned: request.IsBanned,
		Note:     strings.TrimSpace(request.BanNote),
	})
	if err != nil {
		return AdminUserDetail{}, InternalError("failed to update user moderation state", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"target_user_id": targetUser.ID,
		"is_banned":      control.IsBanned,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.user_control.upsert",
		ResourceType: "user_control",
		ResourceID:   strconv.FormatInt(targetUser.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return AdminUserDetail{}, InternalError("failed to write user moderation audit log", err)
	}

	return s.GetUserDetail(ctx, userID)
}

// ListAllocations returns all allocation namespaces with owner identity for admin workflows.
func (s *AdminService) ListAllocations(ctx context.Context) ([]model.AdminAllocationSummary, error) {
	items, err := s.db.ListAdminAllocations(ctx)
	if err != nil {
		return nil, InternalError("failed to list admin allocations", err)
	}
	return items, nil
}

// CreateAllocation manually provisions one allocation namespace for any user.
func (s *AdminService) CreateAllocation(ctx context.Context, actor model.User, request CreateAdminAllocationRequest) (model.AdminAllocationSummary, error) {
	owner, managedDomain, normalizedPrefix, fqdn, status, source, err := s.validateAdminAllocationWrite(ctx, request.OwnerUserID, request.RootDomain, request.Prefix, request.Status, request.Source)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}
	if status != "active" && request.IsPrimary {
		return model.AdminAllocationSummary{}, ValidationError("disabled allocations cannot be marked as primary")
	}

	item, err := s.db.CreateAllocation(ctx, storage.CreateAllocationInput{
		UserID:           owner.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           normalizedPrefix,
		NormalizedPrefix: normalizedPrefix,
		FQDN:             fqdn,
		IsPrimary:        request.IsPrimary,
		Source:           source,
		Status:           status,
	})
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return model.AdminAllocationSummary{}, ConflictError("the requested allocation already exists")
		}
		return model.AdminAllocationSummary{}, InternalError("failed to create allocation", err)
	}

	created, err := s.loadAdminAllocation(ctx, item.ID)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"allocation_id": created.ID,
		"owner_user_id": created.UserID,
		"fqdn":          created.FQDN,
		"status":        created.Status,
		"source":        created.Source,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.allocation.create",
		ResourceType: "allocation",
		ResourceID:   strconv.FormatInt(created.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.AdminAllocationSummary{}, InternalError("failed to write allocation create audit log", err)
	}

	return created, nil
}

// UpdateAllocation changes ownership or lifecycle state for one allocation.
func (s *AdminService) UpdateAllocation(ctx context.Context, actor model.User, allocationID int64, request UpdateAdminAllocationRequest) (model.AdminAllocationSummary, error) {
	existing, err := s.loadAdminAllocation(ctx, allocationID)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}

	ownerID := existing.UserID
	if request.OwnerUserID != nil {
		ownerID = *request.OwnerUserID
	}
	owner, err := s.db.GetUserByID(ctx, ownerID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.AdminAllocationSummary{}, NotFoundError("allocation owner not found")
		}
		return model.AdminAllocationSummary{}, InternalError("failed to load allocation owner", err)
	}

	status := normalizeAdminAllocationStatus(request.Status)
	if status == "" {
		status = existing.Status
	}
	source := strings.TrimSpace(request.Source)
	if source == "" {
		source = existing.Source
	}
	if status != "active" && request.IsPrimary != nil && *request.IsPrimary {
		return model.AdminAllocationSummary{}, ValidationError("disabled allocations cannot be marked as primary")
	}
	isPrimary := existing.IsPrimary
	if request.IsPrimary != nil {
		isPrimary = *request.IsPrimary
	}
	if status != "active" {
		isPrimary = false
	}

	updated, err := s.db.UpdateAllocation(ctx, storage.UpdateAllocationInput{
		ID:        allocationID,
		UserID:    owner.ID,
		IsPrimary: isPrimary,
		Source:    source,
		Status:    status,
	})
	if err != nil {
		if storage.IsNotFound(err) {
			return model.AdminAllocationSummary{}, NotFoundError("allocation not found")
		}
		return model.AdminAllocationSummary{}, InternalError("failed to update allocation", err)
	}

	result, err := s.loadAdminAllocation(ctx, updated.ID)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}

	metadata, _ := json.Marshal(map[string]any{
		"allocation_id":    result.ID,
		"previous_user_id": existing.UserID,
		"owner_user_id":    result.UserID,
		"fqdn":             result.FQDN,
		"status":           result.Status,
		"source":           result.Source,
		"is_primary":       result.IsPrimary,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.allocation.update",
		ResourceType: "allocation",
		ResourceID:   strconv.FormatInt(result.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.AdminAllocationSummary{}, InternalError("failed to write allocation update audit log", err)
	}

	return result, nil
}

// ListRecords returns the global DNS record list visible to administrators.
func (s *AdminService) ListRecords(ctx context.Context) ([]model.AdminDNSRecord, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return nil, UnavailableError("cloudflare integration is not configured", nil)
	}

	allocations, err := s.ListAllocations(ctx)
	if err != nil {
		return nil, err
	}

	allocationsByZone := make(map[string][]model.AdminAllocationSummary)
	for _, allocation := range allocations {
		allocationsByZone[allocation.CloudflareZoneID] = append(allocationsByZone[allocation.CloudflareZoneID], allocation)
	}

	items := make([]model.AdminDNSRecord, 0, 128)
	for zoneID, zoneAllocations := range allocationsByZone {
		records, listErr := s.cf.ListAllDNSRecords(ctx, zoneID)
		if listErr != nil {
			return nil, UnavailableError("failed to list cloudflare dns records", listErr)
		}

		sort.Slice(zoneAllocations, func(i int, j int) bool {
			if len(zoneAllocations[i].FQDN) == len(zoneAllocations[j].FQDN) {
				return zoneAllocations[i].FQDN > zoneAllocations[j].FQDN
			}
			return len(zoneAllocations[i].FQDN) > len(zoneAllocations[j].FQDN)
		})

		for _, record := range records {
			allocation, matched := findBestAllocationMatch(record.Name, zoneAllocations)
			if !matched {
				continue
			}

			items = append(items, model.AdminDNSRecord{
				AllocationID:     allocation.ID,
				OwnerUserID:      allocation.UserID,
				OwnerUsername:    allocation.OwnerUsername,
				OwnerDisplayName: allocation.OwnerDisplayName,
				RootDomain:       allocation.RootDomain,
				NamespaceFQDN:    allocation.FQDN,
				ID:               record.ID,
				Type:             record.Type,
				Name:             record.Name,
				RelativeName:     RelativeNameFromAbsolute(record.Name, allocation.FQDN),
				Content:          record.Content,
				TTL:              record.TTL,
				Proxied:          record.Proxied,
				Comment:          record.Comment,
				Priority:         record.Priority,
			})
		}
	}

	sort.Slice(items, func(i int, j int) bool {
		if items[i].Name == items[j].Name {
			if items[i].Type == items[j].Type {
				return items[i].ID < items[j].ID
			}
			return items[i].Type < items[j].Type
		}
		return items[i].Name < items[j].Name
	})
	return items, nil
}

// CreateRecord creates a DNS record inside the specified allocation namespace without the temporary end-user restriction.
func (s *AdminService) CreateRecord(ctx context.Context, actor model.User, allocationID int64, request UpsertAdminRecordRequest) (model.AdminDNSRecord, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return model.AdminDNSRecord{}, UnavailableError("cloudflare integration is not configured", nil)
	}

	allocation, err := s.loadAdminAllocation(ctx, allocationID)
	if err != nil {
		return model.AdminDNSRecord{}, err
	}

	createInput, relativeName, err := s.buildAdminCloudflareRecordInput(allocation, request)
	if err != nil {
		return model.AdminDNSRecord{}, err
	}

	created, err := s.cf.CreateDNSRecord(ctx, allocation.CloudflareZoneID, createInput)
	if err != nil {
		return model.AdminDNSRecord{}, UnavailableError("failed to create cloudflare dns record", err)
	}

	item := adminRecordFromCloudflare(allocation, created, relativeName)
	metadata, _ := json.Marshal(map[string]any{
		"record_id":     item.ID,
		"allocation_id": allocation.ID,
		"name":          item.Name,
		"type":          item.Type,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.dns_record.create",
		ResourceType: "dns_record",
		ResourceID:   item.ID,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.AdminDNSRecord{}, InternalError("failed to write admin dns create audit log", err)
	}
	return item, nil
}

// UpdateRecord updates a DNS record inside the specified allocation namespace.
func (s *AdminService) UpdateRecord(ctx context.Context, actor model.User, allocationID int64, recordID string, request UpsertAdminRecordRequest) (model.AdminDNSRecord, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return model.AdminDNSRecord{}, UnavailableError("cloudflare integration is not configured", nil)
	}

	allocation, err := s.loadAdminAllocation(ctx, allocationID)
	if err != nil {
		return model.AdminDNSRecord{}, err
	}

	existing, err := s.cf.GetDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID))
	if err != nil {
		return model.AdminDNSRecord{}, UnavailableError("failed to load cloudflare dns record", err)
	}
	if !BelongsToNamespace(existing.Name, allocation.FQDN) {
		return model.AdminDNSRecord{}, ForbiddenError("record does not belong to the selected allocation")
	}

	updateInput, relativeName, err := s.buildAdminCloudflareRecordInput(allocation, request)
	if err != nil {
		return model.AdminDNSRecord{}, err
	}

	updated, err := s.cf.UpdateDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID), updateInput)
	if err != nil {
		return model.AdminDNSRecord{}, UnavailableError("failed to update cloudflare dns record", err)
	}

	item := adminRecordFromCloudflare(allocation, updated, relativeName)
	metadata, _ := json.Marshal(map[string]any{
		"record_id":     item.ID,
		"allocation_id": allocation.ID,
		"name":          item.Name,
		"type":          item.Type,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.dns_record.update",
		ResourceType: "dns_record",
		ResourceID:   item.ID,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.AdminDNSRecord{}, InternalError("failed to write admin dns update audit log", err)
	}
	return item, nil
}

// DeleteRecord deletes a DNS record inside the specified allocation namespace.
func (s *AdminService) DeleteRecord(ctx context.Context, actor model.User, allocationID int64, recordID string) error {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return UnavailableError("cloudflare integration is not configured", nil)
	}

	allocation, err := s.loadAdminAllocation(ctx, allocationID)
	if err != nil {
		return err
	}

	existing, err := s.cf.GetDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID))
	if err != nil {
		return UnavailableError("failed to load cloudflare dns record", err)
	}
	if !BelongsToNamespace(existing.Name, allocation.FQDN) {
		return ForbiddenError("record does not belong to the selected allocation")
	}

	if err := s.cf.DeleteDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID)); err != nil {
		return UnavailableError("failed to delete cloudflare dns record", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"record_id":     strings.TrimSpace(recordID),
		"allocation_id": allocation.ID,
		"name":          existing.Name,
		"type":          existing.Type,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.dns_record.delete",
		ResourceType: "dns_record",
		ResourceID:   strings.TrimSpace(recordID),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write admin dns delete audit log", err)
	}
	return nil
}

// ListEmailRoutes returns all persisted email forwarding rules.
func (s *AdminService) ListEmailRoutes(ctx context.Context) ([]model.EmailRoute, error) {
	items, err := s.db.ListEmailRoutes(ctx)
	if err != nil {
		return nil, InternalError("failed to list email routes", err)
	}
	return items, nil
}

// CreateEmailRoute inserts one administrator-managed email forwarding rule.
func (s *AdminService) CreateEmailRoute(ctx context.Context, actor model.User, request UpsertEmailRouteRequest) (model.EmailRoute, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	if _, err := s.db.GetUserByID(ctx, request.OwnerUserID); err != nil {
		if storage.IsNotFound(err) {
			return model.EmailRoute{}, NotFoundError("email route owner not found")
		}
		return model.EmailRoute{}, InternalError("failed to load email route owner", err)
	}
	if _, err := s.db.GetManagedDomainByRoot(ctx, request.RootDomain); err != nil {
		if storage.IsNotFound(err) {
			return model.EmailRoute{}, NotFoundError("managed domain not found")
		}
		return model.EmailRoute{}, InternalError("failed to load managed domain", err)
	}

	normalizedPrefix, err := NormalizePrefix(request.Prefix)
	if err != nil {
		return model.EmailRoute{}, ValidationError(err.Error())
	}
	if _, err := mail.ParseAddress(strings.TrimSpace(request.TargetEmail)); err != nil {
		return model.EmailRoute{}, ValidationError("target_email must be a valid email address")
	}

	if _, err := s.db.GetEmailRouteByAddress(ctx, request.RootDomain, normalizedPrefix); err == nil {
		return model.EmailRoute{}, ConflictError("the requested email route already exists")
	} else if !storage.IsNotFound(err) {
		return model.EmailRoute{}, InternalError("failed to check for an existing email route", err)
	}

	desiredState := newForwardingEmailRouteSyncState(request.RootDomain, normalizedPrefix, request.TargetEmail, request.Enabled)
	var item model.EmailRoute
	if err := routing.SyncForwardingState(ctx, newDeletedEmailRouteSyncState(request.RootDomain, normalizedPrefix), desiredState, func() error {
		var persistErr error
		item, persistErr = s.db.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
			OwnerUserID: request.OwnerUserID,
			RootDomain:  request.RootDomain,
			Prefix:      normalizedPrefix,
			TargetEmail: request.TargetEmail,
			Enabled:     request.Enabled,
		})
		if persistErr != nil {
			if strings.Contains(strings.ToLower(persistErr.Error()), "unique") {
				return ConflictError("the requested email route already exists")
			}
			return InternalError("failed to create email route", persistErr)
		}
		return nil
	}); err != nil {
		return model.EmailRoute{}, err
	}

	metadata, _ := json.Marshal(map[string]any{"email_route_id": item.ID, "address": item.Prefix + "@" + item.RootDomain})
	logAuditWriteFailure("admin.email_route.create", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.create",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))
	return item, nil
}

// UpdateEmailRoute updates the mutable fields of one email forwarding rule.
func (s *AdminService) UpdateEmailRoute(ctx context.Context, actor model.User, routeID int64, request UpdateEmailRouteRequest) (model.EmailRoute, error) {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	if _, err := mail.ParseAddress(strings.TrimSpace(request.TargetEmail)); err != nil {
		return model.EmailRoute{}, ValidationError("target_email must be a valid email address")
	}

	existingRoutes, err := s.db.ListEmailRoutes(ctx)
	if err != nil {
		return model.EmailRoute{}, InternalError("failed to load email routes before update", err)
	}

	var existing *model.EmailRoute
	for index := range existingRoutes {
		if existingRoutes[index].ID == routeID {
			existing = &existingRoutes[index]
			break
		}
	}
	if existing == nil {
		return model.EmailRoute{}, NotFoundError("email route not found")
	}

	beforeState := newForwardingEmailRouteSyncState(existing.RootDomain, existing.Prefix, existing.TargetEmail, existing.Enabled)
	desiredState := newForwardingEmailRouteSyncState(existing.RootDomain, existing.Prefix, request.TargetEmail, request.Enabled)
	var item model.EmailRoute
	if err := routing.SyncForwardingState(ctx, beforeState, desiredState, func() error {
		var persistErr error
		item, persistErr = s.db.UpdateEmailRoute(ctx, storage.UpdateEmailRouteInput{ID: routeID, TargetEmail: request.TargetEmail, Enabled: request.Enabled})
		if persistErr != nil {
			if storage.IsNotFound(persistErr) {
				return NotFoundError("email route not found")
			}
			return InternalError("failed to update email route", persistErr)
		}
		return nil
	}); err != nil {
		return model.EmailRoute{}, err
	}

	metadata, _ := json.Marshal(map[string]any{"email_route_id": item.ID, "address": item.Prefix + "@" + item.RootDomain})
	logAuditWriteFailure("admin.email_route.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.update",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))
	return item, nil
}

// DeleteEmailRoute removes one stored email forwarding rule.
func (s *AdminService) DeleteEmailRoute(ctx context.Context, actor model.User, routeID int64) error {
	routing := newEmailRoutingProvisioner(s.cfg, s.cf)

	routes, err := s.db.ListEmailRoutes(ctx)
	if err != nil {
		return InternalError("failed to load email routes before delete", err)
	}

	var route *model.EmailRoute
	for index := range routes {
		if routes[index].ID == routeID {
			route = &routes[index]
			break
		}
	}
	if route == nil {
		return NotFoundError("email route not found")
	}

	beforeState := newForwardingEmailRouteSyncState(route.RootDomain, route.Prefix, route.TargetEmail, route.Enabled)
	afterState := newDeletedEmailRouteSyncState(route.RootDomain, route.Prefix)
	if err := routing.SyncForwardingState(ctx, beforeState, afterState, func() error {
		if persistErr := s.db.DeleteEmailRoute(ctx, routeID); persistErr != nil {
			if storage.IsNotFound(persistErr) {
				return NotFoundError("email route not found")
			}
			return InternalError("failed to delete email route", persistErr)
		}
		return nil
	}); err != nil {
		return err
	}

	metadata, _ := json.Marshal(map[string]any{"email_route_id": routeID})
	logAuditWriteFailure("admin.email_route.delete", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.email_route.delete",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(routeID, 10),
		MetadataJSON: string(metadata),
	}))
	return nil
}

// ListApplications returns all administrator-visible moderation requests.
func (s *AdminService) ListApplications(ctx context.Context) ([]model.AdminApplication, error) {
	items, err := s.db.ListAdminApplications(ctx)
	if err != nil {
		return nil, InternalError("failed to list admin applications", err)
	}
	return items, nil
}

// UpdateApplication applies one moderation decision to a request.
func (s *AdminService) UpdateApplication(ctx context.Context, actor model.User, applicationID int64, request UpdateApplicationRequest) (model.AdminApplication, error) {
	status := normalizeAdminApplicationStatus(request.Status)
	if status == "" {
		return model.AdminApplication{}, ValidationError("status must be pending, approved, or rejected")
	}

	currentApplication, err := s.findAdminApplicationByID(ctx, applicationID)
	if err != nil {
		return model.AdminApplication{}, err
	}

	nextApplication := currentApplication
	nextApplication.Status = status
	nextApplication.ReviewNote = strings.TrimSpace(request.ReviewNote)
	nextApplication.ReviewedByUserID = &actor.ID
	if err := ensureCatchAllRelayIngressDNSForApplication(ctx, s.cfg, s.cf, nextApplication); err != nil {
		return model.AdminApplication{}, err
	}
	if err := s.disableCatchAllEmailRouteForApplication(ctx, actor, nextApplication); err != nil {
		return model.AdminApplication{}, err
	}

	item, err := s.db.UpdateAdminApplication(ctx, storage.UpdateAdminApplicationInput{
		ID:               applicationID,
		Status:           status,
		ReviewNote:       strings.TrimSpace(request.ReviewNote),
		ReviewedByUserID: actor.ID,
	})
	if err != nil {
		if storage.IsNotFound(err) {
			return model.AdminApplication{}, NotFoundError("application not found")
		}
		return model.AdminApplication{}, InternalError("failed to update admin application", err)
	}
	metadata, _ := json.Marshal(map[string]any{"application_id": item.ID, "status": item.Status})
	logAuditWriteFailure("admin.application.update", s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.application.update",
		ResourceType: "admin_application",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}))
	return item, nil
}

// disableCatchAllEmailRouteForApplication ensures that revoking or re-pending an
// approved catch-all permission immediately disables the corresponding forward.
func (s *AdminService) disableCatchAllEmailRouteForApplication(ctx context.Context, actor model.User, application model.AdminApplication) error {
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
			ID:          route.ID,
			TargetEmail: route.TargetEmail,
			Enabled:     false,
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

// findAdminApplicationByID locates one moderation request without mutating it.
// The moderation flow uses this to disable privileged side effects before the
// stored status flips away from approved.
func (s *AdminService) findAdminApplicationByID(ctx context.Context, applicationID int64) (model.AdminApplication, error) {
	items, err := s.db.ListAdminApplications(ctx)
	if err != nil {
		return model.AdminApplication{}, InternalError("failed to load admin applications", err)
	}
	for _, item := range items {
		if item.ID == applicationID {
			return item, nil
		}
	}
	return model.AdminApplication{}, NotFoundError("application not found")
}

// ListRedeemCodes returns all generated redeem codes.
func (s *AdminService) ListRedeemCodes(ctx context.Context) ([]model.RedeemCode, error) {
	items, err := s.db.ListRedeemCodes(ctx)
	if err != nil {
		return nil, InternalError("failed to list redeem codes", err)
	}
	return items, nil
}

// GenerateRedeemCodes creates one batch of random redeem codes.
func (s *AdminService) GenerateRedeemCodes(ctx context.Context, actor model.User, request GenerateRedeemCodesRequest) ([]model.RedeemCode, error) {
	if request.Amount < 1 || request.Amount > 100 {
		return nil, ValidationError("amount must be between 1 and 100")
	}
	typeValue := normalizeRedeemType(request.Type)
	if typeValue == "" {
		return nil, ValidationError("type must be single, multiple, or wildcard")
	}
	if strings.TrimSpace(request.Target) == "" {
		return nil, ValidationError("target is required")
	}

	items := make([]model.RedeemCode, 0, request.Amount)
	for index := 0; index < request.Amount; index++ {
		var created model.RedeemCode
		var createErr error
		for attempt := 0; attempt < 5; attempt++ {
			candidate, tokenErr := generateRedeemCodeValue()
			if tokenErr != nil {
				return nil, InternalError("failed to generate redeem code", tokenErr)
			}
			created, createErr = s.db.CreateRedeemCode(ctx, storage.CreateRedeemCodeInput{
				Code:            candidate,
				Type:            typeValue,
				Target:          strings.TrimSpace(request.Target),
				Note:            strings.TrimSpace(request.Note),
				CreatedByUserID: actor.ID,
			})
			if createErr == nil {
				break
			}
			if !strings.Contains(strings.ToLower(createErr.Error()), "unique") {
				return nil, InternalError("failed to persist redeem code", createErr)
			}
		}
		if createErr != nil {
			return nil, ConflictError("failed to allocate a unique redeem code after multiple retries")
		}
		items = append(items, created)
	}

	for _, item := range items {
		metadata, _ := json.Marshal(map[string]any{"redeem_code_id": item.ID, "type": item.Type, "target": item.Target})
		if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
			ActorUserID:  &actor.ID,
			Action:       "admin.redeem_code.create",
			ResourceType: "redeem_code",
			ResourceID:   strconv.FormatInt(item.ID, 10),
			MetadataJSON: string(metadata),
		}); err != nil {
			return nil, InternalError("failed to write redeem code audit log", err)
		}
	}
	return items, nil
}

// DeleteRedeemCode removes one generated redeem code.
func (s *AdminService) DeleteRedeemCode(ctx context.Context, actor model.User, redeemCodeID int64) error {
	if err := s.db.DeleteRedeemCode(ctx, redeemCodeID); err != nil {
		if storage.IsNotFound(err) {
			return NotFoundError("redeem code not found")
		}
		return InternalError("failed to delete redeem code", err)
	}

	metadata, _ := json.Marshal(map[string]any{"redeem_code_id": redeemCodeID})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.redeem_code.delete",
		ResourceType: "redeem_code",
		ResourceID:   strconv.FormatInt(redeemCodeID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write redeem code delete audit log", err)
	}
	return nil
}

// loadAdminAllocation resolves one allocation and enriches it with owner identity.
func (s *AdminService) loadAdminAllocation(ctx context.Context, allocationID int64) (model.AdminAllocationSummary, error) {
	items, err := s.ListAllocations(ctx)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}
	for _, item := range items {
		if item.ID == allocationID {
			return item, nil
		}
	}
	return model.AdminAllocationSummary{}, NotFoundError("allocation not found")
}

// buildAdminCloudflareRecordInput validates and converts the admin record form into a Cloudflare request.
func (s *AdminService) buildAdminCloudflareRecordInput(allocation model.AdminAllocationSummary, request UpsertAdminRecordRequest) (cloudflare.CreateDNSRecordInput, string, error) {
	recordType := strings.ToUpper(strings.TrimSpace(request.Type))
	if recordType == "MX" {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("MX records are reserved for the system-managed mail relay")
	}
	if !isSupportedRecordType(recordType) {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("unsupported dns record type")
	}

	relativeName, err := NormalizeRelativeRecordName(request.Name)
	if err != nil {
		return cloudflare.CreateDNSRecordInput{}, "", err
	}
	absoluteName := BuildAbsoluteName(relativeName, allocation.FQDN)
	normalizedContent, normalizedPriority, normalizedProxied, err := validateAndNormalizeRecordPayload(recordType, strings.TrimSpace(request.Content), request.Priority, request.Proxied)
	if err != nil {
		return cloudflare.CreateDNSRecordInput{}, "", err
	}

	ttl := request.TTL
	if ttl <= 0 {
		ttl = 1
	}
	return cloudflare.CreateDNSRecordInput{
		Type:     recordType,
		Name:     absoluteName,
		Content:  normalizedContent,
		TTL:      ttl,
		Proxied:  normalizedProxied,
		Comment:  strings.TrimSpace(request.Comment),
		Priority: normalizedPriority,
	}, relativeName, nil
}

// adminRecordFromCloudflare converts a Cloudflare record into the admin console row representation.
func adminRecordFromCloudflare(allocation model.AdminAllocationSummary, record cloudflare.DNSRecord, relativeName string) model.AdminDNSRecord {
	if strings.TrimSpace(relativeName) == "" {
		relativeName = RelativeNameFromAbsolute(record.Name, allocation.FQDN)
	}
	return model.AdminDNSRecord{
		AllocationID:     allocation.ID,
		OwnerUserID:      allocation.UserID,
		OwnerUsername:    allocation.OwnerUsername,
		OwnerDisplayName: allocation.OwnerDisplayName,
		RootDomain:       allocation.RootDomain,
		NamespaceFQDN:    allocation.FQDN,
		ID:               record.ID,
		Type:             record.Type,
		Name:             record.Name,
		RelativeName:     relativeName,
		Content:          record.Content,
		TTL:              record.TTL,
		Proxied:          record.Proxied,
		Comment:          record.Comment,
		Priority:         record.Priority,
	}
}

// findBestAllocationMatch assigns a DNS record to the most specific allocation namespace that contains it.
func findBestAllocationMatch(recordName string, allocations []model.AdminAllocationSummary) (model.AdminAllocationSummary, bool) {
	for _, allocation := range allocations {
		if BelongsToNamespace(recordName, allocation.FQDN) {
			return allocation, true
		}
	}
	return model.AdminAllocationSummary{}, false
}

// validateAdminAllocationWrite resolves the owner and root domain while
// normalizing the mutable administrator allocation payload.
func (s *AdminService) validateAdminAllocationWrite(ctx context.Context, ownerUserID int64, rootDomain string, prefix string, rawStatus string, rawSource string) (model.User, model.ManagedDomain, string, string, string, string, error) {
	owner, err := s.db.GetUserByID(ctx, ownerUserID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.User{}, model.ManagedDomain{}, "", "", "", "", NotFoundError("allocation owner not found")
		}
		return model.User{}, model.ManagedDomain{}, "", "", "", "", InternalError("failed to load allocation owner", err)
	}

	managedDomain, err := s.db.GetManagedDomainByRoot(ctx, strings.ToLower(strings.TrimSpace(rootDomain)))
	if err != nil {
		if storage.IsNotFound(err) {
			return model.User{}, model.ManagedDomain{}, "", "", "", "", NotFoundError("managed domain not found")
		}
		return model.User{}, model.ManagedDomain{}, "", "", "", "", InternalError("failed to load managed domain", err)
	}

	normalizedPrefix, err := NormalizePrefix(prefix)
	if err != nil {
		return model.User{}, model.ManagedDomain{}, "", "", "", "", ValidationError(err.Error())
	}

	status := normalizeAdminAllocationStatus(rawStatus)
	if status == "" {
		return model.User{}, model.ManagedDomain{}, "", "", "", "", ValidationError("allocation status must be active or disabled")
	}

	source := strings.TrimSpace(rawSource)
	if source == "" {
		source = "manual"
	}

	return owner, managedDomain, normalizedPrefix, normalizedPrefix + "." + managedDomain.RootDomain, status, source, nil
}

// normalizeAdminAllocationStatus restricts administrator allocation lifecycle
// writes to the states currently supported by the application.
func normalizeAdminAllocationStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "active":
		return "active"
	case "disabled":
		return "disabled"
	default:
		return ""
	}
}

// normalizeAdminApplicationStatus validates and normalizes moderation request states.
func normalizeAdminApplicationStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "pending":
		return "pending"
	case "approved":
		return "approved"
	case "rejected":
		return "rejected"
	default:
		return ""
	}
}

// normalizeRedeemType validates and normalizes redeem code types.
func normalizeRedeemType(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "single":
		return "single"
	case "multiple":
		return "multiple"
	case "wildcard":
		return "wildcard"
	default:
		return ""
	}
}

// generateRedeemCodeValue emits a short human-friendly code suitable for manual copy and paste.
func generateRedeemCodeValue() (string, error) {
	token, err := security.RandomToken(12)
	if err != nil {
		return "", err
	}
	sanitized := strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(token, "-", ""), "_", ""))
	if len(sanitized) < 12 {
		return "", fmt.Errorf("generated token was unexpectedly short")
	}
	return fmt.Sprintf("LDS-%s-%s", sanitized[:6], sanitized[6:12]), nil
}
