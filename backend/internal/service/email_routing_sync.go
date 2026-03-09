package service

import (
	"context"
	"fmt"
	"strings"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
)

// emailRoutingProvisioner centralizes the Cloudflare Email Routing orchestration
// shared by both the public user flows and the administrator console.
type emailRoutingProvisioner struct {
	cfg config.Config
	cf  CloudflareClient
}

// emailRouteSyncState captures the Cloudflare-facing state of one exact mailbox
// route so service methods can apply a change and roll it back if the database
// write that follows does not succeed.
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

// newForwardingEmailRouteSyncState describes one exact mailbox that should exist
// in Cloudflare and forward to the provided target inbox.
func newForwardingEmailRouteSyncState(rootDomain string, prefix string, targetEmail string, enabled bool) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain:  strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:      strings.ToLower(strings.TrimSpace(prefix)),
		TargetEmail: strings.ToLower(strings.TrimSpace(targetEmail)),
		Enabled:     enabled,
		Exists:      true,
	}
}

// newDeletedEmailRouteSyncState describes one exact mailbox that should not have
// a Cloudflare Email Routing rule after the current mutation finishes.
func newDeletedEmailRouteSyncState(rootDomain string, prefix string) emailRouteSyncState {
	return emailRouteSyncState{
		RootDomain: strings.ToLower(strings.TrimSpace(rootDomain)),
		Prefix:     strings.ToLower(strings.TrimSpace(prefix)),
		Exists:     false,
	}
}

// Address returns the exact mailbox address represented by this synchronization
// snapshot. Missing states still keep the address so delete and rollback logs are
// easy to understand.
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
		if rollbackErr := p.applyForwardingState(ctx, before); rollbackErr != nil {
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

// wrapEmailRoutingUnavailable expands Cloudflare's generic authentication
// errors into a clearer operator hint about missing Email Routing permissions.
func wrapEmailRoutingUnavailable(message string, err error) error {
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(normalized, "authentication error") {
		return UnavailableError(message+"; the Cloudflare API token must include Email Routing Addresses and Email Routing Rules permissions", err)
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

// buildEmailRouteAddress normalizes the visible mailbox address used by the
// Cloudflare Email Routing matcher.
func buildEmailRouteAddress(prefix string, rootDomain string) string {
	return strings.ToLower(strings.TrimSpace(prefix)) + "@" + strings.ToLower(strings.TrimSpace(rootDomain))
}
