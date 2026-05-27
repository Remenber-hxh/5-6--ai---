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
if grep -Eq "^DASHSCOPE_API_KEY=sk-|^MYSQL_PASSWORD=[^[:space:]]+|^MYSQL_ROOT_PASSWORD=[^[:space:]]+|^INSPECTAI_ADMIN_PASSWORD=[^[:space:]]+" .env.prod; then
  echo "ERROR: .env.prod 含明文密钥，新版方案禁止。" >&2
  echo "       把所有密钥从 .env.prod 移除，改走 secrets/：" >&2
  echo "         bash scripts/prepare-secrets.sh" >&2
  exit 3
fi

# 检查 secrets 文件存在且非空
SECRETS_DIR="$ROOT_DIR/secrets"
REQUIRED_SECRETS=(
  dashscope_api_key
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
echo

# ---------- 拉起 ----------

echo "[1/4] 构建 + 启动容器"
$COMPOSE up -d --build

echo
echo "[2/4] 等待 go-backend 健康（最长 120 秒）"
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
echo "[3/4] 全部服务状态"
$COMPOSE ps

echo
echo "[4/4] 完成"
echo "  移动端：http://<服务器 IP>/"
echo "  管理端：http://<服务器 IP>/admin/"
echo "  健康检查：http://<服务器 IP>/health"
echo
echo "下一步建议："
echo "  - 在云平台安全组开放 80 端口"
echo "  - 配置 HTTPS（推荐 caddy 或 acme.sh + nginx）"
echo "  - 安排定期备份：见 docs/DEPLOY.md 第 5 节"
