package config

import (
	"net/netip"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// TestLoadDisablesAdminConsoleByDefault verifies that the backend no longer
// exposes any administrator access unless the deployment explicitly configures
// both the admin allowlist and the second-factor password.
func TestLoadDisablesAdminConsoleByDefault(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config without admin settings: %v", err)
	}
	if len(cfg.App.AdminUsernames) != 0 {
		t.Fatalf("expected no admin usernames by default, got %+v", cfg.App.AdminUsernames)
	}
	if cfg.App.AdminPassword != "" {
		t.Fatalf("expected no admin password by default, got %q", cfg.App.AdminPassword)
	}
}

// TestLoadRejectsPartialAdminConfiguration verifies that deployments cannot
// accidentally enable only half of the required admin protection.
func TestLoadRejectsPartialAdminConfiguration(t *testing.T) {
	testCases := []struct {
		name             string
		adminUsernames   string
		adminPassword    string
		expectedFragment string
	}{
		{
			name:             "missing password",
			adminUsernames:   "MoYeRanQianZhi,user2996",
			adminPassword:    "",
			expectedFragment: "APP_ADMIN_PASSWORD or APP_ADMIN_PASSWORD_HASHES is required",
		},
		{
			name:             "missing usernames",
			adminUsernames:   "",
			adminPassword:    "strong-password",
			expectedFragment: "APP_ADMIN_USERNAMES is required",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Setenv("APP_SESSION_SECRET", "test-session-secret")
			t.Setenv("APP_ENV", "development")
			t.Setenv("APP_ADMIN_USERNAMES", testCase.adminUsernames)
			t.Setenv("APP_ADMIN_PASSWORD", testCase.adminPassword)

			_, err := Load()
			if err == nil {
				t.Fatalf("expected partial admin configuration to fail")
			}
			if !strings.Contains(err.Error(), testCase.expectedFragment) {
				t.Fatalf("expected error to contain %q, got %v", testCase.expectedFragment, err)
			}
		})
	}
}

// TestLoadRequiresExplicitAdminConfigInProduction verifies that production
// deployments fail fast unless the admin protection settings are configured.
func TestLoadRequiresExplicitAdminConfigInProduction(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_SECURE", "true")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected production admin misconfiguration to fail")
	}
	if !strings.Contains(err.Error(), "APP_ADMIN_USERNAMES and either APP_ADMIN_PASSWORD or APP_ADMIN_PASSWORD_HASHES are required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadAcceptsExplicitAdminConfigInProduction verifies that production still
// starts normally once both administrator settings are provided explicitly.
func TestLoadAcceptsExplicitAdminConfigInProduction(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_SECURE", "true")
	t.Setenv("APP_ADMIN_USERNAMES", "MoYeRanQianZhi,user2996")
	t.Setenv("APP_ADMIN_PASSWORD", "strong-password")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config with explicit production admin settings: %v", err)
	}
	if len(cfg.App.AdminUsernames) != 2 {
		t.Fatalf("expected 2 admin usernames, got %+v", cfg.App.AdminUsernames)
	}
	if cfg.App.AdminPassword != "strong-password" {
		t.Fatalf("expected configured admin password to survive load, got %q", cfg.App.AdminPassword)
	}
}

// TestLoadAcceptsPerAdminPasswordHashes verifies that deployments can switch to
// per-admin bcrypt hashes without using the legacy shared plaintext password.
func TestLoadAcceptsPerAdminPasswordHashes(t *testing.T) {
	adminHash, err := bcrypt.GenerateFromPassword([]byte("mo-secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate admin hash: %v", err)
	}
	secondHash, err := bcrypt.GenerateFromPassword([]byte("u2996-secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate second admin hash: %v", err)
	}

	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_SECURE", "true")
	t.Setenv("APP_ADMIN_USERNAMES", "MoYeRanQianZhi,user2996")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("APP_ADMIN_PASSWORD_HASHES", `{"MoYeRanQianZhi":"`+string(adminHash)+`","user2996":"`+string(secondHash)+`"}`)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config with per-admin hashes: %v", err)
	}
	if len(cfg.App.AdminPasswordHashes) != 2 {
		t.Fatalf("expected 2 admin password hashes, got %+v", cfg.App.AdminPasswordHashes)
	}
	if cfg.App.AdminPasswordHashes["moyeranqianzhi"] == "" {
		t.Fatalf("expected normalized lowercase hash entry for MoYeRanQianZhi")
	}
}

