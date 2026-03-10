-- 004_permission_policies.sql adds persistent permission-policy configuration
-- together with one uniqueness constraint that lets one application row act as
-- both the approval trace and the current permission state for a user/target.

CREATE TABLE IF NOT EXISTS permission_policies (
    key TEXT PRIMARY KEY,
    display_name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    enabled INTEGER NOT NULL DEFAULT 1,
    auto_approve INTEGER NOT NULL DEFAULT 1,
    min_trust_level INTEGER NOT NULL DEFAULT 2,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

INSERT OR IGNORE INTO permission_policies (
    key,
    display_name,
    description,
    enabled,
    auto_approve,
    min_trust_level,
    created_at,
    updated_at
) VALUES (
    'email_catch_all',
    'catch-all@<username>.linuxdo.space',
    'Allows one dedicated catch-all mailbox forwarding permission under the user namespace.',
    1,
    1,
    2,
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
);

CREATE INDEX IF NOT EXISTS idx_permission_policies_enabled ON permission_policies(enabled);

DELETE FROM admin_applications
WHERE id IN (
    SELECT stale.id
    FROM admin_applications AS stale
    INNER JOIN admin_applications AS newer
        ON newer.applicant_user_id = stale.applicant_user_id
       AND newer.type = stale.type
       AND newer.target = stale.target
       AND (
            newer.updated_at > stale.updated_at
            OR (newer.updated_at = stale.updated_at AND newer.id > stale.id)
       )
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_admin_applications_applicant_type_target
ON admin_applications(applicant_user_id, type, target);
