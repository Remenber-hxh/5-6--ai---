# Claude 现场问题反馈：转人工与重拍弹窗

日期：2026-05-12  
反馈来源：现场使用截图与当前代码检查  
处理要求：本文件只记录问题和建议，不改代码。

## 1. 现场现象

当前进入服务后，页面直接弹出：

```text
需要重新拍照
图片质量不足，请重新拍摄
已尝试 0 / 3 次
```

同时弹窗里的“转人工填写”按钮不能正常使用。

这两个现象说明当前“重拍 / 转人工”流程没有按产品预期运行。

## 2. 预期流程

正确流程应该是：

1. 用户刚进入系统时，只展示拍照入口，不应立刻弹出“图片质量不足”。
2. 用户拍照后，系统先做场景识别。
3. 如果场景识别不确定，应进入“手动选择模板”，而不是直接弹出“图片质量不足”。
4. 创建巡检记录后，再进行字段识别。
5. 字段识别失败时，才提示重新拍照。
6. 重拍累计超过 3 次后，系统应自动进入人工填写。
7. 用户主动点击“转人工填写”时，也应可以直接进入人工表单。

## 3. 当前问题判断

### 问题一：刚进入服务就弹“质量不足”

截图显示“已尝试 0 / 3 次”，但已经出现重拍弹窗。

这不符合三次重拍逻辑。  
0 次尝试时不应该提示图片质量不足，更不应该进入失败弹窗。

建议 Claude 优先排查：

- 页面初始化时是否错误恢复了上一次未完成记录。
- `localStorage.activeRecord` 是否残留了旧记录，导致刷新后进入异常状态。
- 初始化逻辑是否应该清理 `retakeModal` 的显示状态。
- `retakeModal` 是否只依赖 HTML hidden 状态，缺少启动时强制隐藏。
- 是否把“场景识别失败”和“字段识别图片质量不足”混在一起处理了。

相关代码位置：

- `frontend/app.js:612`：`init()` 初始化逻辑。
- `frontend/app.js:621`：恢复 `localStorage.activeRecord`。
- `frontend/app.js:628`：识别成功或人工状态恢复表单。
- `frontend/app.js:631`：其他状态回到拍照页。
- `frontend/index.html:104`：重拍弹窗 DOM。

建议结果：

- 首次进入系统必须稳定停留在拍照页。
- 初始化时无论是否有历史状态，都不应该直接展示重拍弹窗。
- 如果存在历史失败记录，应给出“继续填写 / 重新开始”的明确选择，不要默认弹质量不足。

## 4. 转人工按钮不可用的原因判断

当前前端“转人工填写”按钮逻辑是：

```javascript
$("#retakeManualBtn").addEventListener("click", async () => {
  hideRetakeModal();
  await setManualMode();
  showManualHint();
});
```

而 `setManualMode()` 内部依赖：

```javascript
state.record = await API.enableManual(state.record.id);
```

问题在于：如果弹窗出现在“还没创建巡检记录”的阶段，`state.record` 为空，或者没有 `state.record.id`，那么“转人工填写”必然不能用。

相关代码位置：

- `frontend/app.js:282`：`setManualMode()`。
- `frontend/app.js:353`：重拍弹窗“转人工填写”按钮。
- `go-backend/cmd/server/handlers.go:443`：后端人工模式接口。

建议 Claude 修正：

- 如果已经有 `state.record.id`，点击“转人工填写”应调用 `/api/inspection/records/{id}/manual`，然后进入表单页。
- 如果还没有 `state.record.id`，点击“转人工填写”不应调用 manual 接口，而应跳转到“手动选择模板”。
- 用户选定模板后，先创建记录，再进入人工填写表单。
- 前端必须对 `state.record` 是否存在做防御判断，不能默认一定有记录。

## 5. 场景识别失败和字段识别失败要拆开

当前产品逻辑里有两类失败：

- 场景识别失败：不知道是哪类设备 / 哪张日报模板。
- 字段识别失败：知道模板，但图片看不清或字段读不出来。

这两类失败不应该共用同一个“图片质量不足”弹窗。

建议交互：

| 阶段 | 失败表现 | 应该进入 |
| --- | --- | --- |
| 场景识别失败 | 不确定模板 | 手动选择模板 |
| 字段识别失败 1-3 次 | 图片读数不稳定 | 重新拍照 |
| 字段识别失败超过 3 次 | 无法稳定识别 | 人工填写 |
| 用户主动点转人工 | 不想继续拍 | 人工填写或先选模板 |

## 6. 后端返回结构也要统一

当前 `handleClassifyScene` 在 AI classify 出错时可能返回裸的 `SceneClassifyResult`，成功时返回：

```json
{
  "classify": {},
  "images": []
}
```

这会让前端在异常分支里拿不到稳定的 `result.classify` / `result.images`。

相关代码位置：

- `go-backend/cmd/server/handlers.go:547`：调用 AI classify。
- `go-backend/cmd/server/handlers.go:549`：classify 异常分支。
- `go-backend/cmd/server/handlers.go:567`：classify 成功分支。
- `frontend/app.js:207`：前端读取 classify 返回。
- `frontend/app.js:208`：使用 `result.classify`。
- `frontend/app.js:209`：使用 `result.images`。

建议：

无论成功失败，`/api/scene/classify` 都返回同一种结构：

```json
{
  "classify": {
    "templateId": "unknown",
    "needsManualPick": true,
    "reason": "AI 无法确定模板，请手动选择"
  },
  "images": [],
  "tmpDir": "..."
}
```

这样前端可以稳定进入“手动选择模板”，而不是误触发重拍弹窗。

## 7. 建议修正优先级

第一优先级：

- 修复“刚进入系统就弹质量不足”的初始化问题。
- 初始化时强制隐藏 `retakeModal`。
- 清理或校验 `localStorage.activeRecord`，不要恢复异常失败状态。

第二优先级：

- 修复“转人工填写”按钮。
- 如果无 `state.record.id`，先进入手动模板选择。
- 如果有 `state.record.id`，再调用后端 manual 接口。

第三优先级：

- 拆分“场景识别失败”和“字段识别失败”。
- 场景失败进模板选择。
- 字段失败才走重拍 / 三次转人工。

第四优先级：

- 统一 `/api/scene/classify` 返回结构。
- 避免前端在失败分支读不到 `result.classify`。

## 8. 给 Claude 的一句话要求

请先把“刚进入系统误弹质量不足”和“转人工按钮依赖不存在的 record id”这两个问题修掉。  
这两个问题会直接影响领导演示，因为用户第一眼看到的不是拍照入口，而是错误兜底弹窗。
