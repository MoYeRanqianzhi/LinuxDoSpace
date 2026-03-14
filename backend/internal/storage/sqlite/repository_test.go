package sqlite

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// TestMigrateRemainsIdempotentAfterAddingAdminSessionVerification ensures the
// embedded migration set can still be replayed on every startup.
func TestMigrateRemainsIdempotentAfterAddingAdminSessionVerification(t *testing.T) {
	store := newTestStore(t)

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("run migrations twice: %v", err)
	}
}

// TestNewStoreCreatesDatabaseFileOnFirstBoot verifies that a brand-new data
// directory can still boot without requiring an operator to pre-create the
// SQLite file.
func TestNewStoreCreatesDatabaseFileOnFirstBoot(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "nested", "linuxdospace.sqlite")

	if _, err := os.Stat(databasePath); !os.IsNotExist(err) {
		t.Fatalf("expected test database file to start missing, got err=%v", err)
	}

	store, err := NewStore(databasePath)
	if err != nil {
		t.Fatalf("new store on missing database file: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate newly created store: %v", err)
	}

	info, err := os.Stat(databasePath)
	if err != nil {
		t.Fatalf("stat created database file: %v", err)
	}
	if info.IsDir() {
		t.Fatalf("expected %s to be a file, but it is a directory", databasePath)
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

// TestCreateAllocationReassignsPrimaryOnCreate verifies that promoting a new
// allocation to primary automatically demotes the old primary in repository
// code before the database unique index is even involved.
func TestCreateAllocationReassignsPrimaryOnCreate(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	user := newTestUser(t, ctx, store, "primary-owner")
	managedDomain := newTestManagedDomain(t, ctx, store, "linuxdo.space")

	originalPrimary := newTestAllocation(t, ctx, store, user, managedDomain, "alpha", "active", true)
	replacementPrimary, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "beta",
		NormalizedPrefix: "beta",
		FQDN:             "beta." + managedDomain.RootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create replacement primary allocation: %v", err)
	}
	if !replacementPrimary.IsPrimary {
		t.Fatalf("expected replacement allocation to stay primary")
	}

	reloadedOriginalPrimary, err := store.GetAllocationByID(ctx, originalPrimary.ID)
	if err != nil {
		t.Fatalf("reload original primary allocation: %v", err)
	}
	if reloadedOriginalPrimary.IsPrimary {
		t.Fatalf("expected original primary allocation to be demoted after replacement")
	}

	primaryCount := countPrimaryAllocationsForUserDomain(t, ctx, store, user.ID, managedDomain.ID)
	if primaryCount != 1 {
		t.Fatalf("expected exactly one primary allocation after replacement, got %d", primaryCount)
	}
}

// TestConsumeOAuthStateConcurrentStoresOnlyOneSucceeds verifies that two
// independent SQLite store instances cannot both consume the same OAuth state.
func TestConsumeOAuthStateConcurrentStoresOnlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "linuxdospace-race.sqlite")

	firstStore := newTestStoreAtPath(t, databasePath)
	secondStore := newTestStoreAtPath(t, databasePath)
	for _, store := range []*Store{firstStore, secondStore} {
		if _, err := store.db.ExecContext(ctx, `PRAGMA busy_timeout = 5000`); err != nil {
			t.Fatalf("set sqlite busy_timeout: %v", err)
		}
	}

	state := model.OAuthState{
		ID:           "oauth-state-concurrent",
		CodeVerifier: "verifier",
		NextPath:     "/oauth/callback",
		ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
		CreatedAt:    time.Now().UTC(),
	}
	if err := firstStore.SaveOAuthState(ctx, state); err != nil {
		t.Fatalf("save oauth state: %v", err)
	}

	type consumeResult struct {
		state model.OAuthState
		err   error
	}

	start := make(chan struct{})
	results := make(chan consumeResult, 2)

	consume := func(store *Store) {
		<-start
		item, err := store.ConsumeOAuthState(ctx, state.ID)
		results <- consumeResult{state: item, err: err}
	}

	go consume(firstStore)
	go consume(secondStore)
	close(start)

	successCount := 0
	notFoundCount := 0
	for resultIndex := 0; resultIndex < 2; resultIndex++ {
		result := <-results
		if result.err == nil {
			successCount++
			if result.state.ID != state.ID {
				t.Fatalf("expected consumed oauth state %q, got %q", state.ID, result.state.ID)
			}
			continue
		}
		if !storage.IsNotFound(result.err) {
			t.Fatalf("expected not-found from losing consumer, got %v", result.err)
		}
		notFoundCount++
	}

	if successCount != 1 {
		t.Fatalf("expected exactly one successful oauth state consume, got %d", successCount)
	}
	if notFoundCount != 1 {
		t.Fatalf("expected exactly one not-found loser, got %d", notFoundCount)
	}
	if _, err := firstStore.GetOAuthState(ctx, state.ID); !storage.IsNotFound(err) {
		t.Fatalf("expected oauth state %q to be gone after concurrent consume, got err=%v", state.ID, err)
	}
}

