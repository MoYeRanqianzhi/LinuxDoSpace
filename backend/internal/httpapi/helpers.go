package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/service"
)

// oauthStateCookieName 是 OAuth state 绑定浏览器时使用的 Cookie 名称。
const oauthStateCookieName = "linuxdospace_oauth_state"

// writeJSON 统一输出成功响应。
func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"data": payload,
	})
}

// writeError 统一输出失败响应。
func writeError(w http.ResponseWriter, err error) {
	normalized := service.NormalizeError(err)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(normalized.StatusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    normalized.Code,
			"message": normalized.Message,
		},
	})
}

// decodeJSONBody 严格解析 JSON 请求体，并拒绝未知字段。
func decodeJSONBody(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return service.ValidationError("invalid json request body")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return service.ValidationError("request body must contain exactly one json object")
	}
	return nil
}

// pathInt64 从标准库路由的 PathValue 中解析 int64。
func pathInt64(r *http.Request, key string) (int64, error) {
	value := strings.TrimSpace(r.PathValue(key))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, service.ValidationError("invalid path parameter: " + key)
	}
	return parsed, nil
}

// currentSessionCookieValue 读取当前请求中的会话 Cookie。
func (a *API) currentSessionCookieValue(r *http.Request) string {
	cookie, err := r.Cookie(a.config.App.SessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

// setSessionCookie 写入登录态 Cookie。
func (a *API) setSessionCookie(w http.ResponseWriter, sessionID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.config.App.SessionCookieName,
		Value:    sessionID,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(a.config.App.SessionTTL.Seconds()),
	})
}

// clearSessionCookie 清除登录态 Cookie。
func (a *API) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     a.config.App.SessionCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// setOAuthStateCookie 写入短期 OAuth state Cookie。
func (a *API) setOAuthStateCookie(w http.ResponseWriter, stateID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    stateID,
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
}

// clearOAuthStateCookie 清除短期 OAuth state Cookie。
func (a *API) clearOAuthStateCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthStateCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// currentOAuthStateCookie 读取当前请求中的 OAuth state Cookie。
func (a *API) currentOAuthStateCookie(r *http.Request) string {
	cookie, err := r.Cookie(oauthStateCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

// optionalActor 尝试解析当前用户，但当会话缺失或失效时不会直接返回 HTTP 错误。
func (a *API) optionalActor(w http.ResponseWriter, r *http.Request) (*model.Session, *model.User, error) {
	if a.authService == nil {
		return nil, nil, nil
	}

	sessionID := a.currentSessionCookieValue(r)
	if sessionID == "" {
		return nil, nil, nil
	}

	session, user, err := a.authService.AuthenticateSession(r.Context(), sessionID, security.FingerprintUserAgent(r))
	if err != nil {
		if normalized := service.NormalizeError(err); normalized.StatusCode == http.StatusUnauthorized {
			a.clearSessionCookie(w)
			return nil, nil, nil
		}
		return nil, nil, err
	}

	return &session, &user, nil
}

// requireActor 要求请求必须带有有效会话。
func (a *API) requireActor(w http.ResponseWriter, r *http.Request) (*model.Session, *model.User, bool) {
	session, user, err := a.optionalActor(w, r)
	if err != nil {
		writeError(w, err)
		return nil, nil, false
	}
	if session == nil || user == nil {
		writeError(w, service.UnauthorizedError("authentication required"))
		return nil, nil, false
	}
	return session, user, true
}

// requireAdmin 要求当前用户必须具备应用管理员身份。
func (a *API) requireAdmin(w http.ResponseWriter, r *http.Request) (*model.Session, *model.User, bool) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return nil, nil, false
	}
	if !user.IsAppAdmin {
		writeError(w, service.ForbiddenError("admin permission required"))
		return nil, nil, false
	}
	return session, user, true
}

// enforceCSRF 对有副作用的请求执行双提交令牌校验。
func (a *API) enforceCSRF(w http.ResponseWriter, r *http.Request, session *model.Session) bool {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return true
	}
	if strings.TrimSpace(r.Header.Get("X-CSRF-Token")) != session.CSRFToken {
		writeError(w, service.UnauthorizedError("invalid csrf token"))
		return false
	}
	return true
}

// frontendRedirectURL 把相对路径拼接到前端基地址上，生成登录完成后的跳转地址。
func (a *API) frontendRedirectURL(nextPath string) string {
	base := strings.TrimRight(strings.TrimSpace(a.config.App.FrontendURL), "/")
	path := security.NormalizePathOnly(nextPath)
	if base == "" {
		return path
	}
	return base + path
}
