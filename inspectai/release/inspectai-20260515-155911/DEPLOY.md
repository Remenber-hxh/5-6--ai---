# InspectAI 服务器一键部署

## 目标

把当前项目以 Docker Compose 方式部署到一台 Linux 云服务器。部署后包含：

- Nginx：对外 80 端口
- Go 后端：容器内 8080
- Python AI 服务：容器内 9100
- MySQL 8：容器内 3306，数据持久化到 Docker volume
- 上传图片：持久化到 Docker volume

## 1. 本地打包

在 Windows 本地项目目录执行：

```powershell
cd "D:\5-6月 ai 大会\inspectai"
.\scripts\package-release.ps1
```

脚本会生成：

```text
release\inspectai-deploy-YYYYMMDD-HHMMSS.zip
```

压缩包不会包含 `.env`、`.env.secure`、`storage/`、Go 缓存、运行日志、exe 文件。

## 2. 上传服务器

上传 zip 到服务器，例如 `/opt/inspectai`：

```bash
mkdir -p /opt/inspectai
cd /opt/inspectai
unzip inspectai-deploy-*.zip
```

服务器需要先安装 Docker 和 Docker Compose v2。

## 3. 配置生产环境变量

```bash
cp .env.prod.example .env.prod
nano .env.prod
```

必须修改：

```text
MYSQL_PASSWORD=一个强密码
MYSQL_ROOT_PASSWORD=一个强密码
DASHSCOPE_API_KEY=你的千问 DashScope Key
INSPECTAI_AUTH_TOKEN=一个系统访问令牌
```

MySQL 密码建议先用大小写字母和数字组合，避免 `@`、`:`、`/`、`?`、`#` 这类 DSN 特殊字符；如果必须使用特殊字符，需要 URL 编码后再写入。

不要把 `.env.prod` 提交到仓库或发给别人。

`INSPECTAI_AUTH_TOKEN` 是当前版本的轻量访问控制。生产环境设置后，`/api/` 和 `/storage/` 会要求令牌；前端首次打开页面时会提示输入，输入后保存在当前浏览器本地。`/health` 保持可访问，便于服务器健康检查。

## 4. MySQL 建库说明

默认部署方式已经包含 MySQL 容器，第一次执行 `bash scripts/deploy-linux.sh` 时会自动创建：

```text
database: inspectai
user:     inspectai
tables:   records / assets / ai_tasks
```

也就是说，使用内置 Docker MySQL 时，不需要单独手工建库。

如果服务器已经有 MySQL，不想用 Docker 内置 MySQL，可以先执行：

```bash
python3 -m pip install pymysql
python3 scripts/init-mysql-db.py --env-file .env.prod
```

这个脚本会读取 `.env.prod`，自动创建数据库、业务用户、授权并执行 `schema_mysql.sql` 建表。

## 5. 一键启动

```bash
bash scripts/deploy-linux.sh
```

成功后访问：

```text
http://服务器IP/
http://服务器IP/health
```

健康检查中 `storeKind` 应该是 `mysql`。

第一次进入业务页面时，按提示输入 `.env.prod` 中配置的 `INSPECTAI_AUTH_TOKEN`。如果后续更换令牌，需要清理浏览器本地存储后重新输入。

## 6. 常用命令

查看服务状态：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml ps
```

查看日志：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml logs -f --tail=200
```

重启：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml restart
```

更新代码后重新构建：

```bash
bash scripts/deploy-linux.sh
```

停止服务但保留数据：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml down
```

删除服务和数据库数据：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml down -v
```

谨慎使用 `down -v`，会删除 MySQL 数据和上传文件 volume。

## 7. 现有本地 MySQL 数据迁移

如果要把本机 MySQL 的数据带到服务器，需要单独导出导入。

本地导出：

```powershell
mysqldump -h 127.0.0.1 -P 3306 -u root -p --default-character-set=utf8mb4 inspectai > inspectai.sql
```

服务器导入：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml exec -T mysql mysql -u root -p"$MYSQL_ROOT_PASSWORD" --default-character-set=utf8mb4 inspectai < inspectai.sql
```

如果只是先验证框架，可以不迁移本地测试数据，让服务器从空库开始。
