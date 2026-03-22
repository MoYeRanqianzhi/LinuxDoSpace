-- 022_payment_order_fulfillment_state.sql records local entitlement-application
-- progress separately from upstream payment confirmation so paid-but-unapplied
-- orders stop looking like healthy "still pending" orders forever.

ALTER TABLE payment_orders
    ADD COLUMN fulfillment_status TEXT NOT NULL DEFAULT 'pending';

ALTER TABLE payment_orders
    ADD COLUMN fulfillment_error TEXT NOT NULL DEFAULT '';

ALTER TABLE payment_orders
    ADD COLUMN fulfillment_failed_at TEXT NULL;

UPDATE payment_orders
SET
    fulfillment_status = CASE
        WHEN applied_at IS NOT NULL THEN 'applied'
        ELSE COALESCE(NULLIF(fulfillment_status, ''), 'pending')
    END,
    fulfillment_error = COALESCE(fulfillment_error, '')
WHERE fulfillment_status = ''
   OR fulfillment_status IS NULL
   OR applied_at IS NOT NULL;
