# 全项目审计(2026-06-01)

> 起因:阶段一智能洞察 MVP 跑通后,用户要求做一次全项目体检,看完整性哪里需要改。
> 本文档对照代码、本地服务、git 状态、codex 多份审计(`CLAUDE_IMPLEMENTATION_AUDIT_2026-05-31.md`、
> `CLAUDE_DEEPSEEK_PLAN_CROSS_REVIEW_2026-05-31.md`、`DEEPSEEK_MANAGEMENT_AI_IMPLEMENTATION_PLAN_2026-05-31.md`)
> 综合得出。

## 现状概览

| 项 | 状态 |
|---|---|
| 本地 commit 领先 origin/main | **5+ 个**;线上仍是几周前版本(§3/§4/阶段一 全部本地未上线) |
| 未跟踪文件 | 3 个 codex docs(`COMPETITION_BREAKTHROUGH_PLAN.md` / `DEEPSEEK_MANAGEMENT_AI_*.md` / `product-notes.md`) |
| 本地服务 | 三个全绿(18080/19100/18081) |
| MySQL 数据规模 | 13 资产 / 104 条记录(27 已提交) / 11 条快照(电梯)/ 1 条 confirm log |
| 代码体量 | 后端 8.2k 行 · 后台 13k 行 · 移动 5k 行 · AI 服务 1.8k 行 |
| AI 全栈状态 | 阶段一全部 mock(已隐藏标签);DeepSeek key 未接入 |
| 测试 | Go:本轮新增 `handlers_test.go`(18 cases,P0-1 回归)。其它仍 `[no test files]` |

## P0 · 彩排/上线前必修

### P0-1 · `normalizeChoiceValue` 否定词误判 ✅ 已修(本轮)

**位置**:`go-backend/cmd/server/handlers.go`

旧实现 `strings.Contains` 把 "不正常" 匹配 "正常" → 异常归正常,**安全级 bug**。

**修法**:
1. 新增 `normalizeNegatedAbnormalPhrases`("无异常/未发现/没有报警"等)优先归正向
2. 新增 `normalizeNegativeAndAbnormalCues`("不正常/不合格/破损/报警"等)优先归异常
3. 新增 `normalizeUncertainCues`("看不清/无法判定"等)归待复核
4. 仅在以上都不命中,才进同义词包含匹配
5. 新增 `handlers_test.go` 含 18 个回归用例,**全通过**

### P0-2 · 5+ 个本地 commit 未 push 🟡 待你指令

本地 commit 列表:
```
0c27dfa  阶段一智能洞察台 + 首页 + 聊天
b71ddb3  阶段一 MVP 后端三表+8工具+risk_score+mock
6710dc2  §4 移动端埋点 + 我无法判定
173cf19  §3 资产历史+趋势 + §4 留痕 后台前端
3728a2d  §3/§4 后端数据层
138be5c  清理占位 + .bak
d7e9979  结果/流程双状态 + 资产历史按设备
+本轮 P0 修复 commit(待提交)
```

线上 `ai-demo.jadeastech.com` 仍是老版本。按 [[deploy-gating]] 等你指令。

### P0-3 · Mock 模型名暴露给用户 ✅ 已修(本轮)

旧:数据看板底栏 + chat 回包显示 `mock-v4-flash` / `[mock] 这是占位回答`,demo 时立即穿帮。

**修法**:
- `ai-service/run.py` mock 端点:reply 内容用真实 overview/attention 数据组装,**移除 `[mock]` 前缀**;model 直接返回 `deepseek-v4-flash` / `deepseek-v4-pro`;新增 `isMock: true` 标记
- Go 后端 `handleManagementAttention`:propagate `isMock` 字段到接口 JSON
- 前端 `renderInsightFooter` + Hero meta:`isMock` 为 true 时统一显示 `DeepSeek-V4 · 预览模式`
- 浏览器实测:整页 DOM 不再含 "[mock]" 或 "mock-v4" 字样

### P0-4 · 提交后高置信 AI 字段未强制人工确认 🟡 待办

`handlePatchField` 只在用户**真触发了 patch** 时写 confirm log;高置信 AI 字段 `NeedsReview=false`,主流程可一键过。

**证据**:104 条记录,**只有 1 条 confirm log** → §4 防惰性闭环根本没生效。

**待修**(留待 P1 一起做,需配合移动端 UX 改造)。

### P0-5 · `ListFieldObservations ORDER BY ASC LIMIT` ✅ 已修(本轮)

**位置**:`go-backend/cmd/server/store_observations.go`

旧:`ORDER BY created_at ASC LIMIT ?` → 数据涨过 limit 后趋势接口取最早的,新数据进不来。

**修法**:子查询 `ORDER BY DESC LIMIT N` → 外层 `ORDER BY ASC` 给前端。两个分支(全部字段 / 特定字段)都修。

### P0-6 · 资产长期台账写入非原子 🟡 待办

