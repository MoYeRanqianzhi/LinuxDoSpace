# LinuxDoSpace Changelog

## Unreleased

- Added per-root-domain sale settings plus public dynamic namespace purchase flow on the domain search page, with fixed length multipliers, hidden random 12+ character purchases, and built-in bootstrap roots for `cifang.love`, `openapi.best`, and `metapi.cc`.
- Added dedicated dynamic Linux Do Credit domain-order creation plus paid entitlement application so successful checkouts can automatically create new allocation namespaces.
- Added a reviewed-but-not-auto-applied reserved-prefix audit draft for common single-character, digit, infrastructure, and high-value prefixes.
- Added database-relay ingress DNS backfill on catch-all save and service
  startup, so previously approved namespace mail domains are repaired
  automatically after operators switch MX targets or migrate servers.
- Removed the optional upstream relay branch from the built-in SMTP relay and
  standardized outbound delivery on direct MX, with MX caching, explicit HELO
  identity, and per-domain concurrency limiting.
- Added administrator-manageable PoW configuration, including a global feature switch, base reward min/max, per-benefit toggles, per-difficulty toggles, and per-user daily completion overrides.
- Updated the public PoW panel so realtime attempt count, best progress, and elapsed time now appear directly inside the “当前题目” area.
- Added per-user proof-of-work welfare challenges under the public permissions page, with server-generated Argon2id puzzles, browser-side local solving, daily UTC claim limits, and atomic reward grants into `email_catch_all_remaining_count`.
- Added Linux Do Credit purchase products, local payment orders, EasyPay-compatible signature verification, asynchronous notify handling, and idempotent entitlement application for catch-all subscription days, catch-all quota, and payment-test purchases.
- Added administrator Linux Do Credit order APIs plus a dedicated admin order-management page for cross-user purchase inspection and manual status refresh.
- Added dual runtime billing modes for `email_catch_all`: subscription time and prepaid remaining count, with subscription taking priority and all catch-all mail still capped by a configurable per-user UTC-day limit.
- Added mutable catch-all access tables plus SMTP relay enforcement so `*@<username>.linuxdo.space` now checks real subscription/count state instead of relying only on permission approval.
- Added administrator policy support for `default_daily_limit` and a dedicated admin endpoint for per-user catch-all access adjustments.
- Added an append-only quantity ledger plus user/admin quantity APIs so future billing, redeem-code, and subscription work can track auditable resource deltas instead of mutating balances in place.
- Added a built-in SMTP relay mode controlled by `EMAIL_FORWARDING_BACKEND=database_relay`, allowing LinuxDoSpace to receive mail itself and forward it according to database-stored routes instead of relying on Cloudflare catch-all delivery.
- Added database-relay DNS bootstrap so LinuxDoSpace can automatically create its own managed MX/TXT records for routed mail domains and subdomains instead of depending on manual DNS setup.
- Added `internal/mailrelay` with SMTP recipient resolution, route ownership checks, relay-loop protection headers, and upstream SMTP forwarding.
- Added relay-aware config validation, health fields, deployment variables, Docker port exposure, and service tests that lock `database_relay` mode into a Cloudflare-free route execution path.
- Added a storage-agnostic `internal/storage` contract layer so the service package no longer depends directly on SQLite DTO types.
- Added a new PostgreSQL backend implementation with embedded PostgreSQL migrations and placeholder rebinding for the existing repository SQL.
- Added runtime database driver selection through `DATABASE_DRIVER`, `DATABASE_POSTGRES_DSN`, and `DATABASE_URL`, while keeping SQLite available for local development and rollback.
- Added a one-shot `cmd/migrate-sqlite-to-postgres` migration command for moving existing SQLite production data into PostgreSQL.
- Updated deployment templates and documentation to recommend PostgreSQL for production instead of SQLite-only persistence.
- Rebuilt the admin console switch and select controls with the same custom-rendered approach used on the public frontend, fixing native-control rendering glitches across platforms.
- Added searchable administrator user selectors for long user lists in the admin domain-allocation and email-routing forms.
- Fixed the public configuration center so `/v1/me` and `/v1/my/allocations` now return every namespace already owned by the user, including administrator-granted namespaces that do not match the Linux Do username.
- Removed the accidental DNS-management lockout that previously restricted users to the root record of their default same-name namespace; owned namespaces now expose their full in-namespace record set again.
- Redesigned the public settings page so the default namespace and every extra namespace appear as explicit selectable cards instead of being hidden behind a single implicit default view.
- Added a documented child-zone probe workflow for real subdomain catch-all validation, plus a reusable PowerShell script that can bootstrap `test.<root-domain>` once the Cloudflare token has zone-create permission.
- Recorded the 2026-03-11 architecture finding that the public same-zone `catch_all?subdomain=` API is not a safe basis for multi-user real catch-all support.
- Hardened OAuth callback completion so per-state browser cookies support concurrent login tabs and SQLite now consumes the OAuth state only when the session insert succeeds.
- Added live session invalidation hooks for both frontends so expired public or admin sessions are reflected without requiring a full-page reload.
- Tightened reverse-proxy trust boundaries with configurable `APP_TRUSTED_PROXY_CIDRS`, defaulting to loopback-only forwarding headers for admin password rate limiting.
- Improved admin console resilience by splitting application-list and policy loading, surfacing email-route modal validation errors, and refusing to fake a successful logout when the backend did not confirm it.
- Added real Cloudflare Email Routing synchronization for user-managed and administrator-managed mailbox forwards, including verified destination-address checks and exact-address rule sync.
- Documented the new `CLOUDFLARE_ACCOUNT_ID` requirement plus the Email Routing token scopes needed for production deployments.
- Added the first real user-facing permission flow for `*@<username>.linuxdo.space`, including policy-backed auto-approval, administrator policy controls, and end-to-end frontend integration for the email and permission pages.
- Added persistent permission-policy storage plus user-side email-route APIs so catch-all forwarding now has a real backend instead of preview-only UI.
- Added direct administrator permission controls inside the user management flow so admins can inspect and override `email_catch_all` status per user, with review notes kept alongside the application record.
- Added administrator allocation lifecycle controls so the admin console can now create namespaces, transfer ownership, disable allocations, and reassign the primary namespace without editing SQLite manually.
- Restored the public anime background as an opt-out browser preference, defaulting to enabled and exposing the toggle from a new navbar settings button beside the GitHub icon.
- Hardened administrator configuration so the backend now fails closed unless `APP_ADMIN_USERNAMES` and `APP_ADMIN_PASSWORD` are explicitly configured together.
- Added configuration tests that verify development defaults stay locked down and production rejects incomplete admin protection settings.
- Tightened admin identity resolution so only the local administrator allowlist can unlock the admin console, even if Linux Do marks a user as a forum administrator.
- Added rate limiting and failure audit logs to `POST /v1/admin/verify-password` so the extra admin password cannot be brute-forced indefinitely.
- Removed the administrator console's third-party background image dependency so loading the page no longer leaks admin access metadata to an external host.
- Rebuilt the public frontend shell with the same local-only background approach so the main site also avoids third-party image requests while keeping the existing page wiring intact.
- Added one shared storage-behavior test suite plus opt-in PostgreSQL integration tests so key repository semantics are no longer validated only through SQLite.
- Hardened PostgreSQL SQL placeholder rebinding so literal `?` characters inside strings, identifiers, and comments are no longer rewritten by mistake.
- Added a PostgreSQL index migration focused on allocation lists, admin review lists, email target sorting, and the public supervision audit-log scan.

