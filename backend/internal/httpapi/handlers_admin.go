package httpapi

import (
	"net/http"

	"linuxdospace/backend/internal/service"
)

// handleAdminDomains 返回管理员视角下的全部根域名配置。
func (a *API) handleAdminDomains(w http.ResponseWriter, r *http.Request) {
	_, _, ok := a.requireAdmin(w, r)
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

// handleAdminUpsertDomain 创建或更新可分发根域名配置。
func (a *API) handleAdminUpsertDomain(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireAdmin(w, r)
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

// handleAdminSetQuota 为指定用户写入根域名配额覆盖。
func (a *API) handleAdminSetQuota(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireAdmin(w, r)
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
