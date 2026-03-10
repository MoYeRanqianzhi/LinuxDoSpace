package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
)

// emailRoutingRollbackTimeout gives the best-effort rollback a fresh timeout so
// the cleanup still has a chance to run after the original request context has
// already expired while persisting the local database mutation.
const emailRoutingRollbackTimeout = 20 * time.Second

// emailRoutingProvisioner centralizes the Cloudflare Email Routing orchestration
// shared by both the public user flows and the administrator console.
type emailRoutingProvisioner struct {
	cfg config.Config
	cf  CloudflareClient
}

// emailRouteSyncState captures the Cloudflare-facing state of one mailbox route
// so service methods can apply a change and roll it back if the database write
// that follows does not succeed.
type emailRouteSyncState struct {
	RootDomain  string
	Prefix      string
	TargetEmail string
	Enabled     bool
	Exists      bool
}

// newEmailRoutingProvisioner builds the shared Cloudflare Email Routing helper.
func newEmailRoutingProvisioner(cfg config.Config, cf CloudflareClient) emailRoutingProvisioner {
	return emailRoutingProvisioner{cfg: cfg, cf: cf}
}

// newForwardingEmailRouteSyncState describes one exact mailbox that should
// exist in Cloudflare and forward to the provided target inbox.
func newForwardingEmailRouteSyncState(rootDomain string, prefix string, targetEmail string, enabled bool) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain:  strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:      strings.ToLower(strings.TrimSpace(prefix)),
		TargetEmail: strings.ToLower(strings.TrimSpace(targetEmail)),
		Enabled:     enabled,
		Exists:      true,
	}
}

// newDeletedEmailRouteSyncState describes one exact mailbox that should not
// have a Cloudflare Email Routing rule after the current mutation finishes.
func newDeletedEmailRouteSyncState(rootDomain string, prefix string) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain: strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:     strings.ToLower(strings.TrimSpace(prefix)),
		Exists:     false,
	}
}

// newCatchAllEmailRouteSyncState keeps the historical "catch-all permission"
// terminology while mapping the feature to its real Cloudflare representation:
// one exact mailbox named `catch-all@<namespace>`.
func newCatchAllEmailRouteSyncState(rootDomain string, targetEmail string, enabled bool) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain:  strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:      emailCatchAllPrefix,
		TargetEmail: strings.ToLower(strings.TrimSpace(targetEmail)),
		Enabled:     enabled,
		Exists:      true,
	}
}

// newDeletedCatchAllEmailRouteSyncState describes the absent state for the same
// dedicated `catch-all@<namespace>` mailbox.
func newDeletedCatchAllEmailRouteSyncState(rootDomain string) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain: strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:     emailCatchAllPrefix,
		Exists:     false,
	}
}

// Address returns the visible mailbox or namespace represented by this
// synchronization snapshot. Missing states still keep the address so delete and
// rollback logs are easy to understand.
func (s emailRouteSyncState) Address() string {
	return buildEmailRouteAddress(s.Prefix, s.RootDomain)
}

// SyncForwardingState applies the desired Cloudflare change first and then runs
// the caller's database mutation. If the database write fails, the helper makes
// one best-effort attempt to restore Cloudflare back to the previous state so
// local storage and the external provider do not silently diverge.
func (p emailRoutingProvisioner) SyncForwardingState(ctx context.Context, before emailRouteSyncState, after emailRouteSyncState, persist func() error) error {
	if err := p.applyForwardingState(ctx, after); err != nil {
		return err
	}

	if err := persist(); err != nil {
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), emailRoutingRollbackTimeout)
		defer cancel()

		if rollbackErr := p.applyForwardingState(rollbackCtx, before); rollbackErr != nil {
			return InternalError(
				"failed to persist email route after syncing cloudflare email routing; rollback also failed",
				fmt.Errorf("persist database change: %w; rollback cloudflare route %s: %v", err, before.Address(), rollbackErr),
			)
		}
		return err
	}

	return nil
}

// applyForwardingState translates one internal route snapshot into the exact
// Cloudflare API operation required to reach that state.
func (p emailRoutingProvisioner) applyForwardingState(ctx context.Context, state emailRouteSyncState) error {
	if !state.Exists {
		return p.DeleteForwardingRule(ctx, state.RootDomain, state.Prefix)
	}
	return p.EnsureForwardingRule(ctx, state.RootDomain, state.Prefix, state.TargetEmail, state.Enabled)
}

