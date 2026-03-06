package httpapi

import (
	"net/http"

	"linuxdospace/backend/internal/service"
)

// handleMyAllocations 返回当前用户的全部分配。
func (a *API) handleMyAllocations(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	items, err := a.domainService.ListVisibleAllocationsForUser(r.Context(), *user)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateAllocation 为当前用户创建新的命名空间分配。
func (a *API) handleCreateAllocation(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}
	if !a.enforceCSRF(w, r, session) {
		return
	}

	var request struct {
		RootDomain string `json:"root_domain"`
		Prefix     string `json:"prefix"`
		Source     string `json:"source"`
		Primary    bool   `json:"primary"`
	}
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.domainService.CreateAllocation(r.Context(), *user, request.RootDomain, request.Prefix, request.Source, request.Primary)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleAllocationRecords 返回当前用户某个分配下的全部 DNS 记录。
func (a *API) handleAllocationRecords(w http.ResponseWriter, r *http.Request) {
	_, user, ok := a.requireActor(w, r)
	if !ok {
		return
	}

	allocationID, err := pathInt64(r, "allocationID")
	if err != nil {
		writeError(w, err)
		return
	}

	items, err := a.domainService.ListRecordsForAllocation(r.Context(), *user, allocationID)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleCreateRecord 为当前用户某个分配创建一条 DNS 记录。
func (a *API) handleCreateRecord(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
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

	var request service.DNSRecordInput
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.domainService.CreateRecord(r.Context(), *user, allocationID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

// handleUpdateRecord 更新当前用户某个分配中的 DNS 记录。
func (a *API) handleUpdateRecord(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
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

	var request service.DNSRecordInput
	if err := decodeJSONBody(r, &request); err != nil {
		writeError(w, err)
		return
	}

	item, err := a.domainService.UpdateRecord(r.Context(), *user, allocationID, recordID, request)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// handleDeleteRecord 删除当前用户某个分配中的 DNS 记录。
func (a *API) handleDeleteRecord(w http.ResponseWriter, r *http.Request) {
	session, user, ok := a.requireActor(w, r)
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

	if err := a.domainService.DeleteRecord(r.Context(), *user, allocationID, recordID); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deleted": true,
	})
}
