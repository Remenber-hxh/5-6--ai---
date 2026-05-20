"""
SQLite -> MySQL 一键迁移
========================

用法：
    1. 在 .env 里填好 MYSQL_DSN（或者用环境变量 MYSQL_DSN）
       格式：user:password@host:port/database
       例：  inspectai:你的密码@127.0.0.1:3306/inspectai

    2. 确保目标数据库已创建（脚本不创建 database，避免要 root 权限）：
       mysql> CREATE DATABASE inspectai DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;

    3. 跑脚本：
       cd inspectai
       python scripts/migrate-to-mysql.py
       # 或者：MYSQL_DSN='root:pwd@127.0.0.1:3306/inspectai' python scripts/migrate-to-mysql.py

行为：
    - 应用 go-backend/cmd/server/schema_mysql.sql 建表（IF NOT EXISTS）
    - 从 storage/inspectai.db 读 records / assets / ai_tasks
    - INSERT 到 MySQL（已有同 ID 的行 -> REPLACE 覆盖）
    - 完成后打印各表行数对比

注意：
    - 不删除 SQLite 数据
    - 不改 .env，需要你手动加 DB_DRIVER=mysql 后重启 backend
"""
import os
import re
import sys
import sqlite3
import pymysql
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
SQLITE_PATH = ROOT / "storage" / "inspectai.db"
SCHEMA_MYSQL = ROOT / "go-backend" / "cmd" / "server" / "schema_mysql.sql"
ENV_PATH = ROOT / ".env"


def load_dsn() -> str:
    dsn = os.environ.get("MYSQL_DSN", "").strip()
    if dsn:
        return dsn
    if ENV_PATH.exists():
        for line in ENV_PATH.read_text(encoding="utf-8").splitlines():
            line = line.strip()
            if line.startswith("MYSQL_DSN="):
                return line.split("=", 1)[1].strip().strip('"').strip("'")
    raise SystemExit(
        "MYSQL_DSN 未配置。请在 .env 里写：\n"
        "  MYSQL_DSN=root:你的密码@127.0.0.1:3306/inspectai\n"
        "或者命令行：MYSQL_DSN='...' python scripts/migrate-to-mysql.py"
    )


def parse_dsn(dsn: str) -> dict:
    """兼容两种 DSN：
      Go 风格: user:pwd@tcp(host:port)/db?args
      简化:    user:pwd@host:port/db
    """
    # 先剥掉 tcp(...) 和 ?args
    cleaned = re.sub(r"@tcp\(([^)]+)\)", r"@\1", dsn)
    cleaned = cleaned.split("?", 1)[0]
    m = re.match(r"^([^:]+):([^@]*)@([^:/]+)(?::(\d+))?/(.+)$", cleaned)
    if not m:
        raise SystemExit(
            f"DSN 解析失败：{dsn}\n"
            "支持格式：\n"
            "  Go 风格   user:pwd@tcp(host:port)/db?args\n"
            "  简化      user:pwd@host:port/db"
        )
    return {
        "user": m.group(1),
        "password": m.group(2),
        "host": m.group(3),
        "port": int(m.group(4) or 3306),
        "database": m.group(5),
    }


def split_sql(sql_text: str) -> list[str]:
    """按 ; 切分，跳过 -- 注释行"""
    cleaned = "\n".join(
        line for line in sql_text.splitlines()
        if not line.strip().startswith("--")
    )
    return [s.strip() for s in cleaned.split(";") if s.strip()]


def main():
    if not SQLITE_PATH.exists():
        raise SystemExit(f"SQLite 文件不存在：{SQLITE_PATH}")
    if not SCHEMA_MYSQL.exists():
        raise SystemExit(f"MySQL schema 不存在：{SCHEMA_MYSQL}")

    dsn = load_dsn()
    cfg = parse_dsn(dsn)
    print(f"[1/5] 连接 MySQL: {cfg['user']}@{cfg['host']}:{cfg['port']}/{cfg['database']}")
    try:
        conn = pymysql.connect(
            host=cfg["host"], port=cfg["port"],
            user=cfg["user"], password=cfg["password"],
            database=cfg["database"],
            charset="utf8mb4",
            autocommit=False,
        )
    except pymysql.MySQLError as e:
        raise SystemExit(
            f"连接失败：{e}\n"
            f"请确认：\n"
            f"  1. MySQL 已启动（services.msc 看 MySQL80 / mysql 服务）\n"
            f"  2. 数据库 {cfg['database']} 已创建：\n"
            f"     CREATE DATABASE {cfg['database']} DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;\n"
            f"  3. 用户 {cfg['user']} 有 {cfg['database']} 库的权限"
        )

    try:
        with conn.cursor() as cur:
            print(f"[2/5] 应用 schema_mysql.sql 建表（IF NOT EXISTS）")
            for stmt in split_sql(SCHEMA_MYSQL.read_text(encoding="utf-8")):
                cur.execute(stmt)
            conn.commit()

            print(f"[3/5] 读取 SQLite: {SQLITE_PATH}")
            sq = sqlite3.connect(str(SQLITE_PATH))
            sq.row_factory = sqlite3.Row

            stats = {}
            for table in ("records", "assets", "ai_tasks"):
                print(f"[4/5] 迁移表 {table} ...", end=" ", flush=True)
                rows = sq.execute(f"SELECT * FROM {table}").fetchall()
                if not rows:
                    print("0 行，跳过")
                    stats[table] = (0, 0)
                    continue
                cols = rows[0].keys()
                placeholders = ",".join(["%s"] * len(cols))
                col_list = ",".join(f"`{c}`" for c in cols)
                # REPLACE INTO：已有同 PK 的行覆盖（迁移幂等）
                sql = f"REPLACE INTO `{table}` ({col_list}) VALUES ({placeholders})"
                values = [tuple(r[c] for c in cols) for r in rows]
                cur.executemany(sql, values)
                # 验证
                cur.execute(f"SELECT COUNT(*) FROM `{table}`")
                got = cur.fetchone()[0]
                stats[table] = (len(rows), got)
                print(f"SQLite {len(rows)} 行 -> MySQL 共 {got} 行")
            conn.commit()
            sq.close()

        print(f"[5/5] 完成。摘要：")
        for table, (src, dst) in stats.items():
            print(f"  {table:12} SQLite={src} -> MySQL={dst}")

        print("")
        print("下一步：")
        print("  1. 编辑 .env，加（或改）这两行：")
        print("       DB_DRIVER=mysql")
        print("       MYSQL_DSN=<按 .env/.env.secure 中实际值配置，不要在日志里明文粘贴>")
        print("  2. 重启 backend：scripts\\start-local.ps1")
        print("  3. 验证：curl --noproxy '*' http://127.0.0.1:18080/health")
        print("       storeKind 应为 \"mysql\"")
    finally:
        conn.close()


if __name__ == "__main__":
    main()
