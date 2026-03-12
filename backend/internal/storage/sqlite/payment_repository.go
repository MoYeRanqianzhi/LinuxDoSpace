package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
	"linuxdospace/backend/internal/storage"
)

type UpsertPaymentProductInput = storage.UpsertPaymentProductInput
type CreatePaymentOrderInput = storage.CreatePaymentOrderInput
type UpdatePaymentOrderGatewayStateInput = storage.UpdatePaymentOrderGatewayStateInput
type ApplyPaymentOrderEntitlementInput = storage.ApplyPaymentOrderEntitlementInput

const (
	paymentQuantityResourceSubscriptionDays = "email_catch_all_subscription_days"
	paymentQuantityResourceRemainingCount   = "email_catch_all_remaining_count"
	paymentQuantityResourceTestCount        = "payment_test_count"
	paymentQuantityScopeCatchAll            = "email_catch_all"
	paymentQuantityScopeGateway             = "linuxdo_credit"
	paymentQuantitySourceGateway            = "linuxdo_credit"
	paymentQuantityReferenceTypeOrder       = "payment_order"
)

// ListPaymentProducts returns Linux Do Credit products in stable display order.
func (s *Store) ListPaymentProducts(ctx context.Context, includeDisabled bool) ([]model.PaymentProduct, error) {
	query := `
SELECT
    key,
    display_name,
    description,
    enabled,
    unit_price_cents,
    grant_quantity,
    grant_unit,
    effect_type,
    sort_order,
    created_at,
    updated_at
FROM payment_products
`
	args := []any{}
	if !includeDisabled {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY sort_order ASC, key ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.PaymentProduct, 0, 4)
	for rows.Next() {
		item, scanErr := scanPaymentProduct(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// GetPaymentProduct loads one stable product row by key.
func (s *Store) GetPaymentProduct(ctx context.Context, key string) (model.PaymentProduct, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    key,
    display_name,
    description,
    enabled,
    unit_price_cents,
    grant_quantity,
    grant_unit,
    effect_type,
    sort_order,
    created_at,
    updated_at
FROM payment_products
WHERE key = ?
`, strings.TrimSpace(key))
	return scanPaymentProduct(row)
}

// UpsertPaymentProduct inserts or updates one administrator-managed product row.
func (s *Store) UpsertPaymentProduct(ctx context.Context, input UpsertPaymentProductInput) (model.PaymentProduct, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO payment_products (
    key,
    display_name,
    description,
    enabled,
    unit_price_cents,
    grant_quantity,
    grant_unit,
    effect_type,
    sort_order,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    display_name=excluded.display_name,
    description=excluded.description,
    enabled=excluded.enabled,
    unit_price_cents=excluded.unit_price_cents,
    grant_quantity=excluded.grant_quantity,
    grant_unit=excluded.grant_unit,
    effect_type=excluded.effect_type,
    sort_order=excluded.sort_order,
    updated_at=excluded.updated_at
RETURNING
    key,
    display_name,
    description,
    enabled,
    unit_price_cents,
    grant_quantity,
    grant_unit,
    effect_type,
    sort_order,
    created_at,
    updated_at
`,
		strings.TrimSpace(input.Key),
		strings.TrimSpace(input.DisplayName),
		strings.TrimSpace(input.Description),
		boolToInt(input.Enabled),
		input.UnitPriceCents,
		input.GrantQuantity,
		strings.TrimSpace(input.GrantUnit),
		strings.TrimSpace(input.EffectType),
		input.SortOrder,
		formatTime(now),
		formatTime(now),
	)
	return scanPaymentProduct(row)
}

// CreatePaymentOrder reserves one local order row before the backend talks to
// the external Linux Do Credit gateway.
func (s *Store) CreatePaymentOrder(ctx context.Context, input CreatePaymentOrderInput) (model.PaymentOrder, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO payment_orders (
    user_id,
    product_key,
    product_name,
    title,
    gateway_type,
    out_trade_no,
    provider_trade_no,
    status,
    units,
    grant_quantity,
    granted_total,
    grant_unit,
    unit_price_cents,
    total_price_cents,
    effect_type,
    payment_url,
    notify_payload_raw,
    paid_at,
    applied_at,
    last_checked_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', NULL, NULL, NULL, ?, ?)
RETURNING id
`,
		input.UserID,
		strings.TrimSpace(input.ProductKey),
		strings.TrimSpace(input.ProductName),
		strings.TrimSpace(input.Title),
		strings.TrimSpace(input.GatewayType),
		strings.TrimSpace(input.OutTradeNo),
		"",
		strings.TrimSpace(input.Status),
		input.Units,
		input.GrantQuantity,
		input.GrantedTotal,
		strings.TrimSpace(input.GrantUnit),
		input.UnitPriceCents,
		input.TotalPriceCents,
		strings.TrimSpace(input.EffectType),
		strings.TrimSpace(input.PaymentURL),
		formatTime(now),
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.PaymentOrder{}, err
	}
	return s.getPaymentOrderByID(ctx, id)
}

// ListPaymentOrdersByUser returns one user's most recent orders first.
func (s *Store) ListPaymentOrdersByUser(ctx context.Context, userID int64, limit int) ([]model.PaymentOrder, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.db.QueryContext(ctx, `
SELECT
    po.id,
    po.user_id,
    u.username,
    u.display_name,
    po.product_key,
    po.product_name,
    po.title,
    po.gateway_type,
    po.out_trade_no,
    po.provider_trade_no,
    po.status,
    po.units,
    po.grant_quantity,
    po.granted_total,
    po.grant_unit,
    po.unit_price_cents,
    po.total_price_cents,
    po.effect_type,
    po.payment_url,
    po.notify_payload_raw,
    po.paid_at,
    po.applied_at,
    po.last_checked_at,
    po.created_at,
    po.updated_at
FROM payment_orders po
INNER JOIN users u ON u.id = po.user_id
WHERE po.user_id = ?
ORDER BY po.created_at DESC, po.id DESC
LIMIT ?
`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.PaymentOrder, 0, limit)
	for rows.Next() {
		item, scanErr := scanPaymentOrder(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// GetPaymentOrderByOutTradeNo loads one local order by its business order
// number so query polling and callback handlers can share the same key.
func (s *Store) GetPaymentOrderByOutTradeNo(ctx context.Context, outTradeNo string) (model.PaymentOrder, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    po.id,
    po.user_id,
    u.username,
    u.display_name,
    po.product_key,
    po.product_name,
    po.title,
    po.gateway_type,
    po.out_trade_no,
    po.provider_trade_no,
    po.status,
    po.units,
    po.grant_quantity,
    po.granted_total,
    po.grant_unit,
    po.unit_price_cents,
    po.total_price_cents,
    po.effect_type,
    po.payment_url,
    po.notify_payload_raw,
    po.paid_at,
    po.applied_at,
    po.last_checked_at,
    po.created_at,
    po.updated_at
FROM payment_orders po
INNER JOIN users u ON u.id = po.user_id
WHERE po.out_trade_no = ?
`, strings.TrimSpace(outTradeNo))
	return scanPaymentOrder(row)
}

// UpdatePaymentOrderGatewayState persists gateway-facing mutable fields such as
// checkout URL, upstream trade ID, paid timestamp, and the latest callback
// payload snapshot.
func (s *Store) UpdatePaymentOrderGatewayState(ctx context.Context, input UpdatePaymentOrderGatewayStateInput) (model.PaymentOrder, error) {
	row := s.db.QueryRowContext(ctx, `
UPDATE payment_orders
SET
    status = ?,
    provider_trade_no = COALESCE(NULLIF(?, ''), provider_trade_no),
    payment_url = COALESCE(NULLIF(?, ''), payment_url),
    notify_payload_raw = COALESCE(NULLIF(?, ''), notify_payload_raw),
    paid_at = COALESCE(?, paid_at),
    last_checked_at = COALESCE(?, last_checked_at),
    updated_at = ?
WHERE out_trade_no = ?
RETURNING id
`,
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.ProviderTradeNo),
		strings.TrimSpace(input.PaymentURL),
		strings.TrimSpace(input.NotifyPayloadRaw),
		formatNullableTime(input.PaidAt),
		formatNullableTime(input.LastCheckedAt),
		formatTime(time.Now().UTC()),
		strings.TrimSpace(input.OutTradeNo),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.PaymentOrder{}, err
	}
	return s.getPaymentOrderByID(ctx, id)
}

// ApplyPaymentOrderEntitlement idempotently turns one paid order into actual
// entitlements plus immutable quantity-ledger entries inside one transaction.
func (s *Store) ApplyPaymentOrderEntitlement(ctx context.Context, input ApplyPaymentOrderEntitlementInput) (model.PaymentOrder, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.PaymentOrder{}, false, err
	}
	defer tx.Rollback()

	order, err := getPaymentOrderByOutTradeNoTx(ctx, tx, input.OutTradeNo)
	if err != nil {
		return model.PaymentOrder{}, false, err
	}
	if order.AppliedAt != nil {
		if err := tx.Commit(); err != nil {
			return model.PaymentOrder{}, false, err
		}
		return order, false, nil
	}
	if order.Status != model.PaymentOrderStatusPaid {
		return model.PaymentOrder{}, false, fmt.Errorf("payment order %s is not paid", order.OutTradeNo)
	}

	switch order.EffectType {
	case model.PaymentEffectEmailCatchAllSubscriptionDays:
		if err := applyCatchAllSubscriptionDaysTx(ctx, tx, order, input.AppliedAt.UTC()); err != nil {
			return model.PaymentOrder{}, false, err
		}
	case model.PaymentEffectEmailCatchAllRemainingCount:
		if err := applyCatchAllRemainingCountTx(ctx, tx, order, input.AppliedAt.UTC()); err != nil {
			return model.PaymentOrder{}, false, err
		}
	case model.PaymentEffectPaymentTestCounter:
		if err := insertPaymentQuantityRecordTx(ctx, tx, order.UserID, paymentQuantityResourceTestCount, paymentQuantityScopeGateway, order.GrantedTotal, order.Title, order.OutTradeNo, input.AppliedAt.UTC()); err != nil {
			return model.PaymentOrder{}, false, err
		}
	default:
		return model.PaymentOrder{}, false, fmt.Errorf("unsupported payment effect type %q", order.EffectType)
	}

	if _, err := tx.ExecContext(ctx, `
UPDATE payment_orders
SET applied_at = ?, updated_at = ?
WHERE id = ?
`, formatTime(input.AppliedAt.UTC()), formatTime(input.AppliedAt.UTC()), order.ID); err != nil {
		return model.PaymentOrder{}, false, err
	}

	finalOrder, err := getPaymentOrderByOutTradeNoTx(ctx, tx, order.OutTradeNo)
	if err != nil {
		return model.PaymentOrder{}, false, err
	}

	if err := tx.Commit(); err != nil {
		return model.PaymentOrder{}, false, err
	}
	return finalOrder, true, nil
}

