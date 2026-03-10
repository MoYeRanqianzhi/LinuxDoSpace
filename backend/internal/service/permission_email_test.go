package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage/sqlite"
)

// fakeEmailRoutingCloudflare keeps the tests focused on the permission service
// behavior by emulating only the Email Routing operations exercised here.
type fakeEmailRoutingCloudflare struct {
	zones                      map[string]cloudflare.Zone
	zoneIDsByRoot              map[string]string
	rulesByZone                map[string][]cloudflare.EmailRoutingRule
	requiredDNSByZoneSubdomain map[string]map[string][]cloudflare.EmailRoutingDNSRecord
	dnsRecordsByZone           map[string][]cloudflare.DNSRecord
	addressesByAccount         map[string][]cloudflare.EmailRoutingDestinationAddress
	deletedRule                []string
	createdAddresses           []string
	enabledDNSZones            []string
}

// ResolveZone returns the configured in-memory zone for the requested root.
func (f *fakeEmailRoutingCloudflare) ResolveZone(ctx context.Context, rootDomain string) (cloudflare.Zone, error) {
	normalizedRoot := strings.ToLower(strings.TrimSpace(rootDomain))
	if zoneID, ok := f.zoneIDsByRoot[normalizedRoot]; ok {
		return f.GetZone(ctx, zoneID)
	}
	return cloudflare.Zone{}, fmt.Errorf("zone %q not found", rootDomain)
}

// GetZone returns one configured in-memory zone snapshot.
func (f *fakeEmailRoutingCloudflare) GetZone(ctx context.Context, zoneID string) (cloudflare.Zone, error) {
	if zone, ok := f.zones[strings.TrimSpace(zoneID)]; ok {
		return zone, nil
	}
	if strings.TrimSpace(zoneID) == "zone-default" {
		return cloudflare.Zone{
			ID:   "zone-default",
			Name: "linuxdo.space",
			Account: struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			}{
				ID:   "account-default",
				Name: "Test Account",
			},
		}, nil
	}
	return cloudflare.Zone{}, fmt.Errorf("zone %q not found", zoneID)
}

// ResolveZoneID resolves one configured root domain to its in-memory zone id.
func (f *fakeEmailRoutingCloudflare) ResolveZoneID(ctx context.Context, rootDomain string) (string, error) {
	normalizedRoot := strings.ToLower(strings.TrimSpace(rootDomain))
	if zoneID, ok := f.zoneIDsByRoot[normalizedRoot]; ok {
		return zoneID, nil
	}
	if normalizedRoot == "linuxdo.space" {
		return "zone-default", nil
	}
	return "", fmt.Errorf("zone %q not found", rootDomain)
}

// ListAllDNSRecords returns the in-memory DNS snapshot visible in one zone.
func (f *fakeEmailRoutingCloudflare) ListAllDNSRecords(ctx context.Context, zoneID string) ([]cloudflare.DNSRecord, error) {
	records := f.dnsRecordsByZone[strings.TrimSpace(zoneID)]
	cloned := make([]cloudflare.DNSRecord, len(records))
	copy(cloned, records)
	return cloned, nil
}

// GetDNSRecord returns one in-memory DNS record by id.
func (f *fakeEmailRoutingCloudflare) GetDNSRecord(ctx context.Context, zoneID string, recordID string) (cloudflare.DNSRecord, error) {
	for _, item := range f.dnsRecordsByZone[strings.TrimSpace(zoneID)] {
		if item.ID == strings.TrimSpace(recordID) {
			return item, nil
		}
	}
	return cloudflare.DNSRecord{}, fmt.Errorf("record %q not found", recordID)
}

