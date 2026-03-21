# LinuxDoSpace 已知问题

## 当前阶段

- Linux Do OAuth 已按公开接入文档实现，但尚未在真实 `client_id` / `client_secret` 下完成完整登录联调。
- Linux Do Credit 已接入下单、查询、回调和商品配置，但当前还没有退款接口、后台订单检索页和自动对账报表。
- 当前 Docker 发布工作流默认发布 `linux/amd64` 单架构镜像；如果未来需要 ARM 服务器，还需要补充多架构构建与验证。
- 当前邮件转发已切换到服务端 SMTP 中转，但交付质量仍取决于服务器公网 `25` 端口、`PTR/rDNS`、`HELO`、`SPF`、`DKIM`、`DMARC` 等邮件基础设施是否完善。
- 如果根域名本身仍由 Cloudflare Email Routing 接管，Cloudflare 会拒绝 LinuxDoSpace 通过通用 DNS API 自动改写该根域名的 `MX` 记录；当前版本会把这个特定场景降级为启动警告而不是直接退出，实际根域名收信链路仍需要运维侧手动规划。
- 代码中仍保留 `EMAIL_FORWARDING_BACKEND=cloudflare` 的历史兼容路径；在数据库中转模式稳定后，应考虑进一步收缩这条回滚分支，降低后续维护复杂度。
- 多语言 SDK 当前均有独立 CI / release，但并非每一种语言都能在当前开发机上做运行时实测，跨平台回归仍需要持续补强。
