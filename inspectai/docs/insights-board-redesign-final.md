# 数据看板重设计 + DeepSeek 管理 AI — 最终合并方案

> 状态:**已对齐**,2026-05-31
> 取代:`docs/insights-deepseek-plan.md`(我的初版,架构选错)
> 参照:`docs/DEEPSEEK_MANAGEMENT_AI_IMPLEMENTATION_PLAN_2026-05-31.md`(codex 版,API/Tool Calls/风险分公式/缓存表全采纳)
>
> 这份只记**两份方案合成的最终决策 + 数据看板新页 wireframe + 文件级落地清单**,
> 共享的接口/Tool Calls/风险分公式/缓存表/权限脱敏请直接看 codex 那份,不再复述。

## 0. 跟领导对齐的核心定位

**数据看板** = 资产数据 + 时间维度状态可视化 + AI 洞察解读(不是聊天)
**首页 dashboard** = AI 聊天主舞台 + 现有 hero/quick tiles + Top 3 重点关注 mini 引流

两条线职责清晰:
- 数据看板:**主管来看数据**,AI 在旁边把数据"翻译"成判断和建议
- 首页:**主管来问 AI**,问出来的答案能引用看板里的数字和资产

## 1. 跟初版方案的差异(只记 delta)

| 维度 | 我初版 | codex | **最终** |
|---|---|---|---|
| AI 微服务部署 | 新起 analytics-ai(19200) | 复用现 ai-service 加 `/management/*` 内部路由 | **走 codex** —— 不开新公网端口 |
| AI 拿数据 | 后端全聚合塞 prompt | DeepSeek Tool Calls + 后端白名单工具 | **走 codex** —— Tool Calls,8 个白名单工具 |
| 重点关注谁挑 | AI 主观挑 | 后端按可解释公式打分,前 20 给 AI 写摘要 | **走 codex** —— risk_score 在后端算,AI 挂了能用规则版降级 |
| 缓存策略 | 进程内 600s | `management_ai_reports` 表 + 30 min + 异常触发刷 | **走 codex** —— DB 持久化,重启不丢 |
| 重点关注摆位 | 首页 dashboard | 首页 dashboard | **改放数据看板**(主舞台);首页放 Top 3 mini 引流 |
| AI 聊天框 | 想搬数据看板 | 首页 | **首页**(用户拍板) |
| 数据看板"时间维度状态" | 没提 | 提了状态事件趋势 | **必做:设备状态热力图** —— 资产 × 时间 × 状态色块,用户的核心诉求 |
| 时间颗粒度 tabs(周/月/季/年) | 重新设计 | 没提 | **保留现有**,不动 |
| 现有 KPI / 环图 / 资产类型 | 删 | 没提 | **降级**到辅助区,不删 |

## 2. 新「数据看板 = 智能洞察台」页布局

```
┌──────────────────────────────────────────────────────────┐
│ ① HERO                                                    │
│   智能洞察台                                                │
│   AI 综合判定 · DeepSeek-V4 · 数据更新 10:32  [重新分析]   │
│   [周][月][季][年]  [项目筛选 ▼]                            │
│                                                          │
│   AI 全局摘要(1-2 段话):                                  │
│   "本月 13 台资产共完成 27 次巡检,异常 1 项已闭环;         │
│    无机房电梯 HYZX-WJ-DT01 出现 2 次按钮面板待复核,       │
│    建议下次重点补拍。"                                       │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ② 风险全景 4 KPI(AI 综合,不是裸数字)                       │
│  风险指数 72 / 重点资产 5 / 字段漂移 2 / 复核惰性 18%        │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ③ 今日重点关注 — Top 5(主舞台之一)                         │
│  AI 综合判定 + risk_score + 原因 + 依据 + 建议 + 跳转       │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ④ 设备状态时间轴(用户强调的"时间维度直观看")★关键           │
│                                                          │
│         5/1  5/8  5/15 5/22 5/29(按 tab 切粒度)         │
│  HYZX-WJ-DT01 ■■■■■■■■■■■■■■■■▲■■■■■■■■ ← 一行 = 一台资产 │
│  配电箱        ■■■■■■■■■■■■■■■■■■■■■■■■■                │
│  Z1能耗表      ■■■■■■■■■■■▲■■■■■■■■■■■■                │
│  ...                                                     │
│                                                          │
│  色块:■绿正常 / ▲黄待复核 / ●红异常 / □空白未巡             │
│  hover 显示具体记录跳转;点击跳资产详情                       │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ⑤ 字段漂移看板                                              │
│  ┌─────────────────────────┬───────────────────────────┐│
│  │ 数值字段(电表/温度等)    │ 状态字段(电梯/配电等)       ││
│  │ 列表 + sparkline +       │ 计数 + 重复异常字段警示     ││
│  │ 变化率徽章(超阈红)      │ 例:按钮面板待复核 2 次/30d ││
│  └─────────────────────────┴───────────────────────────┘│
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ⑥ 巡检员质量榜(§4 防惰性主管视角)                         │
│  张管理员  补拍 0 · 无判 0 · 未看图确认 3 · 快速确认 2      │
│  张二      ...                                            │
│  ...                                                     │
│  附 AI 一句话评语                                          │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ⑦ 异常本期 vs 上期(合并 trendPanel)                       │
│  折线 + 同比 + AI 一句话解读                                │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ⑧ 辅助区(降级保底:旧 KPI / 准确率 / 闭环 / 类型分布)       │
│  资产总数 / 正常 / 异常 / 巡检记录                          │
│  AI 准确率环 + 异常闭环率环                                  │
│  资产类型 Top6 分布                                          │
└──────────────────────────────────────────────────────────┘

┌──────────────────────────────────────────────────────────┐
│ ⑨ 底栏元信息                                                │
│  数据更新 10:32 · 模型 deepseek-v4-pro · prompt v1.0      │
│  报告生成 1.8s · 缓存命中 · 上次刷新 30 分钟前               │
└──────────────────────────────────────────────────────────┘
```

