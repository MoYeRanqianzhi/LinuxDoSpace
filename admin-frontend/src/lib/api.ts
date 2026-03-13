import type {
  AdminAllocationRecord,
  AdminApplicationRecord,
  CreateAdminAllocationInput,
  AdminDomainRecord,
  AdminEmailRecord,
  AdminPaymentOrder,
  AdminRedeemCodeRecord,
  AdminSessionResponse,
  UpdateAdminAllocationInput,
  AdminUserPermission,
  AdminUserDetail,
  AdminUserRecord,
  GenerateRedeemCodesInput,
  ManagedDomain,
  PermissionPolicy,
  PaymentProduct,
  SetAdminUserPermissionInput,
  SetUserQuotaInput,
  UpdatePaymentProductInput,
  UpdateEmailRouteInput,
  UpdateAdminUserInput,
  UpdateApplicationInput,
  UpdatePermissionPolicyInput,
  UpsertAdminDomainRecordInput,
  UpsertEmailRouteInput,
  UpsertManagedDomainInput,
} from '../types/admin';

interface APIEnvelope<T> {
  data: T;
}

interface APIErrorBody {
  error: {
    code: string;
    message: string;
  };
}

// adminAuthInvalidatedEvent lets the app shell react when any protected admin
// request proves that the current session or second-factor verification is no
// longer valid.
export const adminAuthInvalidatedEvent = 'linuxdospace:admin-auth-invalidated';

function resolveAPIBaseURL(): string {
  const configuredBaseURL = import.meta.env.VITE_API_BASE_URL?.trim();
  if (configuredBaseURL) {
    return configuredBaseURL.replace(/\/+$/, '');
  }

  const currentURL = new URL(window.location.origin);
  const { port } = currentURL;
  const isFrontendSubdomain = currentURL.hostname.startsWith('app.') || currentURL.hostname.startsWith('admin.');
  const isLocalHostname = currentURL.hostname === 'localhost' || currentURL.hostname.endsWith('.localhost');

  if (isFrontendSubdomain && !isLocalHostname) {
    const apiOrigin = `${currentURL.protocol}//api.${currentURL.hostname.split('.').slice(1).join('.')}${port ? `:${port}` : ''}`;
    return apiOrigin.replace(/\/+$/, '');
  }

  return currentURL.origin.replace(/\/+$/, '');
}

// apiBaseURL points the standalone admin frontend at the shared backend origin.
export const apiBaseURL = resolveAPIBaseURL();

// APIError is thrown when the backend returns a non-2xx JSON error envelope.
export class APIError extends Error {
  code: string;
  status: number;

  constructor(message: string, code: string, status: number) {
    super(message);
    this.name = 'APIError';
    this.code = code;
    this.status = status;
  }
}

// notifyAdminAuthInvalidated broadcasts authentication-state failures back to
// the admin shell so page-level loaders do not get stuck in a stale authorized UI.
function notifyAdminAuthInvalidated(path: string, code: string, status: number, message: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  if (!['unauthorized', 'forbidden', 'admin_password_required'].includes(code)) {
    return;
  }
  if (path === '/v1/admin/verify-password' && code === 'unauthorized') {
    return
  }
  window.dispatchEvent(new CustomEvent(adminAuthInvalidatedEvent, { detail: { code, status, message } }));
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(`${apiBaseURL}${path}`, {
    credentials: 'include',
    ...init,
    headers: {
      Accept: 'application/json',
      ...(init.body ? { 'Content-Type': 'application/json' } : {}),
      ...(init.headers ?? {}),
    },
  });

  const contentType = response.headers.get('content-type')?.toLowerCase() ?? '';
  const isJSONResponse = contentType.includes('application/json');

  if (!response.ok) {
    let errorBody: APIErrorBody | null = null;
    if (isJSONResponse) {
      try {
        errorBody = (await response.json()) as APIErrorBody;
      } catch {
        errorBody = null;
      }
    }
    if (!isJSONResponse) {
      throw await buildNonJSONAPIError(path, response);
    }
    notifyAdminAuthInvalidated(path, errorBody?.error.code ?? 'http_error', response.status, errorBody?.error.message ?? '');
    throw new APIError(
      errorBody?.error.message ?? `Request failed with status ${response.status}`,
      errorBody?.error.code ?? 'http_error',
      response.status,
    );
  }

  if (!isJSONResponse) {
    throw await buildNonJSONAPIError(path, response);
  }

  try {
    const envelope = (await response.json()) as APIEnvelope<T>;
    return envelope.data;
  } catch {
    throw new APIError(
      `管理员前端收到无法解析的 JSON 数据：${path}（HTTP ${response.status}）。`,
      'invalid_json',
      response.status,
    );
  }
}

