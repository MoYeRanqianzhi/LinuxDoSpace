package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"linuxdospace/backend/internal/model"
)

// UpsertUserInput 描述用户落库或更新时需要的字段。
type UpsertUserInput struct {
	LinuxDOUserID  int64
	Username       string
	DisplayName    string
	AvatarURL      string
	TrustLevel     int
	IsLinuxDOAdmin bool
	IsAppAdmin     bool
}

// CreateSessionInput 描述服务端会话创建参数。
type CreateSessionInput struct {
	ID                   string
	UserID               int64
	CSRFToken            string
	UserAgentFingerprint string
	ExpiresAt            time.Time
}

// UpsertManagedDomainInput 描述可分发根域名的写入参数。
type UpsertManagedDomainInput struct {
	RootDomain       string
	CloudflareZoneID string
	DefaultQuota     int
	AutoProvision    bool
	IsDefault        bool
	Enabled          bool
}

// SetUserQuotaInput 描述用户域名配额覆盖的写入参数。
type SetUserQuotaInput struct {
	UserID          int64
	ManagedDomainID int64
	MaxAllocations  int
	Reason          string
}

// CreateAllocationInput 描述一次子域名分配创建参数。
type CreateAllocationInput struct {
	UserID           int64
	ManagedDomainID  int64
	Prefix           string
	NormalizedPrefix string
	FQDN             string
	IsPrimary        bool
	Source           string
	Status           string
}

// AuditLogInput 描述一次审计事件写入参数。
type AuditLogInput struct {
	ActorUserID  *int64
	Action       string
	ResourceType string
	ResourceID   string
	MetadataJSON string
}

// UpsertUser 根据 Linux Do 用户 ID 写入或更新本地用户记录。
func (s *Store) UpsertUser(ctx context.Context, input UpsertUserInput) (model.User, error) {
	now := time.Now().UTC()

	query := `
INSERT INTO users (
    linuxdo_user_id,
    username,
    display_name,
    avatar_url,
    trust_level,
    is_linuxdo_admin,
    is_app_admin,
    created_at,
    updated_at,
    last_login_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(linuxdo_user_id) DO UPDATE SET
    username=excluded.username,
    display_name=excluded.display_name,
    avatar_url=excluded.avatar_url,
    trust_level=excluded.trust_level,
    is_linuxdo_admin=excluded.is_linuxdo_admin,
    is_app_admin=excluded.is_app_admin,
    updated_at=excluded.updated_at,
    last_login_at=excluded.last_login_at
RETURNING
    id,
    linuxdo_user_id,
    username,
    display_name,
    avatar_url,
    trust_level,
    is_linuxdo_admin,
    is_app_admin,
    created_at,
    updated_at,
    last_login_at
`

	row := s.db.QueryRowContext(
		ctx,
		query,
		input.LinuxDOUserID,
		strings.ToLower(strings.TrimSpace(input.Username)),
		strings.TrimSpace(input.DisplayName),
		strings.TrimSpace(input.AvatarURL),
		input.TrustLevel,
		boolToInt(input.IsLinuxDOAdmin),
		boolToInt(input.IsAppAdmin),
		formatTime(now),
		formatTime(now),
		formatTime(now),
	)

	return scanUser(row)
}

// GetUserByID 通过本地用户 ID 查询用户。
func (s *Store) GetUserByID(ctx context.Context, userID int64) (model.User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    linuxdo_user_id,
    username,
    display_name,
    avatar_url,
    trust_level,
    is_linuxdo_admin,
    is_app_admin,
    created_at,
    updated_at,
    last_login_at
FROM users
WHERE id = ?
`, userID)

	return scanUser(row)
}

// GetUserByUsername 通过用户名查询用户。
func (s *Store) GetUserByUsername(ctx context.Context, username string) (model.User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    linuxdo_user_id,
    username,
    display_name,
    avatar_url,
    trust_level,
    is_linuxdo_admin,
    is_app_admin,
    created_at,
    updated_at,
    last_login_at
FROM users
WHERE username = ?
`, strings.ToLower(strings.TrimSpace(username)))

	return scanUser(row)
}

