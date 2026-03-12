package httpapi

import (
	"net/http"
	"strings"

	"linuxdospace/backend/internal/service"
)

// handlePublicPaymentProducts returns all currently enabled LDC products so the
// public frontend can render pricing before the user logs in.
func (a *API) handlePublicPaymentProducts(w http.ResponseWriter, r *http.Request) {
	items, err := a.paymentService.ListPublicProducts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleMyPaymentOrders returns the authenticated user's recent LDC orders.
func (a *API) handleMyPaymentOrders(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.paymentService.ListMyOrders(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateMyPaymentOrder reserves one local order and returns the upstream
// Linux Do Credit checkout URL.
func (a *API) handleCreateMyPaymentOrder(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request service.CreatePaymentOrderRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.paymentService.CreateOrder(r.Context(), *user, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleMyPaymentOrder returns one specific order and opportunistically
// refreshes its gateway status for the current user.
func (a *API) handleMyPaymentOrder(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	outTradeNo := strings.TrimSpace(r.PathValue("outTradeNo"))
	if outTradeNo == "" {
		writeError(w, service.ValidationError("outTradeNo is required"))
		return
	}

	item, err := a.paymentService.GetMyOrder(r.Context(), *user, outTradeNo)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleLinuxDOCreditNotify verifies and applies one asynchronous gateway
// success callback. The upstream retrier requires a literal `success` body.
func (a *API) handleLinuxDOCreditNotify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, service.ValidationError("notify endpoint only accepts GET"))
		return
	}

	if _, _, err := a.paymentService.HandleGatewayNotification(r.Context(), r.URL.Query()); err != nil {
		writeError(w, err)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("success"))
}

// handleAdminPaymentProducts returns the full administrator-editable product
// list, including disabled items.
func (a *API) handleAdminPaymentProducts(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}

	items, err := a.paymentService.ListPaymentProducts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAdminUpdatePaymentProduct updates one administrator-editable LDC
// product row.
func (a *API) handleAdminUpdatePaymentProduct(w http.ResponseWriter, r *http.Request) {
	session, actor, ok := a.requireVerifiedAdmin(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	productKey := strings.TrimSpace(r.PathValue("productKey"))
	if productKey == "" {
		writeError(w, service.ValidationError("productKey is required"))
		return
	}

	var request service.UpdatePaymentProductRequest
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.paymentService.UpdatePaymentProduct(r.Context(), *actor, productKey, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
