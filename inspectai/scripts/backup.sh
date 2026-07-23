#!/usr/bin/env bash
# InspectAI 自动备份脚本（数据库 + 巡检照片 + 密钥）
#
# 设计要点：
#   - mysqldump 走 --single-transaction，InnoDB 热备不锁表，业务无感，不用停服务
#   - 备份前先查磁盘余量，不够直接中止，绝不写到一半把盘撑爆
#   - flock 独占，定时任务与手工执行撞车时后来者直接退出，不会跑出半份备份
#   - 任何一步失败都推企业微信告警（复用 secrets/wework_bot_webhook）
#   - 密钥单独打包且 chmod 600，不与数据备份混在一个文件里
#
# 用法：
#   bash scripts/backup.sh              # 正常备份
#   bash scripts/backup.sh --dry-run    # 只检查环境和磁盘，不真备份
#
# 可调环境变量：
#   INSPECTAI_BACKUP_DIR   备份根目录（默认 ./backups）
#   BACKUP_KEEP_DAILY      每日备份保留天数（默认 7）
#   BACKUP_KEEP_WEEKLY     周日备份额外保留天数（默认 28）
#   BACKUP_MIN_FREE_MB     要求的磁盘最小余量，MB（默认 2048）
#
# 恢复见 scripts/restore.sh，定时任务安装见 docs/DEPLOY.md 第 7 节。

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BACKUP_DIR="${INSPECTAI_BACKUP_DIR:-$ROOT_DIR/backups}"
KEEP_DAILY="${BACKUP_KEEP_DAILY:-7}"
KEEP_WEEKLY="${BACKUP_KEEP_WEEKLY:-28}"
MIN_FREE_MB="${BACKUP_MIN_FREE_MB:-2048}"
COMPOSE="docker compose --env-file .env.prod -f docker-compose.prod.yml"

DRY_RUN=0
[[ "${1:-}" == "--dry-run" ]] && DRY_RUN=1

STAMP="$(date +%F)"            # 2026-07-22
TIME_STAMP="$(date +%F_%H%M%S)"
DEST="$BACKUP_DIR/$STAMP"
LOG_FILE="$BACKUP_DIR/backup.log"

# ---------- 日志 ----------

mkdir -p "$BACKUP_DIR"

log() { printf '%s  %s\n' "$(date '+%F %T')" "$*" | tee -a "$LOG_FILE"; }
die() { log "ERROR: $*"; exit 1; }

# ---------- 企业微信告警 ----------

# 失败时推群机器人。webhook 缺失不算错误（只是没配），但会在日志里留痕。
notify() {
  local level="$1" text="$2"
  local hook_file="$ROOT_DIR/secrets/wework_bot_webhook"
  [[ -s "$hook_file" ]] || { log "（未配置 wework_bot_webhook，跳过告警推送）"; return 0; }
  local hook payload
  hook="$(tr -d '\r\n' < "$hook_file")"
  payload=$(printf '{"msgtype":"text","content":{"content":"[智巡备份][%s] %s"}}' "$level" "$text")
  curl -fsS -m 10 -H 'Content-Type: application/json' -d "$payload" "$hook" >/dev/null 2>&1 \
    || log "（告警推送失败，不影响备份结果判定）"
}

# 任何非零退出都告警。EXIT_MSG 由各步骤更新，方便定位失败在哪一步。
EXIT_MSG="未知阶段"
on_error() {
  local code=$?
  log "备份失败（阶段：$EXIT_MSG，退出码 $code）"
  notify "失败" "备份失败，阶段：$EXIT_MSG。请登录服务器查看 $LOG_FILE"
  exit "$code"
}
trap on_error ERR

# ---------- 并发保护 ----------

exec 9>"$BACKUP_DIR/.backup.lock"
if ! flock -n 9; then
  log "已有备份在跑，本次跳过"
  exit 0
fi

# ---------- 前置检查 ----------

EXIT_MSG="前置检查"

command -v docker >/dev/null 2>&1 || die "docker 未安装"
[[ -f "$ROOT_DIR/.env.prod" ]] || die "缺少 .env.prod"
[[ -s "$ROOT_DIR/secrets/mysql_root_password" ]] || die "缺少 secrets/mysql_root_password"

# 库名从 .env.prod 读，不硬编码
DB_NAME="$(grep -E '^MYSQL_DATABASE=' "$ROOT_DIR/.env.prod" | head -1 | cut -d= -f2- | tr -d '"'"'"' \r')"
[[ -n "$DB_NAME" ]] || die ".env.prod 里没读到 MYSQL_DATABASE"

$COMPOSE ps mysql --status running --quiet | grep -q . || die "mysql 容器没在跑，先 docker compose up -d"

# 卷名带 compose 项目前缀，项目名可能被改过，按后缀反查更稳
resolve_volume() {
  local suffix="$1" name
  name="$(docker volume ls --format '{{.Name}}' | grep -E "(^|_)${suffix}\$" | head -1)"
  [[ -n "$name" ]] || die "找不到 Docker 卷 *_${suffix}"
  printf '%s' "$name"
}
STORAGE_VOL="$(resolve_volume app_storage)"

