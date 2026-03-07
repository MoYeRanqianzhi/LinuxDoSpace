package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"linuxdospace/backend/internal/model"
)

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

	writeDNSAuditLog(t, ctx, store, user, usedAllocation, "dns_record.create")
	writeDNSAuditLog(t, ctx, store, user, deletedAllocation, "dns_record.create")
	writeDNSAuditLog(t, ctx, store, user, deletedAllocation, "dns_record.delete")
	writeDNSAuditLog(t, ctx, store, user, inactiveAllocation, "dns_record.create")

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

	user, err := store.UpsertUser(ctx, UpsertUserInput{
		LinuxDOUserID: 101,
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
func writeDNSAuditLog(t *testing.T, ctx context.Context, store *Store, user model.User, allocation model.Allocation, action string) {
	t.Helper()

	metadata, err := json.Marshal(map[string]any{
		"allocation_id": allocation.ID,
		"record_id":     action + "-record",
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
		ResourceID:   action + "-resource",
		MetadataJSON: string(metadata),
	}); err != nil {
		t.Fatalf("write dns audit log %q: %v", action, err)
	}
}
