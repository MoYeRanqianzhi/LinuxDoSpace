package service

import (
	"context"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
)

// TestSyncForwardingStateDatabaseRelaySkipsCloudflareWhenDNSAutomationDisabled
// verifies that the database-relay mode can still behave as a pure database
// write when the operator explicitly disables DNS automation.
func TestSyncForwardingStateDatabaseRelaySkipsCloudflareWhenDNSAutomationDisabled(t *testing.T) {
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = false

	persistCalls := 0
	err := newEmailRoutingProvisioner(cfg, nil).SyncForwardingState(
		context.Background(),
		newDeletedEmailRouteSyncState("linuxdo.space", "alice"),
		newForwardingEmailRouteSyncState("linuxdo.space", "alice", "owner@example.com", true),
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

// TestDatabaseRelayCatchAllEnsuresRelayDNS verifies that the catch-all save
// path prepares the MX/TXT records needed to deliver mail to the built-in SMTP
// relay instead of Cloudflare Email Routing.
func TestDatabaseRelayCatchAllEnsuresRelayDNS(t *testing.T) {
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

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected relay MX record to be created, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected relay SPF record to be created, got %+v", zoneDNSRecords)
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

// TestDatabaseRelayDefaultMailboxEnsuresRelayDNS verifies that the always-owned
// exact mailbox path also prepares the SMTP-relay ingress DNS when the public
// root domain has not been bootstrapped yet.
func TestDatabaseRelayDefaultMailboxEnsuresRelayDNS(t *testing.T) {
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
	if !hasDNSRecord(zoneDNSRecords, "MX", "linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected default-domain relay MX record to be created, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected default-domain relay SPF record to be created, got %+v", zoneDNSRecords)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
}

// TestDatabaseRelayModeIgnoresCloudflareSnapshots verifies that the database
// relay mode does not treat remote Cloudflare state as a fallback truth source
// for public email search or the mailbox settings page.
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

	forwardingSnapshot, err := service.lookupCloudflareForwardingSnapshot(ctx, "linuxdo.space", "alice")
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
		t.Fatalf("expected search to ignore stale cloudflare-only state, got %+v", availability)
	}
}