// getPaymentOrderByID reloads one joined order row by local primary key.
func (s *Store) getPaymentOrderByID(ctx context.Context, id int64) (model.PaymentOrder, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    po.id,
    po.user_id,
    u.username,
    u.display_name,
    po.product_key,
    po.product_name,
    po.title,
    po.gateway_type,
    po.out_trade_no,
    po.provider_trade_no,
    po.status,
    po.units,
    po.grant_quantity,
    po.granted_total,
    po.grant_unit,
    po.unit_price_cents,
    po.total_price_cents,
    po.effect_type,
    po.payment_url,
    po.notify_payload_raw,
    po.paid_at,
    po.applied_at,
    po.last_checked_at,
    po.created_at,
    po.updated_at
FROM payment_orders po
INNER JOIN users u ON u.id = po.user_id
WHERE po.id = ?
`, id)
	return scanPaymentOrder(row)
}

// getPaymentOrderByOutTradeNoTx reloads one joined order row inside an active
// transaction so entitlement application can stay atomic.
func getPaymentOrderByOutTradeNoTx(ctx context.Context, tx *sql.Tx, outTradeNo string) (model.PaymentOrder, error) {
	row := tx.QueryRowContext(ctx, `
SELECT
    po.id,
    po.user_id,
    u.username,
    u.display_name,
    po.product_key,
    po.product_name,
    po.title,
    po.gateway_type,
    po.out_trade_no,
    po.provider_trade_no,
    po.status,
    po.units,
    po.grant_quantity,
    po.granted_total,
    po.grant_unit,
    po.unit_price_cents,
    po.total_price_cents,
    po.effect_type,
    po.payment_url,
    po.notify_payload_raw,
    po.paid_at,
    po.applied_at,
    po.last_checked_at,
    po.created_at,
    po.updated_at
FROM payment_orders po
INNER JOIN users u ON u.id = po.user_id
WHERE po.out_trade_no = ?
`, strings.TrimSpace(outTradeNo))
	return scanPaymentOrder(row)
}

// applyCatchAllSubscriptionDaysTx extends the stored subscription expiry and
// records the grant in the immutable quantity ledger.
func applyCatchAllSubscriptionDaysTx(ctx context.Context, tx *sql.Tx, order model.PaymentOrder, appliedAt time.Time) error {
	if order.GrantedTotal > int64(math.MaxInt) {
		return fmt.Errorf("subscription grant is too large")
	}

	access, found, err := getEmailCatchAllAccessTx(ctx, tx, order.UserID)
	if err != nil {
		return err
	}
	if !found {
		access = model.EmailCatchAllAccess{UserID: order.UserID}
	}

	base := appliedAt
	if access.SubscriptionExpiresAt != nil && access.SubscriptionExpiresAt.After(appliedAt) {
		base = access.SubscriptionExpiresAt.UTC()
	}
	nextExpiresAt := base.AddDate(0, 0, int(order.GrantedTotal))
	if err := upsertEmailCatchAllAccessTx(ctx, tx, order.UserID, &nextExpiresAt, access.RemainingCount, access.DailyLimitOverride, appliedAt); err != nil {
		return err
	}

	return insertPaymentQuantityRecordTx(ctx, tx, order.UserID, paymentQuantityResourceSubscriptionDays, paymentQuantityScopeCatchAll, order.GrantedTotal, order.Title, order.OutTradeNo, appliedAt)
}

// applyCatchAllRemainingCountTx increments the prepaid remaining count and
// records the grant in the immutable quantity ledger.
func applyCatchAllRemainingCountTx(ctx context.Context, tx *sql.Tx, order model.PaymentOrder, appliedAt time.Time) error {
	if order.GrantedTotal > int64(math.MaxInt) {
		return fmt.Errorf("remaining count grant is too large")
	}

	access, found, err := getEmailCatchAllAccessTx(ctx, tx, order.UserID)
	if err != nil {
		return err
	}
	if !found {
		access = model.EmailCatchAllAccess{UserID: order.UserID}
	}
	nextRemainingCount := access.RemainingCount + order.GrantedTotal
	if err := upsertEmailCatchAllAccessTx(ctx, tx, order.UserID, access.SubscriptionExpiresAt, nextRemainingCount, access.DailyLimitOverride, appliedAt); err != nil {
		return err
	}

	return insertPaymentQuantityRecordTx(ctx, tx, order.UserID, paymentQuantityResourceRemainingCount, paymentQuantityScopeCatchAll, order.GrantedTotal, order.Title, order.OutTradeNo, appliedAt)
}

// upsertEmailCatchAllAccessTx persists the current mutable catch-all access
// snapshot inside the surrounding entitlement-application transaction.
func upsertEmailCatchAllAccessTx(ctx context.Context, tx *sql.Tx, userID int64, subscriptionExpiresAt *time.Time, remainingCount int64, dailyLimitOverride *int64, now time.Time) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO email_catch_all_access (
    user_id,
    subscription_expires_at,
    remaining_count,
    daily_limit_override,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    subscription_expires_at=excluded.subscription_expires_at,
    remaining_count=excluded.remaining_count,
    daily_limit_override=excluded.daily_limit_override,
    updated_at=excluded.updated_at
`,
		userID,
		formatNullableTime(subscriptionExpiresAt),
		remainingCount,
		normalizeNullableInt64(dailyLimitOverride),
		formatTime(now),
		formatTime(now),
	)
	return err
}

