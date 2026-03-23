package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/storage"
)

// TestSyncForwardingStateDatabaseRelaySkipsCloudflareForSubdomainExactRoute
// verifies that a subdomain-scoped exact mailbox still behaves as a pure
// database write when relay DNS automation is disabled.
func TestSyncForwardingStateDatabaseRelaySkipsCloudflareForSubdomainExactRoute(t *testing.T) {
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = false

	persistCalls := 0
	err := newEmailRoutingProvisioner(cfg, nil).SyncForwardingState(
		context.Background(),
		newDeletedEmailRouteSyncState("alice.linuxdo.space", "hello"),
		newForwardingEmailRouteSyncState("alice.linuxdo.space", "hello", "owner@example.com", true),
		func() error {
			persistCalls++
			return nil
		},
	)
	if err != nil {
		t.Fatalf("sync forwarding state in database relay mode: %v", err)
	}
	if persistCalls != 1 {
		t.Fatalf("expected persist callback to run exactly once, got %d", persistCalls)
	}
}

// TestWrapEmailRoutingUnavailableExplainsDestinationAddressLimit verifies that
// Cloudflare's generic "Limit Exceeded" response becomes an operator-actionable
// message instead of the earlier opaque destination-address creation failure.
func TestWrapEmailRoutingUnavailableExplainsDestinationAddressLimit(t *testing.T) {
	err := wrapEmailRoutingUnavailable(
		"failed to create cloudflare email routing destination address",
		errors.New("cloudflare api error: Limit Exceeded"),
	)
	normalized := NormalizeError(err)
	if normalized.StatusCode != 503 {
		t.Fatalf("expected service_unavailable error, got %+v", normalized)
	}
	if normalized.Code != "service_unavailable" {
		t.Fatalf("expected service_unavailable code, got %q", normalized.Code)
	}
	if !strings.Contains(normalized.Message, "destination-address limit") {
		t.Fatalf("expected limit guidance in message, got %q", normalized.Message)
	}
}

