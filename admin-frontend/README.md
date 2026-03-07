# LinuxDoSpace Admin Frontend

这个目录是从 `new-ui-design` 中拆分出的独立管理员前端工程，目标是单独部署到另一个 Cloudflare Pages。

## 当前状态

- 已保留管理员端的玻璃拟态视觉风格
- 已拆分为独立的 Vite + React + Tailwind CSS v4 工程
- 当前仅为 UI 原型，尚未接入真实管理员鉴权和后端管理 API
- 支持本地演示口令、主题切换、移动端导航和 hash 标签页切换

## 本地开发

```bash
npm install
npm run dev
```

默认开发端口为 `3001`。

## 构建

```bash
npm run build
```

产物目录为 `dist/`。

## Cloudflare Pages 配置

建议设置：

- Root directory: `admin-frontend`
- Build command: `npm run build`
- Build output directory: `dist`

可选环境变量：

- `VITE_ADMIN_DEMO_PASSWORD`
  说明：管理员 UI 原型的演示口令。未配置时默认使用 `linuxdospace-admin-demo`。

## 说明

该工程目前故意没有接入真实管理员 API，避免在后台协议和权限模型尚未稳定时提前绑定错误接口。
后续如果要接入真实管理能力，建议优先补充：

1. 服务端管理员身份鉴权
2. 用户、域名、邮箱、兑换码和审核接口
3. 审计日志与操作回滚能力
