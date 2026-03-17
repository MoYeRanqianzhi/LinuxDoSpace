package service

import (
	"context"
	"path/filepath"
	"testing"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/sqlite"
)

// TestNormalizePrefix 验证用户输入前缀会被正确清洗成 DNS label。
func TestNormalizePrefix(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{name: "simple", input: "alice", expected: "alice"},
		{name: "mixed case", input: "Alice-01", expected: "alice-01"},
		{name: "invalid chars", input: "Alice 中文 @@@", expected: "alice"},
		{name: "blank", input: "   ", expectError: true},
		{name: "symbols only", input: "@@@", expectError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := NormalizePrefix(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

// TestNormalizeRelativeRecordName 验证命名空间内相对记录名的校验逻辑。
func TestNormalizeRelativeRecordName(t *testing.T) {
	testCases := []struct {
		name        string
		input       string
		expected    string
		expectError bool
	}{
		{name: "root record", input: "@", expected: "@"},
		{name: "wildcard child", input: "*.api", expected: "*.api"},
		{name: "nested child", input: "WWW.Api", expected: "www.api"},
		{name: "invalid label", input: "bad_label", expectError: true},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			actual, err := NormalizeRelativeRecordName(testCase.input)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if actual != testCase.expected {
				t.Fatalf("expected %q, got %q", testCase.expected, actual)
			}
		})
	}
}

// TestNamespaceHelpers 验证绝对名称与相对名称之间的转换逻辑。
func TestNamespaceHelpers(t *testing.T) {
	namespace := "alice.linuxdo.space"

	fullName := BuildAbsoluteName("@", namespace)
	if fullName != namespace {
		t.Fatalf("expected namespace root, got %q", fullName)
	}

	childName := BuildAbsoluteName("www.api", namespace)
	if childName != "www.api.alice.linuxdo.space" {
		t.Fatalf("unexpected child name: %q", childName)
	}

	if !BelongsToNamespace(childName, namespace) {
		t.Fatalf("expected child name to belong to namespace")
	}

	if BelongsToNamespace("admin.linuxdo.space", namespace) {
		t.Fatalf("unexpected cross-namespace match")
	}

	if actual := RelativeNameFromAbsolute(childName, namespace); actual != "www.api" {
		t.Fatalf("expected relative name \"www.api\", got %q", actual)
	}
}

// TestValidateAndNormalizeRecordPayload 验证记录内容的类型约束。
func TestValidateAndNormalizeRecordPayload(t *testing.T) {
	if _, _, _, err := validateAndNormalizeRecordPayload("A", "1.1.1.1", nil, true); err != nil {
		t.Fatalf("expected valid A record, got error: %v", err)
	}

	if _, _, _, err := validateAndNormalizeRecordPayload("AAAA", "2001:db8::1", nil, false); err != nil {
		t.Fatalf("expected valid AAAA record, got error: %v", err)
	}

	priority := 10
	if _, actualPriority, actualProxied, err := validateAndNormalizeRecordPayload("MX", "mail.example.com", &priority, true); err != nil {
		t.Fatalf("expected valid MX record, got error: %v", err)
	} else if actualPriority == nil || *actualPriority != 10 || actualProxied {
		t.Fatalf("expected MX priority=10 and proxied=false, got priority=%v proxied=%v", actualPriority, actualProxied)
	}

	if _, _, _, err := validateAndNormalizeRecordPayload("A", "not-an-ip", nil, false); err == nil {
		t.Fatalf("expected invalid A record to fail")
	}
}

// TestIsSupportedRecordTypeReservesMX verifies that manual DNS management keeps
// MX reserved for the system-managed mail relay path.
func TestIsSupportedRecordTypeReservesMX(t *testing.T) {
	if !isSupportedRecordType("A") {
		t.Fatalf("expected A records to stay supported")
	}
	if isSupportedRecordType("MX") {
		t.Fatalf("expected MX records to be rejected from manual DNS management")
	}
}

// TestEnsureTemporaryFreeRegistrationEligibility verifies that the temporary
// free self-service flow requires both a username match and a root domain that
// explicitly enables the free same-name path.
func TestEnsureTemporaryFreeRegistrationEligibility(t *testing.T) {
	user := model.User{
		Username: "Alice",
	}
	managedDomain := model.ManagedDomain{
		RootDomain:    "linuxdo.space",
		AutoProvision: true,
	}

	if err := ensureTemporaryFreeRegistrationEligibility(user, managedDomain, "alice"); err != nil {
		t.Fatalf("expected alice to be allowed, got %v", err)
	}
	if err := ensureTemporaryFreeRegistrationEligibility(user, managedDomain, "other-name"); err == nil {
		t.Fatalf("expected mismatched prefix to be rejected")
	}

	managedDomain.AutoProvision = false
	if err := ensureTemporaryFreeRegistrationEligibility(user, managedDomain, "alice"); err == nil {
		t.Fatalf("expected non-free managed domain to reject temporary self-service")
	}
}

// TestListVisibleAllocationsForUserReturnsAllOwnedAllocations verifies that
// administrator-granted namespaces remain visible in the public configuration
// center even when they do not match the Linux Do username.
func TestListVisibleAllocationsForUserReturnsAllOwnedAllocations(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 101,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	if _, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice",
		NormalizedPrefix: "alice",
		FQDN:             "alice.linuxdo.space",
		IsPrimary:        true,
		Source:           "auto_provision",
		Status:           "active",
	}); err != nil {
		t.Fatalf("create primary allocation: %v", err)
	}

	if _, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "project-room",
		NormalizedPrefix: "project-room",
		FQDN:             "project-room.linuxdo.space",
		IsPrimary:        false,
		Source:           "admin_grant",
		Status:           "active",
	}); err != nil {
		t.Fatalf("create admin-granted allocation: %v", err)
	}

	domainService := NewDomainService(config.Config{}, store, nil)
	visibleAllocations, err := domainService.ListVisibleAllocationsForUser(ctx, user)
	if err != nil {
		t.Fatalf("list visible allocations: %v", err)
	}

	if len(visibleAllocations) != 2 {
		t.Fatalf("expected 2 visible allocations, got %d", len(visibleAllocations))
	}
	if visibleAllocations[0].FQDN != "alice.linuxdo.space" {
		t.Fatalf("expected primary allocation first, got %q", visibleAllocations[0].FQDN)
	}
	if visibleAllocations[1].FQDN != "project-room.linuxdo.space" {
		t.Fatalf("expected admin-granted allocation to remain visible, got %q", visibleAllocations[1].FQDN)
	}
}

