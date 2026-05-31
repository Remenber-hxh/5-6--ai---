> **⚠ SUPERSEDED · 2026-05-31**
>
> 这份是初版,其中"新起 analytics-ai 微服务/独立公网端口""后端全聚合塞 prompt""AI 主观挑重点关注"
> 等几条选错了。最终对齐结果见:
>
> - **`insights-board-redesign-final.md`** ← 走这份(合并 codex 方案 + 用户对齐结论)
> - `DEEPSEEK_MANAGEMENT_AI_IMPLEMENTATION_PLAN_2026-05-31.md`(codex 原案,API/Tool Calls/缓存表全采纳)
>
> 本文档保留作背景,不再施工依据。

---

# 看板智能洞察 + DeepSeek 独立 AI 微服务 — 实施方案(Claude 初版,已 superseded)

> 起因:2026-05-31 与领导讨论后新增需求 ——
> 1. 看板展示历史台账聚合数据
> 2. AI 基于完整历史挑出"最近需重点关注的巡检"
> 3. 管理员从首页可问 AI 关于全部后台数据 + 异常对比
> 4. 这一块 AI **独立成一个 API**,模型用 **DeepSeek v4**

## 1. 需求拆解

| 子功能 | 用户 | 在哪 | 数据 |
|---|---|---|---|
| 看板·今日重点关注 | 主管 | 后台首页 dashboard | AI 看完整历史台账(snapshots / observations / confirm-logs),挑 Top N |
| 首页问 AI | 主管 | 首页聊天框 | 自然语言问后台数据(异常/趋势/覆盖率…),AI 拿历史做对比回答 |
| AI 独立 API | — | 新建 Python 微服务 | DeepSeek v4,跟现有 ai-service(qwen 视觉+总结)解耦 |

### 现状对照
- 既有 ai-service(Python,19100):`/classify` `/analyze` `/summarize` `/chat`,模型走 qwen-vl + qwen-plus(dashscope)
- 现有后台聊天框已经在用 ai-service `/chat`,但上下文很窄(只塞 platform snapshot 几个数字)
- 新需求 = **换模型(DeepSeek)+ 扩上下文(全历史聚合)+ 看板焦点新功能**

### "AI 独立成一个 API"为什么是对的
- 职责不同:ai-service = 看图说话(单条记录场景),analytics-ai = 看表分析(全量聚合)
- 模型不同:qwen 视觉 vs DeepSeek 推理
- prompt 不同:字段提取/重拍判断 vs 风险综合判定/对话引证
- 出问题独立降级,看板坏不影响巡检主流程

## 2. 推荐架构

```
┌────────────────────────────────────────────────────────┐
│ admin-frontend (18081)                                 │
│  · 看板「今日重点关注」卡片(focus board)                  │
│  · 首页聊天框升级版(带后端历史数据上下文)                 │
└──────┬─────────────────────────────────────────────────┘
       │ POST /api/insights/focus     (取焦点清单,可缓存)
       │ POST /api/insights/chat       (问答)
       │ GET  /api/insights/snapshot   (看板数字直接读)
       ↓
┌────────────────────────────────────────────────────────┐
│ go-backend (18080)                                     │
│  · aggregateInsights() 跨 assets/snapshots/observations│
│      /confirm-logs/change-requests 聚合                │
│  · 计算字段漂移、覆盖率、复核惰性指标                      │
│  · 调 analytics-ai,带缓存(默认 600s)                   │
└──────┬─────────────────────────────────────────────────┘
       │ POST /focus   POST /chat
       ↓
┌────────────────────────────────────────────────────────┐
│ analytics-ai (19200) ★新建                              │
│  · 模型 DeepSeek v4(OpenAI 兼容)                       │
│  · prompt 库独立:focus_curator / chat_analyst         │
│  · 不直连 DB(服务边界单向)                              │
└────────────────────────────────────────────────────────┘
```

### 关键架构决策

1. **后端做聚合,不让 analytics-ai 直连 MySQL**:服务依赖单向,避免循环;且后端聚合可以做缓存
2. **AI 服务无状态**:横向扩缩、出问题独立降级,不影响巡检主流程
3. **传给 AI 的不是原始行,是摘要 + TopN + 阈值偏离项**:token 省、准确率高、可解释

## 3. 各部分具体改动

### A. `analytics-ai/` 新 Python 服务(端口 19200)

