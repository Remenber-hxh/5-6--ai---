# Prompt 资源目录

> 由 Claude 维护，codex 负责加载和拼接，不要修改 prompt 内容。
> 修改 prompt 后无需重启 ai-service（如果 codex 用文件 watch 加载）；保险起见还是重启一次。

## 文件清单

| 文件 | 用途 | 调用模型 | 触发模板 |
| --- | --- | --- | --- |
| `_common.md` | 所有视觉 prompt 共享的输出 schema、失败判定 | — | 作为 SYSTEM 拼到所有视觉调用前 |
| `energy_meter.md` | 能耗抄表（电表/水表 OCR） | qwen-vl-max / qwen-vl-plus | `zihan_energy` |
| `screen_reading.md` | LCD 屏读数（除湿机、控制柜） | qwen-vl-max / qwen-vl-plus | `zihan_daily`、`hot_water_room` |
| `paper_form.md` | 纸质巡视表 OCR（黑马场景） | qwen-vl-max | `?paperOCR=1` 触发，覆盖任意机房模板 |
| `summary.md` | 字段确认后生成总结正文 | qwen-plus（纯文本） | 所有模板，提交时 |

## 加载策略（codex 实现指南）

```python
# ai-service/run.py 推荐做法

PROMPTS_DIR = os.path.join(os.path.dirname(__file__), "prompts")

def load_prompt(name):
    with open(os.path.join(PROMPTS_DIR, f"{name}.md"), encoding="utf-8") as f:
        return f.read()

COMMON = load_prompt("_common")

def prompt_for_template(template_id, paper_ocr=False):
    if paper_ocr:
        return load_prompt("paper_form")
    return {
        "zihan_energy":   load_prompt("energy_meter"),
        "zihan_daily":    load_prompt("screen_reading"),
        "hot_water_room": load_prompt("screen_reading"),
    }.get(template_id)  # None → 走通用 fallback 或拒绝调用
```

调用时拼接：
- `system = COMMON`
- `user = scenario_prompt + "\n\n字段清单：\n" + format_fields(template.fields) + "\n\ncurrent_date: " + today + "\n\n（图片在 image_url 中）"`

`current_date` 字段对 paper_form.md 必须，其他场景可选。

## 模板 → prompt 未覆盖怎么办

第一版只做 3 个主模板，对应 prompt 已写。其他 7 个模板（UPS / 变电所 / 消防泵 / 生活水泵 / 扶梯 / 电梯 ×2）走兜底策略：
- 千问端不调用，直接返回 `recognitionStatus=manual_required`，原因="该模板暂未启用 AI 识别，请直接人工填写"
- 不要硬塞通用 prompt（会出难看的假结果）

## 调试

每个 prompt 文件结尾都有"完整输出示例"，开发时直接拿示例回灌测前端渲染逻辑：

```bash
# 把示例的 JSON 部分提取出来，curl 灌给 ai-service 走本地接口模拟
```

## 修改 prompt 的协作约定

- Claude 改 prompt → 在本 README 顶部加一行变更日志（日期 / 改动 / 影响）
- Codex 改加载逻辑 → 不要改 prompt 文件本身
- 互相 review 时打 `@`

## 变更日志

- `[2026-05-11 14:55 claude]` 初版 5 个 prompt + 加载约定
