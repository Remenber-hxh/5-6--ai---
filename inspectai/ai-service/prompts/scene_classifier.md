# 场景分类 — 用户拍照后反推模板

> 触发：用户打开 app 直接拍照（无 record，无模板）
> 模型：`qwen-vl-plus`（速度优先，1-2 秒返回最好）
> 调用：单次 chat completion，1-3 张图打包

## 任务

看用户刚拍的照片，**判断这是哪类巡检场景**，自动选模板，让用户不用先选模板再拍照。

## 候选模板（必须从这个清单选一个）

当前业务入口已恢复会议中心相关模板。必须从下表选择最匹配的一项；如果特征不足，返回 `unknown` 并让用户手动选择。

| templateId | 描述 | 图片特征 |
| --- | --- | --- |
| `zihan_energy` | 能耗抄表 | LCD 电表（蓝底显示 EP/ΣEP/kWh/Wh）或机械字轮水表（黑色字轮读数） |
| `zihan_daily` | 综合巡检 | 强电井 / 配电箱 / 除湿机屏 / 消防泵房环境 |
| `hot_water_room` | 热水机房（历史扩展） | 控制柜 LCD 显示温度（如金锴 Keader）+ 大型水箱 + 水温管 |
| `fire_pump` | 消防泵房（历史扩展） | 红色消防泵 + 不锈钢水箱 + 绿色地坪（典型颜色组合） |
| `water_pump` | 生活水泵房（历史扩展） | 蓝色压力罐 + 不锈钢水箱 + 银色管道 + 压力表 |
| `ups_room` | UPS 机房（历史扩展） | 灰白机房 + UPS 主机柜（带显示屏）+ 电池组排列 + 防静电地板 |
| `power_room` | 变电所 | 高压柜/低压柜阵列、黄黑警示带、有电危险标识、变压器 |
| `escalator` | 扶梯巡检 | 扶梯梯级、扶手带、梳齿板、急停按钮、安全警示牌 |
| `elevator_no_room` | 电梯巡检（无机房） | **轿厢/层站现场**:轿厢内按钮面板、楼层显示屏、轿厢照明、电梯使用登记标志、层门;**全程无独立机房设备** |
| `elevator_machine_room` | 电梯巡检（有机房） | **独立机房**:曳引机、控制柜/控制屏、限速器、盘车手轮、松闸扳手、「盘车手轮/松闸扳手/救援说明」标识牌、「电梯机房 / ELEVATOR MACHINE ROOM」门牌 |
| `unknown` | 无法判断 | 拍到墙、地、天花板、人手、与巡检无关的物体 |

## 输出（严格 JSON）

```json
{
  "templateId": "上面候选之一",
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
- 高压柜阵列（黄黑警示带 + 红色"有电危险"贴纸）→ power_room

### 电梯：有机房 vs 无机房（重点，最容易误判）

两者都叫"电梯巡检"，唯一区别是**有没有拍到独立机房及其专属设备**：

- **`elevator_machine_room`（有机房）** —— 只要出现下列**任一机房专属特征**，立即判有机房（confidence ≥ 0.85）：
  - 曳引机（带绳轮的大电机）、限速器、电梯控制柜 / 控制屏
  - 墙上「盘车手轮 / 松闸扳手 / 救援说明」红字标识牌，或挂着的盘车手轮（橙色圆盘）、松闸扳手（长杆）
  - 门牌写「电梯机房 / ELEVATOR MACHINE ROOM」
  - ⚠️ 机房里通常**还会有火警电话、灭火器、温控器**——这些是机房配套设施，**绝不能因为看到灭火器/火警电话就误判成消防泵房(fire_pump)或综合巡检(zihan_daily)**；只要同时存在上述电梯机房特征，就是 `elevator_machine_room`
- **`elevator_no_room`（无机房）** —— 拍的是**轿厢内或层站现场**，没有独立机房：
  - 轿厢内按钮面板、楼层显示屏、轿厢照明、电梯使用登记标志、层门
  - 画面里**看不到曳引机 / 控制柜 / 盘车手轮 / 救援说明牌**等机房设备

⚠️ **最常见的误判**：机房墙上常装**控制柜显示屏、温控器（如显示 19.0℃）、门禁/EXIT 刷卡面板、开关面板**——这些是机房环境设施，**不是轿厢按钮面板、也不是楼层显示屏**，绝不能因为看到"屏/面板"就判成无机房（elevator_no_room）。无机房的"按钮面板/楼层显示"特指**电梯轿厢内**的乘客操作面板。

**判定顺序（先有机房后无机房）**：先扫画面有没有 曳引机 / 控制柜 / 盘车手轮 / 松闸扳手 / 救援说明牌 / 「电梯机房」门牌——**有任一 → `elevator_machine_room`（有机房），直接定，不再考虑无机房**；只有确认画面是**轿厢内部**（乘客面板 + 楼层数字 + 登记标志，且无任何机房设备）才选 `elevator_no_room`。

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

- 用户可能一次拍多张
- 多张图都是同一场景 → 取置信度最高的那张做主依据
- 多张图分属不同场景（罕见，比如同时拍了电表 + 除湿机）→ 选**首张图**对应的场景，alternatives 里列其他场景，needsManualPick=true
- **电梯特例（重要，覆盖上面的"首张图优先"）**：有机房电梯和无机房电梯**轿厢照片完全一样**，唯一区别是有机房多拍了机房照。所以：**只要这一组里任意一张**出现机房特征（曳引机 / 控制柜 / 盘车手轮 / 松闸扳手 / 救援说明牌 / 「电梯机房」门牌），不管它是第几张、也不管其他几张是不是轿厢，**整组都判 `elevator_machine_room`（有机房）**；只有**全部**图都只有轿厢/层门/乘客面板、且**没有任何一张机房图**时，才判 `elevator_no_room`（无机房）。

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

### 例 5：电梯机房（有机房）— 重点防误判

输入：机房照，墙上红字「盘车手轮 松闸扳手 救援说明」标识牌 + 橙色盘车手轮 + 灰色控制柜；旁边墙上还有火警电话、温控器、门禁面板

```json
{
  "templateId": "elevator_machine_room",
  "templateName": "电梯巡检（有机房）",
  "confidence": 0.93,
  "reason": "见盘车手轮与救援说明牌、电梯控制柜，典型电梯机房",
  "alternatives": [
    {"templateId": "elevator_no_room", "confidence": 0.15}
  ],
  "needsManualPick": false
}
```

> 注意：图里的温控器、门禁/EXIT 面板、火警电话都是机房配套，**不要**据此判成无机房或消防泵房。

## 不要做

- 不要给 templateId 之外的值（这是固定枚举）
- 不要在 reason 里超过 30 字
- 不要返回 retake_required（场景分类失败 ≠ 字段识别失败，不要阻塞用户）
- 不要把会议中心模板误判为紫涵雅集综合巡检；有明确会议中心设备特征时，优先返回对应会议中心模板
