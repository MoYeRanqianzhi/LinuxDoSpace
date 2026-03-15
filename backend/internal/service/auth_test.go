package service

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/linuxdo"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/sqlite"

	"golang.org/x/crypto/bcrypt"
)

// staticOAuthClient is a deterministic OAuth stub used to exercise the real
// login completion path against one temporary migrated test store.
type staticOAuthClient struct {
	profile     model.LinuxDOProfile
	exchangeErr error
	userErr     error
}

// Configured reports that the stub is ready to exchange and resolve the fixed profile.
func (c staticOAuthClient) Configured() bool {
	return true
}

// BuildAuthorizationURL is unused by these tests, but it still returns a stable
// value so the stub satisfies the OAuthClient interface completely.
func (c staticOAuthClient) BuildAuthorizationURL(state string, codeChallenge string) string {
	return "https://connect.linux.do/oauth2/authorize?state=" + state
}

// ExchangeCode returns one fixed access token because these tests only care
// about the authorization result stored after callback completion.
func (c staticOAuthClient) ExchangeCode(ctx context.Context, code string, codeVerifier string) (linuxdo.TokenResponse, error) {
	if c.exchangeErr != nil {
		return linuxdo.TokenResponse{}, c.exchangeErr
	}
	return linuxdo.TokenResponse{AccessToken: "test-access-token"}, nil
}

// GetCurrentUser returns the fixed Linux Do profile configured for the test case.
func (c staticOAuthClient) GetCurrentUser(ctx context.Context, accessToken string) (model.LinuxDOProfile, error) {
	if c.userErr != nil {
		return model.LinuxDOProfile{}, c.userErr
	}
	return c.profile, nil
}

// TestCompleteLoginOnlyGrantsAdminToConfiguredUsernames verifies that backend
// administrator access only comes from the local allowlist, even when Linux Do
// reports the user as a forum administrator.
func TestCompleteLoginOnlyGrantsAdminToConfiguredUsernames(t *testing.T) {
	testCases := []struct {
		name               string
		adminUsernames     []string
		profile            model.LinuxDOProfile
		expectedIsAppAdmin bool
	}{
		{
			name:           "allowlisted user keeps admin access",
			adminUsernames: []string{"MoYeRanQianZhi", "user2996"},
			profile: model.LinuxDOProfile{
				ID:             101,
				Username:       "user2996",
				Name:           "User 2996",
				AvatarTemplate: "/user_avatar/linux.do/user2996/{size}/1.png",
				TrustLevel:     4,
				Admin:          false,
			},
			expectedIsAppAdmin: true,
		},
		{
			name:           "linuxdo admin without allowlist is rejected",
			adminUsernames: []string{"MoYeRanQianZhi", "user2996"},
			profile: model.LinuxDOProfile{
				ID:             202,
				Username:       "unexpected-admin",
				Name:           "Unexpected Admin",
				AvatarTemplate: "/user_avatar/linux.do/unexpected-admin/{size}/2.png",
				TrustLevel:     4,
				Admin:          true,
			},
			expectedIsAppAdmin: false,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			ctx := context.Background()
			store := newAuthTestStore(t)

			service := NewAuthService(config.Config{
				App: config.AppConfig{
					SessionTTL:           time.Hour,
					SessionBindUserAgent: true,
					AdminUsernames:       testCase.adminUsernames,
				},
			}, store, staticOAuthClient{profile: testCase.profile})

			stateID := "state-" + testCase.profile.Username
			if err := store.SaveOAuthState(ctx, model.OAuthState{
				ID:        stateID,
				NextPath:  "/admin",
				ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
				CreatedAt: time.Now().UTC(),
			}); err != nil {
				t.Fatalf("save oauth state: %v", err)
			}

			result, err := service.CompleteLogin(ctx, stateID, stateID, "oauth-code", "test-user-agent")
			if err != nil {
				t.Fatalf("complete login: %v", err)
			}

			if result.User.IsLinuxDOAdmin != testCase.profile.Admin {
				t.Fatalf("expected linuxdo admin=%v, got %v", testCase.profile.Admin, result.User.IsLinuxDOAdmin)
			}
			if result.User.IsAppAdmin != testCase.expectedIsAppAdmin {
				t.Fatalf("expected app admin=%v, got %v", testCase.expectedIsAppAdmin, result.User.IsAppAdmin)
			}

			persistedUser, err := store.GetUserByID(ctx, result.User.ID)
			if err != nil {
				t.Fatalf("reload persisted user: %v", err)
			}
			if persistedUser.IsAppAdmin != testCase.expectedIsAppAdmin {
				t.Fatalf("expected persisted app admin=%v, got %v", testCase.expectedIsAppAdmin, persistedUser.IsAppAdmin)
			}
		})
	}
}