// TestLoadRejectsInsecureProductionCookies verifies that production cannot boot
// with non-secure authentication cookies.
func TestLoadRejectsInsecureProductionCookies(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_SESSION_SECURE", "false")
	t.Setenv("APP_ADMIN_USERNAMES", "MoYeRanQianZhi,user2996")
	t.Setenv("APP_ADMIN_PASSWORD", "strong-password")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected insecure production cookie configuration to fail")
	}
	if !strings.Contains(err.Error(), "APP_SESSION_SECURE must be true") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadDefaultsTrustedProxyCIDRs verifies that the backend only trusts
// loopback reverse proxies unless the deployment explicitly expands the list.
func TestLoadDefaultsTrustedProxyCIDRs(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config with default trusted proxies: %v", err)
	}

	expected := []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
	}
	if len(cfg.App.TrustedProxyCIDRs) != len(expected) {
		t.Fatalf("expected %d trusted proxy cidrs, got %+v", len(expected), cfg.App.TrustedProxyCIDRs)
	}
	for index, prefix := range expected {
		if cfg.App.TrustedProxyCIDRs[index] != prefix {
			t.Fatalf("expected trusted proxy prefix %v at index %d, got %v", prefix, index, cfg.App.TrustedProxyCIDRs[index])
		}
	}
}

// TestLoadDefaultsToSQLite ensures existing deployments keep working without
// requiring a new database driver variable during the migration window.
func TestLoadDefaultsToSQLite(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("DATABASE_DRIVER", "")
	t.Setenv("SQLITE_PATH", "./data/test.sqlite")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load default sqlite config: %v", err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("expected default driver sqlite, got %q", cfg.Database.Driver)
	}
	if cfg.Database.SQLitePath != "./data/test.sqlite" {
		t.Fatalf("expected sqlite path to survive load, got %q", cfg.Database.SQLitePath)
	}
}

// TestLoadRequiresPostgresDSN ensures PostgreSQL deployments fail closed unless
// one explicit DSN is configured.
func TestLoadRequiresPostgresDSN(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_POSTGRES_DSN", "")
	t.Setenv("DATABASE_URL", "")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected postgres config without DSN to fail")
	}
	if !strings.Contains(err.Error(), "DATABASE_POSTGRES_DSN or DATABASE_URL is required") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadAcceptsDatabaseURLFallback ensures hosted PostgreSQL environments can
// keep using the conventional DATABASE_URL variable name.
func TestLoadAcceptsDatabaseURLFallback(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("DATABASE_DRIVER", "postgres")
	t.Setenv("DATABASE_POSTGRES_DSN", "")
	t.Setenv("DATABASE_URL", "postgres://linuxdospace:secret@db:5432/linuxdospace?sslmode=disable")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load postgres config from DATABASE_URL: %v", err)
	}
	if cfg.Database.Driver != "postgres" {
		t.Fatalf("expected postgres driver, got %q", cfg.Database.Driver)
	}
	if cfg.Database.PostgresDSN == "" {
		t.Fatalf("expected postgres dsn to be loaded from DATABASE_URL")
	}
}

// TestLoadDefaultsToCloudflareEmailForwarding ensures existing deployments keep
// the current Email Routing execution mode unless they explicitly opt into the
// database-driven SMTP relay.
func TestLoadDefaultsToCloudflareEmailForwarding(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config with default email forwarding backend: %v", err)
	}
	if cfg.Mail.ForwardingBackend != EmailForwardingBackendCloudflare {
		t.Fatalf("expected default email forwarding backend %q, got %q", EmailForwardingBackendCloudflare, cfg.Mail.ForwardingBackend)
	}
	if cfg.UsesDatabaseMailRelay() {
		t.Fatalf("expected database mail relay to be disabled by default")
	}
}