// TestListRecordsForAllocationReturnsNestedNamespaceRecords verifies that one
// owned namespace exposes both the root record and nested child records.
func TestListRecordsForAllocationReturnsNestedNamespaceRecords(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 102,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "project-room",
		NormalizedPrefix: "project-room",
		FQDN:             "project-room.linuxdo.space",
		IsPrimary:        true,
		Source:           "admin_grant",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}

	cloudflareClient := &fakeEmailRoutingCloudflare{
		dnsRecordsByZone: map[string][]cloudflare.DNSRecord{
			"zone-test": {
				{ID: "root", Type: "A", Name: "project-room.linuxdo.space", Content: "1.1.1.1", TTL: 1},
				{ID: "www", Type: "CNAME", Name: "www.project-room.linuxdo.space", Content: "project-room.linuxdo.space", TTL: 1},
				{ID: "nested", Type: "TXT", Name: "api.v2.project-room.linuxdo.space", Content: "hello", TTL: 120},
				{ID: "outside", Type: "A", Name: "other.linuxdo.space", Content: "2.2.2.2", TTL: 1},
			},
		},
	}

	domainService := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{
			APIToken: "test-token",
		},
	}, store, cloudflareClient)

	records, err := domainService.ListRecordsForAllocation(ctx, user, allocation.ID)
	if err != nil {
		t.Fatalf("list records for allocation: %v", err)
	}

	if len(records) != 3 {
		t.Fatalf("expected 3 namespace records, got %d", len(records))
	}
	if records[0].RelativeName != "@" {
		t.Fatalf("expected root record first, got %q", records[0].RelativeName)
	}
	if records[1].RelativeName != "api.v2" {
		t.Fatalf("expected nested child record to be included, got %q", records[1].RelativeName)
	}
	if records[2].RelativeName != "www" {
		t.Fatalf("expected direct child record to be included, got %q", records[2].RelativeName)
	}
}

