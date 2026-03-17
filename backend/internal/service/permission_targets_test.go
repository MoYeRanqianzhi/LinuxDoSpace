package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
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

// TestCreateMyEmailTargetRepairsCloudflareCreateResponseLoss verifies that the
// service can recover when Cloudflare creates the destination address but the
// immediate API response fails before the backend receives the new id.
func TestCreateMyEmailTargetRepairsCloudflareCreateResponseLoss(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 102, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{
		addressesByAccount:    make(map[string][]cloudflare.EmailRoutingDestinationAddress),
		createAddressThenFail: errors.New("simulated response loss"),
	}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "recover@example.com"})
	if err != nil {
		t.Fatalf("create email target with repairable cloudflare failure: %v", err)
	}
	if item.Email != "recover@example.com" {
		t.Fatalf("expected normalized target email, got %q", item.Email)
	}
	if item.CloudflareAddressID == "" {
		t.Fatalf("expected repaired target to keep the cloudflare destination id")
	}
	if len(cf.createdAddresses) != 1 || cf.createdAddresses[0] != "recover@example.com" {
		t.Fatalf("expected exactly one remote destination create, got %v", cf.createdAddresses)
	}

	stored, err := store.GetEmailTargetByEmail(ctx, "recover@example.com")
	if err != nil {
		t.Fatalf("load repaired email target: %v", err)
	}
	if stored.CloudflareAddressID == "" {
		t.Fatalf("expected repaired local row to persist the cloudflare destination id")
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

// TestCreateMyEmailTargetKeepsRecentPendingBindingDuringPropagationWindow
// verifies that a repeated add attempt during Cloudflare propagation returns
// the existing pending row instead of creating another destination address.
func TestCreateMyEmailTargetKeepsRecentPendingBindingDuringPropagationWindow(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 203, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	sentAt := time.Now().UTC()
	existing, err := store.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
		OwnerUserID:            user.ID,
		Email:                  "pending@example.com",
		CloudflareAddressID:    "addr-pending",
		VerifiedAt:             nil,
		LastVerificationSentAt: &sentAt,
	})
	if err != nil {
		t.Fatalf("seed recent pending email target: %v", err)
	}

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "pending@example.com"})
	if err != nil {
		t.Fatalf("repeat create for recent pending target: %v", err)
	}
	if item.ID != existing.ID {
		t.Fatalf("expected existing local target id %d, got %d", existing.ID, item.ID)
	}
	if item.CloudflareAddressID != "addr-pending" {
		t.Fatalf("expected existing cloudflare id to stay stable, got %q", item.CloudflareAddressID)
	}
	if len(cf.createdAddresses) != 0 {
		t.Fatalf("expected no additional remote destination create during propagation window, got %v", cf.createdAddresses)
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

// TestListMyEmailTargetsKeepsRecentPendingBindingWithoutCloudflareSnapshot
// verifies that a just-created pending target does not get its local binding
// cleared merely because Cloudflare's list endpoint has not caught up yet.
func TestListMyEmailTargetsKeepsRecentPendingBindingWithoutCloudflareSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 402, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	sentAt := time.Now().UTC()
	if _, err := store.CreateEmailTarget(ctx, storage.CreateEmailTargetInput{
		OwnerUserID:            user.ID,
		Email:                  "lagging@example.com",
		CloudflareAddressID:    "addr-lagging",
		VerifiedAt:             nil,
		LastVerificationSentAt: &sentAt,
	}); err != nil {
		t.Fatalf("seed lagging pending email target: %v", err)
	}

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	items, err := service.ListMyEmailTargets(ctx, user)
	if err != nil {
		t.Fatalf("list my email targets during propagation window: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected one visible email target, got %d", len(items))
	}
	if items[0].CloudflareAddressID != "addr-lagging" {
		t.Fatalf("expected local cloudflare id to survive propagation lag, got %q", items[0].CloudflareAddressID)
	}
	if items[0].VerificationStatus != EmailTargetVerificationPending {
		t.Fatalf("expected target to remain pending, got %q", items[0].VerificationStatus)
	}

	stored, err := store.GetEmailTargetByEmail(ctx, "lagging@example.com")
	if err != nil {
		t.Fatalf("reload lagging email target: %v", err)
	}
	if stored.CloudflareAddressID != "addr-lagging" {
		t.Fatalf("expected backing store to preserve cloudflare id during propagation lag, got %q", stored.CloudflareAddressID)
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

	oldSentAt := time.Now().UTC().Add(-emailTargetVerificationResendCooldown - time.Second)
	if _, err := store.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     item.ID,
		CloudflareAddressID:    item.CloudflareAddressID,
		VerifiedAt:             nil,
		LastVerificationSentAt: &oldSentAt,
	}); err != nil {
		t.Fatalf("age pending email target verification timestamp: %v", err)
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

// TestResendMyEmailTargetVerificationRejectsRapidRepeat verifies that the
// endpoint enforces a server-side resend cooldown instead of trusting the
// frontend to suppress duplicate clicks.
func TestResendMyEmailTargetVerificationRejectsRapidRepeat(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 503, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "rapid@example.com"})
	if err != nil {
		t.Fatalf("create pending email target: %v", err)
	}

	_, err = service.ResendMyEmailTargetVerification(ctx, user, item.ID)
	if err == nil {
		t.Fatalf("expected resend cooldown rejection")
	}

	normalized := NormalizeError(err)
	if normalized.Code != "too_many_requests" {
		t.Fatalf("expected too_many_requests error, got %s: %v", normalized.Code, err)
	}
}

