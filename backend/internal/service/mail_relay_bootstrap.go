package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
)

// EnsureDatabaseRelayIngressDNSForRootDomain ensures one routed mail root
// points at the built-in SMTP relay when database-relay mode is active.
func EnsureDatabaseRelayIngressDNSForRootDomain(ctx context.Context, cfg config.Config, cf CloudflareClient, rootDomain string) error {
	return ensureDatabaseRelayIngressDNSForRootDomain(ctx, cfg, cf, rootDomain)
}

// EnsureDatabaseRelayIngressDNSState scans the current database state and keeps
// the relay MX/TXT records aligned with the routed mail roots that are
// actually active right now.
//
// Two production constraints drive this stricter reconciliation:
//  1. Cloudflare free zones have a hard DNS-record quota, so pre-allocating two
//     relay records for every merely approved permission does not scale.
//  2. Disabled or never-configured catch-all permissions should not keep
//     consuming scarce DNS slots once the service moved to database-relay mode.
//
// The startup pass therefore does three things atomically from the operator's
// perspective: collect the currently required namespace roots from active mail
// routes, delete stale LinuxDoSpace-managed relay records that no longer back a
// real route, then backfill the required roots that are still missing.
func EnsureDatabaseRelayIngressDNSState(ctx context.Context, cfg config.Config, store Store, cf CloudflareClient) error {
	if !cfg.UsesDatabaseMailRelay() || !cfg.Mail.EnsureDNS {
		return nil
	}
	if store == nil {
		return fmt.Errorf("database mail relay dns bootstrap store is nil")
	}
	if cf == nil || !cfg.CloudflareConfigured() {
		return fmt.Errorf("database mail relay dns bootstrap requires cloudflare dns access")
	}

	namespaceRoots, err := collectRequiredDatabaseRelayNamespaceRoots(ctx, cfg, store)
	if err != nil {
		return err
	}

	provisioner := newEmailRoutingProvisioner(cfg, cf)
	if err := pruneStaleDatabaseRelayIngressDNS(ctx, cfg, cf, provisioner, namespaceRoots); err != nil {
		return err
	}

	orderedRoots := make([]string, 0, len(namespaceRoots))
	for rootDomain := range namespaceRoots {
		orderedRoots = append(orderedRoots, rootDomain)
	}
	sort.Strings(orderedRoots)

	warnings := make([]string, 0, len(orderedRoots))
	for _, rootDomain := range orderedRoots {
		if err := ensureDatabaseRelayIngressDNSForRootDomain(ctx, cfg, cf, rootDomain); err != nil {
			if warningMessage, ok := classifyDatabaseRelayIngressDNSBootstrapWarning(cfg, rootDomain, err); ok {
				warnings = append(warnings, warningMessage)
				continue
			}
			return fmt.Errorf("ensure database mail relay ingress dns for %s: %w", rootDomain, err)
		}
	}
	if len(warnings) > 0 {
		return &DatabaseRelayIngressDNSBootstrapWarning{Warnings: warnings}
	}
	return nil
}

// DatabaseRelayIngressDNSBootstrapWarning captures one or more known bootstrap
// limitations that should not abort process startup. LinuxDoSpace currently
// uses this only for the default root-domain MX conflict caused by
// Cloudflare-managed Email Routing, because that case blocks automatic DNS
// provisioning but does not make the HTTP/API service itself unsafe to start.
type DatabaseRelayIngressDNSBootstrapWarning struct {
	Warnings []string
}

// Error renders the aggregated warning payload as one log-friendly string.
func (w *DatabaseRelayIngressDNSBootstrapWarning) Error() string {
	if w == nil || len(w.Warnings) == 0 {
		return ""
	}
	return strings.Join(w.Warnings, "; ")
}