// CreateSession 创建一条新的服务端会话记录。
func (s *Store) CreateSession(ctx context.Context, input CreateSessionInput) (model.Session, error) {
	now := time.Now().UTC()

	row := s.db.QueryRowContext(ctx, `
INSERT INTO sessions (
    id,
    user_id,
    csrf_token,
    user_agent_fingerprint,
    expires_at,
    created_at,
    last_seen_at
) VALUES (?, ?, ?, ?, ?, ?, ?)
RETURNING
    id,
    user_id,
    csrf_token,
    user_agent_fingerprint,
    expires_at,
    created_at,
    last_seen_at
`,
		input.ID,
		input.UserID,
		input.CSRFToken,
		input.UserAgentFingerprint,
		formatTime(input.ExpiresAt.UTC()),
		formatTime(now),
		formatTime(now),
	)

	return scanSession(row)
}

// GetSessionWithUserByID 根据会话 ID 读取会话及其对应的用户信息。
func (s *Store) GetSessionWithUserByID(ctx context.Context, sessionID string) (model.Session, model.User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    s.id,
    s.user_id,
    s.csrf_token,
    s.user_agent_fingerprint,
    s.expires_at,
    s.created_at,
    s.last_seen_at,
    u.id,
    u.linuxdo_user_id,
    u.username,
    u.display_name,
    u.avatar_url,
    u.trust_level,
    u.is_linuxdo_admin,
    u.is_app_admin,
    u.created_at,
    u.updated_at,
    u.last_login_at
FROM sessions s
JOIN users u ON u.id = s.user_id
WHERE s.id = ?
`, sessionID)

	return scanSessionWithUser(row)
}

// TouchSession 更新会话最近访问时间。
func (s *Store) TouchSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `
UPDATE sessions
SET last_seen_at = ?
WHERE id = ?
`, formatTime(time.Now().UTC()), sessionID)
	return err
}

// DeleteSession 删除一条会话记录。
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, sessionID)
	return err
}

// SaveOAuthState 保存一次 OAuth 登录态。
func (s *Store) SaveOAuthState(ctx context.Context, state model.OAuthState) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO oauth_states (
    id,
    code_verifier,
    next_path,
    expires_at,
    created_at
) VALUES (?, ?, ?, ?, ?)
`,
		state.ID,
		state.CodeVerifier,
		state.NextPath,
		formatTime(state.ExpiresAt.UTC()),
		formatTime(state.CreatedAt.UTC()),
	)
	return err
}

// ConsumeOAuthState 原子地读取并删除一次性 OAuth state。
func (s *Store) ConsumeOAuthState(ctx context.Context, stateID string) (model.OAuthState, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.OAuthState{}, err
	}
	defer tx.Rollback()

	row := tx.QueryRowContext(ctx, `
SELECT
    id,
    code_verifier,
    next_path,
    expires_at,
    created_at
FROM oauth_states
WHERE id = ?
`, stateID)

	state, err := scanOAuthState(row)
	if err != nil {
		return model.OAuthState{}, err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_states WHERE id = ?`, stateID); err != nil {
		return model.OAuthState{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.OAuthState{}, err
	}

	return state, nil
}

// ListManagedDomains 列出当前系统中的根域名配置。
func (s *Store) ListManagedDomains(ctx context.Context, includeDisabled bool) ([]model.ManagedDomain, error) {
	query := `
SELECT
    id,
    root_domain,
    cloudflare_zone_id,
    default_quota,
    auto_provision,
    is_default,
    enabled,
    created_at,
    updated_at
FROM managed_domains
`
	args := []any{}
	if !includeDisabled {
		query += ` WHERE enabled = 1`
	}
	query += ` ORDER BY is_default DESC, root_domain ASC`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.ManagedDomain
	for rows.Next() {
		item, err := scanManagedDomain(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetManagedDomainByID 通过数据库主键读取一个根域名配置。
func (s *Store) GetManagedDomainByID(ctx context.Context, id int64) (model.ManagedDomain, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    root_domain,
    cloudflare_zone_id,
    default_quota,
    auto_provision,
    is_default,
    enabled,
    created_at,
    updated_at
FROM managed_domains
WHERE id = ?
`, id)

	return scanManagedDomain(row)
}

