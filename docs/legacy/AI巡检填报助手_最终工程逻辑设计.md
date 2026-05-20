# AI巡检填报助手 - 最终工程逻辑设计

## 1. 项目定位

第一版目标是做一个可演示、可试用的巡检填报辅助工具，不做全自动巡检系统。

核心流程：

```text
巡检人员选择点位 -> 上传巡检照片 -> AI提取客观事实 -> 后端规则生成检查项建议 -> 人工确认/修正 -> 生成汇报内容 -> 提交记录
```

关键原则：

- AI 负责看图、OCR、事实提取和文字生成。
- 后端规则引擎负责正常、异常、复核等检查项判定。
- 人工确认是最终结果来源。
- 图片识别用于辅助填报，不做设备资产身份识别。
- 智能穿戴作为未来入口，不作为第一版前置条件。

## 2. 第一版功能边界

第一版只做：

- 最小登录：JWT + 单角色巡检员。
- 点位选择。
- 多张巡检照片上传。
- 图片预处理：HEIC 转 JPEG、EXIF 旋转、压缩、去 EXIF。
- 图片分类和事实提取。
- 规则引擎生成检查项建议。
- 异常描述和汇报正文生成。
- 人工确认和修正。
- 提交巡检记录。
- 演示模式：固定样例可走 MockProvider。

第一版明确不做：

- 离线巡检。第一版要求在线使用。
- 企业微信历史汇报全量拉取。
- 智能穿戴真机接入。
- 复杂设备台账系统。
- 自动派工闭环。
- 场景图精确识别具体设备资产。
- 自动用 EXIF 拒绝巡检。第一版只记录拍摄时间、GPS、设备信息，暂不强校验。

## 3. 页面原型流程

### 3.1 首页 / 任务入口

用途：进入巡检填报。

页面内容：

- 今日巡检入口。
- 最近巡检记录。
- 待确认分析任务。
- 今日异常和待复核数量。

主要操作：

- 开始巡检。
- 查看历史记录。

### 3.2 新建巡检页

用途：选择本次巡检对象。

字段：

- 巡检项目：如会议中心、紫涵雅集。
- 巡检点位：如 UPS机房、配电房、水表点位。
- 巡检类型：日常巡检、能耗抄表、异常复核。
- 巡检时间：默认当前时间。
- 巡检人：从登录态带出。

操作：

- 下一步：上传照片。

### 3.3 照片上传页

用途：上传现场巡检图片。

字段：

- 上传照片，多张。
- 图片类型：可手动选择，不选则保存为 `unknown`，由分析阶段识别后回写。
- 备注，可选。

图片类型枚举：

- `unknown`：未知，待分析。
- `environment_scene`：环境场景图。
- `device_panel`：设备面板图。
- `meter_reading`：仪表/表计读数图。
- `indicator_light`：指示灯状态图。
- `paper_form`：纸质表单图。
- `other`：其他。

前端上传处理：

- HEIC 转 JPEG。
- 按 EXIF 旋转后再编码。
- 分析图长边压缩到 `1600px`，JPEG 质量 `85`。
- 多图并发上传限制 `<=3`。

后端上传处理：

- 再次校验格式和大小。
- 允许格式：`jpeg/png/webp`。
- 单张图片最大 `10MB`。
- 原图和分析图分开保存：`origin_path`、`analyze_path`。
- 记录 EXIF 中的拍摄时间、GPS、设备信息；第一版只记录，不拒绝。

操作：

- 开始 AI 分析。

### 3.4 分析任务页

用途：展示异步分析进度。

状态：

- `queued`：排队中。
- `processing`：分析中。
- `succeeded`：全部成功。
- `partial`：部分图片失败。
- `failed`：全部失败。
- `superseded`：被新任务覆盖。

页面内容：

- 已分析图片数 / 总图片数。
- 当前状态。
- 失败原因。
- 重试按钮。
- 改为人工填写按钮。

第一版不支持取消任务；分析一旦发起不可中断。同一记录重复发起分析时，新任务覆盖旧任务，旧任务标记为 `superseded`。

### 3.5 AI分析结果页

用途：展示图片事实提取结果和规则判定后的检查项建议。

页面分区：

- 图片识别事实。
- 规则判定结果。
- 待复核项。
- 异常描述建议。
- 建议补查项。

每个检查项显示：

- 检查项名称。
- 判断方式：`rule`、`vision`、`hybrid`、`manual_only`。
- 系统建议：正常 / 异常 / 待复核 / 未识别。
- 判断依据。
- 置信度。
- 复核状态。
- 修改按钮。

