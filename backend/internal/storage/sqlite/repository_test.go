package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
)

// TestMigrateRemainsIdempotentAfterAddingAdminSessionVerification ensures the
// embedded migration set can still be replayed on every startup.
func TestMigrateRemainsIdempotentAfterAddingAdminSessionVerification(t *testing.T) {
	store := newTestStore(t)

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("run migrations twice: %v", err)
	}
}

// TestSessionAdminVerificationPersists verifies that the extra admin password
// verification state survives round-trips through SQLite.
func TestSessionAdminVerificationPersists(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user := newTestUser(t, ctx, store, "admin-user")

	session, err := store.CreateSession(ctx, CreateSessionInput{
		ID:                   "session-test",
		UserID:               user.ID,
		CSRFToken:            "csrf-test",
		UserAgentFingerprint: "ua-fingerprint",
		ExpiresAt:            time.Now().UTC().Add(30 * time.Minute),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if session.AdminVerifiedAt != nil {
		t.Fatalf("expected new session to start without admin verification, got %+v", session.AdminVerifiedAt)
	}

	verifiedAt := time.Now().UTC().Truncate(time.Second)
	if err := store.MarkSessionAdminVerified(ctx, session.ID, verifiedAt); err != nil {
		t.Fatalf("mark session admin verified: %v", err)
	}

	reloadedSession, _, err := store.GetSessionWithUserByID(ctx, session.ID)
	if err != nil {
		t.Fatalf("reload session with user: %v", err)
	}
	if reloadedSession.AdminVerifiedAt == nil {
		t.Fatalf("expected reloaded session to contain admin verification timestamp")
	}
	if !reloadedSession.AdminVerifiedAt.Equal(verifiedAt) {
		t.Fatalf("expected verified timestamp %s, got %s", verifiedAt.Format(time.RFC3339Nano), reloadedSession.AdminVerifiedAt.Format(time.RFC3339Nano))
	}
}

// TestListPublicAllocationOwnershipsOnlyReturnsActivelyUsedAllocations 验证公开监督页只返回数据库中仍然实际在用的子域名。
func TestListPublicAllocationOwnershipsOnlyReturnsActivelyUsedAllocations(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	user := newTestUser(t, ctx, store, "alice")
	managedDomain := newTestManagedDomain(t, ctx, store, "linuxdo.space")

	unusedAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "unused", "active")
	usedAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "used", "active")
	deletedAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "deleted", "active")
	inactiveAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "inactive", "disabled")

	writeDNSAuditLog(t, ctx, store, user, usedAllocation, "dns_record.create", "used-record-a")
	writeDNSAuditLog(t, ctx, store, user, usedAllocation, "dns_record.create", "used-record-b")
	writeDNSAuditLog(t, ctx, store, user, usedAllocation, "dns_record.delete", "used-record-a")
	writeDNSAuditLog(t, ctx, store, user, deletedAllocation, "dns_record.create", "deleted-record")
	writeDNSAuditLog(t, ctx, store, user, deletedAllocation, "dns_record.delete", "deleted-record")
	writeDNSAuditLog(t, ctx, store, user, inactiveAllocation, "dns_record.create", "inactive-record")

	items, err := store.ListPublicAllocationOwnerships(ctx)
	if err != nil {
		t.Fatalf("list public allocation ownerships: %v", err)
	}

	if len(items) != 1 {
		t.Fatalf("expected exactly 1 active used allocation, got %d: %+v", len(items), items)
	}

	if items[0].FQDN != usedAllocation.FQDN {
		t.Fatalf("expected fqdn %q, got %q", usedAllocation.FQDN, items[0].FQDN)
	}
	if items[0].OwnerUsername != user.Username {
		t.Fatalf("expected owner username %q, got %q", user.Username, items[0].OwnerUsername)
	}

	for _, item := range items {
		if item.FQDN == unusedAllocation.FQDN {
			t.Fatalf("unused allocation %q should not be returned", unusedAllocation.FQDN)
		}
		if item.FQDN == deletedAllocation.FQDN {
			t.Fatalf("deleted allocation %q should not be returned", deletedAllocation.FQDN)
		}
		if item.FQDN == inactiveAllocation.FQDN {
			t.Fatalf("inactive allocation %q should not be returned", inactiveAllocation.FQDN)
		}
	}
}

