import type {
  Allocation,
  APIEnvelope,
  APIErrorBody,
  AvailabilityResult,
  CreatePaymentOrderInput,
  CreateAllocationInput,
  CreateMyEmailTargetInput,
  DNSRecord,
  EmailRouteAvailabilityResult,
  ManagedDomain,
  MeResponse,
  SubmitPermissionApplicationInput,
  SupervisionEntry,
  PaymentOrder,
  PaymentProduct,
  UpsertMyCatchAllEmailRouteInput,
  UpsertMyDefaultEmailRouteInput,
  UpsertDNSRecordInput,
  UserEmailTarget,
  UserEmailRoute,
  UserPermission,
} from '../types/api';

// resolveAPIBaseURL keeps local development simple while still protecting the
// deployed frontend from accidentally talking to itself and parsing HTML as if
// it were an API response.
function resolveAPIBaseURL(): string {
  const configuredBaseURL = import.meta.env.VITE_API_BASE_URL?.trim();
  if (configuredBaseURL) {
    return configuredBaseURL.replace(/\/+$/, '');
  }

  if (typeof window === 'undefined') {
    return '';
  }

  const currentURL = new URL(window.location.origin);
  const { hostname, origin, port, protocol } = currentURL;
  const isFrontendSubdomain = hostname.startsWith('app.') || hostname.startsWith('admin.');
  const isLocalHostname = hostname === 'localhost' || hostname.endsWith('.localhost');

  if (isFrontendSubdomain && !isLocalHostname) {
    const apiHostname = `api.${hostname.split('.').slice(1).join('.')}`;
    const apiOrigin = `${protocol}//${apiHostname}${port ? `:${port}` : ''}`;
    return apiOrigin.replace(/\/+$/, '');
  }

  return origin.replace(/\/+$/, '');
}

// apiBaseURL stores the backend origin used by the public frontend.
export const apiBaseURL = resolveAPIBaseURL();

// authInvalidEventName lets the app shell react when an authenticated request
// proves that the browser session expired after page bootstrap.
export const authInvalidEventName = 'linuxdospace:auth-invalid';

// APIError is the shared frontend error shape used by public pages.
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

// notifyAuthInvalid informs the top-level app shell about expired or forbidden
// sessions while still letting the original caller handle the rejected request.
function notifyAuthInvalid(path: string, status: number, code: string): void {
  if (typeof window === 'undefined') {
    return;
  }
  if (path === '/v1/me' || path === '/v1/auth/logout') {
    return;
  }
  if (!(status === 401 || status === 403 || code === 'unauthorized' || code === 'forbidden')) {
    return;
  }

  window.dispatchEvent(
    new CustomEvent(authInvalidEventName, {
      detail: { path, status, code },
    }),
  );
}

// request wraps fetch so every page consistently gets cookies, JSON decoding,
// and clearer deployment diagnostics when a proxy returns HTML instead of the
// documented JSON envelope.
async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const requestMethod = (init.method ?? 'GET').toUpperCase();
  const requestURL = `${apiBaseURL}${path}`;
  const response = await fetch(requestURL, {
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
      throw await buildNonJSONAPIError(path, requestMethod, response);
    }

    notifyAuthInvalid(path, response.status, errorBody?.error.code ?? 'http_error');
    throw new APIError(
      errorBody?.error.message ?? `请求失败：${requestMethod} ${path}（HTTP ${response.status}）`,
      errorBody?.error.code ?? 'http_error',
      response.status,
    );
  }

  if (!isJSONResponse) {
    throw await buildNonJSONAPIError(path, requestMethod, response);
  }

  try {
    const envelope = (await response.json()) as APIEnvelope<T>;
    return envelope.data;
  } catch {
    throw new APIError(
      `后端返回了无法解析的 JSON 数据：${requestMethod} ${path}（HTTP ${response.status}）。`,
      'invalid_json',
      response.status,
    );
  }
}

// buildNonJSONAPIError adds deployment-facing context when the backend returns
// HTML or another unexpected payload instead of the documented JSON envelope.
async function buildNonJSONAPIError(path: string, method: string, response: Response): Promise<APIError> {
  let responsePreview = '';
  try {
    responsePreview = (await response.text()).trim().slice(0, 120).toLowerCase();
  } catch {
    responsePreview = '';
  }

  const responseURL = response.url || `${apiBaseURL}${path}`;
  const looksLikeHTML = responsePreview.startsWith('<!doctype html') || responsePreview.startsWith('<html');
  const htmlHint = looksLikeHTML ? ' 返回内容看起来像 HTML 页面。' : '';

  return new APIError(
    `后端返回了非 JSON 响应，请检查 VITE_API_BASE_URL 或反向代理配置。请求：${method} ${path}；响应地址：${responseURL}；状态码：${response.status}。这通常表示 API 请求被转发到了错误的页面。${htmlHint}`,
    'invalid_response_content_type',
    response.status,
  );
}

// getAuthLoginURL builds the public Linux Do OAuth entrypoint.
export function getAuthLoginURL(nextPath: string): string {
  return `${apiBaseURL}/v1/auth/login?next=${encodeURIComponent(nextPath)}`;
}

// getCurrentSession loads the current browser session state.
export function getCurrentSession(): Promise<MeResponse> {
  return request<MeResponse>('/v1/me');
}

