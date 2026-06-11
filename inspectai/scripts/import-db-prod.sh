#!/usr/bin/env bash
# 覆盖导入 InspectAI 生产 MySQL 数据库。
#
# 典型用法（在服务器项目根目录执行）：
#   bash scripts/import-db-prod.sh --file _migrate_local.sql --yes
#
# 安全策略：
#   - 默认先备份当前生产库到 backups/db/
#   - 默认要求输入 OVERWRITE；CI/自动脚本需显式传 --yes
#   - 导入前 DROP/CREATE 目标库，避免旧表/旧数据残留

set -euo pipefail

COMPOSE_FILE="docker-compose.prod.yml"
ENV_FILE=".env.prod"
IMPORT_FILE=""
DB_NAME=""
BACKUP_DIR="backups/db"
YES=0
NO_BACKUP=0
NO_RESTART_BACKEND=0

usage() {
  cat <<'EOF'
Usage:
  bash scripts/import-db-prod.sh --file dump.sql [options]

Options:
  --file PATH             要导入的 SQL 文件，支持 .sql / .sql.gz
  --compose PATH          docker compose 文件，默认 docker-compose.prod.yml
  --env-file PATH         compose env 文件，默认 .env.prod
  --db-name NAME          目标数据库名；默认读取 .env.prod MYSQL_DATABASE，缺省 inspectai
  --backup-dir PATH       服务器备份目录，默认 backups/db
  --yes                   跳过交互确认
  --no-backup             不备份服务器现有库，不建议
  --no-restart-backend    导入后不重启 go-backend
  -h, --help              显示帮助
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --file) IMPORT_FILE="${2:-}"; shift 2 ;;
    --compose) COMPOSE_FILE="${2:-}"; shift 2 ;;
    --env-file) ENV_FILE="${2:-}"; shift 2 ;;
    --db-name) DB_NAME="${2:-}"; shift 2 ;;
    --backup-dir) BACKUP_DIR="${2:-}"; shift 2 ;;
    --yes) YES=1; shift ;;
    --no-backup) NO_BACKUP=1; shift ;;
    --no-restart-backend) NO_RESTART_BACKEND=1; shift ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown argument: $1" >&2; usage; exit 2 ;;
  esac
done

if [[ -z "$IMPORT_FILE" ]]; then
  echo "ERROR: --file is required." >&2
  usage
  exit 2
fi
if [[ ! -f "$IMPORT_FILE" ]]; then
  echo "ERROR: import file not found: $IMPORT_FILE" >&2
  exit 2
fi
if [[ ! -f "$COMPOSE_FILE" ]]; then
  echo "ERROR: compose file not found: $COMPOSE_FILE" >&2
  exit 2
fi
if [[ ! -f "$ENV_FILE" ]]; then
  echo "ERROR: env file not found: $ENV_FILE" >&2
  exit 2
fi

read_env() {
  local key="$1"
  awk -F= -v k="$key" '
    $1 == k {
      v = substr($0, index($0, "=") + 1)
      gsub(/^[[:space:]]+|[[:space:]]+$/, "", v)
      gsub(/^"|"$/, "", v)
      gsub(/^'\''|'\''$/, "", v)
      print v
      exit
    }
  ' "$ENV_FILE"
}

if [[ -z "$DB_NAME" ]]; then
  DB_NAME="$(read_env MYSQL_DATABASE)"
fi
DB_NAME="${DB_NAME:-inspectai}"

if [[ ! "$DB_NAME" =~ ^[A-Za-z0-9_]+$ ]]; then
  echo "ERROR: invalid db name: $DB_NAME" >&2
  exit 2
fi

COMPOSE=(docker compose --env-file "$ENV_FILE" -f "$COMPOSE_FILE")

echo "Target DB      : $DB_NAME"
echo "Compose        : $COMPOSE_FILE"
echo "Env file       : $ENV_FILE"
echo "Import file    : $IMPORT_FILE"
echo "Backup dir     : $BACKUP_DIR"
echo

if [[ "$YES" -ne 1 ]]; then
  echo "危险操作：即将 DROP/CREATE 并覆盖生产数据库：$DB_NAME"
  read -r -p "继续请输入 OVERWRITE: " CONFIRM
  if [[ "$CONFIRM" != "OVERWRITE" ]]; then
    echo "Canceled."
    exit 1
  fi
fi

echo "[1/5] Check mysql container..."
"${COMPOSE[@]}" ps mysql
"${COMPOSE[@]}" exec -T mysql sh -lc 'mysqladmin ping -h 127.0.0.1 -uroot -p"$(cat /run/secrets/mysql_root_password)" --silent'

mkdir -p "$BACKUP_DIR"
STAMP="$(date +%Y%m%d_%H%M%S)"
BACKUP_FILE="$BACKUP_DIR/${DB_NAME}_before_overwrite_${STAMP}.sql"

if [[ "$NO_BACKUP" -eq 1 ]]; then
  echo "[2/5] Skip server backup."
else
  echo "[2/5] Backup current server DB -> $BACKUP_FILE"
  "${COMPOSE[@]}" exec -T mysql sh -lc \
    'mysqldump -uroot -p"$(cat /run/secrets/mysql_root_password)" --default-character-set=utf8mb4 --single-transaction --routines --triggers --events --hex-blob --no-tablespaces "$1"' \
    sh "$DB_NAME" > "$BACKUP_FILE"
  if command -v gzip >/dev/null 2>&1; then
    gzip -f "$BACKUP_FILE"
    BACKUP_FILE="$BACKUP_FILE.gz"
  fi
  echo "Backup saved   : $BACKUP_FILE"
fi

echo "[3/5] Drop and recreate database..."
"${COMPOSE[@]}" exec -T mysql sh -lc \
  'db="$1"; mysql -uroot -p"$(cat /run/secrets/mysql_root_password)" -e "DROP DATABASE IF EXISTS \`$db\`; CREATE DATABASE \`$db\` DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;"' \
  sh "$DB_NAME"

echo "[4/5] Import local dump..."
case "$IMPORT_FILE" in
  *.gz)
    gzip -dc "$IMPORT_FILE" | "${COMPOSE[@]}" exec -T mysql sh -lc \
      'mysql -uroot -p"$(cat /run/secrets/mysql_root_password)" "$1"' \
      sh "$DB_NAME"
    ;;
  *)
    "${COMPOSE[@]}" exec -T mysql sh -lc \
      'mysql -uroot -p"$(cat /run/secrets/mysql_root_password)" "$1"' \
      sh "$DB_NAME" < "$IMPORT_FILE"
    ;;
esac

rm -f "$IMPORT_FILE"

if [[ "$NO_RESTART_BACKEND" -eq 1 ]]; then
  echo "[5/5] Skip go-backend restart."
else
  echo "[5/5] Restart go-backend to clear runtime state..."
  "${COMPOSE[@]}" restart go-backend
fi

echo
echo "DB overwrite finished."
if [[ "$NO_BACKUP" -ne 1 ]]; then
  echo "Rollback backup: $BACKUP_FILE"
fi
