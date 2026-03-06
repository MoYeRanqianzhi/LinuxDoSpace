# LinuxDoSpace 更新日志

## 0.5.1-alpha.1

- 修复 GitHub Actions 工作流语法错误。
- 避免在 job 级 `if` 条件中直接引用 `secrets.*`，改为仅判断手动部署输入。
- 增加部署 job 内部的 secret 校验步骤，确保缺失配置时明确失败。

## 0.5.0-alpha.1

- 增加单镜像 Docker 部署方案，前端构建产物会嵌入 Go 二进制。
- 增加根目录 `Dockerfile` 与 `.dockerignore`。
- 增加 Debian 服务器使用的 `docker-compose.yml` 与环境变量模板。
- 增加 GitHub Actions 容器构建、GHCR 发布与可选 Debian SSH 部署工作流。
- 补充部署文档、运行手册和发布说明。

## 0.4.1-alpha.1

- 修复 `Agents.md` 被错误提交到仓库的问题。
- 在 `.gitignore` 中增加 `Agents.md` 与 `AGENTS.md` 忽略规则。
- 将已跟踪的 `Agents.md` 从 Git 索引移除，但保留本地文件。

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

## 0.4.0-alpha.1

- 前端接入后端真实 API，不再使用随机占用状态和本地 mock 记录。
- 增加前端统一 API 客户端、类型定义和环境变量配置。
- 增加前端登录态同步、OAuth 跳转和 URL 与 tab 状态同步。
- 增加前端 allocation 申请、DNS 记录查询、创建、更新和删除能力。
- 在保留原有 UI 设计风格的前提下完成真实业务联调。
