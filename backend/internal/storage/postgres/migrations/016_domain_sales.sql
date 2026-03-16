-- 016_domain_sales.sql adds managed-domain sale settings plus the extra
-- payment-order context needed to turn one paid checkout into a namespace
-- allocation under a specific root domain.

ALTER TABLE managed_domains
    ADD COLUMN IF NOT EXISTS sale_enabled INTEGER NOT NULL DEFAULT 0;

ALTER TABLE managed_domains
    ADD COLUMN IF NOT EXISTS sale_base_price_cents BIGINT NOT NULL DEFAULT 0;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_root_domain TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_mode TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_prefix TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_normalized_prefix TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_requested_length INTEGER NOT NULL DEFAULT 0;

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_assigned_prefix TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN IF NOT EXISTS purchase_assigned_fqdn TEXT NOT NULL DEFAULT '';

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
) VALUES (
    'domain_allocation_purchase',
    '域名购买',
    '系统内部动态域名购买订单类型，不在公开 Linux Do Credit 商品列表中直接展示。',
    0,
    1,
    1,
    'allocation',
    'domain_allocation_purchase',
    1000,
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
)
ON CONFLICT (key) DO NOTHING;
