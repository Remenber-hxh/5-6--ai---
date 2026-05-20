# AI巡检填报助手 - VLM 接入说明书

> 面向第一版 AI 服务开发。目标不是训练模型，而是把视觉模型包装成稳定的"事实提取接口"。
> 价格、模型可用区域、免费额度以阿里云百炼控制台和官方文档为准。本文只给工程口径和量级判断。

---

## 0. TL;DR

- 推荐主力：`qwen3.6-plus` 做视觉事实提取，稳定后切 `qwen3.6-flash` 降成本。
- OCR：先用 `Qwen-OCR` 快速跑通；后续可加 `PaddleOCR` 做本地化和低成本路线。
- 文本汇报：`qwen3.6-flash` 或 `qwen-turbo`，按实际效果和价格选择。
- 调用方式：优先封装 OpenAI-compatible Provider，但不要假设换厂商只改 `base_url` 就完全零改动。
- 图片预处理：HEIC/JPEG 转换、EXIF 旋转、长边 1568 px、JPEG q=85、去 EXIF。
- 输出约束：优先用厂商结构化输出能力；无论是否支持，都必须做 Pydantic 校验。
- 事实提取温度：`0.1`。
- 汇报生成温度：`0.5`。
- 超时：`30s`。
- 重试：`1` 次。
- 降级链：`qwen3.6-plus -> qwen3.6-flash -> MockProvider -> 人工复核`。
- Demo 必备：`MockProvider` + `DEMO_MODE`。

---

## 1. 三类图像能力

| 类别 | 干什么 | 在本项目的角色 |
| --- | --- | --- |
| OCR | 提取图片里的文字和数字 | 表计读数、纸质表单、屏幕读数 |
| CV / 目标检测 | 找图里有什么物体、在哪 | 第一版不训练检测模型；指示灯可后续用简单颜色规则补充 |
| VLM 视觉大模型 | 看图理解场景，按自然语言问题返回结果 | 第一版主力，用于场景事实提取和综合描述 |

工程边界固定为：

```text
VLM / OCR：图片分类、文字提取、事实描述、汇报正文生成
规则引擎：阈值判断、检查项状态判定、是否需要复核
人工：最终确认和提交
```

重点：不要让 VLM 直接输出"是否异常"作为最终结论。它只提供事实和视觉建议，后端规则和人工确认才是最终来源。

---

## 2. VLM 工作机制

### 2.1 图片如何进入模型

```text
原图 -> 缩放 -> 切成 patch -> 编码成视觉 token -> 与 prompt 拼接 -> 模型推理
```

工程后果：

- 图越大，token 越多，越贵越慢。
- 常用输入长边控制在 `1024-1568px`。
- 阿里云视觉理解文档给过图片 token 估算公式：`h * w / (32 * 32) + 2`。
- 例子：`1600x900` 约 `1408` image tokens；`1568x1568` 约 `2403` image tokens。
- 不要把"一张图固定 1000-1500 token"写死，只能当部分长宽比的量级参考。

### 2.2 输出本质是文字

VLM 本质上输出文本。系统要稳定使用，就必须把它约束成结构化数据：

- 优先使用厂商提供的 JSON Mode / JSON Schema / function calling / tool use。
- 再用 Pydantic 校验。
- 校验失败时重试一次。
- 重试仍失败则标 `needReview=true`，进入人工复核。

### 2.3 典型风险

- 看不清会猜：模糊图片可能编出看似合理的数值。
- 被问题诱导：你问"是不是异常"，模型更容易顺着问题回答。
- 结构化不稳定：只靠 prompt 要求 JSON，模型可能加解释文字。

因此 prompt 必须中性：

```text
只描述图片中的客观事实，不做异常判断，不猜测设备身份。
```

---

## 3. 模型选型

### 3.1 视觉模型

| 模型 | 定位 | 建议 |
| --- | --- | --- |
| qwen3.6-plus | 效果优先 | 第一版先用它保证效果 |
| qwen3.6-flash | 成本和速度优先 | 稳定后切换，或作为降级模型 |
| qwen-vl-max / qwen-vl-max-latest | 旧版/兼容选择 | 不作为新项目首选 |
| qwen-vl-plus | 旧版低成本选择 | 仅作为兼容备选 |

说明：模型名、价格和可用区域变化快，开发时以百炼控制台为准。

### 3.2 OCR

| 方案 | 用途 | 建议 |
| --- | --- | --- |
| Qwen-OCR | 云端 OCR，快速跑通 | 第一版优先，减少本地环境阻力 |
| PaddleOCR | 本地 OCR，低成本 | 后续本地化路线，Windows 环境可能需要适配 |
| 云厂商 OCR | 兜底 | OCR 失败时补充 |

