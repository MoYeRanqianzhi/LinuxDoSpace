package config

import (
	"net/netip"
	"strings"
	"testing"
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
			expectedFragment: "APP_ADMIN_PASSWORD is required",
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
	if !strings.Contains(err.Error(), "APP_ADMIN_USERNAMES and APP_ADMIN_PASSWORD are required") {
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
// can be enabled once the required SMTP listener, envelope sender, and optional
// upstream relay settings are provided explicitly.
func TestLoadAcceptsDatabaseRelayConfiguration(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_HOST", "smtp.example.com:587")
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
	if cfg.Mail.ForwardHost != "smtp.example.com:587" {
		t.Fatalf("expected forward host to survive load, got %q", cfg.Mail.ForwardHost)
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
}

// TestLoadAcceptsDatabaseRelayDirectMXFallback verifies the built-in relay can
// start without MAIL_RELAY_FORWARD_HOST and will rely on direct MX delivery.
func TestLoadAcceptsDatabaseRelayDirectMXFallback(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_HOST", "")
	t.Setenv("MAIL_RELAY_FORWARD_USERNAME", "")
	t.Setenv("MAIL_RELAY_FORWARD_PASSWORD", "")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@mail.linuxdo.space")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("load config for database mail relay direct mx fallback: %v", err)
	}
	if cfg.Mail.ForwardHost != "" {
		t.Fatalf("expected direct mx fallback to keep empty forward host, got %q", cfg.Mail.ForwardHost)
	}
	if cfg.Mail.ForwardFrom != "relay@mail.linuxdo.space" {
		t.Fatalf("expected configured envelope sender to survive load, got %q", cfg.Mail.ForwardFrom)
	}
}

// TestLoadRejectsRelayAuthWithoutHost ensures operator typos cannot configure
// SMTP authentication credentials without also specifying the upstream host.
func TestLoadRejectsRelayAuthWithoutHost(t *testing.T) {
	t.Setenv("APP_SESSION_SECRET", "test-session-secret")
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ADMIN_USERNAMES", "")
	t.Setenv("APP_ADMIN_PASSWORD", "")
	t.Setenv("EMAIL_FORWARDING_BACKEND", EmailForwardingBackendDatabaseRelay)
	t.Setenv("MAIL_RELAY_ENABLED", "true")
	t.Setenv("MAIL_RELAY_SMTP_ADDR", ":2525")
	t.Setenv("MAIL_RELAY_DOMAIN", "mail.linuxdo.space")
	t.Setenv("MAIL_RELAY_FORWARD_HOST", "")
	t.Setenv("MAIL_RELAY_FORWARD_USERNAME", "user")
	t.Setenv("MAIL_RELAY_FORWARD_PASSWORD", "pass")
	t.Setenv("MAIL_RELAY_FORWARD_FROM", "")

	_, err := Load()
	if err == nil {
		t.Fatalf("expected incomplete database relay configuration to fail")
	}
	if !strings.Contains(err.Error(), "MAIL_RELAY_FORWARD_FROM is required") {
		t.Fatalf("unexpected error for missing forward from: %v", err)
	}

	t.Setenv("MAIL_RELAY_FORWARD_FROM", "relay@mail.linuxdo.space")
	_, err = Load()
	if err == nil {
		t.Fatalf("expected auth without forward host to fail")
	}
	if !strings.Contains(err.Error(), "MAIL_RELAY_FORWARD_HOST is required when MAIL_RELAY_FORWARD_USERNAME or MAIL_RELAY_FORWARD_PASSWORD is set") {
		t.Fatalf("unexpected error: %v", err)
	}
}
