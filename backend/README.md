# LinuxDoSpace Backend

本目录包含 LinuxDoSpace 的 Go 后端实现。

当前后端设计目标：

- 只接受 Linux Do OAuth 登录
- 使用 Cloudflare API 管理 `linuxdo.space` 等根域名下的 DNS 记录
- 通过本地 SQLite 保存用户、会话、配额、分配记录和审计日志
- 保持“从不信任，始终验证”的零信任风格

当前已实现的后端能力：

- `GET /healthz`
- `GET /v1/public/domains`
- `GET /v1/public/supervision`
- `GET /v1/public/allocations/check`
- `GET /v1/auth/login`
- `GET /v1/auth/callback`
- `POST /v1/auth/logout`
- `GET /v1/me`
- `GET/POST /v1/my/allocations`
- `GET/POST/PATCH/DELETE /v1/my/allocations/{allocationID}/records`
- `GET /v1/admin/domains`
- `POST /v1/admin/domains`
- `POST /v1/admin/quotas`

当前部署方式：

- 本仓库提供单镜像 Docker 构建
- 前端生产构建产物会在镜像构建阶段复制到 `backend/web/dist`
- 后端会直接托管前端静态资源与 API

本地运行：

```powershell
cd backend
go run ./cmd/linuxdospace
```

建议阅读顺序：

1. [docs/development/README.md](/G:/ClaudeProjects/LinuxDoSpace/docs/development/README.md)
2. [docs/development/architecture.md](/G:/ClaudeProjects/LinuxDoSpace/docs/development/architecture.md)
3. [docs/development/api.md](/G:/ClaudeProjects/LinuxDoSpace/docs/development/api.md)
