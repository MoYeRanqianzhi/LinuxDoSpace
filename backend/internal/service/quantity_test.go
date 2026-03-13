package service

import (
	"context"
	"strings"
	"testing"
	"time"
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