async function buildNonJSONAPIError(path: string, response: Response): Promise<APIError> {
  let responsePreview = '';
  try {
    responsePreview = (await response.text()).trim().slice(0, 120).toLowerCase();
  } catch {
    responsePreview = '';
  }

  const responseURL = response.url || `${apiBaseURL}${path}`;
  const htmlHint = responsePreview.startsWith('<!doctype html') || responsePreview.startsWith('<html')
    ? ' 返回内容看起来像 HTML 页面。'
    : '';

  return new APIError(
    `管理员前端收到非 JSON 响应，请检查 VITE_API_BASE_URL 或反向代理配置。请求：${path}；响应地址：${responseURL}；状态码：${response.status}。${htmlHint}`,
    'invalid_response_content_type',
    response.status,
  );
}

export function getAdminLoginURL(nextPath: string): string {
  return `${apiBaseURL}/v1/admin/auth/login?next=${encodeURIComponent(nextPath)}`;
}

export function getAdminSession(): Promise<AdminSessionResponse> {
  return request<AdminSessionResponse>('/v1/admin/me');
}

export function verifyAdminPassword(password: string, csrfToken: string): Promise<AdminSessionResponse> {
  return request<AdminSessionResponse>('/v1/admin/verify-password', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify({ password }),
  });
}

export function logout(csrfToken: string): Promise<{ logged_out: boolean }> {
  return request<{ logged_out: boolean }>('/v1/auth/logout', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
  });
}

export function listManagedDomains(): Promise<ManagedDomain[]> {
  return request<ManagedDomain[]>('/v1/admin/domains');
}