// GetManagedDomainByRoot 通过根域名字符串读取根域名配置。
func (s *Store) GetManagedDomainByRoot(ctx context.Context, rootDomain string) (model.ManagedDomain, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    id,
    root_domain,
    cloudflare_zone_id,
    default_quota,
    auto_provision,
    is_default,
    enabled,
    created_at,
    updated_at
FROM managed_domains
WHERE root_domain = ?
`, strings.ToLower(strings.TrimSpace(rootDomain)))

	return scanManagedDomain(row)
}

// UpsertManagedDomain 按根域名写入或更新可分发域名配置。
func (s *Store) UpsertManagedDomain(ctx context.Context, input UpsertManagedDomainInput) (model.ManagedDomain, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.ManagedDomain{}, err
	}
	defer tx.Rollback()

	if input.IsDefault {
		if _, err := tx.ExecContext(ctx, `UPDATE managed_domains SET is_default = 0`); err != nil {
			return model.ManagedDomain{}, err
		}
	}

	row := tx.QueryRowContext(ctx, `
INSERT INTO managed_domains (
    root_domain,
    cloudflare_zone_id,
    default_quota,
    auto_provision,
    is_default,
    enabled,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(root_domain) DO UPDATE SET
    cloudflare_zone_id=excluded.cloudflare_zone_id,
    default_quota=excluded.default_quota,
    auto_provision=excluded.auto_provision,
    is_default=excluded.is_default,
    enabled=excluded.enabled,
    updated_at=excluded.updated_at
RETURNING
    id,
    root_domain,
    cloudflare_zone_id,
    default_quota,
    auto_provision,
    is_default,
    enabled,
    created_at,
    updated_at
`,
		strings.ToLower(strings.TrimSpace(input.RootDomain)),
		strings.TrimSpace(input.CloudflareZoneID),
		input.DefaultQuota,
		boolToInt(input.AutoProvision),
		boolToInt(input.IsDefault),
		boolToInt(input.Enabled),
		formatTime(time.Now().UTC()),
		formatTime(time.Now().UTC()),
	)

	item, err := scanManagedDomain(row)
	if err != nil {
		return model.ManagedDomain{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.ManagedDomain{}, err
	}

	return item, nil
}

// SetUserQuota 为用户写入某个根域名上的配额覆盖值。
func (s *Store) SetUserQuota(ctx context.Context, input SetUserQuotaInput) (model.UserDomainQuota, error) {
	row := s.db.QueryRowContext(ctx, `
INSERT INTO user_domain_quotas (
    user_id,
    managed_domain_id,
    max_allocations,
    reason,
    created_at,
    updated_at
) VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT(user_id, managed_domain_id) DO UPDATE SET
    max_allocations=excluded.max_allocations,
    reason=excluded.reason,
    updated_at=excluded.updated_at
RETURNING
    id,
    user_id,
    managed_domain_id,
    max_allocations,
    reason,
    created_at,
    updated_at
`,
		input.UserID,
		input.ManagedDomainID,
		input.MaxAllocations,
		strings.TrimSpace(input.Reason),
		formatTime(time.Now().UTC()),
		formatTime(time.Now().UTC()),
	)

	return scanUserDomainQuota(row)
}

// GetEffectiveQuota 返回用户在指定根域名上的有效配额。
// 如果不存在覆盖值，则回退到根域名默认配额。
func (s *Store) GetEffectiveQuota(ctx context.Context, userID int64, managedDomainID int64) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COALESCE((
    SELECT max_allocations
    FROM user_domain_quotas
    WHERE user_id = ? AND managed_domain_id = ?
), (
    SELECT default_quota
    FROM managed_domains
    WHERE id = ?
))
`, userID, managedDomainID, managedDomainID)

	var quota sql.NullInt64
	if err := row.Scan(&quota); err != nil {
		return 0, err
	}
	if !quota.Valid {
		return 0, sql.ErrNoRows
	}
	return int(quota.Int64), nil
}

// CountAllocationsByUserAndDomain 统计用户在某个根域名下的活动分配数量。
func (s *Store) CountAllocationsByUserAndDomain(ctx context.Context, userID int64, managedDomainID int64) (int, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT COUNT(1)
FROM allocations
WHERE user_id = ? AND managed_domain_id = ? AND status = 'active'
`, userID, managedDomainID)

	var count int
	if err := row.Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

// FindAllocationByNormalizedPrefix 根据标准化后的前缀查找分配记录。
func (s *Store) FindAllocationByNormalizedPrefix(ctx context.Context, managedDomainID int64, normalizedPrefix string) (model.Allocation, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    a.id,
    a.user_id,
    a.managed_domain_id,
    a.prefix,
    a.normalized_prefix,
    a.fqdn,
    a.is_primary,
    a.source,
    a.status,
    a.created_at,
    a.updated_at,
    d.root_domain,
    d.cloudflare_zone_id
FROM allocations a
JOIN managed_domains d ON d.id = a.managed_domain_id
WHERE a.managed_domain_id = ? AND a.normalized_prefix = ?
`, managedDomainID, normalizedPrefix)

	return scanAllocation(row)
}

