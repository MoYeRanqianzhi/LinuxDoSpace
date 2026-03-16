package service

import (
	"context"
	"path/filepath"
	"testing"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
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

// TestEnsureTemporaryUsernameMatch 验证当前临时策略只放行“用户名同名”的子域名前缀。
func TestEnsureTemporaryUsernameMatch(t *testing.T) {
	user := model.User{
		Username: "Alice",
	}

	if err := ensureTemporaryUsernameMatch(user, "alice"); err != nil {
		t.Fatalf("expected alice to be allowed, got %v", err)
	}
	if err := ensureTemporaryUsernameMatch(user, "other-name"); err == nil {
		t.Fatalf("expected mismatched prefix to be rejected")
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
