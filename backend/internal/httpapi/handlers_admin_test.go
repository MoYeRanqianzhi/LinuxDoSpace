package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
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
			AdminUsernames:       []string{"user2996"},
		},
	}

	api := &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(store, 5, 15*time.Minute, time.Hour),
	}

	for attempt := 1; attempt <= 5; attempt++ {
		recorder := performAdminPasswordRequest(t, api, session.ID, session.CSRFToken, "wrong-password")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected status 401, got %d with body %s", attempt, recorder.Code, recorder.Body.String())
		}
	}

	// Recreate the limiter to prove the lockout now survives process-local state
	// loss and is backed by the shared database.
	api = &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(store, 5, 15*time.Minute, time.Hour),
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

// TestHandleAdminVerifyPasswordRateLimitsByUserIdentity verifies that the
// limiter still blocks brute-force attempts when both the session id and client
// IP change but the same administrator account is targeted.
func TestHandleAdminVerifyPasswordRateLimitsByUserIdentity(t *testing.T) {
	ctx := context.Background()
	store := newAdminPasswordTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  1000,
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

	firstSession, err := store.CreateSession(ctx, sqlite.CreateSessionInput{
		ID:        "session-admin-user-bucket-a",
		UserID:    user.ID,
		CSRFToken: "csrf-admin-user-bucket-a",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create first admin session: %v", err)
	}
	secondSession, err := store.CreateSession(ctx, sqlite.CreateSessionInput{
		ID:        "session-admin-user-bucket-b",
		UserID:    user.ID,
		CSRFToken: "csrf-admin-user-bucket-b",
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create second admin session: %v", err)
	}

	cfg := config.Config{
		App: config.AppConfig{
			SessionCookieName:    "linuxdospace_session",
			SessionBindUserAgent: false,
			SessionTTL:           time.Hour,
			AdminPassword:        "correct-horse-battery-staple",
			AdminUsernames:       []string{"user2996"},
		},
	}

	api := &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(store, 5, 15*time.Minute, time.Hour),
	}

	for attempt := 1; attempt <= 5; attempt++ {
		recorder := performAdminPasswordRequestFromIP(t, api, firstSession.ID, firstSession.CSRFToken, "wrong-password", "203.0.113.42")
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: expected status 401, got %d with body %s", attempt, recorder.Code, recorder.Body.String())
		}
	}

	blocked := performAdminPasswordRequestFromIP(t, api, secondSession.ID, secondSession.CSRFToken, "wrong-password", "198.51.100.73")
	if blocked.Code != http.StatusTooManyRequests {
		t.Fatalf("expected second session and ip to still be blocked by user bucket, got %d with body %s", blocked.Code, blocked.Body.String())
	}
	if blocked.Header().Get("Retry-After") == "" {
		t.Fatalf("expected blocked attempt to include Retry-After header")
	}
}

// TestHandleAdminVerifyPasswordSerializesConcurrentFailures verifies that a
// burst of parallel incorrect guesses for the same administrator account cannot
// all pass the pre-check window before failure counters are persisted.
func TestHandleAdminVerifyPasswordSerializesConcurrentFailures(t *testing.T) {
	ctx := context.Background()
	store := newAdminPasswordTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  1002,
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
		ID:        "session-admin-concurrent-rate-limit",
		UserID:    user.ID,
		CSRFToken: "csrf-admin-concurrent-rate-limit",
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
			AdminUsernames:       []string{"user2996"},
		},
	}

	api := &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(store, 5, 15*time.Minute, time.Hour),
	}

	const concurrentAttempts = 10
	results := make(chan int, concurrentAttempts)
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < concurrentAttempts; attempt++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			recorder := performAdminPasswordRequestFromIP(t, api, session.ID, session.CSRFToken, "wrong-password", "203.0.113.42")
			results <- recorder.Code
		}()
	}
	waitGroup.Wait()
	close(results)

	var unauthorizedCount int
	var blockedCount int
	for code := range results {
		switch code {
		case http.StatusUnauthorized:
			unauthorizedCount++
		case http.StatusTooManyRequests:
			blockedCount++
		default:
			t.Fatalf("expected only 401 or 429 responses, got %d", code)
		}
	}

	if unauthorizedCount != 5 {
		t.Fatalf("expected exactly 5 unauthorized attempts before lockout, got unauthorized=%d blocked=%d", unauthorizedCount, blockedCount)
	}
	if blockedCount != concurrentAttempts-unauthorizedCount {
		t.Fatalf("expected remaining concurrent attempts to be blocked, got unauthorized=%d blocked=%d", unauthorizedCount, blockedCount)
	}
}

