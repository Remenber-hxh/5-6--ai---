# Docker 与云部署迭代方案

> 日期：2026-05-12（5/13 demo 前预留）
> 状态：**未实施**，本文档为后续上云时的迭代依据
> 写给：5/15 比赛交付后的工程化阶段
> 前置阅读：[`16-Claude-当前架构与目录总览.md`](16-Claude-当前架构与目录总览.md)、[`17-Codex-二次复查与交付建议.md`](17-Codex-二次复查与交付建议.md)

## 1. 当前 Docker 配置的真实状况

| 文件 | 现状 | 问题 |
|---|---|---|
| `inspectai/docker-compose.yml` | `DEMO_MODE=true` + `AI_PROVIDER=mock` | 容器跑起来是 mock，不是真 AI |
| `inspectai/ai-service/Dockerfile` | `python:3.13-slim` 基础镜像 | 缺 `curl` 和 `ca-certificates`，run.py 调 DashScope 会失败 |
| `inspectai/go-backend/Dockerfile` | 基本可用 | 未验证多平台交叉编译，未明确 GOTOOLCHAIN |
| `inspectai/.env.secure` | DPAPI 加密（绑当前 Windows 用户） | 拷到 Linux 容器或别人机器无法解密，必须换密钥载入方式 |
| `inspectai/nginx/nginx.conf` | 只监听 80，无 HTTPS / 域名 / 上传限流 | 不能直接做生产反代 |

## 2. 整体目标

把现在 Windows 本地能跑的链路平移到一台云服务器（Linux），通过 HTTPS 域名对外提供服务，能接入企业微信。**不重构架构**，只补容器化运行所需的差异。

## 3. 迭代步骤（按优先级排序）

### 第一步：让 ai-service 容器能调真 API

**改 `inspectai/ai-service/Dockerfile`**：

```dockerfile
FROM python:3.13-slim

# 加 curl + 证书（run.py 用 subprocess curl 调 DashScope）
RUN apt-get update && apt-get install -y --no-install-recommends \
        curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY run.py prompts/ ./
EXPOSE 9100
CMD ["python", "run.py"]
```

**或者**（更稳，去掉 subprocess curl）：把 run.py 里的 `call_qwen_chat` 换成 `httpx`/`requests`，避免依赖系统 curl。Linux 容器里 IPv6 通常没有 Windows 那种 IPv4/v6 优先级问题，可以直接用标准 HTTP 客户端。

### 第二步：拆出生产 compose

新建 `inspectai/docker-compose.prod.yml`，**不复用本地 demo 配置**：

```yaml
version: "3.9"
services:
  ai-service:
    build: ./ai-service
    environment:
      DEMO_MODE: "false"
      AI_PROVIDER: "qwen"
      QWEN_VISION_MODEL: "qwen-vl-plus"
      QWEN_TEXT_MODEL: "qwen-plus"
      DASHSCOPE_API_KEY: ${DASHSCOPE_API_KEY}      # 从宿主机环境变量注入
    expose: ["9100"]
    restart: unless-stopped

  backend:
    build: ./go-backend
    environment:
      AI_SERVICE_URL: "http://ai-service:9100"
      SQLITE_PATH: "/data/inspectai.db"
      STORAGE_DIR: "/data"
      FRONTEND_DIR: "/app/frontend"
    volumes:
      - ./frontend:/app/frontend:ro
      - inspectai_data:/data                       # SQLite + uploads 持久化
    expose: ["8080"]
    depends_on: [ai-service]
    restart: unless-stopped

  nginx:
    image: nginx:1.27-alpine
    ports: ["80:80", "443:443"]
    volumes:
      - ./nginx/nginx.prod.conf:/etc/nginx/nginx.conf:ro
      - /etc/letsencrypt:/etc/letsencrypt:ro       # 证书路径
    depends_on: [backend]
    restart: unless-stopped

volumes:
  inspectai_data:
```

启动：

```bash
export DASHSCOPE_API_KEY=sk-xxx   # 或写在 /etc/inspectai.env，不进 git
docker compose -f docker-compose.prod.yml up -d
```

### 第三步：密钥管理换思路（不能用 DPAPI）

云端密钥**绝不写镜像**。三种推荐方式（从轻到重）：

| 方式 | 适用 | 操作 |
|---|---|---|
| 宿主机环境变量 | 单机部署 | `/etc/inspectai.env`（root 600 权限）→ docker-compose `env_file:` 加载 |
| Docker secret | swarm/k8s | `echo $KEY \| docker secret create dashscope_key -`，容器读 `/run/secrets/dashscope_key` |
| 云厂商 KMS | 多人/合规 | 阿里云 KMS / AWS Secret Manager，容器启动时拉取 |

> `.env.secure`（DPAPI）**只用于本地 Windows 开发**，不进生产部署。

### 第四步：Nginx 生产化

