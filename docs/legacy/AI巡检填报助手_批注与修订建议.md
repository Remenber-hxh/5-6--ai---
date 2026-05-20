# AI巡检填报助手 - 批注与修订建议

本文件不修改原立项文档，仅汇总技术实现层面的批注与修订建议，供动手前对齐。

---

## 一、按优先级速览

### P0 - 动手前必须确认/修改

| 章节 | 议题 | 动作 |
| --- | --- | --- |
| §2 | 登录、离线、EXIF 元数据 | 明确做/不做，最小做 JWT + 单角色 |
| §3.3 / §6.4 | 图片预处理 + 多图并发 + 对象存储 | 前端 HEIC→JPEG、EXIF 旋转、长边 1600px 压缩；后端接 MinIO；前端并发≤3 |
| §4.1 | AI vs 规则引擎边界 | 选定方案：AI 只出客观事实，规则引擎判异常 |
| §6.5 / §6.6 | 异步任务状态机 | 重构：用 taskId 查询，明确状态/进度/失败/取消 |
| §6.8 | 提交幂等 | 加 `Idempotency-Key`，去重 24h |
| §8.2 | 模板覆盖能力 | 两层结构：point_type 默认 + point_id override |
| §9 | 模型接入 | 三个 Provider 拆分、prompt caching、结构化输出、MockProvider、隐私策略 |

### P1 - 第一版上线前要做

| 章节 | 议题 | 动作 |
| --- | --- | --- |
| §5.1 | 输出 schema 补全 | 加 schemaVersion / model / tokenUsage / partialFailure / processedAt |
| §6.7 | 修正接口 | 批量接口 + 乐观锁 + source 由后端判 |
| §8.6 / §8.7 | 关系表 + 索引 | source_image_ids 改多对多表；review_status 拆 4 态；revision_group_id |
| §9 | 稳定性与成本 | 30s 超时 + 1 次重试、速率限制、单日预算上限 |

### P2 - 二版考虑

- 离线模式真机适配
- 图片 hash 去重，命中复用分析结果
- 演示模式开关 + 录屏兜底

---

## 二、详细批注（按章节）

### §2 第一版功能边界 — 三项必须明确

立项文档列了"做"与"不做"，但以下三项第一版必须明确表态：

1. **登录与权限**：现在所有接口都没有 `Authorization` 概念。`user_id` 字段无来源。最小做：JWT + 单角色（巡检员）。SSO 可放到二版。
2. **离线/弱网**：机房常无信号。要不要支持"先拍照本地暂存，回办公区再统一上传分析"？如果不做，文档要明确写"必须在线巡检"，否则演示时一定被问。
3. **照片元数据校验**：要不要校验 EXIF 的 GPS/拍摄时间，防止用历史照片应付巡检？第一版可只"记录不拒绝"，但表结构要先留位置（建议 `inspection_image` 加 `taken_at`、`gps_lat`、`gps_lng`、`exif_json`）。

### §3.3 照片上传页 — 工程坑点

1. **iPhone HEIC**：直接上传会被多数视觉模型拒绝。前端用 `heic2any` / 浏览器 `<input>` 转 JPEG，后端用 `pillow-heif` 兜底。
2. **EXIF 旋转**：手机竖拍照片在很多浏览器/模型里会"躺着"识别。上传前必须按 EXIF 旋转后重新编码。
3. **客户端压缩**：原图常 8–12MB，主流视觉模型最佳输入边长 1024–1568 px。前端压到长边 1600 px / JPEG q=85 ≈ 300KB。原图与"用于分析的图"路径分开（`storage_path` vs `analyze_path`）。
4. **图片类型自动判断**：文档既写"AI 自动判断"又在 §6.4 上传时要求 `imageType`。建议：上传时未选填存 `unknown`，由 §6.5 分析阶段判定后回写。

### §4.1 AI 职责 — 与规则引擎的边界冲突

