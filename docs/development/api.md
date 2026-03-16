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
The payload now also exposes `mail_forwarding_backend` and `mail_relay_enabled`
so operators can verify whether the instance is running in `cloudflare` or
`database_relay` mode.

### `GET /v1/public/domains`
Returns the enabled managed root-domain list.

### `GET /v1/public/supervision`
Returns privacy-safe ownership rows for the public supervision page.
Only subdomain ownership is exposed. Concrete DNS values are never returned.

### `GET /v1/public/allocations/check?root_domain=linuxdo.space&prefix=alice`
Checks whether a specific prefix is currently available under the selected root domain.

### `GET /v1/public/email-routes/check?root_domain=linuxdo.space&prefix=alice`
Checks whether a mailbox local-part is available on the selected managed email domain.
The backend also treats existing Linux Do usernames as reserved so each user keeps their implicit default mailbox.

### `GET /v1/public/ldc/products`
Returns every currently enabled Linux Do Credit product.
The public frontend uses this endpoint so pricing can be rendered before login.

### `GET /v1/public/ldc/products`
Returns all currently enabled Linux Do Credit purchase products in display order.
This endpoint is intentionally public so the frontend can show pricing before login.

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
Returns the current public-site session, user payload, CSRF token, and every active allocation already owned by the current user.
The temporary restriction that only allows self-service requests for a username-matching namespace does not hide administrator-granted namespaces from this response.

### `GET /v1/my/allocations`
Lists every active allocation namespace currently owned by the authenticated user.

### `GET /v1/my/permissions`
Returns the current authenticated user's visible permission cards.
The current release exposes the `email_catch_all` permission used by the public email page.
That permission card now also includes `catch_all_access`, which reports:
- whether an active subscription window currently exists
- the remaining prepaid count when no subscription is active
- the current UTC day's used count
- the effective per-user daily cap
- whether catch-all delivery is currently available

### `GET /v1/my/quantity-records`
Returns the current authenticated user's full append-only quantity ledger.
This endpoint is intended for future billing, redeem-code, and quota history UIs.

Each record contains:
- `resource_key`: machine-readable resource type such as `domain_slot`
- `scope`: optional namespace such as `linuxdo.space`
- `delta`: signed quantity change
- `source`: machine-readable origin such as `admin_manual`
- `reason`: human-readable explanation
- `reference_type` and `reference_id`: optional external linkage for future payment or redeem flows
- `expires_at`: optional future expiry timestamp

### `GET /v1/my/quantity-balances`
Returns the current authenticated user's derived non-zero quantity balances.
The backend sums only non-expired ledger entries and groups them by `resource_key + scope`.

### `GET /v1/my/pow/status`
Returns the authenticated user's proof-of-work dashboard state.

The payload includes:
- whether the whole PoW feature is currently enabled
- the currently enabled benefit options
- the supported difficulty options
- the fixed daily completion cap
- how many rewards were already claimed today
- how many claims remain today
- the current accumulated `email_catch_all_remaining_count`
- the current active challenge, when one exists

Important behavior:
- difficulty is measured in leading zero **bits**, not hexadecimal digits
- each browser-side trial currently uses **64 MiB** of Argon2 memory cost
- each user can keep only one active challenge at a time
- generating a new challenge always supersedes the previous active one
- the backend does **not** pre-generate the random reward amount when the challenge is created
- the backend remains the only trusted source for challenge generation and verification

### `POST /v1/my/pow/challenges`
Creates or replaces the current authenticated user's active proof-of-work challenge.
This endpoint requires `X-CSRF-Token`.

Request example:

```json
{
  "benefit_key": "email_catch_all_remaining_count",
  "difficulty": 6
}
```

Current constraints:
- `benefit_key` currently only supports `email_catch_all_remaining_count`
- `difficulty` must be one of `3`, `6`, `9`, or `12`
- once the user already claimed 5 rewards in the current UTC day, the backend returns `429`

### `POST /v1/my/pow/challenges/claim`
Submits one browser-computed nonce for the current authenticated user's active challenge.
This endpoint requires `X-CSRF-Token`.

