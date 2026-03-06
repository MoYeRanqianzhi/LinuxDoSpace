package httpapi

import (
	"log"
	"net/http"

	"linuxdospace/backend/internal/security"
)

// handleAuthLogin 发起 Linux Do OAuth 登录。
func (a *API) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	result, err := a.authService.BeginLogin(r.Context(), r.URL.Query().Get("next"))
	if err != nil {
		writeError(w, err)
		return
	}

	a.setOAuthStateCookie(w, result.StateID)
	http.Redirect(w, r, result.RedirectURL, http.StatusFound)
}

// handleAuthCallback 处理 Linux Do OAuth 回调。
func (a *API) handleAuthCallback(w http.ResponseWriter, r *http.Request) {
	result, err := a.authService.CompleteLogin(
		r.Context(),
		r.URL.Query().Get("state"),
		a.currentOAuthStateCookie(r),
		r.URL.Query().Get("code"),
		security.FingerprintUserAgent(r),
	)
	if err != nil {
		writeError(w, err)
		return
	}

	// 自动分配不是登录成功的必要条件，因此失败时只记录日志，不中断登录。
	if err := a.domainService.AutoProvisionForUser(r.Context(), result.User); err != nil {
		log.Printf("auto provision after login failed for user %s: %v", result.User.Username, err)
	}

	a.setSessionCookie(w, result.Session.ID)
	a.clearOAuthStateCookie(w)
	http.Redirect(w, r, a.frontendRedirectURL(result.NextPath), http.StatusFound)
}

// handleAuthLogout 销毁当前会话。
func (a *API) handleAuthLogout(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	if err := a.authService.Logout(r.Context(), session.ID, user.ID); err != nil {
		writeError(w, err)
		return
	}

	a.clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"logged_out": true,
	})
}

// handleMe 返回当前登录用户的基础信息、会话和分配列表。
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	session, user, err := a.optionalActor(w, r)
	if err != nil {
		writeError(w, err)
		return
	}

	if session == nil || user == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":    false,
			"oauth_configured": a.config.OAuthConfigured(),
		})
		return
	}

	allocations, err := a.domainService.ListAllocationsForUser(r.Context(), user.ID)
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":      true,
		"user":               user,
		"csrf_token":         session.CSRFToken,
		"session_expires_at": session.ExpiresAt,
		"allocations":        allocations,
	})
}