// classifyDatabaseRelayIngressDNSBootstrapWarning decides whether one
// bootstrap-time DNS failure is a recognized operator-managed limitation that
// should degrade to a warning instead of killing the backend process.
//
// The rule is intentionally narrow:
//   - only the configured default root domain may soft-fail
//   - only Cloudflare's explicit "Email Routing manages MX here" response is
//     accepted as a warning
//
// Child namespaces still fail closed because their relay MX/TXT rows are
// required for the user-facing catch-all forwarding path to function.
func classifyDatabaseRelayIngressDNSBootstrapWarning(cfg config.Config, rootDomain string, err error) (string, bool) {
	normalizedRoot := normalizeDNSName(rootDomain)
	defaultRoot := normalizeDNSName(cfg.Cloudflare.DefaultRootDomain)
	if normalizedRoot == "" || normalizedRoot != defaultRoot {
		return "", false
	}
	if !isCloudflareEmailRoutingManagedMXConflict(err) {
		return "", false
	}
	return fmt.Sprintf(
		"default mail root %s still uses Cloudflare-managed Email Routing MX records; skipping automatic relay DNS bootstrap for this root and continuing startup",
		normalizedRoot,
	), true
}

// isCloudflareEmailRoutingManagedMXConflict recognizes the exact Cloudflare
// provider response raised when a zone already delegates MX handling to Email
// Routing and therefore rejects direct MX writes through the generic DNS API.
func isCloudflareEmailRoutingManagedMXConflict(err error) bool {
	normalized := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(normalized, "managed by email routing") &&
		strings.Contains(normalized, "disable email routing to add/modify mx records")
}

// collectRequiredDatabaseRelayNamespaceRoots reduces the live email-route table
// to the routed roots that currently need SMTP ingress. The configured default
// root is always included because every logged-in user conceptually owns one
// implicit default mailbox there, even before they save a forwarding target.
func collectRequiredDatabaseRelayNamespaceRoots(ctx context.Context, cfg config.Config, store Store) (map[string]struct{}, error) {
	namespaceRoots := map[string]struct{}{}
	addNamespaceRoot := func(rootDomain string) {
		normalizedRoot := normalizeDNSName(rootDomain)
		if normalizedRoot == "" || !cfg.UsesDatabaseMailRelay() {
			return
		}
		namespaceRoots[normalizedRoot] = struct{}{}
	}

	addNamespaceRoot(cfg.Cloudflare.DefaultRootDomain)

	emailRoutes, err := store.ListEmailRoutes(ctx)
	if err != nil {
		return nil, fmt.Errorf("list email routes for database mail relay dns bootstrap: %w", err)
	}
	for _, item := range emailRoutes {
		if !item.Enabled || !routeHasConfiguredTarget(item) {
			continue
		}
		addNamespaceRoot(item.RootDomain)
	}
	return namespaceRoots, nil
}

// pruneStaleDatabaseRelayIngressDNS removes LinuxDoSpace-managed relay MX/TXT
// records for roots that no longer have any active database-backed mail route.
// The default public email root is intentionally kept because default mailbox
// delivery now also depends on the local SMTP relay.
func pruneStaleDatabaseRelayIngressDNS(ctx context.Context, cfg config.Config, cf CloudflareClient, provisioner emailRoutingProvisioner, requiredRoots map[string]struct{}) error {
	inspectedZones := map[string]struct{}{}
	for rootDomain := range requiredRoots {
		zoneID, err := provisioner.resolveZoneID(ctx, rootDomain)
		if err != nil {
			return fmt.Errorf("resolve zone for database mail relay dns pruning: %w", err)
		}
		inspectedZones[zoneID] = struct{}{}
	}
	if len(inspectedZones) == 0 {
		defaultRoot := normalizeDNSName(cfg.Cloudflare.DefaultRootDomain)
		if defaultRoot != "" {
			zoneID, err := provisioner.resolveZoneID(ctx, defaultRoot)
			if err != nil {
				return fmt.Errorf("resolve default zone for database mail relay dns pruning: %w", err)
			}
			inspectedZones[zoneID] = struct{}{}
		}
	}

	for zoneID := range inspectedZones {
		existingRecords, err := cf.ListAllDNSRecords(ctx, zoneID)
		if err != nil {
			return fmt.Errorf("list cloudflare dns records for database mail relay dns pruning: %w", err)
		}

		for _, item := range existingRecords {
			if !isDatabaseRelayManagedDNSRecord(cfg, item) {
				continue
			}
			normalizedName := normalizeDNSName(item.Name)
			defaultRoot := normalizeDNSName(cfg.Cloudflare.DefaultRootDomain)
			if normalizedName == defaultRoot {
				continue
			}
			if _, stillRequired := requiredRoots[normalizedName]; stillRequired {
				continue
			}
			if err := cf.DeleteDNSRecord(ctx, zoneID, strings.TrimSpace(item.ID)); err != nil {
				return fmt.Errorf("delete stale database mail relay dns record %s %s: %w", item.Type, item.Name, err)
			}
		}
	}
	return nil
}

