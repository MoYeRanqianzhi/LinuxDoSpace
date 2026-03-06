package httpapi

import (
	"net/http"
	"strings"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
)

// RouterDependencies 汇总构造 HTTP 路由所需的基础依赖。
type RouterDependencies struct {
	Config        config.Config
	Version       string
	AuthService   *service.AuthService
	DomainService *service.DomainService
}

// NewRouter 创建 LinuxDoSpace 后端的完整 HTTP 路由。
func NewRouter(deps RouterDependencies) http.Handler {
	api := &API{
		config:        deps.Config,
		version:       deps.Version,
		authService:   deps.AuthService,
		domainService: deps.DomainService,
	}

	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", api.handleHealth)
	mux.HandleFunc("GET /v1/public/domains", api.handlePublicDomains)
	mux.HandleFunc("GET /v1/public/allocations/check", api.handleAllocationAvailability)
	mux.HandleFunc("GET /v1/auth/login", api.handleAuthLogin)
	mux.HandleFunc("GET /v1/auth/callback", api.handleAuthCallback)
	mux.HandleFunc("POST /v1/auth/logout", api.handleAuthLogout)
	mux.HandleFunc("GET /v1/me", api.handleMe)
	mux.HandleFunc("GET /v1/my/allocations", api.handleMyAllocations)
	mux.HandleFunc("POST /v1/my/allocations", api.handleCreateAllocation)
	mux.HandleFunc("GET /v1/my/allocations/{allocationID}/records", api.handleAllocationRecords)
	mux.HandleFunc("POST /v1/my/allocations/{allocationID}/records", api.handleCreateRecord)
	mux.HandleFunc("PATCH /v1/my/allocations/{allocationID}/records/{recordID}", api.handleUpdateRecord)
	mux.HandleFunc("DELETE /v1/my/allocations/{allocationID}/records/{recordID}", api.handleDeleteRecord)
	mux.HandleFunc("GET /v1/admin/domains", api.handleAdminDomains)
	mux.HandleFunc("POST /v1/admin/domains", api.handleAdminUpsertDomain)
	mux.HandleFunc("POST /v1/admin/quotas", api.handleAdminSetQuota)

	return withCORS(deps.Config.App.AllowedOrigins, mux)
}

// withCORS 为浏览器调用提供最小可用的跨域支持。
func withCORS(allowedOrigins []string, next http.Handler) http.Handler {
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		originSet[trimmed] = struct{}{}
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if _, allowed := originSet[origin]; allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Vary", "Origin")
			}
		}

		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