// TestLoadAcceptsDatabaseRelayConfiguration verifies the server-side relay mode
// can be enabled once the required SMTP listener, direct-MX identity, and
// queue settings are provided explicitly.
func TestLoadAcceptsDatabaseRelayConfiguration(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_HELO_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@linuxdo.space")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config for database mail relay: %v", err)
	}
	if !cfg.UsesDatabaseMailRelay() {
		t.Fatalf("expected database mail relay mode to be enabled")
	}
	if !cfg.Mail.RelayEnabled {
		t.Fatalf("expected smtp relay listener to be enabled")
	}
	if cfg.Mail.HELODomain != "mail.linuxdo.space" {
		t.Fatalf("expected helo domain to survive load, got %q", cfg.Mail.HELODomain)
	}
	if !cfg.Mail.EnsureDNS {
		t.Fatalf("expected dns automation to stay enabled by default in database relay mode")
	}
	if cfg.Mail.MXTarget != "mail.linuxdo.space" {
		t.Fatalf("expected default mail relay mx target mail.linuxdo.space, got %q", cfg.Mail.MXTarget)
	}
	if cfg.Mail.SPFValue != "v=spf1 -all" {
		t.Fatalf("expected default relay spf value, got %q", cfg.Mail.SPFValue)
	}
	if cfg.Mail.ResolveTimeout != 5*time.Second {
		t.Fatalf("expected default resolve timeout 5s, got %v", cfg.Mail.ResolveTimeout)
	}
	if cfg.Mail.EnqueueTimeout != 15*time.Second {
		t.Fatalf("expected default enqueue timeout 15s, got %v", cfg.Mail.EnqueueTimeout)
	}
	if cfg.Mail.MaxConcurrentIngress != 32 {
		t.Fatalf("expected default max concurrent ingress 32, got %d", cfg.Mail.MaxConcurrentIngress)
	}
	if cfg.Mail.QueueWorkers != 16 {
		t.Fatalf("expected default queue workers 16, got %d", cfg.Mail.QueueWorkers)
	}
	if cfg.Mail.MaxAttempts != 10 {
		t.Fatalf("expected default max attempts 10, got %d", cfg.Mail.MaxAttempts)
	}
	if cfg.Mail.MXLookupTimeout != 5*time.Second {
		t.Fatalf("expected default mx lookup timeout 5s, got %v", cfg.Mail.MXLookupTimeout)
	}
	if cfg.Mail.MXCacheTTL != 10*time.Minute {
		t.Fatalf("expected default mx cache ttl 10m, got %v", cfg.Mail.MXCacheTTL)
	}
	if cfg.Mail.MaxDomainConcurrency != 8 {
		t.Fatalf("expected default max domain concurrency 8, got %d", cfg.Mail.MaxDomainConcurrency)
	}
}

// TestLoadDefaultsHELODomain verifies that the direct-MX relay keeps a stable
// SMTP greeting hostname even when the operator leaves the explicit variable
// unset in development.
func TestLoadDefaultsHELODomain(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_HELO_DOMAIN", "   ")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@mail.linuxdo.space")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected blank helo domain to fall back to default, got %v", err)
	}
	if cfg.Mail.HELODomain != "mail.linuxdo.space" {
		t.Fatalf("expected helo domain fallback mail.linuxdo.space, got %q", cfg.Mail.HELODomain)
	}
}

// TestLoadRejectsInvalidDomainConcurrency verifies that the direct-MX relay
// fails closed when the per-domain burst cap is not a positive integer.
func TestLoadRejectsInvalidDomainConcurrency(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_HELO_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_MAX_DOMAIN_CONCURRENCY", "0")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected invalid per-domain concurrency to fail")
	}
	if !strings.Contains(err.Error(), "MAIL_RELAY_MAX_DOMAIN_CONCURRENCY must be at least 1") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoadRejectsRetryWindowInversion verifies that the durable mail queue
// cannot boot with a retry cap smaller than the base retry delay.
func TestLoadRejectsRetryWindowInversion(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_RETRY_BASE_DELAY", "30s")
	t.Setenv("MAIL_RELAY_RETRY_MAX_DELAY", "10s")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected inverted retry window to fail validation")
	}
	if !strings.Contains(err.Error(), "MAIL_RELAY_RETRY_MAX_DELAY must be greater than or equal to MAIL_RELAY_RETRY_BASE_DELAY") {
		t.Fatalf("unexpected error: %v", err)
	}
}