### 3.3 文本模型

| 模型 | 用途 | 建议 |
| --- | --- | --- |
| qwen3.6-flash / qwen-turbo | 巡检汇报生成 | 第一版主力 |
| qwen3.6-plus / qwen-plus | 更复杂文本生成 | 后续升级 |

---

## 4. 成本估算

估算公式：

```text
费用 = input tokens * 输入单价 + output tokens * 输出单价
```

巡检场景量级：

```text
5 张图，每张约 1000-2500 image tokens
系统提示 + 点位模板约 1000-2000 text tokens
事实 JSON 输出约 500-1000 output tokens
```

结论：

- 单次巡检通常是分钱级量级，但必须以真实 `usage` 为准。
- 第一版必须记录 `tokenUsage`。
- Demo 阶段建议单日预算闸门 `10-20 RMB`，超过后切 `MockProvider`。
- 免费额度以百炼控制台为准，不写死。

---

## 5. SDK 安装与配置

### 5.1 OpenAI-compatible Provider

```bash
pip install openai pydantic
```

```bash
DASHSCOPE_API_KEY=sk-xxx
DASHSCOPE_BASE_URL=https://dashscope.aliyuncs.com/compatible-mode/v1
```

注意：

- OpenAI-compatible 可以让业务层保持统一。
- 但图片输入、结构化输出、usage 字段、错误码仍可能有差异。
- 这些差异必须封装在 Provider 内部，不要泄漏到业务后端。

### 5.2 原生 SDK

```bash
pip install dashscope
```

只有在 OpenAI-compatible 模式覆盖不了某些阿里云独有能力时再使用原生 SDK。

---

## 6. 最小调用示例

```python
import base64
import os
from openai import OpenAI

client = OpenAI(
    api_key=os.environ["DASHSCOPE_API_KEY"],
    base_url=os.environ["DASHSCOPE_BASE_URL"],
    timeout=30.0,
    max_retries=1,
)

with open("ups_panel.jpg", "rb") as f:
    img_b64 = base64.b64encode(f.read()).decode()

resp = client.chat.completions.create(
    model="qwen3.6-plus",
    temperature=0.1,
    messages=[
        {
            "role": "system",
            "content": (
                "你是巡检事实提取助手。"
                "只描述图片中的可见事实，不做异常判断，不做设备身份确认。"
                "看不清的字段返回 null，禁止编造数值。"
            ),
        },
        {
            "role": "user",
            "content": [
                {
                    "type": "image_url",
                    "image_url": {
                        "url": f"data:image/jpeg;base64,{img_b64}"
                    },
                },
                {
                    "type": "text",
                    "text": "请提取这张巡检图片中的客观事实。",
                },
            ],
        },
    ],
)

print(resp.choices[0].message.content)
print(resp.usage)
```

说明：

- `usage` 字段不同厂商可能不完全一致，Provider 内要做适配。
- 不要在业务代码里直接依赖某个厂商的原始返回结构。

---

## 7. 结构化输出

### 7.1 推荐做法

第一版要求：

```text
厂商结构化输出能力 + Pydantic 校验 + 失败重试 1 次
```

如果厂商支持 JSON Schema / function calling / tool use，就使用官方方式。若只支持 `json_object`，则使用 prompt 强约束并用 Pydantic 校验兜底。

### 7.2 Schema 示例

```python
schema = {
    "type": "object",
    "properties": {
        "detectedScene": {"type": "string"},
        "detectedImageType": {
            "type": "string",
            "enum": [
                "environment_scene",
                "device_panel",
                "meter_reading",
                "indicator_light",
                "paper_form",
                "other",
                "unknown",
            ],
        },
        "observations": {
            "type": "array",
            "items": {"type": "string"},
        },
        "extractedFields": {
            "type": "object",
            "properties": {
                "temperature": {"type": ["number", "null"]},
                "humidity": {"type": ["number", "null"]},
                "voltageA": {"type": ["number", "null"]},
                "voltageB": {"type": ["number", "null"]},
                "voltageC": {"type": ["number", "null"]},
                "meterReading": {"type": ["number", "null"]},
                "indicatorRed": {"type": ["string", "null"]},
                "indicatorYellow": {"type": ["string", "null"]},
                "indicatorGreen": {"type": ["string", "null"]},
            },
        },
        "quality": {
            "type": "object",
            "properties": {
                "isBlurry": {"type": "boolean"},
                "isOverExposed": {"type": "boolean"},
                "isRotated": {"type": "boolean"},
                "score": {"type": "number"},
            },
            "required": ["score"],
        },
        "confidence": {"type": "number"},
        "needReview": {"type": "boolean"},
    },
    "required": [
        "detectedScene",
        "detectedImageType",
        "observations",
        "extractedFields",
        "quality",
        "confidence",
        "needReview",
    ],
}
```