log "----- 备份开始 $TIME_STAMP -----"
log "库名=$DB_NAME  照片卷=$STORAGE_VOL  目标=$DEST"

# ---------- 磁盘余量检查 ----------

EXIT_MSG="磁盘余量检查"

# 照片卷实际占用（这是备份体积的大头），拿来估算需求
STORAGE_MB="$(docker run --rm -v "$STORAGE_VOL":/data:ro alpine:3.20 du -sm /data 2>/dev/null | awk '{print $1}')"
STORAGE_MB="${STORAGE_MB:-0}"
FREE_MB="$(df -Pm "$BACKUP_DIR" | awk 'NR==2 {print $4}')"
# 照片压缩比按 1.0 保守估（JPEG 本来就压过了），数据库再留 512MB
NEED_MB=$(( STORAGE_MB + 512 ))
[[ "$NEED_MB" -lt "$MIN_FREE_MB" ]] && NEED_MB="$MIN_FREE_MB"

log "磁盘可用 ${FREE_MB}MB，本次预计需要 ${NEED_MB}MB（照片卷 ${STORAGE_MB}MB）"
if [[ "$FREE_MB" -lt "$NEED_MB" ]]; then
  notify "失败" "磁盘空间不足，未执行备份。可用 ${FREE_MB}MB < 需要 ${NEED_MB}MB"
  die "磁盘空间不足，已中止（宁可不备份，也不能把盘写满导致服务挂掉）"
fi

if [[ "$DRY_RUN" == "1" ]]; then
  log "--dry-run：环境与磁盘检查通过，未执行实际备份"
  exit 0
fi

mkdir -p "$DEST"

# ---------- 1/3 数据库 ----------

EXIT_MSG="数据库导出"

# 不加 --databases：导出的 SQL 不含 CREATE DATABASE/USE，
# 恢复时可以灌进任意库名 —— restore.sh 默认恢复到临时库就靠这个。
# 密码走容器内 MYSQL_PWD，不出现在 host 或容器的进程列表里。
$COMPOSE exec -T mysql sh -c "
  MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) \
  exec mysqldump -u root --single-transaction --routines --triggers \
    --default-character-set=utf8mb4 '$DB_NAME'
" | gzip -9 > "$DEST/db.sql.gz"

DB_SIZE="$(du -h "$DEST/db.sql.gz" | cut -f1)"
# 空转出来的 gz 也有几十字节，这里卡一个下限防"备份成功但其实是空的"
[[ "$(stat -c %s "$DEST/db.sql.gz")" -gt 1024 ]] || die "数据库导出结果异常偏小，疑似导出失败"
log "1/3 数据库导出完成（$DB_SIZE）"

# ---------- 2/3 巡检照片 ----------

EXIT_MSG="照片卷打包"

docker run --rm \
  -v "$STORAGE_VOL":/data:ro \
  -v "$DEST":/backup \
  alpine:3.20 \
  tar czf /backup/storage.tgz -C /data .

log "2/3 照片打包完成（$(du -h "$DEST/storage.tgz" | cut -f1)）"

# ---------- 3/3 密钥 ----------

EXIT_MSG="密钥打包"

# 单独一个包 + 600 权限：数据备份可能被拷来拷去，密钥不能跟着走
tar czf "$DEST/secrets.tgz" -C "$ROOT_DIR" secrets
chmod 600 "$DEST/secrets.tgz"
log "3/3 密钥打包完成"

# ---------- 校验和 ----------

EXIT_MSG="生成校验和"

( cd "$DEST" && sha256sum db.sql.gz storage.tgz secrets.tgz > SHA256SUMS )
log "校验和已生成（恢复前会先核对，防止取到损坏的备份）"

# ---------- 过期清理 ----------

EXIT_MSG="清理过期备份"

# 每日备份保留 KEEP_DAILY 天；周日那份额外保留到 KEEP_WEEKLY 天
now_epoch="$(date +%s)"
cleaned=0
for dir in "$BACKUP_DIR"/20*-*-*; do
  [[ -d "$dir" ]] || continue
  d="$(basename "$dir")"
  dir_epoch="$(date -d "$d" +%s 2>/dev/null)" || continue
  age_days=$(( (now_epoch - dir_epoch) / 86400 ))
  dow="$(date -d "$d" +%u)"          # 7 = 周日
  keep="$KEEP_DAILY"
  [[ "$dow" == "7" ]] && keep="$KEEP_WEEKLY"
  if [[ "$age_days" -gt "$keep" ]]; then
    rm -rf "$dir"
    log "清理过期备份 $d（$age_days 天前）"
    cleaned=$(( cleaned + 1 ))
  fi
done
[[ "$cleaned" == "0" ]] && log "无过期备份需要清理"

# ---------- 收尾 ----------

trap - ERR
TOTAL="$(du -sh "$DEST" | cut -f1)"
FREE_AFTER="$(df -Pm "$BACKUP_DIR" | awk 'NR==2 {print $4}')"
log "备份完成：$DEST（合计 $TOTAL，磁盘剩余 ${FREE_AFTER}MB）"
log "----- 备份结束 -----"
