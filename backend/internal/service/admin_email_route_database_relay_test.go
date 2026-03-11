package service

import (
	"context"
	"testing"
)

// TestAdminCreateEmailRouteDatabaseRelayEnsuresRelayDNS verifies that the
// administrator email-route workflow also provisions the SMTP-relay ingress DNS
// records instead of relying on Cloudflare Email Routing.
func TestAdminCreateEmailRouteDatabaseRelayEnsuresRelayDNS(t *testing.T) {
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
	if !hasDNSRecord(zoneDNSRecords, "MX", "linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected admin flow to create relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected admin flow to create relay SPF record, got %+v", zoneDNSRecords)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no cloudflare exact email-routing rule writes, got %+v", cf.rulesByZone["zone-default"])
	}
}
