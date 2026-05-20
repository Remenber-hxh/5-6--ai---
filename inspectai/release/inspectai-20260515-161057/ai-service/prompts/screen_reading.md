# 屏幕读数场景 — 强电井除湿机 + 热水机房控制柜

> 适用模板：`zihan_daily`（紫涵综合，强电井部分）+ `hot_water_room`（热水机房控制柜）
> 输入图片：通常 1-3 张，照设备 LCD 屏
> 关键挑战：屏可能多页循环，要识别"温度 / 湿度 / 时间戳"等多个数值

## 任务

从设备 LCD 屏照片读出温度、湿度、控温目标等数值，并对照阈值判断是否正常。

## 字段映射约定

不同模板的字段不同，按字段清单 code 给：

### 紫涵综合 — 强电井除湿机
| code | label | kind | 阈值 |
| --- | --- | --- | --- |
| `strong_room_01` | 强电井室内情况 | choice | 温度<40℃ 且 湿度<70% → "正常"；任一超 → "异常" |
| `temperature` | 温度℃（如清单里有） | number | — |
| `humidity` | 湿度% | number | — |

### 热水机房控制柜
| code | label | kind | 阈值 |
| --- | --- | --- | --- |
| `cabinet_temperature` | 控制柜显示温度 | number | 看着即可，不做正常异常判定 |

## 怎么读屏

### 强电井除湿机（湿博 SHITENG 等品牌）

典型屏：左右两个圆环数字，分别是湿度（湿度% 标注在下方）和温度（温度℃ 标注在下方），顶部时间戳。

```text
04/29 15:13                ← 时间戳，不读
   ⌒              ⌒
  34            21
湿度%          温度℃
```

→ `humidity=34, temperature=21`，对照阈值 → `strong_room_01="正常"`

### 热水机房控制柜（金锴 Keader）

屏上多个数字交替显示：

```text
98  60  --                 ← 三个数字
设定温度  太阳能温度  ?    ← 标识
 21:00                     ← 加热时段定时
```

只取**实际温度**（屏上标"太阳能温度"或"控温温度"那个），填到 `cabinet_temperature`。
设定温度（标"设定温度"的）不填，除非清单里有 `target_temperature` code。

## 数值读取注意

- 七段数码管 OCR 容易混 `B/8`、`6/8`、`0/D`，置信度按字符可读性给
- 屏可能闪烁，照片是某一帧；如果数字在切换中（半个数字+下一个数字叠加）→ confidence 降到 0.5，warnings 里提示
- 温度显示 "--" 或 "ER" / "Err" → 不填，warnings 提示"屏幕显示错误码 XX"

## 阈值判定（正常/异常）

强电井除湿机的 `strong_room_01` 字段是 choice：

```
温度 < 40℃ AND 湿度 < 70% → "正常"
温度 ≥ 40℃ OR  湿度 ≥ 70% → "异常"
读不出温湿度任一 → 不填该字段（让人工选）
```

reason 字段里给依据，例：`"温度21℃<40, 湿度34%<70, 判定正常"` 或 `"湿度78%>70, 判定异常"`

## 失败判定

- 屏完全熄灭 / 黑屏 → retake_required，reason="设备屏幕未亮，请检查通电后重拍"
- 屏只看到外壳，没拍到显示区 → retake_required
- 屏被反光遮挡看不到数字 → retake_required
- 拍到的是空白墙或无关物体 → retake_required

## 完整输出示例

### 例 1：强电井除湿机正常

输入：1 张除湿机屏照（湿度 34，温度 21）
字段清单包含：`strong_room_01`（choice 正常/异常）

```json
{
  "recognitionStatus": "recognized",
  "retakeReason": "",
  "observations": [
    "图1：湿博SHITENG屏显示 湿度34% 温度21℃，时间戳04/29 15:13"
  ],
  "recognizedFields": [
    {"code": "strong_room_01", "value": "正常", "confidence": 0.94, "reason": "湿度34%<70, 温度21℃<40, 双指标均在正常范围"}
  ],
  "warnings": []
}
```

### 例 2：热水机房控制柜

输入：1 张金锴控制柜屏照
字段清单包含：`cabinet_temperature`（number）

```json
{
  "recognitionStatus": "recognized",
  "retakeReason": "",
  "observations": [
    "图1：金锴Keader控制柜屏显示 设定温度98 太阳能温度60，加热定时21:00"
  ],
  "recognizedFields": [
    {"code": "cabinet_temperature", "value": "60", "confidence": 0.91, "reason": "屏幕'太阳能温度'标识下方读数为60℃"}
  ],
  "warnings": []
}
```

## 不要做

- 不要把时间戳填到任何字段
- 不要看到红色 LED 就判"异常"——红色 LED 在很多设备上是普通运行指示灯
- 不要把 choice 字段填成 "34" 或 "21℃"——温湿度数值要填到 number 字段，正常异常判定要填到 choice 字段
