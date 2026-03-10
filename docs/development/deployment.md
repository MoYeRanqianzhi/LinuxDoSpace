# LinuxDoSpace Deployment Guide

## Recommended production architecture

The current recommended production layout is split deployment:

- public frontend on Cloudflare Pages, for example `https://app.example.com`
- admin frontend on Cloudflare Pages, for example `https://admin.example.com`
- backend API on Debian with Docker, for example `https://api.example.com`

The repository still supports single-image self-hosting because the Go backend can embed the frontend build output, but the main production path used by this project is the split frontend/backend model above.

## Docker image

- Dockerfile: [Dockerfile](/G:/ClaudeProjects/LinuxDoSpace/Dockerfile)
- The container listens on `8080` internally.
- SQLite data is stored at `/app/data/linuxdospace.sqlite` by default.

## Debian server preparation

Required software:

- Docker Engine
- Docker Compose plugin

Recommended deployment directory:

- `/opt/linuxdospace`

## Server files

The repository provides:

- Compose file: [deploy/docker-compose.yml](/G:/ClaudeProjects/LinuxDoSpace/deploy/docker-compose.yml)
- Environment template: [deploy/linuxdospace.env.example](/G:/ClaudeProjects/LinuxDoSpace/deploy/linuxdospace.env.example)

Typical Debian deployment steps:

1. Create `/opt/linuxdospace`.
2. Place `docker-compose.yml` there.
3. Place a real `.env` file there.
4. Run `docker compose pull`.
5. Run `docker compose up -d`.

## Environment variable guidance

For the split deployment model, the important public URLs are:

- `APP_BASE_URL=https://api.example.com`
- `APP_FRONTEND_URL=https://app.example.com`
- `APP_ADMIN_FRONTEND_URL=https://admin.example.com`
- `APP_ADMIN_VERIFICATION_TTL=30m`
- `APP_TRUSTED_PROXY_CIDRS=127.0.0.1/32,::1/128`
- `LINUXDO_OAUTH_REDIRECT_URL=https://api.example.com/v1/auth/callback`

`APP_ALLOWED_ORIGINS` must include both frontend origins.

`APP_TRUSTED_PROXY_CIDRS` should only list the direct reverse-proxy hops that are allowed to supply `CF-Connecting-IP`, `X-Forwarded-For`, or `X-Real-IP`. The default loopback-only value is the safe choice when Nginx runs on the same host.

## GitHub Actions workflow

Workflow file:

- [.github/workflows/container-release.yml](/G:/ClaudeProjects/LinuxDoSpace/.github/workflows/container-release.yml)

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
```

When the service is behind Nginx or another reverse proxy, also verify:

- the public frontend can call `GET /v1/me` and receive JSON
- the admin frontend can call `GET /v1/admin/me` and receive JSON
- the Linux Do OAuth callback URL matches the production API domain exactly
- CORS allows both configured frontend origins

## Cloudflare Email Routing

Mailbox forwarding now depends on Cloudflare Email Routing in addition to DNS.

Required backend environment variables:

- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `CLOUDFLARE_DEFAULT_ROOT_DOMAIN`
- `CLOUDFLARE_DEFAULT_ZONE_ID` (recommended for deterministic zone resolution)

Required Cloudflare token capabilities:

- DNS read/write for the managed zone
- Email Routing Addresses read/write
- Email Routing Rules read/write
- Zone read

Operational notes:

- destination mailboxes must be verified in Cloudflare before LinuxDoSpace can activate forwarding rules
- permission-gated mailbox routes such as `catch-all@<username>.linuxdo.space` now have their required Email Routing MX and SPF records ensured automatically by the backend before the exact mailbox rule is synced
- the zone must already have the Email Routing DNS records in place

## Frontend deployment on Cloudflare Pages

Public frontend recommended settings:

- Root directory: `frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Required environment variable: `VITE_API_BASE_URL=https://api.example.com`

Admin frontend recommended settings:

- Root directory: `admin-frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Required environment variable: `VITE_API_BASE_URL=https://api.example.com`

## Admin security notes

- the admin frontend requires Linux Do OAuth, backend admin authorization, and one extra password verification
- all real write operations still go through backend sessions, CSRF validation, and audit logging