// TestResendMyEmailTargetVerificationClearsStaleBindingAfterCreateFailure
// verifies that a failed recreate does not leave the local row pointing at a
// deleted Cloudflare destination id.
func TestResendMyEmailTargetVerificationClearsStaleBindingAfterCreateFailure(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 504, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "recover@example.com"})
	if err != nil {
		t.Fatalf("create pending email target: %v", err)
	}

	oldSentAt := time.Now().UTC().Add(-emailTargetVerificationResendCooldown - time.Second)
	if _, err := store.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     item.ID,
		CloudflareAddressID:    item.CloudflareAddressID,
		VerifiedAt:             nil,
		LastVerificationSentAt: &oldSentAt,
	}); err != nil {
		t.Fatalf("age pending email target verification timestamp: %v", err)
	}

	cf.failNextCreateAddress = errors.New("upstream create failed")
	if _, err := service.ResendMyEmailTargetVerification(ctx, user, item.ID); err == nil {
		t.Fatalf("expected recreate failure")
	}

	stored, err := store.GetEmailTargetByEmail(ctx, "recover@example.com")
	if err != nil {
		t.Fatalf("reload repaired email target: %v", err)
	}
	if stored.CloudflareAddressID != "" {
		t.Fatalf("expected stale cloudflare destination id to be cleared, got %q", stored.CloudflareAddressID)
	}
	if stored.VerifiedAt != nil {
		t.Fatalf("expected failed recreate to keep target pending")
	}
}

// TestResendMyEmailTargetVerificationRollsBackNewDestinationOnDBFailure
// verifies that the service deletes the freshly created Cloudflare destination
// when the local persistence update fails afterward.
func TestResendMyEmailTargetVerificationRollsBackNewDestinationOnDBFailure(t *testing.T) {
	ctx := context.Background()
	innerStore := newAuthTestStore(t)
	store := &failingEmailTargetUpdateStore{Store: innerStore}
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, innerStore, 505, "alice")
	seedPermissionEmailManagedDomain(t, ctx, innerStore)

	cf := &fakeEmailRoutingCloudflare{addressesByAccount: make(map[string][]cloudflare.EmailRoutingDestinationAddress)}
	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)

	item, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "rollback@example.com"})
	if err != nil {
		t.Fatalf("create pending email target: %v", err)
	}

	oldSentAt := time.Now().UTC().Add(-emailTargetVerificationResendCooldown - time.Second)
	if _, err := innerStore.UpdateEmailTarget(ctx, storage.UpdateEmailTargetInput{
		ID:                     item.ID,
		CloudflareAddressID:    item.CloudflareAddressID,
		VerifiedAt:             nil,
		LastVerificationSentAt: &oldSentAt,
	}); err != nil {
		t.Fatalf("age pending email target verification timestamp: %v", err)
	}

	store.failNextUpdate = true
	if _, err := service.ResendMyEmailTargetVerification(ctx, user, item.ID); err == nil {
		t.Fatalf("expected local update failure")
	}

	if len(cf.deletedAddresses) < 2 {
		t.Fatalf("expected original + rollback destination deletion, got %v", cf.deletedAddresses)
	}

	stored, err := innerStore.GetEmailTargetByEmail(ctx, "rollback@example.com")
	if err != nil {
		t.Fatalf("reload rolled back email target: %v", err)
	}
	if stored.CloudflareAddressID != "" {
		t.Fatalf("expected rolled back target to clear the stale cloudflare id, got %q", stored.CloudflareAddressID)
	}
}

// failingEmailTargetUpdateStore lets one test simulate a persistence failure on
// the resend path without reimplementing the full storage contract.
type failingEmailTargetUpdateStore struct {
	*sqlite.Store
	failNextUpdate bool
}

// UpdateEmailTarget fails once when requested so the service rollback path can
// be exercised against the real SQLite-backed repository implementation.
func (s *failingEmailTargetUpdateStore) UpdateEmailTarget(ctx context.Context, input storage.UpdateEmailTargetInput) (model.EmailTarget, error) {
	if s.failNextUpdate {
		s.failNextUpdate = false
		return model.EmailTarget{}, errors.New("simulated update failure")
	}
	return s.Store.UpdateEmailTarget(ctx, input)
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
