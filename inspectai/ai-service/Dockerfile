# syntax=docker/dockerfile:1.7
FROM python:3.13-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

# 切 apt 源到腾讯云镜像（国内构建提速 10-50 倍）。
# Why: python:3.13-slim 基于 debian trixie，默认 deb.debian.org 国内拉很慢；
#      trixie 用 DEB822 格式（/etc/apt/sources.list.d/debian.sources）。
RUN if [ -f /etc/apt/sources.list.d/debian.sources ]; then \
      sed -i 's|http://deb.debian.org|http://mirrors.tencent.com|g; s|http://security.debian.org|http://mirrors.tencent.com|g' /etc/apt/sources.list.d/debian.sources; \
    elif [ -f /etc/apt/sources.list ]; then \
      sed -i 's|http://deb.debian.org|http://mirrors.tencent.com|g; s|http://security.debian.org|http://mirrors.tencent.com|g' /etc/apt/sources.list; \
    fi \
 && apt-get update \
 && apt-get install -y --no-install-recommends curl ca-certificates \
 && rm -rf /var/lib/apt/lists/*

# Step 1: 依赖层（变更频率低，命中缓存）
COPY ai-service/requirements.txt /app/requirements.txt
RUN pip install --no-cache-dir -r /app/requirements.txt

# Step 2: 业务代码
COPY ai-service/ /app/

# 非 root 运行
RUN useradd --system --uid 10001 --home /app inspectai \
 && chown -R inspectai:inspectai /app
USER inspectai

EXPOSE 9100

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS http://127.0.0.1:9100/health || exit 1

CMD ["python", "run.py"]
