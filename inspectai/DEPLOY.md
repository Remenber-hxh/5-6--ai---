# InspectAI 生产部署

> ⚠️ 本文件只是入口。完整部署指南请看 → **[docs/DEPLOY.md](docs/DEPLOY.md)**

## 30 秒摘要

```bash
# 服务器上
git clone https://github.com/Remenber-hxh/5-6--ai---.git
cd 5-6--ai---/inspectai

# 1. 配非敏感项
cp .env.prod.example .env.prod
$EDITOR .env.prod

# 2. 准备 secrets（密钥/密码 → 文件，不入环境变量）
export DASHSCOPE_API_KEY=sk-xxx \
       DEEPSEEK_API_KEY=sk-xxx \
       MYSQL_PASSWORD=alphanum \
       MYSQL_ROOT_PASSWORD=alphanum \
       INSPECTAI_ADMIN_PASSWORD=strong
bash scripts/prepare-secrets.sh

# 3. 一键启动
bash scripts/deploy-linux.sh
```

首次没有证书时会先启动 HTTP 引导模式。域名解析完成后，按 [docs/DEPLOY.md](docs/DEPLOY.md) 的 HTTPS 章节签发证书，再重跑部署脚本即可自动切换 HTTPS。

完成后：
- 移动端 `http://<host>/`
- 管理端 `http://<host>/admin/`

详细说明（备份 / 轮换 / 排错 / 镜像版本）→ [docs/DEPLOY.md](docs/DEPLOY.md)
