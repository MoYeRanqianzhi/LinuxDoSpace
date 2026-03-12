-- 008_email_catch_all_access.sql adds mutable runtime state for catch-all mail
-- delivery. The quantity ledger remains append-only audit history, while these
-- tables carry the currently effective subscription expiry, remaining count,
-- and per-day usage state enforced by the SMTP relay.

ALTER TABLE permission_policies
ADD COLUMN default_daily_limit INTEGER NOT NULL DEFAULT 1000000;

UPDATE permission_policies
SET default_daily_limit = 1000000
WHERE key = 'email_catch_all'
  AND default_daily_limit <= 0;

CREATE TABLE IF NOT EXISTS email_catch_all_access (
    user_id INTEGER PRIMARY KEY,
    subscription_expires_at TEXT NULL,
    remaining_count INTEGER NOT NULL DEFAULT 0,
    daily_limit_override INTEGER NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS email_catch_all_daily_usage (
    user_id INTEGER NOT NULL,
    usage_date TEXT NOT NULL,
    used_count INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (user_id, usage_date),
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_email_catch_all_daily_usage_user_date
ON email_catch_all_daily_usage(user_id, usage_date);