复用 `ai-service/run.py` 脚手架(curl+http+JSON+prompt 加载),最小改两处:
- 调用 baseurl 改 `https://api.deepseek.com`(实际值在 .env)
- 模型名走 env(`ANALYTICS_MODEL=deepseek-chat`)

**新增文件**:
```
analytics-ai/
  run.py                # 复制 ai-service/run.py 改 baseurl + model
  requirements.txt
  Dockerfile
  prompts/
    _common.md          # 输出 JSON 规范、数据语义说明
    focus_curator.md    # 焦点清单 prompt(输入聚合数据,输出 TopN)
    chat_analyst.md     # 主管问答 prompt(含 context 字段语义)
```

**两个端点**:

| 端点 | 输入 | 输出 |
|---|---|---|
| POST /focus | `{ snapshot, threshold, recent }` | `{ items:[{assetId,assetName,reason,priority,basis}] }` |
| POST /chat | `{ message, history, context }` | `{ reply, citations }` |
| GET /health | — | `{ status, hasKey, model }` |

### B. `go-backend` 数据聚合 + 中转 + 缓存

**新文件** `go-backend/cmd/server/insights.go`(同 package):

```go
// AI 友好快照 —— 不传原始行,只传摘要/TopN/偏离项
type InsightsSnapshot struct {
    Generated       time.Time
    AssetCount      AssetCountSummary
    RecentSubmits7d int                    // 近 7d 已提交记录数
    Coverage        CoverageStat           // 计划 vs 实巡覆盖率
    AbnormalRecent  []AbnormalEntry        // 近 30d 异常 Top N
    DriftFields     []FieldDriftEntry      // 数值字段偏离 Top N(从 observations 算)
    OpenChangeReqs  int                    // 待审批数
    ConfirmLazyRate float64                // 复核留痕里 viewedPhoto=false 占比
}

type FocusItem struct {
    AssetID, AssetName string
    Priority           string  // high/medium/low
    Reason             string  // 1 句话
    Basis              string  // 数字依据
    LastRecordID       string
}

type InsightsCache struct {
    Focus     []FocusItem
    Snapshot  *InsightsSnapshot
    FetchedAt time.Time
}
```

**新路由**:
- `GET /api/insights/snapshot` → 直接返回聚合数字给看板
- `POST /api/insights/focus?refresh=1` → 默认 600s 缓存,调 analytics-ai
- `POST /api/insights/chat` → 单轮/多轮聊天,聚合+history+message 一起转 analytics-ai

**关键函数**:
- `aggregateInsights()` — 跨表汇总
- `computeFieldDrift()` — 用 §3 `ListFieldObservations` 算近 30d 变化率 > 10% 的字段
- `computeConfirmLazyRate()` — 用 §4 `field_confirm_logs` 算"未看图就确认"比率(主管最爱看的防惰性数字)

**调用 analytics-ai** 加一个 `AnalyticsClient`,跟现有 `AIClient` 同款 timeout/retry,但 base URL/超时单独配。

### C. `admin-frontend` 看板 + 聊天升级

#### C.1 dashboard 首页新增"今日重点关注"卡片

```html
<section class="focus-board">
  <div class="focus-head">
    <h2>今日重点关注 <small>AI 基于历史台账综合判定</small></h2>
    <span class="focus-meta">数据更新于 [fetchedAt] · <button data-action="refresh-focus">重新分析</button></span>
  </div>
  <div class="focus-list">
    [Top 5 卡片:资产名 / 原因 / 优先级徽章 / 数字依据 / "查看资产 →" 链接]
  </div>
</section>
```

数据走 `loadInsightsFocus()` — 默认读缓存,点"重新分析"才打 DeepSeek。

#### C.2 聊天框升级

- 调用 `/api/ai-chat` 换成 `/api/insights/chat`
- 输入框旁加快捷气泡:"近一周异常对比"、"哪些设备值得复巡"、"复核率怎么样"
- 后端 context 更丰富(完整 snapshot),回答能引用真实数字

#### C.3 看板顶部数字加 3 个新指标

利用 `/api/insights/snapshot` 拿现成的数字:
- **近 7d 提交数**(已有的"今日异常"旁边)
- **复核惰性指标**(viewedPhoto=false 比率,新)
- **字段漂移项数**(超阈值数值字段数,新)

### D. 配置 + 部署

**`.env`** 加(DPAPI 加密,符合 [[feedback_security]]):
```env
DEEPSEEK_API_KEY=...                           # 加密落盘
DEEPSEEK_BASE_URL=https://api.deepseek.com
ANALYTICS_MODEL=deepseek-chat                   # ← 等确认 v4 model id
ANALYTICS_SERVICE_URL=http://127.0.0.1:19200
ANALYTICS_SERVICE_ADDR=127.0.0.1
ANALYTICS_SERVICE_PORT=19200
```