// CreateAllocation 为某个用户创建一条新的命名空间分配。
func (s *Store) CreateAllocation(ctx context.Context, input CreateAllocationInput) (model.Allocation, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Allocation{}, err
	}
	defer tx.Rollback()

	if input.IsPrimary {
		if _, err := tx.ExecContext(ctx, `
UPDATE allocations
SET is_primary = 0, updated_at = ?
WHERE user_id = ? AND managed_domain_id = ?
`, formatTime(time.Now().UTC()), input.UserID, input.ManagedDomainID); err != nil {
			return model.Allocation{}, err
		}
	}

	row := tx.QueryRowContext(ctx, `
INSERT INTO allocations (
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
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
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
		input.ManagedDomainID,
		input.Prefix,
		input.NormalizedPrefix,
		input.FQDN,
		boolToInt(input.IsPrimary),
		input.Source,
		input.Status,
		formatTime(time.Now().UTC()),
		formatTime(time.Now().UTC()),
	)

	var allocation model.Allocation
	var createdAt string
	var updatedAt string
	var isPrimary int
	if err := row.Scan(
		&allocation.ID,
		&allocation.UserID,
		&allocation.ManagedDomainID,
		&allocation.Prefix,
		&allocation.NormalizedPrefix,
		&allocation.FQDN,
		&isPrimary,
		&allocation.Source,
		&allocation.Status,
		&createdAt,
		&updatedAt,
	); err != nil {
		return model.Allocation{}, err
	}

	allocation.IsPrimary = isPrimary == 1
	if allocation.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Allocation{}, err
	}
	if allocation.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.Allocation{}, err
	}

	if err := tx.Commit(); err != nil {
		return model.Allocation{}, err
	}

	return s.GetAllocationByID(ctx, allocation.ID)
}

// ListAllocationsByUser 返回用户拥有的所有活动分配。
func (s *Store) ListAllocationsByUser(ctx context.Context, userID int64) ([]model.Allocation, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
    a.id,
    a.user_id,
    a.managed_domain_id,
    a.prefix,
    a.normalized_prefix,
    a.fqdn,
    a.is_primary,
    a.source,
    a.status,
    a.created_at,
    a.updated_at,
    d.root_domain,
    d.cloudflare_zone_id
FROM allocations a
JOIN managed_domains d ON d.id = a.managed_domain_id
WHERE a.user_id = ? AND a.status = 'active'
ORDER BY a.is_primary DESC, a.created_at ASC
`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.Allocation
	for rows.Next() {
		item, err := scanAllocation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// ListPublicAllocationOwnerships 返回公开监督页所需的脱敏子域归属列表。
// 该查询只暴露分配的 FQDN 与拥有者身份，不会返回任何 DNS 记录值。
func (s *Store) ListPublicAllocationOwnerships(ctx context.Context) ([]model.PublicAllocationOwnership, error) {
	rows, err := s.db.QueryContext(ctx, `
WITH latest_dns_events AS (
    SELECT
        CAST(json_extract(metadata_json, '$.allocation_id') AS INTEGER) AS allocation_id,
        action,
        ROW_NUMBER() OVER (
            PARTITION BY CAST(json_extract(metadata_json, '$.allocation_id') AS INTEGER)
            ORDER BY created_at DESC, id DESC
        ) AS row_num
    FROM audit_logs
    WHERE resource_type = 'dns_record'
      AND action IN ('dns_record.create', 'dns_record.update', 'dns_record.delete')
      AND json_extract(metadata_json, '$.allocation_id') IS NOT NULL
)
SELECT
    a.fqdn,
    u.username,
    u.display_name
FROM latest_dns_events e
JOIN allocations a ON a.id = e.allocation_id
JOIN users u ON u.id = a.user_id
WHERE e.row_num = 1
  AND e.action IN ('dns_record.create', 'dns_record.update')
  AND a.status = 'active'
ORDER BY a.fqdn ASC, u.username ASC
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []model.PublicAllocationOwnership
	for rows.Next() {
		item, err := scanPublicAllocationOwnership(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetAllocationByID 通过分配主键读取一个分配详情。
func (s *Store) GetAllocationByID(ctx context.Context, allocationID int64) (model.Allocation, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    a.id,
    a.user_id,
    a.managed_domain_id,
    a.prefix,
    a.normalized_prefix,
    a.fqdn,
    a.is_primary,
    a.source,
    a.status,
    a.created_at,
    a.updated_at,
    d.root_domain,
    d.cloudflare_zone_id
FROM allocations a
JOIN managed_domains d ON d.id = a.managed_domain_id
WHERE a.id = ?
`, allocationID)

	return scanAllocation(row)
}

// GetAllocationByIDForUser 确保某条分配属于指定用户。
func (s *Store) GetAllocationByIDForUser(ctx context.Context, allocationID int64, userID int64) (model.Allocation, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT
    a.id,
    a.user_id,
    a.managed_domain_id,
    a.prefix,
    a.normalized_prefix,
    a.fqdn,
    a.is_primary,
    a.source,
    a.status,
    a.created_at,
    a.updated_at,
    d.root_domain,
    d.cloudflare_zone_id
FROM allocations a
JOIN managed_domains d ON d.id = a.managed_domain_id
WHERE a.id = ? AND a.user_id = ?
`, allocationID, userID)

	return scanAllocation(row)
}

// WriteAuditLog 写入一条审计日志。
func (s *Store) WriteAuditLog(ctx context.Context, input AuditLogInput) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO audit_logs (
    actor_user_id,
    action,
    resource_type,
    resource_id,
    metadata_json,
    created_at
) VALUES (?, ?, ?, ?, ?, ?)
`,
		input.ActorUserID,
		input.Action,
		input.ResourceType,
		input.ResourceID,
		input.MetadataJSON,
		formatTime(time.Now().UTC()),
	)
	return err
}

// scanUser 负责把一条用户结果扫描为 model.User。
func scanUser(scanner interface{ Scan(dest ...any) error }) (model.User, error) {
	var item model.User
	var createdAt string
	var updatedAt string
	var lastLoginAt string
	var isLinuxDOAdmin int
	var isAppAdmin int

	err := scanner.Scan(
		&item.ID,
		&item.LinuxDOUserID,
		&item.Username,
		&item.DisplayName,
		&item.AvatarURL,
		&item.TrustLevel,
		&isLinuxDOAdmin,
		&isAppAdmin,
		&createdAt,
		&updatedAt,
		&lastLoginAt,
	)
	if err != nil {
		return model.User{}, err
	}

	item.IsLinuxDOAdmin = isLinuxDOAdmin == 1
	item.IsAppAdmin = isAppAdmin == 1

	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.User{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.User{}, err
	}
	if item.LastLoginAt, err = parseTime(lastLoginAt); err != nil {
		return model.User{}, err
	}

	return item, nil
}

// scanSession 负责把一条会话结果扫描为 model.Session。
func scanSession(scanner interface{ Scan(dest ...any) error }) (model.Session, error) {
	var item model.Session
	var createdAt string
	var expiresAt string
	var lastSeenAt string

	err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.CSRFToken,
		&item.UserAgentFingerprint,
		&expiresAt,
		&createdAt,
		&lastSeenAt,
	)
	if err != nil {
		return model.Session{}, err
	}

	if item.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return model.Session{}, err
	}
	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Session{}, err
	}
	if item.LastSeenAt, err = parseTime(lastSeenAt); err != nil {
		return model.Session{}, err
	}

	return item, nil
}

