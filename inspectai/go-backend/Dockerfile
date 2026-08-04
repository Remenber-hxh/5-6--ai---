# syntax=docker/dockerfile:1.7
FROM golang:1.24-alpine AS build

ENV GOTELEMETRY=off \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOPROXY=https://goproxy.cn,direct

WORKDIR /src

# Step 1: copy dependency manifests only, prime module cache
COPY go-backend/go.mod go-backend/go.sum ./go-backend/
RUN --mount=type=cache,target=/go/pkg/mod \
    cd go-backend && go mod download

# Step 2: copy source and build
COPY go-backend/ ./go-backend/
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    cd go-backend && go build -trimpath -ldflags="-s -w" -o /out/inspectai ./cmd/server

# ---- 新版移动端 ----
# 2026-08-04 起 Go 后端的根路径发的是 mobile-web/dist(旧版挪到 /old/)。
# 【必须在镜像里构建】否则容器里没有 dist,线上根路径会变成"前端还没构建"
# 的提示页 —— 等于把移动端打挂。
FROM node:20-alpine AS web
WORKDIR /web
# 先只拷清单,让依赖层能命中缓存(改前端代码不必重装依赖)
COPY mobile-web/package.json mobile-web/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY mobile-web/ ./
RUN npm run build

FROM alpine:3.20

# 切 apk 源到腾讯云镜像（国内构建提速）
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.tencent.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates wget tzdata \
 && adduser -D -u 10001 inspectai

WORKDIR /app
COPY --from=build /out/inspectai /app/inspectai
COPY frontend/ /app/frontend/
# 旧版留在 /app/frontend(挂 /old/),新版占根路径
COPY --from=web /web/dist /app/mobile-web/dist

# 预建 /app/storage 并归属非 root 用户。
# Why: 生产用 app_storage 命名卷挂载到 /app/storage。
# Docker 挂卷会继承容器内目录的 owner/perm（如果挂卷为空），
# 没有这一步，挂卷后 /app/storage 是 root:root，inspectai 用户写不进，
# 上传照片直接 permission denied。
RUN mkdir -p /app/storage \
 && chown -R inspectai:inspectai /app/storage /app

USER inspectai
EXPOSE 8080

HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health || exit 1

CMD ["/app/inspectai"]
