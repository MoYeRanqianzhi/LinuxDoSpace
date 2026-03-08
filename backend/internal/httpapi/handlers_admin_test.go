package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
	"linuxdospace/backend/internal/storage/sqlite"
)

// TestHandleAdminVerifyPasswordRateLimitsRepeatedFailures verifies that the
// administrator password endpoint stops accepting unlimited guesses and that
// each incorrect password attempt is still captured in the audit log.
func TestHandleAdminVerifyPasswordRateLimitsRepeatedFailures(t *testing.T) {
	ctx := context.Background()
	store := newAdminPasswordTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  999,
		Username:       "user2996",
		DisplayName:    "User 2996",
		AvatarURL:      "https://example.com/avatar.png",
		TrustLevel:     4,
		IsLinuxDOAdmin: false,
		IsAppAdmin:     true,
	})
	if err != nil {
		t.Fatalf("upsert admin user: %v", err)
	}

	session, err := store.CreateSession(ctx, sqlite.CreateSessionInput{
		ID:        "session-admin-rate-limit",
		UserID:    user.ID,
		CSRFToken: "csrf-admin-rate-limit",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create admin session: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{
			SessionCookieName:    "linuxdospace_session",
			SessionBindUserAgent: false,
			SessionTTL:           time.Hour,
			AdminPassword:        "correct-horse-battery-staple",
		},
	}

	api := &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(5, 15*time.Minute, time.Hour),
	}

	for attempt := 1; attempt <= 5; attempt++ {
		recorder := performAdminPasswordRequest(t, api, session.ID, session.CSRFToken, "wrong-password")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected status 401, got %d with body %s", attempt, recorder.Code, recorder.Body.String())
		}
	}

	blocked := performAdminPasswordRequest(t, api, session.ID, session.CSRFToken, "wrong-password")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected blocked attempt to return 429, got %d with body %s", blocked.Code, blocked.Body.String())
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("expected blocked attempt to include Retry-After header")
	}
	if !strings.Contains(blocked.Body.String(), "too_many_requests") {
		t.Fatalf("expected blocked response body to mention too_many_requests, got %s", blocked.Body.String())
	}

	var failedAuditCount int
	if err := store.DB().QueryRowContext(ctx, `
SELECT COUNT(*)
FROM audit_logs
WHERE action = 'admin.session.verify_password_failed'
`).Scan(&failedAuditCount); err != nil {
		t.Fatalf("count failed audit logs: %v", err)
	}
	if failedAuditCount != 5 {
		t.Fatalf("expected 5 failed password audit logs, got %d", failedAuditCount)
	}
}

// performAdminPasswordRequest sends one JSON password verification request into
// the real handler with the session cookie and CSRF token already attached.
func performAdminPasswordRequest(t *testing.T, api *API, sessionID string, csrfToken string, password string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/admin/verify-password", strings.NewReader(`{"password":"`+password+`"}`))
	request.AddCookie(&http.Cookie{Name: "linuxdospace_session", Value: sessionID})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("CF-Connecting-IP", "203.0.113.42")

	recorder := httptest.NewRecorder()
	api.handleAdminVerifyPassword(recorder, request)
	return recorder
}

// newAdminPasswordTestStore builds a migrated SQLite store for HTTP handler tests.
func newAdminPasswordTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.NewStore(filepath.Join(t.TempDir(), "admin-password-test.sqlite"))
	if err != nil {
		t.Fatalf("new admin password test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close admin password test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate admin password test store: %v", err)
	}

	return store
}
