package httpapi

import (
	"net/http"
	"strings"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
)

// RouterDependencies groups the dependencies required to build the HTTP router.
type RouterDependencies struct {
	Config        config.Config
	Version       string
	AuthService   *service.AuthService
	DomainService *service.DomainService
	AdminService  *service.AdminService
}

// NewRouter builds the complete HTTP router used by the backend process.
func NewRouter(deps RouterDependencies) http.Handler {
	api := &API{
		config:        deps.Config,
		version:       deps.Version,
		authService:   deps.AuthService,
		domainService: deps.DomainService,
		adminService:  deps.AdminService,
	}

	mux := http.NewServeMux()
	spaHandler, err := newSPAHandler()
	if err != nil {
		panic(err)
	}

	mux.HandleFunc("GET /healthz", api.handleHealth)
	mux.HandleFunc("GET /v1/public/domains", api.handlePublicDomains)
	mux.HandleFunc("GET /v1/public/supervision", api.handlePublicSupervision)
	mux.HandleFunc("GET /v1/public/allocations/check", api.handleAllocationAvailability)
	mux.HandleFunc("GET /v1/auth/login", api.handleAuthLogin)
	mux.HandleFunc("GET /v1/admin/auth/login", api.handleAdminAuthLogin)
	mux.HandleFunc("GET /v1/auth/callback", api.handleAuthCallback)
	mux.HandleFunc("POST /v1/auth/logout", api.handleAuthLogout)
	mux.HandleFunc("GET /v1/me", api.handleMe)
	mux.HandleFunc("GET /v1/admin/me", api.handleAdminMe)
	mux.HandleFunc("POST /v1/admin/verify-password", api.handleAdminVerifyPassword)
	mux.HandleFunc("GET /v1/my/allocations", api.handleMyAllocations)
	mux.HandleFunc("POST /v1/my/allocations", api.handleCreateAllocation)
	mux.HandleFunc("GET /v1/my/allocations/{allocationID}/records", api.handleAllocationRecords)
	mux.HandleFunc("POST /v1/my/allocations/{allocationID}/records", api.handleCreateRecord)
	mux.HandleFunc("PATCH /v1/my/allocations/{allocationID}/records/{recordID}", api.handleUpdateRecord)
	mux.HandleFunc("DELETE /v1/my/allocations/{allocationID}/records/{recordID}", api.handleDeleteRecord)
	mux.HandleFunc("GET /v1/admin/domains", api.handleAdminDomains)
	mux.HandleFunc("POST /v1/admin/domains", api.handleAdminUpsertDomain)
	mux.HandleFunc("POST /v1/admin/quotas", api.handleAdminSetQuota)
	mux.HandleFunc("GET /v1/admin/users", api.handleAdminUsers)
	mux.HandleFunc("GET /v1/admin/users/{userID}", api.handleAdminUserDetail)
	mux.HandleFunc("PATCH /v1/admin/users/{userID}", api.handleAdminUpdateUser)
	mux.HandleFunc("GET /v1/admin/allocations", api.handleAdminAllocations)
	mux.HandleFunc("GET /v1/admin/records", api.handleAdminRecords)
	mux.HandleFunc("POST /v1/admin/allocations/{allocationID}/records", api.handleAdminCreateRecord)
	mux.HandleFunc("PATCH /v1/admin/allocations/{allocationID}/records/{recordID}", api.handleAdminUpdateRecord)
	mux.HandleFunc("DELETE /v1/admin/allocations/{allocationID}/records/{recordID}", api.handleAdminDeleteRecord)
	mux.HandleFunc("GET /v1/admin/email-routes", api.handleAdminEmailRoutes)
	mux.HandleFunc("POST /v1/admin/email-routes", api.handleAdminCreateEmailRoute)
	mux.HandleFunc("PATCH /v1/admin/email-routes/{routeID}", api.handleAdminUpdateEmailRoute)
	mux.HandleFunc("DELETE /v1/admin/email-routes/{routeID}", api.handleAdminDeleteEmailRoute)
	mux.HandleFunc("GET /v1/admin/applications", api.handleAdminApplications)
	mux.HandleFunc("PATCH /v1/admin/applications/{applicationID}", api.handleAdminUpdateApplication)
	mux.HandleFunc("GET /v1/admin/redeem-codes", api.handleAdminRedeemCodes)
	mux.HandleFunc("POST /v1/admin/redeem-codes/batch", api.handleAdminGenerateRedeemCodes)
	mux.HandleFunc("DELETE /v1/admin/redeem-codes/{redeemCodeID}", api.handleAdminDeleteRedeemCode)
	mux.Handle("/", spaHandler)

	return withCORS(deps.Config.App.AllowedOrigins, mux)
}

// withCORS provides the minimum browser cross-origin support required by the frontends.
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
