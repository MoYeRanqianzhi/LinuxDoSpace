package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/httpapi"
	"linuxdospace/backend/internal/linuxdo"
	"linuxdospace/backend/internal/linuxdocredit"
	"linuxdospace/backend/internal/mailrelay"
	"linuxdospace/backend/internal/service"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/postgres"
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

	store, err := openStore(cfg)
	if err != nil {
		log.Fatalf("open storage backend: %v", err)
	}
	defer store.Close()

	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate storage backend: %v", err)
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
	tokenHub := mailrelay.NewTokenStreamHub()
	tokenService := service.NewTokenService(store, tokenHub)
	quantityService := service.NewQuantityService(store)
	powService := service.NewPOWService(cfg, store)
	creditClient := linuxdocredit.NewClient(
		cfg.LinuxDOCredit.PID,
		cfg.LinuxDOCredit.Key,
		cfg.LinuxDOCredit.BaseURL,
		cfg.LinuxDOCredit.NotifyURL,
		cfg.LinuxDOCredit.ReturnURL,
		cfg.LinuxDOCredit.Timeout,
	)
	paymentService := service.NewPaymentService(cfg, store, cloudflareClient, creditClient)

	if err := domainService.EnsureDefaultManagedDomain(ctx); err != nil {
		log.Fatalf("bootstrap default managed domain: %v", err)
	}
	if err := domainService.EnsureBuiltInManagedDomains(ctx); err != nil {
		log.Fatalf("bootstrap built-in managed domains: %v", err)
	}
	bootstrapWarnings := make([]string, 0, 4)
	if err := service.EnsureDatabaseRelayIngressDNSState(ctx, cfg, store, cloudflareClient); err != nil {
		var warning *service.DatabaseRelayIngressDNSBootstrapWarning
		if errors.As(err, &warning) {
			bootstrapWarnings = append(bootstrapWarnings, warning.Warnings...)
			log.Printf("bootstrap database mail relay ingress dns warning: %v", warning)
		} else {
			log.Fatalf("bootstrap database mail relay ingress dns: %v", err)
		}
	}

	handler := httpapi.NewRouter(httpapi.RouterDependencies{
		Config:            cfg,
		Version:           version,
		StartupWarnings:   bootstrapWarnings,
		Store:             store,
		AuthService:       authService,
		DomainService:     domainService,
		AdminService:      adminService,
		PermissionService: permissionService,
		TokenService:      tokenService,
		QuantityService:   quantityService,
		PaymentService:    paymentService,
		POWService:        powService,
	})

	server := &http.Server{
		Addr:         cfg.App.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.App.ReadTimeout,
		WriteTimeout: cfg.App.WriteTimeout,
		IdleTimeout:  cfg.App.IdleTimeout,
	}

	type runtimeServerError struct {
		name string
		err  error
	}

	serverErrors := make(chan runtimeServerError, 2)
	go func() {
		log.Printf("linuxdospace backend listening on %s", cfg.App.Addr)
		serverErrors <- runtimeServerError{name: "http", err: server.ListenAndServe()}
	}()

	var smtpServer *mailrelay.Server
	if cfg.UsesDatabaseMailRelay() && cfg.Mail.RelayEnabled {
		dispatcher := mailrelay.NewDispatcher(
			cfg.Mail,
			store,
			mailrelay.NewSMTPForwarder(cfg.Mail),
			log.Default(),
		)
		smtpServer = mailrelay.NewServer(
			cfg.Mail,
			mailrelay.NewDBResolver(store),
			store,
			tokenHub,
			log.Default(),
		)
		go dispatcher.Run(ctx)
		go func() {
			log.Printf("linuxdospace mail relay listening on %s", cfg.Mail.SMTPAddr)
			serverErrors <- runtimeServerError{name: "smtp", err: smtpServer.ListenAndServe()}
		}()
	}

	var runtimeErr error
	select {
	case <-ctx.Done():
		log.Printf("shutdown signal received")
	case serverError := <-serverErrors:
		if serverError.err != nil && serverError.err != http.ErrServerClosed {
			runtimeErr = fmt.Errorf("%s server failed: %w", serverError.name, serverError.err)
		}
	}
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown http server: %v", err)
	}
	if smtpServer != nil {
		if err := smtpServer.Shutdown(shutdownCtx); err != nil {
			log.Fatalf("shutdown smtp server: %v", err)
		}
	}
	if runtimeErr != nil {
		log.Fatalf("%v", runtimeErr)
	}
}

// openStore selects the configured storage backend and returns one migrated
// repository implementation that satisfies the service-layer contract.
func openStore(cfg config.Config) (storage.Backend, error) {
	switch cfg.Database.Driver {
	case "sqlite":
		return sqlite.NewStore(cfg.Database.SQLitePath)
	case "postgres":
		return postgres.NewStore(cfg.Database.PostgresDSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", cfg.Database.Driver)
	}
}
