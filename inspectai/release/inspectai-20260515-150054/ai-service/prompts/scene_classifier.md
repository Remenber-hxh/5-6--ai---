# 场景分类 — 用户拍照后反推模板

> 触发：用户打开 app 直接拍照（无 record，无模板）
> 模型：`qwen-vl-plus`（速度优先，1-2 秒返回最好）
> 调用：单次 chat completion，1-3 张图打包

## 任务

看用户刚拍的照片，**判断这是哪类巡检场景**，自动选模板，让用户不用先选模板再拍照。

## 候选模板（必须从这个清单选一个）

当前生产入口优先只返回 `zihan_energy`、`zihan_daily`、`unknown`。会议中心相关模板是历史扩展场景，除非图片特征极其明确，否则不要在自动分类里返回会议中心模板，避免把紫涵雅集巡检误分流。

| templateId | 描述 | 图片特征 |
| --- | --- | --- |
| `zihan_energy` | 能耗抄表 | LCD 电表（蓝底显示 EP/ΣEP/kWh/Wh）或机械字轮水表（黑色字轮读数） |
| `zihan_daily` | 综合巡检 | 强电井 / 配电箱 / 除湿机屏 / 消防泵房环境 |
| `hot_water_room` | 热水机房（历史扩展） | 控制柜 LCD 显示温度（如金锴 Keader）+ 大型水箱 + 水温管 |
| `fire_pump` | 消防泵房（历史扩展） | 红色消防泵 + 不锈钢水箱 + 绿色地坪（典型颜色组合） |
| `water_pump` | 生活水泵房（历史扩展） | 蓝色压力罐 + 不锈钢水箱 + 银色管道 + 压力表 |
| `ups_room` | UPS 机房（历史扩展） | 灰白机房 + UPS 主机柜（带显示屏）+ 电池组排列 + 防静电地板 |
| `unknown` | 无法判断 | 拍到墙、地、天花板、人手、与巡检无关的物体 |

## 输出（严格 JSON）

```json
{
  "templateId": "上面 7 个之一",
  "templateName": "中文模板名",
  "confidence": 0.0,
  "reason": "判断依据，<30字",
  "alternatives": [
    {"templateId": "次优候选 1", "confidence": 0.0},
    {"templateId": "次优候选 2", "confidence": 0.0}
  ],
  "needsManualPick": true
}
```

`needsManualPick=true` 触发条件（前端见到这个就弹"AI 不太确定，请手动选模板"）：
- `confidence < 0.7`
- 或 `templateId="unknown"`
- 或 alternatives 第一个的 confidence 与 templateId 的差 < 0.15（说明 AI 在两个之间犹豫）

## 判断时的关键线索

### LCD 电表 vs 控制柜屏 — 容易混

- 电表（zihan_energy）：屏小、字符显示"EP/EQ/Ia/Ub"等电量标识，背景蓝、字符白
- 除湿机屏（zihan_daily）：屏大、显示"湿度% 温度℃"两个圆环数字
- 控制柜屏（hot_water_room）：屏中等、显示"设定温度 / 太阳能温度 / 加热定时"等中文标签

### 机房环境照 — 看颜色组合

- 红色泵 + 绿色地坪 → fire_pump（消防泵房）
- 蓝色压力罐 + 不锈钢水箱 → water_pump（生活水泵房）
- 灰白机房 + 金属机柜 + 电池组 → ups_room
- 高压柜阵列（黄黑警示带 + 红色"有电危险"贴纸）→ 紫涵雅集没这场景，归 zihan_daily 或 unknown

### 强电井（zihan_daily 子场景）

- 拍到配电箱内部（断路器排列）→ zihan_daily
- 拍到强电井除湿机屏 → zihan_daily（不是 hot_water_room）
- 关键区分：除湿机屏会标"配电房 除湿机"等文字

### 水表 vs 电表

- 机械字轮（白底黑红数字旋转） → zihan_energy（生活水表）
- LCD 数字 → zihan_energy（Z1-Z4 能耗表）
- YKE 蓝屏、EP 标识、上下两行数字 → zihan_energy
- 水表盖上有 Q1/Q2/Q3 或二维码，但主体是机械字轮 → zihan_energy

## 多图怎么处理

- 用户可能一次拍多张（最多 3 张前置分类，能耗抄表场景实际可能拍 5 张但分类只看前 3 张）
- 多张图都是同一场景 → 取置信度最高的那张做主依据
- 多张图分属不同场景（罕见，比如同时拍了电表 + 除湿机）→ 选**首张图**对应的场景，alternatives 里列其他场景，needsManualPick=true

## 失败判定

不要返回 retake_required（场景分类不应阻塞主流程）。
读不出场景就返回 `templateId="unknown"`，让前端走"手动选模板"路径。

## 完整输出示例

### 例 1：明确的能耗表

输入：1 张电表 LCD 屏（EP 1756 / 3304k）

```json
{
  "templateId": "zihan_energy",
  "templateName": "紫涵雅集能耗抄表",
  "confidence": 0.94,
  "reason": "LCD屏显示EP电能标识符，蓝底白字典型能耗表特征",
  "alternatives": [],
  "needsManualPick": false
}
```

### 例 2：除湿机屏（容易和电表混）

输入：1 张除湿机屏（湿度 34% 温度 21℃）

```json
{
  "templateId": "zihan_daily",
  "templateName": "紫涵雅集每日综合巡检",
  "confidence": 0.88,
  "reason": "屏显示湿度%和温度℃双圆环，是除湿机典型UI",
  "alternatives": [
    {"templateId": "hot_water_room", "confidence": 0.32}
  ],
  "needsManualPick": false
}
```

### 例 3：UPS 机房环境（无明显特征）

输入：1 张机房地面 + 灭火器柱 + 远处机柜

```json
{
  "templateId": "ups_room",
  "templateName": "会议中心UPS机房巡检报批",
  "confidence": 0.65,
  "reason": "机房环境照，可见机柜阵列，疑似UPS机房",
  "alternatives": [
    {"templateId": "zihan_daily", "confidence": 0.55}
  ],
  "needsManualPick": true
}
```

### 例 4：完全无法判断

输入：1 张墙壁照

```json
{
  "templateId": "unknown",
  "templateName": "无法识别",
  "confidence": 0.2,
  "reason": "图片仅显示墙面，无可识别设备",
  "alternatives": [],
  "needsManualPick": true
}
```

## 不要做

- 不要给 templateId 之外的值（这是固定枚举）
- 不要在 reason 里超过 30 字
- 不要返回 retake_required（场景分类失败 ≠ 字段识别失败，不要阻塞用户）
- 不要做"扶梯/电梯/变电所"的分类（这些未纳入当前模板范围，AI 看到这类直接归 unknown）