// EnsureForwardingRule ensures that the exact mailbox address forwards to the
// desired verified destination with the requested enabled state.
func (p emailRoutingProvisioner) EnsureForwardingRule(ctx context.Context, rootDomain string, prefix string, targetEmail string, enabled bool) error {
	if err := p.ensureConfigured(); err != nil {
		return err
	}

	zoneID, err := p.resolveZoneID(ctx, rootDomain)
	if err != nil {
		return err
	}
	accountID, err := p.resolveAccountID(ctx, zoneID)
	if err != nil {
		return err
	}

	normalizedTarget := strings.ToLower(strings.TrimSpace(targetEmail))
	if err := p.ensureVerifiedDestinationAddress(ctx, accountID, normalizedTarget); err != nil {
		return err
	}

	zoneRoot, err := p.resolveZoneRootDomain(ctx, zoneID, rootDomain)
	if err != nil {
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(rootDomain), zoneRoot) {
		if err := p.ensureEmailRoutingDNS(ctx, zoneID, rootDomain, zoneRoot); err != nil {
			return err
		}
	}

	address := buildEmailRouteAddress(prefix, rootDomain)
	rules, err := p.cf.ListEmailRoutingRules(ctx, zoneID)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to list cloudflare email routing rules", err)
	}

	payload := cloudflare.UpsertEmailRoutingRuleInput{
		Name:    address,
		Enabled: enabled,
		Matchers: []cloudflare.EmailRoutingMatcher{{
			Type:  "literal",
			Field: "to",
			Value: address,
		}},
		Actions: []cloudflare.EmailRoutingAction{{
			Type:  "forward",
			Value: []string{normalizedTarget},
		}},
	}

	if existingRule, found := findEmailRoutingRuleByAddress(rules, address); found {
		ruleIdentifier := existingRule.Identifier()
		if ruleIdentifier == "" {
			return wrapEmailRoutingUnavailable("failed to identify cloudflare email routing rule", fmt.Errorf("rule for %s did not expose an id or tag", address))
		}
		if _, err := p.cf.UpdateEmailRoutingRule(ctx, zoneID, ruleIdentifier, payload); err != nil {
			return wrapEmailRoutingUnavailable("failed to update cloudflare email routing rule", err)
		}
		return nil
	}

	if _, err := p.cf.CreateEmailRoutingRule(ctx, zoneID, payload); err != nil {
		return wrapEmailRoutingUnavailable("failed to create cloudflare email routing rule", err)
	}
	return nil
}

// DeleteForwardingRule removes the Cloudflare Email Routing rule for one exact
// mailbox address when the application deletes the corresponding local route.
func (p emailRoutingProvisioner) DeleteForwardingRule(ctx context.Context, rootDomain string, prefix string) error {
	if err := p.ensureConfigured(); err != nil {
		return err
	}

	zoneID, err := p.resolveZoneID(ctx, rootDomain)
	if err != nil {
		return err
	}

	address := buildEmailRouteAddress(prefix, rootDomain)
	rules, err := p.cf.ListEmailRoutingRules(ctx, zoneID)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to list cloudflare email routing rules", err)
	}

	existingRule, found := findEmailRoutingRuleByAddress(rules, address)
	if !found {
		return nil
	}

	ruleIdentifier := existingRule.Identifier()
	if ruleIdentifier == "" {
		return wrapEmailRoutingUnavailable("failed to identify cloudflare email routing rule", fmt.Errorf("rule for %s did not expose an id or tag", address))
	}
	if err := p.cf.DeleteEmailRoutingRule(ctx, zoneID, ruleIdentifier); err != nil {
		return wrapEmailRoutingUnavailable("failed to delete cloudflare email routing rule", err)
	}
	return nil
}

// ensureConfigured keeps email-routing mutations fail-closed unless Cloudflare
// integration is explicitly configured.
func (p emailRoutingProvisioner) ensureConfigured() error {
	if p.cf == nil || !p.cfg.CloudflareConfigured() {
		return UnavailableError("cloudflare email routing integration is not configured", nil)
	}
	return nil
}

