package httpapi

import (
	"net/http"
	"strings"

	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
)

// RouterDependencies groups the dependencies required to build the HTTP router.
type RouterDependencies struct {
	Config            config.Config
	Version           string
	AuthService       *service.AuthService
	DomainService     *service.DomainService
	AdminService      *service.AdminService
	PermissionService *service.PermissionService
	QuantityService   *service.QuantityService
	PaymentService    *service.PaymentService
}

// NewRouter builds the complete HTTP router used by the backend process.
func NewRouter(deps RouterDependencies) http.Handler {
	api := &API{
		config:               deps.Config,
		version:              deps.Version,
		authService:          deps.AuthService,
		domainService:        deps.DomainService,
		adminService:         deps.AdminService,
		permissionService:    deps.PermissionService,
		quantityService:      deps.QuantityService,
		paymentService:       deps.PaymentService,
		adminPasswordLimiter: newAdminPasswordLimiter(adminPasswordMaxFailures, adminPasswordBlockDuration, adminPasswordStateTTL),
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
	mux.HandleFunc("GET /v1/public/email-routes/check", api.handlePublicEmailRouteAvailability)
	mux.HandleFunc("GET /v1/public/ldc/products", api.handlePublicPaymentProducts)
	mux.HandleFunc("GET /v1/auth/login", api.handleAuthLogin)
	mux.HandleFunc("GET /v1/admin/auth/login", api.handleAdminAuthLogin)
	mux.HandleFunc("GET /v1/auth/callback", api.handleAuthCallback)
	mux.HandleFunc("POST /v1/auth/logout", api.handleAuthLogout)
	mux.HandleFunc("GET /v1/payments/linuxdo-credit/notify", api.handleLinuxDOCreditNotify)
	mux.HandleFunc("GET /v1/me", api.handleMe)
	mux.HandleFunc("GET /v1/admin/me", api.handleAdminMe)
	mux.HandleFunc("POST /v1/admin/verify-password", api.handleAdminVerifyPassword)
	mux.HandleFunc("GET /v1/my/allocations", api.handleMyAllocations)
	mux.HandleFunc("GET /v1/my/permissions", api.handleMyPermissions)
	mux.HandleFunc("GET /v1/my/quantity-records", api.handleMyQuantityRecords)
	mux.HandleFunc("GET /v1/my/quantity-balances", api.handleMyQuantityBalances)
	mux.HandleFunc("GET /v1/my/ldc/orders", api.handleMyPaymentOrders)
	mux.HandleFunc("POST /v1/my/ldc/orders", api.handleCreateMyPaymentOrder)
	mux.HandleFunc("GET /v1/my/ldc/orders/{outTradeNo}", api.handleMyPaymentOrder)
	mux.HandleFunc("POST /v1/my/permissions/applications", api.handleSubmitPermissionApplication)
	mux.HandleFunc("GET /v1/my/email-targets", api.handleMyEmailTargets)
	mux.HandleFunc("POST /v1/my/email-targets", api.handleCreateMyEmailTarget)
	mux.HandleFunc("GET /v1/my/email-routes", api.handleMyEmailRoutes)
	mux.HandleFunc("PUT /v1/my/email-routes/default", api.handleUpsertDefaultEmailRoute)
	mux.HandleFunc("PUT /v1/my/email-routes/catch-all", api.handleUpsertCatchAllEmailRoute)
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
	mux.HandleFunc("GET /v1/admin/users/{userID}/permissions", api.handleAdminUserPermissions)
	mux.HandleFunc("PATCH /v1/admin/users/{userID}/permissions/{permissionKey}/access", api.handleAdminUpdateUserPermissionAccess)
	mux.HandleFunc("GET /v1/admin/users/{userID}/quantity-records", api.handleAdminUserQuantityRecords)
	mux.HandleFunc("GET /v1/admin/users/{userID}/quantity-balances", api.handleAdminUserQuantityBalances)
	mux.HandleFunc("POST /v1/admin/users/{userID}/quantity-records", api.handleAdminCreateQuantityRecord)
	mux.HandleFunc("PATCH /v1/admin/users/{userID}/permissions/{permissionKey}", api.handleAdminSetUserPermission)
	mux.HandleFunc("GET /v1/admin/allocations", api.handleAdminAllocations)
	mux.HandleFunc("POST /v1/admin/allocations", api.handleAdminCreateAllocation)
	mux.HandleFunc("PATCH /v1/admin/allocations/{allocationID}", api.handleAdminUpdateAllocation)
	mux.HandleFunc("GET /v1/admin/records", api.handleAdminRecords)
	mux.HandleFunc("POST /v1/admin/allocations/{allocationID}/records", api.handleAdminCreateRecord)
	mux.HandleFunc("PATCH /v1/admin/allocations/{allocationID}/records/{recordID}", api.handleAdminUpdateRecord)
	mux.HandleFunc("DELETE /v1/admin/allocations/{allocationID}/records/{recordID}", api.handleAdminDeleteRecord)
	mux.HandleFunc("GET /v1/admin/email-routes", api.handleAdminEmailRoutes)
	mux.HandleFunc("POST /v1/admin/email-routes", api.handleAdminCreateEmailRoute)
	mux.HandleFunc("PATCH /v1/admin/email-routes/{routeID}", api.handleAdminUpdateEmailRoute)
	mux.HandleFunc("DELETE /v1/admin/email-routes/{routeID}", api.handleAdminDeleteEmailRoute)
	mux.HandleFunc("GET /v1/admin/applications", api.handleAdminApplications)
	mux.HandleFunc("GET /v1/admin/permission-policies", api.handleAdminPermissionPolicies)
	mux.HandleFunc("GET /v1/admin/ldc/products", api.handleAdminPaymentProducts)
	mux.HandleFunc("GET /v1/admin/ldc/orders", api.handleAdminPaymentOrders)
	mux.HandleFunc("GET /v1/admin/ldc/orders/{outTradeNo}", api.handleAdminPaymentOrder)
	mux.HandleFunc("PATCH /v1/admin/ldc/products/{productKey}", api.handleAdminUpdatePaymentProduct)
	mux.HandleFunc("PATCH /v1/admin/permission-policies/{policyKey}", api.handleAdminUpdatePermissionPolicy)
	mux.HandleFunc("PATCH /v1/admin/applications/{applicationID}", api.handleAdminUpdateApplication)
	mux.HandleFunc("GET /v1/admin/redeem-codes", api.handleAdminRedeemCodes)
	mux.HandleFunc("POST /v1/admin/redeem-codes/batch", api.handleAdminGenerateRedeemCodes)
	mux.HandleFunc("DELETE /v1/admin/redeem-codes/{redeemCodeID}", api.handleAdminDeleteRedeemCode)
	mux.HandleFunc("/v1/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, service.NotFoundError("api endpoint not found"))
	})
	mux.Handle("/", spaHandler)

	return withCORS(deps.Config, mux)
}

// withCORS provides the minimum browser cross-origin support required by the frontends.
func withCORS(cfg config.Config, next http.Handler) http.Handler {
	allowedOrigins := cfg.App.AllowedOrigins
	originSet := make(map[string]struct{}, len(allowedOrigins))
	for _, origin := range allowedOrigins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		originSet[trimmed] = struct{}{}
	}
	adminOrigin := strings.TrimRight(strings.TrimSpace(cfg.App.AdminFrontendURL), "/")

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			_, allowed := originSet[origin]
			if strings.HasPrefix(r.URL.Path, "/v1/admin/") {
				allowed = allowed && adminOrigin != "" && origin == adminOrigin
			}
			if allowed {
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
