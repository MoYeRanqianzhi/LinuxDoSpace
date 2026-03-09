package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
)

// UpsertUserControlInput describes a moderation update for one local user account.
type UpsertUserControlInput struct {
	UserID   int64
	IsBanned bool
	Note     string
}

// CreateEmailRouteInput describes one administrator-created email forwarding rule.
type CreateEmailRouteInput struct {
	OwnerUserID int64
	RootDomain  string
	Prefix      string
	TargetEmail string
	Enabled     bool
}

// UpdateEmailRouteInput describes the mutable portion of an email forwarding rule.
type UpdateEmailRouteInput struct {
	ID          int64
	TargetEmail string
	Enabled     bool
}

// UpdateAllocationInput describes the mutable administrator-managed portion of
// one allocation namespace row.
type UpdateAllocationInput struct {
	ID        int64
	UserID    int64
	IsPrimary bool
	Source    string
	Status    string
}

// UpdateAdminApplicationInput describes one moderation decision for an application request.
type UpdateAdminApplicationInput struct {
	ID               int64
	Status           string
	ReviewNote       string
	ReviewedByUserID int64
}

// UpsertAdminApplicationInput describes one user-side permission application.
// The unique applicant/type/target tuple lets one row act as both the latest
// application snapshot and the currently effective approval state.
type UpsertAdminApplicationInput struct {
	ApplicantUserID  int64
	Type             string
	Target           string
	Reason           string
	Status           string
	ReviewNote       string
	ReviewedByUserID *int64
	ReviewedAt       *time.Time
}

// UpsertEmailRouteByAddressInput describes an idempotent email-route write keyed
// by the full local address instead of the local row identifier.
type UpsertEmailRouteByAddressInput struct {
	OwnerUserID int64
	RootDomain  string
	Prefix      string
	TargetEmail string
	Enabled     bool
}

// UpsertPermissionPolicyInput describes the mutable fields of one permission
// policy row stored in SQLite.
type UpsertPermissionPolicyInput struct {
	Key           string
	DisplayName   string
	Description   string
	Enabled       bool
	AutoApprove   bool
	MinTrustLevel int
}

// CreateRedeemCodeInput describes one redeem code emitted by the administrator console.
type CreateRedeemCodeInput struct {
	Code            string
	Type            string
	Target          string
	Note            string
	CreatedByUserID int64
}

// GetUserControlByUserID loads the persisted moderation state for one local user.
func (s *Store) GetUserControlByUserID(ctx context.Context, userID int64) (model.UserControl, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    user_id,
    is_banned,
    note,
    created_at,
    updated_at
FROM user_controls
WHERE user_id = ?
`, userID)

	item, err := scanUserControl(row)
	if err != nil {
		if IsNotFound(err) {
			return model.UserControl{UserID: userID}, nil
		}
		return model.UserControl{}, err
	}
	return item, nil
}

// UpsertUserControl inserts or updates the moderation state for one local user.
func (s *Store) UpsertUserControl(ctx context.Context, input UpsertUserControlInput) (model.UserControl, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO user_controls (
    user_id,
    is_banned,
    note,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?)
ON CONFLICT(user_id) DO UPDATE SET
    is_banned=excluded.is_banned,
    note=excluded.note,
    updated_at=excluded.updated_at
RETURNING
    user_id,
    is_banned,
    note,
    created_at,
    updated_at
`,
		input.UserID,
		boolToInt(input.IsBanned),
		strings.TrimSpace(input.Note),
		formatTime(now),
		formatTime(now),
	)
	return scanUserControl(row)
}

