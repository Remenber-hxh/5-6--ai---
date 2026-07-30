# 移动端迁移方案:antd-mobile → Arco Design Mobile

> 状态:**第一层已实施**(2026-07-28,commit `b4dea9c`)。适配层 + 三个危险弹窗路径已浏览器实测,
> 打包体积 55.4kB → 16.3kB gzip。**第二层(观感重做)未开始**,详见文末阶段划分。
> **目标(已确认)**:让移动端更好看。范围仅 `mobile-web`,后台 admin-web 不动。

---

## 一、必须先说清楚的一件事

**只做组件库替换,你几乎看不出变化。**

审计实测:这套移动端 **95% 的界面是手写 CSS**,组件库只承担了 4 样东西。

| 用到的 antd-mobile 组件 | 文件数 |
|---|---|
| Toast | 13 |
| Button | 6 |
| Dialog | 3 |
| Input | 1 |

其余全是自己写的:列表行、卡片、状态标签、取景框、字段行、概览数字、
分组筛选、审批卡……**这些才是你眼睛看到的部分,和用哪个组件库无关。**

所以本方案分两层:

| 层 | 内容 | 视觉收益 | 工作量 |
|---|---|---|---|
| **第一层** | API 迁移(4 个组件换掉) | ≈ 5%,几乎看不出 | 半天 |
| **第二层** | 让 Arco 组件接管手搓 UI + 用它的设计令牌 | **这才是"变好看"** | 2–4 天,可分页推进 |

第一层是第二层的前提,但**单做第一层达不到你的目标**。

---

## 二、第二层:好看在哪里(具体到页面)

盘了一遍现有界面,以下几处是**换成 Arco 组件后观感提升最明显**的:

### 1. 统一顶栏(NavBar)—— 收益最大

**现状问题**:每个页面自己写标题,**没有统一的返回按钮**。从资产详情、审批页只能靠底部
「返回」大按钮走,不符合移动端习惯,也占掉一整条底部空间。

**改用 `NavBar`**:统一标题栏 + 左侧返回 + 右侧操作位。腾出底部空间给主操作,
整体立刻规整。

### 2. 列表行(Cell / Cell.Group)

现在手写了三套长得不一样的列表行:

| 现有手写 | 替换为 |
|---|---|
| `.asset-row`(台账设备行) | `Cell` + `showArrow` |
| `.template-row`(模板选择) | `Cell.Group` |
| `.task-card`(任务卡) | `Cell` + 自定义右侧 |

统一后:间距、分割线、点按反馈、箭头全部由组件保证,**不再三处各写一套**。

### 3. 流程进度(Steps)

**现状问题**:旧版有顶部进度条,新版**完全没有**——巡检员不知道自己走到哪一步了。

用 `Steps` 展示「选照片 → 确认场景 → 填日报 → 提交」,既好看又解决真实困惑。

### 4. 状态标签(Tag)

现在 `.ar-tag` / `.task-status` / `.cr-tt` 是三套手写的标签样式,颜色对不齐。
统一用 `Tag`,配色由设计系统给。

### 5. 空状态与结果页(Result)

现在的空状态是一行灰字("暂无待办巡检任务")。Arco 的 `Result` 带插图与主操作按钮,
观感差距明显。提交成功页也该用它。

### 6. 主题令牌

Arco 支持主题变量注入。现在的配色是我从旧版抠出来的硬编码值,
接进 Arco 主题后可**统一调一次**,而不是散在 CSS 里。

> **一个必须保留的东西**:登录页和拍照台的深色科技风(玻璃卡片 + 翡翠渐变)
> 是你确认过要跟旧版一致的。这两屏建议**保留自定义样式**,只换里面的组件;
> 其余流程页(浅色 iOS 风)全面交给 Arco。

---

## 三、第一层:API 迁移(已按官方文档核对)

### ⚠️ 坑一:`Dialog.confirm` 从 Promise 变回调 —— 最危险

| | antd-mobile | Arco |
|---|---|---|
| 返回 | **`Promise<boolean>`** | **`{ close, update }`,非布尔** |
| 文案 | `confirmText` | `okText` |

现有 5 处全是 `const ok = await Dialog.confirm(...); if (!ok) return;`
直接换包会**静默失效**:`await` 非 Promise 立刻得真值,于是
**「删除照片」「提交日报」跳过确认直接执行**。不报错、不崩,只是确认框形同虚设。

### ⚠️ 坑二:`Input.onChange` 是失焦才触发

Arco 逐字触发的是 **`onInput(e, value)`**;`onChange` 要等失焦,且参数是 `(事件, 值)`。
原样映射会让**登录框打字没反应**。

### 坑三:Toast 不自动去重

antd-mobile 的 `Toast.show` 自动替换上一条;Arco 需手动 `close()` 前一个实例,
否则连续操作会叠出一堆。方法名也不同:`Toast.show` → `Toast.toast` / `.info`,
位置参数 `position` → `direction`。

### 其他

- `Button`:**无 `block`**(默认块级,行内用 `inline`);`color` → `type="primary"|"ghost"|"default"`
- 样式前缀:`.adm-*` → `.arco-*`(现有 CSS 只有 3 处)

