# LinuxDoSpace 更新日志

## 0.1.0-alpha.1

- 初始化 Git 仓库。
- 建立 Go 后端基础骨架。
- 增加配置加载、SQLite 初始化和 SQL 迁移。
- 增加 Linux Do / Cloudflare 客户端初版。
- 增加 `GET /healthz` 健康检查接口。
- 建立开发文档目录与基础文档。

## 0.2.0-alpha.1

- 增加 Linux Do OAuth 登录流程、会话创建和退出登录。
- 增加服务端 Session、CSRF 校验和 User-Agent 指纹绑定。
- 增加根域名配置、用户配额覆盖和命名空间分配能力。
- 增加 Cloudflare 实时 DNS 记录创建、查询、更新和删除。
- 增加管理员接口和审计日志写入。
- 增加单元测试与 Cloudflare 真实集成测试。
