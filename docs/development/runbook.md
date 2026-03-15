# LinuxDoSpace Runbook

## Local backend startup

1. Enter `backend/`.
2. Copy or reference [backend/.env.example](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/backend/.env.example) and fill real values.
3. Run `go run ./cmd/linuxdospace`.
4. Open `http://localhost:8080/healthz` and confirm the service is healthy.
5. Call `GET /v1/public/domains` to verify the default managed root domain is available.

## Local frontend startup

1. Enter `frontend/`.
2. Set `VITE_API_BASE_URL`, usually `http://localhost:8080` for local development.
3. Run `npm install`.
4. Run `npm run dev`.
5. Open `http://localhost:3000`.
6. The login button should redirect to `${VITE_API_BASE_URL}/v1/auth/login`.

## Local Docker build

From the repository root:

```powershell
docker build -t linuxdospace:local --build-arg VERSION=local .
```

Run the container with:

```powershell
docker run --rm -p 8080:8080 -p 2525:2525 --env-file deploy/linuxdospace.env.example linuxdospace:local
```

## Required dependencies

- Go 1.25.x
- Node.js and npm
- SQLite
- Cloudflare API token
- Linux Do OAuth client credentials

## Key environment variables

- `APP_SESSION_SECRET`
- `CLOUDFLARE_ACCOUNT_ID`
- `CLOUDFLARE_API_TOKEN`
- `LINUXDO_OAUTH_CLIENT_ID`
- `LINUXDO_OAUTH_CLIENT_SECRET`
- `LINUXDO_OAUTH_REDIRECT_URL`
- `EMAIL_FORWARDING_BACKEND`

When `EMAIL_FORWARDING_BACKEND=database_relay`, also configure:

- `MAIL_RELAY_ENABLED=true`
- `MAIL_RELAY_ENSURE_DNS=true`
- `MAIL_RELAY_SMTP_ADDR=:2525`
- `MAIL_RELAY_DOMAIN`
- `MAIL_RELAY_HELO_DOMAIN`
- `MAIL_RELAY_MX_TARGET`
- `MAIL_RELAY_MX_PRIORITY`
- `MAIL_RELAY_SPF_VALUE`
- `MAIL_RELAY_FORWARD_FROM`
- `MAIL_RELAY_MX_LOOKUP_TIMEOUT`
- `MAIL_RELAY_MX_CACHE_TTL`
- `MAIL_RELAY_MAX_DOMAIN_CONCURRENCY`

Cloudflare Email Routing also requires the API token to include Email Routing Addresses and Email Routing Rules permissions in addition to the existing DNS permissions.

## PostgreSQL integration tests

The repository now includes opt-in PostgreSQL repository integration tests.

To run them:

1. Provision one disposable PostgreSQL database that is safe for test schemas.
2. Export `LINUXDOSPACE_TEST_POSTGRES_DSN`.
3. Run `go test ./internal/storage/postgres`.

The test harness creates one isolated schema per test case, runs migrations
inside that schema, and drops the schema during cleanup.

## Production PostgreSQL cutover

This production cutover was completed on `2026-03-11` against the live `remote4` deployment.

Execution summary:

1. Start a dedicated PostgreSQL container on the production compose stack.
2. Keep the original SQLite file in place until the new image and migrations are verified.
3. Freeze writes long enough to create a final SQLite backup.
4. Run `go run ./cmd/migrate-sqlite-to-postgres` with the frozen SQLite snapshot and the production PostgreSQL DSN.
5. Switch the backend container to `DATABASE_DRIVER=postgres`.
6. Verify public health checks and key list endpoints before considering the cutover finished.

Final frozen-source counts at cutover time:

- `users=634`
- `sessions=879`
- `managed_domains=1`
- `allocations=625`
- `audit_logs=1725`
- `email_routes=33`
- `admin_applications=13`
- `permission_policies=1`
- `email_targets=43`

Recorded backup artifacts:

- Remote frozen SQLite backup: `data/backups/linuxdospace.sqlite.postgres-cutover-20260311-220736.bak`
- Local migration staging backup: `.migration-staging/postgres-final-cutover-20260311-220736/linuxdospace.sqlite.postgres-cutover.bak`

Post-cutover validation:

- `GET /healthz` must return `200` with `"status":"ok"`.
- `GET /v1/public/domains` must return JSON.
- `GET /v1/public/supervision` must return JSON.
- The reported runtime version must match the released PostgreSQL-capable commit.

Rollback rule:

- If the PostgreSQL container boots but the application migrations or repository queries fail, redeploy the last known-good SQLite image first, restore read-write traffic, then fix the PostgreSQL issue offline.

## Verification checklist

After local startup, verify:

- `GET /healthz` returns `200`
- `GET /v1/me` returns an anonymous session payload when not logged in
- the public frontend can load domains and email search data
- Linux Do OAuth redirects back to the configured backend callback
- saving a mailbox forward returns JSON, not an HTML error page

## Troubleshooting notes

- When OAuth is not configured, authentication endpoints should fail closed instead of pretending to work.
- If `CLOUDFLARE_DEFAULT_ZONE_ID` is empty, the backend will resolve the zone through the Cloudflare API.
- If the frontend reports a non-JSON API response, check `VITE_API_BASE_URL` and reverse-proxy routing first.
- If mailbox forwarding save fails, verify that the target mailbox has already completed Cloudflare destination-address verification.
- If `database_relay` mode is enabled and inbound mail never arrives, verify LinuxDoSpace created the managed MX/TXT records you expect, the MX target resolves correctly, and host port `25` reaches the container's SMTP listener on `2525`.
- If `database_relay` mode is enabled and mail is accepted but not forwarded, verify outbound TCP `25` egress, remote MX reachability, `PTR/rDNS`, `MAIL_RELAY_HELO_DOMAIN`, and recipient-domain SMTP replies.