// isDatabaseRelayManagedDNSRecord narrows the pruning pass to the exact relay
// MX/TXT rows LinuxDoSpace owns, so unrelated user DNS records are never
// touched even when they live on the same namespace root.
func isDatabaseRelayManagedDNSRecord(cfg config.Config, item cloudflare.DNSRecord) bool {
	if !strings.EqualFold(strings.TrimSpace(item.Comment), strings.TrimSpace(databaseRelayManagedDNSComment)) {
		return false
	}
	switch strings.ToUpper(strings.TrimSpace(item.Type)) {
	case "MX", "TXT":
	default:
		return false
	}
	return cfg.UsesDatabaseMailRelay()
}

// ensureDatabaseRelayIngressDNSForRootDomain keeps the service-layer validation
// in one place so both request-time saves and startup backfills apply the same
// normalization rules before writing relay MX/TXT records.
func ensureDatabaseRelayIngressDNSForRootDomain(ctx context.Context, cfg config.Config, cf CloudflareClient, rootDomain string) error {
	if !cfg.UsesDatabaseMailRelay() {
		return nil
	}

	normalizedRoot := normalizeDNSName(rootDomain)
	if normalizedRoot == "" {
		return ValidationError("mail relay root domain is empty")
	}

	return newEmailRoutingProvisioner(cfg, cf).ensureDatabaseRelayIngressDNS(ctx, normalizedRoot)
}

// deleteDatabaseRelayIngressDNSForRootDomain removes LinuxDoSpace-managed relay
// MX/TXT records for one namespace root immediately. This is used when a user
// switches the namespace away from mailbox catch-all mode and needs the root
// freed right away, without waiting for the next startup reconciliation pass.
func deleteDatabaseRelayIngressDNSForRootDomain(ctx context.Context, cfg config.Config, cf CloudflareClient, rootDomain string) error {
	if !cfg.UsesDatabaseMailRelay() || !cfg.Mail.EnsureDNS {
		return nil
	}
	if cf == nil || !cfg.CloudflareConfigured() {
		return UnavailableError("database mail relay dns automation is not configured", nil)
	}

	normalizedRoot := normalizeDNSName(rootDomain)
	if normalizedRoot == "" {
		return nil
	}

	provisioner := newEmailRoutingProvisioner(cfg, cf)
	zoneID, err := provisioner.resolveZoneID(ctx, normalizedRoot)
	if err != nil {
		return err
	}
	records, err := cf.ListAllDNSRecords(ctx, zoneID)
	if err != nil {
		return wrapEmailRoutingUnavailable("failed to list cloudflare dns records while deleting database mail relay ingress", err)
	}

	for _, item := range records {
		if !isDatabaseRelayManagedDNSRecord(cfg, item) {
			continue
		}
		if normalizeDNSName(item.Name) != normalizedRoot {
			continue
		}
		if err := cf.DeleteDNSRecord(ctx, zoneID, strings.TrimSpace(item.ID)); err != nil {
			return wrapEmailRoutingUnavailable("failed to delete cloudflare dns record required for database mail relay cleanup", err)
		}
	}
	return nil
}