// TestCompleteLoginKeepsOAuthStateReusableAfterTransientFailure verifies that
// an upstream Linux Do timeout does not permanently burn the local OAuth state.
func TestCompleteLoginKeepsOAuthStateReusableAfterTransientFailure(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	serviceWithFailure := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminVerificationTTL: 30 * time.Minute,
			AdminUsernames:       []string{"MoYeRanQianZhi", "user2996"},
		},
	}, store, staticOAuthClient{
		profile: model.LinuxDOProfile{
			ID:             303,
			Username:       "user2996",
			Name:           "User 2996",
			AvatarTemplate: "/user_avatar/linux.do/user2996/{size}/1.png",
			TrustLevel:     4,
		},
		exchangeErr: errors.New("linuxdo timeout"),
	})

	stateID := "state-retryable-login"
	if err := store.SaveOAuthState(ctx, model.OAuthState{
		ID:        stateID,
		NextPath:  "/settings",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save oauth state: %v", err)
	}

	if _, err := serviceWithFailure.CompleteLogin(ctx, stateID, stateID, "oauth-code", "test-user-agent"); err == nil {
		t.Fatalf("expected transient oauth exchange failure")
	}

	if _, err := store.GetOAuthState(ctx, stateID); err != nil {
		t.Fatalf("expected oauth state to remain reusable after transient failure, got %v", err)
	}

	serviceWithSuccess := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminVerificationTTL: 30 * time.Minute,
			AdminUsernames:       []string{"MoYeRanQianZhi", "user2996"},
		},
	}, store, staticOAuthClient{
		profile: model.LinuxDOProfile{
			ID:             303,
			Username:       "user2996",
			Name:           "User 2996",
			AvatarTemplate: "/user_avatar/linux.do/user2996/{size}/1.png",
			TrustLevel:     4,
		},
	})

	result, err := serviceWithSuccess.CompleteLogin(ctx, stateID, stateID, "oauth-code", "test-user-agent")
	if err != nil {
		t.Fatalf("retry complete login: %v", err)
	}
	if result.NextPath != "/settings" {
		t.Fatalf("expected next path /settings, got %q", result.NextPath)
	}
	if _, err := store.GetOAuthState(ctx, stateID); !storage.IsNotFound(err) {
		t.Fatalf("expected oauth state to be consumed after successful login, got %v", err)
	}
}

