package service

import (
	"context"
	"testing"
)

// TestUpdateEmailCatchAllAccessForUser verifies that administrators can grant
// subscription days, prepaid remaining count, and a custom daily cap while the
// public permission view immediately reflects the effective access mode.
func TestUpdateEmailCatchAllAccessForUser(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	admin := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 950, "admin")
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 951, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	service := NewPermissionService(newPermissionEmailTestConfig(), store, nil)
	if _, err := service.SubmitPermissionApplication(ctx, user, SubmitPermissionApplicationRequest{Key: PermissionKeyEmailCatchAll}); err != nil {
		t.Fatalf("submit catch-all permission application: %v", err)
	}

	customDailyLimit := int64(321)
	view, err := service.UpdateEmailCatchAllAccessForUser(ctx, admin, user.ID, AdminUpdateEmailCatchAllAccessRequest{
		AddSubscriptionDays: 2,
		RemainingCountDelta: 50,
		DailyLimitOverride:  &customDailyLimit,
		Reason:              "grant paid catch-all access for testing",
	})
	if err != nil {
		t.Fatalf("update email catch-all access: %v", err)
	}

	if view.Status != "approved" {
		t.Fatalf("expected permission to stay approved, got %q", view.Status)
	}
	if view.CatchAllAccess == nil {
		t.Fatalf("expected catch-all access view to be present")
	}
	if view.CatchAllAccess.AccessMode != "subscription" {
		t.Fatalf("expected subscription mode to take priority, got %q", view.CatchAllAccess.AccessMode)
	}
	if !view.CatchAllAccess.SubscriptionActive {
		t.Fatalf("expected subscription to be active")
	}
	if view.CatchAllAccess.RemainingCount != 50 {
		t.Fatalf("expected remaining count 50, got %d", view.CatchAllAccess.RemainingCount)
	}
	if view.CatchAllAccess.EffectiveDailyLimit != customDailyLimit {
		t.Fatalf("expected effective daily limit %d, got %d", customDailyLimit, view.CatchAllAccess.EffectiveDailyLimit)
	}
	if !view.CatchAllAccess.DeliveryAvailable {
		t.Fatalf("expected delivery to be available once approved access exists")
	}

	records, err := store.ListQuantityRecordsByUser(ctx, user.ID)
	if err != nil {
		t.Fatalf("list quantity records after access update: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("expected 2 quantity records, got %d", len(records))
	}

	recordKeys := map[string]int{}
	for _, item := range records {
		recordKeys[item.ResourceKey] = item.Delta
	}
	if recordKeys[QuantityResourceEmailCatchAllSubscriptionDays] != 2 {
		t.Fatalf("expected subscription-days quantity delta 2, got %+v", recordKeys)
	}
	if recordKeys[QuantityResourceEmailCatchAllRemainingCount] != 50 {
		t.Fatalf("expected remaining-count quantity delta 50, got %+v", recordKeys)
	}
}
