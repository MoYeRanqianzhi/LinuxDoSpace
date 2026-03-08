import type {
  AdminAllocationRecord,
  AdminApplicationRecord,
  AdminDomainRecord,
  AdminEmailRecord,
  AdminRedeemCodeRecord,
  AdminSessionResponse,
  AdminUserDetail,
  AdminUserRecord,
  GenerateRedeemCodesInput,
  ManagedDomain,
  SetUserQuotaInput,
  UpdateAdminUserInput,
  UpdateApplicationInput,
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

// apiBaseURL points the standalone admin frontend at the shared backend origin.
export const apiBaseURL = (
  import.meta.env.VITE_API_BASE_URL && import.meta.env.VITE_API_BASE_URL.trim() !== ''
    ? import.meta.env.VITE_API_BASE_URL
    : window.location.origin
).replace(/\/+$/, '');

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

  if (!response.ok) {
    let errorBody: APIErrorBody | null = null;
    try {
      errorBody = (await response.json()) as APIErrorBody;
    } catch {
      errorBody = null;
    }
    throw new APIError(
      errorBody?.error.message ?? `Request failed with status ${response.status}`,
      errorBody?.error.code ?? 'http_error',
      response.status,
    );
  }

  const envelope = (await response.json()) as APIEnvelope<T>;
  return envelope.data;
}

export function getAdminLoginURL(nextPath: string): string {
  return `${apiBaseURL}/v1/admin/auth/login?next=${encodeURIComponent(nextPath)}`;
}

export function getAdminSession(): Promise<AdminSessionResponse> {
  return request<AdminSessionResponse>('/v1/admin/me');
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

export function listAdminAllocations(): Promise<AdminAllocationRecord[]> {
  return request<AdminAllocationRecord[]>('/v1/admin/allocations');
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

export function updateEmailRoute(routeID: number, input: UpsertEmailRouteInput, csrfToken: string): Promise<AdminEmailRecord> {
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
