# Claude 版本代码审计与框架解读

日期：2026-05-12  
项目目录：`D:\5-6月 ai 大会\inspectai`  
目标：给当前 Claude 重写后的程序做一次结构说明、代码审计和后续修正优先级整理，方便继续协作。

## 1. 总体结论

当前项目已经从“原型页面”进入到“可联调的第一版工程骨架”阶段，主要模块已经形成：

- `frontend`：移动端优先的拍照填报页面。
- `go-backend`：Go 后端，负责接口、记录、任务、台账和静态资源托管。
- `ai-service`：Python AI 微服务，负责场景识别、字段识别、日报总结。
- `storage`：本地 SQLite 数据库、上传图片、临时识别图片、日志文件。
- `scripts`：本地启动、停止、密钥配置脚本。
- `nginx` / `docker-compose.yml`：已有部署雏形，但还不是生产可直接上云版本。

项目方向是对的：现在已经不是单纯 HTML 演示，而是具备“拍照、AI 识别、人工确认、提交、沉淀台账”的基本闭环。

但当前版本还不能直接作为领导验收或云端上线版本，主要风险集中在 5 个地方：

- AI 调用失败时会回落到模拟数据，可能把假识别结果当真数据入库。
- 提交接口缺少后端必填校验，前端校验不能替代后端校验。
- 幂等键只是检查存在，没有真正防止重复提交。
- 台账目前能看，但还不能编辑。
- 临时图片转正式图片时，图片 ID 解析有明显 bug，会导致图片 ID 变成 `img`，影响后续图片追踪。

## 2. 当前目录结构解读

```text
inspectai/
├─ frontend/
│  ├─ index.html
│  ├─ app.js
│  ├─ styles.css
│  └─ *.codex-bak
├─ go-backend/
│  ├─ cmd/server/
│  │  ├─ main.go
│  │  ├─ handlers.go
│  │  ├─ store.go
│  │  ├─ types.go
│  │  ├─ templates.go
│  │  ├─ ai_client.go
│  │  ├─ schema.sql
│  │  └─ util.go
│  └─ Dockerfile
├─ ai-service/
│  ├─ run.py
│  ├─ Dockerfile
│  └─ prompts/
├─ storage/
│  ├─ inspectai.db
│  ├─ uploads/
│  ├─ tmp_classify/
│  ├─ logs/
│  └─ pids/
├─ scripts/
│  ├─ start-local.ps1
│  ├─ stop-local.ps1
│  └─ setup-key.ps1
├─ nginx/
│  └─ nginx.conf
├─ docker-compose.yml
├─ .env
├─ .env.example
└─ .env.secure
```

### frontend

前端目前是移动端流程页面，核心文件：

- `frontend/index.html`：页面结构，包含拍照、识别中、场景确认、表单确认、日报预览、台账查看几个场景。
- `frontend/app.js`：前端业务逻辑，主要流程是 `camera -> loading -> classify -> form -> preview -> ledger`。
- `frontend/styles.css`：企业微信风格的移动端样式。

关键逻辑位置：

- `frontend/app.js:204`：上传图片并进行场景识别。
- `frontend/app.js:260`：按模板创建巡检记录。
- `frontend/app.js:291`：启动 AI 识别任务。
- `frontend/app.js:368`：渲染人工确认表单。
- `frontend/app.js:459`：保存人工修改字段。
- `frontend/app.js:494`：生成日报预览。
- `frontend/app.js:543`：提交记录。
- `frontend/app.js:569`：渲染资产台账列表。

### go-backend

Go 后端已经从单文件拆成相对清晰的服务结构：

- `main.go`：服务启动、配置读取、SQLite / 内存存储选择、静态资源挂载。
- `handlers.go`：HTTP API 和核心业务流程。
- `store.go`：Store 接口、内存实现、SQLite 实现。
- `types.go`：记录、字段、图片、AI 任务、资产台账等结构定义。
- `templates.go`：内置巡检模板和字段定义。
- `ai_client.go`：调用 Python AI 服务。
- `schema.sql`：SQLite 表结构。
- `util.go`：ID、时间、JSON 等工具函数。

关键逻辑位置：