// TestDatabaseRelayCatchAllPermissionApprovalDefersRelayDNSUntilRouteSave
// verifies that permission approval alone no longer burns Cloudflare DNS quota.
// The relay MX/TXT records are allocated lazily when the user actually saves a
// concrete catch-all forwarding target.
func TestDatabaseRelayCatchAllPermissionApprovalDefersRelayDNSUntilRouteSave(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 702, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	service, mailer := newPermissionServiceWithTestMailer(cfg, store, cf)
	target := createVerifiedPermissionEmailTarget(t, ctx, service, mailer, user, "owner@example.com")

	if _, err := service.SubmitPermissionApplication(ctx, user, SubmitPermissionApplicationRequest{Key: PermissionKeyEmailCatchAll}); err != nil {
		t.Fatalf("submit catch-all permission application: %v", err)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if len(zoneDNSRecords) != 0 {
		t.Fatalf("expected permission approval to defer relay dns allocation, got %+v", zoneDNSRecords)
	}

	view, err := service.UpsertMyCatchAllEmailRoute(ctx, user, UpsertMyCatchAllEmailRouteRequest{
		TargetEmail: target.Email,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("save catch-all email route in database relay mode: %v", err)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured enabled catch-all route, got %+v", view)
	}

	zoneDNSRecords = cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice-mail.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected catch-all save to allocate relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice-mail.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected catch-all save to allocate relay SPF record, got %+v", zoneDNSRecords)
	}

	for _, item := range zoneDNSRecords {
		if item.Name != "alice-mail.linuxdo.space" {
			continue
		}
		if item.Comment != databaseRelayManagedDNSComment {
			t.Fatalf("expected relay-managed dns comment, got %+v", item)
		}
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
	if len(cf.catchAllRuleByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare catch-all rule writes, got %+v", cf.catchAllRuleByZone["zone-default"])
	}
}

// TestDatabaseRelayDefaultMailboxUsesLocalRelayIngress verifies that the
// parent-domain default mailbox now boots the same relay MX/TXT ingress as the
// child-namespace routes, without creating any Cloudflare Email Routing rule.
func TestDatabaseRelayDefaultMailboxUsesLocalRelayIngress(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 703, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	service, mailer := newPermissionServiceWithTestMailer(cfg, store, cf)
	target := createVerifiedPermissionEmailTarget(t, ctx, service, mailer, user, "owner@example.com")

	view, err := service.UpsertMyDefaultEmailRoute(ctx, user, UpsertMyDefaultEmailRouteRequest{
		TargetEmail: target.Email,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("save default email route in database relay mode: %v", err)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured enabled default route, got %+v", view)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected relay MX record for the parent domain mailbox, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected relay SPF record for the parent domain mailbox, got %+v", zoneDNSRecords)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
}

// TestDatabaseRelayAdminGrantDefersRelayDNSUntilRouteSave verifies that manual
// administrator approval follows the same quota-preserving lazy bootstrap rule
// as the self-service application flow.
func TestDatabaseRelayAdminGrantDefersRelayDNSUntilRouteSave(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	admin := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 704, "admin")
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 705, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	service := NewPermissionService(cfg, store, cf)
	if _, err := service.SetPermissionForUser(ctx, admin, user.ID, PermissionKeyEmailCatchAll, AdminSetUserPermissionRequest{
		Status: "approved",
		Reason: "admin approval",
	}); err != nil {
		t.Fatalf("grant catch-all permission from admin flow: %v", err)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if len(zoneDNSRecords) != 0 {
		t.Fatalf("expected admin approval to defer relay dns allocation, got %+v", zoneDNSRecords)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
	if len(cf.catchAllRuleByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare catch-all rule writes, got %+v", cf.catchAllRuleByZone["zone-default"])
	}
}

// TestDatabaseRelayCatchAllSaveBackfillsMissingIngressDNS verifies that saving
// a catch-all route repairs relay MX/TXT records when the permission had been
// approved earlier, before the deployment switched to database-relay mode.
func TestDatabaseRelayCatchAllSaveBackfillsMissingIngressDNS(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 706, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	service, mailer := newPermissionServiceWithTestMailer(cfg, store, cf)
	target := createVerifiedPermissionEmailTarget(t, ctx, service, mailer, user, "owner@example.com")

	if _, err := store.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          "*@alice-mail.linuxdo.space",
		Reason:          "approved before database relay mode was enabled",
		Status:          "approved",
	}); err != nil {
		t.Fatalf("seed approved catch-all application without dns bootstrap: %v", err)
	}

	view, err := service.UpsertMyCatchAllEmailRoute(ctx, user, UpsertMyCatchAllEmailRouteRequest{
		TargetEmail: target.Email,
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("save catch-all email route with missing relay dns: %v", err)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured enabled catch-all route, got %+v", view)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice-mail.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected catch-all save to backfill relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice-mail.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected catch-all save to backfill relay SPF record, got %+v", zoneDNSRecords)
	}
}

// TestEnsureDatabaseRelayIngressDNSStatePrunesOrphansAndBackfillsActiveRoutes
// verifies that startup now reconciles relay DNS against actual active routes:
// stale permission-only records are deleted, enabled subdomain routes are
// backfilled, and the default public root is always kept pointed at the relay.
func TestEnsureDatabaseRelayIngressDNSStatePrunesOrphansAndBackfillsActiveRoutes(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	alice := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 707, "alice")
	bob := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 708, "bob")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, alice, "linuxdo.space", "alice")
	seedPermissionEmailAllocation(t, ctx, store, bob, "linuxdo.space", "bob")

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: bob.ID,
		RootDomain:  "bob.linuxdo.space",
		Prefix:      "hello",
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("seed bob subdomain email route: %v", err)
	}
	priorityTen := 10
	cf.dnsRecordsByZone["zone-default"] = []cloudflare.DNSRecord{
		{
			ID:       "stale-mx",
			Type:     "MX",
			Name:     "alice.linuxdo.space",
			Content:  "mail.linuxdo.space",
			TTL:      1,
			Comment:  databaseRelayManagedDNSComment,
			Priority: &priorityTen,
		},
		{
			ID:      "stale-txt",
			Type:    "TXT",
			Name:    "alice.linuxdo.space",
			Content: "v=spf1 -all",
			TTL:     1,
			Comment: databaseRelayManagedDNSComment,
		},
	}

	if err := EnsureDatabaseRelayIngressDNSState(ctx, cfg, store, cf); err != nil {
		t.Fatalf("backfill database relay ingress dns on startup: %v", err)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected startup reconciliation to delete stale alice relay MX record, got %+v", zoneDNSRecords)
	}
	if hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected startup reconciliation to delete stale alice relay SPF record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "MX", "bob.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected startup bootstrap to create bob relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "bob.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected startup bootstrap to create bob relay SPF record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "MX", "linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected startup bootstrap to keep the default root on the relay, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected startup bootstrap to keep the default root spf record, got %+v", zoneDNSRecords)
	}
}

