# LinuxDoSpace Deployment Guide

## Recommended production architecture

The current recommended production layout is split deployment:

- public frontend on Cloudflare Pages, for example `https://app.example.com`
- admin frontend on Cloudflare Pages, for example `https://admin.example.com`
- backend API on Debian with Docker, for example `https://api.example.com`
- PostgreSQL as the production database backend

Important note:

- The production deployment used by this project is PostgreSQL-based.
- SQLite support remains in the codebase only for local development, automated tests, and rollback-only fallback handling.

The repository still supports single-image self-hosting because the Go backend can embed the frontend build output, but the main production path used by this project is the split frontend/backend model above.

## Docker image

- Dockerfile: [Dockerfile](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/Dockerfile)
- The container listens on `8080` for HTTP and can also listen on `2525` for SMTP relay ingress when `EMAIL_FORWARDING_BACKEND=database_relay` and `MAIL_RELAY_ENABLED=true`.
- Production should use `DATABASE_DRIVER=postgres`.
- SQLite remains available for local development and rollback-only scenarios.

## Debian server preparation

Required software:

- Docker Engine
- Docker Compose plugin

Recommended deployment directory:

- `/opt/linuxdospace`

## Server files

The repository provides:

- Compose file: [deploy/docker-compose.yml](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/deploy/docker-compose.yml)
- Environment template: [deploy/linuxdospace.env.example](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/deploy/linuxdospace.env.example)

Typical Debian deployment steps:

1. Create `/opt/linuxdospace`.
2. Place `docker-compose.yml` there.
3. Place a real `.env` file there.
4. Run `docker compose pull`.
5. Run `docker compose up -d`.

## Environment variable guidance

Database selection:

- `DATABASE_DRIVER=postgres`
- `DATABASE_POSTGRES_DSN=postgres://linuxdospace:change-me@postgres:5432/linuxdospace?sslmode=disable`

SQLite compatibility fallback:

- `DATABASE_DRIVER=sqlite`
- `SQLITE_PATH=/app/data/linuxdospace.sqlite`

Existing SQLite production data can be migrated into PostgreSQL with:

```bash
cd backend
go run ./cmd/migrate-sqlite-to-postgres \
  -sqlite-path ./data/linuxdospace.sqlite \
  -postgres-dsn "postgres://linuxdospace:change-me@postgres:5432/linuxdospace?sslmode=disable"
```

For the split deployment model, the important public URLs are:

- `APP_BASE_URL=https://api.example.com`
- `APP_FRONTEND_URL=https://app.example.com`
- `APP_ADMIN_FRONTEND_URL=https://admin.example.com`
- `APP_ADMIN_VERIFICATION_TTL=30m`
- `APP_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`
- `LINUXDO_OAUTH_REDIRECT_URL=https://api.example.com/v1/auth/callback`
- `LINUXDO_CREDIT_NOTIFY_URL=https://api.example.com/v1/payments/linuxdo-credit/notify`
- `LINUXDO_CREDIT_RETURN_URL=https://app.example.com/payments/callback`

`APP_ALLOWED_ORIGINS` must include both frontend origins.

`APP_TRUSTED_PROXY_CIDRS` should only list the direct reverse-proxy hops that are allowed to supply `CF-Connecting-IP`, `X-Forwarded-For`, or `X-Real-IP`. The default loopback-only value is the safe choice when Nginx runs on the same host.

## GitHub Actions workflow

Workflow file:

- [.github/workflows/container-release.yml](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/.github/workflows/container-release.yml)

The workflow is designed to:

- build and publish the image to GHCR on pushes that should produce a release image
- publish versioned images on tag pushes
- optionally deploy the already-published image to Debian after publication succeeds

## Required GitHub secrets

GHCR publishing normally uses the built-in `GITHUB_TOKEN`.

Server deployment requires these secrets when the workflow is configured to deploy remotely:

- `DEPLOY_HOST`
- `DEPLOY_PORT` (optional, default `22`)
- `DEPLOY_USER`
- `DEPLOY_PATH` (optional, default `/opt/linuxdospace`)
- `DEPLOY_SSH_KNOWN_HOSTS`
- `DEPLOY_SSH_PRIVATE_KEY`
- `DEPLOY_ENV_FILE`
- `DEPLOY_GHCR_USERNAME`
- `DEPLOY_GHCR_TOKEN`

Notes:

- `DEPLOY_ENV_FILE` should contain the full multi-line `.env` file content.
- `DEPLOY_SSH_KNOWN_HOSTS` must contain the pinned SSH host key lines for the Debian server. Do not rely on live `ssh-keyscan` during deployment.
- `DEPLOY_GHCR_TOKEN` must have permission to pull the GHCR image.

## Post-deploy verification

On the server, verify with:

```bash
docker compose ps
docker compose logs -f
curl http://127.0.0.1:8080/healthz
docker compose exec postgres pg_isready
```

When the service is behind Nginx or another reverse proxy, also verify:

