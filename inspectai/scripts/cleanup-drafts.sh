#!/usr/bin/env bash
# InspectAI 清理:未提交草稿 + 孤儿照片
#
# 清什么(只清这两类):
#   A 未提交的巡检草稿(records.submitted=0)及其认领的照片、记录目录
#   B 孤儿照片:offline_shots 里 record_id 非空、但那条记录已经不在了
#
# 【绝不动待处理照片】record_id='' 的是现场拍了还没成单的东西,
# 删了等于让人白跑一趟。这一类在任何模式下都不会被选中。
#
# 三段式,每一段都要人明确往下走:
#   1. bash scripts/cleanup-drafts.sh inventory        只读盘点,不改任何东西
#   2. bash scripts/cleanup-drafts.sh plan             导出待删的精确 id 清单
#   3. bash scripts/cleanup-drafts.sh apply --confirm  按清单删,删前强制备份
#
# 【为什么要分三段】删除不可逆。盘点让你先看见"要删多少、多久以前的";
# 清单让你能逐条核对、甚至手工划掉几行;apply 只认清单里的 id ——
# 从头到尾没有一处模糊匹配,不会因为条件写宽了多删。
#
# 可调:
#   BEFORE=2026-08-01   只处理这个日期【之前】创建的草稿(默认:不限,全清)
#   OUT_DIR=...         清单输出目录(默认 ./cleanup-YYYY-MM-DD)

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

COMPOSE="docker compose --env-file .env.prod -f docker-compose.prod.yml"
MODE="${1:-}"
CONFIRM=0
[[ "${2:-}" == "--confirm" ]] && CONFIRM=1

BEFORE="${BEFORE:-}"
OUT_DIR="${OUT_DIR:-$ROOT_DIR/cleanup-$(date +%F)}"

die() { echo "错误: $*" >&2; exit 1; }

usage() {
  cat >&2 <<'USAGE'
用法:
  bash scripts/cleanup-drafts.sh inventory        只读盘点
  bash scripts/cleanup-drafts.sh plan             导出待删 id 清单
  bash scripts/cleanup-drafts.sh apply --confirm  按清单删除(先自动备份)

可选:BEFORE=2026-08-01 只处理该日期之前的草稿
USAGE
  exit 2
}

case "$MODE" in
  inventory | plan | apply) ;;
  *) usage ;;
esac

[[ -f .env.prod ]] || die "当前目录不是部署目录(没有 .env.prod)"
DB_NAME="$(grep -E '^MYSQL_DATABASE=' .env.prod | head -1 | cut -d= -f2- | tr -d "\"' \r")"
[[ -n "$DB_NAME" ]] || die ".env.prod 里没读到 MYSQL_DATABASE"
$COMPOSE ps mysql --status running --quiet | grep -q . || die "mysql 容器没在跑"

# 在容器里跑 SQL。密码走 MYSQL_PWD,不出现在进程列表里。
sql() {
  $COMPOSE exec -T mysql sh -c \
    "MYSQL_PWD=\$(cat /run/secrets/mysql_root_password) exec mysql -u root -N -B '$DB_NAME'" \
    <<< "$1"
}

# 草稿的时间条件。空 = 不限。
# 【拼进 SQL 的只有这个日期,而且先校验格式】其余一切都走 id 清单。
COND=""
COND_R=""   # 同一条件,但字段带 r. 前缀(用在 join 里)
if [[ -n "$BEFORE" ]]; then
  [[ "$BEFORE" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}$ ]] || die "BEFORE 格式应为 YYYY-MM-DD,实际 $BEFORE"
  COND="AND created_at < '$BEFORE'"
  COND_R="AND r.created_at < '$BEFORE'"
fi