// CreateDNSRecord appends one new in-memory DNS record.
func (f *fakeEmailRoutingCloudflare) CreateDNSRecord(ctx context.Context, zoneID string, input cloudflare.CreateDNSRecordInput) (cloudflare.DNSRecord, error) {
	record := cloudflare.DNSRecord{
		ID:       fmt.Sprintf("dns-%d", len(f.dnsRecordsByZone[strings.TrimSpace(zoneID)])+1),
		Type:     strings.ToUpper(strings.TrimSpace(input.Type)),
		Name:     strings.ToLower(strings.TrimSpace(input.Name)),
		Content:  strings.TrimSpace(input.Content),
		TTL:      input.TTL,
		Proxied:  input.Proxied,
		Comment:  strings.TrimSpace(input.Comment),
		Priority: input.Priority,
	}
	if f.dnsRecordsByZone == nil {
		f.dnsRecordsByZone = make(map[string][]cloudflare.DNSRecord)
	}
	f.dnsRecordsByZone[strings.TrimSpace(zoneID)] = append(f.dnsRecordsByZone[strings.TrimSpace(zoneID)], record)
	return record, nil
}

// UpdateDNSRecord replaces the stored payload for one existing in-memory DNS record.
func (f *fakeEmailRoutingCloudflare) UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, input cloudflare.UpdateDNSRecordInput) (cloudflare.DNSRecord, error) {
	records := f.dnsRecordsByZone[strings.TrimSpace(zoneID)]
	for index := range records {
		if records[index].ID != strings.TrimSpace(recordID) {
			continue
		}
		records[index].Type = strings.ToUpper(strings.TrimSpace(input.Type))
		records[index].Name = strings.ToLower(strings.TrimSpace(input.Name))
		records[index].Content = strings.TrimSpace(input.Content)
		records[index].TTL = input.TTL
		records[index].Proxied = input.Proxied
		records[index].Comment = strings.TrimSpace(input.Comment)
		records[index].Priority = input.Priority
		f.dnsRecordsByZone[strings.TrimSpace(zoneID)] = records
		return records[index], nil
	}
	return cloudflare.DNSRecord{}, fmt.Errorf("record %q not found", recordID)
}

// DeleteDNSRecord removes one in-memory DNS record.
func (f *fakeEmailRoutingCloudflare) DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error {
	records := f.dnsRecordsByZone[strings.TrimSpace(zoneID)]
	filtered := make([]cloudflare.DNSRecord, 0, len(records))
	for _, item := range records {
		if item.ID == strings.TrimSpace(recordID) {
			continue
		}
		filtered = append(filtered, item)
	}
	f.dnsRecordsByZone[strings.TrimSpace(zoneID)] = filtered
	return nil
}

// ListEmailRoutingDestinationAddresses returns the in-memory destination
// addresses visible under one Cloudflare account.
func (f *fakeEmailRoutingCloudflare) ListEmailRoutingDestinationAddresses(ctx context.Context, accountID string) ([]cloudflare.EmailRoutingDestinationAddress, error) {
	addresses := f.addressesByAccount[accountID]
	cloned := make([]cloudflare.EmailRoutingDestinationAddress, len(addresses))
	copy(cloned, addresses)
	return cloned, nil
}

// CreateEmailRoutingDestinationAddress stores one new in-memory destination
// address so tests can emulate Cloudflare's verification lifecycle.
func (f *fakeEmailRoutingCloudflare) CreateEmailRoutingDestinationAddress(ctx context.Context, accountID string, email string) (cloudflare.EmailRoutingDestinationAddress, error) {
	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	for _, item := range f.addressesByAccount[accountID] {
		if strings.EqualFold(strings.TrimSpace(item.Email), normalizedEmail) {
			return item, nil
		}
	}

	created := cloudflare.EmailRoutingDestinationAddress{
		ID:    fmt.Sprintf("addr-%d", len(f.createdAddresses)+1),
		Email: normalizedEmail,
	}
	if f.addressesByAccount == nil {
		f.addressesByAccount = make(map[string][]cloudflare.EmailRoutingDestinationAddress)
	}
	f.addressesByAccount[accountID] = append(f.addressesByAccount[accountID], created)
	f.createdAddresses = append(f.createdAddresses, normalizedEmail)
	return created, nil
}

