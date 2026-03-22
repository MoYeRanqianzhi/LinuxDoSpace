package httpapi

import (
	"net/http"
	"time"
)

type publicManagedDomainView struct {
	ID                 int64     `json:"id"`
	RootDomain         string    `json:"root_domain"`
	DefaultQuota       int       `json:"default_quota"`
	AutoProvision      bool      `json:"auto_provision"`
	IsDefault          bool      `json:"is_default"`
	Enabled            bool      `json:"enabled"`
	SaleEnabled        bool      `json:"sale_enabled"`
	SaleBasePriceCents int64     `json:"sale_base_price_cents"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// handleHealth 返回服务健康状态。
func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	payload := map[string]any{
		"status":  "ok",
		"version": a.version,
		"time":    time.Now().UTC(),
	}
	if len(a.startupWarnings) > 0 {
		payload["degraded"] = true
		payload["startup_warnings"] = append([]string(nil), a.startupWarnings...)
	}
	writeJSON(w, http.StatusOK, payload)
}

// handlePublicDomains 返回当前可公开申请的根域名。
func (a *API) handlePublicDomains(w http.ResponseWriter, r *http.Request) {
	items, err := a.domainService.ListPublicDomains(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	views := make([]publicManagedDomainView, 0, len(items))
	for _, item := range items {
		views = append(views, publicManagedDomainView{
			ID:                 item.ID,
			RootDomain:         item.RootDomain,
			DefaultQuota:       item.DefaultQuota,
			AutoProvision:      item.AutoProvision,
			IsDefault:          item.IsDefault,
			Enabled:            item.Enabled,
			SaleEnabled:        item.SaleEnabled,
			SaleBasePriceCents: item.SaleBasePriceCents,
			CreatedAt:          item.CreatedAt,
			UpdatedAt:          item.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, views)
}

// handlePublicSupervision 返回公开监督页使用的脱敏子域归属列表。
func (a *API) handlePublicSupervision(w http.ResponseWriter, r *http.Request) {
	items, err := a.domainService.ListPublicAllocationOwnerships(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}

// handleAllocationAvailability 检查某个前缀在指定根域名下是否可用。
func (a *API) handleAllocationAvailability(w http.ResponseWriter, r *http.Request) {
	rootDomain := r.URL.Query().Get("root_domain")
	prefix := r.URL.Query().Get("prefix")

	result, err := a.domainService.CheckPublicAvailability(r.Context(), rootDomain, prefix)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// handlePublicEmailRouteAvailability checks whether one mailbox local-part is
// currently available on a managed email domain.
func (a *API) handlePublicEmailRouteAvailability(w http.ResponseWriter, r *http.Request) {
	rootDomain := r.URL.Query().Get("root_domain")
	prefix := r.URL.Query().Get("prefix")

	result, err := a.permissionService.CheckPublicEmailAvailability(r.Context(), rootDomain, prefix)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
