package service

import (
	"context"
	"testing"

	"linuxdospace/backend/internal/cloudflare"
)

// TestSyncForwardingStateDatabaseRelaySkipsCloudflare verifies that the new
// database-relay mode writes only the local database mutation and never requires
// Cloudflare Email Routing to succeed.
func TestSyncForwardingStateDatabaseRelaySkipsCloudflare(t *testing.T) {
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"

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