文档同时写了"AI 生成检查项建议"和（§9）"判断规则后端配置，不完全交给模型"，两句话有冲突。第一版必须选一个：

**推荐方案（更稳）**：AI 只做客观事实层。

- AI 输出：`observations`、`extractedFields`、`detectedScene`、置信度。
- 后端读 `inspection_template_item.threshold_json`，对 `extractedFields` 做规则判定（如湿度 53% 是否在 30–70%），生成 `statusSuggestion`。
- 好处：阈值调整不改 prompt、规则可解释、单元测试好写、AI 跑飞影响面可控。
- 例外：纯感官类（"环境是否整洁"）让 AI 直接给建议，但用 `judge_type ∈ {rule | vision | hybrid | manual_only}` 显式区分。

如坚持让 AI 直接给 `statusSuggestion`，则 §8.2 的 `threshold_json` 要么进 prompt、要么删掉，不能两边各留一份。

### §5.1 总体返回结构 — schema 补全

建议补字段，便于排错与扩展：

```jsonc
{
  "schemaVersion": "1.0",
  "model": { "name": "...", "version": "...", "provider": "..." },
  "tokenUsage": { "input": 0, "output": 0, "imageCount": 0 },
  "partialFailure": false,
  "failedImageIds": [],
  "processedAt": "2026-04-28T10:00:00Z",
  "durationMs": 12345,
  // ... 原有字段
}
```

`confidence` 语义需明确：定义为"后端综合置信度（参考模型自报 + 规则匹配度），0–1 两位小数"，并约定阈值带（≥0.8 直采、0.5–0.8 review、<0.5 unknown）。

### §6.4 上传图片 — 多图、去重、存储

1. **批量上传**：保留单图接口，前端做并发上传（≤3）；可加 `POST /images:batch`。第一版只做并发即可。
2. **去重**：上传时算 `sha256` 入库，相同 hash 复用之前分析结果（演示来回切撞重复图很常见）。`inspection_image` 加 `content_hash`。
3. **文件存储**：`/files/inspection/img_001.jpg` 暗示落本地磁盘。建议第一版直接接 MinIO（一行 docker），用签名 URL，便于扩展和备份。
4. **大小/格式校验**：服务端必须再校验，限制 ≤ 10MB、白名单 `jpeg / png / webp`。HEIC 由前端转，后端兜底拒绝。

### §6.5 / §6.6 异步分析任务 — 不闭环

现在 §6.5 返回 `taskId`，§6.6 查询用 `recordId`，对不上；失败/进度/取消未定义。

修订建议：

1. **状态机**：`status ∈ {queued, processing, succeeded, failed, partial}`，`failed` 返回 `errorCode + errorMessage`，前端展示"重试 / 改人工"。
2. **进度**：`GET .../ai/latest` 在 `status=processing` 时返回 `progress: { processed: N, total: M }`。
3. **接口路径**：

   ```text
   GET /api/ai/tasks/{taskId}                          # 按任务查
   GET /api/inspection/records/{recordId}/ai/latest    # 取最近一次完成
   ```

4. **取消**：第一版不做，文档明确"分析发起后不可中断"。
5. **重复发起**：覆盖最新一次，旧任务标 `superseded`，结果留库。
6. **后端实现**：FastAPI BackgroundTasks 不可重启不可观测；用 Celery + Redis 或 RQ。

### §6.7 PATCH 修正 — 冲突、来源、批量

1. **乐观锁**：加 `If-Match` ETag 或请求带 `version` 字段，避免同条记录两端覆盖。
2. **`source` 字段**：由后端按登录态判断，不让前端传，避免伪造。
3. **批量修正**：补 `PATCH .../check-items:batch`，避免 N 次往返。

### §6.8 提交汇报 — 幂等

1. 请求头支持 `Idempotency-Key`，去重 24h，弱网双击不会产生两条。
2. `finalReportContent` 不让前端传，提交时由后端按当前 `inspection_check_result` 重新拼；或带 `reportVersion` 防过期。

