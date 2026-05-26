#!/usr/bin/env bash
# 在 Linux 生产机上准备 docker secret 文件。
# 用法：先 export 环境变量，再跑此脚本。
#
#   export DASHSCOPE_API_KEY=sk-xxx
#   export MYSQL_PASSWORD=...
#   export MYSQL_ROOT_PASSWORD=...
#   export MYSQL_USER=inspectai
#   export MYSQL_DATABASE=inspectai
#   export INSPECTAI_AUTH_TOKEN=...        # 留空则脚本随机生成
#   export INSPECTAI_SUPERVISOR_TOKEN=...
#   export INSPECTAI_ADMIN_PASSWORD=...
#   ./scripts/prepare-secrets.sh
#
# Why: 把敏感值从 shell history / env list 收口到磁盘上 0600 的文件，
# 避免被 `docker inspect` / `ps -e` 看到。

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
SECRETS_DIR="$ROOT_DIR/secrets"

mkdir -p "$SECRETS_DIR"
chmod 700 "$SECRETS_DIR"

write_secret() {
  local name="$1"
  local value="$2"
  local path="$SECRETS_DIR/$name"
  if [[ -z "$value" ]]; then
    echo "WARN: $name 为空，跳过写入" >&2
    return
  fi
  printf '%s' "$value" > "$path"
  chmod 600 "$path"
  echo "✔ wrote $path ($(stat -c '%a %U' "$path"))"
}

# 必填 5 个
write_secret dashscope_api_key        "${DASHSCOPE_API_KEY:-}"
write_secret mysql_password           "${MYSQL_PASSWORD:-}"
write_secret mysql_root_password      "${MYSQL_ROOT_PASSWORD:-}"
write_secret inspectai_admin_password "${INSPECTAI_ADMIN_PASSWORD:-}"

# 自动生成 token（若未设）
gen_token() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -base64 32 | tr -d '=' | tr '+/' '-_'
  else
    head -c 32 /dev/urandom | base64 | tr -d '=' | tr '+/' '-_'
  fi
}
write_secret inspectai_auth_token       "${INSPECTAI_AUTH_TOKEN:-$(gen_token)}"
write_secret inspectai_supervisor_token "${INSPECTAI_SUPERVISOR_TOKEN:-$(gen_token)}"

# 组装 MYSQL_DSN —— 校验 password 不含会破坏 DSN 解析的特殊字符
MYSQL_USER="${MYSQL_USER:-inspectai}"
MYSQL_DATABASE="${MYSQL_DATABASE:-inspectai}"
if [[ -z "${MYSQL_PASSWORD:-}" ]]; then
  echo "ERROR: MYSQL_PASSWORD 未设置，跳过 mysql_dsn" >&2
elif [[ "$MYSQL_PASSWORD" =~ [@:/?\#\&] ]]; then
  echo "ERROR: MYSQL_PASSWORD 含 @:/?#& 之一，go-sql-driver DSN 解析会失败。" >&2
  echo "       请换成纯字母+数字的密码，或自行 URL-encode 后写入 secrets/mysql_dsn。" >&2
  exit 1
else
  DSN="${MYSQL_USER}:${MYSQL_PASSWORD}@tcp(mysql:3306)/${MYSQL_DATABASE}?charset=utf8mb4&parseTime=false&loc=Local"
  write_secret mysql_dsn "$DSN"
fi

echo
echo "Secrets prepared in: $SECRETS_DIR"
echo "请确认权限为 0600 / 目录 0700："
ls -la "$SECRETS_DIR"
