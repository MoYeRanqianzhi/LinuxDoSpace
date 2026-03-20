-- 019_api_tokens.sql adds user-generated API tokens and lets email routes
-- target either verified email addresses or live API-token mail streams.

CREATE TABLE IF NOT EXISTS api_tokens (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    owner_user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    public_id TEXT NOT NULL UNIQUE,
    token_hash TEXT NOT NULL UNIQUE,
    scopes_json TEXT NOT NULL,
    last_used_at TEXT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revoked_at TEXT NULL
);

CREATE INDEX IF NOT EXISTS idx_api_tokens_owner_user_id
ON api_tokens(owner_user_id, created_at DESC, id DESC);

CREATE INDEX IF NOT EXISTS idx_api_tokens_public_id
ON api_tokens(public_id);

CREATE INDEX IF NOT EXISTS idx_api_tokens_token_hash
ON api_tokens(token_hash);

ALTER TABLE email_routes ADD COLUMN target_kind TEXT NOT NULL DEFAULT 'email';
ALTER TABLE email_routes ADD COLUMN target_token_public_id TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_email_routes_target_kind
ON email_routes(target_kind, target_token_public_id);