### §8.2 模板表 — 覆盖能力

现在模板只挂 `point_type`，意味着同类型所有点位检查项一致。实际"会议中心UPS机房"和"紫涵雅集UPS机房"阈值常不同。两层结构：

- `inspection_template_item`（按 `point_type`，全局默认）
- `inspection_point_item_override`（按 `point_id`，覆盖/新增/禁用）

第一版可不实现 override 逻辑，但表先建好。

`judge_type` 显式枚举：`rule | vision | hybrid | manual_only`。
`threshold_json` 用统一 schema：

```jsonc
{ "type": "range", "min": 30, "max": 70, "unit": "%", "warnBand": 5 }
```

### §8.6 / §8.7 检查结果与修正日志 — 数据建模

1. `source_image_ids` 不要用 JSON 数组字段；建中间表 `inspection_check_image(check_result_id, image_id)`，便于"按图反查项"和索引。
2. 明确字段类型：`ai_basis` 是 JSONB（文本数组），`ai_suggestion / final_status` 是枚举字符串。
3. `need_review` 拆成 `review_status ∈ {not_required, pending, reviewed_pass, reviewed_change}`，区分"已复核维持原判"vs"还没复核"。
4. `inspection_revision_log` 加 `revision_group_id`，把同一次多字段修改的多行串起来。
5. 必加索引：

   ```text
   inspection_record(point_id, report_time)
   inspection_image(record_id)
   inspection_check_result(record_id, item_code)  -- unique
   ```

### §9 模型接入 — 抽象与稳定性

1. **抽象拆分**：

   ```text
   VisionProvider   - analyzeImage()
   OcrProvider      - extractText()
   TextProvider     - generateReport()
   ```

   理由：`generateReport` 是纯文本，OCR 常用本地 PaddleOCR，三者可独立替换。

2. **prompt caching**：选支持 caching 的模型（Claude、通义千问长文本类）。固定不变部分（系统指令、点位描述、检查项模板）放 prompt 前部命中缓存，图片放后部。一个点位多张图复用模板时缓存命中率高。
3. **结构化输出**：用模型的 JSON Mode / function calling / tool use，配 pydantic schema 校验，不要靠 prompt 硬要求 JSON。
4. **超时与重试**：`analyzeImage` 30s timeout、最多重试 1 次，重试用更小图（长边 1024）兜底。
5. **成本控制**：单用户/小时 N 次 + 单日预算上限，避免一次 bug 烧光额度。
6. **MockProvider**：必备。本地无网/演示重放/单元测试都靠它，要能返回 §5 中所有几种典型 JSON。
7. **图片预处理流水线**（统一在 AI 服务里做一次）：

   ```text
   HEIC → JPEG → EXIF 旋转 → 长边缩放到 1568px → JPEG q=85 → base64
   ```

   写成一个函数，所有 provider 复用。

8. **隐私 / 敏感信息**：照片可能含人脸、车牌、设备序列号。第一版必须明确：调云端模型前是否人脸打码？数据保留期？哪类点位走云、哪类走内网模型？这件事一定会被甲方/法务问。

### §10 演示稳定性

1. **演示模式开关**：AI 服务加 `DEMO_MODE` 环境变量。打开时三个固定场景图走 MockProvider 返回固化 JSON，新图才走真模型。
2. **预热 + 录屏兜底**：演示前一晚把三个场景跑通，结果固化进库；现场实际读缓存。同时录一份完整流程视频，设备/网络异常立即切播放。

---

## 三、推荐技术栈与组件清单