// insertPaymentQuantityRecordTx appends one immutable quantity-ledger row that
// explains which paid order changed which user-facing resource.
func insertPaymentQuantityRecordTx(ctx context.Context, tx *sql.Tx, userID int64, resourceKey string, scope string, delta int64, title string, outTradeNo string, createdAt time.Time) error {
	if delta > int64(math.MaxInt) {
		return fmt.Errorf("payment quantity delta is too large")
	}

	_, err := tx.ExecContext(ctx, `
INSERT INTO quantity_records (
    user_id,
    resource_key,
    scope,
    delta,
    source,
    reason,
    reference_type,
    reference_id,
    expires_at,
    created_by_user_id,
    created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?)
`,
		userID,
		resourceKey,
		scope,
		int(delta),
		paymentQuantitySourceGateway,
		"Linux Do Credit 兑换："+strings.TrimSpace(title),
		paymentQuantityReferenceTypeOrder,
		strings.TrimSpace(outTradeNo),
		formatTime(createdAt),
	)
	return err
}

// normalizeNullableInt64 keeps SQLite writes compatible with the existing
// pointer-based optional integer fields.
func normalizeNullableInt64(value *int64) any {
	if value == nil {
		return nil
	}
	return *value
}

// scanPaymentProduct maps one product row into the shared model package.
func scanPaymentProduct(scanner interface{ Scan(dest ...any) error }) (model.PaymentProduct, error) {
	var item model.PaymentProduct
	var enabled int
	var createdAt string
	var updatedAt string

	if err := scanner.Scan(
		&item.Key,
		&item.DisplayName,
		&item.Description,
		&enabled,
		&item.UnitPriceCents,
		&item.GrantQuantity,
		&item.GrantUnit,
		&item.EffectType,
		&item.SortOrder,
		&createdAt,
		&updatedAt,
	); err != nil {
		return model.PaymentProduct{}, err
	}

	item.Enabled = enabled == 1

	var err error
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.PaymentProduct{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.PaymentProduct{}, err
	}
	return item, nil
}

