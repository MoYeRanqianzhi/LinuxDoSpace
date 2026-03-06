# LinuxDoSpace 架构说明

## 当前架构分层

- `frontend/`：Vite 前端展示层。
- `backend/cmd/linuxdospace`：Go 进程入口。
- `backend/internal/config`：环境变量配置加载。
- `backend/internal/storage/sqlite`：SQLite 持久化层。
- `backend/internal/linuxdo`：Linux Do OAuth / 用户信息客户端。
- `backend/internal/cloudflare`：Cloudflare API 客户端。
- `backend/internal/service`：认证、配额、域名分配和 DNS 业务规则层。
- `backend/internal/httpapi`：HTTP 路由与响应层。
- `backend/internal/security`：随机令牌、指纹和安全辅助函数。

## 安全设计原则

- 不信任来自浏览器的任何用户输入。
- 不把敏感令牌写入仓库。
- 默认使用服务端会话而不是把身份信息直接暴露给前端。
- 对所有写操作执行 CSRF 校验。
- DNS 记录更新前必须先验证记录属于用户自己的命名空间。
- 所有重要操作都写入审计日志。

## 核心数据对象

- `users`：Linux Do 登录用户。
- `sessions`：服务端会话，绑定 CSRF token，并可选绑定 User-Agent 指纹。
- `oauth_states`：一次性 OAuth state / PKCE verifier。
- `managed_domains`：允许平台分发的根域名与 Cloudflare zone 绑定。
- `user_domain_quotas`：用户在某个根域名上的配额覆盖值。
- `allocations`：用户获得的命名空间，例如 `alice.linuxdo.space`。
- `audit_logs`：关键动作审计日志。

## DNS 命名空间模型

- 用户拥有的是一个命名空间，而不是单条记录。
- 当用户持有 `alice.linuxdo.space` 时，可管理：
  - `alice.linuxdo.space`
  - `www.alice.linuxdo.space`
  - `api.v2.alice.linuxdo.space`
- 用户不能管理：
  - `linuxdo.space`
  - `bob.linuxdo.space`
  - 任何不属于自己分配前缀后缀的记录

## 当前权衡

- 为了保证越权检查准确，记录列表与冲突检查会直接读取 Cloudflare 实时 DNS 记录。
- 当前没有做 Cloudflare 结果缓存，因此大规模数据下还需要进一步优化读取性能。

## 下一步架构扩展

- 将现有前端页面改为真实调用后端 API。
- 增加兑换码 / L 站积分兑换流程。
- 增加更细粒度的速率限制、审计检索和后台管理界面。
