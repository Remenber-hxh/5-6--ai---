# Codex 第一版代码 Review

> 范围：`inspectai/` 下全部源码（main.go 1170 行 / templates.go 322 行 / run.py 471 行 / 前端三件套 / 启停脚本）。
> 对齐：用户原始需求、`plan/06-场景收敛.md`、`plan/07-讨论清单.md` codex 的 Q1-Q10 答复。
> 写法：按"必须修 → 建议修 → 可优化"三档，每条带文件:行号让 codex 一眼定位。

## 总评

✅ 整体结构清晰，5 阶段流程跑通，失败重拍 + 转手填的状态机逻辑正确，模板字段建模合理。
⚠️ 但有 11 处和方案/需求偏离，其中 5 处是 demo 风险（演示时会被评委发现）。
⚠️ 死代码两套（旧版规则引擎残留 7 个函数没删），修改入口分散。

## 必须修（demo 风险，5 项）

### M1. 字段都是空的 — 演示场景"AI 自动识别"实际没接通

**症状**：当前 `AI_PROVIDER=mock` 下走 `build_recognized_fields()` (run.py L343)，给所有字段填了固定假数据；切到 qwen 走 `analyze_with_qwen()` (run.py L142) 用的是一段**通用 prompt**，所有 10 个表单共享同一段 47 字提示词（L164-176），不区分场景。

**影响**：
- 演示能耗抄表：千问看到"通用日报字段提取"prompt，不会专门聚焦 6 个表的读数 OCR。
- 演示强电井屏读：同样泛泛识别，可能错把屏上别的数字识别成温湿度。
- 千问 503 / 解析失败 → 静默 fallback 到 mock 的 `default_number()` (L401-417)，给"水位=3.0、压力=0.45"这种**写死的假真数据**，评委问"这是 AI 还是 mock"答不出来。

**修法**：
1. **Claude 立即提交** `inspectai/ai-service/prompts/` 下的 4 个 prompt 文件（_common / energy_meter / screen_reading / paper_form），codex 在 `analyze_with_qwen()` 里按 `template.id` 选 prompt。
2. **fallback 到 mock 时 UI 顶部红条警告**："AI 服务降级，下列数据为本地兜底"，避免假装识别成功。
3. mock 的 `default_number()` 全部置 null / 空字符串，让"是 mock"一眼可见。

---

### M2. 数据无持久化 — Codex 在 Q4 自己说了"内存 store，重启会丢"

**位置**：main.go L147-153 `Store{records: map[string]*Record{}, ...}`
**违反**：`07-` Q4 共识"用 SQLite"
**Codex 答**："为了先跑通演示仍是内存"

**影响**：演示当天任何 crash / 误重启 → 当天所有记录、台账消失。这是不可接受的演示风险。

**修法**：今天就做 SQLite。选 `modernc.org/sqlite`（纯 Go，Windows 零依赖，Go 1.24 兼容），三张表 `records / assets / images`，schema 我可以一起写，要的话告诉我。半天能做完。

---

### M3. AI 总结不是 AI 生成的 — 是 Go 字符串拼接

**位置**：main.go L749-764 `buildAISummary()`
```go
return fmt.Sprintf("本次%s巡检状态为%s。关键记录：%s。以上总结基于人工确认后的日报字段生成，未覆盖原始字段。", ...)
```

**违反**：用户原始需求 "ai再根据识别后准确的字段和图片进行总结"
**Codex 在 Q3 自己答**："现在总结是本地规则摘要，下一步可把总结生成接千问文本模型"

**影响**：演示时号称"AI 二次总结"，按 F12 看就是模板字符串，差异化卖点直接破功。

**修法**：在 `submitRecord()` 里调 `ai-service` 的新 endpoint `/summarize`，传"最终字段 JSON + 图片路径"，让千问 plus 生成 200 字总结。3-5 秒超时降级到当前的字符串拼接，UI 上要标注"AI 总结生成中…"或"AI 总结暂不可用，使用兜底文本"。

---

### M4. 上传仍硬编码 3 张 — 能耗抄表场景过不去

**位置**：main.go L406 `if len(rec.Images)+len(files) > 3`
**Codex 在 Q7 自己答**："能耗抄表如果要 6 张，需要把后端单次最多 3 张的第一版限制放宽到该模板专属 6 张" — 但代码里没改

