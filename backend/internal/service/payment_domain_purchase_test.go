package service

import (
	"context"
	"net/url"
	"testing"
	"time"

	"linuxdospace/backend/internal/cloudflare"
	"linuxdospace/backend/internal/config"
	"linuxdospace/backend/internal/linuxdocredit"
	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
	"linuxdospace/backend/internal/storage/sqlite"
)

// fakeConfiguredLinuxDOCredit keeps payment-service tests focused on business
// rules that run before the gateway request is attempted.
type fakeConfiguredLinuxDOCredit struct{}

// Configured reports a fully enabled fake gateway client.
func (fakeConfiguredLinuxDOCredit) Configured() bool {
	return true
}

// SubmitOrder should stay unreachable in tests that reject requests before the
// gateway is contacted.
func (fakeConfiguredLinuxDOCredit) SubmitOrder(ctx context.Context, request linuxdocredit.SubmitOrderRequest) (linuxdocredit.SubmitOrderResult, error) {
	return linuxdocredit.SubmitOrderResult{}, nil
}

// QueryOrder is unused by the current unit tests.
func (fakeConfiguredLinuxDOCredit) QueryOrder(ctx context.Context, outTradeNo string) (linuxdocredit.QueryOrderResult, error) {
	return linuxdocredit.QueryOrderResult{}, nil
}

// VerifyNotification is unused by the current unit tests.
func (fakeConfiguredLinuxDOCredit) VerifyNotification(values url.Values) (linuxdocredit.Notification, error) {
	return linuxdocredit.Notification{}, nil
}

// TestListPublicProductsHidesInternalDomainPurchaseProduct verifies that the
// dedicated dynamic domain-purchase product never leaks into the public Linux
// Do Credit product list, even if an operator mistakenly enables it.
func TestListPublicProductsHidesInternalDomainPurchaseProduct(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	product, err := store.GetPaymentProduct(ctx, PaymentProductDomainAllocationPurchase)
	if err != nil {
		t.Fatalf("load internal domain purchase product: %v", err)
	}
	if _, err := store.UpsertPaymentProduct(ctx, sqlite.UpsertPaymentProductInput{
		Key:            product.Key,
		DisplayName:    product.DisplayName,
		Description:    product.Description,
		Enabled:        true,
		UnitPriceCents: product.UnitPriceCents,
		GrantQuantity:  product.GrantQuantity,
		GrantUnit:      product.GrantUnit,
		EffectType:     product.EffectType,
		SortOrder:      product.SortOrder,
	}); err != nil {
		t.Fatalf("enable internal domain purchase product: %v", err)
	}

	service := NewPaymentService(config.Config{}, store, nil, fakeConfiguredLinuxDOCredit{})
	items, err := service.ListPublicProducts(ctx)
	if err != nil {
		t.Fatalf("list public payment products: %v", err)
	}
	for _, item := range items {
		if item.Key == PaymentProductDomainAllocationPurchase {
			t.Fatalf("expected internal domain purchase product to stay hidden from public products")
		}
	}
}

// TestCreateOrderRejectsInternalDomainPurchaseProduct verifies that even if an
// operator enables the internal product row, the generic order API still
// refuses to create checkout orders without the dedicated purchase context.
func TestCreateOrderRejectsInternalDomainPurchaseProduct(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)
	user, err := store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID: 2001,
		Username:      "buyer",
		DisplayName:   "Buyer",
		AvatarURL:     "https://example.com/avatar.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	product, err := store.GetPaymentProduct(ctx, PaymentProductDomainAllocationPurchase)
	if err != nil {
		t.Fatalf("load internal domain purchase product: %v", err)
	}
	if _, err := store.UpsertPaymentProduct(ctx, sqlite.UpsertPaymentProductInput{
		Key:            product.Key,
		DisplayName:    product.DisplayName,
		Description:    product.Description,
		Enabled:        true,
		UnitPriceCents: product.UnitPriceCents,
		GrantQuantity:  product.GrantQuantity,
		GrantUnit:      product.GrantUnit,
		EffectType:     product.EffectType,
		SortOrder:      product.SortOrder,
	}); err != nil {
		t.Fatalf("enable internal domain purchase product: %v", err)
	}

	service := NewPaymentService(config.Config{
		LinuxDOCredit: config.LinuxDOCreditConfig{
			PID: "pid",
			Key: "key",
		},
	}, store, nil, fakeConfiguredLinuxDOCredit{})
	if _, err := service.CreateOrder(ctx, user, CreatePaymentOrderRequest{
		ProductKey: PaymentProductDomainAllocationPurchase,
		Units:      1,
	}); err == nil {
		t.Fatalf("expected generic payment order creation to reject the internal domain purchase product")
	} else if normalized := NormalizeError(err); normalized.StatusCode != 403 {
		t.Fatalf("expected forbidden error, got %+v", normalized)
	}
}

