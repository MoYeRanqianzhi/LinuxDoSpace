# Mail Stream Protocol

本文档定义 LinuxDoSpace 邮件 Token SDK 的当前公共协议约束。

## 目标

该协议用于让客户端通过一个 API Token 建立一条长连接，持续接收服务器转发的邮件事件。

## 传输层

- 方法：`GET`
- 路径：`/v1/token/email/stream`
- 鉴权：`Authorization: Bearer <token>`
- 协议：远端必须使用 `HTTPS`
- 本地调试例外：`localhost`、`127.0.0.1`、`::1` 与 `*.localhost` 可使用 `HTTP`
- 响应内容类型：`application/x-ndjson; charset=utf-8`

## 数据格式

服务端按行持续输出 JSON，每一行都是一个独立事件。

示例：

```json
{"type":"ready","token_public_id":"tok123","owner_username":"testuser"}
{"type":"heartbeat"}
{"type":"mail","original_envelope_from":"bounce@example.com","original_recipients":["alice@testuser.linuxdo.space"],"received_at":"2026-03-20T10:11:12Z","raw_message_base64":"RnJvbTogLi4u"}
```

## 事件类型

### `ready`

表示连接已经建立成功，当前 SDK 应忽略该事件或仅用于内部握手。

`owner_username` 是必需字段。SDK 使用它把第一方枚举后缀
`linuxdo.space` 解析成当前 token 拥有者的真实命名空间后缀
`<owner_username>.linuxdo.space`。

示例：

```json
{
  "type": "ready",
  "token_public_id": "tok123",
  "owner_username": "testuser"
}
```

### `heartbeat`

表示连接保活，SDK 应忽略该事件，不向最终用户暴露。

示例：

```json
{
  "type": "heartbeat"
}
```

### `mail`

表示收到一封真实邮件。

字段：

- `original_envelope_from`
- `original_recipients`
- `received_at`
- `raw_message_base64`

示例：

```json
{
  "type": "mail",
  "original_envelope_from": "bounce@example.com",
  "original_recipients": [
    "alice@testuser.linuxdo.space"
  ],
  "received_at": "2026-03-20T10:11:12Z",
  "raw_message_base64": "RnJvbTogU2VuZGVyIDxzZW5kZXJAZXhhbXBsZS5jb20+DQpUbzogUmVjZWl2ZXIgPGFsaWNlQGxpbnV4ZG8uc3BhY2U+DQpTdWJqZWN0OiBUZXN0DQoNCkhlbGxv"
}
```

## SDK 公共行为

### 上游连接

- `Client` 创建时立即尝试建立连接
- 若初始连接失败，应直接报错
- 若连接在后续中断，SDK 可在后台重连
- 若鉴权失败，应停止并抛出认证错误

### 完整监听

- `client.listen(...)` 是完整接收接口
- 它应接收当前 Token 收到的每一封邮件

### 本地绑定

- 绑定基于邮箱地址的 local-part 与 suffix
- 第一方枚举后缀 `linuxdo.space` 是语义后缀，不是字面父域名
- SDK 必须把它解析为 `<owner_username>.linuxdo.space`
- `prefix` 与 `pattern` 二选一
- `pattern` 使用全匹配，不是搜索匹配
- 同一 suffix 下的所有绑定共享一条创建顺序链

### 匹配顺序

- 精确绑定和正则绑定没有独立优先级
- 只按创建顺序匹配
- 某条绑定命中后：
  - 必定收到消息
  - 若 `allow_overlap=false`，则立即停止
  - 若 `allow_overlap=true`，则继续向后匹配

### 队列语义

- 绑定在 `bind(...)` 时立即注册
- mailbox 队列在 `listen()` 开始时才激活
- `listen()` 之前到达的消息不会回填
- 同一个 mailbox 同时只允许一个活动监听器

## 邮件解析

SDK 应至少产出这些公共字段：

- `address`
- `sender`
- `recipients`
- `received_at`
- `subject`
- `message_id`
- `date`
- `from_header`
- `to_header`
- `cc_header`
- `reply_to_header`
- `from_addresses`
- `to_addresses`
- `cc_addresses`
- `reply_to_addresses`
- `text`
- `html`
- `headers`
- `raw`
- `raw_bytes` 或等价二进制字段

说明：

- 各语言 SDK 的 MIME 深解析能力可以不同
- 但以上字段至少要有稳定的公开访问方式
- 当某些字段无法从当前邮件中解析时，应返回空值而不是伪造数据