// resolveZoneID maps one routed root domain to the authoritative Cloudflare
// zone. Subdomains under the configured default root reuse the parent zone.
func (p emailRoutingProvisioner) resolveZoneID(ctx context.Context, rootDomain string) (string, error) {
	normalizedRoot := strings.ToLower(strings.TrimSpace(rootDomain))
	defaultRoot := strings.ToLower(strings.TrimSpace(p.cfg.Cloudflare.DefaultRootDomain))

	if defaultRoot != "" && (normalizedRoot == defaultRoot || strings.HasSuffix(normalizedRoot, "."+defaultRoot)) {
		if strings.TrimSpace(p.cfg.Cloudflare.DefaultZoneID) != "" {
			return strings.TrimSpace(p.cfg.Cloudflare.DefaultZoneID), nil
		}

		zone, err := p.cf.ResolveZone(ctx, defaultRoot)
		if err != nil {
			return "", UnavailableError("failed to resolve default cloudflare zone for email routing", err)
		}
		return zone.ID, nil
	}

	zone, err := p.cf.ResolveZone(ctx, normalizedRoot)
	if err != nil {
		return "", UnavailableError("failed to resolve cloudflare zone for email routing", err)
	}
	return zone.ID, nil
}

// resolveZoneRootDomain derives the canonical zone root name for one routed
// domain so the service can calculate subdomain-specific Email Routing settings.
func (p emailRoutingProvisioner) resolveZoneRootDomain(ctx context.Context, zoneID string, routedRoot string) (string, error) {
	normalizedRoot := normalizeDNSName(routedRoot)
	defaultRoot := normalizeDNSName(p.cfg.Cloudflare.DefaultRootDomain)
	if defaultRoot != "" && (normalizedRoot == defaultRoot || strings.HasSuffix(normalizedRoot, "."+defaultRoot)) {
		return defaultRoot, nil
	}

	zone, err := p.cf.GetZone(ctx, zoneID)
	if err != nil {
		return "", UnavailableError("failed to resolve cloudflare zone root for email routing", err)
	}

	zoneRoot := normalizeDNSName(zone.Name)
	if zoneRoot == "" {
		return "", UnavailableError("cloudflare zone root is missing for email routing", fmt.Errorf("zone %s returned an empty name", zoneID))
	}
	return zoneRoot, nil
}

// resolveAccountID returns the configured Cloudflare account ID when available
// and otherwise derives it from the authoritative zone.
func (p emailRoutingProvisioner) resolveAccountID(ctx context.Context, zoneID string) (string, error) {
	if strings.TrimSpace(p.cfg.Cloudflare.AccountID) != "" {
		return strings.TrimSpace(p.cfg.Cloudflare.AccountID), nil
	}

	zone, err := p.cf.GetZone(ctx, zoneID)
	if err != nil {
		return "", UnavailableError("failed to resolve cloudflare account for email routing", err)
	}
	if strings.TrimSpace(zone.Account.ID) == "" {
		return "", UnavailableError("cloudflare account id is missing for email routing", fmt.Errorf("zone %s returned an empty account id", zoneID))
	}
	return strings.TrimSpace(zone.Account.ID), nil
}

// ensureVerifiedDestinationAddress verifies that the requested target inbox is a
// Cloudflare Email Routing destination that has already completed email
// verification. When the destination is new, Cloudflare sends the verification
// email immediately and LinuxDoSpace asks the user to retry after confirming it.
func (p emailRoutingProvisioner) ensureVerifiedDestinationAddress(ctx context.Context, accountID string, targetEmail string) error {
	addresses, err := p.cf.ListEmailRoutingDestinationAddresses(ctx, accountID)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to list cloudflare email routing destination addresses", err)
	}

	for _, item := range addresses {
		if !strings.EqualFold(strings.TrimSpace(item.Email), targetEmail) {
			continue
		}
		if item.Verified != nil {
			return nil
		}
		return ConflictError(fmt.Sprintf("cloudflare email routing destination %s is not verified yet; complete the verification email and retry", targetEmail))
	}

	created, err := p.cf.CreateEmailRoutingDestinationAddress(ctx, accountID, targetEmail)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to create cloudflare email routing destination address", err)
	}
	if created.Verified != nil {
		return nil
	}
	return ConflictError(fmt.Sprintf("cloudflare sent a destination verification email to %s; verify it and retry saving the forwarding rule", targetEmail))
}

// ensureEmailRoutingDNS makes the required Email Routing DNS records exist for
// one routed namespace before Cloudflare is asked to accept a subdomain route.
func (p emailRoutingProvisioner) ensureEmailRoutingDNS(ctx context.Context, zoneID string, routedRoot string, zoneRoot string) error {
	if _, err := p.cf.EnableEmailRoutingDNS(ctx, zoneID); err != nil {
		return wrapEmailRoutingUnavailable("failed to enable cloudflare email routing dns", err)
	}

	subdomain, err := cloudflareEmailRoutingScopedDomain(routedRoot, zoneRoot)
	if err != nil {
		return err
	}

	requiredRecords, err := p.cf.ListEmailRoutingDNSRecords(ctx, zoneID, subdomain)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to load required cloudflare email routing dns records", err)
	}
	if len(requiredRecords) == 0 {
		return nil
	}

	existingRecords, err := p.cf.ListAllDNSRecords(ctx, zoneID)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to list cloudflare dns records while ensuring email routing", err)
	}

	for _, required := range requiredRecords {
		if err := p.upsertEmailRoutingDNSRecord(ctx, zoneID, &existingRecords, required); err != nil {
			return err
		}
	}
	return nil
}