// TestPrimaryAllocationUniqueIndexRejectsSecondPrimary verifies that the
// SQLite partial unique index prevents two primary allocations for one user and
// one managed domain even when a caller bypasses repository safeguards.
func TestPrimaryAllocationUniqueIndexRejectsSecondPrimary(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)

	user := newTestUser(t, ctx, store, "primary-index-owner")
	managedDomain := newTestManagedDomain(t, ctx, store, "linuxdo.space")

	primaryAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "first-primary", "active", true)
	secondaryAllocation := newTestAllocation(t, ctx, store, user, managedDomain, "second-primary", "active", false)

	if _, err := store.db.ExecContext(ctx, `
UPDATE allocations
SET is_primary = 1, updated_at = ?
WHERE id = ?
`, formatTime(time.Now().UTC()), secondaryAllocation.ID); err == nil {
		t.Fatalf("expected unique index to reject a second primary allocation")
	}

	primaryCount := countPrimaryAllocationsForUserDomain(t, ctx, store, user.ID, managedDomain.ID)
	if primaryCount != 1 {
		t.Fatalf("expected exactly one primary allocation after failed direct update, got %d", primaryCount)
	}

	reloadedPrimaryAllocation, err := store.GetAllocationByID(ctx, primaryAllocation.ID)
	if err != nil {
		t.Fatalf("reload original primary allocation: %v", err)
	}
	if !reloadedPrimaryAllocation.IsPrimary {
		t.Fatalf("expected original primary allocation to remain primary")
	}

	reloadedSecondaryAllocation, err := store.GetAllocationByID(ctx, secondaryAllocation.ID)
	if err != nil {
		t.Fatalf("reload secondary allocation: %v", err)
	}
	if reloadedSecondaryAllocation.IsPrimary {
		t.Fatalf("expected secondary allocation to remain non-primary after failed direct update")
	}
}

// newTestStore 创建一个只用于当前测试的 sqlite store，并自动执行迁移。
func newTestStore(t *testing.T) *Store {
	t.Helper()

	return newTestStoreAtPath(t, filepath.Join(t.TempDir(), "linuxdospace-test.sqlite"))
}

// newTestStoreAtPath 创建一个使用指定数据库路径的 sqlite store，并自动执行迁移。
func newTestStoreAtPath(t *testing.T, databasePath string) *Store {
	t.Helper()

	store, err := NewStore(databasePath)
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

// countPrimaryAllocationsForUserDomain returns the database-level number of
// rows still marked as primary for one owner/domain pair.
func countPrimaryAllocationsForUserDomain(t *testing.T, ctx context.Context, store *Store, userID int64, managedDomainID int64) int {
	t.Helper()

	row := store.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM allocations
WHERE user_id = ? AND managed_domain_id = ? AND is_primary = 1
`, userID, managedDomainID)

	var count int
	if err := row.Scan(&count); err != nil {
		t.Fatalf("count primary allocations for user %d domain %d: %v", userID, managedDomainID, err)
	}
	return count
}

// newTestAllocation 写入一条分配记录，方便后续为其补充 DNS 审计日志。
func newTestAllocation(t *testing.T, ctx context.Context, store *Store, user model.User, managedDomain model.ManagedDomain, prefix string, status string, isPrimary ...bool) model.Allocation {
	t.Helper()

	primary := false
	if len(isPrimary) > 0 {
		primary = isPrimary[0]
	}

	item, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           prefix,
		NormalizedPrefix: prefix,
		FQDN:             prefix + "." + managedDomain.RootDomain,
		IsPrimary:        primary,
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
