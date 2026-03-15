package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

// TestCreateMyEmailTargetCreatesOwnedPendingBinding verifies that a newly added
// target email is bound to the current user and stays pending until Cloudflare
// confirms ownership through the verification email flow.
func TestCreateMyEmailTargetCreatesOwnedPendingBinding(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 101, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "Owner@Example.com"})
	if err != nil {
		t.Fatalf("create email target: %v", err)
	}
	if item.Email != "owner@example.com" {
		t.Fatalf("expected normalized target email, got %q", item.Email)
	}
	if item.Verified {
		t.Fatalf("expected the new target to stay pending until the verification email is confirmed")
	}
	if len(cf.createdAddresses) != 1 || cf.createdAddresses[0] != "owner@example.com" {
		t.Fatalf("expected one cloudflare destination address to be created, got %v", cf.createdAddresses)
	}

	stored, err := store.GetEmailTargetByEmail(ctx, "owner@example.com")
	if err != nil {
		t.Fatalf("load stored email target: %v", err)
	}
	if stored.OwnerUserID != user.ID {
		t.Fatalf("expected owner user id %d, got %d", user.ID, stored.OwnerUserID)
	}
}

// TestCreateMyEmailTargetRejectsOtherUsersTarget verifies that the same target
// email cannot be rebound by a second user.
func TestCreateMyEmailTargetRejectsOtherUsersTarget(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	userA := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 201, "alice")
	userB := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 202, "bob")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	if _, err := service.CreateMyEmailTarget(ctx, userA, CreateMyEmailTargetRequest{Email: "shared@example.com"}); err != nil {
		t.Fatalf("create first email target: %v", err)
	}

	_, err := service.CreateMyEmailTarget(ctx, userB, CreateMyEmailTargetRequest{Email: "shared@example.com"})
	if err == nil {
		t.Fatalf("expected rebinding another user's target email to fail")
	}

	normalized := NormalizeError(err)
	if normalized.Code != "forbidden" {
		t.Fatalf("expected forbidden error, got %s: %v", normalized.Code, err)
	}
}

// TestUpsertMyDefaultEmailRouteRejectsUnverifiedOwnedTarget verifies that the
// default mailbox can only use targets that have completed Cloudflare
// verification, even when the target is already bound to the current user.
func TestUpsertMyDefaultEmailRouteRejectsUnverifiedOwnedTarget(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 301, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	target, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "pending@example.com"})
	if err != nil {
		t.Fatalf("create pending email target: %v", err)
	}

	_, err = service.UpsertMyDefaultEmailRoute(ctx, user, UpsertMyDefaultEmailRouteRequest{
		TargetEmail: target.Email,
		Enabled:     true,
	})
	if err == nil {
		t.Fatalf("expected unverified target email to be rejected")
	}

	normalized := NormalizeError(err)
	if normalized.Code != "conflict" {
		t.Fatalf("expected conflict error, got %s: %v", normalized.Code, err)
	}
	if !strings.Contains(normalized.Message, "尚未完成 Cloudflare 验证") {
		t.Fatalf("expected verification guidance in error message, got %q", normalized.Message)
	}
}

// TestUpsertMyDefaultEmailRouteAcceptsVerifiedOwnedTarget verifies that the
// default mailbox save succeeds once Cloudflare reports the bound target as
// verified and the service synchronizes that state back into SQLite.
func TestUpsertMyDefaultEmailRouteAcceptsVerifiedOwnedTarget(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 401, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	target, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "verified@example.com"})
	if err != nil {
		t.Fatalf("create email target: %v", err)
	}

	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"][0].Verified = &verifiedAt

	view, err := service.UpsertMyDefaultEmailRoute(ctx, user, UpsertMyDefaultEmailRouteRequest{
		TargetEmail: target.Email,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("save default email route with verified target: %v", err)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured enabled default route, got %+v", view)
	}
	if view.TargetEmail != "verified@example.com" {
		t.Fatalf("expected verified target email to be saved, got %q", view.TargetEmail)
	}

	storedTarget, err := store.GetEmailTargetByEmail(ctx, "verified@example.com")
	if err != nil {
		t.Fatalf("load stored verified target: %v", err)
	}
	if storedTarget.VerifiedAt == nil {
		t.Fatalf("expected verified timestamp to be synchronized into the backing store")
	}
}

// TestResendMyEmailTargetVerificationRecreatesPendingAddress verifies that a
// pending target can trigger a fresh Cloudflare verification email.
func TestResendMyEmailTargetVerificationRecreatesPendingAddress(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 501, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "pending@example.com"})
	if err != nil {
		t.Fatalf("create pending email target: %v", err)
	}

	resent, err := service.ResendMyEmailTargetVerification(ctx, user, item.ID)
	if err != nil {
		t.Fatalf("resend email target verification: %v", err)
	}
	if resent.Verified {
		t.Fatalf("expected resent target to stay pending")
	}
	if resent.LastVerificationSentAt == nil {
		t.Fatalf("expected resent target to record resend timestamp")
	}
	if len(cf.deletedAddresses) != 1 {
		t.Fatalf("expected one destination address deletion, got %v", cf.deletedAddresses)
	}
	if len(cf.createdAddresses) != 2 {
		t.Fatalf("expected create + recreate to happen, got %v", cf.createdAddresses)
	}
	if resent.CloudflareAddressID == item.CloudflareAddressID {
		t.Fatalf("expected resend to recreate the Cloudflare destination address id")
	}
}

// TestResendMyEmailTargetVerificationRejectsVerifiedTarget verifies that
// already-verified targets cannot request another verification email.
func TestResendMyEmailTargetVerificationRejectsVerifiedTarget(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 502, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "verified@example.com"})
	if err != nil {
		t.Fatalf("create verified email target: %v", err)
	}

	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"][0].Verified = &verifiedAt
	if _, err := service.ListMyEmailTargets(ctx, user); err != nil {
		t.Fatalf("sync verified email target: %v", err)
	}

	_, err = service.ResendMyEmailTargetVerification(ctx, user, item.ID)
	if err == nil {
		t.Fatalf("expected verified target resend to be rejected")
	}
	if NormalizeError(err).Code != "conflict" {
		t.Fatalf("expected conflict for verified target resend, got %v", err)
	}
}

// seedPermissionEmailTestUserWithLinuxDOID inserts one local user while letting
// each test control the unique Linux Do user id.
func seedPermissionEmailTestUserWithLinuxDOID(t *testing.T, ctx context.Context, store *sqlite.Store, linuxdoUserID int64, username string) model.User {
	t.Helper()

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  linuxdoUserID,
		Username:       username,
		DisplayName:    username,
		AvatarURL:      "https://example.com/avatar.png",
		TrustLevel:     2,
		IsLinuxDOAdmin: false,
		IsAppAdmin:     false,
	})
	if err != nil {
		t.Fatalf("seed test user: %v", err)
	}
	return user
}
