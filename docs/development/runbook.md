# LinuxDoSpace Runbook

## Local backend startup

1. Enter `backend/`.
2. Copy or reference [backend/.env.example](/G:/ClaudeProjects/LinuxDoSpace/backend/.env.example) and fill real values.
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
docker run --rm -p 8080:8080 --env-file deploy/linuxdospace.env.example linuxdospace:local
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

Cloudflare Email Routing also requires the API token to include Email Routing Addresses and Email Routing Rules permissions in addition to the existing DNS permissions.

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