// scanPaymentOrder maps one joined payment-order row into the shared model.
func scanPaymentOrder(scanner interface{ Scan(dest ...any) error }) (model.PaymentOrder, error) {
	var item model.PaymentOrder
	var paidAt sql.NullString
	var appliedAt sql.NullString
	var lastCheckedAt sql.NullString
	var createdAt string
	var updatedAt string

	if err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.Username,
		&item.DisplayName,
		&item.ProductKey,
		&item.ProductName,
		&item.Title,
		&item.GatewayType,
		&item.OutTradeNo,
		&item.ProviderTradeNo,
		&item.Status,
		&item.Units,
		&item.GrantQuantity,
		&item.GrantedTotal,
		&item.GrantUnit,
		&item.UnitPriceCents,
		&item.TotalPriceCents,
		&item.EffectType,
		&item.PaymentURL,
		&item.NotifyPayloadRaw,
		&paidAt,
		&appliedAt,
		&lastCheckedAt,
		&createdAt,
		&updatedAt,
	); err != nil {
		return model.PaymentOrder{}, err
	}

	var err error
	if item.PaidAt, err = parseNullableTime(paidAt); err != nil {
		return model.PaymentOrder{}, err
	}
	if item.AppliedAt, err = parseNullableTime(appliedAt); err != nil {
		return model.PaymentOrder{}, err
	}
	if item.LastCheckedAt, err = parseNullableTime(lastCheckedAt); err != nil {
		return model.PaymentOrder{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.PaymentOrder{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.PaymentOrder{}, err
	}
	return item, nil
}
