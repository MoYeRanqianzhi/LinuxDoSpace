package httpapi

import (
	"net/http"
	"strconv"
	"time"

	"linuxdospace/backend/internal/service"
)

// verifyAdminPasswordRequest describes the JSON payload accepted by the admin
// second-factor password verification endpoint.
type verifyAdminPasswordRequest struct {
	Password string `json:"password"`
}

// handleAdminMe returns the current administrator session state for the standalone admin frontend.
func (a *API) handleAdminMe(w http.ResponseWriter, r *http.Request) {
	session, user, err := a.optionalActor(w, r)
	if err != nil {
		writeError(w, err)
		return
	}

	if session == nil || user == nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":     false,
			"authorized":        false,
			"password_verified": false,
			"oauth_configured":  a.config.OAuthConfigured(),
		})
		return
	}

	if !user.IsAppAdmin {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":      true,
			"authorized":         false,
			"password_verified":  false,
			"oauth_configured":   a.config.OAuthConfigured(),
			"user":               user,
			"csrf_token":         session.CSRFToken,
			"session_expires_at": session.ExpiresAt,
		})
		return
	}

	adminPasswordVerified := service.AdminVerificationIsFresh(session.AdminVerifiedAt, a.config.App.AdminVerificationTTL, time.Now().UTC())
	if !adminPasswordVerified {
		writeJSON(w, http.StatusOK, map[string]any{
			"authenticated":      true,
			"authorized":         true,
			"password_verified":  false,
			"oauth_configured":   a.config.OAuthConfigured(),
			"user":               user,
			"csrf_token":         session.CSRFToken,
			"session_expires_at": session.ExpiresAt,
		})
		return
	}

	managedDomains, err := a.domainService.ListAdminDomains(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":      true,
		"authorized":         true,
		"password_verified":  adminPasswordVerified,
		"oauth_configured":   a.config.OAuthConfigured(),
		"user":               user,
		"csrf_token":         session.CSRFToken,
		"session_expires_at": session.ExpiresAt,
		"admin_verified_at":  session.AdminVerifiedAt,
		"managed_domains":    managedDomains,
	})
}

// handleAdminVerifyPassword upgrades one authenticated administrator session
// after the extra password has been verified on the server side.
func (a *API) handleAdminVerifyPassword(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	clientIP := requestClientIP(r, a.config.App.TrustedProxyCIDRs)
	unlockUser := a.adminPasswordLimiter.lockUser(user.ID)
	defer unlockUser()
	if retryAfter, blocked := a.adminPasswordLimiter.Check(r.Context(), session.ID, clientIP, user.ID, time.Now().UTC()); blocked {
		w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())))
		writeError(w, service.TooManyRequestsError("too many invalid admin password attempts, please retry later"))
		return
	}

	var request verifyAdminPasswordRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	verifiedSession, err := a.authService.VerifyAdminPassword(r.Context(), *session, *user, request.Password)
	if err != nil {
		if normalized := service.NormalizeError(err); normalized != nil && normalized.Code == "unauthorized" {
			a.adminPasswordLimiter.RegisterFailure(r.Context(), session.ID, clientIP, user.ID, time.Now().UTC())
		}
		writeError(w, err)
		return
	}
	a.adminPasswordLimiter.Reset(r.Context(), session.ID, clientIP, user.ID)
	a.setSessionCookie(w, verifiedSession.ID)

	managedDomains, err := a.domainService.ListAdminDomains(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"authenticated":      true,
		"authorized":         true,
		"password_verified":  true,
		"oauth_configured":   a.config.OAuthConfigured(),
		"user":               user,
		"csrf_token":         verifiedSession.CSRFToken,
		"session_expires_at": verifiedSession.ExpiresAt,
		"admin_verified_at":  verifiedSession.AdminVerifiedAt,
		"managed_domains":    managedDomains,
	})
}