// upsertEmailRoutingDNSRecord creates or updates one Cloudflare DNS record so
// the routed namespace matches the Email Routing requirements returned by the
// Cloudflare API.
func (p emailRoutingProvisioner) upsertEmailRoutingDNSRecord(ctx context.Context, zoneID string, existingRecords *[]cloudflare.DNSRecord, required cloudflare.EmailRoutingDNSRecord) error {
	desiredInput := cloudflare.CreateDNSRecordInput{
		Type:     strings.ToUpper(strings.TrimSpace(required.Type)),
		Name:     normalizeDNSName(required.Name),
		Content:  strings.TrimSpace(required.Content),
		TTL:      required.TTL,
		Proxied:  required.Proxied,
		Priority: required.Priority,
	}
	if desiredInput.Type == "" || desiredInput.Name == "" || strings.TrimSpace(desiredInput.Content) == "" {
		return UnavailableError("cloudflare returned an incomplete email routing dns record", fmt.Errorf("type=%q name=%q content=%q", required.Type, required.Name, required.Content))
	}
	if desiredInput.TTL <= 0 {
		desiredInput.TTL = 1
	}

	if index, found := findEquivalentEmailRoutingDNSRecord(*existingRecords, desiredInput); found {
		// Keep the in-memory slice stable so repeated requirements do not trigger
		// duplicate writes during one save operation.
		(*existingRecords)[index] = normalizeDNSRecordSnapshot((*existingRecords)[index], desiredInput)
		return nil
	}

	if index, found := findUpdatableEmailRoutingDNSRecord(*existingRecords, desiredInput); found {
		updatedRecord, err := p.cf.UpdateDNSRecord(ctx, zoneID, (*existingRecords)[index].ID, cloudflare.UpdateDNSRecordInput(desiredInput))
		if err != nil {
			return wrapEmailRoutingUnavailable("failed to update cloudflare dns record required for email routing", err)
		}
		(*existingRecords)[index] = updatedRecord
		return nil
	}

	createdRecord, err := p.cf.CreateDNSRecord(ctx, zoneID, desiredInput)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to create cloudflare dns record required for email routing", err)
	}
	*existingRecords = append(*existingRecords, createdRecord)
	return nil
}

// wrapEmailRoutingUnavailable expands Cloudflare's generic authentication
// errors into a clearer operator hint about the required Cloudflare token
// permissions for Email Routing and its supporting DNS writes.
func wrapEmailRoutingUnavailable(message string, err error) error {
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(normalized, "authentication error") {
		return UnavailableError(message+"; the Cloudflare API token must include DNS read/write, Email Routing Addresses read/write, Email Routing Rules read/write, and Zone read permissions", err)
	}
	return UnavailableError(message, err)
}

// findEmailRoutingRuleByAddress matches one Cloudflare Email Routing rule to an
// exact recipient address by inspecting its literal `to` matchers.
func findEmailRoutingRuleByAddress(rules []cloudflare.EmailRoutingRule, address string) (cloudflare.EmailRoutingRule, bool) {
	normalizedAddress := strings.ToLower(strings.TrimSpace(address))
	for _, item := range rules {
		for _, matcher := range item.Matchers {
			if !strings.EqualFold(strings.TrimSpace(matcher.Type), "literal") {
				continue
			}
			if !strings.EqualFold(strings.TrimSpace(matcher.Field), "to") {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(matcher.Value), normalizedAddress) {
				return item, true
			}
		}
	}
	return cloudflare.EmailRoutingRule{}, false
}

// findEquivalentEmailRoutingDNSRecord detects whether the desired Email Routing
// DNS record already exists with the same effective configuration.
func findEquivalentEmailRoutingDNSRecord(records []cloudflare.DNSRecord, desired cloudflare.CreateDNSRecordInput) (int, bool) {
	for index, item := range records {
		if !dnsRecordMatchesIdentity(item, desired) {
			continue
		}
		if dnsRecordMatchesContent(item, desired) {
			return index, true
		}
	}
	return 0, false
}

