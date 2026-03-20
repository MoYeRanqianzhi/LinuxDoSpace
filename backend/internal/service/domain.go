package service

import (
	"context"
	"encoding/json"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// labelPattern 用于校验普通 DNS label。
var labelPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)

// builtInSaleManagedDomains defines the extra managed roots that should exist
// in every production deployment once Cloudflare can resolve their zone IDs.
var builtInSaleManagedDomains = []string{
	"cifang.love",
	"openapi.best",
	"metapi.cc",
}

const (
	// specialDNSRecordTypeEmailCatchAll is the synthetic record type exposed to
	// the public DNS panel. The frontend renders this as the human-facing
	// “邮箱泛解析” option instead of exposing the relay's real MX/TXT details.
	specialDNSRecordTypeEmailCatchAll = "EMAIL_CATCH_ALL"

	// syntheticCatchAllDNSRecordIDPrefix marks one DNS row as a panel-only
	// representation of the catch-all mailbox toggle rather than a literal
	// Cloudflare DNS record id.
	syntheticCatchAllDNSRecordIDPrefix = "synthetic:email-catch-all:"
)

// DomainService 承接根域名、命名空间分配与 DNS 记录管理业务。
type DomainService struct {
	cfg config.Config
	db  Store
	cf  CloudflareClient
}

// AvailabilityResult 描述某个前缀在某个根域名下是否可用。
type AvailabilityResult struct {
	RootDomain       string   `json:"root_domain"`
	Prefix           string   `json:"prefix"`
	NormalizedPrefix string   `json:"normalized_prefix"`
	FQDN             string   `json:"fqdn"`
	Available        bool     `json:"available"`
	Reasons          []string `json:"reasons"`
}

