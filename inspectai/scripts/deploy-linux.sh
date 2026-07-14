#!/usr/bin/env bash
# InspectAI 一键部署脚本（Linux + Docker + Secrets）
#
# 前置条件：
#   1. 已安装 docker + docker compose v2
#   2. 已编辑 .env.prod（只填非敏感配置；从 .env.prod.example 复制）
#   3. 已执行 bash scripts/prepare-secrets.sh 生成 ./secrets/* 文件
#
# 用法：
#   bash scripts/deploy-linux.sh
#
# 详细说明见 docs/DEPLOY.md。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE="docker compose --env-file .env.prod -f docker-compose.prod.yml"

# ---------- 前置检查 ----------

if ! command -v docker >/dev/null 2>&1; then
  echo "ERROR: docker 未安装。先装：curl -fsSL https://get.docker.com | sh" >&2
  exit 1
fi

if ! docker compose version >/dev/null 2>&1; then
  echo "ERROR: docker compose v2 插件不可用。" >&2
  exit 1
fi

if [ ! -f ".env.prod" ]; then
  cp ".env.prod.example" ".env.prod"
  echo "ERROR: .env.prod 不存在，已从 .env.prod.example 创建。" >&2
  echo "       编辑它填非敏感配置后重跑本脚本。" >&2
  echo "       注意：密钥/密码不要写进 .env.prod！全部走 secrets/。" >&2
  exit 2
fi

# 防回退：旧版可能把密钥写在 .env.prod 里
if grep -Eq "^DASHSCOPE_API_KEY=[^[:space:]]+|^DEEPSEEK_API_KEY=[^[:space:]]+|^MYSQL_PASSWORD=[^[:space:]]+|^MYSQL_ROOT_PASSWORD=[^[:space:]]+|^INSPECTAI_ADMIN_PASSWORD=[^[:space:]]+" .env.prod; then
  echo "ERROR: .env.prod 含明文密钥，新版方案禁止。" >&2
  echo "       把所有密钥从 .env.prod 移除，改走 secrets/：" >&2
  echo "         bash scripts/prepare-secrets.sh" >&2
  exit 3
fi

# 检查 secrets 文件存在且非空
SECRETS_DIR="$ROOT_DIR/secrets"
REQUIRED_SECRETS=(
  dashscope_api_key
  deepseek_api_key
  mysql_password
  mysql_root_password
  mysql_dsn
  inspectai_auth_token
  inspectai_supervisor_token
  inspectai_admin_password
)

if [ ! -d "$SECRETS_DIR" ]; then
  echo "ERROR: secrets/ 目录不存在。先跑：" >&2
  echo "       bash scripts/prepare-secrets.sh" >&2
  exit 4
fi

missing=()
for name in "${REQUIRED_SECRETS[@]}"; do
  path="$SECRETS_DIR/$name"
  if [ ! -s "$path" ]; then
    missing+=("$name")
  fi
done

if [ "${#missing[@]}" -gt 0 ]; then
  echo "ERROR: 以下 secret 缺失或为空：" >&2
  for n in "${missing[@]}"; do echo "  - secrets/$n" >&2; done
  echo "       重跑 prepare-secrets.sh 或手动补齐。" >&2
  exit 5
fi

echo "✓ 前置检查通过"
echo "  .env.prod 已就位（不含明文密钥）"
echo "  secrets/ 下 ${#REQUIRED_SECRETS[@]} 个文件齐全"

# 可选 secret：compose 会挂载该文件；为空代表相关能力未启用。
if [ ! -e "$SECRETS_DIR/wework_app_secret" ]; then
  : > "$SECRETS_DIR/wework_app_secret"
  chmod 600 "$SECRETS_DIR/wework_app_secret"
  chown "${INSPECTAI_CONTAINER_UID:-10001}:${INSPECTAI_CONTAINER_GID:-10001}" "$SECRETS_DIR/wework_app_secret" 2>/dev/null || true
  echo "  已创建可选空 secret：secrets/wework_app_secret"