// findUpdatableEmailRoutingDNSRecord locates an existing DNS record that refers
// to the same logical Email Routing requirement and can therefore be replaced.
func findUpdatableEmailRoutingDNSRecord(records []cloudflare.DNSRecord, desired cloudflare.CreateDNSRecordInput) (int, bool) {
	for index, item := range records {
		if dnsRecordMatchesIdentity(item, desired) {
			return index, true
		}
	}
	return 0, false
}

// dnsRecordMatchesIdentity compares the stable identity fields shared by one
// existing DNS record and one desired Email Routing DNS record.
func dnsRecordMatchesIdentity(existing cloudflare.DNSRecord, desired cloudflare.CreateDNSRecordInput) bool {
	if !strings.EqualFold(strings.TrimSpace(existing.Type), strings.TrimSpace(desired.Type)) {
		return false
	}
	if normalizeDNSName(existing.Name) != normalizeDNSName(desired.Name) {
		return false
	}
	return equalRecordPriority(existing.Priority, desired.Priority)
}

// dnsRecordMatchesContent compares the mutable record payload fields so the
// service can skip a write when Cloudflare already has the right value.
func dnsRecordMatchesContent(existing cloudflare.DNSRecord, desired cloudflare.CreateDNSRecordInput) bool {
	if normalizeDNSRecordContent(existing.Type, existing.Content) != normalizeDNSRecordContent(desired.Type, desired.Content) {
		return false
	}
	if existing.Proxied != desired.Proxied {
		return false
	}
	if desired.TTL > 0 && existing.TTL != desired.TTL {
		return false
	}
	return true
}

// normalizeDNSRecordSnapshot patches the in-memory DNS snapshot with the newly
// required Email Routing record shape when the fake or remote API does not
// return a fuller representation than the desired payload.
func normalizeDNSRecordSnapshot(existing cloudflare.DNSRecord, desired cloudflare.CreateDNSRecordInput) cloudflare.DNSRecord {
	existing.Type = strings.ToUpper(strings.TrimSpace(desired.Type))
	existing.Name = normalizeDNSName(desired.Name)
	existing.Content = strings.TrimSpace(desired.Content)
	existing.TTL = desired.TTL
	existing.Proxied = desired.Proxied
	existing.Priority = desired.Priority
	return existing
}

// equalRecordPriority compares optional MX priorities safely.
func equalRecordPriority(left *int, right *int) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return *left == *right
	}
}

// normalizeDNSName makes DNS-name comparisons insensitive to case and trailing
// dots because Cloudflare may emit either form.
func normalizeDNSName(value string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(value)), ".")
}

// normalizeDNSRecordContent keeps TXT values case-sensitive while treating
// domain-like values on MX records as case-insensitive.
func normalizeDNSRecordContent(recordType string, value string) string {
	trimmed := strings.TrimSpace(value)
	if strings.EqualFold(strings.TrimSpace(recordType), "TXT") {
		return trimmed
	}
	return strings.ToLower(trimmed)
}

// cloudflareEmailRoutingScopedDomain converts one routed namespace root into
// the value expected by Cloudflare's Email Routing namespace-scoped APIs. The
// root zone uses an empty query value, while subdomains use the full FQDN such
// as `alice.linuxdo.space`.
func cloudflareEmailRoutingScopedDomain(routedRoot string, zoneRoot string) (string, error) {
	normalizedRoutedRoot := normalizeDNSName(routedRoot)
	normalizedZoneRoot := normalizeDNSName(zoneRoot)
	if normalizedRoutedRoot == normalizedZoneRoot {
		return "", nil
	}

	suffix := "." + normalizedZoneRoot
	if !strings.HasSuffix(normalizedRoutedRoot, suffix) {
		return "", UnavailableError("failed to resolve email routing subdomain", fmt.Errorf("%s does not belong to zone %s", normalizedRoutedRoot, normalizedZoneRoot))
	}
	return normalizedRoutedRoot, nil
}

// buildEmailRouteAddress normalizes the visible mailbox address used by the
// Cloudflare Email Routing matcher.
func buildEmailRouteAddress(prefix string, rootDomain string) string {
	return strings.ToLower(strings.TrimSpace(prefix)) + "@" + strings.ToLower(strings.TrimSpace(rootDomain))
}

// buildCatchAllEmailRouteAddress renders the canonical public address for the
// permission-gated mailbox under one user namespace.
func buildCatchAllEmailRouteAddress(rootDomain string) string {
	return emailCatchAllPrefix + "@" + strings.ToLower(strings.TrimSpace(rootDomain))
}