| 层 | 选型 | 理由 |
| --- | --- | --- |
| 前端 | React + TypeScript | 移动端优先，主流可维护 |
| 图像处理（前端） | `heic2any`、Canvas API、`exifr` | HEIC 转码、EXIF 旋转、压缩 |
| 业务后端 | FastAPI / Spring Boot | 团队熟悉哪个用哪个；接口简单 |
| AI 服务 | FastAPI（独立部署） | 与业务后端解耦，独立扩缩 |
| 任务队列 | Celery + Redis 或 RQ | 异步分析任务可观测、可重启 |
| 对象存储 | MinIO（自建）或阿里云 OSS | 一行 docker 起，签名 URL |
| 数据库 | PostgreSQL | JSONB 存 `threshold_json` / `analysis_json` 方便 |
| OCR | PaddleOCR（本地）+ 云端 OCR 兜底 | 表计读数本地够用，省成本 |
| 视觉模型 | OpenAI-compatible（Claude / 通义千问 VL / GLM-4V） | 走 prompt cache、结构化输出 |
| 监控 | 日志 + 简单 Prometheus | 第一版不上 APM，只看 token 用量 |

---

## 四、第一版最小落地路径（建议两周冲刺）

**Week 1 - 骨架**

- D1–D2：表结构（含 P0 修订项）、登录、点位/模板基础接口
- D3–D4：图片上传 + MinIO + 前端 HEIC/EXIF/压缩 流水线
- D5：AI 服务骨架（FastAPI）+ MockProvider，跑通"假分析"端到端

**Week 2 - 真模型 + 演示**

- D6–D7：接一家真视觉模型，跑通三个演示场景
- D8：规则引擎 + 三个 `judge_type` 实现
- D9：人工修正 / 提交 / 详情 / 日报
- D10：演示模式开关、固化数据、录屏兜底

第一版坚决不做：智能穿戴、企业微信对接、设备台账、自动派工、设备资产识别。

---

## 五、Codex 复核意见

整体判断：这份批注大部分成立，尤其是 P0 部分。原设计最大风险是把"AI 判断异常"和"规则判断异常"混在一起，这一点已经在最终工程逻辑设计里修正为：AI 提取事实，规则引擎判定检查项，人工确认最终结果。

### 1. 我完全采纳的部分

#### 1.1 AI 与规则引擎边界

采纳。

最终口径应固定为：

```text
AI：图片分类、OCR、事实提取、视觉描述、汇报文字生成
规则引擎：阈值判断、检查项状态判定、是否需要复核
人工：最终确认和提交
```

这是整个项目最关键的工程边界。这样做的好处是：

- 阈值调整不用改 prompt。
- 规则判断可解释。
- 后端可写单元测试。
- AI 识别不稳定时不会直接污染最终巡检结论。

#### 1.2 异步任务状态机

采纳。

必须使用 `taskId` 查询任务状态，不能只用 `recordId` 模糊查询。最终设计应保留两类接口：

```text
GET /api/ai/tasks/{taskId}
GET /api/inspection/records/{recordId}/ai/latest
```

前者看任务进度，后者取最近一次分析结果。状态枚举建议采用：

```text
queued / processing / succeeded / failed / partial / superseded
```

#### 1.3 图片预处理

采纳。

移动端照片问题是必踩点。第一版就要处理：

- HEIC 转 JPEG。
- EXIF 旋转。
- 长边压缩。
- 去 EXIF。
- 原图和分析图分开存。
- 服务端再次校验大小和格式。

这里不建议等二版再做，否则 iPhone 照片和竖拍仪表图很容易在演示时翻车。

#### 1.4 提交幂等

采纳。

移动端弱网和重复点击很常见，`Idempotency-Key` 应该放进第一版。否则一次提交生成两条巡检记录，后续汇总和日报都会乱。

#### 1.5 MockProvider / 演示模式

采纳，并且应从 P2 提到 P0。

参赛 demo 必须有 `DEMO_MODE`。固定样例图走 MockProvider 或缓存结果，新图再走真实模型。这样可以保证现场网络、模型额度、模型延迟出问题时仍能完整演示。

### 2. 我部分采纳的部分

#### 2.1 登录与权限

部分采纳。

第一版需要登录，但不建议一开始接完整企业微信 SSO。最小实现即可：

