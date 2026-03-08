# LinuxDoSpace 閮ㄧ讲璇存槑

## 閮ㄧ讲褰㈡€?
褰撳墠浠撳簱閲囩敤鍗曢暅鍍忛儴缃叉柟妗堬細

- GitHub Actions 鏋勫缓鍓嶇闈欐€佽祫婧?- Go 鍚庣鎶婂墠绔瀯寤轰骇鐗╁祵鍏ヤ簩杩涘埗
- Debian 鏈嶅姟鍣ㄥ彧闇€瑕佽繍琛屼竴涓鍣?
杩欐牱鍙互閬垮厤鍓嶅悗绔媶鍒嗛儴缃插甫鏉ョ殑璺ㄥ煙銆佸洖璋冨湴鍧€鍜岄潤鎬佽祫婧愬悓姝ラ棶棰樸€?
## Docker 闀滃儚

- Dockerfile锛氫粨搴撴牴鐩綍 [Dockerfile](/G:/ClaudeProjects/LinuxDoSpace/Dockerfile)
- 杩愯鏃堕暅鍍忛粯璁ょ洃鍚鍣ㄥ唴 `8080`
- SQLite 鏁版嵁搴撻粯璁ゆ寕杞藉埌 `/app/data/linuxdospace.sqlite`

## Debian 鏈嶅姟鍣ㄥ噯澶?
闇€瑕佸畨瑁咃細

- Docker Engine
- Docker Compose Plugin

鎺ㄨ崘閮ㄧ讲鐩綍锛?
- `/opt/linuxdospace`

## 鏈嶅姟鍣ㄦ枃浠?
浠撳簱鎻愪緵锛?
- Compose 鏂囦欢锛歔deploy/docker-compose.yml](/G:/ClaudeProjects/LinuxDoSpace/deploy/docker-compose.yml)
- 鐜鍙橀噺妯℃澘锛歔deploy/linuxdospace.env.example](/G:/ClaudeProjects/LinuxDoSpace/deploy/linuxdospace.env.example)

鍦?Debian 鏈嶅姟鍣ㄤ笂锛岄€氬父闇€瑕侊細

1. 鍒涘缓 `/opt/linuxdospace`
2. 鏀惧叆 `docker-compose.yml`
3. 鏀惧叆 `.env`
4. 鎵ц `docker compose pull`
5. 鎵ц `docker compose up -d`

## GitHub Actions 宸ヤ綔娴?
宸ヤ綔娴佹枃浠讹細

- [container-release.yml](/G:/ClaudeProjects/LinuxDoSpace/.github/workflows/container-release.yml)

鍔熻兘锛?
- push 鍒?`main` 鏃惰嚜鍔ㄦ瀯寤哄苟鎺ㄩ€侀暅鍍忓埌 GHCR
- push 鐗堟湰 tag 鏃惰嚜鍔ㄦ瀯寤哄苟鎺ㄩ€佸搴?tag 闀滃儚
- `workflow_dispatch` 鎵嬪姩瑙﹀彂鏃跺彲閫夌洿鎺ラ儴缃插埌 Debian 鏈嶅姟鍣?
## 闇€瑕侀厤缃殑 GitHub Secrets

鏋勫缓鎺ㄩ€佸埌 GHCR锛?
- 榛樿浣跨敤 `GITHUB_TOKEN`锛屾棤闇€棰濆 Secrets

鎵嬪姩閮ㄧ讲鍒?Debian 鏈嶅姟鍣ㄦ椂闇€瑕侊細

- `DEPLOY_HOST`
- `DEPLOY_PORT`锛堝彲閫夛紝榛樿 `22`锛?- `DEPLOY_USER`
- `DEPLOY_PATH`锛堝彲閫夛紝榛樿 `/opt/linuxdospace`锛?- `DEPLOY_SSH_PRIVATE_KEY`
- `DEPLOY_ENV_FILE`
- `DEPLOY_GHCR_USERNAME`
- `DEPLOY_GHCR_TOKEN`

鍏朵腑锛?
- `DEPLOY_ENV_FILE` 搴旀槸瀹屾暣鐨勫琛?`.env` 鏂囦欢鍐呭
- `DEPLOY_GHCR_TOKEN` 闇€瑕佸叿澶囪鍙?GHCR 闀滃儚鐨勬潈闄?
## 閮ㄧ讲鍚庨獙璇?
鍙互鍦ㄦ湇鍔″櫒涓婃墽琛岋細

```bash
docker compose ps
docker compose logs -f
curl http://127.0.0.1:8080/healthz
```

濡傛灉鏈嶅姟瀵瑰缁忚繃鍙嶄唬锛岃繕搴旈獙璇侊細

- 鍓嶇棣栭〉鏄惁鍙闂?- `/v1/me` 鏄惁鍙繑鍥炴湭鐧诲綍鐘舵€?- Linux Do OAuth 鍥炶皟鍦板潃鏄惁涓庣敓浜у煙鍚嶄竴鑷?

## OAuth 注意事项

- LINUXDO_OAUTH_REDIRECT_URL 必须指向后端回调地址，例如 https://api.linuxdo.space/v1/auth/callback`r
- LINUXDO_OAUTH_SCOPE 建议保持为 user，与 Linux Do 官方示例一致


## Admin Frontend (Cloudflare Pages)

The administrator frontend is now a real standalone application that talks to the shared backend:

- [admin-frontend/README.md](/G:/ClaudeProjects/LinuxDoSpace/admin-frontend/README.md)

Recommended Cloudflare Pages settings:

- Root directory: `admin-frontend`
- Build command: `npm run build`
- Build output directory: `dist`
- Required env: `VITE_API_BASE_URL=https://api.linuxdo.space`

Backend requirements for the admin frontend:

- `APP_ADMIN_FRONTEND_URL` must point to the deployed admin site
- `APP_ALLOWED_ORIGINS` must include the admin frontend origin
- `APP_ADMIN_USERNAMES` must list the Linux Do usernames allowed to access the admin console
- `APP_ADMIN_PASSWORD` should be set to a private value instead of relying on the temporary default password

Security notes:

- The admin frontend now requires Linux Do admin OAuth plus one extra backend password verification.
- All real write operations go through backend sessions, admin authorization, admin second-factor verification, CSRF validation, and audit logging.