// TestListLiveDomainPurchaseBlockedPrefixes verifies that the Cloudflare DNS
// snapshot is reduced to the real second-level namespace labels already in use
// under the selected root domain.
func TestListLiveDomainPurchaseBlockedPrefixes(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	if _, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:         "linuxdo.space",
		CloudflareZoneID:   "zone-default",
		DefaultQuota:       1,
		AutoProvision:      true,
		IsDefault:          true,
		Enabled:            true,
		SaleEnabled:        false,
		SaleBasePriceCents: 1000,
	}); err != nil {
		t.Fatalf("upsert managed domain: %v", err)
	}

	cloudflareClient := &fakeEmailRoutingCloudflare{
		dnsRecordsByZone: map[string][]cloudflare.DNSRecord{
			"zone-default": {
				{ID: "1", Name: "linuxdo.space", Type: "TXT", Content: "root"},
				{ID: "2", Name: "mail.linuxdo.space", Type: "A", Content: "1.1.1.1"},
				{ID: "3", Name: "api.chat.linuxdo.space", Type: "CNAME", Content: "example.com"},
				{ID: "4", Name: "*.bot.linuxdo.space", Type: "TXT", Content: "wildcard"},
			},
		},
	}

	service := NewPaymentService(config.Config{
		Cloudflare: config.CloudflareConfig{APIToken: "token"},
	}, store, cloudflareClient, nil)
	blocked, err := service.listLiveDomainPurchaseBlockedPrefixes(ctx, "linuxdo.space")
	if err != nil {
		t.Fatalf("list blocked purchase prefixes: %v", err)
	}

	blockedSet := make(map[string]struct{}, len(blocked))
	for _, value := range blocked {
		blockedSet[value] = struct{}{}
	}
	for _, expected := range []string{"mail", "chat", "bot"} {
		if _, ok := blockedSet[expected]; !ok {
			t.Fatalf("expected blocked prefixes to contain %q, got %v", expected, blocked)
		}
	}
	if _, ok := blockedSet["linuxdo"]; ok {
		t.Fatalf("root-domain records should not create a fake blocked namespace: %v", blocked)
	}
}

// TestEnsureBuiltInManagedDomainsDoesNotOverrideExistingConfiguration verifies
// that restart-time bootstrapping only inserts missing domains and never
// rewrites administrator-managed pricing or enable flags.
func TestEnsureBuiltInManagedDomainsDoesNotOverrideExistingConfiguration(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	existing, err := store.UpsertManagedDomain(ctx, sqlite.UpsertManagedDomainInput{
		RootDomain:         "cifang.love",
		CloudflareZoneID:   "zone-cifang",
		DefaultQuota:       9,
		AutoProvision:      true,
		IsDefault:          false,
		Enabled:            false,
		SaleEnabled:        true,
		SaleBasePriceCents: 2500,
	})
	if err != nil {
		t.Fatalf("seed existing managed domain: %v", err)
	}

	service := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{APIToken: "token"},
	}, store, &fakeEmailRoutingCloudflare{
		zoneIDsByRoot: map[string]string{
			"openapi.best": "zone-openapi",
			"metapi.cc":    "zone-metapi",
		},
	})
	if err := service.EnsureBuiltInManagedDomains(ctx); err != nil {
		t.Fatalf("ensure built-in managed domains: %v", err)
	}

	reloaded, err := store.GetManagedDomainByRoot(ctx, existing.RootDomain)
	if err != nil {
		t.Fatalf("reload existing managed domain: %v", err)
	}
	if reloaded.DefaultQuota != existing.DefaultQuota || reloaded.AutoProvision != existing.AutoProvision || reloaded.Enabled != existing.Enabled || reloaded.SaleEnabled != existing.SaleEnabled || reloaded.SaleBasePriceCents != existing.SaleBasePriceCents {
		t.Fatalf("expected existing managed domain to stay unchanged, got %+v", reloaded)
	}
}

// TestEnsureBuiltInManagedDomainsSkipsMissingOptionalZones verifies that one
// not-yet-delegated optional sale root does not break backend startup.
func TestEnsureBuiltInManagedDomainsSkipsMissingOptionalZones(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	service := NewDomainService(config.Config{
		Cloudflare: config.CloudflareConfig{APIToken: "token"},
	}, store, &fakeEmailRoutingCloudflare{
		zoneIDsByRoot: map[string]string{},
	})
	if err := service.EnsureBuiltInManagedDomains(ctx); err != nil {
		t.Fatalf("expected missing optional built-in zones to be skipped, got %v", err)
	}
}

