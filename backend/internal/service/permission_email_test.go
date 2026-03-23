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
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/sqlite"
)

// stubEmailTargetVerificationMailer keeps target-binding tests deterministic by
// recording outbound verification emails instead of touching the real network.
type stubEmailTargetVerificationMailer struct {
	sent []EmailTargetVerificationMailInput
	err  error
}

// SendVerification appends the requested verification email or returns the
// preconfigured stub error.
func (m *stubEmailTargetVerificationMailer) SendVerification(ctx context.Context, input EmailTargetVerificationMailInput) error {
	if m.err != nil {
		return m.err
	}
	m.sent = append(m.sent, input)
	return nil
}

// fakeEmailRoutingCloudflare keeps the tests focused on the permission service
// behavior by emulating only the Email Routing operations exercised here.
type fakeEmailRoutingCloudflare struct {
	zones                      map[string]cloudflare.Zone
	zoneIDsByRoot              map[string]string
	rulesByZone                map[string][]cloudflare.EmailRoutingRule
	catchAllRuleByZone         map[string]map[string]cloudflare.EmailRoutingRule
	requiredDNSByZoneSubdomain map[string]map[string][]cloudflare.EmailRoutingDNSRecord
	dnsRecordsByZone           map[string][]cloudflare.DNSRecord
	addressesByAccount         map[string][]cloudflare.EmailRoutingDestinationAddress
	deletedAddresses           []string
	deletedRule                []string
	createdAddresses           []string
	enabledDNSZones            []string
	updatedCatchAllSubdomains  []string
	createDNSErrors            map[string]error
	updateDNSErrors            map[string]error
	failNextCreateAddress      error
	createAddressThenFail      error
	deleteAddressErrors        map[string]error
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
	if err := f.lookupDNSMutationError(f.createDNSErrors, input.Type, input.Name); err != nil {
		return cloudflare.DNSRecord{}, err
	}
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
	if err := f.lookupDNSMutationError(f.updateDNSErrors, input.Type, input.Name); err != nil {
		return cloudflare.DNSRecord{}, err
	}
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
	if f.failNextCreateAddress != nil {
		err := f.failNextCreateAddress
		f.failNextCreateAddress = nil
		return cloudflare.EmailRoutingDestinationAddress{}, err
	}

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
	if f.createAddressThenFail != nil {
		err := f.createAddressThenFail
		f.createAddressThenFail = nil
		return cloudflare.EmailRoutingDestinationAddress{}, err
	}
	return created, nil
}

