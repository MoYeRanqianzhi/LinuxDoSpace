-- 011_pow_settings.sql stores administrator-editable proof-of-work feature
-- settings, benefit toggles, difficulty toggles, and per-user daily overrides.

CREATE TABLE IF NOT EXISTS pow_global_settings (
    id INTEGER PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 1,
    default_daily_completion_limit INTEGER NOT NULL DEFAULT 5,
    base_reward_min INTEGER NOT NULL DEFAULT 5,
    base_reward_max INTEGER NOT NULL DEFAULT 10,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO pow_global_settings (
    id,
    enabled,
    default_daily_completion_limit,
    base_reward_min,
    base_reward_max,
    created_at,
    updated_at
) VALUES (
    1,
    1,
    5,
    5,
    10,
    STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'),
    STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
)
ON CONFLICT (id) DO NOTHING;

CREATE TABLE IF NOT EXISTS pow_benefit_settings (
    key TEXT PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO pow_benefit_settings (
    key,
    enabled,
    created_at,
    updated_at
) VALUES (
    'email_catch_all_remaining_count',
    1,
    STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'),
    STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')
)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS pow_difficulty_settings (
    difficulty INTEGER PRIMARY KEY,
    enabled INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT INTO pow_difficulty_settings (
    difficulty,
    enabled,
    created_at,
    updated_at
) VALUES
    (3, 1, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'), STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
    (6, 1, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'), STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
    (9, 1, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'), STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now')),
    (12, 1, STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'), STRFTIME('%Y-%m-%dT%H:%M:%fZ', 'now'))
ON CONFLICT (difficulty) DO NOTHING;

CREATE TABLE IF NOT EXISTS pow_user_settings (
    user_id INTEGER PRIMARY KEY,
    daily_completion_limit_override INTEGER NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);