### 做法:加适配层,不要散着改

新建 `src/ui/`,把 Arco 包一层并**保持现有调用签名**:

```diff
- import { Button, Dialog, Toast } from "antd-mobile";
+ import { Button, Dialog, Toast } from "@/ui";
```

业务代码每个文件只改一行,API 差异集中在 4 个小文件。关键实现:

```ts
// dialog.ts —— 回调式包回 Promise,业务层 await 写法不用动
export const Dialog = {
  confirm(cfg) {
    return new Promise<boolean>((resolve) => {
      ArcoDialog.confirm({
        content: cfg.content,
        okText: cfg.confirmText ?? "确定",
        cancelText: cfg.cancelText ?? "取消",
        onOk: () => resolve(true),
        onCancel: () => resolve(false),
        onClose: () => resolve(false),  // 点遮罩关闭也要 resolve,否则 await 永久挂起
      });
    });
  },
};

// toast.ts —— 补上 Arco 没有的单实例去重
let cur = null;
export const Toast = {
  show(cfg) {
    cur?.close();
    cur = ArcoToast.toast({
      content: cfg.content,
      duration: cfg.duration ?? 2000,
      direction: cfg.position ?? "center",   // position → direction
    });
  },
};
```

> `onClose` 那行是关键:只写 `onOk`/`onCancel`,用户点遮罩关闭时 Promise 永不 resolve,
> 页面会卡在"等待确认"。

---

## 四、执行顺序(每步可独立验收、可回退)

### 第一层(先做,半天)

| 步 | 内容 | 验证 |
|---|---|---|
| 0 | 建分支;基线:`tsc` 零错 + 走通登录/拍照/提交 | 现状全绿 |
| 1 | 装 Arco,**与 antd-mobile 并存**,先不卸载 | dev 能起 |
| 2 | 写 `src/ui/` 适配层 + 单测(确定/取消/点遮罩三条路径) | 单测通过 |
| 3 | 13 个文件改 import(每文件一行) | `tsc` 零错误 |
| 4 | CSS 前缀 `.adm-*` → `.arco-*` | 截图比对 |
| 5 | 回归清单(见下) | 全通过 |
| 6 | 卸载 antd-mobile;清 `vite.config.ts` 里写死的 `antd: ["antd-mobile"]` | 构建正常 |

**第 1 步"两库并存"是关键**:任何一步出问题都能立刻切回,不会卡在"两边都不能用"。

### 第二层(视觉改造,按页推进,随时可停)

按**收益从高到低**排,每做完一页就能看到效果:

| 顺序 | 内容 | 为什么排这个位置 |
|---|---|---|
| 1 | **NavBar 统一顶栏** | 一次改动,所有页面受益;顺带解决"没有返回按钮" |
| 2 | **列表页 Cell 化**(台账 / 模板 / 任务) | 三套手写合并成一套,视觉统一 |
| 3 | **Tag 统一状态标签** | 颜色语义一次对齐 |
| 4 | **Steps 流程进度** | 补回旧版有、新版缺的进度感知 |
| 5 | **Result 空状态 / 成功页** | 观感提升明显,工作量小 |
| 6 | 主题令牌接管配色 | 收尾,让后续调色只改一处 |

---

## 五、必须回归的清单(换库最容易静默坏的地方)

1. **删除照片确认框** —— 点"取消"必须真的不删
2. **提交日报确认框** —— 同上;点遮罩关闭不能卡死
3. **登录输入框** —— 逐字输入实时更新
4. **连续 Toast** —— 快速操作两次不应叠出两条
5. **按钮满宽** —— 底部主按钮不能变成行内小按钮
6. **loading 态** —— 提交中转圈且不可重复点
7. **深色两屏** —— 登录页玻璃卡片、拍照台取景框观感不变

---

## 六、成本与风险

| | 第一层 | 第二层 |
|---|---|---|
| 工作量 | 半天(含回归) | 2–4 天,可分页停 |
| 主要风险 | `Dialog.confirm` 静默失效 → 适配层 + 单测封死 | 视觉走样 → 每页截图比对 |
| 回退 | 极低:业务只依赖 `@/ui` | 按页 revert |

**包体积**:Arco 解包约 15MB(含全部组件与图标),必须按需引入,
否则产物会明显变大。第 1 步装完就要量一次构建产物做基线。

---

## 七、建议

**先做第一层,再从第二层的 NavBar 开始。**

理由:NavBar 是"一次改动、全页受益"的那类改动,做完你立刻能看到整体规整度的变化,
再决定要不要继续往下推。如果观感提升不明显,及时止损的成本也最低。

---

## 附:审计原始数据

```
组件使用(grep 实测):
  Toast   13 文件
  Button   6 文件
  Dialog   3 文件 / 5 处调用
  Input    1 文件(LoginPage)

CSS 覆盖:.adm-button / .adm-input / .adm-input-element(11 行)

Arco:@arco-design/mobile-react@2.39.1
peer:react >=16.9(当前 18.3,兼容)
解包体积:约 15MB(需按需引入)
```
