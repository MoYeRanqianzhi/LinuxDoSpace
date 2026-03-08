// AdminTabKey enumerates the top-level tabs rendered by the standalone admin console.
export type AdminTabKey = 'users' | 'domains' | 'emails' | 'applications' | 'redeem';

// UserStatus is the simplified moderation status displayed in the admin UI.
export type UserStatus = 'active' | 'banned';

// ApplicationStatus mirrors the backend moderation request lifecycle.
export type ApplicationStatus = 'pending' | 'approved' | 'rejected';

// RedeemPermissionType mirrors the backend redeem code type field.
export type RedeemPermissionType = 'single' | 'multiple' | 'wildcard';

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
  status: string;
  cloudflare_zone_id: string;
  created_at: string;
  updated_at: string;
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

// AdminApplicationRecord mirrors one moderation request row.
export interface AdminApplicationRecord {
  id: number;
  applicant_user_id: number;
  applicant_username: string;
  applicant_name: string;
  type: RedeemPermissionType;
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

// GenerateRedeemCodesInput mirrors the batch generation payload.
export interface GenerateRedeemCodesInput {
  amount: number;
  type: RedeemPermissionType;
  target: string;
  note: string;
}
