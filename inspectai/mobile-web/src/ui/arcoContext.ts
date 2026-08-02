import type { GlobalContextParams } from "@arco-design/mobile-react/esm/context-provider";

// ===== Arco 全局上下文 =====
//
// 【为什么必须固定 system】
// Arco 按 UA 判平台,给组件挂 .ios / .android / .pc 类名,而它的样式
// **只写了 .ios 和 .android 两套,没有 .pc**。在桌面浏览器里打开
// (开发自测、企业微信 PC 端)会被判成 pc,于是 button / dialog / image /
// input / popover / tabs 这 6 个组件完全没有样式 —— 弹窗表现为一个白框、
// 文字直接继承 52px 的根字号、占满整屏。
//
// 【为什么命令式 API 要单独传】
// <ContextProvider system="ios"> 只覆盖 React 树内的组件。
// Dialog.confirm() / Toast.toast() 是命令式的,渲染到树外的 portal,
// 拿不到树里的 context —— 它们的签名第二个参数就是给这个用的:
//   confirm(config, context?)   toast(config, context?)
// 少传一处,那一处就退回 pc 无样式。
//
// 选 ios 而不是 android:本项目设计语言本来就是 iOS 风(流程页沿用旧版的
// 浅色 iOS 卡片),而且移动端产品不该因为"用什么浏览器打开"而变形。
//
// 【实际影响面 —— 扫过全部在用组件的编译产物,别再逐个猜】
//   组件            平台分支条数   其中影响尺寸/布局
//   dialog          52             10  ← 只有它真受影响,pc 下等于完全没样式
//   input            8              0     (只管 border,且适配层传了 border="none")
//   image            2              0     (只管 bordered 变体的 border)
//   button           1              0     (只管 ghost+half-border 的 border)
//   其余在用组件(avatar/badge/nav-bar/cell/notice-bar/progress/
//   pull-refresh/skeleton/loading/toast)平台分支为 0
//
// 结论:固定 system 是为弹窗修的,不会连带改变其他组件的排版。
// 若将来引入 popover / tabs,记得重跑这个扫描 —— 它们也有平台分支。
export const ARCO_CONTEXT: GlobalContextParams = { system: "ios" };
