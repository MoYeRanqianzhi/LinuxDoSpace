import type {
  AdminApplicationRecord,
  AdminDomainRecord,
  AdminEmailRecord,
  AdminRedeemCodeRecord,
  AdminUserRecord,
} from '../types/admin';

// 这些假数据用于把管理员设计稿先独立为可部署工程。
// 等后台管理 API 就绪后，可以按类型逐步替换为真实请求。
export const mockUsers: AdminUserRecord[] = [
  { id: 1, username: 'moyer', email: 'moyer@linuxdo.space', status: 'active', registeredAt: '2026-03-01' },
  { id: 2, username: 'alice', email: 'alice@example.com', status: 'active', registeredAt: '2026-03-04' },
  { id: 3, username: 'omega', email: 'omega@example.com', status: 'banned', registeredAt: '2026-03-06' },
];

export const mockDomains: AdminDomainRecord[] = [
  {
    id: 1,
    owner: 'moyer',
    hostname: 'moyer.linuxdo.space',
    type: 'A',
    content: '203.0.113.10',
    proxied: true,
    createdAt: '2026-03-01',
  },
  {
    id: 2,
    owner: 'alice',
    hostname: 'blog.linuxdo.space',
    type: 'CNAME',
    content: 'cname.vercel-dns.com',
    proxied: false,
    createdAt: '2026-03-05',
  },
  {
    id: 3,
    owner: 'omega',
    hostname: 'lab.linuxdo.space',
    type: 'AAAA',
    content: '2001:db8::42',
    proxied: true,
    createdAt: '2026-03-07',
  },
];

export const mockEmails: AdminEmailRecord[] = [
  { id: 1, owner: 'moyer', prefix: 'hello', target: 'moyer@example.com', createdAt: '2026-03-01' },
  { id: 2, owner: 'alice', prefix: 'contact', target: 'alice@example.com', createdAt: '2026-03-05' },
  { id: 3, owner: 'omega', prefix: 'ops', target: 'omega@example.com', createdAt: '2026-03-06' },
];

export const mockApplications: AdminApplicationRecord[] = [
  {
    id: 1,
    applicant: 'alice',
    type: 'single',
    target: 'api.linuxdo.space',
    reason: '希望为开源 API 服务申请一个固定二级域名，便于社区调用与文档维护。',
    status: 'pending',
    appliedAt: '2026-03-06',
  },
  {
    id: 2,
    applicant: 'beta',
    type: 'wildcard',
    target: '*.dev.linuxdo.space',
    reason: '需要为多租户测试环境准备统一的开发子域名。',
    status: 'approved',
    appliedAt: '2026-03-05',
  },
  {
    id: 3,
    applicant: 'omega',
    type: 'multiple',
    target: '10 次追加额度',
    reason: '需要批量测试多种解析场景，但当前说明不够充分。',
    status: 'rejected',
    appliedAt: '2026-03-04',
  },
];

export const mockRedeemCodes: AdminRedeemCodeRecord[] = [
  {
    id: 1,
    code: 'LINUXDO-2026-ALPHA1',
    type: 'single',
    target: 'api.linuxdo.space',
    usedBy: null,
    createdAt: '2026-03-06',
  },
  {
    id: 2,
    code: 'LINUXDO-2026-MULTI5',
    type: 'multiple',
    target: '5 次额度',
    usedBy: 'alice',
    createdAt: '2026-03-05',
  },
  {
    id: 3,
    code: 'LINUXDO-2026-WILD01',
    type: 'wildcard',
    target: '*.dev.linuxdo.space',
    usedBy: null,
    createdAt: '2026-03-04',
  },
];
