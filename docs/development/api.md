# LinuxDoSpace API Documentation

## Response envelope

Successful responses:

```json
{
  "data": {}
}
```

Error responses:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "prefix is required"
  }
}
```

## Public endpoints

### `GET /healthz`
Returns process health, version, and dependency readiness.

### `GET /v1/public/domains`
Returns the enabled managed root-domain list.

### `GET /v1/public/supervision`
Returns privacy-safe ownership rows for the public supervision page.
Only subdomain ownership is exposed. Concrete DNS values are never returned.

### `GET /v1/public/allocations/check?root_domain=linuxdo.space&prefix=alice`
Checks whether a specific prefix is currently available under the selected root domain.

## User authentication and self-service endpoints

### `GET /v1/auth/login?next=/settings`
Starts the Linux Do OAuth login flow for the main frontend.

### `GET /v1/auth/callback`
Shared OAuth callback used by both the main frontend and the admin frontend.
The backend decides which frontend should receive the redirect through a short-lived login-target cookie.

### `POST /v1/auth/logout`
Destroys the current authenticated session.
Requires a valid session and `X-CSRF-Token`.

### `GET /v1/me`
Returns the current public-site session, user payload, CSRF token, and visible allocations.

### `GET /v1/my/allocations`
Lists the current user's visible allocation namespaces.

### `GET /v1/my/permissions`
Returns the current authenticated user's visible permission cards.
The current release exposes the `email_catch_all` permission used by the public email page.

### `POST /v1/my/permissions/applications`
Creates or refreshes one permission application for the current user.
For `email_catch_all`, the backend stores a canonical pledge text server-side and may auto-approve the request when the configured policy allows it.

Request example:

```json
{
  "key": "email_catch_all"
}
```

### `GET /v1/my/email-routes`
Returns the current user's visible email forwarding rows.
At the moment this endpoint returns the placeholder or persisted row for `catch-all@<username>.linuxdo.space`.

### `PUT /v1/my/email-routes/catch-all`
Creates or updates the current user's catch-all forwarding target after the permission has been approved.

Request example:

```json
{
  "target_email": "owner@example.com",
  "enabled": true
}
```

### `POST /v1/my/allocations`
Creates a new allocation namespace for the current user.

Request example:

```json
{
  "root_domain": "linuxdo.space",
  "prefix": "alice",
  "source": "manual",
  "primary": true
}
```

### `GET /v1/my/allocations/{allocationID}/records`
Lists the current user's DNS records inside the selected allocation namespace.

### `POST /v1/my/allocations/{allocationID}/records`
Creates one DNS record inside the selected allocation namespace.

### `PATCH /v1/my/allocations/{allocationID}/records/{recordID}`
Updates one DNS record inside the selected allocation namespace.

### `DELETE /v1/my/allocations/{allocationID}/records/{recordID}`
Deletes one DNS record inside the selected allocation namespace.

## Administrator authentication endpoints

### `GET /v1/admin/auth/login?next=/#users`
Starts the Linux Do OAuth login flow for the standalone admin frontend.
The eventual callback will redirect to `APP_ADMIN_FRONTEND_URL`.

### `GET /v1/admin/me`
Returns the current administrator session state.
Possible results:

- `authenticated=false` when no valid backend session exists
- `authenticated=true, authorized=false` when the Linux Do account is logged in but not granted admin permission
- `authenticated=true, authorized=true` with `csrf_token`, `session_expires_at`, `user`, and `managed_domains` when the account is an administrator

### `POST /v1/admin/verify-password`
Completes the second administrator verification step by checking the extra backend password.
This endpoint is rate limited by both session ID and client IP. Repeated failures return `429 too_many_requests` with a `Retry-After` header.

## Administrator data endpoints

All write endpoints below require:

- a valid authenticated administrator session
- `X-CSRF-Token`

### `GET /v1/admin/domains`
Returns all managed root-domain configurations, including disabled ones.

### `POST /v1/admin/domains`
Creates or updates a managed root-domain configuration.

Request example:

```json
{
  "root_domain": "linuxdo.space",
  "cloudflare_zone_id": "",
  "default_quota": 1,
  "auto_provision": true,
  "is_default": true,
  "enabled": true
}
```

### `POST /v1/admin/quotas`
Writes one user quota override under a managed root domain.

