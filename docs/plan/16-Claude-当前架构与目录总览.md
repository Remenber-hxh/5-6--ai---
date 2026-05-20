# Claude 当前架构与目录总览

> 日期：2026-05-12
> 写给：之后接手维护的人 / codex 回看 / 演示前自己 review
> 替代地位：本文档为现行版本的真相来源；`inspectai/README.md` 写于 codex 第一版，部分信息（mem store / mock fallback 默认）已过时，请以本文档为准。

## 1. 三层架构

```
┌─────────────┐   静态文件 + JSON API     ┌──────────────┐   HTTP+JSON    ┌────────────────┐
│  浏览器     │ ───────────────────────► │  Go 后端     │ ─────────────► │  Python AI     │
│  (移动端)   │       :8080              │  (单二进制)  │   :9100        │  微服务        │
└─────────────┘                           └──────────────┘                 └────────┬───────┘
                                               │                                    │
                                               ▼                                    ▼
                                          SQLite 文件                          subprocess curl
                                          + uploads/                           ──► DashScope
                                          + tmp_classify/                       (qwen-vl-plus
                                                                                 / qwen-plus)
```

每层都是**单进程、本地起**，没有容器、没有外部依赖（除 DashScope）。

### 1.1 前端 `inspectai/frontend/`

- 纯 HTML+CSS+JS，无构建步骤，由 Go 后端直接 ServeFile。
- 移动端优先，max-width 480px，PC 浏览看到的是居中"手机模拟器"。
- 状态机：`camera → loading → classify → form → preview → ledger`。
- localStorage 持久化 `activeRecord`，刷新页面能恢复未完成的巡检。

### 1.2 Go 后端 `inspectai/go-backend/`

- Go 1.24 + 纯 Go SQLite 驱动 `modernc.org/sqlite v1.34.5`（**不要升 v1.49**，那个要 Go 1.25）。
- 单二进制 `server.exe`，监听 `:8080`，同时托管前端静态文件。
- 入口 `cmd/server/main.go`，路由全在 `handlers.go` 的 `router()` switch 里。
- 异步 AI 任务用 goroutine 跑（`runAnalysis`），通过 `ai_tasks` 表轮询状态。

### 1.3 Python AI 微服务 `inspectai/ai-service/`

- Python 3.13，`run.py` 单文件，`http.server` 监听 `:9100`。
- **不用 urllib 调 DashScope**——urllib 在本机 IPv6 上会卡 SSL 握手 45s。
  改用 `subprocess.run([curl, "-4", "--noproxy", "*", ...])` 调 OpenAI-compatible endpoint。
  `-4` 强制 IPv4；`--noproxy *` 绕开系统代理（Clash/Fiddler 会拦 dashscope）。
- prompts 在 `ai-service/prompts/` 目录，启动时一次性加载到内存，改 prompt 要重启 ai-service。

## 2. 端到端数据流

```
1. 拍照 / 从相册选 / 手动选模板
   POST /api/scene/classify   (multipart files=...)
   ── Go 暂存到 storage/tmp_classify/{tmpDirID}/
   ── 调 ai-service /classify ── 调 qwen-vl-plus 出 templateId

2. 用户确认模板，创建巡检记录
   POST /api/inspection/records  {templateId, inspector, tmpDir}
   ── adoptTmpImages: 把 tmpDir 里的图按 ID 移到 storage/uploads/{recordID}/
   ── 写 records 表

3. 启动 AI 字段识别（异步）
   POST /api/inspection/records/{id}/ai-tasks
   ── 立刻写 ai_tasks 表 status=queued
   ── goroutine runAnalysis: 调 /analyze ── 调 qwen-vl-plus
   ── 成功后 applyRecognizedFields 写回 records.fields
       (检查 source==human-* 跳过，不覆盖人工修改)
   ── 任务结束 status=succeeded/failed

4. 前端轮询拿结果
   GET /api/inspection/records/{id}/ai/latest

5. 人工逐项 PATCH
   PATCH /api/inspection/records/{id}/fields/{code}  {value}
   ── source 由 manual 变 human-edited，version+1

6. 提交日报
   POST /api/inspection/records/{id}/submit  + Idempotency-Key 头
   ── 校验：必填字段非空、所有 needsReview 已确认、状态非 processing/queued
   ── 已 submitted 直接返回旧记录（防重复 +1）
   ── 调 ai-service /summarize ── 调 qwen-plus 出 AI 总结+建议
   ── upsert assets 表

7. 看台账
   GET /api/assets
```

