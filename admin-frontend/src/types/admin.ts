// AdminTabKey enumerates the top-level tabs rendered by the standalone admin console.
export type AdminTabKey = 'users' | 'domains' | 'emails' | 'applications' | 'orders' | 'redeem';

// UserStatus is the simplified moderation status displayed in the admin UI.
export type UserStatus = 'active' | 'banned';

// ApplicationStatus mirrors the backend moderation request lifecycle.
export type ApplicationStatus = 'pending' | 'approved' | 'rejected';

// AllocationStatus mirrors the administrator-controlled allocation lifecycle.
export type AllocationStatus = 'active' | 'disabled';

// RedeemPermissionType mirrors the backend redeem code type field.
export type RedeemPermissionType = 'single' | 'multiple' | 'wildcard';

// AdminPermissionType extends redeem types with the user-facing permission keys
// that can also appear inside administrator application records.
export type AdminPermissionType = RedeemPermissionType | 'email_catch_all';

// AdminUser mirrors the authenticated user/session payload returned by the backend.
export interface AdminUser {
  id: number;
  linuxdo_user_id: number;
  username: string;
  display_name: string;
  avatar_url: string;
  trust_level: number;
  is_linuxdo_admin: boolean;
  is_app_admin: boolean;
}

// ManagedDomain mirrors one managed root-domain configuration row.
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

// AdminSessionResponse mirrors the administrator session bootstrap payload.
export interface AdminSessionResponse {
  authenticated: boolean;
  authorized: boolean;
  password_verified: boolean;
  oauth_configured: boolean;
  user?: AdminUser;
  csrf_token?: string;
  session_expires_at?: string;
  admin_verified_at?: string;
  managed_domains?: ManagedDomain[];
}

// AdminUserRecord mirrors one row returned by the admin user list endpoint.
export interface AdminUserRecord {
  id: number;
  linuxdo_user_id: number;
  username: string;
  display_name: string;
  avatar_url: string;
  trust_level: number;
  is_linuxdo_admin: boolean;
  is_app_admin: boolean;
  is_banned: boolean;
  allocation_count: number;
  created_at: string;
  last_login_at: string;
}

// AdminUserQuota mirrors one quota row inside the admin user detail payload.
export interface AdminUserQuota {
  managed_domain_id: number;
  root_domain: string;
  default_quota: number;
  effective_quota: number;
  allocation_count: number;
}

// AdminUserDetail mirrors the expanded admin user detail payload.
export interface AdminUserDetail {
  user: AdminUserRecord;
  ban_note: string;
  quotas: AdminUserQuota[];
}

// AdminAllocationRecord mirrors one allocation namespace row for administrators.
export interface AdminAllocationRecord {
  id: number;
  user_id: number;
  owner_username: string;
  owner_display_name: string;
  managed_domain_id: number;
  root_domain: string;
  prefix: string;
  normalized_prefix: string;
  fqdn: string;
  is_primary: boolean;
  source: string;
  status: AllocationStatus;
  cloudflare_zone_id: string;
  created_at: string;
  updated_at: string;
}

// CreateAdminAllocationInput mirrors the payload accepted by the allocation
// creation endpoint.
export interface CreateAdminAllocationInput {
  owner_user_id: number;
  root_domain: string;
  prefix: string;
  is_primary: boolean;
  source: string;
  status: AllocationStatus;
}

// UpdateAdminAllocationInput mirrors the mutable administrator allocation
// controls accepted by the allocation update endpoint.
export interface UpdateAdminAllocationInput {
  owner_user_id?: number;
  is_primary?: boolean;
  source?: string;
  status?: AllocationStatus;
}

// AdminDomainRecord mirrors one DNS record row visible to administrators.
export interface AdminDomainRecord {
  allocation_id: number;
  owner_user_id: number;
  owner_username: string;
  owner_display_name: string;
  root_domain: string;
  namespace_fqdn: string;
  id: string;
  type: 'A' | 'AAAA' | 'CNAME' | 'TXT' | 'MX';
  name: string;
  relative_name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority?: number;
}

// UpsertAdminDomainRecordInput mirrors the payload accepted by the admin DNS record endpoints.
export interface UpsertAdminDomainRecordInput {
  type: AdminDomainRecord['type'];
  name: string;
  content: string;
  ttl: number;
  proxied: boolean;
  comment: string;
  priority?: number;
}