注意：不同 OpenAI-compatible 厂商对 `response_format` 和 `json_schema` 的支持细节不同。开发时必须按百炼官方文档和实际 SDK 返回验证，不要直接假设完全兼容。

### 7.3 Pydantic 校验

```python
from typing import List, Literal, Optional
from pydantic import BaseModel, ValidationError


class Quality(BaseModel):
    isBlurry: bool = False
    isOverExposed: bool = False
    isRotated: bool = False
    score: float


class ExtractedFields(BaseModel):
    temperature: Optional[float] = None
    humidity: Optional[float] = None
    voltageA: Optional[float] = None
    voltageB: Optional[float] = None
    voltageC: Optional[float] = None
    meterReading: Optional[float] = None
    indicatorRed: Optional[Literal["on", "off", "unknown"]] = None
    indicatorYellow: Optional[Literal["on", "off", "unknown"]] = None
    indicatorGreen: Optional[Literal["on", "off", "unknown"]] = None


class ImageFacts(BaseModel):
    detectedScene: str
    detectedImageType: Literal[
        "environment_scene",
        "device_panel",
        "meter_reading",
        "indicator_light",
        "paper_form",
        "other",
        "unknown",
    ]
    observations: List[str]
    extractedFields: ExtractedFields
    quality: Quality
    confidence: float
    needReview: bool


def parse_facts(raw: str) -> ImageFacts:
    try:
        return ImageFacts.model_validate_json(raw)
    except ValidationError as exc:
        raise ValueError(f"VLM JSON validation failed: {exc}") from exc
```

不要把 `extractedFields` 写成 `Dict[str, Optional[float]]`，因为指示灯字段是 `on/off/unknown` 字符串，会导致校验失败。

---

## 8. 多图调用策略

模型支持一次传多图，但第一版建议：

```text
单次请求 <= 3 张图
超过 3 张拆批
每批失败不影响其他批
最终任务状态可为 partial
```

多图调用示意：

```python
content = []
for url in image_urls:
    content.append({"type": "image_url", "image_url": {"url": url}})

content.append({
    "type": "text",
    "text": "请分别提取每张图的客观事实，并按 imageId 返回。"
})
```

---

## 9. Provider 抽象设计

### 9.1 接口定义

```python
from abc import ABC, abstractmethod
from typing import Any, Dict, List


class VisionProvider(ABC):
    @abstractmethod
    def classify_image(self, image_path: str) -> str:
        ...

    @abstractmethod
    def extract_visual_facts(
        self,
        image_path: str,
        point_ctx: Dict[str, Any],
    ) -> ImageFacts:
        ...


class OcrProvider(ABC):
    @abstractmethod
    def extract_text(self, image_path: str, ocr_type: str) -> Dict[str, Any]:
        ...


class TextProvider(ABC):
    @abstractmethod
    def generate_report(
        self,
        check_items: List[Dict[str, Any]],
        context: Dict[str, Any],
    ) -> str:
        ...
```

### 9.2 目录结构

```text
providers/
├── base.py
├── qwen_vl_provider.py       # qwen3.6-plus / qwen3.6-flash
├── qwen_ocr_provider.py      # Qwen-OCR
├── paddle_ocr_provider.py    # 本地 OCR，后续本地化
├── qwen_text_provider.py     # 汇报生成
├── mock_provider.py          # 演示模式 / 单元测试 / 兜底
└── usage_meter.py            # token / cost 统计
```

### 9.3 切换策略

```python
def get_vision_provider() -> VisionProvider:
    if settings.DEMO_MODE:
        return MockVisionProvider()
    if cost_gate.is_over_budget():
        return MockVisionProvider()
    return QwenVisionProvider(model="qwen3.6-plus")
```

---

## 10. 图片预处理

统一流水线：

```text
HEIC -> JPEG
-> EXIF 旋转
-> 长边缩放到 1568px
-> JPEG q=85
-> 去 EXIF
-> 输入 provider
```

推荐职责：

- 前端：优先做 HEIC 转 JPEG、EXIF 旋转、压缩。
- 后端：再次校验格式、大小、EXIF，并生成 `analyze_path`。
- AI 服务：读取 `analyze_path`，不处理原图。

参数建议：

| 项 | 推荐值 | 说明 |
| --- | --- | --- |
| 长边 | 1568 px | 控制 token 和延迟 |
| JPEG 质量 | 85 | 画质和体积平衡 |
| 单张大小 | <= 10MB | 后端硬限制 |
| 单请求图片数 | <= 3 | 降低超时风险 |