## 3. 数据持久化清单

| 位置 | 存什么 | 何时清 |
|---|---|---|
| `storage/inspectai.db`（+ -shm/-wal） | SQLite：records / assets / ai_tasks | 删则全清 |
| `storage/uploads/{recordID}/` | 已 adopt 的巡检图 | 跟着 record 删 |
| `storage/tmp_classify/{tmpDirID}/` | classify 阶段的临时图 | adopt 后由 adoptTmpImages 自动 RemoveAll |
| `storage/logs/` | go-build.log（仅构建日志） | 手工清 |
| `storage/*.pid` | 当前进程 ID | start-local.ps1 启动时覆盖 |
| `inspectai/.env` | 非密钥配置（DEMO_MODE / 模型名） | 手工 |
| `inspectai/.env.secure` | DPAPI 加密的 DASHSCOPE_API_KEY | `scripts/setup-key.ps1` 维护 |

## 4. 关键端口与健康

```
http://127.0.0.1:8080                   前端入口
http://127.0.0.1:8080/health            Go 健康（含 storeKind）
http://127.0.0.1:9100/health            Python 健康（含 hasKey / promptsLoaded / demoMode）
http://127.0.0.1:8080/api/...           业务接口
http://127.0.0.1:8080/storage/...       上传图片只读访问
```

## 5. 启动 / 停止 / 改密钥

```powershell
cd D:\5-6月 ai 大会\inspectai

# 启动（一次拉起 Python AI + Go backend，覆盖式）
.\scripts\start-local.ps1

# 停止
.\scripts\stop-local.ps1

# 写新 DashScope key（DPAPI 加密落 .env.secure，绑当前 Windows 用户）
.\scripts\setup-key.ps1
# 然后 start-local.ps1 启动时会自动解密注入 DASHSCOPE_API_KEY
```

## 6. 完整目录说明

> 整理时间：2026-05-12（详见 `_archive/InspectAI-Assistant/` 是 codex 第一版归档）

```
D:/5-6月 ai 大会/                根目录
├─ inspectai/                  ← 主程序，唯一在跑的代码
│  ├─ ai-service/
│  │  ├─ run.py                Python AI 微服务（/classify /analyze /summarize /health）
│  │  ├─ prompts/              场景提示词（.md，启动时加载，改完要重启）
│  │  └─ Dockerfile            云部署用，本地不用
│  ├─ frontend/
│  │  ├─ index.html            6 个 scene 容器 + retake modal + footer
│  │  ├─ app.js                状态机 + API 封装 + DOM 渲染
│  │  └─ styles.css            企业微信风格
│  ├─ go-backend/
│  │  ├─ cmd/server/
│  │  │  ├─ main.go            入口、Server 装配、路由挂载
│  │  │  ├─ handlers.go        所有 HTTP handler + adoptTmpImages
│  │  │  ├─ store.go           Store 接口 + MemStore + SQLiteStore
│  │  │  ├─ schema.sql         3 张表的 DDL（go:embed 进二进制）
│  │  │  ├─ types.go           Record / FieldValue / AssetEntry / AITask
│  │  │  ├─ templates.go       内置 5 个巡检模板 + 字段定义
│  │  │  ├─ ai_client.go       调 ai-service 的 HTTP 客户端
│  │  │  └─ util.go            ID 生成 / JSON 工具
│  │  ├─ go.mod                Go 1.24，注意不要让 `go get` 升级到 1.25
│  │  └─ Dockerfile
│  ├─ scripts/                 PowerShell 启停 + 密钥加密
│  ├─ storage/                 运行时数据（启动会自动建）
│  │  ├─ inspectai.db          SQLite 主库
│  │  ├─ uploads/              正式巡检图
│  │  ├─ tmp_classify/         classify 阶段临时图
│  │  ├─ logs/                 go-build.log 等
│  │  └─ *.pid                 进程 ID，stop-local.ps1 用
│  ├─ nginx/                   Nginx 反代示例（云部署用）
│  ├─ docker-compose.yml       容器编排示例（本地不用）
│  ├─ .env                     非密钥配置
│  ├─ .env.secure              DPAPI 加密密钥（gitignore）
│  └─ README.md                ⚠ 写于 codex 第一版，已过时（看本文档代替）
│
├─ docs/
│  ├─ plan/                    ← 你正在看的 14+ 篇方案与讨论
│  │  ├─ 01-08              codex 协作期：方案、字段、时间表、第一版 review
│  │  ├─ 09-13              架构补充：SQLite、密钥、台账、迁移
│  │  ├─ 14-15              Claude 接管期：审计 + 现场反馈
│  │  └─ 16 (本文)         当前架构总览
│  └─ legacy/                  4/28 三份早期 .md（VLM 接入说明等）
│
├─ samples/                    业务素材（非代码，AI 训练/测试输入）
│  ├─ templates/               11 个业务方提供的 Excel 巡检表模板
│  ├─ photos/                  汇报附件 5.6（巡检照片，多个子目录按场景分）
│  └─ misc/                    report_material.docx + tmp_signup_form.xlsx
│
└─ _archive/
   └─ InspectAI-Assistant/    codex 第一版完整快照（保留，不再使用）
```