Request example:

```json
{
  "username": "alice",
  "root_domain": "linuxdo.space",
  "max_allocations": 3,
  "reason": "admin-console"
}
```

### `GET /v1/admin/users`
Returns the compact user list for the administrator console.

### `GET /v1/admin/users/{userID}`
Returns the expanded moderation and quota view for one user.

### `PATCH /v1/admin/users/{userID}`
Updates the moderation state for one user.

Request example:

```json
{
  "is_banned": true,
  "ban_note": "abuse report confirmed"
}
```

### `GET /v1/admin/users/{userID}/permissions`
Returns the current administrator-visible permission cards for one target user.

### `PATCH /v1/admin/users/{userID}/permissions/{permissionKey}`
Lets an administrator directly override one target user's permission state.

Request example:

```json
{
  "status": "approved",
  "review_note": "manual grant after review",
  "reason": "管理员手动设置该权限状态。"
}
```

### `GET /v1/admin/allocations`
Returns all allocation namespaces together with owner identity.
Useful for admin record creation workflows.

### `POST /v1/admin/allocations`
Creates one allocation namespace on behalf of any user.

Request example:

```json
{
  "owner_user_id": 1,
  "root_domain": "linuxdo.space",
  "prefix": "alice",
  "is_primary": true,
  "source": "manual",
  "status": "active"
}
```

### `PATCH /v1/admin/allocations/{allocationID}`
Updates one allocation's owner or lifecycle state.
`status` currently accepts `active` or `disabled`.

Request example:

```json
{
  "owner_user_id": 2,
  "is_primary": true,
  "source": "manual-transfer",
  "status": "active"
}
```

### `GET /v1/admin/records`
Returns the global administrator DNS record list across all allocation namespaces.

### `POST /v1/admin/allocations/{allocationID}/records`
Creates one DNS record inside the selected allocation namespace.

### `PATCH /v1/admin/allocations/{allocationID}/records/{recordID}`
Updates one DNS record inside the selected allocation namespace.

### `DELETE /v1/admin/allocations/{allocationID}/records/{recordID}`
Deletes one DNS record inside the selected allocation namespace.

### `GET /v1/admin/email-routes`
Returns all administrator-managed email forwarding rules.

### `POST /v1/admin/email-routes`
Creates one email forwarding rule.

Request example:

```json
{
  "owner_user_id": 1,
  "root_domain": "linuxdo.space",
  "prefix": "hello",
  "target_email": "owner@example.com",
  "enabled": true
}
```

### `PATCH /v1/admin/email-routes/{routeID}`
Updates one email forwarding rule.

### `DELETE /v1/admin/email-routes/{routeID}`
Deletes one email forwarding rule.

### `GET /v1/admin/applications`
Returns all moderation requests visible to the administrator console.

### `GET /v1/admin/permission-policies`
Returns the administrator-configurable policy rows that control permission eligibility and auto-approval.

### `PATCH /v1/admin/permission-policies/{policyKey}`
Updates one permission-policy row.

Request example:

```json
{
  "enabled": true,
  "auto_approve": true,
  "min_trust_level": 2
}
```

### `PATCH /v1/admin/applications/{applicationID}`
Updates one moderation request state.
`status` accepts `pending`, `approved`, and `rejected` so admins can reopen an application when needed.

Request example:

```json
{
  "status": "approved",
  "review_note": ""
}
```

### `GET /v1/admin/redeem-codes`
Returns all generated redeem codes.

### `POST /v1/admin/redeem-codes/batch`
Generates one batch of redeem codes.

Request example:

```json
{
  "amount": 3,
  "type": "single",
  "target": "api.linuxdo.space",
  "note": "manual reward"
}
```

### `DELETE /v1/admin/redeem-codes/{redeemCodeID}`
Deletes one generated redeem code.

## Security model

- All authenticated state is stored in server-side sessions referenced by an HTTP-only cookie.
- Unsafe endpoints require the current session's `X-CSRF-Token`.
- Sessions can be bound to the browser's user-agent fingerprint.
- Administrator permissions are enforced server-side on every `/v1/admin/*` data endpoint.
- Banned users are blocked both at login time and on subsequent session validation.
- Administrator write operations emit audit log rows for traceability.