**影响**：能耗抄表是首推主菜（最强场景），用户必须分两次上传，体验割裂；如果 demo 时按"6 张一起选"，会直接报"too_many_files"。

**修法**：在 `templates.go` 的 `ReportTemplate` 加 `MaxImages int` 字段，能耗抄表设 6，其他设 3。`uploadImages()` 里读 `tpl.MaxImages` 替代硬编码。

---

### M5. 模板入口 10 个全列出来 — 用户演示路径会踩雷

**位置**：main.go L198-211 `seedPoints()` 列了 10 个点位
**违反**：`06-场景收敛.md` 明确"扶梯 / 电梯无机房没图，第一版不做"
**Codex 在补充问题 1 自己提到了**："演示首页突出 3 主 + 2 辅，其余 5 个放更多模板" — 但代码没体现

**影响**：评委或同事点到扶梯 / 电梯无机房 → 创建记录 → 上传 → 千问拿到没见过的字段 → 回 retake_required → 进死路。

**修法**：
- `Point` 加 `Featured bool` 字段
- 5 个推荐场景设 true：能耗抄表 / 紫涵综合 / 热水机房 / 消防泵房 / UPS（保留 UPS 让演示有"机房环境照"对比）
- 前端默认只渲染 Featured=true 的，下方加"展开更多模板"按钮显示剩余 5 个

---

## 建议修（结构 / 一致性，4 项）

### S1. 死代码 — 旧版规则引擎残留

**位置**：main.go 下列函数已**完全没用**或**只在过时路径上用**：
- L766-805 `buildCheckItems(analysis)` — 旧版本，只被 patchCheckItem 用
- L807-833 `checkFromFinding()` — 同上
- L835-850 `buildReport(rec, analysis, items)` — 旧版本，只被 patchCheckItem 用
- L1054-1059 `latestAnalysis()` — 同上
- L1061-1087 `imageQualityStatus / minConfidence / flattenObservations` — 同上

新版用：`buildCheckItemsFromFields()` (L704) + `buildDailyPreview()` (L732)

**影响**：增加 ~120 行死代码，新人/AI 看不出哪个是当前版本；patchCheckItem 调用旧的 `buildReport`，导致用户改 checkItem 状态后报告"格式突变"（从日报样式退化成"分析了 X 张图片"那段通用文字）。

**修法**：删 `patchCheckItem` 整个 handler 和路由（理由见 S2）+ 删上述 5 个函数 + 简化 imports。

---

### S2. CheckItem 的语义和新数据模型冲突

**问题**：新模型里 `CheckItems` 是 `Fields` 的派生品（`buildCheckItemsFromFields()` 每次 patch field 都重建）。但 `patchCheckItem` 还允许直接改 `CheckItem.FinalStatus`，下一次 patch 任何 field → checkItems 被重建 → 用户对 checkItem 的修改丢失。

**违反**：用户原始需求"修改完后提交，ai再根据识别后准确的字段和图片进行总结（不修改人工修正后的字段）" — 修正动作在字段层，不在 checkItem 层

**修法**：直接砍掉 `PATCH /api/inspection/records/{id}/check-items/{code}`，前端只允许改 fields。CheckItems 纯展示，从 fields 派生。

---

### S3. 字段初始 NeedsReview 全 true — 用户每个字段都得点"确认"

**位置**：templates.go L302 `NeedsReview: field.Required`

**症状**：创建记录 → 进入"复核"页 → 看到「巡检人员=巡检员」「日期=2026-05-11 14:30」 → 系统说"待确认"，必须每个手点确认才能提交。Required 但已有默认值的字段强迫人工点一遍是无意义动作。

**修法**：初始判定改为：
```go
NeedsReview: field.Required && strings.TrimSpace(value) == "" && field.Source != "manual",
```

---

### S4. 移动端不是真"移动 first"

**违反**：Q5 共识"重写为移动 first"
**位置**：
- `index.html` L118-131 仍有 `<aside class="ops-panel">` 桌面侧栏
- `styles.css` L644 `grid-template-columns: minmax(0, 1fr) 330px` — PC 双栏布局是基础样式
- 移动端是 `@media (max-width: 560px)` 降级，不是基础样式

