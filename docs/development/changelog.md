# LinuxDoSpace Changelog

## Unreleased

- Hardened administrator configuration so the backend now fails closed unless `APP_ADMIN_USERNAMES` and `APP_ADMIN_PASSWORD` are explicitly configured together.
- Added configuration tests that verify development defaults stay locked down and production rejects incomplete admin protection settings.
- Tightened admin identity resolution so only the local administrator allowlist can unlock the admin console, even if Linux Do marks a user as a forum administrator.
- Added rate limiting and failure audit logs to `POST /v1/admin/verify-password` so the extra admin password cannot be brute-forced indefinitely.

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
