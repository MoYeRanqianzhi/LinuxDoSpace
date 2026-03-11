# LinuxDoSpace Changelog

## Unreleased

- Fixed the public configuration center so `/v1/me` and `/v1/my/allocations` now return every namespace already owned by the user, including administrator-granted namespaces that do not match the Linux Do username.
- Removed the accidental DNS-management lockout that previously restricted users to the root record of their default same-name namespace; owned namespaces now expose their full in-namespace record set again.
- Redesigned the public settings page so the default namespace and every extra namespace appear as explicit selectable cards instead of being hidden behind a single implicit default view.
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