// EnableEmailRoutingDNS records that Email Routing DNS bootstrap was requested.
func (f *fakeEmailRoutingCloudflare) EnableEmailRoutingDNS(ctx context.Context, zoneID string) ([]cloudflare.EmailRoutingDNSRecord, error) {
	f.enabledDNSZones = append(f.enabledDNSZones, strings.TrimSpace(zoneID))
	return f.ListEmailRoutingDNSRecords(ctx, zoneID, "")
}

// ListEmailRoutingDNSRecords returns the required DNS records configured for one namespace.
func (f *fakeEmailRoutingCloudflare) ListEmailRoutingDNSRecords(ctx context.Context, zoneID string, subdomain string) ([]cloudflare.EmailRoutingDNSRecord, error) {
	requiredBySubdomain := f.requiredDNSByZoneSubdomain[strings.TrimSpace(zoneID)]
	records := requiredBySubdomain[strings.ToLower(strings.TrimSpace(subdomain))]
	cloned := make([]cloudflare.EmailRoutingDNSRecord, len(records))
	copy(cloned, records)
	return cloned, nil
}

// ListEmailRoutingRules returns the in-memory rules currently visible in one zone.
func (f *fakeEmailRoutingCloudflare) ListEmailRoutingRules(ctx context.Context, zoneID string) ([]cloudflare.EmailRoutingRule, error) {
	rules := f.rulesByZone[zoneID]
	cloned := make([]cloudflare.EmailRoutingRule, len(rules))
	copy(cloned, rules)
	return cloned, nil
}

// CreateEmailRoutingRule appends one new literal in-memory forwarding rule.
func (f *fakeEmailRoutingCloudflare) CreateEmailRoutingRule(ctx context.Context, zoneID string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error) {
	rule := cloudflare.EmailRoutingRule{
		ID:       fmt.Sprintf("rule-%d", len(f.rulesByZone[strings.TrimSpace(zoneID)])+1),
		Name:     input.Name,
		Enabled:  input.Enabled,
		Matchers: cloneRuleMatchers(input.Matchers),
		Actions:  cloneRuleActions(input.Actions),
		Priority: input.Priority,
	}
	if f.rulesByZone == nil {
		f.rulesByZone = make(map[string][]cloudflare.EmailRoutingRule)
	}
	f.rulesByZone[strings.TrimSpace(zoneID)] = append(f.rulesByZone[strings.TrimSpace(zoneID)], rule)
	return rule, nil
}

