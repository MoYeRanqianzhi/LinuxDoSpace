package storagetest

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// Factory opens one migrated backend instance dedicated to the current test
// case. The returned backend must already be ready to serve repository calls.
type Factory func(t *testing.T) storage.Backend

// RunRepositoryBehaviorSuite executes the repository behavior that must stay
// identical across SQLite and PostgreSQL.
//
// The suite deliberately exercises areas where backend differences usually hide:
// transactional state consumption, conflict upserts, NULL handling, and
// ordering that depends on multiple columns.
func RunRepositoryBehaviorSuite(t *testing.T, newStore Factory) {
	t.Helper()

	t.Run("session admin verification persists", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		user := newTestUser(t, ctx, store, "admin-user")
		session, err := store.CreateSession(ctx, storage.CreateSessionInput{
			ID:                   "session-admin-verify",
			UserID:               user.ID,
			CSRFToken:            "csrf-admin-verify",
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
	})

	t.Run("oauth state rollback keeps state reusable when session insert fails", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		user := newTestUser(t, ctx, store, "oauth-user")
		if _, err := store.CreateSession(ctx, storage.CreateSessionInput{
			ID:                   "duplicate-session-id",
			UserID:               user.ID,
			CSRFToken:            "csrf-existing",
			UserAgentFingerprint: "ua-existing",
			ExpiresAt:            time.Now().UTC().Add(time.Hour),
		}); err != nil {
			t.Fatalf("seed existing duplicate session: %v", err)
		}

		state := model.OAuthState{
			ID:           "oauth-state-rollback",
			CodeVerifier: "verifier",
			NextPath:     "/settings",
			ExpiresAt:    time.Now().UTC().Add(5 * time.Minute),
			CreatedAt:    time.Now().UTC(),
		}
		if err := store.SaveOAuthState(ctx, state); err != nil {
			t.Fatalf("save oauth state: %v", err)
		}

		if _, err := store.CreateSessionFromOAuthState(ctx, state.ID, storage.CreateSessionInput{
			ID:                   "duplicate-session-id",
			UserID:               user.ID,
			CSRFToken:            "csrf-new",
			UserAgentFingerprint: "ua-new",
			ExpiresAt:            time.Now().UTC().Add(time.Hour),
		}); err == nil {
			t.Fatalf("expected duplicate session insert to fail")
		}

		reloadedState, err := store.GetOAuthState(ctx, state.ID)
		if err != nil {
			t.Fatalf("expected oauth state to remain after rollback, got %v", err)
		}
		if reloadedState.ID != state.ID {
			t.Fatalf("expected oauth state %q to survive rollback, got %q", state.ID, reloadedState.ID)
		}
	})

	t.Run("public allocation ownership only returns actively used allocations", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

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
	})

	t.Run("allocation transfer keeps one primary per user and domain", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		alice := newTestUser(t, ctx, store, "alice")
		bob := newTestUser(t, ctx, store, "bob")
		managedDomain := newTestManagedDomain(t, ctx, store, "linuxdo.space")

		bobPrimary := newTestAllocation(t, ctx, store, bob, managedDomain, "bob-main", "active", true)
		alicePrimary := newTestAllocation(t, ctx, store, alice, managedDomain, "alice-main", "active", true)

		updated, err := store.UpdateAllocation(ctx, storage.UpdateAllocationInput{
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
	})

	t.Run("email targets keep verified entries ahead of pending entries", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		user := newTestUser(t, ctx, store, "mail-owner")

		oldPending, err := store.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
			OwnerUserID:         user.ID,
			Email:               "old-pending@example.com",
			CloudflareAddressID: "cf-old-pending",
		})
		if err != nil {
			t.Fatalf("create old pending target: %v", err)
		}

		time.Sleep(2 * time.Millisecond)

		verified, err := store.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
			OwnerUserID:         user.ID,
			Email:               "verified@example.com",
			CloudflareAddressID: "cf-verified",
		})
		if err != nil {
			t.Fatalf("create verified target: %v", err)
		}

		time.Sleep(2 * time.Millisecond)

		newPending, err := store.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
			OwnerUserID:         user.ID,
			Email:               "new-pending@example.com",
			CloudflareAddressID: "cf-new-pending",
		})
		if err != nil {
			t.Fatalf("create new pending target: %v", err)
		}

		verifiedAt := time.Now().UTC()
		if _, err := store.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
			ID:                  verified.ID,
			CloudflareAddressID: "cf-verified-updated",
			VerifiedAt:          &verifiedAt,
		}); err != nil {
			t.Fatalf("mark verified target as verified: %v", err)
		}

		items, err := store.ListEmailTargetsByOwner(ctx, user.ID)
		if err != nil {
			t.Fatalf("list email targets by owner: %v", err)
		}
		if len(items) != 3 {
			t.Fatalf("expected 3 email targets, got %d", len(items))
		}
		if items[0].Email != verified.Email {
			t.Fatalf("expected verified email target to sort first, got %q", items[0].Email)
		}
		if items[1].Email != newPending.Email {
			t.Fatalf("expected newer pending target second, got %q", items[1].Email)
		}
		if items[2].Email != oldPending.Email {
			t.Fatalf("expected older pending target last, got %q", items[2].Email)
		}
		if items[0].VerifiedAt == nil {
			t.Fatalf("expected first listed target to include a verified timestamp")
		}
	})

	t.Run("quantity records sort newest first and keep creator identity", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		owner := newTestUser(t, ctx, store, "ledger-owner")
		actor := newTestUser(t, ctx, store, "ledger-admin")

		older, err := store.CreateQuantityRecord(ctx, storage.CreateQuantityRecordInput{
			UserID:          owner.ID,
			ResourceKey:     "domain_slot",
			Scope:           "linuxdo.space",
			Delta:           1,
			Source:          "admin_manual",
			Reason:          "initial grant",
			CreatedByUserID: &actor.ID,
		})
		if err != nil {
			t.Fatalf("create older quantity record: %v", err)
		}

		time.Sleep(2 * time.Millisecond)

		newer, err := store.CreateQuantityRecord(ctx, storage.CreateQuantityRecordInput{
			UserID:          owner.ID,
			ResourceKey:     "domain_slot",
			Scope:           "linuxdo.space",
			Delta:           2,
			Source:          "redeem_code",
			Reason:          "campaign bonus",
			CreatedByUserID: &actor.ID,
		})
		if err != nil {
			t.Fatalf("create newer quantity record: %v", err)
		}

		items, err := store.ListQuantityRecordsByUser(ctx, owner.ID)
		if err != nil {
			t.Fatalf("list quantity records: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 quantity records, got %d", len(items))
		}
		if items[0].ID != newer.ID {
			t.Fatalf("expected newest quantity record id %d first, got %d", newer.ID, items[0].ID)
		}
		if items[1].ID != older.ID {
			t.Fatalf("expected older quantity record id %d second, got %d", older.ID, items[1].ID)
		}
		if items[0].CreatedByUserID == nil || *items[0].CreatedByUserID != actor.ID {
			t.Fatalf("expected creator user id %d on newest record, got %+v", actor.ID, items[0].CreatedByUserID)
		}
		if items[0].CreatedByUsername != actor.Username {
			t.Fatalf("expected creator username %q, got %q", actor.Username, items[0].CreatedByUsername)
		}
	})

	t.Run("quantity balances sum only active deltas and omit expired or zero groups", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		owner := newTestUser(t, ctx, store, "balance-owner")
		now := time.Now().UTC()
		expiredAt := now.Add(-time.Hour)
		futureAt := now.Add(24 * time.Hour)

		seedQuantityRecord := func(input storage.CreateQuantityRecordInput) {
			t.Helper()
			if _, err := store.CreateQuantityRecord(ctx, input); err != nil {
				t.Fatalf("create quantity record %+v: %v", input, err)
			}
		}

		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "domain_slot",
			Scope:       "linuxdo.space",
			Delta:       2,
			Source:      "admin_manual",
			Reason:      "base grant",
		})
		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "domain_slot",
			Scope:       "linuxdo.space",
			Delta:       -1,
			Source:      "consumption",
			Reason:      "manual deduction",
		})
		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "email_alias",
			Scope:       "",
			Delta:       3,
			Source:      "subscription",
			Reason:      "monthly plan",
			ExpiresAt:   &futureAt,
		})
		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "expired_bucket",
			Scope:       "",
			Delta:       9,
			Source:      "admin_manual",
			Reason:      "expired grant",
			ExpiresAt:   &expiredAt,
		})
		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "zero_bucket",
			Scope:       "",
			Delta:       4,
			Source:      "admin_manual",
			Reason:      "temporary grant",
		})
		seedQuantityRecord(storage.CreateQuantityRecordInput{
			UserID:      owner.ID,
			ResourceKey: "zero_bucket",
			Scope:       "",
			Delta:       -4,
			Source:      "consumption",
			Reason:      "fully consumed",
		})

		items, err := store.ListQuantityBalancesByUser(ctx, owner.ID, now)
		if err != nil {
			t.Fatalf("list quantity balances: %v", err)
		}
		if len(items) != 2 {
			t.Fatalf("expected 2 non-zero active quantity balances, got %d: %+v", len(items), items)
		}

		if items[0].ResourceKey != "domain_slot" || items[0].Scope != "linuxdo.space" || items[0].CurrentQuantity != 1 {
			t.Fatalf("unexpected first quantity balance: %+v", items[0])
		}
		if items[1].ResourceKey != "email_alias" || items[1].Scope != "" || items[1].CurrentQuantity != 3 {
			t.Fatalf("unexpected second quantity balance: %+v", items[1])
		}

		for _, item := range items {
			if item.ResourceKey == "expired_bucket" {
				t.Fatalf("expired-only balance should not be returned: %+v", item)
			}
			if item.ResourceKey == "zero_bucket" {
				t.Fatalf("zeroed-out balance should not be returned: %+v", item)
			}
		}
	})

	t.Run("email catch-all consumption prefers subscription and then decrements remaining count", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		user := newTestUser(t, ctx, store, "catchall-owner")
		policy, err := store.GetPermissionPolicy(ctx, "email_catch_all")
		if err != nil {
			t.Fatalf("load email catch-all policy: %v", err)
		}
		if policy.DefaultDailyLimit != 1_000_000 {
			t.Fatalf("expected default daily limit 1000000, got %d", policy.DefaultDailyLimit)
		}

		now := time.Now().UTC()
		subscriptionExpiresAt := now.Add(48 * time.Hour)
		dailyLimitOverride := int64(3)
		if _, err := store.UpsertEmailCatchAllAccess(ctx, storage.UpsertEmailCatchAllAccessInput{
			UserID:                user.ID,
			SubscriptionExpiresAt: &subscriptionExpiresAt,
			RemainingCount:        5,
			DailyLimitOverride:    &dailyLimitOverride,
		}); err != nil {
			t.Fatalf("upsert email catch-all access with active subscription: %v", err)
		}

		firstConsume, err := store.ConsumeEmailCatchAll(ctx, storage.ConsumeEmailCatchAllInput{
			UserID:            user.ID,
			Count:             2,
			DefaultDailyLimit: policy.DefaultDailyLimit,
			Now:               now,
		})
		if err != nil {
			t.Fatalf("consume email catch-all with active subscription: %v", err)
		}
		if firstConsume.ConsumedMode != "subscription" {
			t.Fatalf("expected subscription mode to win first, got %q", firstConsume.ConsumedMode)
		}
		if firstConsume.Access.RemainingCount != 5 {
			t.Fatalf("expected remaining count to stay 5 while subscription is active, got %d", firstConsume.Access.RemainingCount)
		}
		if firstConsume.DailyUsage.UsedCount != 2 {
			t.Fatalf("expected used count 2 after first consume, got %d", firstConsume.DailyUsage.UsedCount)
		}

		_, err = store.ConsumeEmailCatchAll(ctx, storage.ConsumeEmailCatchAllInput{
			UserID:            user.ID,
			Count:             2,
			DefaultDailyLimit: policy.DefaultDailyLimit,
			Now:               now,
		})
		if !errors.Is(err, storage.ErrEmailCatchAllDailyLimitExceeded) {
			t.Fatalf("expected daily limit error, got %v", err)
		}

		expiredSubscription := now.Add(-time.Hour)
		resetDailyLimit := int64(10)
		if _, err := store.UpsertEmailCatchAllAccess(ctx, storage.UpsertEmailCatchAllAccessInput{
			UserID:                user.ID,
			SubscriptionExpiresAt: &expiredSubscription,
			RemainingCount:        5,
			DailyLimitOverride:    &resetDailyLimit,
		}); err != nil {
			t.Fatalf("upsert email catch-all access with expired subscription: %v", err)
		}

		secondDay := now.Add(24 * time.Hour)
		secondConsume, err := store.ConsumeEmailCatchAll(ctx, storage.ConsumeEmailCatchAllInput{
			UserID:            user.ID,
			Count:             2,
			DefaultDailyLimit: policy.DefaultDailyLimit,
			Now:               secondDay,
		})
		if err != nil {
			t.Fatalf("consume email catch-all using remaining count: %v", err)
		}
		if secondConsume.ConsumedMode != "quantity" {
			t.Fatalf("expected quantity mode after subscription expiry, got %q", secondConsume.ConsumedMode)
		}
		if secondConsume.Access.RemainingCount != 3 {
			t.Fatalf("expected remaining count to decrement to 3, got %d", secondConsume.Access.RemainingCount)
		}

		_, err = store.ConsumeEmailCatchAll(ctx, storage.ConsumeEmailCatchAllInput{
			UserID:            user.ID,
			Count:             4,
			DefaultDailyLimit: policy.DefaultDailyLimit,
			Now:               secondDay,
		})
		if !errors.Is(err, storage.ErrEmailCatchAllInsufficientRemainingCount) {
			t.Fatalf("expected insufficient remaining count error, got %v", err)
		}
	})

	t.Run("admin application upsert stays idempotent per applicant target", func(t *testing.T) {
		ctx := context.Background()
		store := newStore(t)

		applicant := newTestUser(t, ctx, store, "applicant")

		first, err := store.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
			ApplicantUserID: applicant.ID,
			Type:            "permission",
			Target:          "email_catch_all",
			Reason:          "first reason",
			Status:          "pending",
		})
		if err != nil {
			t.Fatalf("create admin application: %v", err)
		}

		second, err := store.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
			ApplicantUserID: applicant.ID,
			Type:            "permission",
			Target:          "email_catch_all",
			Reason:          "updated reason",
			Status:          "approved",
			ReviewNote:      "auto approved",
		})
		if err != nil {
			t.Fatalf("upsert admin application: %v", err)
		}

		if second.ID != first.ID {
			t.Fatalf("expected upsert to keep one row id=%d, got id=%d", first.ID, second.ID)
		}
		if second.Reason != "updated reason" {
			t.Fatalf("expected reason to be refreshed, got %q", second.Reason)
		}
		if second.Status != "approved" {
			t.Fatalf("expected status to be refreshed, got %q", second.Status)
		}
		if second.ReviewNote != "auto approved" {
			t.Fatalf("expected review note to be refreshed, got %q", second.ReviewNote)
		}

		items, err := store.ListAdminApplicationsByApplicant(ctx, applicant.ID)
		if err != nil {
			t.Fatalf("list admin applications by applicant: %v", err)
		}
		if len(items) != 1 {
			t.Fatalf("expected one persisted admin application, got %d", len(items))
		}
		if items[0].ID != first.ID {
			t.Fatalf("expected stored application id %d, got %d", first.ID, items[0].ID)
		}
	})
}

