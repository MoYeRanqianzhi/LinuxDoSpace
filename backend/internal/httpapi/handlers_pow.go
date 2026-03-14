package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"linuxdospace/backend/internal/service"
)

// handleMyPOWStatus returns the authenticated user's current proof-of-work
// dashboard state, including the active challenge and daily claim counters.
func (a *API) handleMyPOWStatus(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	item, err := a.powService.GetMyStatus(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleCreateMyPOWChallenge replaces any older active challenge with one new
// puzzle for the authenticated user.
func (a *API) handleCreateMyPOWChallenge(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.GeneratePOWChallengeRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.CreateChallenge(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleClaimMyPOWChallenge verifies one submitted nonce and grants the reward
// when the backend confirms that the active challenge is solved.
func (a *API) handleClaimMyPOWChallenge(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.SubmitPOWChallengeRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.SubmitChallenge(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminPOWSettings returns the full administrator-facing PoW settings payload.
func (a *API) handleAdminPOWSettings(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	item, err := a.powService.GetAdminSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdatePOWGlobalSettings updates the singleton global PoW feature settings.
func (a *API) handleAdminUpdatePOWGlobalSettings(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.AdminUpdatePOWGlobalSettingsRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.UpdateAdminGlobalSettings(r.Context(), *actor, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdatePOWBenefitSettings updates one benefit-specific PoW toggle.
func (a *API) handleAdminUpdatePOWBenefitSettings(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	benefitKey := strings.TrimSpace(r.PathValue("benefitKey"))
	if benefitKey == "" {
		writeError(w, service.ValidationError("benefitKey is required"))
		return
	}

	var request service.AdminUpdatePOWBenefitSettingsRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.UpdateAdminBenefitSettings(r.Context(), *actor, benefitKey, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdatePOWDifficultySettings updates one supported difficulty toggle.
func (a *API) handleAdminUpdatePOWDifficultySettings(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	difficultyValue := strings.TrimSpace(r.PathValue("difficulty"))
	if difficultyValue == "" {
		writeError(w, service.ValidationError("difficulty is required"))
		return
	}
	difficulty, err := strconv.Atoi(difficultyValue)
	if err != nil {
		writeError(w, service.ValidationError("difficulty must be a valid integer"))
		return
	}

	var request service.AdminUpdatePOWDifficultySettingsRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.UpdateAdminDifficultySettings(r.Context(), *actor, difficulty, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUserPOWSettings returns one target user's current PoW daily settings.
func (a *API) handleAdminUserPOWSettings(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.GetUserSettingsForAdmin(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleAdminUpdateUserPOWSettings updates one target user's daily PoW completion override.
func (a *API) handleAdminUpdateUserPOWSettings(w http.ResponseWriter, r *http.Request) {
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

	var request service.AdminUpdatePOWUserSettingsRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.powService.UpdateUserSettingsForAdmin(r.Context(), *actor, userID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
