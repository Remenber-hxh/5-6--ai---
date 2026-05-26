# InspectAI 生产部署指南

> 适用：Linux 主机 + Docker 28+ / Compose v2。

## 1. 拉代码 / 配置

```bash
git clone <repo> && cd inspectai
cp .env.prod.example .env.prod
$EDITOR .env.prod        # 只填非敏感项（数据库名 / 模型名 / CORS / 显示名）
```

## 2. 准备 secret 文件（敏感凭据**全部**走这里）

```bash
# 用环境变量临时注入，脚本写入 ./secrets/ 0600 文件后立刻 unset
export DASHSCOPE_API_KEY=sk-xxxxxxxxxxxx
export MYSQL_USER=inspectai
export MYSQL_PASSWORD='alphanum_only_no_special_chars'
export MYSQL_ROOT_PASSWORD='alphanum_only'
export MYSQL_DATABASE=inspectai
export INSPECTAI_ADMIN_PASSWORD='strong_admin_password'
# 下面两条不 export 也行，脚本会随机生成
# export INSPECTAI_AUTH_TOKEN=...
# export INSPECTAI_SUPERVISOR_TOKEN=...

bash scripts/prepare-secrets.sh

# 用完立刻清环境（避免留在 history）
unset DASHSCOPE_API_KEY MYSQL_PASSWORD MYSQL_ROOT_PASSWORD INSPECTAI_ADMIN_PASSWORD
history -d $(history 1)
```

确认 `./secrets/` 权限：
```
drwx------ root root  secrets/
-rw------- root root  secrets/dashscope_api_key
-rw------- root root  secrets/mysql_password
...
```

## 3. 拉起服务

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d --build
docker compose -f docker-compose.prod.yml ps
```

健康检查全绿后访问：
- 移动端：`http://<host>/`
- 管理端：`http://<host>/admin/`

## 4. 验证密钥确实不在 env / process list 中

```bash
# 这两条应该看不到 DASHSCOPE_API_KEY 等敏感值
docker compose -f docker-compose.prod.yml exec ai-service env | grep -i key
docker compose -f docker-compose.prod.yml exec go-backend env | grep -i password

# 应该看到 *_FILE 而不是明文：
#   DASHSCOPE_API_KEY_FILE=/run/secrets/dashscope_api_key
#   INSPECTAI_ADMIN_PASSWORD_FILE=/run/secrets/inspectai_admin_password
```

## 5. 备份 / 恢复

```bash
# MySQL
docker compose -f docker-compose.prod.yml exec mysql \
  mysqldump -u root -p"$(cat secrets/mysql_root_password)" inspectai > backup.sql

# 资产 / 上传图（app_storage 卷）
docker run --rm -v inspectai_app_storage:/data -v "$PWD":/backup alpine \
  tar czf /backup/app_storage.tgz -C /data .
```

## 6. 轮换 secret

```bash
# 1. 写新值
echo -n "new_secret_value" > secrets/dashscope_api_key
chmod 600 secrets/dashscope_api_key

# 2. 重启对应服务（secret 是启动时读的）
docker compose -f docker-compose.prod.yml restart ai-service
```

## 7. 排错

| 现象 | 排查 |
|---|---|
| ai-service 起不来：`未配置 AI 密钥` | `cat secrets/dashscope_api_key` 看是否空文件 |
| go-backend：`DB_DRIVER=mysql 但 MYSQL_DSN 未设置` | 看 `secrets/mysql_dsn` 是否存在且非空 |
| nginx 502 | `docker compose -f docker-compose.prod.yml ps` 看健康状态；任何 unhealthy → `docker compose logs <svc>` |
| MySQL healthcheck 失败 | 大概率是 mysql_root_password 含特殊字符没转义；用纯字母数字 |

## 8. 镜像版本 / Registry

镜像名格式：`${REGISTRY}/<svc>:${INSPECTAI_VERSION}`，默认 `inspectai/<svc>:dev`。

构建（git short-sha 自动作版本号）：
```bash
bash scripts/build-images.sh
# → inspectai/ai-service:c7453dc, inspectai/go-backend:c7453dc, inspectai/admin-frontend:c7453dc
#   同时打 :latest tag
```

显式指定版本：
```bash
INSPECTAI_VERSION=1.2.3 bash scripts/build-images.sh
```

推到私有 registry：
```bash
REGISTRY=registry.cn-hangzhou.aliyuncs.com/myorg \
INSPECTAI_VERSION=1.2.3 \
  bash scripts/build-images.sh --push
```

部署目标机器拉取并部署：
```bash
export REGISTRY=registry.cn-hangzhou.aliyuncs.com/myorg
export INSPECTAI_VERSION=1.2.3
docker compose --env-file .env.prod -f docker-compose.prod.yml pull
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d
```

回滚：把 `INSPECTAI_VERSION` 改回上一个版本，重新 `pull + up -d` 即可。

## 附：现行 secret 清单

| 文件 | 谁读 | 备注 |
|---|---|---|
| `dashscope_api_key` | ai-service | 千问 API key |
| `mysql_password` | mysql | 业务账户密码 |
| `mysql_root_password` | mysql | root 密码 |
| `mysql_dsn` | go-backend | 完整 DSN，由脚本拼接 |
| `inspectai_auth_token` | go-backend | 移动端 token |
| `inspectai_supervisor_token` | go-backend | 主管 token |
| `inspectai_admin_password` | go-backend | 管理后台登录密码 |
