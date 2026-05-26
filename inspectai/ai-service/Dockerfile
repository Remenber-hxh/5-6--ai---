# syntax=docker/dockerfile:1.7
FROM python:3.13-slim AS base

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_DISABLE_PIP_VERSION_CHECK=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

# 系统级依赖：curl 是 run.py 调用 dashscope 的传输层，ca-certificates 走 HTTPS
RUN apt-get update \
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
