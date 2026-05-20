"""
Create MySQL database, user, grants, and InspectAI tables.

Typical usage:
    python scripts/init-mysql-db.py --env-file .env.prod

Required env values:
    MYSQL_HOST=127.0.0.1
    MYSQL_PORT=3306
    MYSQL_ADMIN_USER=root
    MYSQL_ADMIN_PASSWORD=...
    MYSQL_DATABASE=inspectai
    MYSQL_USER=inspectai
    MYSQL_PASSWORD=...

Notes:
    - This script is for an existing MySQL server.
    - The Docker Compose production deployment already creates its internal
      MySQL database and user automatically on first start.
"""

from __future__ import annotations

import argparse
import os
import re
import sys
from pathlib import Path

try:
    import pymysql
except ImportError:
    raise SystemExit("Missing dependency: pymysql. Install with: python -m pip install pymysql")


ROOT = Path(__file__).resolve().parent.parent
SCHEMA_MYSQL = ROOT / "go-backend" / "cmd" / "server" / "schema_mysql.sql"


def read_env_file(path: Path) -> dict[str, str]:
    values: dict[str, str] = {}
    if not path.exists():
        return values
    for line in path.read_text(encoding="utf-8").splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        values[key.strip()] = value.strip().strip('"').strip("'")
    return values


def env_value(values: dict[str, str], key: str, default: str = "") -> str:
    return os.environ.get(key) or values.get(key) or default


def split_sql(sql_text: str) -> list[str]:
    cleaned = "\n".join(
        line for line in sql_text.splitlines()
        if not line.strip().startswith("--")
    )
    return [stmt.strip() for stmt in cleaned.split(";") if stmt.strip()]


def quote_ident(name: str) -> str:
    if not re.fullmatch(r"[A-Za-z0-9_]+", name):
        raise SystemExit(f"Invalid MySQL identifier: {name!r}. Use letters, numbers, and underscore only.")
    return f"`{name}`"


def main() -> int:
    parser = argparse.ArgumentParser(description="Create InspectAI MySQL database and tables.")
    parser.add_argument("--env-file", default=".env.prod", help="Env file path, default: .env.prod")
    parser.add_argument("--skip-user", action="store_true", help="Only create database and tables, do not create/grant user.")
    args = parser.parse_args()

    env_path = Path(args.env_file)
    if not env_path.is_absolute():
        env_path = ROOT / env_path
    values = read_env_file(env_path)

    host = env_value(values, "MYSQL_HOST", "127.0.0.1")
    port = int(env_value(values, "MYSQL_PORT", "3306"))
    admin_user = env_value(values, "MYSQL_ADMIN_USER", "root")
    admin_password = env_value(values, "MYSQL_ADMIN_PASSWORD", env_value(values, "MYSQL_ROOT_PASSWORD"))
    database = env_value(values, "MYSQL_DATABASE", "inspectai")
    app_user = env_value(values, "MYSQL_USER", "inspectai")
    app_password = env_value(values, "MYSQL_PASSWORD")

    if not admin_password:
        raise SystemExit("MYSQL_ADMIN_PASSWORD or MYSQL_ROOT_PASSWORD is required.")
    if not app_password and not args.skip_user:
        raise SystemExit("MYSQL_PASSWORD is required unless --skip-user is used.")
    if not SCHEMA_MYSQL.exists():
        raise SystemExit(f"schema_mysql.sql not found: {SCHEMA_MYSQL}")

    print(f"[1/4] Connect MySQL admin: {admin_user}:***@{host}:{port}")
    conn = pymysql.connect(
        host=host,
        port=port,
        user=admin_user,
        password=admin_password,
        charset="utf8mb4",
        autocommit=True,
    )

    db_ident = quote_ident(database)
    try:
        with conn.cursor() as cur:
            print(f"[2/4] Create database if not exists: {database}")
            cur.execute(
                f"CREATE DATABASE IF NOT EXISTS {db_ident} "
                "DEFAULT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"
            )

            if not args.skip_user:
                print(f"[3/4] Create/grant app user: {app_user}@%")
                cur.execute("CREATE USER IF NOT EXISTS %s@'%' IDENTIFIED BY %s", (app_user, app_password))
                cur.execute("ALTER USER %s@'%' IDENTIFIED BY %s", (app_user, app_password))
                cur.execute(f"GRANT ALL PRIVILEGES ON {db_ident}.* TO %s@'%'", (app_user,))
                cur.execute("FLUSH PRIVILEGES")
            else:
                print("[3/4] Skip user creation/grant")

            print("[4/4] Apply schema_mysql.sql")
            cur.execute(f"USE {db_ident}")
            for stmt in split_sql(SCHEMA_MYSQL.read_text(encoding="utf-8")):
                cur.execute(stmt)

            for table in ("records", "assets", "ai_tasks"):
                cur.execute(f"SELECT COUNT(*) FROM `{table}`")
                count = cur.fetchone()[0]
                print(f"  {table}: {count} rows")
    finally:
        conn.close()

    print("")
    print("MySQL init completed.")
    print("For app connection, use:")
    print(f"  DB_DRIVER=mysql")
    print(f"  MYSQL_DSN={app_user}:<MYSQL_PASSWORD>@tcp({host}:{port})/{database}?charset=utf8mb4&parseTime=false&loc=Local")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

