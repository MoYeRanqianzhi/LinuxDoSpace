package httpapi

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/security"
	"linuxdospace/backend/internal/service"
)

// oauthStateCookieName stores the one-time OAuth state identifier on the browser.
const oauthStateCookieName = "linuxdospace_oauth_state"

// oauthTargetCookieName stores which frontend should receive the post-login redirect.
const oauthTargetCookieName = "linuxdospace_oauth_target"

const (
	oauthTargetApp   = "app"
	oauthTargetAdmin = "admin"
)

// writeJSON writes a successful JSON response envelope.
func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": payload})
}

// writeError writes a normalized JSON error response envelope.
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

// decodeJSONBody strictly parses one JSON request object and rejects unknown fields.
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

// pathInt64 parses one positive int64 from a standard library PathValue.
func pathInt64(r *http.Request, key string) (int64, error) {
	value := strings.TrimSpace(r.PathValue(key))
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, service.ValidationError("invalid path parameter: " + key)
	}
	return parsed, nil
}

// currentSessionCookieValue reads the current session cookie value from the request.
func (a *API) currentSessionCookieValue(r *http.Request) string {
	cookie, err := r.Cookie(a.config.App.SessionCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

// setSessionCookie writes the authenticated session cookie.
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

// clearSessionCookie removes the authenticated session cookie.
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

// setOAuthStateCookie writes the short-lived OAuth state cookie.
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

// clearOAuthStateCookie removes the short-lived OAuth state cookie.
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

// currentOAuthStateCookie reads the short-lived OAuth state cookie.
func (a *API) currentOAuthStateCookie(r *http.Request) string {
	cookie, err := r.Cookie(oauthStateCookieName)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

// setOAuthTargetCookie writes the short-lived login target cookie.
func (a *API) setOAuthTargetCookie(w http.ResponseWriter, target string) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthTargetCookieName,
		Value:    normalizeOAuthTarget(target),
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((10 * time.Minute).Seconds()),
	})
}

// clearOAuthTargetCookie removes the short-lived login target cookie.
func (a *API) clearOAuthTargetCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     oauthTargetCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   a.config.App.SessionSecure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
}

// currentOAuthTargetCookie reads the short-lived login target cookie.
func (a *API) currentOAuthTargetCookie(r *http.Request) string {
	cookie, err := r.Cookie(oauthTargetCookieName)
	if err != nil {
		return oauthTargetApp
	}
	return normalizeOAuthTarget(cookie.Value)
}

// normalizeOAuthTarget restricts login targets to the two supported frontend applications.
func normalizeOAuthTarget(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case oauthTargetAdmin:
		return oauthTargetAdmin
	default:
		return oauthTargetApp
	}
}

// optionalActor attempts to resolve the current user but gracefully treats invalid sessions as signed out.
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
		normalized := service.NormalizeError(err)
		if normalized.StatusCode == http.StatusUnauthorized || normalized.StatusCode == http.StatusForbidden {
			a.clearSessionCookie(w)
			return nil, nil, nil
		}
		return nil, nil, err
	}

	return &session, &user, nil
}

// requireActor requires a valid authenticated session.
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

// requireAdmin requires the current actor to hold application administrator permissions.
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

// requireVerifiedAdmin requires one administrator session that has already
// completed the extra admin password verification step.
func (a *API) requireVerifiedAdmin(w http.ResponseWriter, r *http.Request) (*model.Session, *model.User, bool) {
	session, user, ok := a.requireAdmin(w, r)
	if !ok {
		return nil, nil, false
	}
	if session.AdminVerifiedAt == nil {
		writeError(w, &service.Error{
			StatusCode: http.StatusForbidden,
			Code:       "admin_password_required",
			Message:    "admin password verification required",
		})
		return nil, nil, false
	}
	return session, user, true
}

// enforceCSRF validates the double-submit token on unsafe requests.
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

// frontendRedirectURL combines the configured frontend base URL with a normalized relative path.
func (a *API) frontendRedirectURL(target string, nextPath string) string {
	base := strings.TrimRight(strings.TrimSpace(a.config.App.FrontendURL), "/")
	if normalizeOAuthTarget(target) == oauthTargetAdmin {
		base = strings.TrimRight(strings.TrimSpace(a.config.App.AdminFrontendURL), "/")
	}
	path := security.NormalizePathOnly(nextPath)
	if base == "" {
		return path
	}
	return base + path
}

// requestClientIP extracts the best available client IP from trusted reverse-
// proxy headers and finally falls back to RemoteAddr for local development.
func requestClientIP(r *http.Request) string {
	if value := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); value != "" {
		return value
	}
	if forwardedFor := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwardedFor != "" {
		parts := strings.Split(forwardedFor, ",")
		if len(parts) > 0 {
			if candidate := strings.TrimSpace(parts[0]); candidate != "" {
				return candidate
			}
		}
	}
	if value := strings.TrimSpace(r.Header.Get("X-Real-IP")); value != "" {
		return value
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return strings.TrimSpace(r.RemoteAddr)
}