// DeleteEmailRoutingDestinationAddress removes one in-memory destination
// address so tests can emulate a resend-verification flow.
func (f *fakeEmailRoutingCloudflare) DeleteEmailRoutingDestinationAddress(ctx context.Context, accountID string, addressID string) error {
	if err := f.deleteAddressErrors[strings.TrimSpace(addressID)]; err != nil {
		return err
	}

	addresses := f.addressesByAccount[accountID]
	filtered := make([]cloudflare.EmailRoutingDestinationAddress, 0, len(addresses))
	for _, item := range addresses {
		if item.ID == strings.TrimSpace(addressID) {
			f.deletedAddresses = append(f.deletedAddresses, strings.TrimSpace(addressID))
			continue
		}
		filtered = append(filtered, item)
	}
	f.addressesByAccount[accountID] = filtered
	return nil
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

// GetEmailRoutingCatchAllRule returns one stored catch-all rule for the target namespace.
func (f *fakeEmailRoutingCloudflare) GetEmailRoutingCatchAllRule(ctx context.Context, zoneID string, subdomain string) (cloudflare.EmailRoutingRule, error) {
	rulesBySubdomain := f.catchAllRuleByZone[strings.TrimSpace(zoneID)]
	if rulesBySubdomain == nil {
		return cloudflare.EmailRoutingRule{}, nil
	}
	return rulesBySubdomain[strings.ToLower(strings.TrimSpace(subdomain))], nil
}

// UpdateEmailRoutingCatchAllRule stores the catch-all rule under the requested namespace key.
func (f *fakeEmailRoutingCloudflare) UpdateEmailRoutingCatchAllRule(ctx context.Context, zoneID string, subdomain string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error) {
	zoneKey := strings.TrimSpace(zoneID)
	subdomainKey := strings.ToLower(strings.TrimSpace(subdomain))
	if f.catchAllRuleByZone == nil {
		f.catchAllRuleByZone = make(map[string]map[string]cloudflare.EmailRoutingRule)
	}
	if f.catchAllRuleByZone[zoneKey] == nil {
		f.catchAllRuleByZone[zoneKey] = make(map[string]cloudflare.EmailRoutingRule)
	}

	rule := f.catchAllRuleByZone[zoneKey][subdomainKey]
	if strings.TrimSpace(rule.ID) == "" {
		rule.ID = fmt.Sprintf("catch-all-%s-%d", zoneKey, len(f.catchAllRuleByZone[zoneKey])+1)
	}
	rule.Name = input.Name
	rule.Enabled = input.Enabled
	rule.Matchers = cloneRuleMatchers(input.Matchers)
	rule.Actions = cloneRuleActions(input.Actions)
	rule.Priority = input.Priority
	f.catchAllRuleByZone[zoneKey][subdomainKey] = rule
	f.updatedCatchAllSubdomains = append(f.updatedCatchAllSubdomains, subdomainKey)
	return rule, nil
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

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "cloudflare"
	service := NewPermissionService(cfg, store, cf)
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

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "cloudflare"
	service := NewPermissionService(cfg, store, cf)
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
	if !storage.IsNotFound(err) {
		t.Fatalf("expected no persisted email route row, got %v", err)
	}
}

// TestUpsertMyCatchAllEmailRouteUsesCatchAllRuleAndEnsuresEmailRoutingDNS
// preserves the legacy Cloudflare Email Routing behavior behind the explicit
// cloudflare backend flag so operators can still run the old execution mode
// while production defaults move to the database relay.
func TestUpsertMyCatchAllEmailRouteUsesCatchAllRuleAndEnsuresEmailRoutingDNS(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 501, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	priorityTen := 10
	cf := newFakeEmailRoutingCloudflare()
	cf.requiredDNSByZoneSubdomain["zone-default"]["alice-mail.linuxdo.space"] = []cloudflare.EmailRoutingDNSRecord{
		{
			Type:     "MX",
			Name:     "alice-mail.linuxdo.space",
			Content:  "route1.mx.cloudflare.net",
			TTL:      1,
			Priority: &priorityTen,
		},
		{
			Type:    "TXT",
			Name:    "alice-mail.linuxdo.space",
			Content: "v=spf1 include:_spf.mx.cloudflare.net ~all",
			TTL:     1,
		},
	}

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "cloudflare"
	service, mailer := newPermissionServiceWithTestMailer(cfg, store, cf)
	target := createVerifiedPermissionEmailTarget(t, ctx, service, mailer, user, "owner@example.com")
	verifiedAt := time.Now().UTC()
	cf.addressesByAccount["account-default"] = []cloudflare.EmailRoutingDestinationAddress{{
		ID:       "addr-1",
		Email:    target.Email,
		Verified: &verifiedAt,
	}}

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

	if view.Address != "*@alice-mail.linuxdo.space" {
		t.Fatalf("expected canonical catch-all address, got %q", view.Address)
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
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice-mail.linuxdo.space", "route1.mx.cloudflare.net") {
		t.Fatalf("expected namespace MX record to be created, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice-mail.linuxdo.space", "v=spf1 include:_spf.mx.cloudflare.net ~all") {
		t.Fatalf("expected namespace SPF record to be created, got %+v", zoneDNSRecords)
	}

	catchAllRule := cf.catchAllRuleByZone["zone-default"]["alice-mail.linuxdo.space"]
	if !catchAllRule.Enabled {
		t.Fatalf("expected catch-all rule to stay enabled")
	}
	if len(catchAllRule.Matchers) != 1 || catchAllRule.Matchers[0].Type != "all" {
		t.Fatalf("expected catch-all matcher type=all, got %+v", catchAllRule.Matchers)
	}
	if targetEmail := extractForwardTargetEmail(catchAllRule); targetEmail != "owner@example.com" {
		t.Fatalf("expected catch-all forward target owner@example.com, got %q", targetEmail)
	}
	if len(cf.rulesByZone["zone-default"]) != 0 {
		t.Fatalf("expected no literal email routing rule to be created for catch-all, got %+v", cf.rulesByZone["zone-default"])
	}

	storedRoute, err := store.GetEmailRouteByAddress(ctx, "alice-mail.linuxdo.space", emailCatchAllPrefix)
	if err != nil {
		t.Fatalf("load stored catch-all email route: %v", err)
	}
	if storedRoute.TargetEmail != "owner@example.com" {
		t.Fatalf("expected stored catch-all target owner@example.com, got %q", storedRoute.TargetEmail)
	}
}

// TestLoadEmailCatchAllPermissionMigratesLegacyNamespace verifies that older
// catch-all routes stored under `<username>.<root>` are transparently upgraded
// to the dedicated `<username>-mail.<root>` namespace on first load.
func TestLoadEmailCatchAllPermissionMigratesLegacyNamespace(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user := seedPermissionEmailTestUserWithLinuxDOID(t, ctx, store, 1501, "alice")
	seedPermissionEmailManagedDomain(t, ctx, store)
	seedPermissionEmailAllocation(t, ctx, store, user, "linuxdo.space", "alice")

	if _, err := store.UpsertAdminApplication(ctx, storage.UpsertAdminApplicationInput{
		ApplicantUserID: user.ID,
		Type:            PermissionKeyEmailCatchAll,
		Target:          "*@alice.linuxdo.space",
		Reason:          "legacy approved catch-all namespace",
		Status:          "approved",
	}); err != nil {
		t.Fatalf("seed legacy catch-all application: %v", err)
	}
	if _, err := store.CreateEmailRoute(ctx, storage.CreateEmailRouteInput{
		OwnerUserID: user.ID,
		RootDomain:  "alice.linuxdo.space",
		Prefix:      emailCatchAllPrefix,
		TargetEmail: "owner@example.com",
		Enabled:     true,
	}); err != nil {
		t.Fatalf("create legacy catch-all email route: %v", err)
	}

	cf := newFakeEmailRoutingCloudflare()
	priorityTen := 10
	cf.dnsRecordsByZone["zone-default"] = []cloudflare.DNSRecord{
		{
			ID:       "legacy-mx",
			Type:     "MX",
			Name:     "alice.linuxdo.space",
			Content:  "mail.linuxdo.space",
			TTL:      1,
			Comment:  databaseRelayManagedDNSComment,
			Priority: &priorityTen,
		},
		{
			ID:      "legacy-spf",
			Type:    "TXT",
			Name:    "alice.linuxdo.space",
			Content: "v=spf1 -all",
			TTL:     1,
			Comment: databaseRelayManagedDNSComment,
		},
	}

	cfg := newPermissionEmailTestConfig()
	cfg.Mail.ForwardingBackend = "database_relay"
	cfg.Mail.EnsureDNS = true
	cfg.Mail.Domain = "mail.linuxdo.space"
	cfg.Mail.MXTarget = "mail.linuxdo.space"
	cfg.Mail.MXPriority = 10
	cfg.Mail.SPFValue = "v=spf1 -all"
	service := NewPermissionService(cfg, store, cf)

	permission, err := service.loadEmailCatchAllPermission(ctx, user)
	if err != nil {
		t.Fatalf("load catch-all permission after namespace migration: %v", err)
	}
	if permission.Target != "*@alice-mail.linuxdo.space" {
		t.Fatalf("expected canonical migrated target, got %q", permission.Target)
	}

	if _, err := store.GetEmailRouteByAddress(ctx, "alice.linuxdo.space", emailCatchAllPrefix); !storage.IsNotFound(err) {
		t.Fatalf("expected legacy catch-all route to be removed, got %v", err)
	}
	migratedRoute, err := store.GetEmailRouteByAddress(ctx, "alice-mail.linuxdo.space", emailCatchAllPrefix)
	if err != nil {
		t.Fatalf("load migrated catch-all route: %v", err)
	}
	if migratedRoute.TargetEmail != "owner@example.com" || !migratedRoute.Enabled {
		t.Fatalf("unexpected migrated route: %+v", migratedRoute)
	}

	zoneDNSRecords := cf.dnsRecordsByZone["zone-default"]
	if hasDNSRecord(zoneDNSRecords, "MX", "alice.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected legacy relay MX record to be removed, got %+v", zoneDNSRecords)
	}
	if hasDNSRecord(zoneDNSRecords, "TXT", "alice.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected legacy relay SPF record to be removed, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "MX", "alice-mail.linuxdo.space", "mail.linuxdo.space") {
		t.Fatalf("expected migrated relay MX record, got %+v", zoneDNSRecords)
	}
	if !hasDNSRecord(zoneDNSRecords, "TXT", "alice-mail.linuxdo.space", "v=spf1 -all") {
		t.Fatalf("expected migrated relay SPF record, got %+v", zoneDNSRecords)
	}
}

// TestParseCatchAllTargetAddressAcceptsLegacyAndCanonical verifies that the
// service can still read historical `catch-all@...` targets while exposing the
// canonical public `*@...` representation everywhere else.
func TestParseCatchAllTargetAddressAcceptsLegacyAndCanonical(t *testing.T) {
	testCases := []struct {
		name           string
		target         string
		wantLocalPart  string
		wantRootDomain string
		expectError    bool
	}{
		{
			name:           "canonical namespace target",
			target:         "*@alice-mail.linuxdo.space",
			wantLocalPart:  "*",
			wantRootDomain: "alice-mail.linuxdo.space",
		},
		{
			name:           "legacy stored target",
			target:         "catch-all@alice-mail.linuxdo.space",
			wantLocalPart:  "catch-all",
			wantRootDomain: "alice-mail.linuxdo.space",
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
			Name:          "LinuxDoSpace Test",
			BaseURL:       "https://api.example.com",
			FrontendURL:   "https://app.example.com",
			SessionSecret: []byte("permission-email-test-secret"),
		},
		Cloudflare: config.CloudflareConfig{
			APIToken:          "test-token",
			AccountID:         "account-default",
			DefaultRootDomain: "linuxdo.space",
			DefaultZoneID:     "zone-default",
			DefaultUserQuota:  1,
		},
		Mail: config.MailConfig{
			ForwardingBackend: config.EmailForwardingBackendDatabaseRelay,
			Domain:            "mail.linuxdo.space",
			HELODomain:        "mail.linuxdo.space",
			ForwardFrom:       "relay@linuxdo.space",
		},
	}
}

// newPermissionServiceWithTestMailer constructs the permission service together
// with a deterministic verification-mail stub used by the email tests.
func newPermissionServiceWithTestMailer(cfg config.Config, store Store, cf CloudflareClient) (*PermissionService, *stubEmailTargetVerificationMailer) {
	service := NewPermissionService(cfg, store, cf)
	mailer := &stubEmailTargetVerificationMailer{}
	service.targetVerificationMailer = mailer
	return service, mailer
}

// createVerifiedPermissionEmailTarget drives the full local verification flow
// used by the new platform-owned target-binding implementation so tests do not
// manually mutate storage rows behind the service's back.
func createVerifiedPermissionEmailTarget(t *testing.T, ctx context.Context, service *PermissionService, mailer *stubEmailTargetVerificationMailer, user model.User, email string) UserEmailTargetView {
	t.Helper()

	target, err := service.CreateMyEmailTarget(ctx, user, CreateMyEmailTargetRequest{Email: email})
	if err != nil {
		t.Fatalf("create owned email target: %v", err)
	}
	if len(mailer.sent) == 0 {
		t.Fatalf("expected verification email to be sent for %q", email)
	}

	token := mustExtractVerificationToken(t, mailer.sent[len(mailer.sent)-1].VerificationURL)
	if _, err := service.VerifyEmailTarget(ctx, token); err != nil {
		t.Fatalf("verify owned email target %q: %v", email, err)
	}
	return target
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
		catchAllRuleByZone:         make(map[string]map[string]cloudflare.EmailRoutingRule),
		requiredDNSByZoneSubdomain: map[string]map[string][]cloudflare.EmailRoutingDNSRecord{"zone-default": {}},
		dnsRecordsByZone:           make(map[string][]cloudflare.DNSRecord),
		addressesByAccount:         make(map[string][]cloudflare.EmailRoutingDestinationAddress),
		createDNSErrors:            make(map[string]error),
		updateDNSErrors:            make(map[string]error),
	}
}