- `go-backend/cmd/server/handlers.go:72`：资产台账列表接口。
- `go-backend/cmd/server/handlers.go:90`：创建巡检记录。
- `go-backend/cmd/server/handlers.go:249`：启动 AI 分析任务。
- `go-backend/cmd/server/handlers.go:299`：后台执行 AI 分析。
- `go-backend/cmd/server/handlers.go:404`：人工修改字段。
- `go-backend/cmd/server/handlers.go:462`：提交巡检记录。
- `go-backend/cmd/server/handlers.go:515`：图片场景识别。
- `go-backend/cmd/server/handlers.go:605`：临时图片转正式图片。
- `go-backend/cmd/server/handlers.go:795`：生成资产台账条目。
- `go-backend/cmd/server/store.go:424`：SQLite 台账 upsert。

### ai-service

Python AI 微服务负责把图片和字段交给视觉模型处理：

- `ai-service/run.py`：HTTP 服务入口，包含 `/classify`、`/analyze`、`/summarize`、`/health`。
- `ai-service/prompts/`：不同场景的提示词文件。

关键逻辑位置：

- `ai-service/run.py:151`：通过 `curl` 调用千问 / DashScope。
- `ai-service/run.py:244`：字段识别入口。
- `ai-service/run.py:392`：模拟字段识别结果。
- `ai-service/run.py:437`：日报总结入口。
- `ai-service/run.py:491`：模拟日报总结。
- `ai-service/run.py:510`：场景分类入口。
- `ai-service/run.py:621`：健康检查返回。

## 3. 当前业务闭环

当前代码实现的主流程如下：

```text
企业微信 / 手机浏览器
    ↓
frontend 拍照页面
    ↓
POST /api/scene/classify
    ↓
Go 后端保存临时图片，并调用 Python AI 服务 /classify
    ↓
用户确认模板，或手工选择模板
    ↓
POST /api/inspection/records
    ↓
Go 后端创建巡检记录，并把临时图片转正式图片
    ↓
POST /api/inspection/records/{id}/ai-tasks
    ↓
Go 后端异步调用 Python AI 服务 /analyze
    ↓
前端轮询 AI 任务结果
    ↓
展示识别字段，人工逐项确认或修改
    ↓
PATCH /api/inspection/records/{id}/fields/{code}
    ↓
POST /api/inspection/records/{id}/submit
    ↓
Go 后端调用 Python AI 服务 /summarize
    ↓
形成日报、提交记录、沉淀资产台账
    ↓
GET /api/assets 查看台账
```

这个闭环已经贴近最终产品目标：拍照即填报、人工确认、提交后沉淀台账。

## 4. 数据沉淀逻辑

当前 SQLite 有三张核心表：

- `records`：巡检记录，保存一次巡检的图片、字段、日报、AI 总结、提交状态。
- `assets`：资产台账，保存每个资产 / 点位的最新状态和累计巡检次数。
- `ai_tasks`：AI 异步任务，保存识别任务状态、错误和结果。

表结构位置：

- `go-backend/cmd/server/schema.sql:1`：`records` 表。
- `go-backend/cmd/server/schema.sql:31`：`assets` 表。
- `go-backend/cmd/server/schema.sql:47`：`ai_tasks` 表。

目前台账形成方式：

- 用户提交巡检记录时，后端从记录里提取点位、模板、状态、摘要。
- 后端构造一个 `AssetEntry`。
- SQLite 通过 `UpsertAsset` 写入或更新台账。
- 如果同一个资产再次提交，会更新最新状态，并增加 `inspection_count`。

这说明“数据积累”的方向已经有了，但台账还缺两个关键能力：

- 资产台账详情页 / 编辑接口。
- 字段级历史记录或资产字段快照。

如果领导要看“台账能不能持续积累”，当前可以演示“提交后台账列表更新”。  
如果领导要看“台账能不能维护和修改”，当前还需要补接口和前端编辑页。

## 5. 主要问题清单

### P0：正式模式下 AI 失败会回落到模拟数据

位置：

- `ai-service/run.py:292`
- `ai-service/run.py:455`
- `ai-service/run.py:464`
- `ai-service/run.py:392`
- `ai-service/run.py:491`

问题：

AI 调用失败时，代码会返回 mock 数据。字段识别的 mock 结果还可能返回 `recognitionStatus: recognized`。这意味着即使千问没有成功识别，系统也可能展示一组看起来“识别成功”的假数据。

影响：

这是最危险的问题。因为本项目强调“人工确认真实字段后提交”，如果 mock 数据混入正式流程，后续台账和日报都会失真。

建议：

只有 `DEMO_MODE=true` 才允许 mock。  
当 `DEMO_MODE=false` 且 AI 调用失败时，应返回 `retake_required` 或 `manual_required`，不能生成模拟字段。

