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

// version is injected at build time via -ldflags. It falls back to dev during local development.
var version = "dev"

// main loads configuration, wires dependencies, and starts the backend HTTP server.
func main() {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	store, err := sqlite.NewStore(cfg.SQLite.Path)
	if err != nil {
		log.Fatalf("open sqlite store: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate sqlite store: %v", err)
	}

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
	adminService := service.NewAdminService(cfg, store, cloudflareClient)
	permissionService := service.NewPermissionService(cfg, store, cloudflareClient)

	if err := domainService.EnsureDefaultManagedDomain(ctx); err != nil {
		log.Fatalf("bootstrap default managed domain: %v", err)
	}

	handler := httpapi.NewRouter(httpapi.RouterDependencies{
		Config:            cfg,
		Version:           version,
		AuthService:       authService,
		DomainService:     domainService,
		AdminService:      adminService,
		PermissionService: permissionService,
	})

	server := &http.Server{
		Addr:         cfg.App.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.App.ReadTimeout,
		WriteTimeout: cfg.App.WriteTimeout,
		IdleTimeout:  cfg.App.IdleTimeout,
	}

	serverErrors := make(chan error, 1)
	go func() {
		log.Printf("linuxdospace backend listening on %s", cfg.App.Addr)
		serverErrors <- server.ListenAndServe()
	}()

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