## 3. 首页 dashboard 改动(轻)

保留现有 hero / quick tiles / risk insights / 今日任务 等结构。

**改动**:
- AI 聊天框:模型从 qwen-plus 换 deepseek-v4-flash,接口从 `/api/ai/chat` 换 `/api/management-ai/chat`,context 从几个聚合数字升级成 codex tool calls 全后端
- 快捷气泡:本周异常对比 / 哪些设备值得复巡 / 复核率怎么样 / 今天该优先处理什么
- 新加 **"重点关注 Top 3" mini 卡** + "去洞察台看全部 →" 跳转链接
- AI 标签从 "Qwen Plus · 实时" 改成 "DeepSeek-V4 · 台账分析"

## 4. 后端 / AI 服务结构(沿用 codex)

```
浏览器
  ↓ /api/management-ai/{attention | chat | reports/refresh | reports/latest}
go-backend
  ├─ 白名单工具层(8 个,不暴露 SQL)
  ├─ risk_score 计算(可解释,降级保底)
  ├─ 缓存读 management_ai_reports 表
  └─ ↓ /management/{analyze | chat}
ai-service(现有容器,加内部路由)
  ↓ DeepSeek API
DeepSeek (v4-pro 报告 / v4-flash 实时)
```

接口设计、Tool Calls 8 个工具、风险评分公式、缓存表 schema、prompt 边界约束、权限脱敏 —— **完全照 codex 那份**,这里不重复。

## 5. 文件级执行清单

### 5.1 后端
| 文件 | 操作 | 说明 |
|---|---|---|
| `go-backend/cmd/server/management_ai.go` | 新建 | risk_score 计算 + 8 个白名单工具实现 + AnalyticsClient |
| `go-backend/cmd/server/handlers.go` | 改 | router 加 `/api/management-ai/*` 路由 |
| `go-backend/cmd/server/main.go` | 改 | 读 DEEPSEEK_* env,初始化 AnalyticsClient |
| `go-backend/cmd/server/schema.sql` + `schema_mysql.sql` | 改 | 加 `management_ai_reports` 表(双 schema) |
| `go-backend/cmd/server/store.go` + 新文件 | 改/新 | 加 Store 接口方法:`SaveManagementAIReport / GetLatestManagementAIReport / DeleteExpiredManagementAIReports` |
| `go-backend/cmd/server/types.go` | 改 | 加 `ManagementAIReport` struct + 风险分相关 struct |

### 5.2 AI 服务(复用 ai-service 容器)
| 文件 | 操作 | 说明 |
|---|---|---|
| `ai-service/run.py` | 改 | 新增 `/management/analyze` `/management/chat` 路由;接 DeepSeek base url + model env |
| `ai-service/prompts/management_focus_curator.md` | 新建 | 焦点报告 system prompt + tool 描述 |
| `ai-service/prompts/management_chat_analyst.md` | 新建 | 主管问答 system prompt(回答必须 evidence) |
| `ai-service/prompts/_management_tools.md` | 新建 | 8 个工具的功能说明给模型读 |
| `ai-service/requirements.txt` | 可能改 | 看是否需要 deepseek SDK(用 curl 调 OpenAI 兼容也行) |