// handleAdminDomains returns all managed root domain configurations.
func (a *API) handleAdminDomains(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.domainService.ListAdminDomains(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUpsertDomain creates or updates one managed root domain configuration.
func (a *API) handleAdminUpsertDomain(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.UpsertManagedDomainRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.domainService.UpsertManagedDomain(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAdminSetQuota writes one user quota override for one managed root domain.
func (a *API) handleAdminSetQuota(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.SetUserQuotaRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.domainService.SetUserQuota(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAdminUsers returns the compact user list required by the admin console user page.
func (a *API) handleAdminUsers(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListUsers(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUserDetail returns the expanded moderation and quota view for one user.
func (a *API) handleAdminUserDetail(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.GetUserDetail(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdateUser updates the moderation state for one user.
func (a *API) handleAdminUpdateUser(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	var request service.UpdateAdminUserRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.UpdateUser(r.Context(), *actor, userID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminAllocations returns all allocation namespaces with owner identity.
func (a *API) handleAdminAllocations(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListAllocations(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminCreateAllocation manually creates one allocation namespace.
func (a *API) handleAdminCreateAllocation(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.CreateAdminAllocationRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.CreateAllocation(r.Context(), *actor, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAdminUpdateAllocation changes the lifecycle controls for one existing allocation.
func (a *API) handleAdminUpdateAllocation(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	allocationID, err := pathInt64(r, "allocationID")
	if err != nil {
		writeError(w, err)
		return
	}

	var request service.UpdateAdminAllocationRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.UpdateAllocation(r.Context(), *actor, allocationID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminRecords returns the global DNS record list visible to administrators.
func (a *API) handleAdminRecords(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListRecords(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminCreateRecord creates a DNS record inside one allocation namespace.
func (a *API) handleAdminCreateRecord(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	allocationID, err := pathInt64(r, "allocationID")
	if err != nil {
		writeError(w, err)
		return
	}

	var request service.UpsertAdminRecordRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.CreateRecord(r.Context(), *actor, allocationID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAdminUpdateRecord updates a DNS record inside one allocation namespace.
func (a *API) handleAdminUpdateRecord(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	allocationID, err := pathInt64(r, "allocationID")
	if err != nil {
		writeError(w, err)
		return
	}

	recordID := r.PathValue("recordID")
	if recordID == "" {
		writeError(w, service.ValidationError("recordID is required"))
		return
	}

	var request service.UpsertAdminRecordRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.UpdateRecord(r.Context(), *actor, allocationID, recordID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminDeleteRecord deletes a DNS record inside one allocation namespace.
func (a *API) handleAdminDeleteRecord(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	allocationID, err := pathInt64(r, "allocationID")
	if err != nil {
		writeError(w, err)
		return
	}

	recordID := r.PathValue("recordID")
	if recordID == "" {
		writeError(w, service.ValidationError("recordID is required"))
		return
	}

	if err := a.adminService.DeleteRecord(r.Context(), *actor, allocationID, recordID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// handleAdminEmailRoutes lists all administrator-managed email forwarding rules.
func (a *API) handleAdminEmailRoutes(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListEmailRoutes(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminCreateEmailRoute creates one email forwarding rule.
func (a *API) handleAdminCreateEmailRoute(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.UpsertEmailRouteRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.CreateEmailRoute(r.Context(), *actor, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAdminUpdateEmailRoute updates one email forwarding rule.
func (a *API) handleAdminUpdateEmailRoute(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	routeID, err := pathInt64(r, "routeID")
	if err != nil {
		writeError(w, err)
		return
	}

	var request service.UpdateEmailRouteRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.UpdateEmailRoute(r.Context(), *actor, routeID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminDeleteEmailRoute deletes one email forwarding rule.
func (a *API) handleAdminDeleteEmailRoute(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	routeID, err := pathInt64(r, "routeID")
	if err != nil {
		writeError(w, err)
		return
	}

	if err := a.adminService.DeleteEmailRoute(r.Context(), *actor, routeID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// handleAdminApplications lists all moderation requests.
func (a *API) handleAdminApplications(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListApplications(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUpdateApplication updates the moderation decision for one request.
func (a *API) handleAdminUpdateApplication(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	applicationID, err := pathInt64(r, "applicationID")
	if err != nil {
		writeError(w, err)
		return
	}

	var request service.UpdateApplicationRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.adminService.UpdateApplication(r.Context(), *actor, applicationID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminRedeemCodes lists all generated redeem codes.
func (a *API) handleAdminRedeemCodes(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.adminService.ListRedeemCodes(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminGenerateRedeemCodes generates one batch of redeem codes.
func (a *API) handleAdminGenerateRedeemCodes(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.GenerateRedeemCodesRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	items, err := a.adminService.GenerateRedeemCodes(r.Context(), *actor, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, items)
}

// handleAdminDeleteRedeemCode deletes one generated redeem code.
func (a *API) handleAdminDeleteRedeemCode(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	redeemCodeID, err := pathInt64(r, "redeemCodeID")
	if err != nil {
		writeError(w, err)
		return
	}

	if err := a.adminService.DeleteRedeemCode(r.Context(), *actor, redeemCodeID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
}
