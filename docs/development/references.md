# LinuxDoSpace 参考资料

以下资料均在 2026-03-06 至 2026-03-11 调研并用于本轮实现校验。

## Linux Do OAuth

- Linux Do Connect 文档：
  `https://wiki.linux.do/Community/LinuxDoConnect`
- 文档中提到的关键端点：
  - 授权地址：`https://connect.linux.do/oauth2/authorize`
  - Token 地址：`https://connect.linux.do/oauth2/token`
  - 用户信息：`https://connect.linux.do/api/user`

## Cloudflare DNS

- Cloudflare API Zones List：
  `https://developers.cloudflare.com/api/go/resources/zones/methods/list/`
- Cloudflare API DNS Records：
  `https://developers.cloudflare.com/api/go/resources/dns/subresources/records/`
- Cloudflare Email Routing Subdomains：
  `https://developers.cloudflare.com/email-routing/email-workers/subdomains/`
- Cloudflare Email Routing Catch-All API：
  `https://developers.cloudflare.com/api/resources/email_routing/subresources/rules/subresources/catch_all/`

## Spaceship DNS

- Spaceship DNS Records API：
  `https://docs.spaceship.dev/#tag/DNS-records`

## 本地验证结果

- 2026-03-06 已验证提供的 Cloudflare API Token 处于 `active` 状态。
- 2026-03-06 已验证 `linuxdo.space` 的 Cloudflare Zone ID 为 `9a1e91c12c5575164bf31d0988fd2954`。
- 2026-03-06 已通过集成测试完成临时 TXT 记录的创建、读取和删除闭环。
- 2026-03-11 已验证 `GET /zones/{zone_id}/email/routing/dns?subdomain=test.linuxdo.space` 会返回子域名级 Email Routing 所需的 MX/SPF 记录。
- 2026-03-11 已验证 `GET /zones/{zone_id}/email/routing/rules/catch_all?subdomain=test.linuxdo.space` 仍会返回当前已存在的 catch-all 规则，而不是 `test.linuxdo.space` 的独立 catch-all 资源。
- 2026-03-11 已验证当前生产 Cloudflare Token 缺少 `com.cloudflare.api.account.zone.create`，因此无法直接通过 API 创建 `test.linuxdo.space` child zone。
