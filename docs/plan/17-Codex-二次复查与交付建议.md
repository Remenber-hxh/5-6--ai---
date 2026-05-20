# Codex 二次复查与交付建议

> 日期：2026-05-12  
> 范围：只检查现有项目目录、运行状态、交付风险；未修改业务代码。  
> 写给：Claude 继续开发时参考，也给演示前自查用。

## 1. 结论

当前 `inspectai/` 已经可以作为 5/13 演示框架继续打磨：本地 Go 后端、Python AI 服务、模板接口、台账接口均可访问，AI 服务当前已处于 `qwen` 真 API 模式。

但它还不是“可直接上云的正式包”。主要原因有三点：Docker 配置仍默认 mock、Python Docker 镜像缺少当前千问调用依赖、台账目前是自动沉淀与展示，不是完整可编辑资产后台。

## 2. 当前已确认状态

- 本地后端健康：`http://127.0.0.1:8080/health` 返回 `status=ok`，`storeKind=sqlite`。
- AI 服务健康：`http://127.0.0.1:9100/health` 返回 `demoMode=false`、`provider=qwen`、`hasKey=true`、`promptsLoaded=6`。
- 模板接口可用：`/api/report/templates` 已返回巡检模板与字段。
- 台账接口可用：`/api/assets` 已返回历史提交后沉淀出的资产记录。
- `docs/plan/16-Claude-当前架构与目录总览.md` 已明确作为现行版本真相来源，`inspectai/README.md` 暂不应作为唯一依据。

## 3. 主要问题与建议

### P1：Docker / 上云路径还未对齐当前真 API 运行方式

现状：

- `inspectai/docker-compose.yml` 中 `ai-service` 仍设置 `DEMO_MODE=true`、`AI_PROVIDER=mock`。
- `inspectai/ai-service/Dockerfile` 只基于 `python:3.13-slim`，没有安装 `curl` 和证书包。
- 当前 `ai-service/run.py` 调千问依赖子进程 `curl -4 --noproxy *`，因此容器内大概率无法直接跑真 API。
- `.env.secure` 使用 Windows DPAPI，只适合当前 Windows 用户本机启动，不适合云服务器或 Docker/Linux 环境迁移。

建议：

- 5/13 演示先继续用本地 `scripts/start-local.ps1`，不要临时切到 Docker，避免上云配置影响演示稳定性。
- 云服务器版本单独做一套 `docker-compose.prod.yml` 或生产环境变量模板，不复用当前本地 `.env.secure`。
- 若继续保留 `curl` 调用方式，Dockerfile 需要安装 `curl`、`ca-certificates`；更稳的方案是后续把 AI 服务改为 Python HTTP 客户端调用 DashScope。
- 云端密钥应走服务器环境变量、Docker secret、宝塔/面板环境变量或云厂商 Secret Manager，不把 key 写入镜像和仓库。

### P1：台账目前“可看、可沉淀”，但还不是完整可编辑后台

现状：

- 后端路由里 `/api/assets` 只有 `GET` 列表能力。
- 日报提交时会 `UpsertAsset`，自动写入或更新资产台账。
- 前端 `renderAssets()` 目前只渲染资产名称、项目、类型、累计次数、状态、最近总结。
- 字段修改能力存在于巡检日报表单阶段：人工修改识别字段后再提交，提交结果会影响后续台账。

建议：

- 对领导演示时表述要准确：当前已实现“填报即台账”，即每次提交日报会自动沉淀资产记录；完整“台账后台编辑”是下一阶段。
- 如果必须支持台账修改，建议最小闭环为：资产详情页、历史巡检记录列表、资产基础信息编辑、状态备注编辑、修改日志。
- 后端建议补 `/api/assets/{id}` 的 `GET/PATCH`，前端补资产详情与编辑弹层；不要直接在列表卡片上做复杂编辑，移动端容易误触。
- 台账数据口径建议以“人工确认后的字段”为准，AI 总结只作为附加说明，不反向覆盖人工字段。

### P2：目录清理已经完成大部分，但仍有交付残留

现状残留：