// UpdateEmailRoutingRule replaces one existing literal in-memory forwarding rule.
func (f *fakeEmailRoutingCloudflare) UpdateEmailRoutingRule(ctx context.Context, zoneID string, ruleIdentifier string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error) {
	zoneKey := strings.TrimSpace(zoneID)
	rules := f.rulesByZone[zoneKey]
	for index := range rules {
		if rules[index].Identifier() != strings.TrimSpace(ruleIdentifier) {
			continue
		}
		rules[index].Name = input.Name
		rules[index].Enabled = input.Enabled
		rules[index].Matchers = cloneRuleMatchers(input.Matchers)
		rules[index].Actions = cloneRuleActions(input.Actions)
		rules[index].Priority = input.Priority
		f.rulesByZone[zoneKey] = rules
		return rules[index], nil
	}
	return f.CreateEmailRoutingRule(ctx, zoneID, input)
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

// TestUpsertMyCatchAllEmailRouteUsesLiteralMailboxAndEnsuresEmailRoutingDNS
// verifies that the permission flow now provisions one exact mailbox
// `catch-all@<namespace>` plus the required Email Routing DNS records for that
// namespace, instead of misusing Cloudflare's zone-level catch-all rule API.
func TestUpsertMyCatchAllEmailRouteUsesLiteralMailboxAndEnsuresEmailRoutingDNS(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 501, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	priorityTen := 10
	cf := newFakeEmailRoutingCloudflare()
	cf.requiredDNSByZoneSubdomain["zone-default"]["alice.linuxdo.space"] = []cloudflare.EmailRoutingDNSRecord{
		{
			Type:     "MX",
			Name:     "alice.linuxdo.space",
			Content:  "route1.mx.cloudflare.net",
			TTL:      1,
			Priority: &priorityTen,
		},
		{
			Type:    "TXT",
			Name:    "alice.linuxdo.space",
			Content: "v=spf1 include:_spf.mx.cloudflare.net ~all",
			TTL:     1,
		},
	}

	service := NewPermissionService(newPermissionEmailTestConfig(), store, cf)
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
		t.Fatalf("save catch-all email route: %v", err)
	}

	if view.Address != "catch-all@alice.linuxdo.space" {
		t.Fatalf("expected canonical catch-all mailbox address, got %q", view.Address)
	}
	if !view.Configured || !view.Enabled {
		t.Fatalf("expected configured enabled catch-all view, got %+v", view)
	}
	if view.TargetEmail != "owner@example.com" {
		t.Fatalf("expected saved target email owner@example.com, got %q", view.TargetEmail)
	}

	if len(cf.enabledDNSZones) == 0 || cf.enabledDNSZones[0] != "zone-default" {
		t.Fatalf("expected email routing dns bootstrap for zone-default, got %v", cf.enabledDNSZones)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if len(zoneDNSRecords) != 2 {
		t.Fatalf("expected two DNS records required for namespace routing, got %d", len(zoneDNSRecords))
	}
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "route1.mx.cloudflare.net") {
		t.Fatalf("expected namespace MX record to be created, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 include:_spf.mx.cloudflare.net ~all") {
		t.Fatalf("expected namespace SPF record to be created, got %+v", zoneDNSRecords)
	}

	zoneRules := cf.rulesByZone["zone-default"]
	if len(zoneRules) != 1 {
		t.Fatalf("expected exactly one literal email routing rule, got %+v", zoneRules)
	}
	catchAllRule := zoneRules[0]
	if !catchAllRule.Enabled {
		t.Fatalf("expected literal catch-all mailbox rule to stay enabled")
	}
	if len(catchAllRule.Matchers) != 1 {
		t.Fatalf("expected one literal matcher, got %+v", catchAllRule.Matchers)
	}
	if catchAllRule.Matchers[0].Type != "literal" || catchAllRule.Matchers[0].Field != "to" || catchAllRule.Matchers[0].Value != "catch-all@alice.linuxdo.space" {
		t.Fatalf("expected literal matcher for catch-all@alice.linuxdo.space, got %+v", catchAllRule.Matchers)
	}
	if targetEmail := extractForwardTargetEmail(catchAllRule); targetEmail != "owner@example.com" {
		t.Fatalf("expected catch-all mailbox forward target owner@example.com, got %q", targetEmail)
	}
	storedRoute, err := store.GetEmailRouteByAddress(ctx, "alice.linuxdo.space", emailCatchAllPrefix)
	if err != nil {
		t.Fatalf("load stored catch-all email route: %v", err)
	}
	if storedRoute.TargetEmail != "owner@example.com" {
		t.Fatalf("expected stored catch-all target owner@example.com, got %q", storedRoute.TargetEmail)
	}
}

// TestParseCatchAllTargetAddressAcceptsLegacyAndCanonical verifies that the
// service now exposes `catch-all@...` canonically while still being able to
// read the earlier broken `*@...` application targets.
func TestParseCatchAllTargetAddressAcceptsLegacyAndCanonical(t *testing.T) {
	testCases := []struct {
		name           string
		target         string
		wantLocalPart  string
		wantRootDomain string
		expectError    bool
	}{
		{
			name:           "canonical dedicated mailbox target",
			target:         "catch-all@alice.linuxdo.space",
			wantLocalPart:  "catch-all",
			wantRootDomain: "alice.linuxdo.space",
		},
		{
			name:           "legacy broken wildcard target",
			target:         "*@alice.linuxdo.space",
			wantLocalPart:  "*",
			wantRootDomain: "alice.linuxdo.space",
		},
		{
			name:        "invalid literal target",
			target:      "alice@linuxdo.space",
			expectError: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			localPart, rootDomain, err := parseCatchAllTargetAddress(testCase.target)
			if testCase.expectError {
				if err == nil {
					t.Fatalf("expected parse error for %q", testCase.target)
				}
				return
			}
			if err != nil {
				t.Fatalf("parse catch-all target: %v", err)
			}
			if localPart != testCase.wantLocalPart {
				t.Fatalf("expected local-part %q, got %q", testCase.wantLocalPart, localPart)
			}
			if rootDomain != testCase.wantRootDomain {
				t.Fatalf("expected root domain %q, got %q", testCase.wantRootDomain, rootDomain)
			}
		})
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

// newFakeEmailRoutingCloudflare initializes the in-memory Cloudflare stub with
// the default account and zone used by the permission email tests.
func newFakeEmailRoutingCloudflare() *fakeEmailRoutingCloudflare {
	return &fakeEmailRoutingCloudflare{
		zones: map[string]cloudflare.Zone{
			"zone-default": {
				ID:   "zone-default",
				Name: "linuxdo.space",
				Account: struct {
					ID   string `json:"id"`
					Name string `json:"name"`
				}{
					ID:   "account-default",
					Name: "Test Account",
				},
			},
		},
		zoneIDsByRoot: map[string]string{
			"linuxdo.space": "zone-default",
		},
		rulesByZone:                make(map[string][]cloudflare.EmailRoutingRule),
		requiredDNSByZoneSubdomain: map[string]map[string][]cloudflare.EmailRoutingDNSRecord{"zone-default": {}},
		dnsRecordsByZone:           make(map[string][]cloudflare.DNSRecord),
		addressesByAccount:         make(map[string][]cloudflare.EmailRoutingDestinationAddress),
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

// seedPermissionEmailAllocation creates the username-matching namespace needed
// for catch-all permission eligibility.
func seedPermissionEmailAllocation(t *testing.T, ctx context.Context, store *sqlite.Store, user model.User, rootDomain string, prefix string) model.Allocation {
	t.Helper()

	managedDomain, err := store.GetManagedDomainByRoot(ctx, rootDomain)
	if err != nil {
		t.Fatalf("load managed domain: %v", err)
	}

	allocation, err := store.CreateAllocation(ctx, sqlite.CreateAllocationInput{
		UserID:           user.ID,
		ManagedDomainID:  managedDomain.ID,
		Prefix:           prefix,
		NormalizedPrefix: prefix,
		FQDN:             prefix + "." + rootDomain,
		IsPrimary:        true,
		Source:           "test",
		Status:           "active",
	})
	if err != nil {
		t.Fatalf("seed allocation: %v", err)
	}
	return allocation
}

// cloneRuleMatchers copies one matcher slice so tests can assert later writes
// without being affected by shared backing arrays.
func cloneRuleMatchers(input []cloudflare.EmailRoutingMatcher) []cloudflare.EmailRoutingRuleMatcher {
	cloned := make([]cloudflare.EmailRoutingRuleMatcher, len(input))
	for index, item := range input {
		cloned[index] = cloudflare.EmailRoutingRuleMatcher{
			Type:  item.Type,
			Field: item.Field,
			Value: item.Value,
		}
	}
	return cloned
}

// cloneRuleActions copies one action slice so in-memory rules remain stable.
func cloneRuleActions(input []cloudflare.EmailRoutingAction) []cloudflare.EmailRoutingRuleAction {
	cloned := make([]cloudflare.EmailRoutingRuleAction, len(input))
	for index, item := range input {
		values := make([]string, len(item.Value))
		copy(values, item.Value)
		cloned[index] = cloudflare.EmailRoutingRuleAction{
			Type:  item.Type,
			Value: values,
		}
	}
	return cloned
}

// hasDNSRecord checks whether one in-memory DNS snapshot already contains the
// expected Cloudflare record content.
func hasDNSRecord(records []cloudflare.DNSRecord, recordType string, name string, content string) bool {
	for _, item := range records {
		if !strings.EqualFold(item.Type, recordType) {
			continue
		}
		if !strings.EqualFold(item.Name, name) {
			continue
		}
		if !strings.EqualFold(item.Content, content) {
			continue
		}
		return true
	}
	return false
}