// TestListRecordsForAllocationIncludesSyntheticCatchAllRecord verifies that the
// DNS panel exposes one privacy-safe synthetic row once the namespace-wide
// catch-all route is already enabled in the email subsystem.
func TestListRecordsForAllocationIncludesSyntheticCatchAllRecord(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 103,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice",
		NormalizedPrefix: "alice",
		FQDN:             "alice.linuxdo.space",
		IsPrimary:        true,
		Source:           "auto_provision",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  allocation.FQDN,
		Prefix:      emailCatchAllPrefix,
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("create catch-all email route: %v", err)
	}

	cloudflareClient := &fakeEmailRoutingCloudflare{
		dnsRecordsByZone: map[string][]cloudflare.DNSRecord{
			"zone-test": {
				{ID: "www", Type: "CNAME", Name: "www.alice.linuxdo.space", Content: "alice.linuxdo.space", TTL: 1},
			},
		},
	}

	domainService := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			DefaultRootDomain: "linuxdo.space",
		},
	}, store, cloudflareClient)

	records, err := domainService.ListRecordsForAllocation(ctx, user, allocation.ID)
	if err != nil {
		t.Fatalf("list records for allocation: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected namespace record plus synthetic catch-all record, got %d", len(records))
	}
	if records[0].Type != specialDNSRecordTypeEmailCatchAll {
		t.Fatalf("expected synthetic catch-all record first, got %+v", records[0])
	}
	if records[0].RelativeName != "@" {
		t.Fatalf("expected synthetic catch-all record at root, got %q", records[0].RelativeName)
	}
	if records[0].Content != "邮箱泛解析" {
		t.Fatalf("expected privacy-safe catch-all marker, got %q", records[0].Content)
	}
}

// TestCreateRecordEmailCatchAllRequiresApprovedPermissionAndSavedRoute verifies
// that the synthetic DNS toggle cannot be enabled before the user has both the
// approved permission and a saved forwarding target in the mail settings page.
func TestCreateRecordEmailCatchAllRequiresApprovedPermissionAndSavedRoute(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 104,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice",
		NormalizedPrefix: "alice",
		FQDN:             "alice.linuxdo.space",
		IsPrimary:        true,
		Source:           "auto_provision",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}

	domainService := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			DefaultRootDomain: "linuxdo.space",
			DefaultZoneID:     "zone-test",
		},
		Mail: config.MailConfig{
			ForwardingBackend: config.EmailForwardingBackendDatabaseRelay,
			EnsureDNS:         true,
			MXTarget:          "mail.linuxdo.space",
			MXPriority:        10,
			SPFValue:          "v=spf1 -all",
		},
	}, store, newFakeEmailRoutingCloudflare())

	_, err = domainService.CreateRecord(ctx, user, allocation.ID, DNSRecordInput{
		Type:    specialDNSRecordTypeEmailCatchAll,
		Name:    "@",
		Content: "",
		TTL:     1,
	})
	if err == nil {
		t.Fatalf("expected permission gate to block catch-all toggle")
	}

	serviceErr := NormalizeError(err)
	if serviceErr.StatusCode != 403 {
		t.Fatalf("expected forbidden error, got %+v", serviceErr)
	}
}

// TestCreateRecordEmailCatchAllRejectsRootWebsiteConflict verifies that the
// namespace root cannot simultaneously host one website-style record and the
// synthetic mailbox catch-all toggle.
func TestCreateRecordEmailCatchAllRejectsRootWebsiteConflict(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 105,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice",
		NormalizedPrefix: "alice",
		FQDN:             "alice.linuxdo.space",
		IsPrimary:        true,
		Source:           "auto_provision",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}

	if _, err := store.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          buildCatchAllEmailRouteAddress(allocation.FQDN),
		Reason:          "approved",
		Status:          "approved",
	}); err != nil {
		t.Fatalf("seed approved catch-all application: %v", err)
	}

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  allocation.FQDN,
		Prefix:      emailCatchAllPrefix,
		TargetEmail: "owner@example.com",
		Enabled:     false,
	}); err != nil {
		t.Fatalf("create catch-all email route: %v", err)
	}

	cf := newFakeEmailRoutingCloudflare()
	cf.zoneIDsByRoot["alice.linuxdo.space"] = "zone-test"
	cf.zones["zone-test"] = cloudflare.Zone{
		ID:   "zone-test",
		Name: "linuxdo.space",
		Account: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{
			ID:   "account-default",
			Name: "Test Account",
		},
	}
	cf.dnsRecordsByZone["zone-test"] = []cloudflare.DNSRecord{
		{ID: "root", Type: "A", Name: "alice.linuxdo.space", Content: "1.1.1.1", TTL: 1},
	}

	domainService := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			DefaultRootDomain: "linuxdo.space",
			DefaultZoneID:     "zone-test",
		},
		Mail: config.MailConfig{
			ForwardingBackend: config.EmailForwardingBackendDatabaseRelay,
			EnsureDNS:         true,
			MXTarget:          "mail.linuxdo.space",
			MXPriority:        10,
			SPFValue:          "v=spf1 -all",
		},
	}, store, cf)

	_, err = domainService.CreateRecord(ctx, user, allocation.ID, DNSRecordInput{
		Type:    specialDNSRecordTypeEmailCatchAll,
		Name:    "@",
		Content: "",
		TTL:     1,
	})
	if err == nil {
		t.Fatalf("expected website root conflict to block catch-all enable")
	}

	serviceErr := NormalizeError(err)
	if serviceErr.StatusCode != 409 {
		t.Fatalf("expected conflict error, got %+v", serviceErr)
	}
}

