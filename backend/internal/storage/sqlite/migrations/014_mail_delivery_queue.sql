-- 014_mail_delivery_queue.sql adds the durable outbound mail queue used by
-- the built-in SMTP relay. Inbound SMTP now only needs to commit one message
-- row plus one or more delivery jobs, while background workers perform the actual
-- network delivery and retries later.

CREATE TABLE IF NOT EXISTS mail_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    original_envelope_from TEXT NOT NULL DEFAULT '',
    raw_message BLOB NOT NULL,
    message_size_bytes INTEGER NOT NULL,
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS mail_delivery_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    message_id INTEGER NOT NULL REFERENCES mail_messages(id) ON DELETE CASCADE,
    original_recipients_json TEXT NOT NULL,
    target_recipients_json TEXT NOT NULL,
    reservations_json TEXT NOT NULL,
    status TEXT NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    next_attempt_at TEXT NOT NULL,
    processing_started_at TEXT NULL,
    last_attempt_at TEXT NULL,
    last_error TEXT NOT NULL DEFAULT '',
    delivered_at TEXT NULL,
    failed_at TEXT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_mail_delivery_jobs_ready
ON mail_delivery_jobs(status, next_attempt_at, id);

CREATE INDEX IF NOT EXISTS idx_mail_delivery_jobs_processing
ON mail_delivery_jobs(status, processing_started_at, id);

CREATE INDEX IF NOT EXISTS idx_mail_delivery_jobs_delivered_cleanup
ON mail_delivery_jobs(status, delivered_at, id);

CREATE INDEX IF NOT EXISTS idx_mail_delivery_jobs_failed_cleanup
ON mail_delivery_jobs(status, failed_at, id);

CREATE INDEX IF NOT EXISTS idx_mail_delivery_jobs_message_id
ON mail_delivery_jobs(message_id);
