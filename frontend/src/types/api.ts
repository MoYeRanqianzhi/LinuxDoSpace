// 本文件集中定义前端与后端之间共享的数据结构。
// 这样做的目的是让页面组件不直接依赖“猜出来的字段”，而是基于明确的接口契约开发。

// User 表示后端 `/v1/me` 返回的当前登录用户信息。
export interface User {
  id: number;
  linuxdo_user_id: number;
  username: string;
  display_name: string;
  avatar_url: string;
  trust_level: number;
  is_linuxdo_admin: boolean;
  is_app_admin: boolean;
  created_at: string;
  updated_at: string;
  last_login_at: string;
}

// ManagedDomain 表示一个允许平台分发的根域名。
export interface ManagedDomain {
  id: number;
  root_domain: string;
  cloudflare_zone_id: string;
  default_quota: number;
  auto_provision: boolean;
  is_default: boolean;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

// Allocation 表示用户持有的一个命名空间分配。
export interface Allocation {
  id: number;
  user_id: number;
  managed_domain_id: number;
  prefix: string;
  normalized_prefix: string;
  fqdn: string;
  is_primary: boolean;
  source: string;
  status: string;
  created_at: string;
  updated_at: string;
  root_domain?: string;
  cloudflare_zone_id?: string;
}

// DNSRecord 表示某个命名空间下的一条 Cloudflare DNS 记录。
export interface DNSRecord {
  id: string;
  type: string;
  name: string;
  relative_name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority?: number;
  is_placeholder?: boolean;
}

// AvailabilityResult 表示某个前缀在指定根域名下的可分配检查结果。
export interface AvailabilityResult {
  root_domain: string;
  prefix: string;
  normalized_prefix: string;
  fqdn: string;
  available: boolean;
  reasons: string[];
}

// SupervisionEntry 表示公开监督页中的一条脱敏归属记录。
// 这里只展示子域名和拥有者，不包含任何解析值。
export interface SupervisionEntry {
  fqdn: string;
  owner_username: string;
  owner_display_name: string;
}

// PermissionStatus mirrors the user-visible lifecycle of one permission request.
export type PermissionStatus = 'not_requested' | 'pending' | 'approved' | 'rejected';

// EmailRouteKind mirrors the different mailbox rows shown on the public email page.
export type EmailRouteKind = 'default' | 'custom' | 'catch_all';

// PermissionApplicationSummary mirrors the latest application snapshot returned
// to the public frontend for one permission card.
export interface PermissionApplicationSummary {
  id: number;
  target?: string;
  status: Exclude<PermissionStatus, 'not_requested'>;
  reason: string;
  review_note: string;
  reviewed_at?: string;
  created_at: string;
  updated_at: string;
}

// UserPermission mirrors one user-visible permission card returned by the
// backend user-facing permission endpoints.
export interface UserPermission {
  key: string;
  display_name: string;
  description: string;
  target: string;
  pledge_text: string;
  policy_enabled: boolean;
  auto_approve: boolean;
  min_trust_level: number;
  eligible: boolean;
  eligibility_reasons: string[];
  status: PermissionStatus;
  can_apply: boolean;
  can_manage_route: boolean;
  application?: PermissionApplicationSummary;
}

// EmailRouteAvailabilityResult mirrors the public mailbox search result.
export interface EmailRouteAvailabilityResult {
  root_domain: string;
  prefix: string;
  normalized_prefix: string;
  address: string;
  available: boolean;
  reasons: string[];
}

// EmailTargetVerificationStatus mirrors the Cloudflare destination-email
// verification state enforced by the backend.
export type EmailTargetVerificationStatus = 'pending' | 'verified';

// UserEmailTarget mirrors one user-owned forwarding destination email.
export interface UserEmailTarget {
  id: number;
  email: string;
  cloudflare_address_id?: string;
  verification_status: EmailTargetVerificationStatus;
  verified: boolean;
  verified_at?: string;
  last_verification_sent_at?: string;
  created_at: string;
  updated_at: string;
}

// UserEmailRoute mirrors one user-visible email forwarding row.
export interface UserEmailRoute {
  id?: number;
  kind: EmailRouteKind;
  permission_key?: string;
  display_name: string;
  description: string;
  address: string;
  prefix: string;
  root_domain: string;
  target_email: string;
  enabled: boolean;
  configured: boolean;
  permission_status?: PermissionStatus;
  can_manage: boolean;
  can_delete: boolean;
  updated_at?: string;
}

// MeResponse 表示 `/v1/me` 返回的数据结构。
export interface MeResponse {
  authenticated: boolean;
  oauth_configured?: boolean;
  user?: User;
  csrf_token?: string;
  session_expires_at?: string;
  allocations?: Allocation[];
}

// APIEnvelope 表示后端成功响应时统一包裹的 `data` 外层。
export interface APIEnvelope<T> {
  data: T;
}

// APIErrorBody 表示后端失败响应时统一包裹的 `error` 外层。
export interface APIErrorBody {
  error: {
    code: string;
    message: string;
  };
}

// CreateAllocationInput 表示创建命名空间分配时的请求体。
export interface CreateAllocationInput {
  root_domain: string;
  prefix: string;
  source?: string;
  primary?: boolean;
}

// UpsertDNSRecordInput 表示创建或更新 DNS 记录时的请求体。
export interface UpsertDNSRecordInput {
  type: string;
  name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority?: number;
}

// SubmitPermissionApplicationInput mirrors the currently supported permission
// application payload.
export interface SubmitPermissionApplicationInput {
  key: string;
}

// UpsertMyDefaultEmailRouteInput mirrors the default mailbox save payload.
export interface UpsertMyDefaultEmailRouteInput {
  target_email: string;
  enabled: boolean;
}

// UpsertMyCatchAllEmailRouteInput mirrors the email-forwarding form payload.
export interface UpsertMyCatchAllEmailRouteInput {
  target_email: string;
  enabled: boolean;
}

// CreateMyEmailTargetInput mirrors the user-authored request to bind one new
// forwarding destination email to the current account.
export interface CreateMyEmailTargetInput {
  email: string;
}
