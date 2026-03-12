package model

import "time"

const (
	// PaymentGatewayLinuxDOCredit is the stable gateway identifier stored on
	// every locally tracked LDC order.
	PaymentGatewayLinuxDOCredit = "linuxdo_credit"

	// PaymentOrderStatusCreated marks a local order row that has been reserved
	// but has not yet produced a usable payment URL.
	PaymentOrderStatusCreated = "created"

	// PaymentOrderStatusPending marks an order that already has a checkout URL
	// and is waiting for the upstream gateway to confirm payment success.
	PaymentOrderStatusPending = "pending"

	// PaymentOrderStatusPaid marks an order whose upstream payment has been
	// confirmed. The order may or may not already have its entitlements applied.
	PaymentOrderStatusPaid = "paid"

	// PaymentOrderStatusFailed marks an order that could not enter checkout
	// successfully or was later marked failed by an operator.
	PaymentOrderStatusFailed = "failed"

	// PaymentOrderStatusRefunded marks an order that has been fully refunded.
	PaymentOrderStatusRefunded = "refunded"

	// PaymentEffectEmailCatchAllSubscriptionDays extends the paid catch-all
	// subscription expiry by a configured number of UTC-based days.
	PaymentEffectEmailCatchAllSubscriptionDays = "email_catch_all_subscription_days"

	// PaymentEffectEmailCatchAllRemainingCount adds prepaid remaining message
	// count to the user's catch-all mailbox runtime access state.
	PaymentEffectEmailCatchAllRemainingCount = "email_catch_all_remaining_count"

	// PaymentEffectPaymentTestCounter records one successful payment test so the
	// integration can be verified without changing production entitlements.
	PaymentEffectPaymentTestCounter = "payment_test_counter"
)

// PaymentProduct stores one administrator-configurable purchasable LDC item.
// The effect type stays stable, while pricing and grant quantities remain
// operator-editable.
type PaymentProduct struct {
	Key            string    `json:"key"`
	DisplayName    string    `json:"display_name"`
	Description    string    `json:"description"`
	Enabled        bool      `json:"enabled"`
	UnitPriceCents int64     `json:"unit_price_cents"`
	GrantQuantity  int64     `json:"grant_quantity"`
	GrantUnit      string    `json:"grant_unit"`
	EffectType     string    `json:"effect_type"`
	SortOrder      int       `json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// PaymentOrder stores the local source of truth for one Linux Do Credit
// checkout and its entitlement-application lifecycle.
type PaymentOrder struct {
	ID               int64      `json:"id"`
	UserID           int64      `json:"user_id"`
	Username         string     `json:"username"`
	DisplayName      string     `json:"display_name"`
	ProductKey       string     `json:"product_key"`
	ProductName      string     `json:"product_name"`
	Title            string     `json:"title"`
	GatewayType      string     `json:"gateway_type"`
	OutTradeNo       string     `json:"out_trade_no"`
	ProviderTradeNo  string     `json:"provider_trade_no"`
	Status           string     `json:"status"`
	Units            int64      `json:"units"`
	GrantQuantity    int64      `json:"grant_quantity"`
	GrantedTotal     int64      `json:"granted_total"`
	GrantUnit        string     `json:"grant_unit"`
	UnitPriceCents   int64      `json:"unit_price_cents"`
	TotalPriceCents  int64      `json:"total_price_cents"`
	EffectType       string     `json:"effect_type"`
	PaymentURL       string     `json:"payment_url"`
	NotifyPayloadRaw string     `json:"-"`
	PaidAt           *time.Time `json:"paid_at,omitempty"`
	AppliedAt        *time.Time `json:"applied_at,omitempty"`
	LastCheckedAt    *time.Time `json:"last_checked_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}