### P1：提交接口缺少后端字段校验

位置：

- `go-backend/cmd/server/handlers.go:462`
- `go-backend/cmd/server/handlers.go:463`

问题：

`handleSubmit` 目前只检查 `Idempotency-Key` 是否存在，没有检查必填字段是否完整，也没有检查字段是否还处于 `needsReview`。

影响：

前端虽然做了本地校验，但任何人都可以绕过前端直接调用接口。后端如果不校验，空字段、未确认字段也可能提交成功。

建议：

提交前必须在后端校验：

- 必填字段不能为空。
- 需要人工确认的字段必须被确认或修改。
- 未完成 AI 识别或人工兜底的记录不能提交。
- 已提交记录重复提交时要走幂等逻辑。

### P1：幂等键没有真正生效

位置：

- `go-backend/cmd/server/handlers.go:463`
- `go-backend/cmd/server/store.go:424`

问题：

当前只检查请求头里有没有 `Idempotency-Key`，但没有保存和比对这个 key。重复点击提交按钮时，后端仍可能重复执行提交逻辑。

影响：

`UpsertAsset` 会增加 `inspection_count`，重复提交可能导致巡检次数虚高。

建议：

至少要做到：

- 已提交的 `record` 再次提交时直接返回原结果，不重复更新台账。
- 或新增 `submission_keys` / `idempotency_keys` 表，保存 recordID + idempotencyKey。

### P1：台账写入错误被忽略

位置：

- `go-backend/cmd/server/handlers.go:508`

问题：

当前代码是 `_ = s.store.UpsertAsset(asset)`，直接丢弃错误。

影响：

如果台账写入失败，前端仍会看到提交成功，实际资产台账却没有更新。这会造成“日报有、台账没有”的数据断层。

建议：

提交时必须检查台账写入错误。  
如果日报提交成功但台账写失败，至少要返回明确错误或记录补偿任务。

### P1：台账目前只能查看，不能修改

位置：

- `go-backend/cmd/server/handlers.go:42`
- `go-backend/cmd/server/handlers.go:72`
- `frontend/app.js:569`

问题：

目前只有 `GET /api/assets` 列表接口，没有 `GET /api/assets/{id}` 或 `PATCH /api/assets/{id}`。

影响：

用户已经明确提出“台账需要能看到数据并能修改”。当前版本只满足“能看”，还不满足“能维护”。

建议：

下一步新增：

- 资产详情接口。
- 资产编辑接口。
- 前端台账详情 / 编辑页。
- 编辑日志字段，区分“巡检自动更新”和“人工维护修改”。

### P1：临时图片转正式图片时 ID 解析错误

位置：

- `go-backend/cmd/server/handlers.go:605`
- `go-backend/cmd/server/handlers.go:629`
- `go-backend/cmd/server/util.go:15`

问题：

`newID("img")` 生成的图片 ID 类似 `img_时间戳_随机值`。  
但 `adoptTmpImages` 用 `strings.Index(name, "_")` 取第一个下划线前的内容，最后 ID 会变成 `img`。

影响：

所有图片可能都变成同一个 ID，影响图片追踪、图片与识别结果关联、后续补传和审计。

建议：

不要按第一个下划线截断。  
应根据前端传入的完整 `imageIds` 做前缀匹配，或按固定三段 ID 规则解析 `img_时间戳_随机值`。

### P2：场景识别失败时返回结构和前端预期不一致

位置：

- `go-backend/cmd/server/handlers.go:515`
- `frontend/app.js:204`

问题：

前端期望返回：

```json
{
  "classify": {},
  "images": []
}
```

但后端在某些 AI classify 异常分支可能返回裸的 `SceneClassifyResult`。

影响：

失败兜底路径可能丢失 `tmpDir` / `imageIds`，导致后续手工选择模板后无法创建记录或无法绑定图片。

建议：

无论 AI classify 成功还是失败，都返回统一结构：

```json
{
  "tmpDir": "...",
  "images": [],
  "classify": {}
}
```

### P2：存在数据编码污染风险

观察：

接口返回中曾出现 `����Ա`、`�Ϻ��ż�...` 这类乱码或替换字符。

影响：

如果点位名、资产名、巡检员名称发生编码污染，会直接影响资产 ID 生成和台账归并。

建议：

排查来源：

- 前端提交数据是否 UTF-8。
- PowerShell / 脚本写入样例数据是否编码错误。
- SQLite 中是否已经写入污染数据。
- API 响应头是否统一 `application/json; charset=utf-8`。