```text
JWT + 固定测试账号 + 单角色巡检员
```

企业微信身份、部门权限、多角色审批放后续版本。这样既能让 `user_id` 有来源，也不会把第一版卡在企微集成上。

#### 2.2 离线/弱网

部分采纳。

第一版明确不做真离线。文档中应写清楚：

```text
第一版要求在线使用，不支持离线上传和本地暂存队列。
```

但页面可以保留一个弱网提示。真离线涉及本地存储、重传、冲突处理和图片队列，第一版不值得做。

#### 2.3 EXIF / GPS 校验

部分采纳。

第一版只记录，不强校验：

- 拍摄时间。
- GPS。
- 设备信息。
- 原始 EXIF JSON。

不建议第一版做"照片不在点位范围内则拒绝"，因为现场 GPS 误差、室内定位缺失和隐私问题都会带来额外麻烦。

#### 2.4 模板覆盖能力

部分采纳。

我认可两层结构：

```text
inspection_template_item
inspection_point_item_override
```

但第一版只建议建表和预留逻辑，不做复杂配置页面。比赛 demo 阶段先手写几条样例数据即可。

#### 2.5 批量上传接口

部分采纳。

第一版不一定要做 `POST /images:batch`。前端限制并发 `<=3`，调用单图上传接口即可。批量接口可以后续补。

#### 2.6 图片 hash 去重

部分采纳。

`content_hash` 字段可以第一版就加，但"命中复用历史分析结果"可以放后面。第一版主要价值是防重复上传和演示样例识别。

### 3. 我建议降级的部分

#### 3.1 Prompt caching

降级到 P1/P2。

缓存有价值，但第一版不应依赖它。因为不同模型的缓存能力、计费方式和 SDK 支持差异很大。第一版重点是：

- 结构化输出稳定。
- JSON Schema 校验。
- 超时重试。
- MockProvider。

成本优化后续再做。

#### 3.2 人脸、车牌、设备序列号自动打码

降级到 P2。

第一版先采用管理策略：

```text
演示样例不使用含人脸、车牌、敏感编号的照片。
调用云端模型前去 EXIF，不上传 GPS。
敏感点位可切 MockProvider 或本地 OCR。
```

自动脱敏要引入目标检测和图像编辑，第一版会明显增加工程量。

#### 3.3 Celery / RQ

视开发时间决定。

工程上我认可 Celery + Redis 或 RQ，但如果第一版只是单机 demo，也可以先用数据库任务表 + 后台 worker 线程实现。关键不是队列选型，而是必须有：

- 任务表。
- 状态机。
- 失败记录。
- 可重试。
- 前端可轮询。

如果后面要多人试用，再换成 Celery/RQ。

### 4. 我对技术栈建议的修正

文档中推荐 PostgreSQL + JSONB 是合理的，但如果你更熟 MySQL，也可以继续用 MySQL 8。

选择标准：

- 如果只做比赛 demo，MySQL 8 足够。
- 如果后续大量 JSON 查询、复杂分析，PostgreSQL 更舒服。
- 不建议为了 JSONB 单独换不熟的数据库，比赛阶段稳定性优先。

前端技术栈也不必强制 React。你前面更倾向 Vue/H5 的话，用 Vue 3 更合适。这个项目 UI 不复杂，团队熟悉度比框架优劣更重要。

最终建议：

```text
前端：Vue 3 或 React，选自己更熟的
业务后端：Spring Boot 或 FastAPI，选自己更熟的
AI 服务：FastAPI
数据库：MySQL 8 或 PostgreSQL
对象存储：MinIO
任务：第一版可 DB 任务表 + worker，后续 Celery/RQ
```

### 5. 最终采纳决策

已经合并进最终工程逻辑设计的内容：

