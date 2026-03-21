# LinuxDoSpace（佬友空间）

LinuxDoSpace 是一个基于 Cloudflare DNS 的二级域名分发站，主要面向 Linux Do 社区用户。

当前仓库包含：

- `frontend/`：基于 Vite 的前端演示界面。
- `backend/`：基于 Go 的后端服务。
- `docs/development/`：项目开发文档、API 文档、已知问题与变更记录。
- `sdk/`：邮件实时流的多语言 SDK。

SDK 说明：

- Python SDK 位于 `sdk/python`
- 当前 PyPI 安装命令是 `pip install linuxdospace`
- 多语言 SDK 协议说明见 [sdk/spec/MAIL_STREAM_PROTOCOL.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/sdk/spec/MAIL_STREAM_PROTOCOL.md)

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
- Linux Do Credit 支付、订单查询、回调和权益发放
- 服务端 SMTP 中转、目标邮箱验证与 API Token 邮件流
- Proof-of-Work 福利与原子化奖励发放
- 独立管理员后端 API 与 `admin-frontend/`

当前前端已实现：

- 登录态同步与 OAuth 跳转
- 真实域名可用性查询
- 命名空间申请
- 命名空间内 DNS 记录管理
- URL 与当前页面状态同步
- 邮箱目标验证、邮箱转发、订单列表与支付回调
- PoW 福利与数量/订阅额度展示

当前部署能力已实现：

- 基于 PostgreSQL 的 Docker 部署
- 前端静态资源嵌入 Go 二进制
- GitHub Actions 自动构建并发布到 GHCR
- 可选通过 SSH 自动部署到 Debian 服务器
- tag 触发的验证、镜像发布与远端部署

数据库说明：

- 当前生产部署使用 PostgreSQL。
- 仓库仍保留 SQLite 兼容实现，但它只用于本地开发、测试和回滚兜底场景，不是当前线上主数据库。

当前仍在持续完善：

- SMTP 生产投递质量运维能力（rDNS / DKIM / DMARC / 信誉）
- 多语言 SDK 的持续回归验证与发布
- 更完整的退款、对账与运营报表能力

部署参考见 [deployment.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/deployment.md)。

详细开发文档见 [docs/development/README.md](/G:/ClaudeProjects/LinuxDoSpace/LinuxDoSpace/docs/development/README.md)。
