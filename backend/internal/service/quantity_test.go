package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/storage"
)

// TestCreateQuantityRecordRejectsInvalidExplicitSource verifies that the
// quantity ledger never silently rewrites a malformed operator-provided source
// token into the default source value, because that would destroy auditability.
func TestCreateQuantityRecordRejectsInvalidExplicitSource(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	actor := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 901, "admin")
	target := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 902, "alice")

	service := NewQuantityService(store)
	_, err := service.CreateQuantityRecord(ctx, actor, target.ID, AdminCreateQuantityRecordRequest{
		ResourceKey: "domain_slot",
		Scope:       "linuxdo.space",
		Delta:       1,
		Source:      "manual grant",
		Reason:      "invalid source should fail",
	})
	if err == nil {
		t.Fatalf("expected invalid explicit source to be rejected")
	}

	normalized := NormalizeError(err)
	if normalized.Code != "validation_failed" {
		t.Fatalf("expected validation_failed error, got %s: %v", normalized.Code, err)
	}
	if !strings.Contains(normalized.Message, "source") {
		t.Fatalf("expected source validation guidance, got %q", normalized.Message)
	}
}

// TestCreateQuantityRecordCreatesVisibleBalance verifies that one
// administrator-authored ledger entry is normalized, persisted, and immediately
// visible both in the immutable record stream and the derived live balances.
func TestCreateQuantityRecordCreatesVisibleBalance(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	actor := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 903, "admin")
	target := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 904, "alice")

	service := NewQuantityService(store)
	expiresAt := time.Now().UTC().Add(24 * time.Hour)

	created, err := service.CreateQuantityRecord(ctx, actor, target.ID, AdminCreateQuantityRecordRequest{
		ResourceKey:   "Domain_Slot",
		Scope:         " LinuxDo.Space ",
		Delta:         2,
		Reason:        "manual billing preparation grant",
		ReferenceType: "Redeem_Code",
		ReferenceID:   "SPRING-2026",
		ExpiresAt:     &expiresAt,
	})
	if err != nil {
		t.Fatalf("create quantity record: %v", err)
	}

	if created.ResourceKey != "domain_slot" {
		t.Fatalf("expected normalized resource key domain_slot, got %q", created.ResourceKey)
	}
	if created.Scope != "linuxdo.space" {
		t.Fatalf("expected normalized scope linuxdo.space, got %q", created.Scope)
	}
	if created.Source != QuantitySourceAdminManual {
		t.Fatalf("expected blank source to default to %q, got %q", QuantitySourceAdminManual, created.Source)
	}
	if created.ReferenceType != "redeem_code" {
		t.Fatalf("expected normalized reference type redeem_code, got %q", created.ReferenceType)
	}
	if created.CreatedByUserID == nil || *created.CreatedByUserID != actor.ID {
		t.Fatalf("expected created_by_user_id %d, got %+v", actor.ID, created.CreatedByUserID)
	}
	if created.CreatedByUsername != actor.Username {
		t.Fatalf("expected created_by_username %q, got %q", actor.Username, created.CreatedByUsername)
	}

	records, err := service.ListQuantityRecordsForUser(ctx, target.ID)
	if err != nil {
		t.Fatalf("list quantity records for user: %v", err)
	}
	if len(records) != 1 || records[0].ID != created.ID {
		t.Fatalf("expected created record to appear in ledger, got %+v", records)
	}

	balances, err := service.ListQuantityBalancesForUser(ctx, target.ID)
	if err != nil {
		t.Fatalf("list quantity balances for user: %v", err)
	}
	if len(balances) != 1 {
		t.Fatalf("expected one visible quantity balance, got %+v", balances)
	}
	if balances[0].ResourceKey != "domain_slot" || balances[0].Scope != "linuxdo.space" || balances[0].CurrentQuantity != 2 {
		t.Fatalf("unexpected quantity balance: %+v", balances[0])
	}
}

// TestListQuantityBalancesOverlaysCatchAllRuntimeState verifies that the generic
// quantity-balance endpoint does not leak stale grant totals for catch-all
// entitlements after runtime consumption and expiry rules have changed the
// actually usable balance.
func TestListQuantityBalancesOverlaysCatchAllRuntimeState(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	actor := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 905, "admin")
	target := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 906, "alice")
	service := NewQuantityService(store)

	if _, err := service.CreateQuantityRecord(ctx, actor, target.ID, AdminCreateQuantityRecordRequest{
		ResourceKey: QuantityResourceEmailCatchAllSubscriptionDays,
		Scope:       PermissionKeyEmailCatchAll,
		Delta:       30,
		Reason:      "grant catch-all subscription days",
	}); err != nil {
		t.Fatalf("create subscription quantity record: %v", err)
	}
	if _, err := service.CreateQuantityRecord(ctx, actor, target.ID, AdminCreateQuantityRecordRequest{
		ResourceKey: QuantityResourceEmailCatchAllRemainingCount,
		Scope:       PermissionKeyEmailCatchAll,
		Delta:       100,
		Reason:      "grant catch-all remaining count",
	}); err != nil {
		t.Fatalf("create remaining count quantity record: %v", err)
	}

	subscriptionExpiresAt := time.Now().UTC().Add(36 * time.Hour)
	temporaryRewardExpiresAt := time.Now().UTC().Add(6 * time.Hour)
	if _, err := store.UpsertEmailCatchAllAccess(ctx, storage.UpsertEmailCatchAllAccessInput{
		UserID:                   target.ID,
		SubscriptionExpiresAt:    &subscriptionExpiresAt,
		RemainingCount:           4,
		TemporaryRewardCount:     3,
		TemporaryRewardExpiresAt: &temporaryRewardExpiresAt,
	}); err != nil {
		t.Fatalf("upsert catch-all access: %v", err)
	}

	balances, err := service.ListQuantityBalancesForUser(ctx, target.ID)
	if err != nil {
		t.Fatalf("list quantity balances for user: %v", err)
	}

	balanceByKey := make(map[string]int)
	for _, item := range balances {
		if item.Scope == PermissionKeyEmailCatchAll {
			balanceByKey[item.ResourceKey] = item.CurrentQuantity
		}
	}

	if got := balanceByKey[QuantityResourceEmailCatchAllSubscriptionDays]; got != 2 {
		t.Fatalf("expected runtime subscription balance of 2 days, got %d from %+v", got, balances)
	}
	if got := balanceByKey[QuantityResourceEmailCatchAllRemainingCount]; got != 7 {
		t.Fatalf("expected runtime remaining count balance of 7, got %d from %+v", got, balances)
	}
}