## 0.6.0

- Added a real standalone administrator console integration for `admin-frontend`.
- Added administrator Linux Do OAuth login, backend session bootstrap, server-side authorization checks, CSRF validation, and audit-log-backed write operations.
- Added persistent administrator APIs for users, managed domains, DNS records, email routes, application review, and redeem codes.
- Fixed the public supervision page so it only shows subdomains that are verifiably still in active use.
- Unified the brand icon so the main site and favicon both use `ICON.png`.

## 0.5.3-alpha.23

- Replaced the previous text-based navbar mark with `ICON.png` on the main frontend.

## 0.5.3-alpha.22

- Corrected public supervision ownership listing so unused placeholder allocations are no longer exposed.
- Added SQLite tests that validate the supervision filtering rules.

## 0.5.3-alpha.21

- Extracted the administrator UI from `new-ui-design` into a standalone `admin-frontend` Vite project.
- Added the initial standalone administrator UI prototype, navigation, and Cloudflare Pages deployment notes.

## 0.5.3-alpha.20

- Updated the release pipeline to publish `ghcr.io/moyeranqianzhi/linuxdospace:latest` before Debian deployment.
- Kept Debian deployment aligned with `docker pull ghcr.io/moyeranqianzhi/linuxdospace:latest` based updates.

## Earlier history

Earlier alpha release history remains available in Git history and tags.