// scanSessionWithUser 同时扫描会话和用户信息。
func scanSessionWithUser(scanner interface{ Scan(dest ...any) error }) (model.Session, model.User, error) {
	var session model.Session
	var user model.User

	var sessionCreatedAt string
	var sessionExpiresAt string
	var sessionLastSeenAt string

	var userCreatedAt string
	var userUpdatedAt string
	var userLastLoginAt string

	var isLinuxDOAdmin int
	var isAppAdmin int

	err := scanner.Scan(
		&session.ID,
		&session.UserID,
		&session.CSRFToken,
		&session.UserAgentFingerprint,
		&sessionExpiresAt,
		&sessionCreatedAt,
		&sessionLastSeenAt,
		&user.ID,
		&user.LinuxDOUserID,
		&user.Username,
		&user.DisplayName,
		&user.AvatarURL,
		&user.TrustLevel,
		&isLinuxDOAdmin,
		&isAppAdmin,
		&userCreatedAt,
		&userUpdatedAt,
		&userLastLoginAt,
	)
	if err != nil {
		return model.Session{}, model.User{}, err
	}

	user.IsLinuxDOAdmin = isLinuxDOAdmin == 1
	user.IsAppAdmin = isAppAdmin == 1

	if session.ExpiresAt, err = parseTime(sessionExpiresAt); err != nil {
		return model.Session{}, model.User{}, err
	}
	if session.CreatedAt, err = parseTime(sessionCreatedAt); err != nil {
		return model.Session{}, model.User{}, err
	}
	if session.LastSeenAt, err = parseTime(sessionLastSeenAt); err != nil {
		return model.Session{}, model.User{}, err
	}

	if user.CreatedAt, err = parseTime(userCreatedAt); err != nil {
		return model.Session{}, model.User{}, err
	}
	if user.UpdatedAt, err = parseTime(userUpdatedAt); err != nil {
		return model.Session{}, model.User{}, err
	}
	if user.LastLoginAt, err = parseTime(userLastLoginAt); err != nil {
		return model.Session{}, model.User{}, err
	}

	return session, user, nil
}