export function upsertManagedDomain(input: UpsertManagedDomainInput, csrfToken: string): Promise<ManagedDomain> {
  return request<ManagedDomain>('/v1/admin/domains', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function setUserQuota(input: SetUserQuotaInput, csrfToken: string) {
  return request('/v1/admin/quotas', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listAdminUsers(): Promise<AdminUserRecord[]> {
  return request<AdminUserRecord[]>('/v1/admin/users');
}

export function getAdminUserDetail(userID: number): Promise<AdminUserDetail> {
  return request<AdminUserDetail>(`/v1/admin/users/${userID}`);
}

export function updateAdminUser(userID: number, input: UpdateAdminUserInput, csrfToken: string): Promise<AdminUserDetail> {
  return request<AdminUserDetail>(`/v1/admin/users/${userID}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listAdminUserPermissions(userID: number): Promise<AdminUserPermission[]> {
  return request<AdminUserPermission[]>(`/v1/admin/users/${userID}/permissions`);
}

export function setAdminUserPermission(
  userID: number,
  permissionKey: string,
  input: SetAdminUserPermissionInput,
  csrfToken: string,
): Promise<AdminUserPermission> {
  return request<AdminUserPermission>(`/v1/admin/users/${userID}/permissions/${encodeURIComponent(permissionKey)}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listAdminAllocations(): Promise<AdminAllocationRecord[]> {
  return request<AdminAllocationRecord[]>('/v1/admin/allocations');
}

export function createAdminAllocation(input: CreateAdminAllocationInput, csrfToken: string): Promise<AdminAllocationRecord> {
  return request<AdminAllocationRecord>('/v1/admin/allocations', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function updateAdminAllocation(allocationID: number, input: UpdateAdminAllocationInput, csrfToken: string): Promise<AdminAllocationRecord> {
  return request<AdminAllocationRecord>(`/v1/admin/allocations/${allocationID}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listAdminRecords(): Promise<AdminDomainRecord[]> {
  return request<AdminDomainRecord[]>('/v1/admin/records');
}

export function createAdminRecord(allocationID: number, input: UpsertAdminDomainRecordInput, csrfToken: string): Promise<AdminDomainRecord> {
  return request<AdminDomainRecord>(`/v1/admin/allocations/${allocationID}/records`, {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function updateAdminRecord(allocationID: number, recordID: string, input: UpsertAdminDomainRecordInput, csrfToken: string): Promise<AdminDomainRecord> {
  return request<AdminDomainRecord>(`/v1/admin/allocations/${allocationID}/records/${recordID}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function deleteAdminRecord(allocationID: number, recordID: string, csrfToken: string): Promise<{ deleted: boolean }> {
  return request<{ deleted: boolean }>(`/v1/admin/allocations/${allocationID}/records/${recordID}`, {
    method: 'DELETE',
    headers: { 'X-CSRF-Token': csrfToken },
  });
}

export function listEmailRoutes(): Promise<AdminEmailRecord[]> {
  return request<AdminEmailRecord[]>('/v1/admin/email-routes');
}

export function createEmailRoute(input: UpsertEmailRouteInput, csrfToken: string): Promise<AdminEmailRecord> {
  return request<AdminEmailRecord>('/v1/admin/email-routes', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function updateEmailRoute(routeID: number, input: UpdateEmailRouteInput, csrfToken: string): Promise<AdminEmailRecord> {
  return request<AdminEmailRecord>(`/v1/admin/email-routes/${routeID}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function deleteEmailRoute(routeID: number, csrfToken: string): Promise<{ deleted: boolean }> {
  return request<{ deleted: boolean }>(`/v1/admin/email-routes/${routeID}`, {
    method: 'DELETE',
    headers: { 'X-CSRF-Token': csrfToken },
  });
}

export function listApplications(): Promise<AdminApplicationRecord[]> {
  return request<AdminApplicationRecord[]>('/v1/admin/applications');
}

export function updateApplication(applicationID: number, input: UpdateApplicationInput, csrfToken: string): Promise<AdminApplicationRecord> {
  return request<AdminApplicationRecord>(`/v1/admin/applications/${applicationID}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listPermissionPolicies(): Promise<PermissionPolicy[]> {
  return request<PermissionPolicy[]>('/v1/admin/permission-policies');
}

export function updatePermissionPolicy(policyKey: string, input: UpdatePermissionPolicyInput, csrfToken: string): Promise<PermissionPolicy> {
  return request<PermissionPolicy>(`/v1/admin/permission-policies/${encodeURIComponent(policyKey)}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listAdminPaymentProducts(): Promise<PaymentProduct[]> {
  return request<PaymentProduct[]>('/v1/admin/ldc/products');
}

export function listAdminPaymentOrders(): Promise<AdminPaymentOrder[]> {
  return request<AdminPaymentOrder[]>('/v1/admin/ldc/orders');
}

export function getAdminPaymentOrder(outTradeNo: string): Promise<AdminPaymentOrder> {
  return request<AdminPaymentOrder>(`/v1/admin/ldc/orders/${encodeURIComponent(outTradeNo)}`);
}

export function updateAdminPaymentProduct(productKey: string, input: UpdatePaymentProductInput, csrfToken: string): Promise<PaymentProduct> {
  return request<PaymentProduct>(`/v1/admin/ldc/products/${encodeURIComponent(productKey)}`, {
    method: 'PATCH',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function listRedeemCodes(): Promise<AdminRedeemCodeRecord[]> {
  return request<AdminRedeemCodeRecord[]>('/v1/admin/redeem-codes');
}

export function generateRedeemCodes(input: GenerateRedeemCodesInput, csrfToken: string): Promise<AdminRedeemCodeRecord[]> {
  return request<AdminRedeemCodeRecord[]>('/v1/admin/redeem-codes/batch', {
    method: 'POST',
    headers: { 'X-CSRF-Token': csrfToken },
    body: JSON.stringify(input),
  });
}

export function deleteRedeemCode(redeemCodeID: number, csrfToken: string): Promise<{ deleted: boolean }> {
  return request<{ deleted: boolean }>(`/v1/admin/redeem-codes/${redeemCodeID}`, {
    method: 'DELETE',
    headers: { 'X-CSRF-Token': csrfToken },
  });
}
