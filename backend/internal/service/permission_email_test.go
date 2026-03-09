package service

import (
	"context"
	"testing"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

// fakeEmailRoutingCloudflare keeps the tests focused on the permission service
// behavior by emulating only the Email Routing operations exercised here.
type fakeEmailRoutingCloudflare struct {
	rulesByZone map[string][]cloudflare.EmailRoutingRule
	deletedRule []string
}

// ResolveZone is unused in these tests because the configuration pins a default zone id.
func (f *fakeEmailRoutingCloudflare) ResolveZone(ctx context.Context, rootDomain string) (cloudflare.Zone, error) {
	return cloudflare.Zone{}, nil
}

// GetZone is unused in these tests because the configuration pins a Cloudflare account id.
func (f *fakeEmailRoutingCloudflare) GetZone(ctx context.Context, zoneID string) (cloudflare.Zone, error) {
	return cloudflare.Zone{}, nil
}

// ResolveZoneID is unused in these tests because the configuration pins a default zone id.
func (f *fakeEmailRoutingCloudflare) ResolveZoneID(ctx context.Context, rootDomain string) (string, error) {
	return "", nil
}

// ListAllDNSRecords is outside the scope of the email-routing tests.
func (f *fakeEmailRoutingCloudflare) ListAllDNSRecords(ctx context.Context, zoneID string) ([]cloudflare.DNSRecord, error) {
	return nil, nil
}

// GetDNSRecord is outside the scope of the email-routing tests.
func (f *fakeEmailRoutingCloudflare) GetDNSRecord(ctx context.Context, zoneID string, recordID string) (cloudflare.DNSRecord, error) {
	return cloudflare.DNSRecord{}, nil
}

// CreateDNSRecord is outside the scope of the email-routing tests.
func (f *fakeEmailRoutingCloudflare) CreateDNSRecord(ctx context.Context, zoneID string, input cloudflare.CreateDNSRecordInput) (cloudflare.DNSRecord, error) {
	return cloudflare.DNSRecord{}, nil
}

// UpdateDNSRecord is outside the scope of the email-routing tests.
func (f *fakeEmailRoutingCloudflare) UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, input cloudflare.UpdateDNSRecordInput) (cloudflare.DNSRecord, error) {
	return cloudflare.DNSRecord{}, nil
}

// DeleteDNSRecord is outside the scope of the email-routing tests.
func (f *fakeEmailRoutingCloudflare) DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error {
	return nil
}

// ListEmailRoutingDestinationAddresses is unused because the clearing flow only deletes rules.
func (f *fakeEmailRoutingCloudflare) ListEmailRoutingDestinationAddresses(ctx context.Context, accountID string) ([]cloudflare.EmailRoutingDestinationAddress, error) {
	return nil, nil
}

// CreateEmailRoutingDestinationAddress is unused because the clearing flow only deletes rules.
func (f *fakeEmailRoutingCloudflare) CreateEmailRoutingDestinationAddress(ctx context.Context, accountID string, email string) (cloudflare.EmailRoutingDestinationAddress, error) {
	return cloudflare.EmailRoutingDestinationAddress{}, nil
}

// ListEmailRoutingRules returns the in-memory rules currently visible in one zone.
func (f *fakeEmailRoutingCloudflare) ListEmailRoutingRules(ctx context.Context, zoneID string) ([]cloudflare.EmailRoutingRule, error) {
	rules := f.rulesByZone[zoneID]
	cloned := make([]cloudflare.EmailRoutingRule, len(rules))
	copy(cloned, rules)
	return cloned, nil
}

// CreateEmailRoutingRule is unused by the stale-clear regression tests.
func (f *fakeEmailRoutingCloudflare) CreateEmailRoutingRule(ctx context.Context, zoneID string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error) {
	return cloudflare.EmailRoutingRule{}, nil
}

// UpdateEmailRoutingRule is unused by the stale-clear regression tests.
func (f *fakeEmailRoutingCloudflare) UpdateEmailRoutingRule(ctx context.Context, zoneID string, ruleIdentifier string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error) {
	return cloudflare.EmailRoutingRule{}, nil
}

// DeleteEmailRoutingRule removes one in-memory rule so later reads observe the cleanup.
func (f *fakeEmailRoutingCloudflare) DeleteEmailRoutingRule(ctx context.Context, zoneID string, ruleIdentifier string) error {
	f.deletedRule = append(f.deletedRule, ruleIdentifier)

	rules := f.rulesByZone[zoneID]
	filtered := make([]cloudflare.EmailRoutingRule, 0, len(rules))
	for _, item := range rules {
		if item.Identifier() == ruleIdentifier {
			continue
		}
		filtered = append(filtered, item)
	}
	f.rulesByZone[zoneID] = filtered
	return nil
}

