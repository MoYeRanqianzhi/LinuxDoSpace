-- 020_pow_challenge_invariants.sql hardens proof-of-work invariants so one
-- user cannot keep multiple active challenges or race the daily claim cap.

CREATE UNIQUE INDEX IF NOT EXISTS idx_pow_challenges_one_active_per_user
ON pow_challenges(user_id)
WHERE status = 'active';

CREATE TABLE IF NOT EXISTS pow_challenge_daily_usage (
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    usage_date TEXT NOT NULL,
    used_count INTEGER NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(user_id, usage_date)
);

CREATE INDEX IF NOT EXISTS idx_pow_challenge_daily_usage_usage_date
ON pow_challenge_daily_usage(usage_date, user_id);