// DNSRecordInput 表示创建或更新 DNS 记录时可接受的业务输入。
type DNSRecordInput struct {
	Type     string `json:"type"`
	Name     string `json:"name"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Proxied  bool   `json:"proxied"`
	Comment  string `json:"comment"`
	Priority *int   `json:"priority,omitempty"`
}

// UpsertManagedDomainRequest 表示管理员写入根域名配置时需要的输入。
type UpsertManagedDomainRequest struct {
	RootDomain         string `json:"root_domain"`
	CloudflareZoneID   string `json:"cloudflare_zone_id"`
	DefaultQuota       int    `json:"default_quota"`
	AutoProvision      bool   `json:"auto_provision"`
	IsDefault          bool   `json:"is_default"`
	Enabled            bool   `json:"enabled"`
	SaleEnabled        bool   `json:"sale_enabled"`
	SaleBasePriceCents int64  `json:"sale_base_price_cents"`
}

// SetUserQuotaRequest 表示管理员写入用户配额覆盖时需要的输入。
type SetUserQuotaRequest struct {
	Username       string `json:"username"`
	RootDomain     string `json:"root_domain"`
	MaxAllocations int    `json:"max_allocations"`
	Reason         string `json:"reason"`
}

// NewDomainService 创建域名业务服务。
func NewDomainService(cfg config.Config, db Store, cf CloudflareClient) *DomainService {
	return &DomainService{
		cfg: cfg,
		db:  db,
		cf:  cf,
	}
}

// EnsureDefaultManagedDomain 根据配置自动引导默认根域名。
func (s *DomainService) EnsureDefaultManagedDomain(ctx context.Context) error {
	rootDomain := strings.ToLower(strings.TrimSpace(s.cfg.Cloudflare.DefaultRootDomain))
	if rootDomain == "" || !s.cfg.Cloudflare.AutoBootstrapDomain {
		return nil
	}
	return s.ensureManagedDomainBootstrap(ctx, rootDomain, storage.UpsertManagedDomainInput{
		RootDomain:         rootDomain,
		CloudflareZoneID:   s.cfg.Cloudflare.DefaultZoneID,
		DefaultQuota:       s.cfg.Cloudflare.DefaultUserQuota,
		AutoProvision:      true,
		IsDefault:          true,
		Enabled:            true,
		SaleEnabled:        false,
		SaleBasePriceCents: 1000,
	})
}

// EnsureBuiltInManagedDomains bootstraps the additional paid-sale root domains
// requested by the project so operators do not need to add them by hand after
// every fresh deployment.
func (s *DomainService) EnsureBuiltInManagedDomains(ctx context.Context) error {
	for _, rootDomain := range builtInSaleManagedDomains {
		normalizedRootDomain := strings.ToLower(strings.TrimSpace(rootDomain))
		if normalizedRootDomain == "" {
			continue
		}
		if _, err := s.db.GetManagedDomainByRoot(ctx, normalizedRootDomain); err == nil {
			continue
		} else if !storage.IsNotFound(err) {
			return err
		}

		if err := s.ensureManagedDomainBootstrap(ctx, normalizedRootDomain, storage.UpsertManagedDomainInput{
			RootDomain:         normalizedRootDomain,
			DefaultQuota:       1,
			AutoProvision:      false,
			IsDefault:          false,
			Enabled:            true,
			SaleEnabled:        false,
			SaleBasePriceCents: 1000,
		}); err != nil {
			// Built-in sale domains are optional bootstrap conveniences. When one
			// environment has not delegated the zone to Cloudflare yet, startup
			// should continue instead of taking the whole API offline.
			if statusErr, ok := err.(*Error); ok && statusErr.StatusCode == 503 {
				continue
			}
			return err
		}
	}
	return nil
}

// ListPublicDomains 返回启用中的根域名列表。
func (s *DomainService) ListPublicDomains(ctx context.Context) ([]model.ManagedDomain, error) {
	items, err := s.db.ListManagedDomains(ctx, false)
	if err != nil {
		return nil, InternalError("failed to load managed domains", err)
	}
	return items, nil
}

// ListAdminDomains 返回管理员视角下的全部根域名列表。
func (s *DomainService) ListAdminDomains(ctx context.Context) ([]model.ManagedDomain, error) {
	items, err := s.db.ListManagedDomains(ctx, true)
	if err != nil {
		return nil, InternalError("failed to load managed domains", err)
	}
	return items, nil
}

// ListPublicAllocationOwnerships 返回监督页需要的公开归属数据。
// 这里刻意只返回子域名和拥有者信息，不拼接任何 DNS 记录内容。
func (s *DomainService) ListPublicAllocationOwnerships(ctx context.Context) ([]model.PublicAllocationOwnership, error) {
	items, err := s.db.ListPublicAllocationOwnerships(ctx)
	if err != nil {
		return nil, InternalError("failed to load public allocation ownerships", err)
	}
	return items, nil
}

// CheckAvailability 检查某个前缀是否能在指定根域名下被分配。
func (s *DomainService) CheckAvailability(ctx context.Context, rootDomain string, prefix string) (AvailabilityResult, error) {
	managedDomain, normalizedPrefix, fqdn, err := s.prepareAllocation(ctx, rootDomain, prefix)
	if err != nil {
		return AvailabilityResult{}, err
	}

	result := AvailabilityResult{
		RootDomain:       managedDomain.RootDomain,
		Prefix:           strings.TrimSpace(prefix),
		NormalizedPrefix: normalizedPrefix,
		FQDN:             fqdn,
		Available:        true,
	}

	existing, err := s.db.FindAllocationByNormalizedPrefix(ctx, managedDomain.ID, normalizedPrefix)
	if err == nil && existing.ID > 0 {
		result.Available = false
		result.Reasons = append(result.Reasons, "reserved_in_database")
	}
	if err != nil && !storage.IsNotFound(err) {
		return AvailabilityResult{}, InternalError("failed to check allocation conflicts", err)
	}

	if result.Available {
		conflict, err := s.hasLiveConflict(ctx, managedDomain.CloudflareZoneID, fqdn)
		if err != nil {
			return AvailabilityResult{}, err
		}
		if conflict {
			result.Available = false
			result.Reasons = append(result.Reasons, "existing_dns_records")
		}
	}

	return result, nil
}

// AutoProvisionForUser 尝试为刚登录的用户自动分配 `<username>.<root_domain>`。
func (s *DomainService) AutoProvisionForUser(ctx context.Context, user model.User) error {
	managedDomains, err := s.db.ListManagedDomains(ctx, false)
	if err != nil {
		return InternalError("failed to load managed domains", err)
	}

	for _, managedDomain := range managedDomains {
		if !managedDomain.AutoProvision {
			continue
		}

		normalizedPrefix, err := NormalizePrefix(user.Username)
		if err != nil {
			continue
		}

		if _, err := s.db.FindAllocationByNormalizedPrefix(ctx, managedDomain.ID, normalizedPrefix); err == nil {
			continue
		} else if !storage.IsNotFound(err) {
			return InternalError("failed to check auto-provision conflicts", err)
		}

		currentCount, err := s.db.CountAllocationsByUserAndDomain(ctx, user.ID, managedDomain.ID)
		if err != nil {
			return InternalError("failed to count current allocations", err)
		}

		quota, err := s.db.GetEffectiveQuota(ctx, user.ID, managedDomain.ID)
		if err != nil {
			return InternalError("failed to load effective quota", err)
		}
		if currentCount >= quota {
			continue
		}

		fqdn := normalizedPrefix + "." + managedDomain.RootDomain
		conflict, err := s.hasLiveConflict(ctx, managedDomain.CloudflareZoneID, fqdn)
		if err != nil {
			return err
		}
		if conflict {
			continue
		}

		allocation, err := s.db.CreateAllocation(ctx, storage.CreateAllocationInput{
			UserID:           user.ID,
			ManagedDomainID:  managedDomain.ID,
			Prefix:           normalizedPrefix,
			NormalizedPrefix: normalizedPrefix,
			FQDN:             fqdn,
			IsPrimary:        currentCount == 0,
			Source:           "auto_provision",
			Status:           "active",
		})
		if err != nil {
			if isAllocationConflictError(err) {
				continue
			}
			return InternalError("failed to create auto-provision allocation", err)
		}

		metadata, _ := json.Marshal(map[string]any{
			"managed_domain_id": managedDomain.ID,
			"fqdn":              allocation.FQDN,
			"source":            allocation.Source,
		})
		if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
			ActorUserID:  &user.ID,
			Action:       "allocation.auto_provision",
			ResourceType: "allocation",
			ResourceID:   strconv.FormatInt(allocation.ID, 10),
			MetadataJSON: string(metadata),
		}); err != nil {
			return InternalError("failed to write auto-provision audit log", err)
		}
	}

	return nil
}

// ListAllocationsForUser 返回用户自己的所有分配。
func (s *DomainService) ListAllocationsForUser(ctx context.Context, userID int64) ([]model.Allocation, error) {
	items, err := s.db.ListAllocationsByUser(ctx, userID)
	if err != nil {
		return nil, InternalError("failed to list user allocations", err)
	}
	return items, nil
}

// ListVisibleAllocationsForUser 返回当前用户已经持有、且前端应展示的全部 allocation。
// 当前“只开放用户名同名前缀注册”的限制只作用在 CreateAllocation，不应影响管理员已经发放的命名空间展示。
func (s *DomainService) ListVisibleAllocationsForUser(ctx context.Context, user model.User) ([]model.Allocation, error) {
	return s.ListAllocationsForUser(ctx, user.ID)
}

// CreateAllocation 创建一条新的用户分配。
func (s *DomainService) CreateAllocation(ctx context.Context, user model.User, rootDomain string, prefix string, source string, primary bool) (model.Allocation, error) {
	managedDomain, normalizedPrefix, fqdn, err := s.prepareAllocation(ctx, rootDomain, prefix)
	if err != nil {
		return model.Allocation{}, err
	}
	if err := ensureTemporaryFreeRegistrationEligibility(user, managedDomain, normalizedPrefix); err != nil {
		return model.Allocation{}, err
	}

	if _, err := s.db.FindAllocationByNormalizedPrefix(ctx, managedDomain.ID, normalizedPrefix); err == nil {
		return model.Allocation{}, ConflictError("the requested prefix has already been reserved")
	} else if !storage.IsNotFound(err) {
		return model.Allocation{}, InternalError("failed to check allocation uniqueness", err)
	}

	count, err := s.db.CountAllocationsByUserAndDomain(ctx, user.ID, managedDomain.ID)
	if err != nil {
		return model.Allocation{}, InternalError("failed to count existing allocations", err)
	}

	quota, err := s.db.GetEffectiveQuota(ctx, user.ID, managedDomain.ID)
	if err != nil {
		return model.Allocation{}, InternalError("failed to load effective quota", err)
	}

	if count >= quota {
		return model.Allocation{}, ForbiddenError("allocation quota exceeded")
	}

	conflict, err := s.hasLiveConflict(ctx, managedDomain.CloudflareZoneID, fqdn)
	if err != nil {
		return model.Allocation{}, err
	}
	if conflict {
		return model.Allocation{}, ConflictError("the requested namespace already has live dns records")
	}

	allocation, err := s.db.CreateAllocation(ctx, storage.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           strings.TrimSpace(prefix),
		NormalizedPrefix: normalizedPrefix,
		FQDN:             fqdn,
		IsPrimary:        primary || count == 0,
		Source:           firstNonEmpty(strings.TrimSpace(source), "manual"),
		Status:           "active",
	})
	if err != nil {
		if isAllocationConflictError(err) {
			return model.Allocation{}, ConflictError("the requested allocation already exists or changed concurrently")
		}
		return model.Allocation{}, InternalError("failed to create allocation", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"managed_domain_id": managedDomain.ID,
		"fqdn":              allocation.FQDN,
		"source":            allocation.Source,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "allocation.create",
		ResourceType: "allocation",
		ResourceID:   strconv.FormatInt(allocation.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.Allocation{}, InternalError("failed to write allocation audit log", err)
	}

	return allocation, nil
}

// ListRecordsForAllocation 返回某个用户命名空间下的全部 Cloudflare DNS 记录。
func (s *DomainService) ListRecordsForAllocation(ctx context.Context, user model.User, allocationID int64) ([]model.DNSRecord, error) {
	allocation, err := s.db.GetAllocationByIDForUser(ctx, allocationID, user.ID)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil, NotFoundError("allocation not found")
		}
		return nil, InternalError("failed to load allocation", err)
	}

	records, err := s.listNamespaceRecords(ctx, allocation)
	if err != nil {
		return nil, err
	}
	if catchAllRecord, catchAllErr := s.buildCatchAllSyntheticDNSRecord(ctx, user, allocation); catchAllErr != nil {
		return nil, catchAllErr
	} else if catchAllRecord != nil {
		records = append(records, *catchAllRecord)
	}

	sort.Slice(records, func(i int, j int) bool {
		if records[i].RelativeName == "@" && records[j].RelativeName != "@" {
			return true
		}
		if records[i].RelativeName != "@" && records[j].RelativeName == "@" {
			return false
		}
		if records[i].Name == records[j].Name {
			return records[i].Type < records[j].Type
		}
		return records[i].Name < records[j].Name
	})

	return records, nil
}

// CreateRecord 在用户命名空间下创建一条 DNS 记录。
func (s *DomainService) CreateRecord(ctx context.Context, user model.User, allocationID int64, input DNSRecordInput) (model.DNSRecord, error) {
	allocation, err := s.db.GetAllocationByIDForUser(ctx, allocationID, user.ID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.DNSRecord{}, NotFoundError("allocation not found")
		}
		return model.DNSRecord{}, InternalError("failed to load allocation", err)
	}
	if isSpecialCatchAllDNSRecordType(input.Type) {
		return s.enableCatchAllSyntheticDNSRecord(ctx, user, allocation, input)
	}
	if err := s.ensureWebsiteRootRecordDoesNotConflictWithCatchAll(ctx, user, allocation, input.Name, input.Type); err != nil {
		return model.DNSRecord{}, err
	}

	createInput, relativeName, err := s.buildCloudflareRecordInput(allocation, input)
	if err != nil {
		return model.DNSRecord{}, err
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return model.DNSRecord{}, UnavailableError("cloudflare integration is not configured", nil)
	}

	created, err := s.cf.CreateDNSRecord(ctx, allocation.CloudflareZoneID, createInput)
	if err != nil {
		return model.DNSRecord{}, UnavailableError("failed to create cloudflare dns record", err)
	}

	record := toModelDNSRecord(created, allocation.FQDN, relativeName)
	metadata, _ := json.Marshal(map[string]any{
		"record_id":     record.ID,
		"allocation_id": allocation.ID,
		"name":          record.Name,
		"type":          record.Type,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "dns_record.create",
		ResourceType: "dns_record",
		ResourceID:   record.ID,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.DNSRecord{}, InternalError("failed to write dns record create audit log", err)
	}

	return record, nil
}

// UpdateRecord 在用户命名空间下更新一条 DNS 记录。
func (s *DomainService) UpdateRecord(ctx context.Context, user model.User, allocationID int64, recordID string, input DNSRecordInput) (model.DNSRecord, error) {
	allocation, err := s.db.GetAllocationByIDForUser(ctx, allocationID, user.ID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.DNSRecord{}, NotFoundError("allocation not found")
		}
		return model.DNSRecord{}, InternalError("failed to load allocation", err)
	}
	if isSyntheticCatchAllDNSRecordID(recordID) || isSpecialCatchAllDNSRecordType(input.Type) {
		return s.enableCatchAllSyntheticDNSRecord(ctx, user, allocation, input)
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return model.DNSRecord{}, UnavailableError("cloudflare integration is not configured", nil)
	}

	existing, err := s.cf.GetDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID))
	if err != nil {
		return model.DNSRecord{}, UnavailableError("failed to load cloudflare dns record", err)
	}
	if !BelongsToNamespace(existing.Name, allocation.FQDN) {
		return model.DNSRecord{}, ForbiddenError("the selected dns record does not belong to your namespace")
	}
	if err := s.ensureWebsiteRootRecordDoesNotConflictWithCatchAll(ctx, user, allocation, input.Name, input.Type); err != nil {
		return model.DNSRecord{}, err
	}

	updateInput, relativeName, err := s.buildCloudflareRecordInput(allocation, input)
	if err != nil {
		return model.DNSRecord{}, err
	}

	updated, err := s.cf.UpdateDNSRecord(ctx, allocation.CloudflareZoneID, recordID, updateInput)
	if err != nil {
		return model.DNSRecord{}, UnavailableError("failed to update cloudflare dns record", err)
	}

	record := toModelDNSRecord(updated, allocation.FQDN, relativeName)
	metadata, _ := json.Marshal(map[string]any{
		"record_id":     record.ID,
		"allocation_id": allocation.ID,
		"name":          record.Name,
		"type":          record.Type,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "dns_record.update",
		ResourceType: "dns_record",
		ResourceID:   record.ID,
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.DNSRecord{}, InternalError("failed to write dns record update audit log", err)
	}

	return record, nil
}

// DeleteRecord 删除用户命名空间下的一条 DNS 记录。
func (s *DomainService) DeleteRecord(ctx context.Context, user model.User, allocationID int64, recordID string) error {
	allocation, err := s.db.GetAllocationByIDForUser(ctx, allocationID, user.ID)
	if err != nil {
		if storage.IsNotFound(err) {
			return NotFoundError("allocation not found")
		}
		return InternalError("failed to load allocation", err)
	}
	if isSyntheticCatchAllDNSRecordID(recordID) {
		return s.disableCatchAllSyntheticDNSRecord(ctx, user, allocation)
	}
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return UnavailableError("cloudflare integration is not configured", nil)
	}

	existing, err := s.cf.GetDNSRecord(ctx, allocation.CloudflareZoneID, strings.TrimSpace(recordID))
	if err != nil {
		return UnavailableError("failed to load cloudflare dns record", err)
	}
	if !BelongsToNamespace(existing.Name, allocation.FQDN) {
		return ForbiddenError("the selected dns record does not belong to your namespace")
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
		ActorUserID:  &user.ID,
		Action:       "dns_record.delete",
		ResourceType: "dns_record",
		ResourceID:   strings.TrimSpace(recordID),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write dns record delete audit log", err)
	}

	return nil
}

// buildCatchAllSyntheticDNSRecord returns the privacy-safe synthetic DNS row
// that represents one enabled namespace-wide mailbox catch-all inside the DNS
// console, without leaking the real relay MX/TXT values or forwarding target.
func (s *DomainService) buildCatchAllSyntheticDNSRecord(ctx context.Context, user model.User, allocation model.Allocation) (*model.DNSRecord, error) {
	if !s.allocationCanToggleCatchAll(user, allocation) {
		return nil, nil
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, allocation.FQDN, emailCatchAllPrefix)
	if err != nil {
		if storage.IsNotFound(err) {
			return nil, nil
		}
		return nil, InternalError("failed to load catch-all email route for dns panel", err)
	}
	if route.OwnerUserID != user.ID {
		return nil, UnavailableError("catch-all mailbox is assigned to another user", nil)
	}
	if !route.Enabled || strings.TrimSpace(route.TargetEmail) == "" {
		return nil, nil
	}

	return &model.DNSRecord{
		ID:           syntheticCatchAllDNSRecordIDPrefix + strconv.FormatInt(allocation.ID, 10),
		Type:         specialDNSRecordTypeEmailCatchAll,
		Name:         allocation.FQDN,
		RelativeName: "@",
		Content:      "邮箱泛解析",
		TTL:          0,
		Proxied:      false,
		Comment:      "",
	}, nil
}

// enableCatchAllSyntheticDNSRecord turns on the namespace-wide catch-all route
// behind the synthetic DNS panel record. The real relay MX/TXT remains hidden
// from the user-facing panel.
func (s *DomainService) enableCatchAllSyntheticDNSRecord(ctx context.Context, user model.User, allocation model.Allocation, input DNSRecordInput) (model.DNSRecord, error) {
	relativeName, err := NormalizeRelativeRecordName(input.Name)
	if err != nil {
		return model.DNSRecord{}, ValidationError(err.Error())
	}
	if relativeName != "@" {
		return model.DNSRecord{}, ValidationError("邮箱泛解析记录只能创建在命名空间根 @ 上")
	}
	if !s.allocationCanToggleCatchAll(user, allocation) {
		return model.DNSRecord{}, ForbiddenError("当前命名空间不支持通过 DNS 面板启用邮箱泛解析")
	}

	approved, err := s.isCatchAllPermissionApprovedForAllocation(ctx, user, allocation)
	if err != nil {
		return model.DNSRecord{}, err
	}
	if !approved {
		return model.DNSRecord{}, ForbiddenError("请先通过邮箱泛解析权限审核，再在这里启用该特殊记录")
	}
	if err := s.ensureCatchAllRootHasNoWebsiteRecord(ctx, allocation, ""); err != nil {
		return model.DNSRecord{}, err
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, allocation.FQDN, emailCatchAllPrefix)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.DNSRecord{}, ConflictError("请先在邮箱分发页面配置并保存邮箱泛解析的目标邮箱，然后再回来启用该特殊记录")
		}
		return model.DNSRecord{}, InternalError("failed to load catch-all email route for dns panel enable", err)
	}
	if route.OwnerUserID != user.ID {
		return model.DNSRecord{}, UnavailableError("catch-all mailbox is assigned to another user", nil)
	}
	if !routeHasConfiguredTarget(route) {
		return model.DNSRecord{}, ConflictError("请先在邮箱分发页面配置并保存邮箱泛解析的目标邮箱，然后再回来启用该特殊记录")
	}

	if err := ensureDatabaseRelayIngressDNSForRootDomain(ctx, s.cfg, s.cf, allocation.FQDN); err != nil {
		return model.DNSRecord{}, err
	}

	beforeState := newCatchAllEmailRouteSyncState(route.RootDomain, route.TargetEmail, route.Enabled)
	afterState := newCatchAllEmailRouteSyncState(route.RootDomain, route.TargetEmail, true)
	updated := route
	if !route.Enabled {
		if err := newEmailRoutingProvisioner(s.cfg, s.cf).SyncForwardingState(ctx, beforeState, afterState, func() error {
			var persistErr error
			updated, persistErr = s.db.UpdateEmailRoute(ctx, storage.UpdateEmailRouteInput{
				ID:                  route.ID,
				TargetEmail:         route.TargetEmail,
				TargetKind:          route.TargetKind,
				TargetTokenPublicID: route.TargetTokenPublicID,
				Enabled:             true,
			})
			if persistErr != nil {
				return InternalError("failed to enable catch-all email route from dns panel", persistErr)
			}
			return nil
		}); err != nil {
			return model.DNSRecord{}, err
		}
	}

	record, recordErr := s.buildCatchAllSyntheticDNSRecord(ctx, user, allocation)
	if recordErr != nil {
		return model.DNSRecord{}, recordErr
	}
	if record == nil {
		return model.DNSRecord{}, InternalError("failed to build synthetic catch-all dns record", nil)
	}

	metadata, _ := json.Marshal(map[string]any{
		"allocation_id":  allocation.ID,
		"root_domain":    allocation.FQDN,
		"email_route_id": updated.ID,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "dns_record.catch_all.enable",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.DNSRecord{}, InternalError("failed to write synthetic catch-all dns enable audit log", err)
	}

	return *record, nil
}

// disableCatchAllSyntheticDNSRecord turns off the namespace-wide catch-all
// route represented by the synthetic DNS panel record and frees the hidden
// relay MX/TXT immediately so the namespace can switch back to other uses.
func (s *DomainService) disableCatchAllSyntheticDNSRecord(ctx context.Context, user model.User, allocation model.Allocation) error {
	if !s.allocationCanToggleCatchAll(user, allocation) {
		return ForbiddenError("当前命名空间不支持通过 DNS 面板管理邮箱泛解析")
	}

	route, err := s.db.GetEmailRouteByAddress(ctx, allocation.FQDN, emailCatchAllPrefix)
	if err != nil {
		if storage.IsNotFound(err) {
			return NotFoundError("邮箱泛解析记录不存在")
		}
		return InternalError("failed to load catch-all email route for dns panel disable", err)
	}
	if route.OwnerUserID != user.ID {
		return UnavailableError("catch-all mailbox is assigned to another user", nil)
	}
	if !route.Enabled {
		return NotFoundError("邮箱泛解析记录不存在")
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
			return InternalError("failed to disable catch-all email route from dns panel", persistErr)
		}
		return nil
	}); err != nil {
		return err
	}
	if err := deleteDatabaseRelayIngressDNSForRootDomain(ctx, s.cfg, s.cf, allocation.FQDN); err != nil {
		return err
	}

	metadata, _ := json.Marshal(map[string]any{
		"allocation_id":  allocation.ID,
		"root_domain":    allocation.FQDN,
		"email_route_id": updated.ID,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "dns_record.catch_all.disable",
		ResourceType: "email_route",
		ResourceID:   strconv.FormatInt(updated.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return InternalError("failed to write synthetic catch-all dns disable audit log", err)
	}

	return nil
}

// allocationCanToggleCatchAll keeps the DNS-panel shortcut narrow: it only
// applies to the user's default same-name namespace where the current public
// catch-all permission model already exists.
func (s *DomainService) allocationCanToggleCatchAll(user model.User, allocation model.Allocation) bool {
	defaultRoot := normalizeDNSName(s.cfg.Cloudflare.DefaultRootDomain)
	if defaultRoot == "" || normalizeDNSName(allocation.RootDomain) != defaultRoot {
		return false
	}
	allowedPrefix, err := normalizedUserPrefix(user.Username)
	if err != nil {
		return false
	}
	return normalizeDNSName(allocation.FQDN) == normalizeDNSName(allowedPrefix+"."+defaultRoot)
}

// isCatchAllPermissionApprovedForAllocation resolves whether the current user
// already holds the approved catch-all permission for the selected namespace.
func (s *DomainService) isCatchAllPermissionApprovedForAllocation(ctx context.Context, user model.User, allocation model.Allocation) (bool, error) {
	applications, err := s.db.ListAdminApplicationsByApplicant(ctx, user.ID)
	if err != nil {
		return false, InternalError("failed to load catch-all permission applications for dns panel", err)
	}

	application := findPermissionApplication(
		applications,
		PermissionKeyEmailCatchAll,
		buildCatchAllEmailRouteAddress(allocation.FQDN),
		buildLegacyCatchAllApplicationTarget(allocation.FQDN),
	)
	return application != nil && strings.EqualFold(strings.TrimSpace(application.Status), "approved"), nil
}

// ensureWebsiteRootRecordDoesNotConflictWithCatchAll forbids new root A/AAAA/CNAME
// records while the special catch-all record is active for the namespace.
func (s *DomainService) ensureWebsiteRootRecordDoesNotConflictWithCatchAll(ctx context.Context, user model.User, allocation model.Allocation, rawName string, recordType string) error {
	if !isWebsiteRootRecordType(recordType) {
		return nil
	}
	relativeName, err := NormalizeRelativeRecordName(rawName)
	if err != nil {
		return ValidationError(err.Error())
	}
	if relativeName != "@" {
		return nil
	}

	record, catchAllErr := s.buildCatchAllSyntheticDNSRecord(ctx, user, allocation)
	if catchAllErr != nil {
		return catchAllErr
	}
	if record != nil {
		return ConflictError("当前命名空间已启用邮箱泛解析，请先删除“邮箱泛解析”特殊记录后再设置根网站解析")
	}
	return nil
}

// ensureCatchAllRootHasNoWebsiteRecord checks whether the namespace root is
// currently occupied by one website-style root record. Users must clear those
// records before turning the namespace into a mailbox catch-all.
func (s *DomainService) ensureCatchAllRootHasNoWebsiteRecord(ctx context.Context, allocation model.Allocation, excludedRecordID string) error {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return UnavailableError("cloudflare integration is not configured", nil)
	}

	records, err := s.cf.ListAllDNSRecords(ctx, allocation.CloudflareZoneID)
	if err != nil {
		return UnavailableError("failed to list cloudflare dns records", err)
	}
	for _, item := range records {
		if strings.TrimSpace(excludedRecordID) != "" && strings.TrimSpace(item.ID) == strings.TrimSpace(excludedRecordID) {
			continue
		}
		if normalizeDNSName(item.Name) != normalizeDNSName(allocation.FQDN) {
			continue
		}
		if !isWebsiteRootRecordType(item.Type) {
			continue
		}
		return ConflictError("当前命名空间根记录已用于网站或其他根解析，请先删除 @ 的 A/AAAA/CNAME 记录后再启用邮箱泛解析")
	}
	return nil
}

// isWebsiteRootRecordType enumerates the record families that would occupy the
// namespace root for website-style usage and therefore conflict with the
// mailbox-catch-all shortcut exposed in the DNS panel.
func isWebsiteRootRecordType(recordType string) bool {
	switch strings.ToUpper(strings.TrimSpace(recordType)) {
	case "A", "AAAA", "CNAME":
		return true
	default:
		return false
	}
}

// isSpecialCatchAllDNSRecordType checks whether the requested type is the
// synthetic DNS-panel-only catch-all switch.
func isSpecialCatchAllDNSRecordType(recordType string) bool {
	return strings.EqualFold(strings.TrimSpace(recordType), specialDNSRecordTypeEmailCatchAll)
}

// isSyntheticCatchAllDNSRecordID checks whether one row id belongs to the
// synthetic DNS-panel representation rather than a real Cloudflare record id.
func isSyntheticCatchAllDNSRecordID(recordID string) bool {
	return strings.HasPrefix(strings.TrimSpace(recordID), syntheticCatchAllDNSRecordIDPrefix)
}

// UpsertManagedDomain 允许管理员创建或更新一个可分发根域名。
func (s *DomainService) UpsertManagedDomain(ctx context.Context, actor model.User, request UpsertManagedDomainRequest) (model.ManagedDomain, error) {
	rootDomain := strings.ToLower(strings.TrimSpace(request.RootDomain))
	if !isValidRootDomain(rootDomain) {
		return model.ManagedDomain{}, ValidationError("root_domain must be a valid DNS name")
	}

	if request.DefaultQuota < 1 {
		return model.ManagedDomain{}, ValidationError("default_quota must be at least 1")
	}
	if request.SaleBasePriceCents < 0 {
		return model.ManagedDomain{}, ValidationError("sale_base_price_cents must not be negative")
	}
	if request.SaleEnabled && request.SaleBasePriceCents <= 0 {
		return model.ManagedDomain{}, ValidationError("sale_base_price_cents must be greater than 0 when sales are enabled")
	}

	zoneID := strings.TrimSpace(request.CloudflareZoneID)
	if zoneID == "" {
		if s.cf == nil || !s.cfg.CloudflareConfigured() {
			return model.ManagedDomain{}, UnavailableError("cloudflare integration is not configured", nil)
		}
		resolved, err := s.cf.ResolveZoneID(ctx, rootDomain)
		if err != nil {
			return model.ManagedDomain{}, UnavailableError("failed to resolve cloudflare zone id", err)
		}
		zoneID = resolved
	}

	item, err := s.db.UpsertManagedDomain(ctx, storage.UpsertManagedDomainInput{
		RootDomain:         rootDomain,
		CloudflareZoneID:   zoneID,
		DefaultQuota:       request.DefaultQuota,
		AutoProvision:      request.AutoProvision,
		IsDefault:          request.IsDefault,
		Enabled:            request.Enabled,
		SaleEnabled:        request.SaleEnabled,
		SaleBasePriceCents: request.SaleBasePriceCents,
	})
	if err != nil {
		return model.ManagedDomain{}, InternalError("failed to upsert managed domain", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"managed_domain_id":  item.ID,
		"root_domain":        item.RootDomain,
		"cloudflare_zone_id": item.CloudflareZoneID,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "managed_domain.upsert",
		ResourceType: "managed_domain",
		ResourceID:   strconv.FormatInt(item.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.ManagedDomain{}, InternalError("failed to write managed domain audit log", err)
	}

	return item, nil
}

// ensureManagedDomainBootstrap keeps startup seeding logic small and explicit:
// resolve one root-domain zone ID when missing, then upsert the managed-domain row.
func (s *DomainService) ensureManagedDomainBootstrap(ctx context.Context, rootDomain string, input storage.UpsertManagedDomainInput) error {
	normalizedRootDomain := strings.ToLower(strings.TrimSpace(rootDomain))
	if normalizedRootDomain == "" {
		return nil
	}

	zoneID := strings.TrimSpace(input.CloudflareZoneID)
	if zoneID == "" {
		if s.cf == nil || !s.cfg.CloudflareConfigured() {
			return nil
		}

		resolved, err := s.cf.ResolveZoneID(ctx, normalizedRootDomain)
		if err != nil {
			return UnavailableError("failed to resolve cloudflare zone id", err)
		}
		zoneID = resolved
	}

	_, err := s.db.UpsertManagedDomain(ctx, storage.UpsertManagedDomainInput{
		RootDomain:         normalizedRootDomain,
		CloudflareZoneID:   zoneID,
		DefaultQuota:       input.DefaultQuota,
		AutoProvision:      input.AutoProvision,
		IsDefault:          input.IsDefault,
		Enabled:            input.Enabled,
		SaleEnabled:        input.SaleEnabled,
		SaleBasePriceCents: input.SaleBasePriceCents,
	})
	if err != nil {
		return InternalError("failed to bootstrap managed domain", err)
	}

	return nil
}

// SetUserQuota 允许管理员调整某个用户在某个根域名上的可分配数量。
func (s *DomainService) SetUserQuota(ctx context.Context, actor model.User, request SetUserQuotaRequest) (model.UserDomainQuota, error) {
	if request.MaxAllocations < 1 {
		return model.UserDomainQuota{}, ValidationError("max_allocations must be at least 1")
	}

	user, err := s.db.GetUserByUsername(ctx, request.Username)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.UserDomainQuota{}, NotFoundError("target user not found")
		}
		return model.UserDomainQuota{}, InternalError("failed to load target user", err)
	}

	managedDomain, err := s.db.GetManagedDomainByRoot(ctx, request.RootDomain)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.UserDomainQuota{}, NotFoundError("managed domain not found")
		}
		return model.UserDomainQuota{}, InternalError("failed to load managed domain", err)
	}

	quota, err := s.db.SetUserQuota(ctx, storage.SetUserQuotaInput{
		UserID:          user.ID,
		ManagedDomainID: managedDomain.ID,
		MaxAllocations:  request.MaxAllocations,
		Reason:          strings.TrimSpace(request.Reason),
	})
	if err != nil {
		return model.UserDomainQuota{}, InternalError("failed to set user quota", err)
	}

	metadata, _ := json.Marshal(map[string]any{
		"user_id":           user.ID,
		"managed_domain_id": managedDomain.ID,
		"max_allocations":   quota.MaxAllocations,
	})
	if err := s.db.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "quota.set",
		ResourceType: "user_domain_quota",
		ResourceID:   strconv.FormatInt(quota.ID, 10),
		MetadataJSON: string(metadata),
	}); err != nil {
		return model.UserDomainQuota{}, InternalError("failed to write quota audit log", err)
	}

	return quota, nil
}

// prepareAllocation 校验根域名和前缀，并返回标准化后的结果。
func (s *DomainService) prepareAllocation(ctx context.Context, rootDomain string, prefix string) (model.ManagedDomain, string, string, error) {
	managedDomain, err := s.db.GetManagedDomainByRoot(ctx, rootDomain)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.ManagedDomain{}, "", "", NotFoundError("managed domain not found")
		}
		return model.ManagedDomain{}, "", "", InternalError("failed to load managed domain", err)
	}
	if !managedDomain.Enabled {
		return model.ManagedDomain{}, "", "", ForbiddenError("managed domain is disabled")
	}

	normalizedPrefix, err := NormalizePrefix(prefix)
	if err != nil {
		return model.ManagedDomain{}, "", "", ValidationError(err.Error())
	}

	return managedDomain, normalizedPrefix, normalizedPrefix + "." + managedDomain.RootDomain, nil
}

// hasLiveConflict 检查 Cloudflare 上是否已经存在与该命名空间冲突的记录。
func (s *DomainService) hasLiveConflict(ctx context.Context, zoneID string, fqdn string) (bool, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return false, nil
	}

	records, err := s.cf.ListAllDNSRecords(ctx, zoneID)
	if err != nil {
		return false, UnavailableError("failed to list cloudflare dns records", err)
	}

	for _, record := range records {
		if BelongsToNamespace(record.Name, fqdn) {
			return true, nil
		}
	}

	return false, nil
}

// listNamespaceRecords 列出某个分配命名空间中的全部实时 DNS 记录。
// 这里会返回命名空间根记录以及所有子记录，例如 `@`、`www`、`api.v2`。
func (s *DomainService) listNamespaceRecords(ctx context.Context, allocation model.Allocation) ([]model.DNSRecord, error) {
	if s.cf == nil || !s.cfg.CloudflareConfigured() {
		return nil, UnavailableError("cloudflare integration is not configured", nil)
	}

	records, err := s.cf.ListAllDNSRecords(ctx, allocation.CloudflareZoneID)
	if err != nil {
		return nil, UnavailableError("failed to list cloudflare dns records", err)
	}

	filtered := make([]model.DNSRecord, 0, len(records))
	for _, record := range records {
		if !BelongsToNamespace(record.Name, allocation.FQDN) {
			continue
		}
		// Hide LinuxDoSpace-managed relay ingress records from the end-user DNS
		// panel. These MX/TXT rows are system infrastructure, not user-editable
		// namespace records.
		if strings.EqualFold(strings.TrimSpace(record.Comment), strings.TrimSpace(databaseRelayManagedDNSComment)) {
			continue
		}
		filtered = append(filtered, toModelDNSRecord(record, allocation.FQDN, RelativeNameFromAbsolute(record.Name, allocation.FQDN)))
	}

	sort.Slice(filtered, func(i int, j int) bool {
		if filtered[i].RelativeName == "@" && filtered[j].RelativeName != "@" {
			return true
		}
		if filtered[i].RelativeName != "@" && filtered[j].RelativeName == "@" {
			return false
		}
		if filtered[i].Name == filtered[j].Name {
			return filtered[i].Type < filtered[j].Type
		}
		return filtered[i].Name < filtered[j].Name
	})

	return filtered, nil
}

// buildCloudflareRecordInput 校验并转换业务记录输入。
func (s *DomainService) buildCloudflareRecordInput(allocation model.Allocation, input DNSRecordInput) (cloudflare.CreateDNSRecordInput, string, error) {
	normalizedType := strings.ToUpper(strings.TrimSpace(input.Type))
	if normalizedType == "MX" {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("MX records are reserved for the system-managed mail relay")
	}
	if !isSupportedRecordType(normalizedType) {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("unsupported dns record type")
	}

	relativeName, err := NormalizeRelativeRecordName(input.Name)
	if err != nil {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError(err.Error())
	}

	fullName := BuildAbsoluteName(relativeName, allocation.FQDN)
	if !BelongsToNamespace(fullName, allocation.FQDN) {
		return cloudflare.CreateDNSRecordInput{}, "", ForbiddenError("record name escapes the allocated namespace")
	}

	ttl := input.TTL
	if ttl == 0 {
		ttl = 1
	}
	if ttl != 1 && ttl < 60 {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("ttl must be 1 (auto) or at least 60 seconds")
	}

	content, priority, proxied, err := validateAndNormalizeRecordPayload(normalizedType, strings.TrimSpace(input.Content), input.Priority, input.Proxied)
	if err != nil {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError(err.Error())
	}
	if strings.EqualFold(strings.TrimSpace(input.Comment), strings.TrimSpace(databaseRelayManagedDNSComment)) {
		return cloudflare.CreateDNSRecordInput{}, "", ValidationError("record comment uses a reserved system marker")
	}

	return cloudflare.CreateDNSRecordInput{
		Type:     normalizedType,
		Name:     fullName,
		Content:  content,
		TTL:      ttl,
		Proxied:  proxied,
		Comment:  strings.TrimSpace(input.Comment),
		Priority: priority,
	}, relativeName, nil
}

// NormalizePrefix 把用户输入的前缀标准化为 DNS label。
func NormalizePrefix(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return "", ValidationError("prefix is required")
	}

	var builder strings.Builder
	lastWasDash := false
	for _, runeValue := range value {
		switch {
		case runeValue >= 'a' && runeValue <= 'z':
			builder.WriteRune(runeValue)
			lastWasDash = false
		case runeValue >= '0' && runeValue <= '9':
			builder.WriteRune(runeValue)
			lastWasDash = false
		default:
			if !lastWasDash {
				builder.WriteRune('-')
				lastWasDash = true
			}
		}
	}

	normalized := strings.Trim(builder.String(), "-")
	if len(normalized) == 0 {
		return "", ValidationError("prefix does not contain any valid dns characters")
	}
	if len(normalized) > 63 {
		normalized = normalized[:63]
		normalized = strings.TrimRight(normalized, "-")
	}
	if !labelPattern.MatchString(normalized) {
		return "", ValidationError("prefix must be a valid dns label")
	}

	return normalized, nil
}

// normalizedUserPrefix 把 Linux Do 用户名标准化成平台当前允许申请的子域名前缀。
func normalizedUserPrefix(username string) (string, error) {
	return NormalizePrefix(username)
}

// ensureTemporaryFreeRegistrationEligibility keeps the temporary self-service
// registration path narrow: the prefix must match the current username, and the
// selected root domain must explicitly opt into the free same-name flow.
func ensureTemporaryFreeRegistrationEligibility(user model.User, managedDomain model.ManagedDomain, normalizedPrefix string) error {
	allowedPrefix, err := normalizedUserPrefix(user.Username)
	if err != nil {
		return ForbiddenError("your linux do username cannot be mapped to a valid dns label")
	}
	if normalizedPrefix != allowedPrefix {
		return ForbiddenError("temporary policy only allows the subdomain that exactly matches your username")
	}
	if !managedDomain.AutoProvision {
		return ForbiddenError("the selected root domain is not open for the temporary free registration flow")
	}
	return nil
}

// NormalizeRelativeRecordName 校验用户在命名空间内部填写的记录相对名称。
func NormalizeRelativeRecordName(raw string) (string, error) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || value == "@" {
		return "@", nil
	}
	if strings.HasSuffix(value, ".") {
		return "", ValidationError("record name must not end with a dot")
	}

	labels := strings.Split(value, ".")
	for _, label := range labels {
		if label == "*" {
			continue
		}
		if !labelPattern.MatchString(label) {
			return "", ValidationError("record name contains an invalid label")
		}
	}

	return value, nil
}

// BuildAbsoluteName 把相对名称和命名空间根拼接成绝对名称。
func BuildAbsoluteName(relativeName string, namespaceFQDN string) string {
	if relativeName == "@" {
		return strings.ToLower(strings.TrimSpace(namespaceFQDN))
	}
	return strings.ToLower(strings.TrimSpace(relativeName)) + "." + strings.ToLower(strings.TrimSpace(namespaceFQDN))
}

// BelongsToNamespace 判断一条绝对记录名是否属于某个分配命名空间。
func BelongsToNamespace(recordName string, namespaceFQDN string) bool {
	name := strings.ToLower(strings.TrimSpace(recordName))
	namespace := strings.ToLower(strings.TrimSpace(namespaceFQDN))
	return name == namespace || strings.HasSuffix(name, "."+namespace)
}

// RelativeNameFromAbsolute 从绝对名称反推用户视角下的相对名称。
func RelativeNameFromAbsolute(recordName string, namespaceFQDN string) string {
	name := strings.ToLower(strings.TrimSpace(recordName))
	namespace := strings.ToLower(strings.TrimSpace(namespaceFQDN))
	if name == namespace {
		return "@"
	}
	return strings.TrimSuffix(name, "."+namespace)
}

// isAllocationConflictError collapses backend-specific uniqueness failures into
// one deterministic conflict outcome for callers. This now also covers the
// partial unique index that enforces the single-primary invariant.
func isAllocationConflictError(err error) bool {
	if err == nil {
		return false
	}
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(normalized, "unique") || strings.Contains(normalized, "constraint failed")
}

// toModelDNSRecord 把 Cloudflare 记录转换为对外返回的模型。
func toModelDNSRecord(record cloudflare.DNSRecord, namespace string, relativeName string) model.DNSRecord {
	if strings.TrimSpace(relativeName) == "" {
		relativeName = RelativeNameFromAbsolute(record.Name, namespace)
	}
	return model.DNSRecord{
		ID:           record.ID,
		Type:         record.Type,
		Name:         record.Name,
		RelativeName: relativeName,
		Content:      record.Content,
		TTL:          record.TTL,
		Proxied:      record.Proxied,
		Comment:      record.Comment,
		Priority:     record.Priority,
	}
}

// validateAndNormalizeRecordPayload 根据记录类型校验内容、优先级和代理开关。
func validateAndNormalizeRecordPayload(recordType string, content string, priority *int, proxied bool) (string, *int, bool, error) {
	switch recordType {
	case "A":
		ip := net.ParseIP(content)
		if ip == nil || ip.To4() == nil {
			return "", nil, false, ValidationError("A record content must be a valid IPv4 address")
		}
		return ip.String(), nil, proxied, nil
	case "AAAA":
		ip := net.ParseIP(content)
		if ip == nil || ip.To4() != nil {
			return "", nil, false, ValidationError("AAAA record content must be a valid IPv6 address")
		}
		return ip.String(), nil, proxied, nil
	case "CNAME":
		host := strings.TrimSuffix(strings.ToLower(content), ".")
		if !isValidRootDomain(host) {
			return "", nil, false, ValidationError("CNAME record content must be a valid hostname")
		}
		return host, nil, proxied, nil
	case "TXT":
		if strings.TrimSpace(content) == "" {
			return "", nil, false, ValidationError("TXT record content must not be empty")
		}
		return content, nil, false, nil
	case "MX":
		host := strings.TrimSuffix(strings.ToLower(content), ".")
		if !isValidRootDomain(host) {
			return "", nil, false, ValidationError("MX record content must be a valid hostname")
		}
		if priority == nil {
			return "", nil, false, ValidationError("MX record requires priority")
		}
		return host, priority, false, nil
	default:
		return "", nil, false, ValidationError("unsupported dns record type")
	}
}

// isSupportedRecordType 判断当前面板允许手动维护的记录类型。
// MX 被系统保留给邮件中转入口，不能再由用户或管理员在 DNS 面板中直接写入。
func isSupportedRecordType(recordType string) bool {
	switch recordType {
	case "A", "AAAA", "CNAME", "TXT":
		return true
	default:
		return false
	}
}

// isValidRootDomain 校验根域名或记录内容中的主机名。
func isValidRootDomain(value string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" || len(trimmed) > 253 {
		return false
	}

	labels := strings.Split(trimmed, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if !labelPattern.MatchString(label) {
			return false
		}
	}

	return true
}
