-- 009_payment_orders.sql adds Linux Do Credit purchasable products plus the
-- local order table used to track checkout, callback verification, and
-- idempotent entitlement application.

CREATE TABLE IF NOT EXISTS payment_products (
    key TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL,
    enabled INTEGER NOT NULL DEFAULT 1,
    unit_price_cents BIGINT NOT NULL,
    grant_quantity BIGINT NOT NULL,
    grant_unit TEXT NOT NULL,
    effect_type TEXT NOT NULL,
    sort_order INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

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
) VALUES
(
    'email_catch_all_subscription',
    '邮箱泛解析订阅',
    '每购买 1 份，默认增加 1 天邮箱泛解析订阅时长。',
    1,
    50000,
    1,
    'day',
    'email_catch_all_subscription_days',
    10,
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
),
(
    'email_catch_all_quota',
    '邮箱泛解析额度',
    '每购买 1 份，默认增加 10000 条邮箱泛解析剩余额度。',
    1,
    5000,
    10000,
    'message',
    'email_catch_all_remaining_count',
    20,
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
),
(
    'payment_test',
    '支付测试',
    '用于验证 Linux Do Credit 支付链路本身是否工作正常。',
    1,
    100,
    1,
    'run',
    'payment_test_counter',
    30,
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"'),
    TO_CHAR(CURRENT_TIMESTAMP AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS.MS"Z"')
)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS payment_orders (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    product_key TEXT NOT NULL REFERENCES payment_products(key),
    product_name TEXT NOT NULL,
    title TEXT NOT NULL,
    gateway_type TEXT NOT NULL,
    out_trade_no TEXT NOT NULL UNIQUE,
    provider_trade_no TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,
    units BIGINT NOT NULL,
    grant_quantity BIGINT NOT NULL,
    granted_total BIGINT NOT NULL,
    grant_unit TEXT NOT NULL,
    unit_price_cents BIGINT NOT NULL,
    total_price_cents BIGINT NOT NULL,
    effect_type TEXT NOT NULL,
    payment_url TEXT NOT NULL DEFAULT '',
    notify_payload_raw TEXT NOT NULL DEFAULT '',
    paid_at TEXT NULL,
    applied_at TEXT NULL,
    last_checked_at TEXT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_payment_products_enabled_sort ON payment_products(enabled, sort_order);
CREATE INDEX IF NOT EXISTS idx_payment_orders_user_created_at ON payment_orders(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_payment_orders_status_created_at ON payment_orders(status, created_at);
