package httpapi

import (
	"net/http"

	"linuxdospace/backend/internal/service"
)

// handleMyPermissions returns the current authenticated user's permission cards.
func (a *API) handleMyPermissions(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.permissionService.ListMyPermissions(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleSubmitPermissionApplication stores or refreshes one permission request.
func (a *API) handleSubmitPermissionApplication(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.SubmitPermissionApplicationRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.SubmitPermissionApplication(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleMyEmailRoutes returns the current user's user-manageable email routes.
func (a *API) handleMyEmailRoutes(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.permissionService.ListMyEmailRoutes(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleMyEmailTargets returns the current user's bound forwarding destinations.
func (a *API) handleMyEmailTargets(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.permissionService.ListMyEmailTargets(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateMyEmailTarget binds one external mailbox to the current user and
// asks Cloudflare to send the verification email when needed.
func (a *API) handleCreateMyEmailTarget(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.CreateMyEmailTargetRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.CreateMyEmailTarget(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleResendMyEmailTargetVerification retriggers the Cloudflare verification
// email for one still-pending target mailbox owned by the current user.
func (a *API) handleResendMyEmailTargetVerification(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	targetID, err := pathInt64(r, "targetID")
	if err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.ResendMyEmailTargetVerification(r.Context(), *user, targetID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleUpsertDefaultEmailRoute writes the authenticated user's forwarding
// target for the always-owned default mailbox.
func (a *API) handleUpsertDefaultEmailRoute(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.UpsertMyDefaultEmailRouteRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.UpsertMyDefaultEmailRoute(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleUpsertCatchAllEmailRoute writes the authenticated user's catch-all
// forwarding target after the permission has been approved.
func (a *API) handleUpsertCatchAllEmailRoute(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.UpsertMyCatchAllEmailRouteRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.UpsertMyCatchAllEmailRoute(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminPermissionPolicies returns all administrator-configurable policy
// rows that control permission eligibility and auto-approval.
func (a *API) handleAdminPermissionPolicies(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.permissionService.ListPermissionPolicies(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUpdatePermissionPolicy updates one permission-policy row.
func (a *API) handleAdminUpdatePermissionPolicy(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	policyKey := r.PathValue("policyKey")
	if policyKey == "" {
		writeError(w, service.ValidationError("policyKey is required"))
		return
	}

	var request service.UpdatePermissionPolicyRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.UpdatePermissionPolicy(r.Context(), *actor, policyKey, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUserPermissions returns the current permission cards for one target user.
func (a *API) handleAdminUserPermissions(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	items, err := a.permissionService.ListPermissionsForUser(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminSetUserPermission lets an administrator directly override one target user's permission state.
func (a *API) handleAdminSetUserPermission(w http.ResponseWriter, r *http.Request) {
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

	permissionKey := r.PathValue("permissionKey")
	if permissionKey == "" {
		writeError(w, service.ValidationError("permissionKey is required"))
		return
	}

	var request service.AdminSetUserPermissionRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.permissionService.SetPermissionForUser(r.Context(), *actor, userID, permissionKey, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdateUserPermissionAccess lets an administrator adjust the
// mutable catch-all runtime access state for one target user.
func (a *API) handleAdminUpdateUserPermissionAccess(w http.ResponseWriter, r *http.Request) {
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

	permissionKey := r.PathValue("permissionKey")
	if permissionKey == "" {
		writeError(w, service.ValidationError("permissionKey is required"))
		return
	}

	var request service.AdminUpdateEmailCatchAllAccessRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	if permissionKey != service.PermissionKeyEmailCatchAll {
		writeError(w, service.ValidationError("unsupported permission key"))
		return
	}

	item, err := a.permissionService.UpdateEmailCatchAllAccessForUser(r.Context(), *actor, userID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
