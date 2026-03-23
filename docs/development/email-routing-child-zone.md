# LinuxDoSpace 子域名 Catch-All 设计验证

## 背景

LinuxDoSpace 当前需要的邮箱能力不是字面邮箱 `catch-all@<username>-mail.linuxdo.space`，而是**真实 catch-all**：

- `*@<username>-mail.linuxdo.space`

这意味着 Cloudflare 必须能够把某个用户命名空间视为独立的邮件接收边界，再把所有未命中的地址统一转发到用户配置的目标邮箱。

2026-03-11 这轮验证的目标只有一个：

- 判断 `test.linuxdo.space` 是否可以作为一条**真实、可自动化**的技术路线跑通；
- 只有在设计被验证可行后，才继续推进后端实现。

## 已验证事实

以下结论均来自 2026-03-11 的官方资料核对与真实 Cloudflare API 调用。

### 1. 当前公开 API 能读取子域名所需的 Email Routing DNS

请求：

- `GET /zones/{linuxdo.space zone_id}/email/routing/dns?subdomain=test.linuxdo.space`

结果：

- Cloudflare 返回了 `test.linuxdo.space` 所需的 3 条 MX 和 1 条 SPF(TXT) 记录；
- 这说明 Cloudflare 后端**知道**子域名级邮件路由需要哪些 DNS 记录。

这一步只能证明：

- 子域名级 Email Routing DNS 查询能力存在。

这一步**不能**证明：

- 子域名级真实 catch-all 已经有可安全使用的公开 API。

### 2. 当前公开 API 仍然无法把 catch-all 稳定限定到某个子域名

请求：

- `GET /zones/{linuxdo.space zone_id}/email/routing/rules/catch_all?subdomain=test.linuxdo.space`

结果：

- 返回的仍然是现有的 `*@windy-wu.linuxdo.space` catch-all 规则；
- 返回的规则 `id` 不是一个新的子域名独立资源。

结合前一轮已经做过的写入测试，可以确认：

- 当前公开 `catch_all` API 不能作为“一个 zone 下多个子域名独立 catch-all”的后端基础；
- 继续沿用“同 zone + `subdomain=` 查询参数”的思路，仍然会有覆盖其他用户 catch-all 的风险。

### 3. Child Zone 路线在架构上成立，但当前生产 token 缺权限

我们尝试创建 `test.linuxdo.space` 作为独立 Cloudflare Zone：

- `POST /zones`

Cloudflare 返回明确错误：

- `Requires permission "com.cloudflare.api.account.zone.create" to create zones for the selected account`

这说明：

- `test.linuxdo.space` 作为 child zone 的路线在 Cloudflare 模型上是成立的；
- 但当前用于生产的 API Token **没有**创建 zone 的账号级权限，因此无法完成全自动联调。

### 4. 当前父域已经托管在 Cloudflare，子域委托不一定需要 Spaceship API

已验证 `linuxdo.space` 当前状态：

- 注册商原始 NS 仍然显示为 Spaceship；
- 实际权威 DNS 已经切到 Cloudflare；
- Cloudflare 当前 name servers 为：
  - `gabriel.ns.cloudflare.com`
  - `rosalyn.ns.cloudflare.com`

因此对 `test.linuxdo.space` 做 NS 委托时，实际修改点是：

- 在父 zone `linuxdo.space` 里写入 `test.linuxdo.space` 的 NS 记录；
- 不一定非要调用 Spaceship API。

只有在“父域 DNS 仍由 Spaceship 直接托管”时，才必须通过 Spaceship DNS API 改委托。

## 当前结论

截至 2026-03-11，LinuxDoSpace 要实现“每个用户一个独立真实 catch-all”，唯一可靠方向是：

1. 为用户子域名创建独立 child zone，例如 `test.linuxdo.space`；
2. 在父 zone `linuxdo.space` 中为该 child zone 写入 NS 委托；
3. 在 child zone 内启用 Cloudflare Email Routing；
4. 在 child zone 内配置 root-level catch-all：
   - `*@test.linuxdo.space -> target@example.com`

不能继续采用的旧思路：

- 在同一个 `linuxdo.space` zone 里，用公开 `catch_all?subdomain=` API 直接给多个用户做独立真实 catch-all。

## LinuxDoSpace 后端设计含义

这会直接改变邮件模块的数据模型与 Cloudflare 集成模型。

必须新增的概念：

- 父级托管根域：
  - 例如 `linuxdo.space`
- 邮件 child zone：
  - 例如 `alice-mail.linuxdo.space`
- 父域委托记录：
  - 父 zone 内的 NS 记录集合
- child zone 生命周期：
  - 创建
  - 激活等待
  - Email Routing 启用
  - catch-all 写入
  - 停用与回收

这意味着后端不能再把真实 catch-all 视为：

- “在父 zone 内补几条 MX/TXT 再调用一次 catch-all API”

而必须视为：

- “一个拥有独立 zone 生命周期的邮件命名空间”

## 完整联调前的阻塞项

要把 `test.linuxdo.space` 真正跑通，当前还缺一项权限：

- Cloudflare API Token 需要账号级权限：
  - `com.cloudflare.api.account.zone.create`

如果继续使用 Cloudflare API Token，建议最少补齐下列能力：

- 父 zone `linuxdo.space`：
  - Zone read
  - DNS read/write
- 账号级：
  - Zone create
- child zone：
  - Zone read
  - DNS read/write
  - Email Routing Addresses read/write
  - Email Routing Rules read/write

## 下一步执行顺序

1. 更新 Cloudflare Token，使其具备 child zone 创建权限。
2. 运行 `deploy/probe-child-zone-email-routing.ps1`，对 `test.linuxdo.space` 做完整探针。
3. 只有探针确认可闭环后，才开始改 LinuxDoSpace 后端的数据结构与业务流程。

## 参考资料

- Cloudflare Email Routing Subdomains：
  - `https://developers.cloudflare.com/email-routing/email-workers/subdomains/`
- Cloudflare Email Routing Catch-All API：
  - `https://developers.cloudflare.com/api/resources/email_routing/subresources/rules/subresources/catch_all/`
- Cloudflare Zones API：
  - `https://developers.cloudflare.com/api/resources/zones/`
- Spaceship DNS Records API：
  - `https://docs.spaceship.dev/#tag/DNS-records`
