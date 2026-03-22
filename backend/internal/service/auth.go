package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage"
)

// oauthStateLifetime limits how long one OAuth state token remains valid.
const oauthStateLifetime = 10 * time.Minute

// AuthService handles Linux Do OAuth login, server-side sessions, and current-user resolution.
type AuthService struct {
	cfg   config.Config
	store Store
	oauth OAuthClient
}

// LoginStartResult contains the redirect information returned to the HTTP layer.
type LoginStartResult struct {
	StateID     string
	RedirectURL string
}

// LoginCompleteResult contains the session and redirect information produced by a successful callback.
type LoginCompleteResult struct {
	User     model.User
	Session  model.Session
	NextPath string
}

// NewAuthService creates a new authentication service instance.
func NewAuthService(cfg config.Config, store Store, oauth OAuthClient) *AuthService {
	return &AuthService{cfg: cfg, store: store, oauth: oauth}
}

// Configured reports whether OAuth is sufficiently configured to start a login flow.
func (s *AuthService) Configured() bool {
	return s.oauth != nil && s.oauth.Configured()
}

// BeginLogin creates a short-lived OAuth state record and returns the Linux Do authorization URL.
func (s *AuthService) BeginLogin(ctx context.Context, nextPath string) (LoginStartResult, error) {
	if !s.Configured() {
		return LoginStartResult{}, UnavailableError("linux.do oauth is not configured", nil)
	}

	stateID, err := security.RandomToken(32)
	if err != nil {
		return LoginStartResult{}, InternalError("failed to generate oauth state", err)
	}

	codeVerifier := ""
	codeChallenge := ""
	if s.cfg.LinuxDO.EnablePKCE {
		codeVerifier = security.DerivePKCEVerifier(stateID)
		codeChallenge = security.CodeChallengeS256(codeVerifier)
	}

	state := model.OAuthState{
		ID:           stateID,
		CodeVerifier: codeVerifier,
		NextPath:     security.NormalizePathOnly(nextPath),
		ExpiresAt:    time.Now().UTC().Add(oauthStateLifetime),
		CreatedAt:    time.Now().UTC(),
	}
	if err := s.store.SaveOAuthState(ctx, state); err != nil {
		return LoginStartResult{}, InternalError("failed to persist oauth state", err)
	}

	return LoginStartResult{
		StateID:     state.ID,
		RedirectURL: s.oauth.BuildAuthorizationURL(state.ID, codeChallenge),
	}, nil
}