- AI 只提取事实，规则引擎判定检查项。
- `judge_type = rule | vision | hybrid | manual_only`。
- `threshold_json` 统一阈值 schema。
- 异步分析任务使用 `taskId`。
- 状态机包含 `queued / processing / succeeded / failed / partial / superseded`。
- 图片处理区分 `origin_path` 和 `analyze_path`。
- 上传时支持 `unknown` 图片类型，分析后回写。
- 提交使用 `Idempotency-Key`。
- 单项和批量修正支持版本号。
- 增加 `inspection_ai_task`。
- 增加 `inspection_point_item_override`。
- 增加 `inspection_check_image`。
- Provider 拆分为 `VisionProvider / OcrProvider / TextProvider / RuleEngine`。
- `MockProvider` 和 `DEMO_MODE` 进入第一版。

暂不作为第一版硬要求的内容：

- 真离线模式。
- 企业微信深度集成。
- 自动人脸/车牌/序列号打码。
- Prompt caching。
- 历史图片自动复用分析结果。
- 复杂配置后台。

### 6. 我建议的最终 P0 清单

第一版真正必须完成的是下面这些：

```text
1. 登录：JWT + 单角色
2. 点位和检查项模板
3. 图片上传：HEIC/JPEG/EXIF/压缩/大小校验
4. 图片存储：origin_path + analyze_path
5. 异步任务：taskId + 状态机 + 进度 + 失败
6. AI事实提取：结构化 JSON + schema 校验
7. 规则引擎：根据 judge_type 和 threshold_json 判定
8. 人工确认：单项或批量修正，带 version
9. 汇报生成：后端基于最终检查项生成
10. 提交幂等：Idempotency-Key
11. MockProvider：演示兜底
```

这 11 项做完，第一版 demo 的工程逻辑就是闭环的。

---

## 六、最终结论（Claude × Codex 合并版）

经过两轮复核，对原立项文档的批注与修订决策已收敛。本节为最终一致版本，作为第一版动手依据。

### 6.1 优先级最终矩阵

| 等级 | 含义 | 时间窗 |
| --- | --- | --- |
| **P0** | 第一版必做，做不完不闭环 | 两周冲刺 D1–D10 |
| **P1** | 第一版上线前补齐，缺了能跑但不稳 | 演示前最后两天 |
| **P2** | 二版考虑，第一版只留接口位/数据位 | 比赛后 |

### 6.2 P0 清单（12 项，第一版 DoD）

| # | 项 | 关键点 |
| --- | --- | --- |
| 1 | 登录 | JWT + 固定测试账号 + 单角色巡检员；不做企微 SSO |
| 2 | 点位 / 检查项模板 | `inspection_template_item` + `inspection_point_item_override`（建表，逻辑可后置） |
| 3 | 图片上传链路 | 前端 HEIC→JPEG、EXIF 旋转、长边 1600px / q=85；后端格式&大小复校 |
| 4 | 图片存储 | `origin_path` + `analyze_path` 分开；MinIO；`content_hash` 第一版只防重 |
| 5 | 异步任务 | `taskId` 查询；状态机 `queued/processing/succeeded/failed/partial/superseded`；进度 + 失败原因；DB 任务表 + worker 起步 |
| 6 | AI 事实提取 | 结构化 JSON + schema 校验；`schemaVersion / model / tokenUsage / partialFailure / processedAt` 全字段 |
| 7 | 规则引擎 | 按 `judge_type ∈ {rule, vision, hybrid, manual_only}` + `threshold_json` 判定，输出 `system_status` |
| 8 | 人工确认 | 单项 / 批量修正，带 `version` 乐观锁；`source` 后端判定 |
| 9 | 汇报生成 | 后端基于当前检查项结果生成正文，前端不传最终正文 |
| 10 | 提交幂等 | `Idempotency-Key` + 24h 去重 |
| 11 | MockProvider / DEMO_MODE | 固定样例走 Mock，新图走真模型；现场兜底 |
| 12 | 成本闸门 | AI 服务每日 token 预算上限 + 单用户限流，命中熔断切 MockProvider |

### 6.3 P1 清单（演示前补齐）