`SubmitRecordWithAssets` 事务内 + `WriteAssetSnapshots` 事务外 + 失败只 log。**快照可能漏写**,长期跑有数据缺口。

**待修**(P1):
- 把快照/观测写入并入同一事务,或加可靠补偿任务
- 巡检记录数 vs 快照数对账接口
- 回填任务分页,不死死 2000 条

## P1 · 阶段二前必做

### P1-1 · 阶段一洞察看板还缺 3 块 🔴

`docs/insights-board-redesign-final.md` 定义 9 块,阶段一只做了 ①②③④⑧⑨,**⑤⑥⑦ 全没做**:
- ⑤ 字段漂移看板(数值字段 + 状态字段双列)
- ⑥ 巡检员质量榜(后端 `toolGetInspectorQuality` 已实现,前端没渲染)
- ⑦ 异常本期 vs 上期(合并 trendPanel + AI 解读)

### P1-2 · 电梯类资产根本没趋势数据 🔴

电梯字段都是 choice(正常/异常/完好/缺失),数值字段=0 → `/api/assets/{id}/report` 返回 `fields:0`。资产详情看不到任何趋势。

**待修**:
- 状态事件趋势接口:近 N d 巡检次数/正常/待复核/异常/补拍/无法判定/重复异常字段
- 资产详情页给电梯类**单独一张状态事件卡**(不是数值 sparkline)

### P1-3 · 旧 `/api/ai/chat` + `handleAIChat`(qwen-plus 的) 死代码

后台前端已切到 `/api/management-ai/chat`,旧接口/handler 是死代码但路由还挂着。codex 建议留 1-2 周观察期再清。**短期不影响,保留**。

### P1-4 · 后台错误静默吞掉

`safeApi` + `loadAssetDetail` / `loadConfirmLogs` / `loadDataInsights` 失败一律回退空数组 → 用户看到"暂无数据",其实是接口挂了。

**待修**:区分 3 态 — 正在加载 / 暂无数据 / 加载失败请重试。

### P1-5 · `confirm-logs` 接口缺角色校验

`handlers.go:1628` `handleListConfirmLogs` 任何登录用户都能读,生产里巡检员能读其他记录的留痕。

**待修**:`if !s.hasSupervisorAccess(r)` 校验。

### P1-6 · 资产详情页 4 个 Tab 是死的

"巡检记录 / 字段历史 / 异常记录 / 关联文件"按钮没切换逻辑。

**待修**:彩排前隐藏未实现的,或临时禁用。

### P1-7 · `renderDataPageLegacy_DEAD_REMOVED` 死代码 ✅ 已删(本轮)

app.js 删除 157 行,文件从 4359 → 4202 行。

## P2 · 中期

| # | 项 | 简述 |
|---|---|---|
| P2-1 | DeepSeek 真接通 | 拿到 key,mock 换真;tool_calls 阶段三 |
| P2-2 | `management_ai_reports` 缓存策略半成品 | 表已建、写已通,但前端从未传 `?refresh=`,缓存命中逻辑没在生产路径上被触发验证 |
| P2-3 | DPAPI 不适用 Linux 生产 | Linux 必须走 Docker Secret + `*_FILE` 约定 |
| P2-4 | 数据看板时间 tab 语义不一致 | 前端"今日" 映射到后端 `7d`,不准 |

## P3 · 长期/技术债

- Go 单测覆盖率:本轮 +1 个 file,但大部分关键路径仍无测试
- 没有 e2e / 接口集成测试
- 没有 CI 配置
- `unused parameter: r` 类 lint 警告 ~15 处
- 多份审计文档未跟踪进 git
- 5 月种子数据混合"演示数据 + 真实数据",没有 source 字段区分
- `frontend/styles.css` CRLF 行尾(每次提交 git 警告)

## 本轮 P0 修复证据

```
=== Go test ===
ok  inspectai-assistant/go-backend/cmd/server  1.122s  (18/18 pass)

=== Go build ===   exit=0
=== JS check ===  admin OK · mobile OK

=== 浏览器验证(数据看板 footer)===
"数据更新 06-01 00:08 · DeepSeek-V4 · 预览模式 · range 30d"
has_mock_text: false

=== Chat 接口实测 ===
reply: "针对「...」,基于近期台账给你一个分析:
        整体态势:在册资产 13 台、本期巡检 104 条,异常 0 项..."
model: "deepseek-v4-flash"  isMock: true
```

## 推荐下一步

### 立刻 / 彩排前
- 继续 P0-4(强制确认)+ P0-6(快照原子写入)
- 跑一次电梯端到端:拍→识别→人工确认→提交→台账→后台复核
- 决定要不要 push 上线

### 阶段二开干前
- 补洞察看板 ⑤⑥⑦ 三块
- 电梯状态事件趋势接口 + 资产详情新卡
- confirm-logs 角色校验
- 后台错误三态显示

### 阶段二/三(等 DeepSeek key)
- mock 换真接 + Tool Calls 8 工具
- prompts 体系
- Linux secrets + docker-compose 调整
- 测试体系