// listPublicDomains returns the managed domains currently visible to the public site.
export function listPublicDomains(): Promise<ManagedDomain[]> {
  return request<ManagedDomain[]>('/v1/public/domains');
}

// listPublicSupervisionEntries returns the privacy-safe ownership list shown on the supervision page.
export function listPublicSupervisionEntries(): Promise<SupervisionEntry[]> {
  return request<SupervisionEntry[]>('/v1/public/supervision');
}

// listPublicPaymentProducts returns every currently enabled Linux Do Credit
// product so the permissions page can render pricing before login.
export function listPublicPaymentProducts(): Promise<PaymentProduct[]> {
  return request<PaymentProduct[]>('/v1/public/ldc/products');
}

// checkAllocationAvailability checks one subdomain prefix on a managed root domain.
export function checkAllocationAvailability(rootDomain: string, prefix: string): Promise<AvailabilityResult> {
  const query = new URLSearchParams({
    root_domain: rootDomain,
    prefix,
  });
  return request<AvailabilityResult>(`/v1/public/allocations/check?${query.toString()}`);
}

// checkPublicEmailRouteAvailability checks whether one mailbox local-part is available.
export function checkPublicEmailRouteAvailability(rootDomain: string, prefix: string): Promise<EmailRouteAvailabilityResult> {
  const query = new URLSearchParams({
    root_domain: rootDomain,
    prefix,
  });
  return request<EmailRouteAvailabilityResult>(`/v1/public/email-routes/check?${query.toString()}`);
}

// createAllocation lets the current user request a new subdomain allocation.
export function createAllocation(input: CreateAllocationInput, csrfToken: string): Promise<Allocation> {
  return request<Allocation>('/v1/my/allocations', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// listMyPermissions returns the current authenticated user's permission cards.
export function listMyPermissions(): Promise<UserPermission[]> {
  return request<UserPermission[]>('/v1/my/permissions');
}

// listMyPaymentOrders returns the authenticated user's recent LDC orders.
export function listMyPaymentOrders(): Promise<PaymentOrder[]> {
  return request<PaymentOrder[]>('/v1/my/ldc/orders');
}

// getMyPaymentOrder reloads one specific order and lets the backend reconcile
// its current gateway status.
export function getMyPaymentOrder(outTradeNo: string): Promise<PaymentOrder> {
  return request<PaymentOrder>(`/v1/my/ldc/orders/${encodeURIComponent(outTradeNo)}`);
}

// createMyPaymentOrder reserves one local order and returns the payment URL
// that the browser should open in a new tab.
export function createMyPaymentOrder(input: CreatePaymentOrderInput, csrfToken: string): Promise<PaymentOrder> {
  return request<PaymentOrder>('/v1/my/ldc/orders', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// submitPermissionApplication stores one permission application for the current user.
export function submitPermissionApplication(input: SubmitPermissionApplicationInput, csrfToken: string): Promise<UserPermission> {
  return request<UserPermission>('/v1/my/permissions/applications', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// listMyEmailRoutes returns the mailbox rows visible to the current user.
export function listMyEmailRoutes(): Promise<UserEmailRoute[]> {
  return request<UserEmailRoute[]>('/v1/my/email-routes');
}

// listMyEmailTargets returns every forwarding destination currently bound to
// the authenticated user.
export function listMyEmailTargets(): Promise<UserEmailTarget[]> {
  return request<UserEmailTarget[]>('/v1/my/email-targets');
}

// createMyEmailTarget binds one external inbox to the authenticated user and
// triggers Cloudflare's verification email when needed.
export function createMyEmailTarget(input: CreateMyEmailTargetInput, csrfToken: string): Promise<UserEmailTarget> {
  return request<UserEmailTarget>('/v1/my/email-targets', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// upsertDefaultEmailRoute saves the forwarding target for the implicit default mailbox.
export function upsertDefaultEmailRoute(input: UpsertMyDefaultEmailRouteInput, csrfToken: string): Promise<UserEmailRoute> {
  return request<UserEmailRoute>('/v1/my/email-routes/default', {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// upsertCatchAllEmailRoute saves the forwarding target for the permission-gated catch-all mailbox.
export function upsertCatchAllEmailRoute(input: UpsertMyCatchAllEmailRouteInput, csrfToken: string): Promise<UserEmailRoute> {
  return request<UserEmailRoute>('/v1/my/email-routes/catch-all', {
    method: 'PUT',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
    body: JSON.stringify(input),
  });
}

// listAllocationRecords returns the live DNS records currently attached to one allocation.
export function listAllocationRecords(allocationID: number): Promise<DNSRecord[]> {
  return request<DNSRecord[]>(`/v1/my/allocations/${allocationID}/records`);
}

// createDNSRecord adds one DNS record under the given allocation.
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

// updateDNSRecord updates one DNS record under the given allocation.
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

// deleteDNSRecord deletes one DNS record under the given allocation.
export function deleteDNSRecord(allocationID: number, recordID: string, csrfToken: string): Promise<{ deleted: boolean }> {
  return request<{ deleted: boolean }>(`/v1/my/allocations/${allocationID}/records/${recordID}`, {
    method: 'DELETE',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
  });
}

// logout destroys the current browser session on the backend.
export function logout(csrfToken: string): Promise<{ logged_out: boolean }> {
  return request<{ logged_out: boolean }>('/v1/auth/logout', {
    method: 'POST',
    headers: {
      'X-CSRF-Token': csrfToken,
    },
  });
}
