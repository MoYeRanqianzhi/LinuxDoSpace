# LinuxDoSpace 架构说明

## 当前架构分层

- `frontend/`：Vite 前端展示层。
- `backend/cmd/linuxdospace`：Go 进程入口。
- `backend/internal/config`：环境变量配置加载。
- `backend/internal/storage`：数据库无关的存储接口与 DTO。
- `backend/internal/storage/sqlite`：SQLite 持久化实现，主要用于开发、测试和回滚兜底。
- `backend/internal/storage/postgres`：PostgreSQL 持久化实现，面向生产部署。
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
- `managed_domains`：允许平台分发的根域名与 Cloudflare zone 绑定，并保存每个根域名单独的销售开关与基础价格。
- `user_domain_quotas`：用户在某个根域名上的配额覆盖值。
- `allocations`：用户获得的命名空间，例如 `alice.linuxdo.space`。
- `quantity_records`：面向未来收费、兑换码、订阅和手动赠送场景的追加式数量账本。
- `payment_products`：Linux Do Credit 可购买项目定义，保存启用状态、单价、发放数量和效果类型。
- `payment_orders`：Linux Do Credit 本地订单表，保存业务单号、网关状态、支付 URL、支付成功时间、权益发放时间，以及动态域名购买的根域名/模式/最终分配结果。
- `email_catch_all_access`：邮箱泛解析的可变运行时状态，保存订阅到期时间、剩余次数和用户级日限额覆盖值。
- `email_catch_all_daily_usage`：邮箱泛解析按 UTC 日期累计的当日用量，用于执行单人单日最高限额。
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
- 当前线上主数据库是 PostgreSQL；SQLite 已不作为生产主库使用。
- 当前 PostgreSQL 实现优先保持与 SQLite 一致的 repository 语义，因此布尔值仍使用整数标记、时间戳仍使用 RFC3339 文本存储，以降低迁移期的行为偏差。
- 数量相关能力当前采用“不可变记录 + 实时汇总余额”模型，避免后续收费逻辑直接修改余额而丢失审计链路。
- 邮箱泛解析收费能力当前采用“双层模型”：数量账本只保留审计历史，真正执行时读取单独的订阅/剩余次数状态，并以服务端 UTC 日期执行单人单日限额。
- Linux Do Credit 支付能力当前采用“三段式闭环”：先保留本地订单、再创建上游支付链接、最后通过查询或异步回调幂等发放权益，避免重复到账。

## 下一步架构扩展

- 将现有前端页面改为真实调用后端 API。
- 增加 Linux Do Credit 退款、后台订单检索与更完整的对账能力。
- 增加更细粒度的速率限制、审计检索和后台管理界面。