// ListAdminUsers returns the compact user list required by the administrator console.
func (s *Store) ListAdminUsers(ctx context.Context) ([]model.AdminUserSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    u.id,
    u.linuxdo_user_id,
    u.username,
    u.display_name,
    u.avatar_url,
    u.trust_level,
    u.is_linuxdo_admin,
    u.is_app_admin,
    COALESCE(uc.is_banned, 0) AS is_banned,
    COUNT(DISTINCT a.id) AS allocation_count,
    u.created_at,
    u.last_login_at
FROM users u
LEFT JOIN user_controls uc ON uc.user_id = u.id
LEFT JOIN allocations a ON a.user_id = u.id AND a.status = 'active'
GROUP BY
    u.id,
    u.linuxdo_user_id,
    u.username,
    u.display_name,
    u.avatar_url,
    u.trust_level,
    u.is_linuxdo_admin,
    u.is_app_admin,
    is_banned,
    u.created_at,
    u.last_login_at
ORDER BY u.last_login_at DESC, u.id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminUserSummary, 0, 32)
	for rows.Next() {
		item, scanErr := scanAdminUserSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// ListAdminAllocations returns all allocation namespaces together with their owners.
func (s *Store) ListAdminAllocations(ctx context.Context) ([]model.AdminAllocationSummary, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    a.id,
    a.user_id,
    u.username,
    u.display_name,
    a.managed_domain_id,
    md.root_domain,
    a.prefix,
    a.normalized_prefix,
    a.fqdn,
    a.is_primary,
    a.source,
    a.status,
    md.cloudflare_zone_id,
    a.created_at,
    a.updated_at
FROM allocations a
INNER JOIN users u ON u.id = a.user_id
INNER JOIN managed_domains md ON md.id = a.managed_domain_id
ORDER BY a.created_at DESC, a.id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminAllocationSummary, 0, 64)
	for rows.Next() {
		item, scanErr := scanAdminAllocationSummary(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// UpdateAllocation updates one allocation row for administrator moderation and
// ownership-correction workflows.
func (s *Store) UpdateAllocation(ctx context.Context, input UpdateAllocationInput) (model.Allocation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Allocation{}, err
	}
	defer tx.Rollback()

	var managedDomainID int64
	row := tx.QueryRowContext(ctx, `
SELECT managed_domain_id
FROM allocations
WHERE id = ?
`, input.ID)
	if err := row.Scan(&managedDomainID); err != nil {
		return model.Allocation{}, err
	}

	now := time.Now().UTC()
	if input.IsPrimary {
		if _, err := tx.ExecContext(ctx, `
UPDATE allocations
SET is_primary = 0, updated_at = ?
WHERE user_id = ? AND managed_domain_id = ? AND id <> ?
`, formatTime(now), input.UserID, managedDomainID, input.ID); err != nil {
			return model.Allocation{}, err
		}
	}

	row = tx.QueryRowContext(ctx, `
UPDATE allocations
SET user_id = ?,
    is_primary = ?,
    source = ?,
    status = ?,
    updated_at = ?
WHERE id = ?
RETURNING
    id,
    user_id,
    managed_domain_id,
    prefix,
    normalized_prefix,
    fqdn,
    is_primary,
    source,
    status,
    created_at,
    updated_at
`,
		input.UserID,
		boolToInt(input.IsPrimary),
		strings.TrimSpace(input.Source),
		strings.TrimSpace(input.Status),
		formatTime(now),
		input.ID,
	)

	var item model.Allocation
	var createdAt string
	var updatedAt string
	var isPrimary int
	if err := row.Scan(
		&item.ID,
		&item.UserID,
		&item.ManagedDomainID,
		&item.Prefix,
		&item.NormalizedPrefix,
		&item.FQDN,
		&isPrimary,
		&item.Source,
		&item.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		return model.Allocation{}, err
	}
	item.IsPrimary = isPrimary == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Allocation{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.Allocation{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.Allocation{}, err
	}
	return item, nil
}

// ListEmailRoutes returns all persisted email forwarding rules.
func (s *Store) ListEmailRoutes(ctx context.Context) ([]model.EmailRoute, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    er.id,
    er.owner_user_id,
    u.username,
    u.display_name,
    er.root_domain,
    er.prefix,
    er.target_email,
    er.enabled,
    er.created_at,
    er.updated_at
FROM email_routes er
INNER JOIN users u ON u.id = er.owner_user_id
ORDER BY er.created_at DESC, er.id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.EmailRoute, 0, 32)
	for rows.Next() {
		item, scanErr := scanEmailRoute(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// ListEmailRoutesByOwner returns all email forwarding rules owned by one user.
func (s *Store) ListEmailRoutesByOwner(ctx context.Context, ownerUserID int64) ([]model.EmailRoute, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    er.id,
    er.owner_user_id,
    u.username,
    u.display_name,
    er.root_domain,
    er.prefix,
    er.target_email,
    er.enabled,
    er.created_at,
    er.updated_at
FROM email_routes er
INNER JOIN users u ON u.id = er.owner_user_id
WHERE er.owner_user_id = ?
ORDER BY er.created_at DESC, er.id DESC
`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.EmailRoute, 0, 8)
	for rows.Next() {
		item, scanErr := scanEmailRoute(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// GetEmailRouteByAddress loads one email forwarding rule by the routed address.
func (s *Store) GetEmailRouteByAddress(ctx context.Context, rootDomain string, prefix string) (model.EmailRoute, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    er.id,
    er.owner_user_id,
    u.username,
    u.display_name,
    er.root_domain,
    er.prefix,
    er.target_email,
    er.enabled,
    er.created_at,
    er.updated_at
FROM email_routes er
INNER JOIN users u ON u.id = er.owner_user_id
WHERE er.root_domain = ? AND er.prefix = ?
`, strings.ToLower(strings.TrimSpace(rootDomain)), strings.ToLower(strings.TrimSpace(prefix)))
	return scanEmailRoute(row)
}

// CreateEmailRoute inserts one administrator-managed email forwarding rule.
func (s *Store) CreateEmailRoute(ctx context.Context, input CreateEmailRouteInput) (model.EmailRoute, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO email_routes (
    owner_user_id,
    root_domain,
    prefix,
    target_email,
    enabled,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING id
`,
		input.OwnerUserID,
		strings.ToLower(strings.TrimSpace(input.RootDomain)),
		strings.ToLower(strings.TrimSpace(input.Prefix)),
		strings.ToLower(strings.TrimSpace(input.TargetEmail)),
		boolToInt(input.Enabled),
		formatTime(now),
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.EmailRoute{}, err
	}
	return s.getEmailRouteByID(ctx, id)
}

// UpsertEmailRouteByAddress inserts or updates one email forwarding rule keyed
// by its routed local address.
func (s *Store) UpsertEmailRouteByAddress(ctx context.Context, input UpsertEmailRouteByAddressInput) (model.EmailRoute, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO email_routes (
    owner_user_id,
    root_domain,
    prefix,
    target_email,
    enabled,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(root_domain, prefix) DO UPDATE SET
    owner_user_id = excluded.owner_user_id,
    target_email = excluded.target_email,
    enabled = excluded.enabled,
    updated_at = excluded.updated_at
RETURNING id
`,
		input.OwnerUserID,
		strings.ToLower(strings.TrimSpace(input.RootDomain)),
		strings.ToLower(strings.TrimSpace(input.Prefix)),
		strings.ToLower(strings.TrimSpace(input.TargetEmail)),
		boolToInt(input.Enabled),
		formatTime(now),
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.EmailRoute{}, err
	}
	return s.getEmailRouteByID(ctx, id)
}

// UpdateEmailRoute updates the mutable fields of one email forwarding rule.
func (s *Store) UpdateEmailRoute(ctx context.Context, input UpdateEmailRouteInput) (model.EmailRoute, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
UPDATE email_routes
SET
    target_email = ?,
    enabled = ?,
    updated_at = ?
WHERE id = ?
RETURNING id
`,
		strings.ToLower(strings.TrimSpace(input.TargetEmail)),
		boolToInt(input.Enabled),
		formatTime(now),
		input.ID,
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.EmailRoute{}, err
	}
	return s.getEmailRouteByID(ctx, id)
}

// DeleteEmailRoute removes one email forwarding rule.
func (s *Store) DeleteEmailRoute(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM email_routes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ListAdminApplications returns all moderation requests visible in the administrator console.
func (s *Store) ListAdminApplications(ctx context.Context) ([]model.AdminApplication, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    ap.id,
    ap.applicant_user_id,
    u.username,
    u.display_name,
    ap.type,
    ap.target,
    ap.reason,
    ap.status,
    ap.review_note,
    ap.reviewed_by_user_id,
    ap.reviewed_at,
    ap.created_at,
    ap.updated_at
FROM admin_applications ap
INNER JOIN users u ON u.id = ap.applicant_user_id
ORDER BY
    CASE ap.status WHEN 'pending' THEN 0 WHEN 'approved' THEN 1 ELSE 2 END,
    ap.created_at DESC,
    ap.id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminApplication, 0, 32)
	for rows.Next() {
		item, scanErr := scanAdminApplication(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// ListAdminApplicationsByApplicant returns all moderation requests submitted by
// one user, ordered with the newest state first.
func (s *Store) ListAdminApplicationsByApplicant(ctx context.Context, applicantUserID int64) ([]model.AdminApplication, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    ap.id,
    ap.applicant_user_id,
    u.username,
    u.display_name,
    ap.type,
    ap.target,
    ap.reason,
    ap.status,
    ap.review_note,
    ap.reviewed_by_user_id,
    ap.reviewed_at,
    ap.created_at,
    ap.updated_at
FROM admin_applications ap
INNER JOIN users u ON u.id = ap.applicant_user_id
WHERE ap.applicant_user_id = ?
ORDER BY ap.updated_at DESC, ap.id DESC
`, applicantUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.AdminApplication, 0, 8)
	for rows.Next() {
		item, scanErr := scanAdminApplication(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// UpsertAdminApplication inserts or refreshes one permission application row.
func (s *Store) UpsertAdminApplication(ctx context.Context, input UpsertAdminApplicationInput) (model.AdminApplication, error) {
	now := time.Now().UTC()
	var reviewedAtValue any
	if input.ReviewedAt != nil {
		reviewedAtValue = formatTime(input.ReviewedAt.UTC())
	}
	row := s.db.QueryRowContext(ctx, `
INSERT INTO admin_applications (
    applicant_user_id,
    type,
    target,
    reason,
    status,
    review_note,
    reviewed_by_user_id,
    reviewed_at,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(applicant_user_id, type, target) DO UPDATE SET
    reason = excluded.reason,
    status = excluded.status,
    review_note = excluded.review_note,
    reviewed_by_user_id = excluded.reviewed_by_user_id,
    reviewed_at = excluded.reviewed_at,
    updated_at = excluded.updated_at
RETURNING id
`,
		input.ApplicantUserID,
		strings.TrimSpace(input.Type),
		strings.TrimSpace(input.Target),
		strings.TrimSpace(input.Reason),
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.ReviewNote),
		input.ReviewedByUserID,
		reviewedAtValue,
		formatTime(now),
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.AdminApplication{}, err
	}
	return s.getAdminApplicationByID(ctx, id)
}

// UpdateAdminApplication updates one moderation request decision.
func (s *Store) UpdateAdminApplication(ctx context.Context, input UpdateAdminApplicationInput) (model.AdminApplication, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
UPDATE admin_applications
SET
    status = ?,
    review_note = ?,
    reviewed_by_user_id = ?,
    reviewed_at = ?,
    updated_at = ?
WHERE id = ?
RETURNING id
`,
		strings.TrimSpace(input.Status),
		strings.TrimSpace(input.ReviewNote),
		input.ReviewedByUserID,
		formatTime(now),
		formatTime(now),
		input.ID,
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.AdminApplication{}, err
	}
	return s.getAdminApplicationByID(ctx, id)
}

// ListPermissionPolicies returns all stored permission-policy rules.
func (s *Store) ListPermissionPolicies(ctx context.Context) ([]model.PermissionPolicy, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    key,
    display_name,
    description,
    enabled,
    auto_approve,
    min_trust_level,
    created_at,
    updated_at
FROM permission_policies
ORDER BY key ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.PermissionPolicy, 0, 8)
	for rows.Next() {
		item, scanErr := scanPermissionPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// GetPermissionPolicy loads one permission policy by its stable key.
func (s *Store) GetPermissionPolicy(ctx context.Context, key string) (model.PermissionPolicy, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    key,
    display_name,
    description,
    enabled,
    auto_approve,
    min_trust_level,
    created_at,
    updated_at
FROM permission_policies
WHERE key = ?
`, strings.TrimSpace(key))
	return scanPermissionPolicy(row)
}

// UpsertPermissionPolicy inserts or updates one permission-policy row.
func (s *Store) UpsertPermissionPolicy(ctx context.Context, input UpsertPermissionPolicyInput) (model.PermissionPolicy, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO permission_policies (
    key,
    display_name,
    description,
    enabled,
    auto_approve,
    min_trust_level,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET
    display_name = excluded.display_name,
    description = excluded.description,
    enabled = excluded.enabled,
    auto_approve = excluded.auto_approve,
    min_trust_level = excluded.min_trust_level,
    updated_at = excluded.updated_at
RETURNING key
`,
		strings.TrimSpace(input.Key),
		strings.TrimSpace(input.DisplayName),
		strings.TrimSpace(input.Description),
		boolToInt(input.Enabled),
		boolToInt(input.AutoApprove),
		input.MinTrustLevel,
		formatTime(now),
		formatTime(now),
	)

	var key string
	if err := row.Scan(&key); err != nil {
		return model.PermissionPolicy{}, err
	}
	return s.GetPermissionPolicy(ctx, key)
}

// ListRedeemCodes returns all generated redeem codes together with creator and consumer identity when available.
func (s *Store) ListRedeemCodes(ctx context.Context) ([]model.RedeemCode, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    rc.id,
    rc.code,
    rc.type,
    rc.target,
    rc.note,
    rc.created_by_user_id,
    creator.username,
    rc.used_by_user_id,
    COALESCE(consumer.username, ''),
    rc.created_at,
    rc.used_at
FROM redeem_codes rc
INNER JOIN users creator ON creator.id = rc.created_by_user_id
LEFT JOIN users consumer ON consumer.id = rc.used_by_user_id
ORDER BY rc.created_at DESC, rc.id DESC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]model.RedeemCode, 0, 32)
	for rows.Next() {
		item, scanErr := scanRedeemCode(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

// CreateRedeemCode inserts one generated redeem code.
func (s *Store) CreateRedeemCode(ctx context.Context, input CreateRedeemCodeInput) (model.RedeemCode, error) {
	now := time.Now().UTC()
	row := s.db.QueryRowContext(ctx, `
INSERT INTO redeem_codes (
    code,
    type,
    target,
    note,
    created_by_user_id,
    created_at
) VALUES (?, ?, ?, ?, ?, ?)
RETURNING id
`,
		strings.TrimSpace(input.Code),
		strings.TrimSpace(input.Type),
		strings.TrimSpace(input.Target),
		strings.TrimSpace(input.Note),
		input.CreatedByUserID,
		formatTime(now),
	)

	var id int64
	if err := row.Scan(&id); err != nil {
		return model.RedeemCode{}, err
	}
	return s.getRedeemCodeByID(ctx, id)
}

// DeleteRedeemCode removes one generated redeem code that is no longer needed.
func (s *Store) DeleteRedeemCode(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM redeem_codes WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// getEmailRouteByID loads one email forwarding rule by its local identifier.
func (s *Store) getEmailRouteByID(ctx context.Context, id int64) (model.EmailRoute, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    er.id,
    er.owner_user_id,
    u.username,
    u.display_name,
    er.root_domain,
    er.prefix,
    er.target_email,
    er.enabled,
    er.created_at,
    er.updated_at
FROM email_routes er
INNER JOIN users u ON u.id = er.owner_user_id
WHERE er.id = ?
`, id)
	return scanEmailRoute(row)
}

// getAdminApplicationByID loads one moderation request by its local identifier.
func (s *Store) getAdminApplicationByID(ctx context.Context, id int64) (model.AdminApplication, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    ap.id,
    ap.applicant_user_id,
    u.username,
    u.display_name,
    ap.type,
    ap.target,
    ap.reason,
    ap.status,
    ap.review_note,
    ap.reviewed_by_user_id,
    ap.reviewed_at,
    ap.created_at,
    ap.updated_at
FROM admin_applications ap
INNER JOIN users u ON u.id = ap.applicant_user_id
WHERE ap.id = ?
`, id)
	return scanAdminApplication(row)
}

// scanPermissionPolicy maps one permission policy row into the model package.
func scanPermissionPolicy(scanner interface{ Scan(dest ...any) error }) (model.PermissionPolicy, error) {
	var item model.PermissionPolicy
	var enabled int
	var autoApprove int
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&item.Key,
		&item.DisplayName,
		&item.Description,
		&enabled,
		&autoApprove,
		&item.MinTrustLevel,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.PermissionPolicy{}, err
	}

	item.Enabled = enabled == 1
	item.AutoApprove = autoApprove == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.PermissionPolicy{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.PermissionPolicy{}, err
	}
	return item, nil
}

// getRedeemCodeByID loads one generated redeem code by its local identifier.
func (s *Store) getRedeemCodeByID(ctx context.Context, id int64) (model.RedeemCode, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    rc.id,
    rc.code,
    rc.type,
    rc.target,
    rc.note,
    rc.created_by_user_id,
    creator.username,
    rc.used_by_user_id,
    COALESCE(consumer.username, ''),
    rc.created_at,
    rc.used_at
FROM redeem_codes rc
INNER JOIN users creator ON creator.id = rc.created_by_user_id
LEFT JOIN users consumer ON consumer.id = rc.used_by_user_id
WHERE rc.id = ?
`, id)
	return scanRedeemCode(row)
}

// scanUserControl maps one moderation row into the model package representation.
func scanUserControl(scanner interface{ Scan(dest ...any) error }) (model.UserControl, error) {
	var item model.UserControl
	var createdAt string
	var updatedAt string
	var isBanned int

	err := scanner.Scan(&item.UserID, &isBanned, &item.Note, &createdAt, &updatedAt)
	if err != nil {
		return model.UserControl{}, err
	}
	item.IsBanned = isBanned == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.UserControl{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.UserControl{}, err
	}
	return item, nil
}

// scanAdminUserSummary maps one administrator user list row.
func scanAdminUserSummary(scanner interface{ Scan(dest ...any) error }) (model.AdminUserSummary, error) {
	var item model.AdminUserSummary
	var createdAt string
	var lastLoginAt string
	var isLinuxDOAdmin int
	var isAppAdmin int
	var isBanned int

	err := scanner.Scan(
		&item.ID,
		&item.LinuxDOUserID,
		&item.Username,
		&item.DisplayName,
		&item.AvatarURL,
		&item.TrustLevel,
		&isLinuxDOAdmin,
		&isAppAdmin,
		&isBanned,
		&item.AllocationCount,
		&createdAt,
		&lastLoginAt,
	)
	if err != nil {
		return model.AdminUserSummary{}, err
	}

	item.IsLinuxDOAdmin = isLinuxDOAdmin == 1
	item.IsAppAdmin = isAppAdmin == 1
	item.IsBanned = isBanned == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.AdminUserSummary{}, err
	}
	if item.LastLoginAt, err = parseTime(lastLoginAt); err != nil {
		return model.AdminUserSummary{}, err
	}
	return item, nil
}

// scanAdminAllocationSummary maps one administrator allocation row.
func scanAdminAllocationSummary(scanner interface{ Scan(dest ...any) error }) (model.AdminAllocationSummary, error) {
	var item model.AdminAllocationSummary
	var createdAt string
	var updatedAt string
	var isPrimary int

	err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.OwnerUsername,
		&item.OwnerDisplayName,
		&item.ManagedDomainID,
		&item.RootDomain,
		&item.Prefix,
		&item.NormalizedPrefix,
		&item.FQDN,
		&isPrimary,
		&item.Source,
		&item.Status,
		&item.CloudflareZoneID,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.AdminAllocationSummary{}, err
	}

	item.IsPrimary = isPrimary == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.AdminAllocationSummary{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.AdminAllocationSummary{}, err
	}
	return item, nil
}

// scanEmailRoute maps one administrator email route row.
func scanEmailRoute(scanner interface{ Scan(dest ...any) error }) (model.EmailRoute, error) {
	var item model.EmailRoute
	var createdAt string
	var updatedAt string
	var enabled int

	err := scanner.Scan(
		&item.ID,
		&item.OwnerUserID,
		&item.OwnerUsername,
		&item.OwnerDisplayName,
		&item.RootDomain,
		&item.Prefix,
		&item.TargetEmail,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.EmailRoute{}, err
	}

	item.Enabled = enabled == 1
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.EmailRoute{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.EmailRoute{}, err
	}
	return item, nil
}

// scanAdminApplication maps one stored moderation request row.
func scanAdminApplication(scanner interface{ Scan(dest ...any) error }) (model.AdminApplication, error) {
	var item model.AdminApplication
	var reviewedByUserID sql.NullInt64
	var reviewedAt sql.NullString
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&item.ID,
		&item.ApplicantUserID,
		&item.ApplicantUsername,
		&item.ApplicantName,
		&item.Type,
		&item.Target,
		&item.Reason,
		&item.Status,
		&item.ReviewNote,
		&reviewedByUserID,
		&reviewedAt,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.AdminApplication{}, err
	}

	if reviewedByUserID.Valid {
		value := reviewedByUserID.Int64
		item.ReviewedByUserID = &value
	}
	if reviewedAt.Valid {
		parsed, parseErr := parseTime(reviewedAt.String)
		if parseErr != nil {
			return model.AdminApplication{}, parseErr
		}
		item.ReviewedAt = &parsed
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.AdminApplication{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.AdminApplication{}, err
	}
	return item, nil
}

// scanRedeemCode maps one generated redeem code row.
func scanRedeemCode(scanner interface{ Scan(dest ...any) error }) (model.RedeemCode, error) {
	var item model.RedeemCode
	var usedByUserID sql.NullInt64
	var usedAt sql.NullString
	var createdAt string
	var usedByUsername string

	err := scanner.Scan(
		&item.ID,
		&item.Code,
		&item.Type,
		&item.Target,
		&item.Note,
		&item.CreatedByUserID,
		&item.CreatedByUsername,
		&usedByUserID,
		&usedByUsername,
		&createdAt,
		&usedAt,
	)
	if err != nil {
		return model.RedeemCode{}, err
	}

	if usedByUserID.Valid {
		value := usedByUserID.Int64
		item.UsedByUserID = &value
		item.UsedByUsername = strings.TrimSpace(usedByUsername)
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.RedeemCode{}, err
	}
	if usedAt.Valid {
		parsed, parseErr := parseTime(usedAt.String)
		if parseErr != nil {
			return model.RedeemCode{}, parseErr
		}
		item.UsedAt = &parsed
	}
	return item, nil
}