// TestResolveDefaultEmailRouteBeforeStateFallsBackToCloudflareSnapshot verifies
// that the service treats Cloudflare as the source of truth when the database row
// is missing after a partial failure.
func TestResolveDefaultEmailRouteBeforeStateFallsBackToCloudflareSnapshot(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUser(t, ctx, store, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{
		rulesByZone: map[string][]cloudflare.EmailRoutingRule{
			"zone-default": {
				{
					ID:      "rule-default-mailbox",
					Enabled: true,
					Matchers: []cloudflare.EmailRoutingRuleMatcher{{
						Type:  "literal",
						Field: "to",
						Value: "alice@linuxdo.space",
					}},
					Actions: []cloudflare.EmailRoutingRuleAction{{
						Type:  "forward",
						Value: []string{"remote@example.com"},
					}},
				},
			},
		},
	}

	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)
	spec, err := service.resolveDefaultEmailRouteSpec(ctx, user)
	if err != nil {
		t.Fatalf("resolve default email route spec: %v", err)
	}

	beforeState, existingRoute, err := service.resolveDefaultEmailRouteBeforeState(ctx, user, spec)
	if err != nil {
		t.Fatalf("resolve default email route before state: %v", err)
	}
	if existingRoute != nil {
		t.Fatalf("expected no local email route row, got %+v", *existingRoute)
	}
	if !beforeState.Exists {
		t.Fatalf("expected cloudflare snapshot to mark the route as existing")
	}
	if beforeState.TargetEmail != "remote@example.com" {
		t.Fatalf("expected cloudflare target remote@example.com, got %q", beforeState.TargetEmail)
	}
	if !beforeState.Enabled {
		t.Fatalf("expected cloudflare snapshot to keep the route enabled")
	}
}

// TestUpsertMyDefaultEmailRouteClearsCloudflareWhenDatabaseRowMissing verifies
// the regression fix for stale remote routes: clearing the default mailbox now
// deletes the Cloudflare rule even when the local database row is already gone.
func TestUpsertMyDefaultEmailRouteClearsCloudflareWhenDatabaseRowMissing(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUser(t, ctx, store, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)

	cf := &fakeEmailRoutingCloudflare{
		rulesByZone: map[string][]cloudflare.EmailRoutingRule{
			"zone-default": {
				{
					ID:      "rule-default-mailbox",
					Enabled: true,
					Matchers: []cloudflare.EmailRoutingRuleMatcher{{
						Type:  "literal",
						Field: "to",
						Value: "alice@linuxdo.space",
					}},
					Actions: []cloudflare.EmailRoutingRuleAction{{
						Type:  "forward",
						Value: []string{"remote@example.com"},
					}},
				},
			},
		},
	}

	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)
	view, err := service.UpsertMyDefaultEmailRoute(ctx, user, UpsertMyDefaultEmailRouteRequest{
		TargetEmail: "",
		Enabled:     false,
	})
	if err != nil {
		t.Fatalf("clear default email route: %v", err)
	}

	if len(cf.deletedRule) != 1 || cf.deletedRule[0] != "rule-default-mailbox" {
		t.Fatalf("expected the stale cloudflare rule to be deleted once, got %v", cf.deletedRule)
	}
	if view.Configured {
		t.Fatalf("expected the returned view to become an unconfigured placeholder")
	}
	if view.TargetEmail != "" {
		t.Fatalf("expected cleared placeholder target to be empty, got %q", view.TargetEmail)
	}

	_, err = store.GetEmailRouteByAddress(ctx, "linuxdo.space", "alice")
	if !sqlite.IsNotFound(err) {
		t.Fatalf("expected no persisted email route row, got %v", err)
	}
}

// newPermissionEmailTestConfig keeps the email-routing tests independent from
// environment loading while still exercising the production code paths.
func newPermissionEmailTestConfig() config.Config {
	return config.Config{
		App: config.AppConfig{
			SessionSecret: []byte("permission-email-test-secret"),
		},
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			AccountID:         "account-default",
			DefaultRootDomain: "linuxdo.space",
			DefaultZoneID:     "zone-default",
			DefaultUserQuota:  1,
		},
	}
}

// seedPermissionEmailTestUser inserts one local user used by the email-routing tests.
func seedPermissionEmailTestUser(t *testing.T, ctx context.Context, store *sqlite.Store, username string) model.User {
	t.Helper()

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  1,
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

// seedPermissionEmailManagedDomain inserts the default root domain required by
// resolveDefaultEmailRouteSpec.
func seedPermissionEmailManagedDomain(t *testing.T, ctx context.Context, store *sqlite.Store) {
	t.Helper()

	if _, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:       "linuxdo.space",
		CloudflareZoneID: "zone-default",
		DefaultQuota:     1,
		AutoProvision:    true,
		IsDefault:        true,
		Enabled:          true,
	}); err != nil {
		t.Fatalf("seed managed domain: %v", err)
	}
}
