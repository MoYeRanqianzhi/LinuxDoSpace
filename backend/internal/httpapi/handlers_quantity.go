package httpapi

import (
	"net/http"

	"linuxdospace/backend/internal/service"
)

// handleMyQuantityRecords returns the authenticated user's append-only quantity
// ledger so future billing and redeem flows can render an auditable history.
func (a *API) handleMyQuantityRecords(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.quantityService.ListMyQuantityRecords(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleMyQuantityBalances returns the authenticated user's current non-expired
// balances grouped by resource key and optional scope.
func (a *API) handleMyQuantityBalances(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.quantityService.ListMyQuantityBalances(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUserQuantityRecords returns one target user's full quantity ledger
// for administrator inspection.
func (a *API) handleAdminUserQuantityRecords(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	items, err := a.quantityService.ListQuantityRecordsForUser(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUserQuantityBalances returns one target user's current quantity
// balances grouped by resource key and optional scope.
func (a *API) handleAdminUserQuantityBalances(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	userID, err := pathInt64(r, "userID")
	if err != nil {
		writeError(w, err)
		return
	}

	items, err := a.quantityService.ListQuantityBalancesForUser(r.Context(), userID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminCreateQuantityRecord appends one administrator-authored quantity
// delta to the target user's immutable ledger.
func (a *API) handleAdminCreateQuantityRecord(w http.ResponseWriter, r *http.Request) {
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

	var request service.AdminCreateQuantityRecordRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.quantityService.CreateQuantityRecord(r.Context(), *actor, userID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}