高分辨率图片模式不作为第一版默认能力。是否支持、如何收费，以实际模型文档为准。

---

## 11. 错误处理与降级

### 11.1 错误分类

| 错误类型 | 表现 | 处理 |
| --- | --- | --- |
| 网络/超时 | TimeoutError | 重试 1 次，仍失败标 failed |
| API 限流 | 429 | 退避后重试 1 次 |
| 余额不足 | 计费错误 | 切 MockProvider，记录告警 |
| JSON 校验失败 | Pydantic ValidationError | 让模型修复 1 次，仍失败标 needReview |
| 内容审核拒绝 | 安全错误 | 标 failed，提示人工 |
| 模型不可用 | 5xx | 降级到 qwen3.6-flash，再失败切 MockProvider |

### 11.2 降级链

```text
qwen3.6-plus
  -> qwen3.6-flash
  -> MockProvider
  -> needReview / 人工填写
```

OCR 降级链：

```text
Qwen-OCR
  -> PaddleOCR / 云 OCR 兜底
  -> needReview / 人工填写
```

---

## 12. 成本闸门

```python
import redis
from datetime import date

r = redis.Redis()


def record_usage(cost_rmb: float) -> None:
    key = f"ai:cost:{date.today().isoformat()}"
    r.incrbyfloat(key, cost_rmb)
    r.expire(key, 60 * 60 * 48)


def is_over_budget(daily_limit_rmb: float = 20.0) -> bool:
    key = f"ai:cost:{date.today().isoformat()}"
    cost = float(r.get(key) or 0)
    return cost >= daily_limit_rmb
```

成本估算函数不要写死在业务逻辑里，应按模型配置表维护。

---

## 13. Prompt 模板

### 13.1 事实提取 System Prompt

```text
你是建筑设施巡检事实提取助手。

职责：
- 看图识别场景类型：environment_scene/device_panel/meter_reading/indicator_light/paper_form/other/unknown。
- 描述图片中的客观可见事实。
- 提取可见的数值、文字、状态。
- 评估图片质量：是否模糊、是否过曝、是否旋转。

严格约束：
- 不做异常判断，不输出"是否异常"或"是否合格"。
- 不做设备身份确认，不猜测设备型号、品牌、序列号。
- 看不清的字段必须返回 null。
- 禁止编造数值。
- 输出必须符合给定 JSON Schema，不要任何额外文字。
```

### 13.2 用户 Prompt

```python
user_text = f"""
点位：{point_name}（类型：{point_type}）
图片类型提示：{image_type_hint or "unknown"}

请按 JSON Schema 输出这张图中的客观事实。
"""
```

### 13.3 汇报生成 Prompt

```text
你是巡检汇报正文生成助手。

输入：检查项最终结果列表，包含状态、依据、人工备注。
输出：一段不超过 200 字的中文巡检汇报正文。

要求：
- 客观陈述，不夸大。
- 异常项要明确点出。
- 正常项可以合并表述。
- 待复核项要写明"建议复核"。
- 不要使用"AI识别"等技术术语。
```

---

## 14. 最小上手路径

建议你按这个顺序跑：

1. 注册阿里云百炼，拿 API Key。
2. 用 `qwen3.6-plus` 跑通单张巡检图片事实提取。
3. 加 Pydantic 校验。
4. 换一张模糊图，看模型是否乱编数值。
5. 用 `Qwen-OCR` 或 PaddleOCR 跑一张水表/仪表图。
6. 手写一条规则：湿度 `30-70` 正常。
7. 把事实提取结果丢进规则引擎。
8. 固化一组 MockProvider 返回值。

最小闭环：

```text
图片 -> VLM/OCR facts -> RuleEngine checkResults -> TextProvider report -> 人工确认
```

---

## 15. 二版优化方向

第一版暂不做：

- Prompt caching。
- Batch API。
- 多次采样投票。
- 多模态 embedding 判重。
- 本地视觉大模型。
- 自动人脸/车牌/序列号打码。
- 真离线巡检。

这些都是后续增强，不要放进第一版关键路径。

---

## 16. 参考链接

- 阿里云视觉理解模型：<https://help.aliyun.com/zh/model-studio/vision-understanding/>
- 阿里云模型大全：<https://help.aliyun.com/zh/model-studio/models>
- 阿里云模型价格：<https://help.aliyun.com/zh/model-studio/model-pricing>
- 阿里云 Qwen-OCR：<https://help.aliyun.com/zh/model-studio/qwen-vl-ocr>
- OpenAI Python SDK：<https://github.com/openai/openai-python>
- DashScope Python SDK：<https://github.com/dashscope/dashscope-sdk-python>
- PaddleOCR：<https://github.com/PaddlePaddle/PaddleOCR>