状态枚举：

- `normal`：建议正常。
- `abnormal`：建议异常。
- `review`：建议人工复核。
- `unknown`：未能判断。

### 3.6 异常确认弹窗

用途：巡检人员确认或修改异常项。

字段：

- 异常类型。
- 系统生成异常描述。
- 规则或视觉判断依据。
- 常见原因。
- 建议补查项。
- 人工备注。
- 当前版本号。

操作：

- 确认为正常。
- 确认为异常。
- 标记为待复核。

提交修正时带 `version`，后端做乐观锁，避免多人覆盖。

### 3.7 汇报预览页

用途：提交前查看最终汇报内容。

页面内容：

- 巡检基本信息。
- 上传照片列表。
- 检查项最终结果。
- 异常项汇总。
- 待复核项汇总。
- 生成的汇报正文。
- 人工补充说明。

操作：

- 返回修改。
- 提交汇报。

提交时带 `Idempotency-Key`，后端 24 小时内去重。提交正文由后端基于当前检查项结果生成，前端不直接传最终正文。

### 3.8 提交成功页

用途：展示提交结果。

页面内容：

- 记录编号。
- 巡检时间。
- 异常数量。
- 待复核数量。
- 汇报摘要。

操作：

- 查看详情。
- 继续巡检。

## 4. 处理流程和职责边界

### 4.1 总流程

```text
接收图片和点位上下文
-> 图片预处理
-> OCR / 视觉模型提取客观事实
-> 后端规则引擎读取检查项模板和阈值
-> 生成检查项建议
-> 文本模型生成异常描述和汇报正文
-> 人工确认
-> 保存最终巡检记录
```

### 4.2 AI职责

AI负责：

- 图片分类。
- 描述客观可见事实。
- 提取文字、数值、指示灯状态、表计读数。
- 对 `judge_type = vision` 的感官类检查项给出视觉建议。
- 生成异常描述和汇报正文。

AI不负责：

- 数值阈值判定。
- 最终正常/异常结论。
- 设备资产身份确认。
- 维修结论。
- 责任判定。

### 4.3 规则引擎职责

规则引擎负责：

- 读取 `inspection_template_item.threshold_json`。
- 读取点位覆盖规则 `inspection_point_item_override`。
- 对 `judge_type = rule` 的检查项做确定性判断。
- 对 `judge_type = hybrid` 的检查项合并 AI 事实和规则结果。
- 生成 `system_status`、`basis`、`need_review`。

判断方式枚举：

- `rule`：只看结构化字段和阈值，如温度、湿度、电压。
- `vision`：只看视觉事实，如是否有明显积水、杂物、凌乱。
- `hybrid`：视觉事实 + 阈值规则。
- `manual_only`：系统不判断，只要求人工填写。

### 4.4 不同图片类型处理逻辑

`environment_scene`：

- AI 提取明显杂物、积水、凌乱、照明异常、外观异常迹象。
- `judge_type = vision` 的环境项可由 AI 给视觉建议。

`device_panel`：

- AI/OCR 提取屏幕文字、状态、报警提示、温湿度等。
- 数值是否异常交给规则引擎。

`meter_reading`：

- OCR 提取电压、电流、能耗、水表读数等。
- 读数范围和跳变判断交给规则引擎。

`indicator_light`：

- AI/CV 提取红、黄、绿等指示灯状态。
- 指示灯含义由规则或人工确认，第一版默认建议复核。

`paper_form`：

- OCR 读取表单内容。
- 提取已填写数据，辅助整理为电子记录。

## 5. AI事实提取 JSON

### 5.1 Vision/OCR 返回结构

```json
{
  "schemaVersion": "1.0",
  "analysisId": "ana_20260428_0001",
  "recordId": "rec_20260428_0001",
  "model": {
    "provider": "openai-compatible",
    "name": "vision-model",
    "version": "2026-04"
  },
  "processedAt": "2026-04-28T10:00:00+08:00",
  "durationMs": 8200,
  "tokenUsage": {
    "input": 1200,
    "output": 480,
    "imageCount": 2
  },
  "partialFailure": false,
  "failedImageIds": [],
  "images": [],
  "warnings": []
}
```

### 5.2 单图事实结果