The backend:
- reloads the currently active challenge for the user
- recomputes the Argon2id hash using the stored challenge token and salt
- verifies the leading-zero-bit target server-side
- only after verification succeeds, generates the final random reward amount
- enforces the per-user UTC-day completion cap
- atomically grants the reward and writes one immutable quantity-ledger row

Request example:

```json
{
  "challenge_id": 42,
  "nonce": "183"
}
```

### `GET /v1/my/ldc/orders`
Returns the current authenticated user's recent Linux Do Credit orders.

### `POST /v1/my/ldc/orders`
Creates one Linux Do Credit checkout order for the current user.
The backend reserves the local order first, then asks the upstream gateway for
the checkout URL, and finally returns the tracked local order.

Request example:

```json
{
  "product_key": "email_catch_all_subscription",
  "units": 3
}
```

### `GET /v1/my/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order for the current user.
This endpoint is now read-only and returns the current locally stored state.

### `POST /v1/my/ldc/orders/{outTradeNo}/refresh`
Explicitly refreshes one current user's Linux Do Credit order against the upstream gateway.
This endpoint requires `X-CSRF-Token` because it may update local payment state
and trigger entitlement application.

### `GET /v1/my/ldc/orders`
Returns the current authenticated user's recent Linux Do Credit orders.

### `POST /v1/my/ldc/orders`
Creates one local Linux Do Credit order, reserves it in the database, and returns the upstream payment URL.

Request example:

```json
{
  "product_key": "email_catch_all_subscription",
  "units": 3
}
```

### `GET /v1/my/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order.
When the order is still pending, the backend may query the upstream gateway and, if already paid, apply the corresponding entitlement before returning the refreshed payload.

### `GET /v1/my/ldc/orders`
Returns the current authenticated user's recent Linux Do Credit orders.

### `POST /v1/my/ldc/orders`
Creates one Linux Do Credit checkout order for the current user.
The backend reserves the local order first, then asks the upstream gateway for
the checkout URL, and finally returns the tracked local order.

Request example:

```json
{
  "product_key": "email_catch_all_subscription",
  "units": 3
}
```

### `GET /v1/my/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order for the current user.
This endpoint also performs opportunistic reconciliation:
- when the order is still pending, the backend may call the upstream query API
- when the order is already paid but not yet applied locally, the backend
  retries entitlement application before returning the response

### `GET /v1/my/ldc/orders`
Returns the current authenticated user's recent Linux Do Credit orders.

### `POST /v1/my/ldc/orders`
Creates one local Linux Do Credit order, reserves it in the database, and returns the upstream payment URL.

Request example:

```json
{
  "product_key": "email_catch_all_subscription",
  "units": 3
}
```

### `GET /v1/my/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order.
When the order is still pending, the backend may query the upstream gateway and, if already paid, apply the corresponding entitlement before returning the refreshed payload.

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
The current release returns:
- the always-owned default mailbox row for `<username>@linuxdo.space`
- any extra mailbox aliases already assigned to the user in the database
- the permission-gated `*@<username>.linuxdo.space` row

When the returned row is the catch-all mailbox, the payload also includes
`catch_all_access`. This runtime state is evaluated separately from the
append-only quantity ledger:
- active subscription time always wins first
- once no active subscription remains, the relay consumes `remaining_count`
- all catch-all deliveries still obey the effective single-user daily limit
- all time calculations use the server's UTC clock

Every email-route mutation now syncs the effective forwarding state into the
currently selected backend.
Important operational constraints:
- the target mailbox must already be a verified Cloudflare Email Routing destination address, or Cloudflare will send a verification email and the save will be rejected until verification completes
- when `EMAIL_FORWARDING_BACKEND=cloudflare`, the backend syncs exact-address and catch-all rules directly into Cloudflare Email Routing
- when `EMAIL_FORWARDING_BACKEND=database_relay`, parent-root exact mailboxes still sync to Cloudflare Email Routing, while subdomain relay namespaces and catch-all delivery execute through the built-in SMTP relay