- `inspectai/go-backend/server.exe`
- `inspectai/go-backend/server_clean.exe`
- `inspectai/go-backend/cmd/server/handlers.go.271170045668782683`
- `inspectai/storage/*.pid`
- `inspectai/storage/inspectai.db-shm`
- `inspectai/storage/inspectai.db-wal`
- `inspectai/.gocache/`
- `inspectai/.gotelemetry/`
- `inspectai/ai-service/__pycache__/`

判断：

- 这些残留不一定影响当前本地演示。
- 但它们不应进入最终交付包或云服务器源码包。
- `.gitignore` 已覆盖不少运行产物，但只忽略了 `go-backend/cmd/server/main.go.*`，没有覆盖 `handlers.go.*` 这类临时备份文件。

建议：

- 5/13 演示前不要随意删除 `storage/inspectai.db`，里面已有台账演示数据。
- 打包上云前再做一次“源码包清理”，只保留源码、模板、必要配置和一份可控 demo 数据。
- `.gitignore` 后续可补：`go-backend/cmd/server/*.go.*`、`go-backend/*.exe`、`storage/tmp_classify/`、`storage/uploads/` 的交付策略。

### P2：样本数据很有价值，但不应默认跟随上云

现状：

- `samples/templates/` 有 10+ 个真实 Excel 模板，是字段设计的重要依据。
- `samples/photos/` 有大量真实巡检照片，适合做模型测试和 demo。
- 部分目录仍有 `.WeDrive` 标记文件。
- 样本总量较大，且可能包含真实项目、人员、设备信息。

建议：

- 保留 `samples/` 作为本地研发资产，不直接放入云服务器正式目录。
- 为演示单独整理 `samples/demo/`，只放 3 到 5 组可公开展示的图片和模板。
- 若要对外展示或上传云端，建议先做脱敏：项目名、人员名、设备编号、二维码、定位信息都要检查。

### P2：文档权威源需要继续收敛

现状：

- `docs/plan/16-Claude-当前架构与目录总览.md` 已声明为现行真相来源。
- `inspectai/README.md` 仍可能让后来的人误以为第一版说明就是当前事实。

建议：

- 后续只维护一个主入口：`docs/plan/README.md`。
- `inspectai/README.md` 顶部应补一句“当前架构以 docs/plan/16 为准”，或者在下一轮由 Claude 重写成最短启动说明。
- 每次结构调整后更新 `docs/plan/README.md`，否则多人协作会再次出现“代码已改、文档没同步”的问题。

### P3：Nginx 目前只是本地反代，不是生产域名证书方案

现状：

- `inspectai/nginx/nginx.conf` 只监听 80 端口。
- 当前没有 HTTPS server、证书路径、域名、HSTS、上传限流、真实客户端 IP 可信代理配置。

建议：

- 本地演示不需要改。
- 云服务器需要单独准备：可信域名备案/解析、HTTPS 证书、Nginx 443 配置、企业微信可信域名白名单。
- 如果用宝塔或云厂商 Nginx，项目内 `nginx/nginx.conf` 可作为反代逻辑参考，不建议直接当生产配置复制。

## 4. 5/13 演示前建议顺序

1. 不做大迁移，不切 Docker，先保证当前本地链路稳定。
2. 准备 2 组固定演示图片：一组识别成功，一组触发重拍/转人工。
3. 演示话术聚焦三段：拍照识别、人工确认、提交后形成台账。
4. 对台账能力的表述要收口：当前是“自动沉淀资产台账与最近状态”，不是“完整资产管理后台”。
5. 演示前保留当前 SQLite 数据，除非确认已有可复现的 demo 数据导入脚本。

## 5. 给 Claude 的下一步落点

- 优先补台账详情与编辑方案，而不是继续扩大 AI 识别范围。
- 优先修 Docker 真 API 运行路径，而不是先上云试错。
- 优先做 demo 数据集和一键恢复脚本，避免演示现场临时依赖真实图片质量。
- 优先把“当前能做 / 下阶段做”写进展示材料，避免领导把原型当成已上线系统验收。

