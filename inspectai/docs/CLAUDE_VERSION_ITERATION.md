# 给 Claude 的协作说明：云端部署后的版本迭代标准

本文用于指导后续代码修改、部署脚本修复和云服务器版本迭代。目标是把项目从“本地演示型”推进到“可持续升级、可回滚、可维护”的交付方式。

## 1. 总原则

服务器只运行稳定版本，不在服务器上直接改代码。

后续所有迭代应遵循：

- 本地开发、本地验证、本地打包。
- 通过版本号发布，不通过覆盖散文件升级。
- 生产服务器只接收 release 包或 Docker 镜像。
- 每次升级前必须备份 MySQL 和上传文件。
- 每次涉及数据库结构变化，必须提供迁移脚本。
- 每个版本必须能说明：改了什么、是否改库、如何回滚。

## 2. 推荐版本策略

采用语义化版本：

- `v1.0.0`：第一个稳定可部署版本。
- `v1.0.1`：小修复，例如 UI、按钮、提示词、导出格式。
- `v1.1.0`：新增功能，例如新增巡检模板、AI 问答、异常复核流程。
- `v2.0.0`：数据库结构或核心业务流程大改，例如接入智能穿戴设备。

每次发版建议同步记录：

- 版本号。
- 发布时间。
- 主要变更。
- 数据库是否变更。
- 部署步骤。
- 回滚方式。
- 已知风险。

建议新增或维护：

- `CHANGELOG.md`
- `release/`
- `migrations/`

## 3. 生产部署目标形态

云端推荐保留以下服务结构：

- `nginx`：唯一对外入口，暴露 80/443。
- `go-backend`：业务 API + 移动端静态页面。
- `admin-frontend`：管理后台静态页面，走 `/admin/`。
- `ai-service`：千问视觉/文本服务。
- `mysql`：生产数据库。
- `app_storage`：上传图片、临时识别文件、报表附件等持久化文件。

对外访问应统一为：

- 移动端：`https://域名/`
- 管理后台：`https://域名/admin/`
- 后端健康检查：`https://域名/health`

不要让生产用户直接访问：

- `:18080`
- `:18081`
- `:19100`

这些端口只适合本地开发。

## 4. 当前需要优先修正的部署问题

以下问题是当前代码审计发现的生产阻塞点，建议优先处理。

### 4.1 `prepare-secrets.sh` 语法损坏

文件：`scripts/prepare-secrets.sh`

问题：

- 有缺失结束引号的 `echo`。
- 部分中文注释和 Shell 语句挤在同一行。
- `dashscope_api_key` 写入逻辑疑似被注释吞掉。
- `gen_token()` 函数定义疑似被注释吞掉。

要求：

- 修复后必须通过 `bash -n scripts/prepare-secrets.sh`。
- 脚本必须能生成以下 secret 文件：
  - `secrets/dashscope_api_key`
  - `secrets/mysql_password`
  - `secrets/mysql_root_password`
  - `secrets/mysql_dsn`
  - `secrets/inspectai_auth_token`
  - `secrets/inspectai_supervisor_token`
  - `secrets/inspectai_admin_password`

### 4.2 `build-images.sh` 语法损坏

文件：`scripts/build-images.sh`

问题：

- `DEFAULT_VERSION=...` 疑似被中文注释吞掉。
- `for svc in ...` 循环疑似被注释吞掉。

要求：

- 修复后必须通过 `bash -n scripts/build-images.sh`。
- 支持：
  - 本地构建。
  - 指定 `INSPECTAI_VERSION`。
  - 指定 `REGISTRY`。
  - 可选 `--push` 推送镜像。

### 4.3 `deploy-linux.sh` 仍是旧部署逻辑

文件：`scripts/deploy-linux.sh`

问题：

- 旧逻辑要求在 `.env.prod` 填敏感信息。
- 新版 `docker-compose.prod.yml` 已改为 Docker secret。
- 两者不匹配。

要求：

- 部署脚本应检查 `.env.prod` 是否存在。
- 部署脚本应检查 `secrets/` 下必要文件是否存在且非空。
- 部署脚本可以提示先运行 `prepare-secrets.sh`。
- 不应再要求把 API key、MySQL 密码写入 `.env.prod`。

### 4.4 管理端 API 地址仍硬编码本地端口

文件：`admin-frontend/app.js`

问题：

- 默认 API 地址仍然是 `http://host:18080`。
- 生产环境只暴露 Nginx 80/443。
- 管理端部署到 `/admin/` 后，应走同源 API。

要求：

- 生产默认 API Base 应为同源，例如 `window.location.origin`。
- 本地 18081 开发可以保留覆盖配置，但不能影响生产。

### 4.5 Go 后端非 root 后的 storage 权限风险

文件：`go-backend/Dockerfile`

问题：

- 当前运行用户改成 `inspectai`，这是正确方向。
- 但 `/app/storage` 没有提前创建并授权。
- Docker volume 挂载后可能导致上传图片、临时识别文件写入失败。

要求：

- 镜像内提前创建 `/app/storage`。
- 授权给 `inspectai:inspectai`。
- 部署文档写明生产数据卷不可删除。

## 5. 数据库迭代规范

后续不要靠代码启动时“隐式改库”完成所有结构演进。建议保留迁移脚本。

建议目录：

```text
migrations/
  001_init.sql
  002_add_admin_users.sql
  003_add_elevator_assets.sql
  004_add_change_request_flow.sql
```

每个迁移脚本要求：

- 可读。
- 可重复判断或明确只能执行一次。
- 注明变更目的。
- 不直接删除生产数据。
- 如果必须删除字段，先提供备份方案。

升级流程：

1. 备份数据库。
2. 执行迁移。
3. 启动新版本服务。
4. 验证移动端、管理端、AI 服务。
5. 验证台账和巡检记录读写。

## 6. 备份和回滚标准

每次升级前必须备份：

- MySQL 数据库。
- `app_storage` 文件卷。
- 当前 `.env.prod`。
- 当前 `secrets/` 目录。

最低回滚能力：

- 保留上一个 Docker 镜像版本。
- 保留上一次数据库备份。
- 保留上一次 storage 备份。
- 可通过修改 `INSPECTAI_VERSION` 回退镜像。

建议回滚流程：

```bash
export INSPECTAI_VERSION=v1.0.0
docker compose --env-file .env.prod -f docker-compose.prod.yml up -d
```

如果数据库已迁移且无法兼容旧版本，必须先恢复数据库备份。

## 7. 发版验收清单

每次交付前至少验证：

- 移动端首页可打开。
- 拍照/上传入口可用。
- AI 识别链路可用。
- 人工确认字段可提交。
- 管理后台 `/admin/` 可打开。
- 登录可用。
- 资产台账可筛选、查看、导出。
- 巡检记录可筛选、查看、导出。
- 异常复核可查看。
- MySQL 正常写入。
- 上传图片能持久保存。
- `docker compose ps` 全部 healthy。
- `GET /health` 返回正常。

## 8. Claude 后续修改建议

建议下一步不要继续做大 UI 改造，先把部署链路闭合：

1. 修复 `prepare-secrets.sh`。
2. 修复 `build-images.sh`。
3. 改造 `deploy-linux.sh` 适配 Docker secret。
4. 修复管理端生产 API Base。
5. 修复 Go 后端 storage volume 权限。
6. 更新 `package-release.ps1`，确保新部署文档和脚本进入 release 包。
7. 增加一份 `CHANGELOG.md` 模板。
8. 增加 `migrations/` 目录和初始迁移说明。

完成以上内容后，再做云服务器部署会更稳。