// TestUpdateAllocationReassignsPrimary verifies that transferring an allocation
// between users still preserves the single-primary invariant per user+domain.
func TestUpdateAllocationReassignsPrimary(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	alice := newTestUser(t, ctx, store, "alice")
	bob, err := store.UpsertUser(ctx, UpsertUserInput{
		LinuxDOUserID: 202,
		Username:      "bob",
		DisplayName:   "bob",
		AvatarURL:     "https://example.com/avatar-bob.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert second test user: %v", err)
	}
	managedDomain := newTestManagedDomain(t, ctx, store, "linuxdo.space")

	bobPrimary, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           bob.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "bob-main",
		NormalizedPrefix: "bob-main",
		FQDN:             "bob-main." + managedDomain.RootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create bob primary allocation: %v", err)
	}

	alicePrimary, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           alice.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alice-main",
		NormalizedPrefix: "alice-main",
		FQDN:             "alice-main." + managedDomain.RootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create alice primary allocation: %v", err)
	}

	updated, err := store.UpdateAllocation(ctx, UpdateAllocationInput{
		ID:        alicePrimary.ID,
		UserID:    bob.ID,
		IsPrimary: true,
		Source:    "manual-transfer",
		Status:    "active",
	})
	if err != nil {
		t.Fatalf("update allocation owner: %v", err)
	}
	if updated.UserID != bob.ID {
		t.Fatalf("expected updated allocation owner %d, got %d", bob.ID, updated.UserID)
	}
	if !updated.IsPrimary {
		t.Fatalf("expected transferred allocation to stay primary for the new owner")
	}
	if updated.Source != "manual-transfer" {
		t.Fatalf("expected updated source to persist, got %q", updated.Source)
	}

	reloadedBobPrimary, err := store.GetAllocationByID(ctx, bobPrimary.ID)
	if err != nil {
		t.Fatalf("reload bob original primary allocation: %v", err)
	}
	if reloadedBobPrimary.IsPrimary {
		t.Fatalf("expected bob original primary allocation to be cleared after transfer")
	}
}

// newTestStore 创建一个只用于当前测试的 sqlite store，并自动执行迁移。
func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(filepath.Join(t.TempDir(), "linuxdospace-test.sqlite"))
	if err != nil {
		t.Fatalf("new test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate test store: %v", err)
	}

	return store
}

// newTestUser 向测试数据库写入一个基础用户。
func newTestUser(t *testing.T, ctx context.Context, store *Store, username string) model.User {
	t.Helper()

	linuxDOUserID := int64(1000)
	for _, runeValue := range username {
		linuxDOUserID = linuxDOUserID*31 + int64(runeValue)
	}

	user, err := store.UpsertUser(ctx, UpsertUserInput{
		LinuxDOUserID: linuxDOUserID,
		Username:      username,
		DisplayName:   username,
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert test user: %v", err)
	}

	return user
}

// newTestManagedDomain 写入一个可分发根域名。
func newTestManagedDomain(t *testing.T, ctx context.Context, store *Store, rootDomain string) model.ManagedDomain {
	t.Helper()

	item, err := store.UpsertManagedDomain(ctx, UpsertManagedDomainInput{
		RootDomain:       rootDomain,
		CloudflareZoneID: "zone-test",
		DefaultQuota:     10,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert test managed domain: %v", err)
	}

	return item
}

// newTestAllocation 写入一条分配记录，方便后续为其补充 DNS 审计日志。
func newTestAllocation(t *testing.T, ctx context.Context, store *Store, user model.User, managedDomain model.ManagedDomain, prefix string, status string) model.Allocation {
	t.Helper()

	item, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           prefix,
		NormalizedPrefix: prefix,
		FQDN:             prefix + "." + managedDomain.RootDomain,
		IsPrimary:        false,
		Source:           "test",
		Status:           status,
	})
	if err != nil {
		t.Fatalf("create test allocation %q: %v", prefix, err)
	}

	return item
}

// writeDNSAuditLog 为指定 allocation 写入一条 DNS 审计事件，用来模拟真实的记录创建/删除历史。
func writeDNSAuditLog(t *testing.T, ctx context.Context, store *Store, user model.User, allocation model.Allocation, action string, recordID string) {
	t.Helper()

	metadata, err := json.Marshal(map[string]any{
		"allocation_id": allocation.ID,
		"record_id":     recordID,
		"name":          allocation.FQDN,
		"type":          "A",
	})
	if err != nil {
		t.Fatalf("marshal dns audit metadata: %v", err)
	}

	if err := store.WriteAuditLog(ctx, AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       action,
		ResourceType: "dns_record",
		ResourceID:   recordID,
		MetadataJSON: string(metadata),
	}); err != nil {
		t.Fatalf("write dns audit log %q: %v", action, err)
	}
}