### 5.3 后台前端
| 文件 | 操作 | 说明 |
|---|---|---|
| `admin-frontend/app.js` `renderDataPage` | **完全重写** | 9 块新结构;旧组件函数(`trendPanel` / 环图 / 类型分布)降级为辅助区子组件复用 |
| `admin-frontend/app.js` `renderDashboard` | 小改 | AI 聊天升级 + Top 3 mini 卡 + 跳转链接 |
| `admin-frontend/app.js` | 新加 | `loadInsightsAttention()` `loadInsightsSnapshot()` `renderStatusHeatmap()` `renderFieldDriftBoard()` `renderInspectorQualityBoard()` |
| `admin-frontend/app.js` `state` | 改 | 加 `dataInsights: {}` `dashboardInsights: {}` 缓存 |
| `admin-frontend/styles.css` | 加 | `.insight-hero / .risk-kpi / .focus-board / .status-heatmap / .field-drift / .inspector-quality / .insight-aux` 等模块 |

### 5.4 部署 / 配置
| 文件 | 操作 | 说明 |
|---|---|---|
| `.env` / `.env.example` | 改 | `DEEPSEEK_API_KEY / BASE_URL / REPORT_MODEL / CHAT_MODEL / TIMEOUT` |
| `scripts/start-local.ps1` | 不动 | 复用 ai-service 启动,无需新服务 |
| `scripts/setup-key.ps1` | 改 | 加 DPAPI 加密 DeepSeek key 入口 |
| `docker-compose.yml` + `prod.yml` | 改 | ai-service 环境注入 DEEPSEEK_*;secret file 挂载 |

## 6. 实施顺序(沿用 codex 三阶段,稍调)

### 第一阶段:最小可用(MVP)
1. 后端 8 个白名单工具实现 + risk_score 计算函数
2. `management_ai_reports` 表 + Store 方法
3. ai-service 加 `/management/chat`,先用 mock 返回(不打 DeepSeek)
4. Go 后端加 `/api/management-ai/attention`(只走 risk_score,不调 AI)+ `/chat`(转 ai-service)
5. 数据看板 hero + Top 5 重点关注 + 设备状态热力图 + 辅助区降级 —— **就这四块**先撑起来,字段漂移 / 巡检员榜 / 异常对比 留阶段二
6. 首页 AI 聊天框接口切换 + Top 3 mini 卡

### 第二阶段:接 DeepSeek + 完整看板
1. ai-service 把 mock 换成真实 DeepSeek-V4 调用(v4-pro 报告 / v4-flash 聊天)
2. Tool Calls 8 个全接通,白名单从 4 扩到全部
3. 数据看板补字段漂移 / 巡检员榜 / 异常对比三块
4. 缓存策略上线:30 min 刷 + 异常提交触发刷 + 手动刷新

### 第三阶段:生产化
1. 权限校验(巡检员不能用全局 AI)
2. 脱敏过滤(原图/手机/密钥不进 AI)
3. 降级路径全打通:DeepSeek 挂了首页和看板仍展示 risk_score 规则版列表
4. operation_logs 记录所有 AI 工具调用
5. 端到端回归测试 + 演示数据补齐(造几条有梯度的种子记录给 AI 有内容可分析)

## 7. 验收标准(沿用 codex §15)

略,直接看 codex 那份。

## 8. 关键风险

1. **数据量小** —— 当前 27 条提交,AI 报告可能空洞。**演示前补一批有梯度种子数据**(正常多、几条明显异常、几条数值漂移、几个不同 operator 的留痕)。
2. **DeepSeek 国内访问稳定性** —— 演示机部署前测一次直连;走 curl `--noproxy "*"` 同 dashscope 套路。
3. **状态热力图密度** —— 13 台 × 30 天 = 390 格,可视化要紧凑;选周/月/年时按周聚合或按月聚合,不是死板的"一格一天"。
4. **Tool Calls 第一阶段不上**,只用规则版 risk_score —— 这是策略选择,要明确告诉用户"第二阶段才上 AI 全功能"。
5. **prompt 注入** —— 主管聊天框 system prompt 必须含"只回答与本系统数据相关的问题,不执行任何修改类操作"。

---

**实施触发条件**:
- DeepSeek API key 拿到 + DPAPI 加密落盘
- 演示数据梯度补齐
- 上面方案 user 最终签字
- push/部署节奏 user 给指令(参 [[deploy-gating]])
