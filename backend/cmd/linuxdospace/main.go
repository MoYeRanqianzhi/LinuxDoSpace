package main

import (
	"context"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/httpapi"
	"linuxdospace/backend/internal/linuxdo"
	"linuxdospace/backend/internal/service"
	"linuxdospace/backend/internal/storage/sqlite"
)

// version 用于在构建时注入版本号。
// 如果没有通过 `-ldflags` 注入，这里会保持为 `dev`。
var version = "dev"

// main 是后端服务的入口函数。
// 它负责加载配置、初始化数据库、构造 HTTP 路由并优雅地启动和关闭服务。
func main() {
	// 使用可取消的根上下文来统一管理启动期和关闭期的资源。
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 从环境变量加载配置，并在必要时为开发环境生成临时会话密钥。
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	// 打开 SQLite 数据库连接，并在服务启动前执行迁移。
	store, err := sqlite.NewStore(cfg.SQLite.Path)
	if err != nil {
		log.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate sqlite store: %v", err)
	}

	// 按配置构造第三方客户端；当某项配置缺失时，对应能力会自动降级为不可用。
	var cloudflareClient *cloudflare.Client
	if cfg.CloudflareConfigured() {
		cloudflareClient = cloudflare.NewClient(cfg.Cloudflare.APIToken)
	}

	oauthClient := linuxdo.NewClient(
		cfg.LinuxDO.ClientID,
		cfg.LinuxDO.ClientSecret,
		cfg.LinuxDO.RedirectURL,
		cfg.LinuxDO.AuthorizeURL,
		cfg.LinuxDO.TokenURL,
		cfg.LinuxDO.UserInfoURL,
		cfg.LinuxDO.Scope,
		cfg.LinuxDO.EnablePKCE,
	)

	authService := service.NewAuthService(cfg, store, oauthClient)
	domainService := service.NewDomainService(cfg, store, cloudflareClient)

	if err := domainService.EnsureDefaultManagedDomain(ctx); err != nil {
		log.Fatalf("bootstrap default managed domain: %v", err)
	}

	// 使用完整业务依赖构造 HTTP 路由。
	handler := httpapi.NewRouter(httpapi.RouterDependencies{
		Config:        cfg,
		Version:       version,
		AuthService:   authService,
		DomainService: domainService,
	})

	// 构造标准库 HTTP Server，显式设置超时，避免慢连接耗尽资源。
	server := &http.Server{
		Addr:         cfg.App.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.App.ReadTimeout,
		WriteTimeout: cfg.App.WriteTimeout,
		IdleTimeout:  cfg.App.IdleTimeout,
	}

	// 在单独协程中启动 HTTP 服务，以便主协程继续等待退出信号。
	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("linuxdospace backend listening on %s", cfg.App.Addr)
		serverErrors <- server.ListenAndServe()
	}()

	// 等待退出信号或者监听错误，然后进行统一的优雅关闭。
	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("http server failed: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown http server: %v", err)
	}
}