// TestGetMyOrderHidesOtherUsersOrders verifies that the user-facing payment API
// does not reveal whether another user's order number exists.
func TestGetMyOrderHidesOtherUsersOrders(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	owner, err := store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID: 3001,
		Username:      "owner",
		DisplayName:   "Owner",
		AvatarURL:     "https://example.com/owner.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert owner user: %v", err)
	}
	otherUser, err := store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID: 3002,
		Username:      "other",
		DisplayName:   "Other",
		AvatarURL:     "https://example.com/other.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert other user: %v", err)
	}

	service := NewPaymentService(config.Config{
		LinuxDOCredit: config.LinuxDOCreditConfig{
			PID: "pid",
			Key: "key",
		},
	}, store, nil, fakeConfiguredLinuxDOCredit{})

	order, err := service.CreateOrder(ctx, owner, CreatePaymentOrderRequest{
		ProductKey: PaymentProductTest,
		Units:      1,
	})
	if err != nil {
		t.Fatalf("create payment order: %v", err)
	}

	_, err = service.GetMyOrder(ctx, otherUser, order.OutTradeNo)
	if err == nil {
		t.Fatalf("expected foreign order lookup to fail")
	}
	if normalized := NormalizeError(err); normalized.Code != "not_found" {
		t.Fatalf("expected not_found for foreign order lookup, got %+v", normalized)
	}
}

// TestRefreshOrderMarksFulfillmentFailure verifies that a paid order whose
// local entitlement application cannot be completed is persisted as a distinct
// fulfillment failure instead of remaining forever in the misleading
// "已支付，待发放" state.
func TestRefreshOrderMarksFulfillmentFailure(t *testing.T) {
	ctx := context.Background()
	store := newAuthTestStore(t)

	user, err := store.UpsertUser(ctx, storage.UpsertUserInput{
		LinuxDOUserID: 3003,
		Username:      "buyer",
		DisplayName:   "Buyer",
		AvatarURL:     "https://example.com/buyer.png",
		TrustLevel:    2,
	})
	if err != nil {
		t.Fatalf("upsert user: %v", err)
	}

	order, err := store.CreatePaymentOrder(ctx, sqlite.CreatePaymentOrderInput{
		UserID:                   user.ID,
		ProductKey:               PaymentProductDomainAllocationPurchase,
		ProductName:              "域名购买",
		Title:                    "购买 missing.example.com",
		GatewayType:              model.PaymentGatewayLinuxDOCredit,
		OutTradeNo:               "LDCMANUALFULFILLFAIL",
		Status:                   model.PaymentOrderStatusCreated,
		Units:                    1,
		GrantQuantity:            1,
		GrantedTotal:             1,
		GrantUnit:                "allocation",
		UnitPriceCents:           1000,
		TotalPriceCents:          1000,
		EffectType:               model.PaymentEffectDomainAllocationPurchase,
		PurchaseRootDomain:       "missing.example.com",
		PurchaseMode:             DomainPurchaseModeExact,
		PurchasePrefix:           "alice",
		PurchaseNormalizedPrefix: "alice",
		PurchaseRequestedLength:  5,
	})
	if err != nil {
		t.Fatalf("create payment order: %v", err)
	}

	if _, err := store.UpdatePaymentOrderGatewayState(ctx, sqlite.UpdatePaymentOrderGatewayStateInput{
		OutTradeNo: order.OutTradeNo,
		Status:     model.PaymentOrderStatusPaid,
	}); err != nil {
		t.Fatalf("mark order paid: %v", err)
	}

	service := NewPaymentService(config.Config{}, store, nil, nil)
	refreshedOrder, err := service.RefreshOrderForAdmin(ctx, order.OutTradeNo)
	if err != nil {
		t.Fatalf("refresh order for admin: %v", err)
	}

	if refreshedOrder.FulfillmentStatus != model.PaymentOrderFulfillmentFailed {
		t.Fatalf("expected fulfillment_status failed, got %+v", refreshedOrder)
	}
	if refreshedOrder.FulfillmentFailedAt == nil {
		t.Fatalf("expected fulfillment_failed_at to be recorded, got %+v", refreshedOrder)
	}
	if refreshedOrder.AppliedAt != nil {
		t.Fatalf("expected unapplied order, got applied_at=%s", refreshedOrder.AppliedAt.Format(time.RFC3339))
	}
	if refreshedOrder.FulfillmentError == "" {
		t.Fatalf("expected fulfillment error to be persisted, got %+v", refreshedOrder)
	}
}
