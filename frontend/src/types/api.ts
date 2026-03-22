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
  default_quota: number;
  auto_provision: boolean;
  is_default: boolean;
  enabled: boolean;
  sale_enabled: boolean;
  sale_base_price_cents: number;
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

// SpecialDNSRecordType is the synthetic record family exposed by the DNS panel
// for platform-managed capabilities that do not map 1:1 to one raw Cloudflare
// record row.
export type SpecialDNSRecordType = 'EMAIL_CATCH_ALL';

// ManualDNSRecordType limits the public DNS console to user-creatable record
// families. MX stays reserved for the platform's own mail-relay bootstrap,
// while EMAIL_CATCH_ALL is a synthetic toggle that hides the real relay MX/TXT.
export type ManualDNSRecordType = 'A' | 'AAAA' | 'CNAME' | 'TXT' | SpecialDNSRecordType;

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
export type EmailRouteTargetType = 'email' | 'api_token';

// PaymentOrderStatus mirrors the lifecycle of one Linux Do Credit checkout.
export type PaymentOrderStatus = 'created' | 'pending' | 'paid' | 'failed' | 'refunded';
export type PaymentOrderFulfillmentStatus = 'pending' | 'applied' | 'failed';

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
  catch_all_access?: {
    access_mode: string;
    subscription_active: boolean;
    subscription_expires_at?: string;
    remaining_count: number;
    permanent_remaining_count: number;
    temporary_reward_count: number;
    temporary_reward_expires_at?: string;
    daily_usage_date: string;
    daily_used_count: number;
    daily_remaining_count: number;
    effective_daily_limit: number;
    has_access: boolean;
    delivery_available: boolean;
  };
  application?: PermissionApplicationSummary;
}

// PaymentProduct mirrors one administrator-configurable Linux Do Credit item.
export interface PaymentProduct {
  key: string;
  display_name: string;
  description: string;
  enabled: boolean;
  unit_price_cents: number;
  grant_quantity: number;
  grant_unit: string;
  effect_type: string;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

// PaymentOrder mirrors one locally tracked Linux Do Credit checkout.
export interface PaymentOrder {
  id: number;
  user_id: number;
  username: string;
  display_name: string;
  product_key: string;
  product_name: string;
  title: string;
  gateway_type: string;
  out_trade_no: string;
  provider_trade_no: string;
  status: PaymentOrderStatus;
  units: number;
  grant_quantity: number;
  granted_total: number;
  grant_unit: string;
  unit_price_cents: number;
  total_price_cents: number;
  effect_type: string;
  purchase_root_domain?: string;
  purchase_mode?: string;
  purchase_prefix?: string;
  purchase_normalized_prefix?: string;
  purchase_requested_length?: number;
  purchase_assigned_prefix?: string;
  purchase_assigned_fqdn?: string;
  payment_url: string;
  fulfillment_status: PaymentOrderFulfillmentStatus;
  fulfillment_error?: string;
  fulfillment_failed_at?: string;
  paid_at?: string;
  applied_at?: string;
  last_checked_at?: string;
  created_at: string;
  updated_at: string;
}

// POWBenefitOption mirrors one currently selectable proof-of-work reward type.
export interface POWBenefitOption {
  key: string;
  display_name: string;
  description: string;
  reward_unit: string;
  enabled: boolean;
}

// POWDifficultyOption mirrors one selectable proof-of-work difficulty entry.
export interface POWDifficultyOption {
  value: number;
  label: string;
  description: string;
  reward_multiplier: number;
  enabled: boolean;
}

// POWChallenge mirrors one backend-generated proof-of-work puzzle row.
export interface POWChallenge {
  id: number;
  benefit_key: string;
  benefit_display_name: string;
  difficulty: number;
  base_reward: number;
  reward_quantity: number;
  reward_unit: string;
  challenge_token: string;
  salt_hex: string;
  argon2_variant: string;
  argon2_memory_kib: number;
  argon2_iterations: number;
  argon2_parallelism: number;
  argon2_hash_length: number;
  status: string;
  claimed_at?: string;
  created_at: string;
  updated_at: string;
}

// POWStatus mirrors the full proof-of-work dashboard state returned by the backend.
export interface POWStatus {
  feature_enabled: boolean;
  benefits: POWBenefitOption[];
  difficulty_options: POWDifficultyOption[];
  max_daily_completions: number;
  completed_today: number;
  remaining_today: number;
  current_remaining_count: number;
  current_permanent_remaining_count: number;
  current_temporary_reward_count: number;
  current_temporary_reward_expires_at?: string;
  current_challenge?: POWChallenge;
}

// SubmitPOWChallengeResult mirrors the successful claim payload returned after
// the backend verifies and grants one proof-of-work reward.
export interface SubmitPOWChallengeResult {
  challenge: POWChallenge;
  granted_quantity: number;
  reward_unit: string;
  current_remaining_count: number;
  current_permanent_remaining_count: number;
  current_temporary_reward_count: number;
  current_temporary_reward_expires_at?: string;
  completed_today: number;
  remaining_today: number;
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

// EmailTargetVerificationStatus mirrors the platform-owned target-email
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
  target_type: EmailRouteTargetType;
  target_email: string;
  target_token_public_id?: string;
  target_token_name?: string;
  target_display: string;
  enabled: boolean;
  configured: boolean;
  permission_status?: PermissionStatus;
  can_manage: boolean;
  can_delete: boolean;
  updated_at?: string;
}

// UserAPIToken mirrors one user-managed bearer token that can receive live email events.
export interface UserAPIToken {
  id: number;
  name: string;
  public_id: string;
  scopes: string[];
  email_enabled: boolean;
  last_used_at?: string;
  created_at: string;
  updated_at: string;
  revoked_at?: string;
}

// CreateMyAPITokenResult mirrors the create-token response where the raw secret is returned once.
export interface CreateMyAPITokenResult {
  token: UserAPIToken;
  raw_token: string;
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
  type: ManualDNSRecordType;
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
  target_type?: EmailRouteTargetType;
  target_email: string;
  target_token_public_id?: string;
  enabled: boolean;
}

// UpsertMyCatchAllEmailRouteInput mirrors the email-forwarding form payload.
export interface UpsertMyCatchAllEmailRouteInput {
  target_type?: EmailRouteTargetType;
  target_email: string;
  target_token_public_id?: string;
  enabled: boolean;
}

// CreateMyEmailTargetInput mirrors the user-authored request to bind one new
// forwarding destination email to the current account.
export interface CreateMyEmailTargetInput {
  email: string;
}

// CreateMyAPITokenInput mirrors the user-authored request to issue one new API token.
export interface CreateMyAPITokenInput {
  name: string;
  email_enabled: boolean;
}

// CreatePaymentOrderInput mirrors one authenticated checkout request.
export interface CreatePaymentOrderInput {
  product_key: string;
  units: number;
}

// CreateDomainPurchaseOrderInput mirrors one paid namespace checkout request
// created directly from the public domain search page.
export interface CreateDomainPurchaseOrderInput {
  root_domain: string;
  mode: 'exact' | 'random';
  prefix?: string;
  random_length?: number;
}

// CreatePOWChallengeInput mirrors one authenticated request to replace the
// current proof-of-work challenge.
export interface CreatePOWChallengeInput {
  benefit_key: string;
  difficulty: number;
}

// SubmitPOWChallengeInput mirrors one browser-computed nonce candidate that
// should be verified by the backend.
export interface SubmitPOWChallengeInput {
  challenge_id: number;
  nonce: string;
}