### P2：Docker 和云部署配置仍偏演示环境

位置：

- `docker-compose.yml:7`
- `docker-compose.yml:8`
- `ai-service/Dockerfile:6`

问题：

`docker-compose.yml` 里仍是：

```yaml
DEMO_MODE: "true"
AI_PROVIDER: "mock"
```

另外 `ai-service/run.py` 通过 `curl` 调用外部模型，但 `ai-service/Dockerfile` 只用了 `python:3.13-slim`，没有明确安装 `curl` 和 CA 证书。

影响：

本地能跑不代表容器里能正常调用千问。上云后如果镜像缺 `curl` 或证书，AI 服务会失败。

建议：

生产部署前要补：

- 关闭 `DEMO_MODE`。
- 设置真实 `AI_PROVIDER=qwen`。
- 使用环境变量或密钥管理注入 `DASHSCOPE_API_KEY`。
- Dockerfile 安装 `curl` 和 `ca-certificates`。
- Nginx 配置 HTTPS、反代和上传体积限制。

## 6. 当前可向领导说明的完成阶段

建议表述：

> 目前系统已经完成第一版工程骨架：前端移动端流程、Go 后端接口、Python AI 微服务、本地 SQLite 台账沉淀都已经打通。现在的重点不是再做一个页面，而是把识别结果、人工确认、日报生成和资产台账串成闭环。当前版本可以展示主流程，但在正式交付前，还需要补齐后端校验、台账编辑、幂等提交和正式 AI 失败兜底，避免演示数据进入真实台账。

## 7. 下一步优先级

### 第一优先级：保证数据真实

必须先修：

- 禁止正式模式使用 mock 识别结果。
- 提交接口增加必填字段和人工确认校验。
- 修复图片 ID 解析。
- 台账写入失败不能吞掉错误。

原因：

这个系统的核心不是“看起来识别了”，而是“沉淀到台账的数据可信”。如果这一层不稳，后面日报、台账、统计都不可信。

### 第二优先级：补台账维护能力

需要补：

- 台账详情页。
- 台账字段编辑。
- 台账修改日志。
- 区分 AI 自动沉淀字段和人工维护字段。

原因：

用户已经明确提出台账要“能看到、能修改”。这是从演示系统走向管理系统的关键。

### 第三优先级：做云部署准备

需要补：

- 生产 `.env.example`。
- Dockerfile 依赖补齐。
- 关闭 demo/mock 默认值。
- Nginx HTTPS 配置说明。
- 企业微信可信域名、JS-SDK、安全回调域名准备清单。
- 数据库备份和图片持久化目录规划。

原因：

当前项目迁移云服务器不难，但不能直接裸跑。需要先把配置、密钥、文件存储、反代、安全边界整理好。

## 8. 给 Claude 的直接建议

请优先处理以下代码点：

1. `ai-service/run.py`：把 mock fallback 限制在 `DEMO_MODE=true`，正式模式失败时返回重拍或人工填写。
2. `go-backend/cmd/server/handlers.go`：修复 `handleSubmit` 的后端校验、幂等逻辑和台账写入错误处理。
3. `go-backend/cmd/server/handlers.go`：修复 `adoptTmpImages` 的图片 ID 解析。
4. `go-backend/cmd/server/handlers.go` 与 `store.go`：增加资产台账详情和编辑接口。
5. `frontend/app.js`：增加台账详情 / 编辑交互，保持移动端优先。
6. `docker-compose.yml` 与 `ai-service/Dockerfile`：补生产部署配置，避免容器里 AI 调用失败。

## 9. 建议验收口径

短期演示验收：

- 手机端可以拍照上传。
- 系统能识别场景或进入人工选择模板。
- AI 能填充字段。
- 人工可以逐项修改字段。
- 提交后生成日报。
- 提交后资产台账出现对应记录。

正式交付验收：

- AI 失败不能生成假数据。
- 必填字段未确认不能提交。
- 重复点击不会重复增加台账次数。
- 台账可以查看、编辑、保留修改痕迹。
- 图片与记录、字段、日报能追溯。
- 云服务器部署后 HTTPS、域名、企业微信入口可用。

## 10. 一句话定位

这个版本已经有了“巡检拍照填报系统”的工程雏形。  
下一阶段的关键不是继续堆功能，而是把“识别结果可信、人工确认有效、台账可维护、云端可迁移”四件事做扎实。