```json
{
  "imageId": "img_001",
  "detectedImageType": "environment_scene",
  "detectedScene": "UPS机房",
  "observations": [
    "现场可见UPS相关设备及配电柜",
    "地面未见明显积水",
    "未见明显杂物堆放"
  ],
  "extractedFields": {
    "temperature": null,
    "humidity": null,
    "voltageA": null,
    "voltageB": null,
    "voltageC": null,
    "meterReading": null,
    "indicatorRed": null,
    "indicatorYellow": null,
    "indicatorGreen": null
  },
  "visualFindings": [
    {
      "code": "NO_VISIBLE_WATER",
      "label": "未见明显积水",
      "confidence": 0.86
    }
  ],
  "quality": {
    "isBlurry": false,
    "isOverExposed": true,
    "isRotated": false,
    "score": 0.78
  },
  "confidence": 0.78,
  "needReview": true
}
```

### 5.3 低置信度返回

```json
{
  "schemaVersion": "1.0",
  "analysisId": "ana_20260428_0002",
  "recordId": "rec_20260428_0002",
  "partialFailure": true,
  "failedImageIds": ["img_003"],
  "images": [
    {
      "imageId": "img_003",
      "detectedImageType": "unknown",
      "observations": [],
      "extractedFields": {},
      "visualFindings": [],
      "quality": {
        "isBlurry": true,
        "isOverExposed": false,
        "isRotated": false,
        "score": 0.31
      },
      "confidence": 0.31,
      "needReview": true
    }
  ],
  "warnings": ["LOW_IMAGE_QUALITY"]
}
```

## 6. 后端规则判定 JSON

### 6.1 检查项结果

```json
{
  "itemCode": "HUMIDITY_STATUS",
  "itemName": "湿度是否正常",
  "category": "environment",
  "judgeType": "rule",
  "systemStatus": "normal",
  "basis": [
    "识别湿度为53%",
    "阈值范围为30%-70%"
  ],
  "source": "rule_engine",
  "confidence": 0.88,
  "needReview": false,
  "reviewStatus": "not_required",
  "sourceImageIds": ["img_002"],
  "version": 1
}
```

### 6.2 视觉类检查项结果

```json
{
  "itemCode": "ENV_CLEAN",
  "itemName": "环境是否整洁",
  "category": "environment",
  "judgeType": "vision",
  "systemStatus": "normal",
  "basis": [
    "图片中未见明显杂物堆放",
    "地面未见明显垃圾或积水"
  ],
  "source": "vision_model",
  "confidence": 0.86,
  "needReview": false,
  "reviewStatus": "not_required",
  "sourceImageIds": ["img_001"],
  "version": 1
}
```

### 6.3 待复核结果

```json
{
  "itemCode": "INDICATOR_STATUS",
  "itemName": "指示灯是否正常",
  "category": "device",
  "judgeType": "hybrid",
  "systemStatus": "review",
  "basis": [
    "识别到红灯、黄灯、绿灯均亮",
    "当前模板未配置该设备指示灯含义"
  ],
  "source": "rule_engine",
  "confidence": 0.74,
  "needReview": true,
  "reviewStatus": "pending",
  "suggestedActions": [
    "人工确认该设备指示灯正常含义",
    "补充设备面板完整照片"
  ],
  "sourceImageIds": ["img_004"],
  "version": 1
}
```

### 6.4 汇报内容结果

```json
{
  "title": "会议中心UPS机房日常巡检",
  "content": "本次对会议中心UPS机房进行日常巡检，现场照片已上传。根据系统分析，现场环境整体较整洁，未见明显积水和杂物堆放。温湿度读数在当前阈值范围内，指示灯状态建议人工复核。请巡检人员确认后提交最终结果。",
  "normalItems": ["环境是否整洁", "湿度是否正常"],
  "abnormalItems": [],
  "reviewItems": ["指示灯是否正常"],
  "nextActions": [
    "复核指示灯状态含义"
  ],
  "reportVersion": 1
}
```

## 7. 后端接口设计

接口统一前缀：`/api`。

除登录接口外，所有接口都带：

```text
Authorization: Bearer <token>
```

### 7.1 登录

`POST /api/auth/login`

第一版可使用测试账号密码或企业微信临时用户映射。

请求：

```json
{
  "username": "inspector01",
  "password": "******"
}
```

返回：

```json
{
  "token": "jwt-token",
  "user": {
    "userId": 1,
    "name": "巡检员",
    "role": "inspector"
  }
}
```

### 7.2 获取点位列表

`GET /api/inspection/points`

查询参数：

- `projectName`：项目名称，可选。
- `pointType`：点位类型，可选。