// CompleteLogin exchanges the authorization code, loads the Linux Do profile, and creates a session.
func (s *AuthService) CompleteLogin(ctx context.Context, stateFromQuery string, stateFromCookie string, code string, userAgentFingerprint string) (LoginCompleteResult, error) {
	if !s.Configured() {
		return LoginCompleteResult{}, UnavailableError("linux.do oauth is not configured", nil)
	}
	if strings.TrimSpace(stateFromQuery) == "" || strings.TrimSpace(code) == "" {
		return LoginCompleteResult{}, ValidationError("missing oauth state or code")
	}
	if strings.TrimSpace(stateFromCookie) == "" || stateFromCookie != stateFromQuery {
		return LoginCompleteResult{}, UnauthorizedError("oauth state mismatch")
	}

	state, err := s.store.GetOAuthState(ctx, stateFromQuery)
	if err != nil {
		if storage.IsNotFound(err) {
			return LoginCompleteResult{}, UnauthorizedError("oauth state is invalid or already consumed")
		}
		return LoginCompleteResult{}, InternalError("failed to load oauth state", err)
	}
	if state.ExpiresAt.Before(time.Now().UTC()) {
		_ = s.store.DeleteOAuthState(ctx, stateFromQuery)
		return LoginCompleteResult{}, UnauthorizedError("oauth state has expired")
	}

	codeVerifier := state.CodeVerifier
	if s.cfg.LinuxDO.EnablePKCE {
		codeVerifier = security.DerivePKCEVerifier(stateFromQuery)
	}
	token, err := s.oauth.ExchangeCode(ctx, code, codeVerifier)
	if err != nil {
		return LoginCompleteResult{}, UnavailableError("failed to exchange linux.do oauth code", err)
	}

	profile, err := s.oauth.GetCurrentUser(ctx, token.AccessToken)
	if err != nil {
		return LoginCompleteResult{}, UnavailableError("failed to fetch linux.do user profile", err)
	}

	user, err := s.store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID:  profile.ID,
		Username:       profile.Username,
		DisplayName:    firstNonEmpty(strings.TrimSpace(profile.Name), strings.TrimSpace(profile.Username)),
		AvatarURL:      buildAvatarURL(profile.AvatarTemplate),
		TrustLevel:     profile.TrustLevel,
		IsLinuxDOAdmin: profile.Admin,
		IsAppAdmin:     isAppAdmin(profile.Username, s.cfg.App.AdminUsernames),
	})
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to upsert local user", err)
	}

	control, err := s.store.GetUserControlByUserID(ctx, user.ID)
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to load user moderation state", err)
	}
	if control.IsBanned {
		return LoginCompleteResult{}, ForbiddenError("your account has been banned")
	}

	sessionID, err := security.RandomToken(32)
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to generate session id", err)
	}
	csrfToken := security.DeriveSessionCSRFToken(sessionID)

	session, err := s.store.CreateSessionFromOAuthState(ctx, stateFromQuery, storage.CreateSessionInput{
		ID:                   sessionID,
		UserID:               user.ID,
		CSRFToken:            csrfToken,
		UserAgentFingerprint: userAgentFingerprint,
		ExpiresAt:            time.Now().UTC().Add(s.cfg.App.SessionTTL),
	})
	if err != nil {
		if storage.IsNotFound(err) {
			return LoginCompleteResult{}, UnauthorizedError("oauth state is invalid or already consumed")
		}
		return LoginCompleteResult{}, InternalError("failed to create session", err)
	}

	logAuditWriteFailure("auth.login", s.store.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "auth.login",
		ResourceType: "session",
		ResourceID:   security.AuditResourceID(session.ID),
		MetadataJSON: `{"provider":"linuxdo"}`,
	}))

	return LoginCompleteResult{User: user, Session: session, NextPath: state.NextPath}, nil
}

// AuthenticateSession resolves and validates the current browser session.
func (s *AuthService) AuthenticateSession(ctx context.Context, sessionID string, userAgentFingerprint string) (model.Session, model.User, error) {
	if strings.TrimSpace(sessionID) == "" {
		return model.Session{}, model.User{}, UnauthorizedError("missing session cookie")
	}

	session, user, err := s.store.GetSessionWithUserByID(ctx, sessionID)
	if err != nil {
		if storage.IsNotFound(err) {
			return model.Session{}, model.User{}, UnauthorizedError("session not found")
		}
		return model.Session{}, model.User{}, InternalError("failed to load session", err)
	}

	if session.ExpiresAt.Before(time.Now().UTC()) {
		_ = s.store.DeleteSession(ctx, session.ID)
		return model.Session{}, model.User{}, UnauthorizedError("session expired")
	}

	if s.cfg.App.SessionBindUserAgent && session.UserAgentFingerprint != "" && session.UserAgentFingerprint != userAgentFingerprint {
		_ = s.store.DeleteSession(ctx, session.ID)
		return model.Session{}, model.User{}, UnauthorizedError("session fingerprint mismatch")
	}

	control, err := s.store.GetUserControlByUserID(ctx, user.ID)
	if err != nil {
		return model.Session{}, model.User{}, InternalError("failed to load user moderation state", err)
	}
	if control.IsBanned {
		_ = s.store.DeleteSession(ctx, session.ID)
		return model.Session{}, model.User{}, ForbiddenError("your account has been banned")
	}

	// Re-evaluate the administrator allowlist at request time so removing a
	// username from configuration takes effect for already-issued sessions too.
	user.IsAppAdmin = isAppAdmin(user.Username, s.cfg.App.AdminUsernames)

	if err := s.store.TouchSession(ctx, session.ID); err != nil {
		return model.Session{}, model.User{}, InternalError("failed to touch session", err)
	}

	return session, user, nil
}

