package service

import (
	"context"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
)

// TestAdminCreateEmailRouteDatabaseRelayKeepsCloudflareExactForwarding verifies
// that administrator-created parent-domain exact mailboxes still sync to
// Cloudflare Email Routing while avoiding relay MX/TXT bootstrap.
func TestAdminCreateEmailRouteDatabaseRelayKeepsCloudflareExactForwarding(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	actor := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 801, "admin")
	owner := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 802, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := newFakeEmailRoutingCloudflare()
	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"
	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"] = []cloudflare.EmailRoutingDestinationAddress{{
		ID:       "addr-1",
		Email:    "owner@example.com",
		Verified: &verifiedAt,
	}}

	service := NewAdminService(cfg, store, cf)
	item, err := service.CreateEmailRoute(ctx, actor, UpsertEmailRouteRequest{
		OwnerUserID: owner.ID,
		RootDomain:  "linuxdo.space",
		Prefix:      "hello",
		TargetEmail: "owner@example.com",
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create admin email route in database relay mode: %v", err)
	}
	if item.RootDomain != "linuxdo.space" || item.Prefix != "hello" {
		t.Fatalf("unexpected stored admin email route: %+v", item)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if len(zoneDNSRecords) != 0 {
		t.Fatalf("expected admin exact-route flow to avoid relay dns bootstrap, got %+v", zoneDNSRecords)
	}
	exactRule, found := findEmailRoutingRuleByAddress(cf.rulesByZone["zone-default"], "hello@linuxdo.space")
	if !found {
		t.Fatalf("expected admin exact-route flow to sync one cloudflare exact rule, got %+v", cf.rulesByZone["zone-default"])
	}
	if targetEmail := extractForwardTargetEmail(exactRule); targetEmail != "owner@example.com" {
		t.Fatalf("expected admin exact route to forward to owner@example.com, got %q", targetEmail)
	}
}
