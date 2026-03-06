import type {
  Allocation,
  APIEnvelope,
  APIErrorBody,
  AvailabilityResult,
  CreateAllocationInput,
  DNSRecord,
  ManagedDomain,
  MeResponse,
  SupervisionEntry,
  UpsertDNSRecordInput,
} from '../types/api';

// apiBaseURL 用于保存当前前端应该连接的后端地址。
// 在 Docker / 同源部署场景下，如果没有显式配置环境变量，就自动回退到当前页面所在源。
export const apiBaseURL = (
  import.meta.env.VITE_API_BASE_URL && import.meta.env.VITE_API_BASE_URL.trim() !== ''
    ? import.meta.env.VITE_API_BASE_URL
    : window.location.origin
).replace(/\/+$/, '');

// APIError 是浏览器端统一使用的接口异常类型。
// 我们额外保留了 `code` 和 `status`，方便页面层做更细的错误提示。
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

// request 负责统一封装 fetch、Cookie、JSON 解析与错误处理。
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

// getAuthLoginURL 根据后端地址拼出 OAuth 登录入口。
export function getAuthLoginURL(nextPath: string): string {
  return `${apiBaseURL}/v1/auth/login?next=${encodeURIComponent(nextPath)}`;
}

// getCurrentSession 获取当前浏览器对应的登录态与分配信息。
export function getCurrentSession(): Promise<MeResponse> {
  return request<MeResponse>('/v1/me');
}

// listPublicDomains 获取当前开放分发的根域名列表。
export function listPublicDomains(): Promise<ManagedDomain[]> {
  return request<ManagedDomain[]>('/v1/public/domains');
}

// listPublicSupervisionEntries 获取公开监督页需要的脱敏子域归属数据。
export function listPublicSupervisionEntries(): Promise<SupervisionEntry[]> {
  return request<SupervisionEntry[]>('/v1/public/supervision');
}

// checkAllocationAvailability 调用后端检查某个前缀是否可用。
export function checkAllocationAvailability(rootDomain: string, prefix: string): Promise<AvailabilityResult> {
  const query = new URLSearchParams({
    root_domain: rootDomain,
    prefix,
  });
  return request<AvailabilityResult>(`/v1/public/allocations/check?${query.toString()}`);
}

// createAllocation 为当前登录用户申请一个新的命名空间分配。
export function createAllocation(input: CreateAllocationInput, csrfToken: string): Promise<Allocation> {
  return request<Allocation>('/v1/my/allocations', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// listAllocationRecords 返回某个命名空间下的全部实时 DNS 记录。
export function listAllocationRecords(allocationID: number): Promise<DNSRecord[]> {
  return request<DNSRecord[]>(`/v1/my/allocations/${allocationID}/records`);
}

// createDNSRecord 在指定命名空间下创建一条新的 DNS 记录。
export function createDNSRecord(
  allocationID: number,
  input: UpsertDNSRecordInput,
  csrfToken: string,
): Promise<DNSRecord> {
  return request<DNSRecord>(`/v1/my/allocations/${allocationID}/records`, {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// updateDNSRecord 更新指定命名空间中的一条 DNS 记录。
export function updateDNSRecord(
  allocationID: number,
  recordID: string,
  input: UpsertDNSRecordInput,
  csrfToken: string,
): Promise<DNSRecord> {
  return request<DNSRecord>(`/v1/my/allocations/${allocationID}/records/${recordID}`, {
    method: 'PATCH',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// deleteDNSRecord 删除指定命名空间中的一条 DNS 记录。
export function deleteDNSRecord(allocationID: number, recordID: string, csrfToken: string): Promise<{ deleted: boolean }> {
  return request<{ deleted: boolean }>(`/v1/my/allocations/${allocationID}/records/${recordID}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
  });
}

// logout 销毁当前浏览器对应的后端会话。
export function logout(csrfToken: string): Promise<{ logged_out: boolean }> {
  return request<{ logged_out: boolean }>('/v1/auth/logout', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
  });
}
