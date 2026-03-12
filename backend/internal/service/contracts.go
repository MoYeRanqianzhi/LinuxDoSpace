package service

import (
	"context"
	"net/url"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/linuxdo"
	"linuxdospace/backend/internal/linuxdocredit"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

// Store aliases the storage-layer contract so the service package can keep the
// existing constructor signatures while the concrete backend becomes pluggable.
type Store = storage.Store

// OAuthClient abstracts Linux Do OAuth operations.
type OAuthClient interface {
	Configured() bool
	BuildAuthorizationURL(state string, codeChallenge string) string
	ExchangeCode(ctx context.Context, code string, codeVerifier string) (linuxdo.TokenResponse, error)
	GetCurrentUser(ctx context.Context, accessToken string) (model.LinuxDOProfile, error)
}

// CloudflareClient abstracts Cloudflare DNS and Email Routing operations.
type CloudflareClient interface {
	ResolveZone(ctx context.Context, rootDomain string) (cloudflare.Zone, error)
	GetZone(ctx context.Context, zoneID string) (cloudflare.Zone, error)
	ResolveZoneID(ctx context.Context, rootDomain string) (string, error)
	ListAllDNSRecords(ctx context.Context, zoneID string) ([]cloudflare.DNSRecord, error)
	GetDNSRecord(ctx context.Context, zoneID string, recordID string) (cloudflare.DNSRecord, error)
	CreateDNSRecord(ctx context.Context, zoneID string, input cloudflare.CreateDNSRecordInput) (cloudflare.DNSRecord, error)
	UpdateDNSRecord(ctx context.Context, zoneID string, recordID string, input cloudflare.UpdateDNSRecordInput) (cloudflare.DNSRecord, error)
	DeleteDNSRecord(ctx context.Context, zoneID string, recordID string) error
	ListEmailRoutingDestinationAddresses(ctx context.Context, accountID string) ([]cloudflare.EmailRoutingDestinationAddress, error)
	CreateEmailRoutingDestinationAddress(ctx context.Context, accountID string, email string) (cloudflare.EmailRoutingDestinationAddress, error)
	EnableEmailRoutingDNS(ctx context.Context, zoneID string) ([]cloudflare.EmailRoutingDNSRecord, error)
	ListEmailRoutingDNSRecords(ctx context.Context, zoneID string, subdomain string) ([]cloudflare.EmailRoutingDNSRecord, error)
	ListEmailRoutingRules(ctx context.Context, zoneID string) ([]cloudflare.EmailRoutingRule, error)
	CreateEmailRoutingRule(ctx context.Context, zoneID string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error)
	UpdateEmailRoutingRule(ctx context.Context, zoneID string, ruleIdentifier string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error)
	DeleteEmailRoutingRule(ctx context.Context, zoneID string, ruleIdentifier string) error
	GetEmailRoutingCatchAllRule(ctx context.Context, zoneID string, subdomain string) (cloudflare.EmailRoutingRule, error)
	UpdateEmailRoutingCatchAllRule(ctx context.Context, zoneID string, subdomain string, input cloudflare.UpsertEmailRoutingRuleInput) (cloudflare.EmailRoutingRule, error)
}

// LinuxDOCreditClient abstracts the EasyPay-compatible Linux Do Credit gateway.
type LinuxDOCreditClient interface {
	Configured() bool
	SubmitOrder(ctx context.Context, request linuxdocredit.SubmitOrderRequest) (linuxdocredit.SubmitOrderResult, error)
	QueryOrder(ctx context.Context, outTradeNo string) (linuxdocredit.QueryOrderResult, error)
	VerifyNotification(values url.Values) (linuxdocredit.Notification, error)
}