// Logout deletes the current session and records an audit event.
func (s *AuthService) Logout(ctx context.Context, sessionID string, actorUserID int64) error {
	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
		if storage.IsNotFound(err) {
			return nil
		}
		return InternalError("failed to delete session", err)
	}

	logAuditWriteFailure("auth.logout", s.store.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actorUserID,
		Action:       "auth.logout",
		ResourceType: "session",
		ResourceID:   security.AuditResourceID(sessionID),
		MetadataJSON: `{}`,
	}))
	return nil
}

// VerifyAdminPassword checks the extra administrator password, rotates the
// session, and returns the newly verified administrator session.
func (s *AuthService) VerifyAdminPassword(ctx context.Context, session model.Session, actor model.User, password string) (model.Session, error) {
	if !actor.IsAppAdmin {
		return model.Session{}, ForbiddenError("admin permission required")
	}
	if strings.TrimSpace(password) == "" {
		return model.Session{}, ValidationError("admin password is required")
	}
	now := time.Now().UTC()
	if AdminVerificationIsFresh(session.AdminVerifiedAt, s.cfg.App.AdminVerificationTTL, now) {
		return session, nil
	}

	if !VerifyAdminPasswordAgainstConfig(s.cfg.App, actor.Username, password) {
		logAuditWriteFailure("admin.session.verify_password_failed", s.store.WriteAuditLog(ctx, storage.AuditLogInput{
			ActorUserID:  &actor.ID,
			Action:       "admin.session.verify_password_failed",
			ResourceType: "session",
			ResourceID:   security.AuditResourceID(session.ID),
			MetadataJSON: `{"second_factor":"password","result":"rejected"}`,
		}))
		return model.Session{}, UnauthorizedError("invalid admin password")
	}

	verifiedAt := now
	newSessionID, err := security.RandomToken(32)
	if err != nil {
		return model.Session{}, InternalError("failed to generate rotated admin session id", err)
	}
	newCSRFToken := security.DeriveSessionCSRFToken(newSessionID)

	rotatedSession, err := s.store.CreateSession(ctx, storage.CreateSessionInput{
		ID:                   newSessionID,
		UserID:               actor.ID,
		CSRFToken:            newCSRFToken,
		UserAgentFingerprint: session.UserAgentFingerprint,
		AdminVerifiedAt:      &verifiedAt,
		ExpiresAt:            session.ExpiresAt,
	})
	if err != nil {
		return model.Session{}, InternalError("failed to create rotated admin session", err)
	}
	if err := s.store.DeleteSession(ctx, session.ID); err != nil {
		_ = s.store.DeleteSession(ctx, rotatedSession.ID)
		if storage.IsNotFound(err) {
			return model.Session{}, UnauthorizedError("admin session is invalid or already rotated")
		}
		return model.Session{}, InternalError("failed to retire pre-verification session", err)
	}

	logAuditWriteFailure("admin.session.verify_password", s.store.WriteAuditLog(ctx, storage.AuditLogInput{
		ActorUserID:  &actor.ID,
		Action:       "admin.session.verify_password",
		ResourceType: "session",
		ResourceID:   security.AuditResourceID(rotatedSession.ID),
		MetadataJSON: fmt.Sprintf(`{"second_factor":"password","result":"verified","previous_session_id":"%s"}`, security.AuditResourceID(session.ID)),
	}))

	return rotatedSession, nil
}

// buildAvatarURL converts the avatar template returned by Linux Do into a directly fetchable image URL.
func buildAvatarURL(avatarTemplate string) string {
	trimmed := strings.TrimSpace(avatarTemplate)
	if trimmed == "" {
		return ""
	}

	trimmed = strings.ReplaceAll(trimmed, "{size}", "256")
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	if strings.HasPrefix(trimmed, "/") {
		return "https://linux.do" + trimmed
	}
	return trimmed
}

// isAppAdmin reports whether the provided Linux Do username is explicitly listed as an application admin.
func isAppAdmin(username string, configuredAdmins []string) bool {
	for _, admin := range configuredAdmins {
		if strings.EqualFold(strings.TrimSpace(admin), strings.TrimSpace(username)) {
			return true
		}
	}
	return false
}

// firstNonEmpty returns the first non-empty string after trimming whitespace.
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
