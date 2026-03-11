# LinuxDoSpace（佬友空间）

LinuxDoSpace 是一个基于 Cloudflare DNS 的二级域名分发站，主要面向 Linux Do 社区用户。

当前仓库包含：

- `frontend/`：基于 Vite 的前端演示界面。
- `backend/`：基于 Go 的后端服务。
- `docs/development/`：项目开发文档、API 文档、已知问题与变更记录。

当前开发重点是后端基础能力：

- Linux Do OAuth 登录
- Cloudflare DNS 记录安全管理
- 多根域名分发
- 配额与子域名归属控制
- 审计日志与安全校验

当前后端已实现：

- Linux Do OAuth 授权码登录流程
- 服务端会话与 `X-CSRF-Token` 写接口保护
- `linuxdo.space` 等根域名自动引导
- 用户命名空间分配与配额覆盖
- Cloudflare DNS 记录实时 CRUD
- 管理员域名配置与用户配额接口
- 可切换的 SQLite / PostgreSQL 持久化层

当前前端已实现：

- 登录态同步与 OAuth 跳转
- 真实域名可用性查询
- 命名空间申请
- 命名空间内 DNS 记录管理
- URL 与当前页面状态同步

当前部署能力已实现：

- 基于 PostgreSQL 的 Docker 部署
- 前端静态资源嵌入 Go 二进制
- GitHub Actions 自动构建并发布到 GHCR
- 可选通过 SSH 自动部署到 Debian 服务器

当前未完成：

- 前端 API 联调
- 兑换码 / L 站积分兑换流程
- 管理后台页面

部署参考见 [deployment.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/deployment.md)。

详细开发文档见 [docs/development/README.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/README.md)。
