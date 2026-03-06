package service

import (
	"context"
	"strings"
	"time"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/storage/sqlite"
)

// oauthStateLifetime 控制一次 OAuth 登录 state 的有效期。
const oauthStateLifetime = 10 * time.Minute

// AuthService 负责 Linux Do OAuth 登录、服务端会话和当前用户解析。
type AuthService struct {
	cfg   config.Config
	store Store
	oauth OAuthClient
}

// LoginStartResult 表示开始 OAuth 登录时需要返回给 HTTP 层的信息。
type LoginStartResult struct {
	StateID     string
	RedirectURL string
}

// LoginCompleteResult 表示 OAuth 回调完成后的结果。
type LoginCompleteResult struct {
	User     model.User
	Session  model.Session
	NextPath string
}

// NewAuthService 创建认证服务。
func NewAuthService(cfg config.Config, store Store, oauth OAuthClient) *AuthService {
	return &AuthService{
		cfg:   cfg,
		store: store,
		oauth: oauth,
	}
}

// Configured 返回认证服务是否具备运行条件。
func (s *AuthService) Configured() bool {
	return s.oauth != nil && s.oauth.Configured()
}

// BeginLogin 创建一次新的 OAuth state，并返回 Linux Do 登录地址。
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
		codeVerifier, err = security.RandomToken(48)
		if err != nil {
			return LoginStartResult{}, InternalError("failed to generate pkce verifier", err)
		}
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

// CompleteLogin 完成授权码交换、获取用户信息、落库并创建会话。
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

	state, err := s.store.ConsumeOAuthState(ctx, stateFromQuery)
	if err != nil {
		if sqlite.IsNotFound(err) {
			return LoginCompleteResult{}, UnauthorizedError("oauth state is invalid or already consumed")
		}
		return LoginCompleteResult{}, InternalError("failed to consume oauth state", err)
	}
	if state.ExpiresAt.Before(time.Now().UTC()) {
		return LoginCompleteResult{}, UnauthorizedError("oauth state has expired")
	}

	token, err := s.oauth.ExchangeCode(ctx, code, state.CodeVerifier)
	if err != nil {
		return LoginCompleteResult{}, UnavailableError("failed to exchange linux.do oauth code", err)
	}

	profile, err := s.oauth.GetCurrentUser(ctx, token.AccessToken)
	if err != nil {
		return LoginCompleteResult{}, UnavailableError("failed to fetch linux.do user profile", err)
	}

	user, err := s.store.UpsertUser(ctx, sqlite.UpsertUserInput{
		LinuxDOUserID:  profile.ID,
		Username:       profile.Username,
		DisplayName:    firstNonEmpty(strings.TrimSpace(profile.Name), strings.TrimSpace(profile.Username)),
		AvatarURL:      buildAvatarURL(profile.AvatarTemplate),
		TrustLevel:     profile.TrustLevel,
		IsLinuxDOAdmin: profile.Admin,
		IsAppAdmin:     isAppAdmin(profile.Username, s.cfg.App.AdminUsernames) || profile.Admin,
	})
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to upsert local user", err)
	}

	sessionID, err := security.RandomToken(32)
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to generate session id", err)
	}

	csrfToken, err := security.RandomToken(32)
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to generate csrf token", err)
	}

	session, err := s.store.CreateSession(ctx, sqlite.CreateSessionInput{
		ID:                   sessionID,
		UserID:               user.ID,
		CSRFToken:            csrfToken,
		UserAgentFingerprint: userAgentFingerprint,
		ExpiresAt:            time.Now().UTC().Add(s.cfg.App.SessionTTL),
	})
	if err != nil {
		return LoginCompleteResult{}, InternalError("failed to create session", err)
	}

	if err := s.store.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &user.ID,
		Action:       "auth.login",
		ResourceType: "session",
		ResourceID:   session.ID,
		MetadataJSON: `{"provider":"linuxdo"}`,
	}); err != nil {
		return LoginCompleteResult{}, InternalError("failed to write auth login audit log", err)
	}

	return LoginCompleteResult{
		User:     user,
		Session:  session,
		NextPath: state.NextPath,
	}, nil
}

// AuthenticateSession 解析并校验当前请求携带的会话。
func (s *AuthService) AuthenticateSession(ctx context.Context, sessionID string, userAgentFingerprint string) (model.Session, model.User, error) {
	if strings.TrimSpace(sessionID) == "" {
		return model.Session{}, model.User{}, UnauthorizedError("missing session cookie")
	}

	session, user, err := s.store.GetSessionWithUserByID(ctx, sessionID)
	if err != nil {
		if sqlite.IsNotFound(err) {
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

	if err := s.store.TouchSession(ctx, session.ID); err != nil {
		return model.Session{}, model.User{}, InternalError("failed to touch session", err)
	}

	return session, user, nil
}

// Logout 删除当前会话并记录审计事件。
func (s *AuthService) Logout(ctx context.Context, sessionID string, actorUserID int64) error {
	if err := s.store.DeleteSession(ctx, sessionID); err != nil {
		return InternalError("failed to delete session", err)
	}

	if err := s.store.WriteAuditLog(ctx, sqlite.AuditLogInput{
		ActorUserID:  &actorUserID,
		Action:       "auth.logout",
		ResourceType: "session",
		ResourceID:   sessionID,
		MetadataJSON: `{}`,
	}); err != nil {
		return InternalError("failed to write auth logout audit log", err)
	}

	return nil
}

// buildAvatarURL 把 Linux Do 返回的头像模板转换为可直接访问的 URL。
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

// isAppAdmin 根据配置判断一个 Linux Do 用户名是否被显式授予应用管理员身份。
func isAppAdmin(username string, configuredAdmins []string) bool {
	for _, admin := range configuredAdmins {
		if strings.EqualFold(strings.TrimSpace(admin), strings.TrimSpace(username)) {
			return true
		}
	}
	return false
}

// firstNonEmpty 返回第一个非空字符串。
func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
