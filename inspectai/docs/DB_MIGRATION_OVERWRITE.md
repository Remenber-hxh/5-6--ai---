# 数据库迁移覆盖方案

> 目标：把本地 MySQL 数据库迁移到云服务器，并覆盖服务器现有生产库。  
> 风险级别：高。执行前必须确认服务器地址、数据库名和备份路径。

## 1. 迁移原则

- 本地只负责导出，不直接操作生产库。
- 服务器先备份，再覆盖。
- 覆盖采用 `DROP DATABASE + CREATE DATABASE + import`，避免旧表和旧数据残留。
- 只迁移 MySQL 数据；如需要同步现场图片，额外加 `-WithImages`。
- 旧记录不会自动重新生成 AI 总结，只迁移数据库中已有内容。

## 2. 文件说明

| 文件 | 作用 |
| --- | --- |
| `scripts/push-db.ps1` | Windows 本地执行：导出本地 MySQL、上传 SQL、远程调用服务器导入脚本 |
| `scripts/import-db-prod.sh` | Linux 服务器执行：备份生产库、清空重建库、导入 SQL、重启后端 |
| `backups/db/` | 服务器端自动备份目录 |

## 3. 前置条件

本地 Windows：

- 已安装 `mysqldump`。
- 本地能访问 MySQL。
- 本地项目根目录存在 `.env.secure` / `.env` / 环境变量 `MYSQL_DSN` 中任意一种。
- 本机 `ssh` / `scp` 能连接服务器。

服务器 Linux：

- 项目目录默认：`/opt/inspectai-src/inspectai`
- 已部署 `docker-compose.prod.yml`
- 已有 `.env.prod`
- 已有 `secrets/mysql_root_password`
- `docker compose ps` 中 `mysql` 为 healthy / Up

## 4. 推荐执行命令

在本地 PowerShell 进入项目目录：

```powershell
cd "D:\5-6月 ai 大会\inspectai"
.\scripts\push-db.ps1 -Server root@你的服务器IP
```

如果需要同步本地上传图片：

```powershell
.\scripts\push-db.ps1 -Server root@你的服务器IP -WithImages
```

如果本机找不到 `mysqldump`：

```powershell
.\scripts\push-db.ps1 `
  -Server root@你的服务器IP `
  -MySqlDumpPath "D:\mysql\mysql-8.0.42-winx64\bin\mysqldump.exe"
```

如果不想用 `.env.secure`，也可以显式传 DSN：

```powershell
.\scripts\push-db.ps1 `
  -Server root@你的服务器IP `
  -LocalDsn "inspectai:密码@tcp(127.0.0.1:3306)/inspectai?charset=utf8mb4&parseTime=false&loc=Local"
```

无人值守执行才使用 `-Force`：

```powershell
.\scripts\push-db.ps1 -Server root@你的服务器IP -Force
```

## 5. 脚本会做什么

1. 解析本地 `MYSQL_DSN`。
2. 调用 `mysqldump` 导出本地库。
3. 上传 SQL 到服务器项目目录。
4. 上传最新版 `scripts/import-db-prod.sh` 到服务器。
5. 服务器执行生产库备份。
6. 服务器 `DROP DATABASE` 并重建目标库。
7. 导入本地 SQL。
8. 重启 `go-backend`，清理运行态缓存。

## 6. 覆盖后验收

服务器执行：

```bash
cd /opt/inspectai-src/inspectai
docker compose --env-file .env.prod -f docker-compose.prod.yml ps
curl -fsS https://ai-demo.jadeastech.com/health
```

应看到：

```json
{
  "status": "ok",
  "storeKind": "mysql"
}
```

再检查关键数据：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml exec -T mysql \
  sh -lc 'mysql -uroot -p"$(cat /run/secrets/mysql_root_password)" "$MYSQL_DATABASE" -e "SELECT COUNT(*) records FROM records; SELECT COUNT(*) assets FROM assets;"'
```

管理端检查：

- `/admin` 能打开。
- 资产台账数量正确。
- 巡检记录数量正确。
- 随机点开一条记录，图片、AI 总结、字段明细正常。

## 7. 回滚方式

脚本会在服务器生成备份：

```text
/opt/inspectai-src/inspectai/backups/db/inspectai_before_overwrite_YYYYmmdd_HHMMSS.sql.gz
```

回滚命令：

```bash
cd /opt/inspectai-src/inspectai
gzip -dc backups/db/inspectai_before_overwrite_YYYYmmdd_HHMMSS.sql.gz > _rollback.sql
bash scripts/import-db-prod.sh --file _rollback.sql --yes
```

## 8. 注意事项

- 覆盖库前不要在生产环境继续录入新巡检，否则会被本地库覆盖掉。
- 如生产已经产生了新图片而本地没有，使用 `-WithImages` 时要注意会把本地 uploads 合并进服务器 volume，但不会删除服务器已有图片。
- 如果只想同步代码，不要执行数据库覆盖脚本。
- 如果只想追加数据，不适合用本方案；本方案是完整覆盖。
