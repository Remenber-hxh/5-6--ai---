#!/usr/bin/env bash
# InspectAI 备份恢复脚本
#
# 设计原则：备份脚本谁都能跑，恢复脚本必须难跑。
#   - 默认恢复到「临时库」，不碰生产库。用来做恢复演练 / 核对数据，随时可跑。
#   - 覆盖生产库必须显式 --force-production，并手工输入确认短语，没有第二条路。
#   - 照片默认解到指定目录供人工核对，不直接覆盖线上卷。
#   - 动手前先校验 SHA256，取到损坏的备份宁可失败也不能灌进库里。
#
# 用法：
#   bash scripts/restore.sh --list                       # 列出可用备份
#   bash scripts/restore.sh 2026-07-22                   # 恢复演练（→ 临时库）
#   bash scripts/restore.sh 2026-07-22 --storage-to /tmp/photos   # 顺带解出照片
#   bash scripts/restore.sh 2026-07-22 --force-production         # 覆盖生产库（危险）
#
# 恢复演练是备份唯一的验收方式 —— 没恢复成功过的备份不算备份。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BACKUP_DIR="${INSPECTAI_BACKUP_DIR:-$ROOT_DIR/backups}"
COMPOSE="docker compose --env-file .env.prod -f docker-compose.prod.yml"
TEMP_DB="${INSPECTAI_RESTORE_DB:-inspectai_restore_check}"

STAMP=""
FORCE_PROD=0
STORAGE_TO=""

die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

while [[ $# -gt 0 ]]; do
  case "$1" in
    --list)
      echo "可用备份："
      for d in "$BACKUP_DIR"/20*-*-*; do
        [[ -d "$d" ]] || continue
        printf '  %-12s  %s\n' "$(basename "$d")" "$(du -sh "$d" | cut -f1)"
      done
      exit 0 ;;
    --force-production) FORCE_PROD=1; shift ;;
    --storage-to) STORAGE_TO="${2:-}"; shift 2 ;;
    -*) die "未知参数：$1" ;;
    *) STAMP="$1"; shift ;;
  esac
done

[[ -n "$STAMP" ]] || die "缺少备份日期。先看有哪些：bash scripts/restore.sh --list"
SRC="$BACKUP_DIR/$STAMP"
[[ -d "$SRC" ]] || die "备份不存在：$SRC"

# ---------- 校验完整性 ----------

echo "==> 校验备份完整性 $STAMP"
[[ -f "$SRC/SHA256SUMS" ]] || die "缺少 SHA256SUMS，无法确认备份是否完整"
( cd "$SRC" && sha256sum -c SHA256SUMS ) || die "校验失败，这份备份已损坏，不要使用"

# ---------- 确定目标库 ----------

DB_NAME="$(grep -E '^MYSQL_DATABASE=' "$ROOT_DIR/.env.prod" | head -1 | cut -d= -f2- | tr -d '"'"'"' \r')"
[[ -n "$DB_NAME" ]] || die ".env.prod 里没读到 MYSQL_DATABASE"

if [[ "$FORCE_PROD" == "1" ]]; then
  TARGET_DB="$DB_NAME"
  cat <<WARN

  ╔══════════════════════════════════════════════════════╗
  ║  警告：即将覆盖生产库 $TARGET_DB
  ║  当前库里的全部数据会被 $STAMP 的备份替换，不可撤销。
  ║  该日之后产生的巡检记录、审批、整改状态将全部丢失。
  ╚══════════════════════════════════════════════════════╝

WARN
  printf '确认请完整输入：覆盖生产库\n> '
  read -r answer
  [[ "$answer" == "覆盖生产库" ]] || die "确认短语不匹配，已中止（什么都没改）"
else
  TARGET_DB="$TEMP_DB"
  echo "==> 恢复演练模式：目标库 $TARGET_DB（生产库 $DB_NAME 不受影响）"
fi

$COMPOSE ps mysql --status running --quiet | grep -q . || die "mysql 容器没在跑"

# ---------- 恢复数据库 ----------

echo "==> 建库 $TARGET_DB"
$COMPOSE exec -T mysql sh -c "
  MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) \
  exec mysql -u root -e \"
    DROP DATABASE IF EXISTS \\\`$TARGET_DB\\\`;
    CREATE DATABASE \\\`$TARGET_DB\\\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\"
"

echo "==> 导入数据（大库需要几分钟，别中断）"
gunzip -c "$SRC/db.sql.gz" | $COMPOSE exec -T mysql sh -c "
  MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) \
  exec mysql -u root --default-character-set=utf8mb4 '$TARGET_DB'
"

# ---------- 核对 ----------

echo
echo "==> 恢复结果核对（$TARGET_DB）"
$COMPOSE exec -T mysql sh -c "
  MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) \
  exec mysql -u root -N -B '$TARGET_DB' -e \"
    SELECT '巡检记录', COUNT(*) FROM records
    UNION ALL SELECT '资产台账', COUNT(*) FROM assets
    UNION ALL SELECT '用户',     COUNT(*) FROM users
    UNION ALL SELECT '工程任务', COUNT(*) FROM engineering_tasks
    UNION ALL SELECT '审批申请', COUNT(*) FROM change_requests
    UNION ALL SELECT '操作日志', COUNT(*) FROM operation_logs;\"
" | awk -F'\t' '{printf "    %-10s %s 条\n", $1, $2}'

# ---------- 照片 ----------

if [[ -n "$STORAGE_TO" ]]; then
  echo
  echo "==> 解出巡检照片到 $STORAGE_TO"
  mkdir -p "$STORAGE_TO"
  tar xzf "$SRC/storage.tgz" -C "$STORAGE_TO"
  echo "    共 $(find "$STORAGE_TO" -type f | wc -l) 个文件"
fi

# ---------- 收尾 ----------

echo
if [[ "$FORCE_PROD" == "1" ]]; then
  echo "生产库已恢复到 $STAMP。接下来重启后端让连接池重建："
  echo "  $COMPOSE restart go-backend"
else
  cat <<TIP
恢复演练完成。上面的条数与线上现状对得上，就说明这份备份是可用的。

演练库用完记得删掉，别占着磁盘：
  $COMPOSE exec -T mysql sh -c 'MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) mysql -u root -e "DROP DATABASE \\\`$TARGET_DB\\\`;"'

真要覆盖生产库，加 --force-production（会再确认一次）。
TIP
fi
