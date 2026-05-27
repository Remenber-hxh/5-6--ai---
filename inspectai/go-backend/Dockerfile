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

FROM alpine:3.20

# 切 apk 源到腾讯云镜像（国内构建提速）
RUN sed -i 's|dl-cdn.alpinelinux.org|mirrors.tencent.com|g' /etc/apk/repositories \
 && apk add --no-cache ca-certificates wget tzdata \
 && adduser -D -u 10001 inspectai

WORKDIR /app
COPY --from=build /out/inspectai /app/inspectai
COPY frontend/ /app/frontend/

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
