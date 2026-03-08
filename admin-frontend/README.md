# LinuxDoSpace Admin Frontend

This project hosts the standalone administrator console extracted from `new-ui-design`.
It is designed to be deployed to a separate Cloudflare Pages site while talking to the shared LinuxDoSpace backend.

## Current scope

- Real Linux Do administrator login via the backend OAuth flow
- Real administrator session bootstrap via `GET /v1/admin/me`
- Real user moderation, quota management, managed-domain management, DNS record management, email routes, application review, and redeem-code generation
- Hash-based client navigation so a static Cloudflare Pages deployment works without SPA rewrite rules

## Local development

```bash
npm install
npm run dev
```

Default local port: `3001`

## Environment variables

- `VITE_API_BASE_URL`
  - Points the admin frontend at the shared backend origin.
  - Example: `http://localhost:8080`
  - When the admin frontend and backend are served from the same origin, this can be left empty.

## Build

```bash
npm run build
```

Build output directory: `dist/`

## Cloudflare Pages

Recommended settings:

- Root directory: `admin-frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Environment variable: `VITE_API_BASE_URL=https://api.linuxdo.space`

## Required backend configuration

The shared backend must allow the admin frontend origin and know where to redirect after admin OAuth login.
At minimum configure:

- `APP_ADMIN_FRONTEND_URL`
- `APP_ALLOWED_ORIGINS`
- `APP_ADMIN_USERNAMES`
- Linux Do OAuth credentials
- Cloudflare API token

## Security notes

- The admin frontend never stores a backend secret.
- All write operations rely on backend sessions, administrator authorization, CSRF validation, and server-side audit logs.
- If a logged-in Linux Do account is not in the backend administrator allowlist, the admin frontend will refuse access.
