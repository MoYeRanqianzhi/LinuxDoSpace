package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/storagetest"
)

// TestRepositoryBehaviorSuite runs the shared repository contract against a
// real PostgreSQL schema when the caller provides one integration DSN.
//
// The test stays opt-in so ordinary local `go test ./...` runs do not require a
// PostgreSQL service, but CI or an operator can enable it with
// `LINUXDOSPACE_TEST_POSTGRES_DSN`.
func TestRepositoryBehaviorSuite(t *testing.T) {
	storagetest.RunRepositoryBehaviorSuite(t, func(t *testing.T) storage.Backend {
		t.Helper()
		return newIntegrationTestStore(t)
	})
}

// TestConsumeOAuthStateConcurrentConsumersOnlyOneSucceeds verifies that the
// PostgreSQL backend only lets one concurrent consumer claim one OAuth state.
func TestConsumeOAuthStateConcurrentConsumersOnlyOneSucceeds(t *testing.T) {
	ctx := context.Background()
	store := newIntegrationTestStore(t)

	const attemptCount = 24
	const consumerCount = 8

	type consumeResult struct {
		stateID string
		err     error
	}

	for attempt := 0; attempt < attemptCount; attempt++ {
		state := model.OAuthState{
			ID:           fmt.Sprintf("oauth-state-race-%02d", attempt),
			CodeVerifier: "verifier",
			NextPath:     "/oauth/callback",
			ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
			CreatedAt:    time.Now().UTC(),
		}
		if err := store.SaveOAuthState(ctx, state); err != nil {
			t.Fatalf("save oauth state for contention attempt %d: %v", attempt, err)
		}

		start := make(chan struct{})
		results := make(chan consumeResult, consumerCount)

		for consumer := 0; consumer < consumerCount; consumer++ {
			go func() {
				<-start
				item, err := store.ConsumeOAuthState(ctx, state.ID)
				results <- consumeResult{stateID: item.ID, err: err}
			}()
		}

		close(start)

		successCount := 0
		notFoundCount := 0
		for consumer := 0; consumer < consumerCount; consumer++ {
			result := <-results
			if result.err == nil {
				successCount++
				if result.stateID != state.ID {
					t.Fatalf("expected consumed oauth state %q, got %q", state.ID, result.stateID)
				}
				continue
			}
			if !storage.IsNotFound(result.err) {
				t.Fatalf("expected not-found from losing consumer in attempt %d, got %v", attempt, result.err)
			}
			notFoundCount++
		}

		if successCount != 1 {
			t.Fatalf("expected exactly one successful oauth state consume in attempt %d, got %d", attempt, successCount)
		}
		if notFoundCount != consumerCount-1 {
			t.Fatalf("expected %d losing consumers in attempt %d, got %d", consumerCount-1, attempt, notFoundCount)
		}
		if _, err := store.GetOAuthState(ctx, state.ID); !storage.IsNotFound(err) {
			t.Fatalf("expected oauth state %q to be gone after contention attempt %d, got err=%v", state.ID, attempt, err)
		}
	}
}

// TestCreateAllocationReassignsPrimaryOnCreate verifies that promoting a new
// allocation to primary automatically demotes the old primary in repository
// code before the database unique index is even involved.
func TestCreateAllocationReassignsPrimaryOnCreate(t *testing.T) {
	ctx := context.Background()
	store := newIntegrationTestStore(t)

	user, err := store.UpsertUser(ctx, UpsertUserInput{
		LinuxDOUserID: 9002,
		Username:      "primary-owner",
		DisplayName:   "primary-owner",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert integration test user: %v", err)
	}

	managedDomain, err := store.UpsertManagedDomain(ctx, UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     10,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert integration test managed domain: %v", err)
	}

	originalPrimary, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "alpha",
		NormalizedPrefix: "alpha",
		FQDN:             "alpha." + managedDomain.RootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create original primary allocation: %v", err)
	}

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

// TestPrimaryAllocationUniqueIndexRejectsSecondPrimary verifies that the
// PostgreSQL partial unique index prevents two primary allocations for one user
// and one managed domain even when a caller bypasses repository safeguards.
func TestPrimaryAllocationUniqueIndexRejectsSecondPrimary(t *testing.T) {
	ctx := context.Background()
	store := newIntegrationTestStore(t)

	user, err := store.UpsertUser(ctx, UpsertUserInput{
		LinuxDOUserID: 9001,
		Username:      "primary-index-owner",
		DisplayName:   "primary-index-owner",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert integration test user: %v", err)
	}

	managedDomain, err := store.UpsertManagedDomain(ctx, UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-test",
		DefaultQuota:     10,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert integration test managed domain: %v", err)
	}

	primaryAllocation, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "first-primary",
		NormalizedPrefix: "first-primary",
		FQDN:             "first-primary." + managedDomain.RootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create original primary allocation: %v", err)
	}

	secondaryAllocation, err := store.CreateAllocation(ctx, CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           "second-primary",
		NormalizedPrefix: "second-primary",
		FQDN:             "second-primary." + managedDomain.RootDomain,
		IsPrimary:        false,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("create secondary allocation: %v", err)
	}

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

// newIntegrationTestStore opens one isolated PostgreSQL schema for the current
// test case, migrates it, and drops it after the test completes.
func newIntegrationTestStore(t *testing.T) *Store {
	t.Helper()

	baseDSN := strings.TrimSpace(os.Getenv("LINUXDOSPACE_TEST_POSTGRES_DSN"))
	if baseDSN == "" {
		t.Skip("set LINUXDOSPACE_TEST_POSTGRES_DSN to run PostgreSQL integration tests")
	}

	adminDB, err := sql.Open("pgx", baseDSN)
	if err != nil {
		t.Fatalf("open postgres integration database: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := adminDB.Close(); closeErr != nil {
			t.Errorf("close postgres integration database: %v", closeErr)
		}
	})

	ctx := context.Background()
	schemaName := fmt.Sprintf("linuxdospace_test_%d", time.Now().UTC().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE SCHEMA `+schemaName); err != nil {
		t.Fatalf("create postgres integration schema %q: %v", schemaName, err)
	}

	cfg, err := pgx.ParseConfig(baseDSN)
	if err != nil {
		t.Fatalf("parse postgres integration dsn: %v", err)
	}
	if cfg.RuntimeParams == nil {
		cfg.RuntimeParams = make(map[string]string)
	}
	cfg.RuntimeParams["search_path"] = schemaName

	store, err := NewStore(cfg.ConnString())
	if err != nil {
		t.Fatalf("open postgres store for schema %q: %v", schemaName, err)
	}
	t.Cleanup(func() {
		if closeErr := store.Close(); closeErr != nil {
			t.Errorf("close postgres store for schema %q: %v", schemaName, closeErr)
		}
		if _, dropErr := adminDB.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS `+schemaName+` CASCADE`); dropErr != nil {
			t.Errorf("drop postgres integration schema %q: %v", schemaName, dropErr)
		}
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("migrate postgres integration schema %q: %v", schemaName, err)
	}

	return store
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
