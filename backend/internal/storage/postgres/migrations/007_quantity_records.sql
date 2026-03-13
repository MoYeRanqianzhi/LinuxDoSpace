-- 007_quantity_records.sql adds one append-only quantity ledger that can later
-- back billing, redeem-code grants, subscriptions, and manual operator
-- adjustments without losing auditability.

CREATE TABLE IF NOT EXISTS quantity_records (
    id BIGSERIAL PRIMARY KEY,
    user_id BIGINT NOT NULL,
    resource_key TEXT NOT NULL,
    scope TEXT NOT NULL DEFAULT '',
    delta INTEGER NOT NULL,
    source TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT '',
    reference_type TEXT NOT NULL DEFAULT '',
    reference_id TEXT NOT NULL DEFAULT '',
    expires_at TEXT NULL,
    created_by_user_id BIGINT NULL,
    created_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    FOREIGN KEY (created_by_user_id) REFERENCES users(id) ON DELETE SET NULL
);

CREATE INDEX IF NOT EXISTS idx_quantity_records_user_created_at ON quantity_records(user_id, created_at);
CREATE INDEX IF NOT EXISTS idx_quantity_records_user_resource_scope ON quantity_records(user_id, resource_key, scope);
