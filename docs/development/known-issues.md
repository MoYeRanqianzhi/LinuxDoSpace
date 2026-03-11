# LinuxDoSpace 已知问题

## 当前阶段

- 前端已接入核心 API，但还没有实现管理员域名管理与配额管理界面。
- Linux Do OAuth 已按公开接入文档实现，但尚未在真实 `client_id` / `client_secret` 下完成完整登录联调。
- 当前 DNS 记录列表与命名空间冲突检查会读取 Cloudflare 全量记录；在记录规模很大时需要引入缓存或更细的索引策略。
- 当前没有实现兑换码、L 站积分兑换和后台界面，只提供了管理员配额接口作为后端能力基础。
- 当前 Docker 发布工作流默认发布 `linux/amd64` 单架构镜像；如果未来需要 ARM 服务器，还需要补充多架构构建与验证。
- 当前真实 catch-all 邮件能力仍未定稿。2026-03-11 的实测结果表明，同一个 `linuxdo.space` zone 下不能继续依赖公开 `catch_all?subdomain=` API 为多个用户安全创建独立子域名 catch-all；后续必须改为 child zone + NS 委托模型。
- 当前用于生产的 Cloudflare Token 缺少 `com.cloudflare.api.account.zone.create`，因此 `test.linuxdo.space` 的 child-zone 联调尚未真正跑通。