fi
if [ ! -e "$SECRETS_DIR/wework_bot_webhook" ]; then
  : > "$SECRETS_DIR/wework_bot_webhook"
  chmod 600 "$SECRETS_DIR/wework_bot_webhook"
  chown "${INSPECTAI_CONTAINER_UID:-10001}:${INSPECTAI_CONTAINER_GID:-10001}" "$SECRETS_DIR/wework_bot_webhook" 2>/dev/null || true
  echo "  已创建可选空 secret：secrets/wework_bot_webhook"
fi
echo

# 首次部署还没有证书，必须先用 HTTP-only 配置完成 ACME challenge。
# 证书安装后再次运行本脚本，会自动切换为 HTTPS 配置。
HTTPS_CERT="$ROOT_DIR/nginx/ssl/jadeast.cloud.crt"
HTTPS_KEY="$ROOT_DIR/nginx/ssl/jadeast.cloud.key"
if [ -s "$HTTPS_CERT" ] && [ -s "$HTTPS_KEY" ]; then
  export INSPECTAI_NGINX_CONF="./nginx/nginx.conf"
  PUBLIC_SCHEME="https"
  echo "✓ 已检测到 HTTPS 证书，本次启用 nginx HTTPS 配置"
else
  export INSPECTAI_NGINX_CONF="./nginx/nginx.bootstrap.conf"
  PUBLIC_SCHEME="http"
  echo "WARN: 尚未检测到 HTTPS 证书，本次使用 HTTP-only bootstrap 配置" >&2
  echo "      服务启动后按 docs/DEPLOY.md 签发证书，再重跑本脚本切换 HTTPS。" >&2
fi
echo

# ---------- 拉起 ----------

echo "[1/5] 构建 + 启动容器"
$COMPOSE up -d --build

echo
echo "[2/5] 等待 go-backend 健康（最长 120 秒）"
for i in $(seq 1 60); do
  if $COMPOSE exec -T go-backend wget -qO- http://127.0.0.1:8080/health 2>/dev/null | grep -q '"status":"ok"'; then
    echo "  ✓ go-backend healthy"
    break
  fi
  sleep 2
  if [ "$i" -eq 60 ]; then
    echo "ERROR: backend health check 超时（120s）" >&2
    $COMPOSE logs --tail=120 go-backend ai-service mysql
    exit 6
  fi
done

echo
echo "[3/5] 验证 AI 密钥已被容器加载"
ai_health="$($COMPOSE exec -T ai-service curl -fsS http://127.0.0.1:9100/health)"
if ! printf '%s' "$ai_health" | grep -Eq '"hasDashscopeKey"[[:space:]]*:[[:space:]]*true'; then
  echo "ERROR: ai-service 未加载千问密钥。检查 secrets/dashscope_api_key 的内容和权限。" >&2
  exit 7
fi
if ! printf '%s' "$ai_health" | grep -Eq '"hasDeepSeekKey"[[:space:]]*:[[:space:]]*true'; then
  echo "ERROR: ai-service 未加载 DeepSeek 密钥。检查 secrets/deepseek_api_key 的内容和权限。" >&2
  exit 7
fi
echo "  ✓ DashScope vision + DeepSeek management AI ready"

echo
echo "[4/5] 全部服务状态"
$COMPOSE ps

echo
echo "[5/5] 完成"
echo "  移动端：${PUBLIC_SCHEME}://<服务器 IP>/"
echo "  管理端：${PUBLIC_SCHEME}://<服务器 IP>/admin/"
echo "  健康检查：${PUBLIC_SCHEME}://<服务器 IP>/health"
echo
echo "下一步建议："
echo "  - 在云平台安全组开放 80 / 443 端口"
echo "  - 如果当前是 HTTP bootstrap，按 docs/DEPLOY.md 签发证书后重跑本脚本"
echo "  - 安排定期备份：见 docs/DEPLOY.md 第 6 节"