// AdminEmailRecord mirrors one stored email forwarding rule.
export interface AdminEmailRecord {
  id: number;
  owner_user_id: number;
  owner_username: string;
  owner_display_name: string;
  root_domain: string;
  prefix: string;
  target_email: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

// UpsertEmailRouteInput mirrors the email route create/update payload.
export interface UpsertEmailRouteInput {
  owner_user_id: number;
  root_domain: string;
  prefix: string;
  target_email: string;
  enabled: boolean;
}

// UpdateEmailRouteInput mirrors the actually mutable fields accepted by the
// email route PATCH endpoint.
export interface UpdateEmailRouteInput {
  target_email: string;
  enabled: boolean;
}

// AdminApplicationRecord mirrors one moderation request row.
export interface AdminApplicationRecord {
  id: number;
  applicant_user_id: number;
  applicant_username: string;
  applicant_name: string;
  type: AdminPermissionType;
  target: string;
  reason: string;
  status: ApplicationStatus;
  review_note: string;
  reviewed_by_user_id?: number;
  reviewed_at?: string;
  created_at: string;
  updated_at: string;
}

// AdminRedeemCodeRecord mirrors one generated redeem code row.
export interface AdminRedeemCodeRecord {
  id: number;
  code: string;
  type: RedeemPermissionType;
  target: string;
  note: string;
  created_by_user_id: number;
  created_by_username: string;
  used_by_user_id?: number;
  used_by_username?: string;
  created_at: string;
  used_at?: string;
}

// UpsertManagedDomainInput mirrors the payload accepted by the managed-domain endpoint.
export interface UpsertManagedDomainInput {
  root_domain: string;
  cloudflare_zone_id: string;
  default_quota: number;
  auto_provision: boolean;
  is_default: boolean;
  enabled: boolean;
}

// SetUserQuotaInput mirrors the quota override payload.
export interface SetUserQuotaInput {
  username: string;
  root_domain: string;
  max_allocations: number;
  reason: string;
}

// UpdateAdminUserInput mirrors the moderation control payload for one user.
export interface UpdateAdminUserInput {
  is_banned: boolean;
  ban_note: string;
}

// UpdateApplicationInput mirrors the moderation update payload for one request.
export interface UpdateApplicationInput {
  status: ApplicationStatus;
  review_note: string;
}

// PermissionPolicy mirrors one administrator-editable permission policy row.
export interface PermissionPolicy {
  key: string;
  display_name: string;
  description: string;
  enabled: boolean;
  auto_approve: boolean;
  min_trust_level: number;
  default_daily_limit?: number;
  created_at: string;
  updated_at: string;
}

// UpdatePermissionPolicyInput mirrors the PATCH payload accepted by the
// permission-policy update endpoint.
export interface UpdatePermissionPolicyInput {
  enabled?: boolean;
  auto_approve?: boolean;
  min_trust_level?: number;
  default_daily_limit?: number;
}

// AdminPermissionApplicationSummary mirrors the latest application snapshot for
// one user permission card rendered inside the admin user editor.
export interface AdminPermissionApplicationSummary {
  id: number;
  status: ApplicationStatus;
  reason: string;
  review_note: string;
  reviewed_at?: string;
  created_at: string;
  updated_at: string;
}

// AdminUserPermission mirrors one permission card returned for a specific user.
export interface AdminUserPermission {
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
  status: 'not_requested' | ApplicationStatus;
  can_apply: boolean;
  can_manage_route: boolean;
  application?: AdminPermissionApplicationSummary;
}

// SetAdminUserPermissionInput mirrors the direct administrator override payload.
export interface SetAdminUserPermissionInput {
  status: ApplicationStatus;
  review_note: string;
  reason?: string;
}

// GenerateRedeemCodesInput mirrors the batch generation payload.
export interface GenerateRedeemCodesInput {
  amount: number;
  type: RedeemPermissionType;
  target: string;
  note: string;
}

// PaymentProduct mirrors one administrator-editable Linux Do Credit product.
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

// UpdatePaymentProductInput mirrors the editable subset of one LDC product row.
export interface UpdatePaymentProductInput {
  enabled?: boolean;
  unit_price?: string;
  grant_quantity?: number;
}

// PaymentOrderStatus mirrors the backend payment-order lifecycle.
export type PaymentOrderStatus = 'created' | 'pending' | 'paid' | 'failed' | 'refunded';

// AdminPaymentOrder mirrors one LDC order row visible to the administrator
// console.
export interface AdminPaymentOrder {
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
  payment_url: string;
  paid_at?: string;
  applied_at?: string;
  last_checked_at?: string;
  created_at: string;
  updated_at: string;
}
