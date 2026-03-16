package model

import "time"

// User 表示已经通过 Linux Do OAuth 登录并落库的站内用户。
type User struct {
	ID             int64     `json:"id"`
	LinuxDOUserID  int64     `json:"linuxdo_user_id"`
	Username       string    `json:"username"`
	DisplayName    string    `json:"display_name"`
	AvatarURL      string    `json:"avatar_url"`
	TrustLevel     int       `json:"trust_level"`
	IsLinuxDOAdmin bool      `json:"is_linuxdo_admin"`
	IsAppAdmin     bool      `json:"is_app_admin"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LastLoginAt    time.Time `json:"last_login_at"`
}

// Session 表示服务端持久化保存的登录会话。
type Session struct {
	ID                   string     `json:"id"`
	UserID               int64      `json:"user_id"`
	CSRFToken            string     `json:"csrf_token"`
	UserAgentFingerprint string     `json:"-"`
	AdminVerifiedAt      *time.Time `json:"admin_verified_at,omitempty"`
	ExpiresAt            time.Time  `json:"expires_at"`
	CreatedAt            time.Time  `json:"created_at"`
	LastSeenAt           time.Time  `json:"last_seen_at"`
}

// OAuthState 表示 OAuth 登录流程中的一次性状态记录。
type OAuthState struct {
	ID           string    `json:"id"`
	CodeVerifier string    `json:"-"`
	NextPath     string    `json:"next_path"`
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// ManagedDomain 表示平台允许分发的一个根域名。
type ManagedDomain struct {
	ID                 int64     `json:"id"`
	RootDomain         string    `json:"root_domain"`
	CloudflareZoneID   string    `json:"cloudflare_zone_id"`
	DefaultQuota       int       `json:"default_quota"`
	AutoProvision      bool      `json:"auto_provision"`
	IsDefault          bool      `json:"is_default"`
	Enabled            bool      `json:"enabled"`
	SaleEnabled        bool      `json:"sale_enabled"`
	SaleBasePriceCents int64     `json:"sale_base_price_cents"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

// UserDomainQuota 表示某个用户在某个根域名上的配额覆盖值。
type UserDomainQuota struct {
	ID              int64     `json:"id"`
	UserID          int64     `json:"user_id"`
	ManagedDomainID int64     `json:"managed_domain_id"`
	MaxAllocations  int       `json:"max_allocations"`
	Reason          string    `json:"reason"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// Allocation 表示某个用户持有的一个二级域名命名空间。
// 例如用户持有 `alice.linuxdo.space` 时，整个 `*.alice.linuxdo.space` 命名空间都由它管理。
type Allocation struct {
	ID               int64     `json:"id"`
	UserID           int64     `json:"user_id"`
	ManagedDomainID  int64     `json:"managed_domain_id"`
	Prefix           string    `json:"prefix"`
	NormalizedPrefix string    `json:"normalized_prefix"`
	FQDN             string    `json:"fqdn"`
	IsPrimary        bool      `json:"is_primary"`
	Source           string    `json:"source"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
	RootDomain       string    `json:"root_domain,omitempty"`
	CloudflareZoneID string    `json:"cloudflare_zone_id,omitempty"`
}

// PublicAllocationOwnership 表示公开监督页使用的脱敏归属信息。
// 该结构只返回“哪个子域名由谁持有”，绝不包含具体解析值、IP、CNAME 目标等隐私数据。
type PublicAllocationOwnership struct {
	FQDN             string `json:"fqdn"`
	OwnerUsername    string `json:"owner_username"`
	OwnerDisplayName string `json:"owner_display_name"`
}

// AuditLog 表示关键动作的审计事件。
type AuditLog struct {
	ID           int64     `json:"id"`
	ActorUserID  *int64    `json:"actor_user_id,omitempty"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	MetadataJSON string    `json:"metadata_json"`
	CreatedAt    time.Time `json:"created_at"`
}

// LinuxDOProfile 表示从 Linux Do Connect 用户信息接口解析出的用户信息。
type LinuxDOProfile struct {
	ID             int64  `json:"id"`
	Username       string `json:"username"`
	Name           string `json:"name"`
	AvatarTemplate string `json:"avatar_template"`
	TrustLevel     int    `json:"trust_level"`
	Admin          bool   `json:"admin"`
}

// DNSRecord 表示 Cloudflare 返回的 DNS 记录，并附带站内相对名称。
type DNSRecord struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	Name         string `json:"name"`
	RelativeName string `json:"relative_name"`
	Content      string `json:"content"`
	TTL          int    `json:"ttl"`
	Proxied      bool   `json:"proxied"`
	Comment      string `json:"comment"`
	Priority     *int   `json:"priority,omitempty"`
}
