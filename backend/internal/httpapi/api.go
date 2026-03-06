package httpapi

import (
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/service"
)

// API 汇总 HTTP 层需要使用的所有依赖对象。
type API struct {
	config        config.Config
	version       string
	authService   *service.AuthService
	domainService *service.DomainService
}
