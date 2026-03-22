package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"linuxdospace/backend/internal/mailrelay"
	"linuxdospace/backend/internal/service"
)

const tokenStreamHeartbeatInterval = 15 * time.Second

// handleMyAPITokens returns the current user's API tokens.
func (a *API) handleMyAPITokens(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if a.tokenService == nil {
		writeError(w, service.UnavailableError("api token service is not configured", nil))
		return
	}

	items, err := a.tokenService.ListMyAPITokens(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateMyAPIToken issues one new API token for the current user.
func (a *API) handleCreateMyAPIToken(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}
	if a.tokenService == nil {
		writeError(w, service.UnavailableError("api token service is not configured", nil))
		return
	}

	var request service.CreateMyAPITokenRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.tokenService.CreateMyAPIToken(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleRevokeMyAPIToken revokes one existing user-owned token.
func (a *API) handleRevokeMyAPIToken(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}
	if a.tokenService == nil {
		writeError(w, service.UnavailableError("api token service is not configured", nil))
		return
	}

	publicID := strings.TrimSpace(r.PathValue("publicID"))
	if publicID == "" {
		writeError(w, service.ValidationError("publicID is required"))
		return
	}

	item, err := a.tokenService.RevokeMyAPIToken(r.Context(), *user, publicID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleTokenEmailStream upgrades one authenticated API token into the live
// NDJSON email stream consumed by the SDKs.
func (a *API) handleTokenEmailStream(w http.ResponseWriter, r *http.Request) {
	if a.tokenService == nil || a.tokenService.Hub() == nil {
		writeError(w, service.UnavailableError("api token stream is not configured", nil))
		return
	}

	rawToken, ok := bearerTokenFromRequest(r)
	if !ok {
		writeError(w, service.UnauthorizedError("missing bearer token"))
		return
	}

	token, err := a.tokenService.AuthenticateEmailStreamToken(r.Context(), rawToken)
	if err != nil {
		writeError(w, err)
		return
	}
	ownerUsername, err := a.tokenService.ResolveStreamOwnerUsername(r.Context(), token)
	if err != nil {
		writeError(w, err)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, service.UnavailableError("streaming is not supported by this server", nil))
		return
	}

	subscription, err := a.tokenService.Hub().Subscribe(token.PublicID)
	if err != nil {
		if err == mailrelay.ErrTokenStreamAlreadyConnected {
			writeError(w, service.ConflictError("this api token already has one active email stream connection"))
			return
		}
		writeError(w, service.InternalError("failed to open api token stream", err))
		return
	}
	defer subscription.Cancel()

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	log.Printf(
		"linuxdospace api token stream connected: token=%s remote_addr=%s user_agent=%q",
		token.PublicID,
		r.RemoteAddr,
		r.UserAgent(),
	)
	defer log.Printf(
		"linuxdospace api token stream disconnected: token=%s remote_addr=%s",
		token.PublicID,
		r.RemoteAddr,
	)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(mailrelay.TokenStreamEvent{
		Type:          "ready",
		TokenPublicID: token.PublicID,
		OwnerUsername: ownerUsername,
	}); err != nil {
		return
	}
	flusher.Flush()

	heartbeatTicker := time.NewTicker(tokenStreamHeartbeatInterval)
	defer heartbeatTicker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-subscription.Done():
			return
		case <-heartbeatTicker.C:
			if err := encoder.Encode(mailrelay.TokenStreamEvent{
				Type:          "heartbeat",
				TokenPublicID: token.PublicID,
			}); err != nil {
				return
			}
			flusher.Flush()
		case event := <-subscription.Events():
			if err := encoder.Encode(event.ToStreamEvent()); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func bearerTokenFromRequest(r *http.Request) (string, bool) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization == "" {
		return "", false
	}
	const bearerPrefix = "Bearer "
	if !strings.HasPrefix(authorization, bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(authorization, bearerPrefix))
	if token == "" {
		return "", false
	}
	return token, true
}
