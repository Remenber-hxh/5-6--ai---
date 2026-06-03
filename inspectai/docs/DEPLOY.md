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
export DEEPSEEK_API_KEY=sk-xxxxxxxxxxxx
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
unset DASHSCOPE_API_KEY DEEPSEEK_API_KEY MYSQL_PASSWORD MYSQL_ROOT_PASSWORD INSPECTAI_ADMIN_PASSWORD
history -d $(history 1)
```

`deploy-linux.sh` 启动后会再次检查容器内健康状态；两个密钥任一未被实际加载，部署会直接失败，不会静默进入规则兜底。

确认 `./secrets/` 权限：
```
drwx------ root  root   secrets/
-rw------- 10001 10001  secrets/dashscope_api_key
-rw------- 10001 10001  secrets/deepseek_api_key
-rw------- 10001 10001  secrets/mysql_password
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

首次部署尚无证书时，脚本会自动使用 `nginx/nginx.bootstrap.conf` 启动 HTTP。已有证书时会自动改用 `nginx/nginx.conf`，开放 HTTPS 并将 HTTP 请求跳转到 HTTPS。

确认两个 AI 密钥都已加载：

```bash
docker compose --env-file .env.prod -f docker-compose.prod.yml exec -T ai-service \
  curl -fsS http://127.0.0.1:9100/health
# 应包含：
# "hasDashscopeKey": true
# "hasVisionKey": true
# "hasDeepSeekKey": true
# "managementAIReady": true
# "managementAI": "deepseek"
```

## 4. 首次部署签发 HTTPS 证书

前置条件：域名 `ai-demo.jadeastech.com` 已解析到服务器公网 IP，云平台安全组已开放 `80` 和 `443`。

```bash
# 1. 首次运行 deploy-linux.sh 后，先确认 HTTP 引导可访问
curl -fsS http://ai-demo.jadeastech.com/health

# 2. 安装 acme.sh 并通过 nginx 暴露的 webroot 签发 Let's Encrypt 证书
curl https://get.acme.sh | sh -s email=your-email@example.com
~/.acme.sh/acme.sh --issue \
  --server letsencrypt \
  --webroot "$PWD/nginx/acme-webroot" \
  -d ai-demo.jadeastech.com

# 3. 安装证书。reloadcmd 用于后续自动续期；首次安装后仍需执行下一步切配置
~/.acme.sh/acme.sh --install-cert -d ai-demo.jadeastech.com \
  --key-file "$PWD/nginx/ssl/ai-demo.jadeastech.com.key" \
  --fullchain-file "$PWD/nginx/ssl/ai-demo.jadeastech.com.crt" \
  --reloadcmd "cd '$PWD' && docker compose --env-file .env.prod -f docker-compose.prod.yml restart nginx"

# 4. 再跑一次部署脚本。脚本检测到证书后会自动启用 HTTPS 配置
bash scripts/deploy-linux.sh
curl -fsS https://ai-demo.jadeastech.com/health
```

## 5. 验证密钥确实不在 env / process list 中

```bash
# 这两条应该看不到 DASHSCOPE_API_KEY / DEEPSEEK_API_KEY 等敏感值
docker compose -f docker-compose.prod.yml exec ai-service env | grep -i key
docker compose -f docker-compose.prod.yml exec go-backend env | grep -i password

# 应该看到 *_FILE 而不是明文：
#   DASHSCOPE_API_KEY_FILE=/run/secrets/dashscope_api_key
#   DEEPSEEK_API_KEY_FILE=/run/secrets/deepseek_api_key
#   INSPECTAI_ADMIN_PASSWORD_FILE=/run/secrets/inspectai_admin_password
```

## 6. 备份 / 恢复

```bash
# MySQL
docker compose -f docker-compose.prod.yml exec mysql \
  mysqldump -u root -p"$(cat secrets/mysql_root_password)" inspectai > backup.sql

# 资产 / 上传图（app_storage 卷）
docker run --rm -v inspectai_app_storage:/data -v "$PWD":/backup alpine \
  tar czf /backup/app_storage.tgz -C /data .
```

## 7. 轮换 secret

```bash
# 1. 写新值
echo -n "new_secret_value" > secrets/dashscope_api_key
chmod 600 secrets/dashscope_api_key

# 2. 重启对应服务（secret 是启动时读的）
docker compose -f docker-compose.prod.yml restart ai-service
```

## 8. 排错

| 现象 | 排查 |
|---|---|
| ai-service 起不来：`未配置 AI 密钥` | `cat secrets/dashscope_api_key` 看是否空文件 |
| AI `/health` 显示 `managementAI=rule_fallback` | `cat secrets/deepseek_api_key` 看是否空文件；补齐后重启 `ai-service` |
| go-backend：`DB_DRIVER=mysql 但 MYSQL_DSN 未设置` | 看 `secrets/mysql_dsn` 是否存在且非空 |
| 首次部署 Nginx 因证书缺失失败 | 确认使用 `bash scripts/deploy-linux.sh` 启动；脚本会自动挂载 `nginx.bootstrap.conf` |
| nginx 502 | `docker compose -f docker-compose.prod.yml ps` 看健康状态；任何 unhealthy → `docker compose logs <svc>` |
| MySQL healthcheck 失败 | 大概率是 mysql_root_password 含特殊字符没转义；用纯字母数字 |

## 9. 镜像版本 / Registry

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
| `deepseek_api_key` | ai-service | DeepSeek 管理分析与问答 API key |
| `mysql_password` | mysql | 业务账户密码 |
| `mysql_root_password` | mysql | root 密码 |
| `mysql_dsn` | go-backend | 完整 DSN，由脚本拼接 |
| `inspectai_auth_token` | go-backend | 移动端 token |
| `inspectai_supervisor_token` | go-backend | 主管 token |
| `inspectai_admin_password` | go-backend | 管理后台登录密码 |