- 中间表与索引：`inspection_check_image`、`inspection_revision_log.revision_group_id`、关键复合索引。
- AI 调用稳定性：30s timeout、最多 1 次重试、重试用更小图（长边 1024）兜底。
- 视觉模型结构化输出（JSON Mode / function calling）+ pydantic 校验。
- EXIF 元数据入库：`taken_at / gps_lat / gps_lng / exif_json`，**只记录不拒绝**。
- 弱网提示文案、双击防重前端兜底。

### 6.4 P2 清单（二版考虑）

- 真离线模式（本地暂存队列、断点续传、冲突处理）。
- Prompt caching 成本优化（待选定主力模型再决策）。
- 人脸 / 车牌 / 设备序列号自动打码；第一版用"演示样例不用敏感图 + 去 EXIF + 敏感点位切 Mock"管理策略替代。
- 图片 hash 命中复用历史分析结果。
- 企业微信 SSO + 多角色审批。
- Celery / RQ 替换 DB 任务表（多人试用阶段再切）。
- 模板配置后台 UI。
- 日报、周报、异常趋势分析。

### 6.5 锁定的工程边界

```text
AI         → 图片分类、OCR、事实提取、视觉描述、汇报文字生成
规则引擎    → 阈值判断、检查项状态判定、是否需要复核
人工       → 最终确认和提交
```

这是整个项目最关键的工程边界，不再调整。

### 6.6 锁定的核心技术栈

| 层 | 选型 | 备注 |
| --- | --- | --- |
| 前端 | Vue 3 / React | 团队熟悉哪个用哪个 |
| 业务后端 | Spring Boot / FastAPI | 团队熟悉哪个用哪个 |
| AI 服务 | FastAPI（独立部署） | Provider 拆分 `VisionProvider / OcrProvider / TextProvider / RuleEngine` |
| 数据库 | MySQL 8 / PostgreSQL | 比赛阶段熟悉度优先；不为 JSONB 单独换库 |
| 对象存储 | MinIO | 一行 docker；签名 URL |
| 任务 | DB 任务表 + worker | 多人试用阶段再换 Celery / RQ |
| OCR | PaddleOCR + 云端 OCR 兜底 | 表计读数本地优先 |
| 视觉模型 | OpenAI-compatible（Claude / 通义千问 VL / GLM-4V） | 走结构化输出 |

### 6.7 两周冲刺路径（最终版）

**Week 1 - 骨架**

- D1–D2：表结构（含 P0 修订项）、JWT 登录、点位/模板基础接口。
- D3–D4：图片上传 + MinIO + 前端 HEIC/EXIF/压缩 流水线 + 后端复校。
- D5：AI 服务骨架 + MockProvider + 异步任务表，跑通"假分析"端到端。

**Week 2 - 真模型 + 演示**

- D6–D7：接一家真视觉模型（结构化输出 + schema 校验）+ 三个固定演示场景。
- D8：规则引擎（按 `judge_type` + `threshold_json`）+ 检查项判定闭环。
- D9：人工修正（单项/批量 + version）、提交幂等、汇报生成、详情页。
- D10：DEMO_MODE 固化数据 + 成本闸门 + 录屏兜底；P1 项查漏补缺。

**第一版坚决不做**：智能穿戴、企业微信深度集成、设备台账、自动派工、设备资产识别、真离线、自动脱敏、Prompt caching。

### 6.8 验收标准（DoD）

第一版完成的判定：

1. 12 项 P0 全部通过端到端测试。
2. 三个固定演示场景在 DEMO_MODE 下 100% 可重放。
3. 关闭 DEMO_MODE，三个场景在真实模型下端到端跑通至少 1 次。
4. 单日 token 预算闸门触发后能正确熔断并切 MockProvider。
5. 弱网双击提交不产生重复记录。
6. iPhone 实机拍照（HEIC + 竖拍）能完整跑完上传→分析→提交。

满足以上 6 条即第一版闭环，可参赛演示。
