package mailrelay

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/sqlite"
)

// TestDBCatchAllAccessManagerMapsStorageState verifies that the SMTP-facing
// access manager translates the underlying mutable quota state into stable
// mailrelay errors.
func TestDBCatchAllAccessManagerMapsStorageState(t *testing.T) {
	ctx := context.Background()
	store := newAccessTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  9991,
		Username:       "alice",
		DisplayName:    "alice",
		AvatarURL:      "https://example.com/avatar.png",
		TrustLevel:     2,
		IsLinuxDOAdmin: false,
		IsAppAdmin:     false,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	dailyLimitOverride := int64(1)
	if _, err := store.UpsertEmailCatchAllAccess(ctx, storage.UpsertEmailCatchAllAccessInput{
		UserID:             user.ID,
		RemainingCount:     1,
		DailyLimitOverride: &dailyLimitOverride,
	}); err != nil {
		t.Fatalf("upsert email catch-all access: %v", err)
	}

	manager := NewDBCatchAllAccessManager(store)
	fixedNow := time.Date(2026, 3, 12, 9, 0, 0, 0, time.UTC)
	manager.now = func() time.Time { return fixedNow }

	if err := manager.Consume(ctx, user.ID, 1); err != nil {
		t.Fatalf("consume first catch-all delivery: %v", err)
	}

	if err := manager.Consume(ctx, user.ID, 1); !errors.Is(err, ErrCatchAllDailyLimitExceeded) {
		t.Fatalf("expected daily limit exceeded error, got %v", err)
	}

	manager.now = func() time.Time { return fixedNow.Add(24 * time.Hour) }
	if err := manager.Consume(ctx, user.ID, 1); !errors.Is(err, ErrCatchAllAccessUnavailable) {
		t.Fatalf("expected access unavailable after remaining count exhaustion, got %v", err)
	}
}

// newAccessTestStore creates one migrated SQLite store for access-manager
// tests.
func newAccessTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.NewStore(filepath.Join(t.TempDir(), "mailrelay-access-test.sqlite"))
	if err != nil {
		t.Fatalf("new mailrelay access test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close mailrelay access test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate mailrelay access test store: %v", err)
	}
	return store
}
