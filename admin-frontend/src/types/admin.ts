// AdminTabKey 描述管理员端顶部导航使用的全部标签页。
export type AdminTabKey = 'users' | 'domains' | 'emails' | 'applications' | 'redeem';

// UserStatus 仅用于管理员 UI 的演示状态切换。
export type UserStatus = 'active' | 'banned';

// ApplicationStatus 表示特批申请的当前审核进度。
export type ApplicationStatus = 'pending' | 'approved' | 'rejected';

// RedeemPermissionType 表示兑换码对应的授权范围。
export type RedeemPermissionType = 'single' | 'multiple' | 'wildcard';

// AdminUserRecord 描述用户管理页中的一行数据。
export interface AdminUserRecord {
  id: number;
  username: string;
  email: string;
  status: UserStatus;
  registeredAt: string;
}

// AdminDomainRecord 描述域名管理页中的一行数据。
export interface AdminDomainRecord {
  id: number;
  owner: string;
  hostname: string;
  type: 'A' | 'AAAA' | 'CNAME' | 'TXT';
  content: string;
  proxied: boolean;
  createdAt: string;
}

// AdminEmailRecord 描述邮箱转发管理页的一行数据。
export interface AdminEmailRecord {
  id: number;
  owner: string;
  prefix: string;
  target: string;
  createdAt: string;
}

// AdminApplicationRecord 描述权限申请页中的审核对象。
export interface AdminApplicationRecord {
  id: number;
  applicant: string;
  type: RedeemPermissionType;
  target: string;
  reason: string;
  status: ApplicationStatus;
  appliedAt: string;
}

// AdminRedeemCodeRecord 描述兑换码页面中的单条兑换码数据。
export interface AdminRedeemCodeRecord {
  id: number;
  code: string;
  type: RedeemPermissionType;
  target: string;
  usedBy: string | null;
  createdAt: string;
}