### `PUT /v1/my/email-routes/default`
Creates, updates, or clears the current user's default mailbox forwarding target.

Request example:

```json
{
  "target_email": "owner@example.com",
  "enabled": true
}
```

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
Lists the current user's DNS records inside the selected allocation namespace, including both the namespace root record and nested child records such as `www` or `api.v2`.

### `POST /v1/my/allocations/{allocationID}/records`
Creates one DNS record inside the selected allocation namespace.
Manual DNS writes only accept `A`, `AAAA`, `CNAME`, and `TXT`.
`MX` is reserved for LinuxDoSpace's own mail-relay bootstrap and will be rejected.

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

### `GET /v1/payments/linuxdo-credit/notify`
EasyPay-compatible asynchronous success callback for Linux Do Credit.
The gateway must receive HTTP `200` with body `success` after signature verification and idempotent entitlement application.

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

### `GET /v1/admin/users/{userID}/quantity-records`
Returns the target user's full quantity ledger for administrator inspection.

### `GET /v1/admin/users/{userID}/quantity-balances`
Returns the target user's currently effective non-zero quantity balances.

### `POST /v1/admin/users/{userID}/quantity-records`
Appends one immutable quantity delta for the target user.
This is the write endpoint that future billing, manual grants, subscriptions, and redeem-code processors can reuse.

Request example:

```json
{
  "resource_key": "domain_slot",
  "scope": "linuxdo.space",
  "delta": 2,
  "source": "admin_manual",
  "reason": "manual promotional grant",
  "reference_type": "campaign",
  "reference_id": "spring-2026",
  "expires_at": "2026-04-01T00:00:00Z"
}
```

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

### `PATCH /v1/admin/users/{userID}/permissions/{permissionKey}/access`
Updates the target user's mutable catch-all runtime allowance.
This endpoint is currently specific to `email_catch_all`.

The runtime model intentionally stays separate from the append-only quantity
ledger:
- subscription time is tracked by `subscription_expires_at`
- prepaid usage is tracked by `remaining_count`
- the effective daily cap is `daily_limit_override` when set, otherwise the
  policy's `default_daily_limit`
- all time calculations use the server's UTC clock

Request example:

```json
{
  "add_subscription_days": 30,
  "remaining_count_delta": 50000,
  "daily_limit_override": 200000,
  "reason": "manual commercial grant"
}
```

### `GET /v1/admin/users/{userID}/pow-settings`
Returns the target user's current PoW daily completion settings.

The payload includes:
- the explicit per-user override, when present
- the currently effective daily completion limit
- how many PoW rewards the user already claimed today
- how many claims remain today

### `PATCH /v1/admin/users/{userID}/pow-settings`
Updates the target user's PoW daily completion override.

Request examples:

```json
{
  "daily_completion_limit_override": 12
}
```

```json
{
  "clear_daily_completion_limit_override": true
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
The admin DNS console follows the same write restriction: manual writes may
only create `A`, `AAAA`, `CNAME`, and `TXT` records.

### `PATCH /v1/admin/allocations/{allocationID}/records/{recordID}`
Updates one DNS record inside the selected allocation namespace.

### `DELETE /v1/admin/allocations/{allocationID}/records/{recordID}`
Deletes one DNS record inside the selected allocation namespace.

### `GET /v1/admin/email-routes`
Returns all administrator-managed email forwarding rules.

Administrator-side create, update, and delete operations also sync the effective route into Cloudflare Email Routing.
In `database_relay` mode, parent-root exact mailboxes still sync to Cloudflare,
while subdomain relay namespaces remain database-backed and are enforced later
by the built-in SMTP relay.

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
The `email_catch_all` policy now also exposes `default_daily_limit`, which
defaults to `1000000`.

### `GET /v1/admin/pow/settings`
Returns the full administrator-facing PoW configuration payload.

The payload groups:
- the global PoW feature switch plus the default daily limit
- the global base reward min/max range
- every benefit-specific toggle row
- every supported difficulty toggle row

### `PATCH /v1/admin/pow/settings`
Updates the singleton global PoW settings row.

Request example:

```json
{
  "enabled": true,
  "default_daily_completion_limit": 5,
  "base_reward_min": 5,
  "base_reward_max": 10
}
```

### `PATCH /v1/admin/pow/benefits/{benefitKey}`
Updates one PoW benefit toggle row.

Request example:

```json
{
  "enabled": true
}
```

### `PATCH /v1/admin/pow/difficulties/{difficulty}`
Updates one supported PoW difficulty toggle row.

Request example:

```json
{
  "enabled": false
}
```

### `GET /v1/admin/ldc/products`
Returns the full administrator-editable Linux Do Credit product list, including disabled items.

### `PATCH /v1/admin/ldc/products/{productKey}`
Updates one Linux Do Credit product row.
Administrators can currently change:
- whether the product is enabled
- the unit price
- the grant quantity per purchased unit

Request example:

```json
{
  "enabled": true,
  "unit_price": "500",
  "grant_quantity": 1
}
```

### `GET /v1/admin/ldc/products`
Returns the full Linux Do Credit product set, including disabled items.

### `GET /v1/admin/ldc/orders`
Returns recent Linux Do Credit orders across all users for the administrator console.

### `GET /v1/admin/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order without mutating its current state.