// TestDeleteRecordSyntheticCatchAllDisablesRouteAndRemovesHiddenRelayDNS verifies
// that deleting the public synthetic row immediately frees the hidden relay
// MX/TXT records so the namespace root can later be reused for website records.
func TestDeleteRecordSyntheticCatchAllDisablesRouteAndRemovesHiddenRelayDNS(t *testing.T) {
	ctx := context.Background()
	store := newDomainTestStore(t)

	managedDomain, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     5,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID: 106,
		Username:      "alice",
		DisplayName:   "Alice",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    3,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice",
		NormalizedPrefix: "alice",
		FQDN:             "alice.linuxdo.space",
		IsPrimary:        true,
		Source:           "auto_provision",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create allocation: %v", err)
	}

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  allocation.FQDN,
		Prefix:      emailCatchAllPrefix,
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("create enabled catch-all email route: %v", err)
	}

	cf := newFakeEmailRoutingCloudflare()
	cf.zoneIDsByRoot["alice.linuxdo.space"] = "zone-test"
	cf.zones["zone-test"] = cloudflare.Zone{
		ID:   "zone-test",
		Name: "linuxdo.space",
		Account: struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}{
			ID:   "account-default",
			Name: "Test Account",
		},
	}
	cf.dnsRecordsByZone["zone-test"] = []cloudflare.DNSRecord{
		{
			ID:       "mx",
			Type:     "MX",
			Name:     "alice.linuxdo.space",
			Content:  "mail.linuxdo.space",
			TTL:      1,
			Comment:  databaseRelayManagedDNSComment,
			Priority: intPointer(10),
		},
		{
			ID:      "spf",
			Type:    "TXT",
			Name:    "alice.linuxdo.space",
			Content: "v=spf1 -all",
			TTL:     1,
			Comment: databaseRelayManagedDNSComment,
		},
		{
			ID:      "user-txt",
			Type:    "TXT",
			Name:    "www.alice.linuxdo.space",
			Content: "hello",
			TTL:     120,
		},
	}

	domainService := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			DefaultRootDomain: "linuxdo.space",
			DefaultZoneID:     "zone-test",
		},
		Mail: config.MailConfig{
			ForwardingBackend: config.EmailForwardingBackendDatabaseRelay,
			EnsureDNS:         true,
			MXTarget:          "mail.linuxdo.space",
			MXPriority:        10,
			SPFValue:          "v=spf1 -all",
		},
	}, store, cf)

	if err := domainService.DeleteRecord(ctx, user, allocation.ID, syntheticCatchAllDNSRecordIDPrefix+"123"); err != nil {
		t.Fatalf("delete synthetic catch-all dns record: %v", err)
	}

	storedRoute, err := store.GetEmailRouteByAddress(ctx, allocation.FQDN, emailCatchAllPrefix)
	if err != nil {
		t.Fatalf("load catch-all email route after delete: %v", err)
	}
	if storedRoute.Enabled {
		t.Fatalf("expected catch-all route to be disabled after synthetic record deletion")
	}

	remainingRecords := cf.dnsRecordsByZone["zone-test"]
	if hasDNSRecord(remainingRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected hidden relay MX record to be removed, got %+v", remainingRecords)
	}
	if hasDNSRecord(remainingRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected hidden relay TXT record to be removed, got %+v", remainingRecords)
	}
	if !hasDNSRecord(remainingRecords, "TXT", "www.alice.linuxdo.space", "hello") {
		t.Fatalf("expected unrelated user dns records to be preserved, got %+v", remainingRecords)
	}
}

func intPointer(value int) *int {
	return &value
}

// newDomainTestStore creates one migrated temporary SQLite database for domain tests.
func newDomainTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.NewStore(filepath.Join(t.TempDir(), "domain-test.sqlite"))
	if err != nil {
		t.Fatalf("new domain test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close domain test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate domain test store: %v", err)
	}

	return store
}