// newTestUser writes one predictable user row for repository behavior tests.
func newTestUser(t *testing.T, ctx context.Context, store storage.Store, username string) model.User {
	t.Helper()

	linuxDOUserID := int64(1000)
	for _, runeValue := range username {
		linuxDOUserID = linuxDOUserID*31 + int64(runeValue)
	}

	user, err := store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID: linuxDOUserID,
		Username:      username,
		DisplayName:   username,
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert test user %q: %v", username, err)
	}
	return user
}

// newTestManagedDomain writes one enabled managed root domain.
func newTestManagedDomain(t *testing.T, ctx context.Context, store storage.Store, rootDomain string) model.ManagedDomain {
	t.Helper()

	item, err := store.UpsertManagedDomain(ctx, storage.UpsertManagedDomainInput{
		RootDomain:       rootDomain,
		CloudflareZoneID: "zone-test",
		DefaultQuota:     10,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	})
	if err != nil {
		t.Fatalf("upsert managed domain %q: %v", rootDomain, err)
	}
	return item
}

// newTestAllocation writes one allocation that the repository tests can later
// inspect, transfer, or attach audit-log activity to.
func newTestAllocation(t *testing.T, ctx context.Context, store storage.Store, user model.User, managedDomain model.ManagedDomain, prefix string, status string, isPrimary ...bool) model.Allocation {
	t.Helper()

	primary := false
	if len(isPrimary) > 0 {
		primary = isPrimary[0]
	}

	item, err := store.CreateAllocation(ctx, storage.CreateAllocationInput{
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

// writeDNSAuditLog records one fake DNS lifecycle event so the supervision
// query can be exercised without touching the real Cloudflare client.
func writeDNSAuditLog(t *testing.T, ctx context.Context, store storage.Store, user model.User, allocation model.Allocation, action string, recordID string) {
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

	if err := store.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       action,
		ResourceType: "dns_record",
		ResourceID:   recordID,
		MetadataJSON: string(metadata),
	}); err != nil {
		t.Fatalf("write dns audit log %q for %q: %v", action, allocation.FQDN, err)
	}
}