返回：

```json
{
  "items": [
    {
      "pointId": 1,
      "pointName": "会议中心UPS机房",
      "pointType": "ups_room",
      "projectName": "会议中心"
    }
  ]
}
```

### 7.3 获取点位检查模板

`GET /api/inspection/points/{pointId}/template`

返回：

```json
{
  "pointId": 1,
  "pointName": "会议中心UPS机房",
  "items": [
    {
      "itemCode": "HUMIDITY_STATUS",
      "itemName": "湿度是否正常",
      "category": "environment",
      "judgeType": "rule",
      "required": true,
      "threshold": {
        "type": "range",
        "min": 30,
        "max": 70,
        "unit": "%",
        "warnBand": 5
      }
    }
  ]
}
```

### 7.4 创建巡检记录

`POST /api/inspection/records`

请求：

```json
{
  "projectName": "会议中心",
  "pointId": 1,
  "inspectionType": "daily",
  "reportTime": "2026-04-28 10:00:00",
  "remark": "日常巡检"
}
```

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "recordNo": "XJ202604280001",
  "status": "draft"
}
```

### 7.5 上传巡检图片

`POST /api/inspection/records/{recordId}/images`

请求类型：`multipart/form-data`

字段：

- `file`：图片文件。
- `imageType`：可选，不传则为 `unknown`。
- `remark`：可选。

返回：

```json
{
  "imageId": "img_001",
  "recordId": "rec_20260428_0001",
  "imageType": "unknown",
  "originPath": "minio://inspection/origin/img_001.jpg",
  "analyzePath": "minio://inspection/analyze/img_001.jpg",
  "contentHash": "sha256-value",
  "exif": {
    "takenAt": "2026-04-28T09:58:00+08:00",
    "gps": null,
    "device": "iPhone"
  }
}
```

### 7.6 发起 AI 分析任务

`POST /api/inspection/records/{recordId}/ai/tasks`

请求：

```json
{
  "imageIds": ["img_001", "img_002"],
  "mode": "inspection_fact_extract",
  "useCloudVision": true
}
```

返回：

```json
{
  "taskId": "task_001",
  "recordId": "rec_20260428_0001",
  "status": "queued",
  "progress": {
    "total": 2,
    "done": 0,
    "failed": 0
  }
}
```

### 7.7 查询 AI 分析任务

`GET /api/ai/tasks/{taskId}`

返回：

```json
{
  "taskId": "task_001",
  "recordId": "rec_20260428_0001",
  "status": "processing",
  "progress": {
    "total": 2,
    "done": 1,
    "failed": 0
  },
  "errorCode": null,
  "errorMessage": null,
  "resultAvailable": false
}
```

失败返回：

```json
{
  "taskId": "task_001",
  "recordId": "rec_20260428_0001",
  "status": "failed",
  "progress": {
    "total": 2,
    "done": 0,
    "failed": 2
  },
  "errorCode": "AI_TIMEOUT",
  "errorMessage": "图片分析超时，可重试或改为人工填写。",
  "resultAvailable": false
}
```

### 7.8 查询最近一次分析结果

`GET /api/inspection/records/{recordId}/ai/latest`

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "taskId": "task_001",
  "taskStatus": "succeeded",
  "factExtraction": {},
  "checkResults": [],
  "generatedReport": {}
}
```

### 7.9 单项人工修正

`PATCH /api/inspection/records/{recordId}/check-items/{itemCode}`

请求：

```json
{
  "version": 1,
  "finalStatus": "abnormal",
  "manualDescription": "现场湿度偏高，已通知复核除湿机运行状态。"
}
```

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "itemCode": "HUMIDITY_STATUS",
  "finalStatus": "abnormal",
  "version": 2,
  "saved": true
}
```

### 7.10 批量人工修正

`PATCH /api/inspection/records/{recordId}/check-items:batch`

请求：

```json
{
  "items": [
    {
      "itemCode": "HUMIDITY_STATUS",
      "version": 1,
      "finalStatus": "normal",
      "manualDescription": "现场复核正常。"
    }
  ]
}
```

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "updated": 1,
  "conflicts": []
}
```

### 7.11 生成汇报预览

`POST /api/inspection/records/{recordId}/report/preview`

请求：

```json
{
  "manualRemark": "现场已复核。"
}
```

返回：见第 6.4 节。

### 7.12 提交汇报

`POST /api/inspection/records/{recordId}/submit`

请求头：