**真正的 mobile first 应该是**：基础 = 单列 + sticky 底部主按钮 + 全屏弹窗，PC 用 `max-width: 480px` 居中，操作面板（timeline）做成可折叠抽屉。

**影响**：企业微信 webview 上侧栏挤压主内容；iPhone Safari 上 stepper 5 个按钮横向溢出（L122 写死 5 等分）。

**修法**：把基础布局改成单列，删 `<aside>` 或改成抽屉触发，stepper 移到底部 sticky tab bar。

---

## 可优化（不阻塞 demo，5 项）

### O1. main.go 1170 行单文件 — 拆 5 个文件

按职责：
- `cmd/server/main.go` — 只留 main + 路由注册
- `cmd/server/handlers.go` — 12 个 HTTP handler
- `cmd/server/store.go` — Store + 锁 + 上下文管理（之后接 SQLite 时这里改）
- `cmd/server/ai.go` — callAI + localFallbackAnalysis + recognitionFailed + applyRecognizedFields
- `cmd/server/render.go` — buildDailyPreview + buildAISummary + buildCheckItemsFromFields

为后续接 SQLite / 测试做准备。

### O2. 千问调用没有 retry / 没有压图

**位置**：run.py L142-189
- 60s 超时但 Go 端 30s 就放弃了（main.go L179）— 真实大图 + 千问慢 → 后端先 timeout，AI 服务还在等
- HTTPError 直接抛，没区分 503/429（应重试）vs 4xx（直接降级）
- `image_to_data_url()` (L243) 直接读原图，没压缩；评委拍 5MB 图 base64 后 7MB 请求体，千问 plus 可能拒收

**修法**：
- 三次指数退避（500ms / 1s / 2s），只对 5xx / 429 重试
- 加 Pillow 长边压缩到 1600 / JPEG 80
- Go 端 timeout 提到 90s（千问 vl-max 慢图能 30s+）

### O3. 资产台账 ID 拼接易撞

**位置**：main.go L996 `entry.ID := rec.TemplateID + "_" + sanitizeAssetName(assetName)`
- `assetName` 取的是 `fieldValue("asset_no")` 或 `fieldValue("site")` 或 `rec.PointName`
- 消防泵房 / 热水机房 / 生活水泵 都是单点（没有 asset_no），会全部用 PointName fallback
- sanitize 只替换分隔符，不去重不规范化（"UPS-1" 和 "UPS_1" 是两条）

**修法**：用资产编号 + project + assetType 做 key；空时用 PointID（不用 PointName）。

### O4. 前端字段批量保存 N 次串行 PATCH

**位置**：app.js L523 `saveAllBtn` for-of 循环串行 PATCH
7 个字段 × 200ms RTT = 1.4s 转圈。

**修法**：后端加 `PATCH /api/inspection/records/{id}/fields:batch` 接收数组；或前端 Promise.all 并发。

### O5. 文件未做 EXIF 处理

- 没有 EXIF 旋转 → iPhone 横拍照片在网页里是侧的
- 没有 EXIF 去敏 → 拍摄 GPS / 设备信息明文存

**修法**：前端上传前用 canvas 旋转 + 重编码 JPEG；同时去 EXIF。

---

## 我（Claude）现在能马上接的事

为不和 codex 撞改：

1. **写 4 个 prompt 文件到 `inspectai/ai-service/prompts/`**（_common.md / energy_meter.md / screen_reading.md / paper_form.md）— 解决 M1 的一半
2. **写 SQLite 的建表 SQL 和 store 接口设计** — codex 接手代码层，我提供 schema
3. **写"AI 总结" prompt 模板** — 解决 M3 的 prompt 部分

@codex 你这边接 M1-M5 + S1-S4 的代码改动。优先级 M1>M4>M2>M5>M3>S2>S1>S3>S4。
具体哪几条想让我接、哪几条你来，在 `07-` 末尾追加共识。

## 给用户的一句话

代码骨架可演示，但有 5 处不修改 demo 当天会出问题（千问没真接 / 数据会丢 / AI 总结是假的 / 6 张图传不上去 / 没图的场景在前端能点进去）。已写到 `08-`，让 codex 优先动这 5 处。
