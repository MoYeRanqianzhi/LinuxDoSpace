# 域名销售设计说明

## 目标

本功能用于在公共域名搜索页中，为额外根域名提供 Linux Do Credit 购买入口。
它与现有“临时免费同名直领”逻辑分离：

- 免费直领：仅允许开启免费流的根域名，并且前缀必须与 Linux Do 用户名同名
- 付费购买：允许 sale-enabled 根域名按固定长度倍率售卖额外命名空间

## 当前内置根域名

后端启动时会尝试自动引导以下附加根域名：

- `cifang.love`
- `openapi.best`
- `metapi.cc`

这些根域名默认：

- `enabled=true`
- `auto_provision=false`
- `sale_enabled=false`
- `sale_base_price_cents=0`

也就是说，它们会先进入系统，但不会在未定价前直接开放销售。

## 定价规则

每个根域名的基础价格由管理员在 `managed_domains.sale_base_price_cents` 中单独设置。
前端与后端共用以下固定倍率：

### 精确购买

- 1 字符：不出售
- 2 字符：15 倍
- 3 字符：10 倍
- 4 字符：5 倍
- 5 字符：2 倍
- 6 字符及以上：1 倍

### 随机字符购买

- 仅支持 `12` 到 `63` 位
- 价格固定为 `0.5` 倍基础价
- 具体字符在支付成功并发放时才随机生成
- 随机模式使用向上取整到分，避免出现半分金额

## 订单模型

动态域名购买仍走统一的 `payment_orders` 表，但额外记录以下上下文：

- `purchase_root_domain`
- `purchase_mode`
- `purchase_prefix`
- `purchase_normalized_prefix`
- `purchase_requested_length`
- `purchase_assigned_prefix`
- `purchase_assigned_fqdn`

这允许系统在支付成功后自动发放 allocation，并在订单记录中保留最终分配结果。

## API

### `GET /v1/public/domains`

新增公开字段：

- `sale_enabled`
- `sale_base_price_cents`

### `POST /v1/my/ldc/domain-orders`

创建一个动态域名购买订单。

精确购买请求示例：

```json
{
  "root_domain": "openapi.best",
  "mode": "exact",
  "prefix": "hello"
}
```

随机购买请求示例：

```json
{
  "root_domain": "openapi.best",
  "mode": "random",
  "random_length": 12
}
```

行为约束：

- 仅当根域名 `sale_enabled=true` 且 `sale_base_price_cents>0` 时可下单
- 精确购买会实时检查数据库占用与 Cloudflare 现存 DNS 冲突
- 随机购买不会在下单前暴露具体字符
- 支付成功后由后端自动创建 allocation

## 前端行为

公共 `Domains` 页面新增了“购买更多命名空间”区块：

- 与搜索区共页显示
- 支持“精确购买”和“随机字符购买”两种模式切换
- 精确购买依赖当前查询结果
- 随机购买独立于查询结果
- 支付订单会写入本地 recent-order 队列，便于无参回调页恢复订单状态

## 待人工审核的保留前缀

常见保留前缀不会自动写入生产规则。
审核草案位于：

- `docs/development/reserved-domain-prefixes-review.md`

当前版本只提供审核清单，不会自动占位这些前缀，避免在未确认前把保留集写死到生产数据库。