**`scripts/start-local.ps1`** 加启动 analytics-ai 块(复制现有 ai-service 启动段)。
**`docker-compose.yml`** + **`docker-compose.prod.yml`** 加 analytics-ai 服务块。
**`scripts/setup-key.ps1`** 加 DPAPI 加密 DeepSeek key 的入口。

## 4. 待决策点

| # | 决策 | 倾向 |
|---|---|---|
| ① | DeepSeek v4 具体 model id —— 是 `deepseek-chat`、`deepseek-reasoner`、还是有内测 v4 模型? | 实施前查官方 docs 确认 |
| ② | 焦点分析缓存策略 | **5-10 min 缓存 + 手动"重新分析"**(DeepSeek 不便宜) |
| ③ | 数据聚合粒度 | **后端先算,只塞摘要+TopN+阈值偏离项**(token 省+准确率高) |
| ④ | 聊天流式 vs 一次性 | **一次性**(首期),后面再升级 SSE |
| ⑤ | 看板焦点卡片摆位 | **独占一行**,跟现有"风险洞察"区分开 |
| ⑥ | 现有 `/api/ai-chat` 处置 | **保留** 1-2 周做对比,新功能默认走 insights/chat |
| ⑦ | 是否给 AI 看复核留痕(field_confirm_logs)? | **要**(主管视角防惰性),但前端不暴露原始数据 |

## 5. 风险

1. **DeepSeek API key 也得 DPAPI 加密**,跟 DASHSCOPE 同等待遇
2. **数据量小(现 27 条提交)**,AI 给的"重点关注"可能空泛 —— demo 前需要造一些有梯度的种子数据(正常多、几条明显异常、几条数值漂移)
3. **`field_confirm_logs.operator` 现在可能漏个体差异**(都是 admin 一个人测的),"防惰性"故事要讲得圆,得有几个不同 operator 的留痕
4. **DeepSeek 国内访问稳不稳** —— dashscope 都得 `curl --noproxy "*"` 绕代理,部署前要在演示机测一次直连
5. **prompt 注入** —— 主管聊天框可输入任意文本,prompt 里要加边界("只回答与本系统数据相关的问题")
6. **MySQL 已有数据存量小**,聚合可以全量;以后涨到几万条要加分页/分片或建物化视图

## 6. 推荐执行顺序

每步本地 commit,远程等指令 push。

1. **建 `analytics-ai/` 骨架 + 健康检查**(mock 返回)→ 端到端跑通
2. **填 prompt + 接 DeepSeek**(dev 环境)→ 验证模型回答靠谱
3. **后端 `insights.go` 聚合 + 路由 + 缓存**
4. **后台前端看板"今日重点关注"**卡片
5. **聊天框切换到新接口** + 快捷气泡
6. **本地完整走一遍** + 截图 + 等审

## 7. 关键文件清单(改动总览)

| 类型 | 文件 | 操作 |
|---|---|---|
| 新服务 | `analytics-ai/run.py` `requirements.txt` `Dockerfile` | 新建 |
| 新服务 | `analytics-ai/prompts/{_common,focus_curator,chat_analyst}.md` | 新建 |
| 后端 | `go-backend/cmd/server/insights.go` | 新建(聚合 + 路由 + 缓存 + AnalyticsClient) |
| 后端 | `go-backend/cmd/server/handlers.go` | 改:router 加 `/api/insights/*` |
| 后端 | `go-backend/cmd/server/main.go` | 改:读 ANALYTICS_SERVICE_URL,初始化 AnalyticsClient |
| 后端 | `go-backend/cmd/server/store.go` | 不动(用现有 ListFieldObservations/ListFieldConfirmLogs) |
| 后台前端 | `admin-frontend/app.js` | 改:`loadInsightsFocus` `loadInsightsSnapshot` + dashboard 渲染 + 聊天切接口 |
| 后台前端 | `admin-frontend/styles.css` | 改:`.focus-board / .focus-card` 样式 |
| 配置 | `.env.example` `.env` | 加 DeepSeek 相关变量 |
| 部署 | `scripts/start-local.ps1` `docker-compose.{,prod}.yml` | 加 analytics-ai 启动块 |
| 部署 | `scripts/setup-key.ps1` | 加 DPAPI 加密 DeepSeek key 入口 |