### `POST /v1/admin/ldc/orders/{outTradeNo}/refresh`
Explicitly refreshes one Linux Do Credit order from the upstream gateway.
This endpoint requires an authenticated, password-verified admin session plus
`X-CSRF-Token`.

### `PATCH /v1/admin/ldc/products/{productKey}`
Updates one administrator-editable Linux Do Credit product row.
Current mutable fields are:
- `enabled`
- `unit_price` as a decimal string with at most two fractional digits
- `grant_quantity`

Request example:

```json
{
  "enabled": true,
  "unit_price": "500",
  "grant_quantity": 1
}
```

### `GET /v1/admin/ldc/products`
Returns the full administrator-editable Linux Do Credit product list, including disabled items.

### `PATCH /v1/admin/ldc/products/{productKey}`
Updates one Linux Do Credit product row.
Administrators can currently change:
- whether the product is enabled
- the unit price
- the grant quantity per purchased unit

Request example:

```json
{
  "enabled": true,
  "unit_price": "500",
  "grant_quantity": 1
}
```

### `GET /v1/admin/ldc/products`
Returns the full Linux Do Credit product set, including disabled items.

### `GET /v1/admin/ldc/orders`
Returns recent Linux Do Credit orders across all users for the administrator console.

### `GET /v1/admin/ldc/orders/{outTradeNo}`
Returns one specific Linux Do Credit order and, when possible, refreshes it from the upstream gateway before responding.

### `PATCH /v1/admin/ldc/products/{productKey}`
Updates one administrator-editable Linux Do Credit product row.
Current mutable fields are:
- `enabled`
- `unit_price` as a decimal string with at most two fractional digits
- `grant_quantity`

Request example:

```json
{
  "enabled": true,
  "unit_price": "500",
  "grant_quantity": 1
}
```

### `PATCH /v1/admin/permission-policies/{policyKey}`
Updates one permission-policy row.

Request example:

```json
{
  "enabled": true,
  "auto_approve": true,
  "min_trust_level": 2,
  "default_daily_limit": 1000000
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

## Linux Do Credit callback endpoint

### `GET /v1/payments/linuxdo-credit/notify`
Receives the EasyPay-compatible asynchronous success notification from Linux Do Credit.
The backend verifies:
- MD5 signature
- `pid`
- `type=epay`
- `trade_status=TRADE_SUCCESS`
- local order existence
- money amount equality with the stored local order total

On success, the backend must respond with plain text `success`.

## Security model

- All authenticated state is stored in server-side sessions referenced by an HTTP-only cookie.
- Unsafe endpoints require the current session's `X-CSRF-Token`.
- Sessions can be bound to the browser's user-agent fingerprint.
- Administrator permissions are enforced server-side on every `/v1/admin/*` data endpoint.
- Banned users are blocked both at login time and on subsequent session validation.
- Administrator write operations emit audit log rows for traceability.