```text
Idempotency-Key: uuid-value
```

请求：

```json
{
  "reportVersion": 1
}
```

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "recordNo": "XJ202604280001",
  "status": "submitted",
  "abnormalCount": 1,
  "reviewCount": 1
}
```

### 7.13 查询巡检记录详情

`GET /api/inspection/records/{recordId}`

返回：

```json
{
  "recordId": "rec_20260428_0001",
  "recordNo": "XJ202604280001",
  "pointName": "会议中心UPS机房",
  "status": "submitted",
  "images": [],
  "checkResults": [],
  "reportContent": ""
}
```

### 7.14 查询日报

`GET /api/inspection/reports/daily`

查询参数：

- `date`
- `projectName`

返回：

```json
{
  "date": "2026-04-28",
  "projectName": "会议中心",
  "totalRecords": 12,
  "abnormalRecords": 2,
  "reviewRecords": 3,
  "aiProcessedImages": 30,
  "summary": "今日共完成12次巡检，其中2次存在异常，3次需复核。",
  "items": []
}
```

## 8. AI服务接口设计

AI服务建议独立为 FastAPI 服务，接口前缀为 `/ai`。业务后端只调用 AI 服务，不直接写模型调用细节。

### 8.1 巡检图片事实提取

`POST /ai/inspection/extract-facts`

请求：

```json
{
  "recordId": "rec_20260428_0001",
  "point": {
    "pointId": 1,
    "pointName": "会议中心UPS机房",
    "pointType": "ups_room"
  },
  "images": [
    {
      "imageId": "img_001",
      "imageTypeHint": "unknown",
      "analyzePath": "minio://inspection/analyze/img_001.jpg"
    }
  ]
}
```

返回：见第 5 节。

### 8.2 OCR 提取

`POST /ai/ocr`

请求：

```json
{
  "imageId": "img_002",
  "analyzePath": "minio://inspection/analyze/img_002.jpg",
  "ocrType": "meter"
}
```

返回：

```json
{
  "imageId": "img_002",
  "rawText": "53 21",
  "fields": {
    "humidity": 53,
    "temperature": 21
  },
  "confidence": 0.88
}
```

### 8.3 汇报文本生成

`POST /ai/report/generate`

请求：

```json
{
  "recordId": "rec_20260428_0001",
  "pointName": "会议中心UPS机房",
  "checkResults": [],
  "manualRemark": "现场已复核"
}
```

返回：见第 6.4 节。

## 9. 核心数据表

### 9.1 inspection_user

用户表。

字段：

- `id`
- `username`
- `name`
- `role`
- `wecom_userid`
- `status`
- `created_at`
- `updated_at`

### 9.2 inspection_point

巡检点位表。

字段：

- `id`
- `project_name`
- `point_name`
- `point_type`
- `location_desc`
- `status`
- `created_at`
- `updated_at`

### 9.3 inspection_template_item

点位类型默认检查项模板。

字段：

- `id`
- `point_type`
- `item_code`
- `item_name`
- `category`
- `required`
- `judge_type`
- `threshold_json`
- `sort_order`
- `status`

`judge_type` 枚举：

- `rule`
- `vision`
- `hybrid`
- `manual_only`

`threshold_json` 示例：

```json
{
  "type": "range",
  "min": 30,
  "max": 70,
  "unit": "%",
  "warnBand": 5
}
```

### 9.4 inspection_point_item_override

点位检查项覆盖表。

用途：覆盖某个具体点位的默认检查项、阈值、启用状态。

字段：

- `id`
- `point_id`
- `item_code`
- `override_name`
- `override_required`
- `override_judge_type`
- `override_threshold_json`
- `enabled`
- `created_at`
- `updated_at`

第一版可以只建表，不做复杂配置页面。

### 9.5 inspection_record

巡检记录主表。

字段：

- `id`
- `record_no`
- `point_id`
- `point_name`
- `inspection_type`
- `report_time`
- `user_id`
- `status`
- `ai_status`
- `abnormal_count`
- `review_count`
- `report_content`
- `report_version`
- `idempotency_key`
- `created_at`
- `updated_at`

索引：

- `idx_record_point_time(point_id, report_time)`
- `uk_record_no(record_no)`
- `uk_idempotency_key(idempotency_key)`

### 9.6 inspection_image

巡检图片表。

字段：

- `id`
- `record_id`
- `image_type`
- `origin_name`
- `origin_path`
- `analyze_path`
- `thumb_path`
- `content_hash`
- `file_size`
- `mime_type`
- `exif_json`
- `ai_processed`
- `created_at`

索引：

- `idx_image_record(record_id)`
- `idx_image_hash(content_hash)`

### 9.7 inspection_ai_task

AI分析任务表。

字段：

- `id`
- `task_id`
- `record_id`
- `status`
- `total_count`
- `done_count`
- `failed_count`
- `error_code`
- `error_message`
- `started_at`
- `finished_at`
- `created_at`

索引：

- `uk_task_id(task_id)`
- `idx_task_record(record_id)`

### 9.8 inspection_ai_analysis

AI事实提取结果表。

字段：

- `id`
- `record_id`
- `task_id`
- `analysis_json`
- `schema_version`
- `model_provider`
- `model_name`
- `token_usage_json`
- `partial_failure`
- `duration_ms`
- `created_at`

### 9.9 inspection_check_result

检查项最终结果表。

字段：

- `id`
- `record_id`
- `item_code`
- `item_name`
- `judge_type`
- `system_status`
- `system_basis_json`
- `final_status`
- `final_description`
- `review_status`
- `confidence`
- `version`
- `updated_by`
- `updated_at`

索引：

- `uk_record_item(record_id, item_code)`

`review_status` 枚举：

- `not_required`
- `pending`
- `reviewed_pass`
- `reviewed_change`

### 9.10 inspection_check_image

检查项和来源图片关系表。

字段：

- `id`
- `check_result_id`
- `image_id`

索引：

- `idx_check_image_check(check_result_id)`
- `idx_check_image_image(image_id)`

### 9.11 inspection_revision_log

人工修正日志表。

字段：

- `id`
- `revision_group_id`
- `record_id`
- `item_code`
- `field_name`
- `old_value`
- `new_value`
- `revision_reason`
- `revised_by`
- `revised_at`

## 10. 模型接入设计

模型能力拆成四类：

```text
VisionProvider
  - classifyImage()
  - extractVisualFacts()