# ---------------------------------------------------------------- inventory
if [[ "$MODE" == "inventory" ]]; then
  echo "=== 只读盘点(不改任何东西)==="
  echo "库:$DB_NAME   草稿时间条件:${BEFORE:-不限}"
  echo
  echo "--- A 未提交草稿 ---"
  sql "SELECT CONCAT('  条数 ', COUNT(*), ' 条,最早 ', IFNULL(MIN(created_at),'-'),
              ',最新 ', IFNULL(MAX(created_at),'-'))
       FROM records WHERE submitted=0 $COND;"
  echo
  echo "  按月分布(看看最近的会不会误删):"
  sql "SELECT CONCAT('    ', LEFT(created_at,7), '  ', COUNT(*), ' 条')
       FROM records WHERE submitted=0 $COND
       GROUP BY LEFT(created_at,7) ORDER BY 1;"
  echo
  sql "SELECT CONCAT('  这些草稿认领的照片:', COUNT(*), ' 张')
       FROM offline_shots s JOIN records r ON s.record_id=r.id
       WHERE r.submitted=0 $COND_R;"
  echo
  echo "--- B 孤儿照片(挂着的记录已经不在了)---"
  sql "SELECT CONCAT('  ', COUNT(*), ' 张')
       FROM offline_shots s LEFT JOIN records r ON s.record_id=r.id
       WHERE s.record_id<>'' AND r.id IS NULL;"
  echo
  echo "--- C 待处理照片(【不会被删】,仅供对照)---"
  sql "SELECT CONCAT('  ', COUNT(*), ' 张 —— 拍了还没成单的,脚本不碰')
       FROM offline_shots WHERE record_id='';"
  echo
  echo "照片卷占用:"
  VOL="$(docker volume ls --format '{{.Name}}' | grep -E '(^|_)app_storage$' | head -1 || true)"
  if [[ -n "$VOL" ]]; then
    docker run --rm -v "$VOL":/data:ro alpine:3.20 du -sh /data
  else
    echo "  (找不到 *_app_storage 卷)"
  fi
  echo
  echo "看完数字没问题,下一步:bash scripts/cleanup-drafts.sh plan"
  exit 0
fi

# --------------------------------------------------------------------- plan
if [[ "$MODE" == "plan" ]]; then
  mkdir -p "$OUT_DIR"
  echo "=== 导出待删清单到 $OUT_DIR ==="

  # 两列输出即可 —— mysql -B 本来就用 tab 分列,不必自己 CONCAT 拼分隔符
  sql "SELECT id FROM records WHERE submitted=0 $COND ORDER BY created_at;" \
    > "$OUT_DIR/draft_ids.txt"

  sql "SELECT s.id, IFNULL(s.image_path,'')
       FROM offline_shots s JOIN records r ON s.record_id=r.id
       WHERE r.submitted=0 $COND_R ORDER BY s.id;" \
    > "$OUT_DIR/draft_shots.tsv"

  sql "SELECT s.id, IFNULL(s.image_path,'')
       FROM offline_shots s LEFT JOIN records r ON s.record_id=r.id
       WHERE s.record_id<>'' AND r.id IS NULL ORDER BY s.id;" \
    > "$OUT_DIR/orphan_shots.tsv"

  echo "  草稿:      $(wc -l < "$OUT_DIR/draft_ids.txt") 条   -> draft_ids.txt"
  echo "  草稿照片:  $(wc -l < "$OUT_DIR/draft_shots.tsv") 张   -> draft_shots.tsv"
  echo "  孤儿照片:  $(wc -l < "$OUT_DIR/orphan_shots.tsv") 张   -> orphan_shots.tsv"
  echo
  echo "【现在可以打开这三个文件逐条核对】不想删的行直接删掉即可 ——"
  echo "apply 只认这些文件里的 id,不会自己再去查一遍。"
  echo
  echo "确认无误后:bash scripts/cleanup-drafts.sh apply --confirm"
  exit 0
fi

# -------------------------------------------------------------------- apply
[[ $CONFIRM -eq 1 ]] || die "apply 必须显式加 --confirm。这一步不可逆。"
[[ -f "$OUT_DIR/draft_ids.txt" ]] || die "找不到 $OUT_DIR/draft_ids.txt,先跑 plan"
[[ -f "$OUT_DIR/draft_shots.tsv" ]] || die "找不到 $OUT_DIR/draft_shots.tsv,先跑 plan"
[[ -f "$OUT_DIR/orphan_shots.tsv" ]] || die "找不到 $OUT_DIR/orphan_shots.tsv,先跑 plan"

echo "=== 删除前强制备份 ==="
bash scripts/backup.sh || die "备份失败,已中止 —— 没有备份就不删"

DRAFTS=$(wc -l < "$OUT_DIR/draft_ids.txt")
SHOTS=$(( $(wc -l < "$OUT_DIR/draft_shots.tsv") + $(wc -l < "$OUT_DIR/orphan_shots.tsv") ))
echo
echo "即将删除:草稿 $DRAFTS 条,照片 $SHOTS 张。10 秒内 Ctrl-C 可中止。"
sleep 10

