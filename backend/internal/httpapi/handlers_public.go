package httpapi

import (
	"net/http"
	"time"
)

// handleHealth 返回服务健康状态。
func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                  "ok",
		"app":                     a.config.App.Name,
		"version":                 a.version,
		"env":                     a.config.App.Env,
		"oauth_ready":             a.config.OAuthConfigured(),
		"cf_ready":                a.config.CloudflareConfigured(),
		"mail_forwarding_backend": a.config.Mail.ForwardingBackend,
		"mail_relay_enabled":      a.config.Mail.RelayEnabled,
		"time":                    time.Now().UTC(),
	})
}

// handlePublicDomains 返回当前可公开申请的根域名。
func (a *API) handlePublicDomains(w http.ResponseWriter, r *http.Request) {
	items, err := a.domainService.ListPublicDomains(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
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

	result, err := a.domainService.CheckAvailability(r.Context(), rootDomain, prefix)
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