## 7. 距 codex 第一版的关键变化

| 项 | codex 第一版 | 当前 |
|---|---|---|
| 数据持久化 | 内存 + state.json 拍快照 | SQLite (modernc.org/sqlite 纯 Go) |
| AI 调用通路 | urllib 直连 DashScope（IPv6 卡死） | subprocess curl `-4 --noproxy *` |
| 提交校验 | 只查 Idempotency-Key 存在 | 校验必填+needsReview+status，重复提交直接返回 |
| 幂等 | 重复点击会让 inspectionCount +1 | rec.Submitted 检查直接返回旧结果 |
| AI 失败兜底 | 永远返回 mock 字段（伪装识别成功） | DEMO_MODE 才 mock，正式模式返回 retake_required |
| 台账 upsert 错误 | `_ = ...` 静默吞掉 | 返回 500，前端能看到 |
| 图片 ID 解析 | 按第一个 `_` 截断（结果全是 `img`） | SplitN 取前 3 段拼回 `img_{ts}_{rand}` |
| 密钥存放 | 明文 .env | DPAPI 加密 .env.secure，绑当前 Windows 用户 |
| 前端首屏 | 只能拍照 | 拍照 + 从相册上传 + 手动选模板 |
| modal/footer 显示 | CSS 子类 `display:flex` 覆盖 `[hidden]`，retake modal 关不掉、footer 跨场景显示 | `[hidden] { display: none !important }` 兜底 |

## 8. 调试常用命令

```bash
# 健康
curl --noproxy "*" http://127.0.0.1:8080/health
curl --noproxy "*" http://127.0.0.1:9100/health

# 看当前所有巡检记录
curl --noproxy "*" http://127.0.0.1:8080/api/inspection/records | python -m json.tool

# 看资产台账
curl --noproxy "*" http://127.0.0.1:8080/api/assets

# 直连 DashScope 检查配额（替换 $KEY）
curl -4 --noproxy "*" -m 30 https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"qwen-plus","messages":[{"role":"user","content":"ping"}]}'
# 返回 AllocationQuota.FreeTierOnly = 免费额度耗尽，需要去控制台开付费
```

## 9. 5/13 demo 前的剩余工作（截止本文档时）

- 浏览器真机回路测试（拍照→识别→改字段→提交→台账）
- 确认台账编辑暂不需求（用户说"能看就够"）
- 失败重拍 3 次后 manual 流程的实测
- DashScope 付费额度确认稳定（目前 vision + text 都用新 key）

—— end ——