# 按清单里的 id 删。
#
# 【每个 id 都要过一遍格式校验】清单是文本文件,人可以手改。校验不做的话,
# 一行被改坏(或粘错)的内容会原样拼进 DELETE 语句 —— 这是删除操作,
# 拼出个 `' OR '1'='1` 就是整表没了。
#
# 【按条数分批,不能按字节切】早先想用 fold 截断长串,那会从某个 id 中间
# 断开,拼出来的是废 SQL。这里纯 bash 计数,500 个一批。
del_by_ids() {
  local table="$1" col="$2" file="$3"
  local batch="" n=0 c=0 bad=0 id
  if [[ ! -s "$file" ]]; then
    echo "  $table: 清单为空,跳过"
    return
  fi
  while IFS= read -r id; do
    [[ -n "$id" ]] || continue
    if [[ ! "$id" =~ ^[A-Za-z0-9_.:-]+$ ]]; then
      echo "  跳过格式异常的 id: $id" >&2
      bad=$((bad + 1))
      continue
    fi
    batch+="${batch:+,}'$id'"
    c=$((c + 1))
    if (( c % 500 == 0 )); then
      sql "DELETE FROM $table WHERE $col IN ($batch);"
      batch=""
      n=$((n + 1))
    fi
  done < <(cut -f1 "$file")
  if [[ -n "$batch" ]]; then
    sql "DELETE FROM $table WHERE $col IN ($batch);"
    n=$((n + 1))
  fi
  echo "  $table: 删了 $c 个 id($n 批)$( ((bad > 0)) && echo ",跳过异常 $bad 个")"
}

echo "--- 删照片行 ---"
cat "$OUT_DIR/draft_shots.tsv" "$OUT_DIR/orphan_shots.tsv" > "$OUT_DIR/_all_shots.tsv"
del_by_ids offline_shots id "$OUT_DIR/_all_shots.tsv"

echo "--- 删草稿及其关联行 ---"
# 【先删关联行再删主行】反过来的话,中途失败会留下一堆指向不存在记录的
# ai_tasks —— 那正是这次要清的"孤儿"。
del_by_ids ai_tasks record_id "$OUT_DIR/draft_ids.txt"
del_by_ids submission_idempotency record_id "$OUT_DIR/draft_ids.txt"
del_by_ids records id "$OUT_DIR/draft_ids.txt"

echo "--- 删磁盘文件 ---"
VOL="$(docker volume ls --format '{{.Name}}' | grep -E '(^|_)app_storage$' | head -1 || true)"
[[ -n "$VOL" ]] || die "找不到照片卷,数据库已清但文件没删 —— 需要手工处理"

# 待删文件 = 离线照片原件 + 每条草稿的记录目录(照片副本)
cut -f2 "$OUT_DIR/_all_shots.tsv" | grep -v '^$' > "$OUT_DIR/_files.txt" || true
sed 's#^#/data/uploads/#' "$OUT_DIR/draft_ids.txt" >> "$OUT_DIR/_files.txt"

# 【路径校验在容器里做】只删 /data(storage 挂载点)子树内的。
# image_path 是库里的值,不校验的话一条脏数据就能让 rm 跑到别处去 ——
# 而这是删除,错了没有回头路。
#
# 脚本走 sh -c,文件清单走 stdin。写成 `sh -s <<'EOF' < 文件` 是不行的:
# 后面的重定向会顶掉 heredoc,容器里执行的就成了那份文件本身。
docker run --rm -i -v "$VOL":/data alpine:3.20 sh -c '
  n=0; skip=0
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    case "$p" in
      /data/*) ;;                                 # 已经是容器内路径
      */storage/*) p="/data/${p#*/storage/}" ;;   # host 绝对路径换算过来
      *) echo "  跳过(不在 storage 子树内): $p"; skip=$((skip+1)); continue ;;
    esac
    case "$p" in
      *..*) echo "  跳过(路径含 ..): $p"; skip=$((skip+1)); continue ;;
    esac
    rm -rf -- "$p" 2>/dev/null && n=$((n+1))
  done
  echo "  已删 $n 个文件/目录,跳过 $skip 个"
' < "$OUT_DIR/_files.txt"

echo
echo "=== 完成 ==="
echo "清单留在 $OUT_DIR,备份在 ./backups/ —— 都别急着删,确认几天没问题再说。"