// lookupDNSMutationError returns one configured fake API failure for the exact
// DNS type/name pair currently being created or updated.
func (f *fakeEmailRoutingCloudflare) lookupDNSMutationError(source map[string]error, recordType string, name string) error {
	if len(source) == 0 {
		return nil
	}
	key := strings.ToUpper(strings.TrimSpace(recordType)) + ":" + strings.ToLower(strings.TrimSpace(name))
	return source[key]
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

// seedPermissionEmailTestUserWithLinuxDOID inserts one local user with a
// caller-controlled Linux Do id so tests can model multiple distinct accounts
// without colliding on the unique upstream identifier.
func seedPermissionEmailTestUserWithLinuxDOID(t *testing.T, ctx context.Context, store *sqlite.Store, linuxDOUserID int64, username string) model.User {
	t.Helper()

	normalizedUsername := strings.TrimSpace(username)
	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  linuxDOUserID,
		Username:       normalizedUsername,
		DisplayName:    normalizedUsername,
		AvatarURL:      "https://example.com/avatar.png",
		TrustLevel:     2,
		IsLinuxDOAdmin: false,
		IsAppAdmin:     strings.EqualFold(normalizedUsername, "admin"),
	})
	if err != nil {
		t.Fatalf("seed test user with linuxdo id: %v", err)
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