// TestHandleAdminVerifyPasswordRotatesSession verifies that a successful admin
// password submission returns a fresh session cookie and CSRF token.
func TestHandleAdminVerifyPasswordRotatesSession(t *testing.T) {
	ctx := context.Background()
	store := newAdminPasswordTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  1001,
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
		ID:                   "session-admin-rotate",
		UserID:               user.ID,
		CSRFToken:            "csrf-admin-rotate",
		UserAgentFingerprint: "test-user-agent",
		ExpiresAt:            time.Now().UTC().Add(time.Hour),
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
			AdminUsernames:       []string{"user2996"},
		},
	}

	api := &API{
		config:               cfg,
		authService:          service.NewAuthService(cfg, store, nil),
		domainService:        service.NewDomainService(cfg, store, nil),
		adminPasswordLimiter: newAdminPasswordLimiter(store, 5, 15*time.Minute, time.Hour),
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/admin/verify-password", strings.NewReader(`{"password":"correct-horse-battery-staple"}`))
	request.AddCookie(&http.Cookie{Name: "linuxdospace_session", Value: session.ID})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	request.Header.Set("User-Agent", "test-user-agent")

	recorder := httptest.NewRecorder()
	api.handleAdminVerifyPassword(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected successful verification to return 200, got %d with body %s", recorder.Code, recorder.Body.String())
	}

	cookies := recorder.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("expected successful verification to rotate the session cookie")
	}
	if cookies[0].Value == session.ID {
		t.Fatalf("expected rotated session cookie value, got original session id")
	}
	if _, _, err := store.GetSessionWithUserByID(ctx, session.ID); !sqlite.IsNotFound(err) {
		t.Fatalf("expected original session to be removed, got %v", err)
	}
}

// TestRequestClientIPIgnoresSpoofedProxyHeaders verifies that the limiter no
// longer trusts client-supplied forwarding headers unless the direct peer is a
// trusted local proxy hop.
func TestRequestClientIPIgnoresSpoofedProxyHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/admin/verify-password", nil)
	request.RemoteAddr = "203.0.113.10:4567"
	request.Header.Set("CF-Connecting-IP", "198.51.100.20")
	request.Header.Set("X-Forwarded-For", "198.51.100.21")

	if got := requestClientIP(request, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}); got != "203.0.113.10" {
		t.Fatalf("expected public remote address to win over spoofed proxy headers, got %q", got)
	}

	request.RemoteAddr = "127.0.0.1:4567"
	if got := requestClientIP(request, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}); got != "198.51.100.20" {
		t.Fatalf("expected trusted local proxy hop to expose CF-Connecting-IP, got %q", got)
	}

	request.Header.Del("CF-Connecting-IP")
	request.Header.Set("X-Forwarded-For", "198.51.100.99, 203.0.113.44")
	if got := requestClientIP(request, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}); got != "203.0.113.44" {
		t.Fatalf("expected trusted proxy parsing to prefer the rightmost forwarded IP, got %q", got)
	}

	request.Header.Set("X-Forwarded-For", "198.51.100.99, 10.0.0.2, 10.0.0.3")
	if got := requestClientIP(request, []netip.Prefix{
		netip.MustParsePrefix("127.0.0.1/32"),
		netip.MustParsePrefix("::1/128"),
		netip.MustParsePrefix("10.0.0.0/8"),
	}); got != "198.51.100.99" {
		t.Fatalf("expected multi-hop trusted proxy parsing to resolve the first untrusted client IP, got %q", got)
	}

	request.Header.Set("CF-Connecting-IP", "not-an-ip")
	request.Header.Set("X-Real-IP", "also-not-an-ip")
	request.Header.Set("X-Forwarded-For", "198.51.100.88")
	if got := requestClientIP(request, []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}); got != "198.51.100.88" {
		t.Fatalf("expected invalid single-value proxy headers to be ignored, got %q", got)
	}
}