- the public frontend can call `GET /v1/me` and receive JSON
- the admin frontend can call `GET /v1/admin/me` and receive JSON
- the Linux Do OAuth callback URL matches the production API domain exactly
- CORS allows both configured frontend origins

## Cloudflare Email Routing

LinuxDoSpace now supports two mailbox-forwarding execution backends:

- `EMAIL_FORWARDING_BACKEND=cloudflare`
  The backend writes exact and catch-all forwarding rules directly into Cloudflare Email Routing.
- `EMAIL_FORWARDING_BACKEND=database_relay`
  The backend uses a hybrid model:
  exact mailboxes on the parent root domain still sync to Cloudflare Email Routing,
  while subdomain-scoped relay namespaces and catch-all delivery are executed by the built-in SMTP listener and forwarded by direct MX delivery from this service itself.

The current implementation still uses Cloudflare destination-address verification
for user-owned forwarding targets in both modes, because that remains the
existing ownership proof workflow.

Required backend environment variables:

- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_DEFAULT_ROOT_DOMAIN`
- `CLOUDFLARE_DEFAULT_ZONE_ID` (recommended for deterministic zone resolution)

Required Cloudflare token capabilities:

- DNS read/write for the managed zone
- Email Routing Addresses read/write
- Zone read

Additional requirement for `EMAIL_FORWARDING_BACKEND=cloudflare` only:

- Email Routing Rules read/write

Additional requirement for `EMAIL_FORWARDING_BACKEND=database_relay`:

- `MAIL_RELAY_ENABLED=true`
- `MAIL_RELAY_ENSURE_DNS=true`
- `MAIL_RELAY_SMTP_ADDR=:2525`
- `MAIL_RELAY_DOMAIN=mail.example.com`
- `MAIL_RELAY_HELO_DOMAIN=mail.example.com`
- `MAIL_RELAY_MX_TARGET=mail.example.com`
- `MAIL_RELAY_MX_PRIORITY=10`
- `MAIL_RELAY_SPF_VALUE=v=spf1 -all`
- `MAIL_RELAY_FORWARD_FROM=relay@mail.example.com`
- `MAIL_RELAY_MX_LOOKUP_TIMEOUT=5s`
- `MAIL_RELAY_MX_CACHE_TTL=10m`
- `MAIL_RELAY_MAX_CONCURRENT_INGRESS=128`
- `MAIL_RELAY_WORKERS=64`
- `MAIL_RELAY_MAX_DOMAIN_CONCURRENCY=8`

Operational DNS note for `database_relay`:

- `MAIL_RELAY_MX_TARGET` must be a real mail host name such as `mail.linuxdo.space`,
  not a raw IP address; the corresponding `A`/`AAAA` record must stay `DNS only`
  so SMTP can reach the server directly
- when `MAIL_RELAY_ENSURE_DNS=true`, LinuxDoSpace will create or update its own
  managed `MX` and optional `TXT` records for routed mail domains and
  subdomains, pointing them at `MAIL_RELAY_MX_TARGET`
- on startup, LinuxDoSpace also scans already-approved catch-all namespaces and
  existing subdomain relay routes, then backfills any missing relay `MX/TXT`
  records before serving traffic
- parent-root exact mailboxes intentionally do not receive these relay `MX/TXT`
  records, because they keep using Cloudflare's exact-address forwarding path
- LinuxDoSpace only updates DNS records carrying its own mail-relay comment, so
  unrelated user TXT/MX records are not rewritten
- the MX target itself must resolve to the real SMTP listener host running
  LinuxDoSpace, and that host must accept inbound SMTP on port `25`
- LinuxDoSpace now always performs direct per-domain MX delivery for outbound
  forwarding, so operators must prepare SMTP egress, `PTR/rDNS`, `HELO`,
  `SPF`, and ideally `DKIM`/`DMARC` before expecting stable deliverability

Operational notes:

- destination mailboxes must be verified in Cloudflare before LinuxDoSpace can activate forwarding rules
- `cloudflare` mode still depends on Cloudflare Email Routing DNS readiness
- `database_relay` mode depends on the built-in SMTP listener being reachable on the configured MX target and on unrestricted outbound SMTP delivery to remote MX hosts

## Frontend deployment on Cloudflare Pages

Public frontend recommended settings:

- Root directory: `frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Required environment variable: `VITE_API_BASE_URL=https://api.example.com`

Linux Do Credit return-route note:

- Configure the LDC application return URL to `https://app.example.com/payments/callback`
- This route is a dedicated frontend callback page that refreshes the order
  explicitly, waits for asynchronous server-side notify processing, and then
  guides the user back to the permissions page

Admin frontend recommended settings:

- Root directory: `admin-frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Required environment variable: `VITE_API_BASE_URL=https://api.example.com`

## Admin security notes

- the admin frontend requires Linux Do OAuth, backend admin authorization, and one extra password verification
- all real write operations still go through backend sessions, CSRF validation, and audit logging
