# syntax=docker/dockerfile:1.7

# frontend-builder 阶段负责构建 Vite 前端静态资源。
# 这里使用官方 Node 镜像，以确保 `npm ci` 与 `vite build` 在受控环境中执行。
FROM node:22-bookworm-slim AS frontend-builder

WORKDIR /workspace/frontend

# 先复制依赖描述文件，以充分利用 Docker layer cache。
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci

# 再复制前端源码并执行生产构建。
COPY frontend/ ./
RUN npm run build


# backend-builder 阶段负责把前端产物嵌入 Go 二进制，并构建最终服务端程序。
FROM golang:1.25-bookworm AS backend-builder

ARG VERSION=dev

WORKDIR /workspace/backend

# 先下载 Go 依赖，减少后续重复构建耗时。
COPY backend/go.mod backend/go.sum ./
RUN go mod download

# 复制后端源码，再把前端构建产物放入嵌入目录。
COPY backend/ ./
COPY --from=frontend-builder /workspace/frontend/dist/ /workspace/backend/web/dist/

# 构建单个可执行文件，后续最终镜像只需要这个二进制。
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -trimpath \
    -ldflags="-s -w -X main.version=${VERSION}" \
    -o /out/linuxdospace \
    ./cmd/linuxdospace


# runtime 阶段是最终运行镜像。
# 选择 Debian slim 以兼顾体积、证书可用性以及 Debian 服务器上的调试友好性。
FROM debian:bookworm-slim AS runtime

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/*

# 创建非 root 运行用户，降低容器被利用后的影响面。
RUN groupadd --system --gid 10001 linuxdospace \
    && useradd --system --uid 10001 --gid 10001 --create-home --home-dir /app linuxdospace \
    && mkdir -p /app/data \
    && chown -R 10001:10001 /app

WORKDIR /app

# 把编译产物复制进最终镜像。
COPY --from=backend-builder /out/linuxdospace /usr/local/bin/linuxdospace

# 这里设置容器内默认环境变量，方便 Debian 服务器直接通过 `.env` 覆盖。
ENV APP_ENV=production \
    APP_ADDR=:8080 \
    DATABASE_DRIVER=postgres \
    EMAIL_FORWARDING_BACKEND=database_relay \
    MAIL_RELAY_ENABLED=false \
    MAIL_RELAY_SMTP_ADDR=:2525 \
    MAIL_RELAY_ENSURE_DNS=true \
    MAIL_RELAY_MX_TARGET=mail.linuxdo.space \
    MAIL_RELAY_SPF_VALUE="v=spf1 -all" \
    APP_SESSION_SECURE=true

EXPOSE 8080
EXPOSE 2525
VOLUME ["/app/data"]

# 直接使用后端自身的健康检查接口作为容器健康检查。
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD curl --fail --silent http://127.0.0.1:8080/healthz >/dev/null || exit 1

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/linuxdospace"]
