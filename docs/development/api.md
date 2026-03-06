# LinuxDoSpace API 文档

## 响应约定

- 成功响应：

```json
{
  "data": {}
}
```

- 失败响应：

```json
{
  "error": {
    "code": "validation_failed",
    "message": "prefix is required"
  }
}
```

## 当前已实现接口

### `GET /healthz`

用途：
返回服务存活状态、版本、环境与基础依赖配置状态。

响应示例：

```json
{
  "data": {
    "status": "ok",
    "app": "LinuxDoSpace",
    "version": "dev",
    "env": "development",
    "oauth_ready": true,
    "cf_ready": true,
    "time": "2026-03-06T00:00:00Z"
  }
}
```

### `GET /v1/public/domains`

用途：
列出当前启用中的可分发根域名。

### `GET /v1/public/supervision`

用途：
返回公开监督页所需的脱敏子域归属列表。

隐私说明：

- 只返回子域名和拥有者信息。
- 不返回任何 DNS 解析值、IP 地址、CNAME 目标或其他敏感解析数据。

### `GET /v1/public/allocations/check?root_domain=linuxdo.space&prefix=alice`

用途：
检查某个前缀是否可以分配。

### `GET /v1/auth/login?next=/settings`

用途：
发起 Linux Do OAuth 登录，并跳转到 `connect.linux.do`。

### `GET /v1/auth/callback`

用途：
完成 OAuth 回调、创建会话并重定向回前端。

### `POST /v1/auth/logout`

用途：
销毁当前登录会话。

要求：

- 需要登录
- 需要 `X-CSRF-Token`

### `GET /v1/me`

用途：
返回当前登录状态、用户资料、CSRF token 和当前用户分配列表。

### `GET /v1/my/allocations`

用途：
返回当前用户所有命名空间分配。

### `POST /v1/my/allocations`

用途：
为当前用户创建新的命名空间分配。

请求示例：

```json
{
  "root_domain": "linuxdo.space",
  "prefix": "alice",
  "source": "manual",
  "primary": true
}
```

### `GET /v1/my/allocations/{allocationID}/records`

用途：
列出当前用户某个命名空间下的全部 DNS 记录。

### `POST /v1/my/allocations/{allocationID}/records`

用途：
在命名空间内创建 DNS 记录。

请求示例：

```json
{
  "type": "A",
  "name": "@",
  "content": "1.1.1.1",
  "ttl": 1,
  "proxied": true,
  "comment": "main site"
}
```

### `PATCH /v1/my/allocations/{allocationID}/records/{recordID}`

用途：
更新命名空间中的指定 DNS 记录。

### `DELETE /v1/my/allocations/{allocationID}/records/{recordID}`

用途：
删除命名空间中的指定 DNS 记录。

### `GET /v1/admin/domains`

用途：
返回管理员视角下的全部根域名配置。

### `POST /v1/admin/domains`

用途：
创建或更新根域名配置。

请求示例：

```json
{
  "root_domain": "linuxdo.space",
  "cloudflare_zone_id": "",
  "default_quota": 1,
  "auto_provision": true,
  "is_default": true,
  "enabled": true
}
```

### `POST /v1/admin/quotas`

用途：
为指定用户设置某个根域名的分配数量。

请求示例：

```json
{
  "username": "alice",
  "root_domain": "linuxdo.space",
  "max_allocations": 3,
  "reason": "redeem-code"
}
```

## 认证与安全说明

- 登录态使用服务端 Session Cookie。
- 所有写接口都需要请求头 `X-CSRF-Token`。
- `GET /v1/me` 会返回当前会话对应的 `csrf_token`。
- DNS 记录写操作会先读取 Cloudflare 实时记录并验证记录是否属于当前用户命名空间。