OcrProvider
  - extractText()
  - extractMeterFields()

TextProvider
  - generateAbnormalDescription()
  - generateReport()

RuleEngine
  - evaluateCheckItems()
```

Provider 实现：

- `OpenAICompatibleVisionProvider`
- `AliyunModelStudioVisionProvider`
- `PaddleOcrProvider`
- `OpenAICompatibleTextProvider`
- `MockVisionProvider`
- `MockOcrProvider`
- `MockTextProvider`

第一版要求：

- 所有模型输出必须走 JSON Schema / function calling / tool use 或 Pydantic 校验。
- `analyzeImage` 超时 `30s`，最多重试 `1` 次。
- 重试时使用长边 `1024px` 的小图。
- AI 服务设置单用户小时限流和单日预算上限。
- `DEMO_MODE=true` 时固定样例图走 MockProvider。
- 调用云端模型前只发送分析图，不发送 EXIF GPS。
- 演示样例避免包含人脸、车牌、敏感设备序列号。

图片送模型前的统一处理流水线：

```text
HEIC -> JPEG
-> EXIF 旋转
-> 长边缩放到 1568px
-> JPEG q=85
-> 去 EXIF
-> base64 或文件流输入 provider
```

## 11. 演示建议

建议固定演示 3 个场景：

1. UPS机房环境图。
2. 除湿机 / 温湿度面板图。
3. 水表或电表读数图。

演示路径：

```text
登录 -> 选择点位 -> 上传照片 -> 发起分析 -> 查看事实提取 -> 查看规则判定 -> 人工确认 -> 生成汇报 -> 提交成功
```

演示保障：

- 前一晚固定跑通三组样例。
- 结果固化进库。
- `DEMO_MODE` 可切换 MockProvider。
- 准备一份完整流程录屏。

核心讲法：

```text
AI不是替代巡检人员，而是从现场照片中提取可用信息，再由规则和人工确认生成规范汇报。
```

## 12. 第一版开发优先级

P0：

- 登录与用户身份。
- 图片上传和预处理。
- 异步任务状态机。
- AI事实提取 JSON。
- 规则引擎判定检查项。
- 人工确认。
- 提交幂等。
- MockProvider。

P1：

- 批量修正。
- 日报汇总。
- 点位检查项 override。
- 速率限制和预算上限。
- 修正日志。

P2：

- 企业微信深度接入。
- 真离线模式。
- 历史图片 hash 复用分析结果。
- 自动脱敏。
- 智能穿戴入口演示。