// TestWithCORSRestrictsAdminEndpointsToAdminOrigin verifies that the public app
// origin cannot call administrator JSON endpoints through browser CORS.
func TestWithCORSRestrictsAdminEndpointsToAdminOrigin(t *testing.T) {
	handler := withCORS(config.Config{
		App: config.AppConfig{
			AllowedOrigins:   []string{"https://app.example.com", "https://admin.example.com"},
			AdminFrontendURL: "https://admin.example.com",
		},
	}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	publicOriginRequest := httptest.NewRequest(http.MethodOptions, "/v1/admin/me", nil)
	publicOriginRequest.Header.Set("Origin", "https://app.example.com")
	publicOriginRequest.Header.Set("Access-Control-Request-Method", http.MethodGet)
	publicOriginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(publicOriginRecorder, publicOriginRequest)
	if got := publicOriginRecorder.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("expected app origin to be rejected for admin endpoint CORS, got %q", got)
	}

	adminOriginRequest := httptest.NewRequest(http.MethodOptions, "/v1/admin/me", nil)
	adminOriginRequest.Header.Set("Origin", "https://admin.example.com")
	adminOriginRequest.Header.Set("Access-Control-Request-Method", http.MethodGet)
	adminOriginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminOriginRecorder, adminOriginRequest)
	if got := adminOriginRecorder.Header().Get("Access-Control-Allow-Origin"); got != "https://admin.example.com" {
		t.Fatalf("expected admin origin to be allowed for admin endpoint CORS, got %q", got)
	}
}

// TestEnforceCSRFFailsForUnexpectedOrigin verifies that unsafe requests with an
// explicit browser origin must come from one configured frontend origin.
func TestEnforceCSRFFailsForUnexpectedOrigin(t *testing.T) {
	api := &API{
		config: config.Config{
			App: config.AppConfig{
				AllowedOrigins:   []string{"https://app.example.com", "https://admin.example.com"},
				FrontendURL:      "https://app.example.com",
				AdminFrontendURL: "https://admin.example.com",
			},
		},
	}

	session := &model.Session{CSRFToken: "csrf-token"}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	request.Header.Set("Origin", "https://evil.example.com")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)

	recorder := httptest.NewRecorder()
	if api.enforceCSRF(recorder, request, session) {
		t.Fatalf("expected unexpected origin to fail CSRF enforcement")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for unexpected origin, got %d", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "forbidden") {
		t.Fatalf("expected forbidden response body, got %s", recorder.Body.String())
	}
}

// TestEnforceCSRFFailsForCrossSiteFetchWithoutOrigin verifies that Fetch
// Metadata still blocks unsafe browser requests even when Origin/Referer are
// missing.
func TestEnforceCSRFFailsForCrossSiteFetchWithoutOrigin(t *testing.T) {
	api := &API{
		config: config.Config{
			App: config.AppConfig{
				AllowedOrigins:   []string{"https://app.example.com", "https://admin.example.com"},
				FrontendURL:      "https://app.example.com",
				AdminFrontendURL: "https://admin.example.com",
			},
		},
	}

	session := &model.Session{CSRFToken: "csrf-token"}
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	request.Header.Set("Sec-Fetch-Site", "cross-site")
	request.Header.Set("X-CSRF-Token", session.CSRFToken)

	recorder := httptest.NewRecorder()
	if api.enforceCSRF(recorder, request, session) {
		t.Fatalf("expected cross-site fetch metadata to fail CSRF enforcement")
	}
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for cross-site fetch metadata, got %d", recorder.Code)
	}
}

// performAdminPasswordRequest sends one JSON password verification request into
// the real handler with the session cookie and CSRF token already attached.
func performAdminPasswordRequest(t *testing.T, api *API, sessionID string, csrfToken string, password string) *httptest.ResponseRecorder {
	return performAdminPasswordRequestFromIP(t, api, sessionID, csrfToken, password, "203.0.113.42")
}

// performAdminPasswordRequestFromIP sends one JSON password verification
// request with an explicit client IP so limiter bucket combinations can be
// tested deterministically.
func performAdminPasswordRequestFromIP(t *testing.T, api *API, sessionID string, csrfToken string, password string, clientIP string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodPost, "/v1/admin/verify-password", strings.NewReader(`{"password":"`+password+`"}`))
	request.AddCookie(&http.Cookie{Name: "linuxdospace_session", Value: sessionID})
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-CSRF-Token", csrfToken)
	request.Header.Set("CF-Connecting-IP", clientIP)

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