// TestCompleteLoginConsumesOAuthStateOnlyOnce verifies that two concurrent
// callbacks racing with the same state can only create a single server session.
func TestCompleteLoginConsumesOAuthStateOnlyOnce(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	service := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminVerificationTTL: 30 * time.Minute,
			AdminUsernames:       []string{"MoYeRanQianZhi", "user2996"},
		},
	}, store, staticOAuthClient{
		profile: model.LinuxDOProfile{
			ID:             404,
			Username:       "user2996",
			Name:           "User 2996",
			AvatarTemplate: "/user_avatar/linux.do/user2996/{size}/1.png",
			TrustLevel:     4,
		},
	})

	stateID := "state-concurrent-login"
	if err := store.SaveOAuthState(ctx, model.OAuthState{
		ID:        stateID,
		NextPath:  "/settings",
		ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save oauth state: %v", err)
	}

	var successCount atomic.Int32
	var unauthorizedCount atomic.Int32
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := service.CompleteLogin(ctx, stateID, stateID, "oauth-code", "test-user-agent")
			if err == nil {
				successCount.Add(1)
				return
			}

			normalized := NormalizeError(err)
			if normalized.Code == "unauthorized" {
				unauthorizedCount.Add(1)
				return
			}
			t.Errorf("unexpected concurrent login error: %v", err)
		}()
	}
	wg.Wait()

	if successCount.Load() != 1 {
		t.Fatalf("expected exactly one successful login, got %d", successCount.Load())
	}
	if unauthorizedCount.Load() != 1 {
		t.Fatalf("expected the second callback to be rejected once the state is consumed, got %d unauthorized errors", unauthorizedCount.Load())
	}

	var sessionCount int
	if err := store.DB().QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions`).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected exactly one session row, got %d", sessionCount)
	}
}

// TestAuthenticateSessionReevaluatesAdminAllowlist verifies that removing a
// username from runtime configuration immediately revokes admin-only access for
// already-issued sessions without forcing a re-login first.
func TestAuthenticateSessionReevaluatesAdminAllowlist(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  505,
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
		ID:                   "runtime-admin-session",
		UserID:               user.ID,
		CSRFToken:            "csrf-runtime-admin",
		UserAgentFingerprint: "test-user-agent",
		ExpiresAt:            time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	service := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminUsernames:       []string{"someone-else"},
		},
	}, store, nil)

	_, authenticatedUser, err := service.AuthenticateSession(ctx, session.ID, "test-user-agent")
	if err != nil {
		t.Fatalf("authenticate session: %v", err)
	}
	if authenticatedUser.IsAppAdmin {
		t.Fatalf("expected runtime admin allowlist revocation to win over the persisted flag")
	}
}

// TestVerifyAdminPasswordRotatesSession verifies that the admin second-factor
// flow invalidates the pre-verification session instead of upgrading it in
// place.
func TestVerifyAdminPasswordRotatesSession(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  606,
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

	originalSession, err := store.CreateSession(ctx, sqlite.CreateSessionInput{
		ID:                   "pre-verify-session",
		UserID:               user.ID,
		CSRFToken:            "csrf-pre-verify",
		UserAgentFingerprint: "test-user-agent",
		ExpiresAt:            time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create pre-verification session: %v", err)
	}

	service := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminVerificationTTL: 30 * time.Minute,
			AdminUsernames:       []string{"user2996"},
			AdminPassword:        "correct-horse-battery-staple",
		},
	}, store, nil)

	rotatedSession, err := service.VerifyAdminPassword(ctx, originalSession, user, "correct-horse-battery-staple")
	if err != nil {
		t.Fatalf("verify admin password: %v", err)
	}
	if rotatedSession.ID == originalSession.ID {
		t.Fatalf("expected verified admin session id to rotate")
	}
	if rotatedSession.CSRFToken == originalSession.CSRFToken {
		t.Fatalf("expected verified admin csrf token to rotate")
	}
	if rotatedSession.AdminVerifiedAt == nil {
		t.Fatalf("expected rotated session to carry admin verification timestamp")
	}

	if _, _, err := store.GetSessionWithUserByID(ctx, originalSession.ID); !storage.IsNotFound(err) {
		t.Fatalf("expected original session to be retired, got %v", err)
	}
	reloadedSession, _, err := store.GetSessionWithUserByID(ctx, rotatedSession.ID)
	if err != nil {
		t.Fatalf("load rotated session: %v", err)
	}
	if reloadedSession.AdminVerifiedAt == nil {
		t.Fatalf("expected stored rotated session to remain admin-verified")
	}
}

// TestVerifyAdminPasswordUsesPerAdminHash verifies that the service prefers the
// current admin user's dedicated bcrypt hash over the legacy shared password.
func TestVerifyAdminPasswordUsesPerAdminHash(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	user, err := store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  607,
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
		ID:                   "per-admin-hash-session",
		UserID:               user.ID,
		CSRFToken:            "csrf-per-admin-hash",
		UserAgentFingerprint: "test-user-agent",
		ExpiresAt:            time.Now().UTC().Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	adminHash, err := bcrypt.GenerateFromPassword([]byte("dedicated-secret"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("generate admin hash: %v", err)
	}

	service := NewAuthService(config.Config{
		App: config.AppConfig{
			SessionTTL:           time.Hour,
			SessionBindUserAgent: true,
			AdminVerificationTTL: 30 * time.Minute,
			AdminUsernames:       []string{"user2996"},
			AdminPassword:        "legacy-shared-password",
			AdminPasswordHashes: map[string]string{
				"user2996": string(adminHash),
			},
		},
	}, store, nil)

	if _, err := service.VerifyAdminPassword(ctx, session, user, "legacy-shared-password"); err == nil {
		t.Fatalf("expected legacy shared password to be ignored when a per-admin hash exists")
	}

	rotatedSession, err := service.VerifyAdminPassword(ctx, session, user, "dedicated-secret")
	if err != nil {
		t.Fatalf("verify dedicated admin password: %v", err)
	}
	if rotatedSession.AdminVerifiedAt == nil {
		t.Fatalf("expected per-admin hash verification to succeed")
	}
}

// newAuthTestStore creates a temporary migrated store so auth tests can
// exercise the real persistence layer instead of a hand-written mock.
func newAuthTestStore(t *testing.T) *sqlite.Store {
	t.Helper()

	store, err := sqlite.NewStore(filepath.Join(t.TempDir(), "auth-test.sqlite"))
	if err != nil {
		t.Fatalf("new auth test store: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Fatalf("close auth test store: %v", err)
		}
	})

	if err := store.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate auth test store: %v", err)
	}

	return store
}
