# Multi-language SDK Notes

本文档记录 LinuxDoSpace 多语言邮件实时流 SDK 的统一约束，避免不同语言实现逐步偏离。

## 目录

- 总览：`sdk/README.md`
- 协议：`sdk/spec/MAIL_STREAM_PROTOCOL.md`
- Python 子仓库：`sdk/python`
- 其余语言 SDK：均为独立 Git 仓库，通过父仓库 submodule 接入

## 统一目标

所有语言 SDK 都应尽量保持这些核心语义一致：

- 一个 `Client` 对应一条真实上游连接
- 上游路径固定为 `/v1/token/email/stream`
- 鉴权方式固定为 `Authorization: Bearer <token>`
- 服务端只知道 Token，不知道客户端内部绑定规则
- 客户端本地负责 mailbox 匹配、顺序路由和 overlap 控制
- `client.listen(...)` 是完整流入口
- `mailbox.listen(...)` 是本地子队列入口

## 统一匹配规则

- `prefix` 与 `pattern` 必须二选一
- 精确匹配与正则匹配共享一条创建顺序链
- 命中后若 `allow_overlap=false` 则立即停止
- 命中后若 `allow_overlap=true` 则继续向后匹配
- `route(message)` 只基于当前 `message.address` 做只读匹配，不做历史回放

## 队列与生命周期

- `bind(...)` 时即注册匹配规则
- mailbox 只有在 `listen()` 激活后才开始接收消息
- `listen()` 之前到达的消息不会回填
- 同一个 mailbox 同时只允许一个活动监听器
- `close()` 或离开作用域后应立即解绑

## 验证策略

- 当前开发机可直接验证：Node.js、Go、Rust、Java、GCC、G++
- 当前开发机不可直接验证：Swift、Dart、Zig、Lua、Kotlin、.NET SDK
- 对于无法本地验证的语言，README 必须明确写出：
  - 当前依赖
  - 推荐构建命令
  - 当前环境未验证的事实

当前一轮落地结果：

- 已本机验证：Rust、Go、Node.js(TypeScript)、Java、C、C++
- 已落代码但未本机验证：Kotlin、C#、Swift、Dart、Lua、Zig
- C/C++ 当前为传输无关核心实现，其余语言为完整 SDK 工程

## 发布顺序

当一次变更同时触及主仓库和 SDK 时，统一按以下顺序发布：

1. 先推送 SDK 子仓库。
2. 等各 SDK 仓库自己的 `ci.yml` / `release.yml` 通过。
3. 再回到父仓库更新 submodule 指针并提交。
4. 最后发布父仓库 tag，触发主服务镜像与 Debian 部署。

这样可以避免父仓库已经引用了一个尚未发布或尚未通过 CI 的 SDK 提交。

## 当前协议说明

当前主仓库最近一轮安全/一致性修复没有修改 `/v1/token/email/stream`
的 wire format。本轮 SDK 工作重点仍然是保持各语言对现有协议的实现
一致，而不是引入新的协议版本。

## Python SDK 特殊说明

- Python SDK 是独立 Git 子仓库
- 其余语言 SDK 现在也都是独立 Git 仓库
- 当前 PyPI 安装命令明确为 `pip install linuxdospace`
- 父仓库只跟踪 submodule 指针，不直接承载 Python SDK 的提交历史
