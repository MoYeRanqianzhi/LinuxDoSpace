package service

import (
	"context"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
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

// TestDatabaseRelayCatchAllPermissionApprovalEnsuresRelayDNS verifies that the
// relay MX/TXT records are prepared when the catch-all permission becomes
// approved, instead of waiting until the user later saves a forwarding target.
func TestDatabaseRelayCatchAllPermissionApprovalEnsuresRelayDNS(t *testing.T) {
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

	service := NewPermissionService(cfg, store, cf)
	target, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "owner@example.com"})
	if err != nil {
		t.Fatalf("create owned email target: %v", err)
	}

	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"][0].Verified = &verifiedAt

	if _, err := service.SubmitPermissionApplication(ctx, user, SubmitPermissionApplicationRequest{Key: PermissionKeyEmailCatchAll}); err != nil {
		t.Fatalf("submit catch-all permission application: %v", err)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected relay MX record to be created on permission approval, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected relay SPF record to be created on permission approval, got %+v", zoneDNSRecords)
	}
	initialDNSRecordCount := len(zoneDNSRecords)

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
	if len(zoneDNSRecords) != initialDNSRecordCount {
		t.Fatalf("expected catch-all save to reuse existing relay dns records, got %+v", zoneDNSRecords)
	}

	for _, item := range zoneDNSRecords {
		if item.Name != "alice.linuxdo.space" {
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

// TestDatabaseRelayDefaultMailboxKeepsCloudflareExactForwarding verifies that
// the parent-domain exact mailbox still uses Cloudflare Email Routing even
// while subdomain catch-all delivery uses the local relay.
func TestDatabaseRelayDefaultMailboxKeepsCloudflareExactForwarding(t *testing.T) {
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

	service := NewPermissionService(cfg, store, cf)
	target, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: "owner@example.com"})
	if err != nil {
		t.Fatalf("create owned email target: %v", err)
	}

	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"][0].Verified = &verifiedAt

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
	if len(zoneDNSRecords) != 0 {
		t.Fatalf("expected no relay dns records for the parent domain mailbox, got %+v", zoneDNSRecords)
	}
	exactRule, found := findEmailRoutingRuleByAddress(cf.rulesByZone["zone-default"], "alice@linuxdo.space")
	if !found {
		t.Fatalf("expected cloudflare exact email-routing rule for the parent domain mailbox, got %+v", cf.rulesByZone["zone-default"])
	}
	if targetEmail := extractForwardTargetEmail(exactRule); targetEmail != "owner@example.com" {
		t.Fatalf("expected parent-domain exact route to forward to owner@example.com, got %q", targetEmail)
	}
}

// TestDatabaseRelayAdminGrantEnsuresRelayDNS verifies that manual permission
// approval from the administrator flow bootstraps relay DNS before the user
// saves any concrete catch-all forwarding target.
func TestDatabaseRelayAdminGrantEnsuresRelayDNS(t *testing.T) {
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
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected admin approval to create relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected admin approval to create relay SPF record, got %+v", zoneDNSRecords)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
	if len(cf.catchAllRuleByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare catch-all rule writes, got %+v", cf.catchAllRuleByZone["zone-default"])
	}
}

// TestDatabaseRelayModeUsesCloudflareSnapshotsOnlyForParentDomain verifies that
// hybrid mode still trusts Cloudflare for parent-domain exact mailboxes while
// keeping subdomain catch-all state database-only.
func TestDatabaseRelayModeUsesCloudflareSnapshotsOnlyForParentDomain(t *testing.T) {
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
	if !forwardingSnapshot.Found {
		t.Fatalf("expected parent-domain exact route snapshot to stay visible in hybrid mode")
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
	if availability.Available {
		t.Fatalf("expected parent-domain search to respect existing cloudflare exact-route state, got %+v", availability)
	}
}
