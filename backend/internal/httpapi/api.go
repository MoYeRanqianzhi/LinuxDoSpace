package httpapi

import (
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
)

// API groups the service dependencies required by the HTTP layer.
type API struct {
	config               config.Config
	version              string
	authService          *service.AuthService
	domainService        *service.DomainService
	adminService         *service.AdminService
	adminPasswordLimiter *adminPasswordLimiter
}