// scanOAuthState 负责把一条 OAuth state 结果扫描为 model.OAuthState。
func scanOAuthState(scanner interface{ Scan(dest ...any) error }) (model.OAuthState, error) {
	var item model.OAuthState
	var createdAt string
	var expiresAt string

	err := scanner.Scan(
		&item.ID,
		&item.CodeVerifier,
		&item.NextPath,
		&expiresAt,
		&createdAt,
	)
	if err != nil {
		return model.OAuthState{}, err
	}

	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.OAuthState{}, err
	}
	if item.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return model.OAuthState{}, err
	}

	return item, nil
}

// scanManagedDomain 负责把一条根域名配置结果扫描为 model.ManagedDomain。
func scanManagedDomain(scanner interface{ Scan(dest ...any) error }) (model.ManagedDomain, error) {
	var item model.ManagedDomain
	var createdAt string
	var updatedAt string
	var autoProvision int
	var isDefault int
	var enabled int

	err := scanner.Scan(
		&item.ID,
		&item.RootDomain,
		&item.CloudflareZoneID,
		&item.DefaultQuota,
		&autoProvision,
		&isDefault,
		&enabled,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.ManagedDomain{}, err
	}

	item.AutoProvision = autoProvision == 1
	item.IsDefault = isDefault == 1
	item.Enabled = enabled == 1

	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.ManagedDomain{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.ManagedDomain{}, err
	}

	return item, nil
}

// scanUserDomainQuota 负责把一条用户配额结果扫描为 model.UserDomainQuota。
func scanUserDomainQuota(scanner interface{ Scan(dest ...any) error }) (model.UserDomainQuota, error) {
	var item model.UserDomainQuota
	var createdAt string
	var updatedAt string

	err := scanner.Scan(
		&item.ID,
		&item.UserID,
		&item.ManagedDomainID,
		&item.MaxAllocations,
		&item.Reason,
		&createdAt,
		&updatedAt,
	)
	if err != nil {
		return model.UserDomainQuota{}, err
	}

	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.UserDomainQuota{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.UserDomainQuota{}, err
	}

	return item, nil
}

// scanAllocation 负责把一条分配结果扫描为 model.Allocation。
func scanAllocation(scanner interface{ Scan(dest ...any) error }) (model.Allocation, error) {
	var item model.Allocation
	var createdAt string
	var updatedAt string
	var isPrimary int

	err := scanner.Scan(
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
		&item.RootDomain,
		&item.CloudflareZoneID,
	)
	if err != nil {
		return model.Allocation{}, err
	}

	item.IsPrimary = isPrimary == 1

	if item.CreatedAt, err = parseTime(createdAt); err != nil {
		return model.Allocation{}, err
	}
	if item.UpdatedAt, err = parseTime(updatedAt); err != nil {
		return model.Allocation{}, err
	}

	return item, nil
}

// scanPublicAllocationOwnership 负责把一条公开监督结果扫描为脱敏归属结构。
func scanPublicAllocationOwnership(scanner interface{ Scan(dest ...any) error }) (model.PublicAllocationOwnership, error) {
	var item model.PublicAllocationOwnership
	if err := scanner.Scan(
		&item.FQDN,
		&item.OwnerUsername,
		&item.OwnerDisplayName,
	); err != nil {
		return model.PublicAllocationOwnership{}, err
	}
	return item, nil
}

// formatTime 统一把时间格式化为数据库存储使用的 RFC3339Nano 文本。
func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

// parseTime 把数据库中的 RFC3339Nano 文本恢复为 time.Time。
func parseTime(raw string) (time.Time, error) {
	value, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", raw, err)
	}
	return value.UTC(), nil
}

// boolToInt 把布尔值转成 SQLite 中更容易处理的 0/1。
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

// IsNotFound 用于统一判断是否是底层 `sql.ErrNoRows`。
func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
