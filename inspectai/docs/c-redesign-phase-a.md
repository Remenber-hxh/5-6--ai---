# C 重设 · Phase A — 视觉/IA/概念 三件齐做(2026-06-01)

> 用户反馈:阶段一虽然功能齐全,但 13 个 nav + 9 块看板 + 多套交叉概念 让信息密度过高、demo 时容易迷路。
> 走方案 C(整体重设)。Phase A 是基础工程,不动具体页内容,只重做视觉系统、信息架构、概念语义。
> 后续 Phase B-D 在此基础上重做具体页面、移动端等。

## Phase A 目标

1. 配色压到 灰阶 + 1 强调红(异常)+ 1 警示黄(待复核)
2. 字体层级 4 级,间距 8px grid
3. 导航 13 → 5/8 项(主管视角 5 / 管理员视角 8)
4. 所有 status/pill/risk/level/priority 体系全局对齐到 3 状态语义

## 实施清单

### A1 视觉 token 系统
- **新建** `styles.css` 头部 `:root` 设计 token 块:
  - 灰阶:`--ds-ink-strong/-ink/-mid/-soft/-faint`
  - 强调:`--ds-danger / --ds-warn / --ds-brand`
  - 字体 4 级:`--ds-font-1/2/3/4/5`
  - 8px 间距:`--ds-s1..s7`
  - 圆角/影子
- **重指向** legacy 变量(`--blue/--green/--orange/--red/--cyan`)到新 token,平滑切换不破坏现有 CSS

### A2 角色化 nav 重构
- **改 HTML** `index.html` nav:13 项 → 5 主视角项 + 3 管理员追加项 + 6 路由保留(`hidden`)
- 主管 5 项:首页 / 待办 / 资产 / 记录 / 洞察台
- 管理员 +3 项:用户 / 操作日志 / 系统
- 删 `首页/个人首页` 二重、`巡检计划/任务` 合到隐藏路由、`资产台账/设备管理` 留资产、`异常复核/修改审批` 合到"待办"
- **改 CSS** `data-role-only="admin"` 默认隐藏,`body.role-admin` 时显示
- **改 JS** 新增 `applyRoleBodyClass()`,登录/loadMe 时按 `currentUser.roleCode` 切 body class

### A3 概念语义统一(styles.css 末尾 250+ 行 override)
- `.status / .pill .normal/.warning/.danger` 全部对齐到 token 3 色
- `.ai-hero / .data-hero / .login-card` 等深色渐变 hero → 白卡 + 浅边
- `.quick-tile` 各色 → 全灰
- `.risk-kpi.normal/.warning/.danger` 边条 3 色
- `.focus-card.focus-{normal,warning,danger}` 边条 3 色
- `.trend-rate / .drift-rate / .se-fill` 漂移与状态事件徽章统一
- `.iq-table td.warn/.danger` 巡检员防惰性指标
- `.pc-value.{danger,ok,neutral}` 异常对比 KPI
- `.confirm-row` 复核留痕
- `.load-error-banner` 三态错误
- `.insight-hero` 数据看板 Hero
- `.ai-chat-model / .ai-chat-chip` 主管聊天面板
- `.aside-finding / .dash-aside-card / .health-card / .ai-month-card` 仪表盘右侧卡
- `.todo-row.danger/.warn/.info` 仪表盘待办行
- `.ai-hero-hi / .ai-hero-line` text gradient 修正(原 `-webkit-text-fill-color: transparent` 让文字看不见)

## 验收

### 视觉
- 全屏只剩 3 种强调色(异常红 / 待复核黄 / 链接蓝),其它一律灰
- 极简谷歌风:白底 + 浅边 + 黑字 + 极淡影子
- 字体层级清晰:页面标题 22px / 块标题 16px / 正文 14px / 辅助 12px

### IA
- 主管默认看 5 nav 项,无重叠职责
- admin 用户看 8 nav 项,多出 用户/日志/系统
- 旧路由(plan/task/device/approval/profile/report)仍可通过 URL 直达,只是 nav 入口收紧

### 截图
- `c-phase-a1-dashboard.png` — A1 基线
- `c-phase-a2-clean.png` — A2 nav 重构后
- `c-phase-a3-final.png` — A3 首页极简谷歌风
- `c-phase-a-data-board.png` — 数据看板全功能 + A3 配色

## Phase A 未覆盖的小瑕疵(留 B 时一起处理)

1. Top N 重点关注卡 第 1 名仍有深色高亮 styling 没被 override(focus-card 在 first-child 时有特殊渲染)
2. JADEAST sidebar logo "智巡后台" 字间距导致末字被裁
3. 资产详情侧栏的 `asset-side-section` 头部样式(Vesper 自己加的 454 行)未经过 A3 token 对齐
4. 仪表盘 quick-tiles 的数字仍用了原色(11/13/100 等),数字本身的 color 没强制成 ds-ink-strong

## 文件改动
- `inspectai/admin-frontend/index.html` — nav 重构 + cache buster
- `inspectai/admin-frontend/styles.css` — token 系统 + A3 override(共 +250 行)
- `inspectai/admin-frontend/app.js` — applyRoleBodyClass()

## Phase A 之后

按方案 C 三阶段执行顺序:
- **Phase B** 首页主舞台合并 — dashboard + data board 合一,5 块封顶
- **Phase C** 资产/记录/待办页 简化
- **Phase D** 移动端 简化(去横幅、改 toast)