// TestEnsureDatabaseRelayIngressDNSStateWarnsWhenDefaultRootMXIsManagedByCloudflareEmailRouting
// verifies that startup no longer crashes when Cloudflare refuses MX writes on
// the default root because that exact root still uses Cloudflare-managed Email
// Routing. The backend should continue booting while still backfilling active
// child namespaces that require local relay ingress.
func TestEnsureDatabaseRelayIngressDNSStateWarnsWhenDefaultRootMXIsManagedByCloudflareEmailRouting(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 709, "bob")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "bob")

	cf := newFakeEmailRoutingCloudflare()
	cf.createDNSErrors["MX:linuxdo.space"] = errors.New("cloudflare api error: This zone is managed by Email Routing. Disable Email Routing to add/modify MX records.")

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  "bob.linuxdo.space",
		Prefix:      "hello",
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("seed bob subdomain email route: %v", err)
	}

	err := EnsureDatabaseRelayIngressDNSState(ctx, cfg, store, cf)
	var warning *DatabaseRelayIngressDNSBootstrapWarning
	if !errors.As(err, &warning) {
		t.Fatalf("expected bootstrap warning, got %v", err)
	}
	if len(warning.Warnings) != 1 {
		t.Fatalf("expected one aggregated warning, got %+v", warning.Warnings)
	}
	if !strings.Contains(warning.Error(), "Cloudflare-managed Email Routing MX records") {
		t.Fatalf("expected operator-facing warning detail, got %q", warning.Error())
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "bob.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected child namespace relay MX bootstrap to continue, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "bob.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected child namespace relay SPF bootstrap to continue, got %+v", zoneDNSRecords)
	}
	if hasDNSRecord(zoneDNSRecords, "MX", "linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected default root relay MX creation to be skipped after Cloudflare-managed warning, got %+v", zoneDNSRecords)
	}
}

// TestEnsureDatabaseRelayIngressDNSStateStillFailsForManagedChildNamespaceMXConflict
// verifies that the new startup warning path stays narrowly scoped to the
// default root. A child namespace that cannot receive relay MX/TXT records must
// still fail closed because user-facing catch-all forwarding would otherwise be
// silently broken.
func TestEnsureDatabaseRelayIngressDNSStateStillFailsForManagedChildNamespaceMXConflict(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 710, "bob")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "bob")

	cf := newFakeEmailRoutingCloudflare()
	cf.createDNSErrors["MX:bob.linuxdo.space"] = errors.New("cloudflare api error: This zone is managed by Email Routing. Disable Email Routing to add/modify MX records.")

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"

	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  "bob.linuxdo.space",
		Prefix:      "hello",
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("seed bob subdomain email route: %v", err)
	}

	err := EnsureDatabaseRelayIngressDNSState(ctx, cfg, store, cf)
	if err == nil {
		t.Fatalf("expected child namespace bootstrap to fail closed")
	}
	var warning *DatabaseRelayIngressDNSBootstrapWarning
	if errors.As(err, &warning) {
		t.Fatalf("expected hard failure instead of warning, got %v", warning)
	}
	if !strings.Contains(err.Error(), "bob.linuxdo.space") {
		t.Fatalf("expected failing namespace in error, got %v", err)
	}
}

// TestDatabaseRelayModeIgnoresCloudflareSnapshots verifies that once the
// database relay backend is selected, remote Cloudflare Email Routing state no
// longer affects search or read paths.
func TestDatabaseRelayModeIgnoresCloudflareSnapshots(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 701, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	cf := &fakeEmailRoutingCloudflare{
		rulesByZone: map[string][]cloudflare.EmailRoutingRule{
			"zone-default": {
				{
					ID:      "rule-default-mailbox",
					Enabled: true,
					Matchers: []cloudflare.EmailRoutingRuleMatcher{{
						Type:  "literal",
						Field: "to",
						Value: "hello@linuxdo.space",
					}},
					Actions: []cloudflare.EmailRoutingRuleAction{{
						Type:  "forward",
						Value: []string{"remote@example.com"},
					}},
				},
			},
		},
		catchAllRuleByZone: map[string]map[string]cloudflare.EmailRoutingRule{
			"zone-default": {
				"alice.linuxdo.space": {
					ID:      "catch-all-1",
					Enabled: true,
					Matchers: []cloudflare.EmailRoutingRuleMatcher{{
						Type: "all",
					}},
					Actions: []cloudflare.EmailRoutingRuleAction{{
						Type:  "forward",
						Value: []string{"remote@example.com"},
					}},
				},
			},
		},
	}

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	service := NewPermissionService(cfg, store, cf)

	forwardingSnapshot, err := service.lookupCloudflareForwardingSnapshot(ctx, "linuxdo.space", "hello")
	if err != nil {
		t.Fatalf("lookup forwarding snapshot in database relay mode: %v", err)
	}
	if forwardingSnapshot.Found {
		t.Fatalf("expected database relay mode to ignore cloudflare exact-route snapshots")
	}

	catchAllSnapshot, err := service.lookupCloudflareCatchAllSnapshot(ctx, "alice.linuxdo.space")
	if err != nil {
		t.Fatalf("lookup catch-all snapshot in database relay mode: %v", err)
	}
	if catchAllSnapshot.Found {
		t.Fatalf("expected database relay mode to ignore cloudflare catch-all snapshots")
	}

	availability, err := service.CheckPublicEmailAvailability(ctx, "linuxdo.space", "hello")
	if err != nil {
		t.Fatalf("check email availability in database relay mode: %v", err)
	}
	if !availability.Available {
		t.Fatalf("expected parent-domain search to ignore remote cloudflare state in database relay mode, got %+v", availability)
	}
}
