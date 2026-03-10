-- 006_fix_email_catch_all_targets.sql corrects the public permission wording
-- and rewrites historical application targets created during the broken
-- `*@<namespace>` period back to the canonical `catch-all@<namespace>` form.

UPDATE permission_policies
SET
    display_name = 'catch-all@<username>.linuxdo.space',
    description = 'Allows one dedicated catch-all mailbox forwarding permission under the user namespace.',
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE key = 'email_catch_all';

DELETE FROM admin_applications
WHERE type = 'email_catch_all'
  AND target LIKE '*@%'
  AND EXISTS (
      SELECT 1
      FROM admin_applications AS canonical
      WHERE canonical.applicant_user_id = admin_applications.applicant_user_id
        AND canonical.type = admin_applications.type
        AND LOWER(canonical.target) = LOWER(REPLACE(admin_applications.target, '*@', 'catch-all@'))
  );

UPDATE admin_applications
SET
    target = REPLACE(target, '*@', 'catch-all@'),
    updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
WHERE type = 'email_catch_all'
  AND target LIKE '*@%';