新建 `inspectai/nginx/nginx.prod.conf`：

```nginx
server {
    listen 80;
    server_name inspectai.example.com;
    return 301 https://$host$request_uri;            # 强制 HTTPS
}

server {
    listen 443 ssl http2;
    server_name inspectai.example.com;

    ssl_certificate     /etc/letsencrypt/live/inspectai.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/inspectai.example.com/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;

    client_max_body_size 30M;                        # 单图最大 15M，留余量

    # 企业微信可信域名校验文件（如有）
    location /MP_verify_xxx.txt {
        alias /usr/share/nginx/wechat/MP_verify_xxx.txt;
    }

    location / {
        proxy_pass http://backend:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        proxy_read_timeout 120s;                     # AI 任务可能 60s+
    }
}
```

证书来源（任选其一）：
- Let's Encrypt + certbot：免费、自动续期、最常用
- 阿里云 / 腾讯云 SSL：托管，不用管续期
- 公司已有 wildcard 证书：直接挂载

### 第五步：企业微信接入清单（域名侧）

需要预先准备：
- 备案过的域名（中国大陆服务器必须）
- DNS A 记录指向服务器 IP（或 CNAME 到 SLB）
- 企业微信后台「应用」→「开发者接口」→ 配置：
  - **可信域名**：`inspectai.example.com`
  - **可信 IP**：服务器出口 IP
  - **应用主页 URL**：`https://inspectai.example.com/`
- 校验文件：企微后台下载 `MP_verify_xxx.txt`，挂到站点根

### 第六步：数据持久化与备份

| 内容 | 容器挂载 | 备份策略 |
|---|---|---|
| SQLite 主库 | `inspectai_data:/data` | 每日 cron `sqlite3 .backup` 到对象存储 |
| 巡检图片 | 同上 `/data/uploads/` | 同步到 OSS/S3，本地保留 30 天 |
| 临时分类图 | 同上 `/data/tmp_classify/` | 不需备份，每日清理 1 天前的 |

启动脚本里加 `sqlite3 /data/inspectai.db "PRAGMA wal_checkpoint(TRUNCATE);"` 定期合并 WAL，避免 -wal 文件膨胀。

### 第七步：源码包瘦身（codex 17 号文档提的）

打包前清掉：
- `go-backend/server*.exe`（Linux 容器要重新 build）
- `storage/*`（不要把开发数据带上云）
- `samples/`（业务素材，云端不需要；如要 demo 数据另起 `samples/demo/`）
- `_archive/`（codex 第一版归档，云端不要）
- `.gocache/` `.gotelemetry/` `__pycache__/`（已被 .gitignore 覆盖）

`.dockerignore` 建议：

```
# 本地构建产物
.gocache/
.gotelemetry/
storage/
__pycache__/
*.pyc
*.pid
*.log

# 本地密钥
.env
.env.secure

# 本地二进制
go-backend/server*.exe
go-backend/server

# 文档与历史
docs/
_archive/
samples/
```

## 4. 一次完整上云的执行清单

```bash
# 0. 服务器准备（Ubuntu 22.04+）
apt update && apt install -y docker.io docker-compose-v2 certbot

# 1. 拉代码
git clone <repo> /opt/inspectai && cd /opt/inspectai/inspectai

# 2. 写密钥（不进 git）
echo "DASHSCOPE_API_KEY=sk-xxx" | sudo tee /etc/inspectai.env
sudo chmod 600 /etc/inspectai.env

# 3. 申请证书（首次）
certbot certonly --standalone -d inspectai.example.com

# 4. 启动
docker compose -f docker-compose.prod.yml --env-file /etc/inspectai.env up -d

# 5. 验证
curl https://inspectai.example.com/health
docker compose logs -f ai-service
```

## 5. 不打算做的事（明确 out of scope）

- 多用户认证、RBAC（第一版单租户演示，企业微信 SSO 留给二期）
- 横向扩展（goroutine 单机够用，没必要起多 backend 实例）
- 离线模式（强依赖 DashScope）
- 模型私有化部署（成本不划算）
- 全自动巡检（最终结果以人工确认为准是产品定位）

## 6. 风险与回退

| 风险 | 影响 | 回退手段 |
|---|---|---|
| DashScope 配额耗尽 | 无法识别 | 临时切回 `DEMO_MODE=true` 走 mock，演示能继续；私下补 key |
| 千问模型变更 / API 不兼容 | 解析失败 | prompt 加 schema 校验 + retake_required 兜底（已做） |
| 证书过期 | HTTPS 不可用 | certbot --auto-renew，cron 监控；备一个手动续期脚本 |
| SQLite 单点 | 数据库锁/损坏 | WAL 模式（已开），每日全量备份，必要时升级到 PostgreSQL |
| 企业微信内嵌 webview 兼容 | 部分功能（如相机）异常 | 已加"从相册上传"+"手动选择模板"备选路径 |

—— end ——
