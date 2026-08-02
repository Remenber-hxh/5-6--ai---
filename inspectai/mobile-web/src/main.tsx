import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";

import ContextProvider from "@arco-design/mobile-react/esm/context-provider";
import setRootPixel from "@arco-design/mobile-react/tools/flexible";

import App from "./App";
import "./styles/global.css";

// Arco 是 rem 自适应体系(基准 1rem = 50px @ 375 设计稿),必须先设根字号,
// 否则它所有组件都按 16px 根字号算 —— 按钮文字会缩成 4.8px。
// 本项目自身样式全用 px,不受根字号影响,所以两套可以共存。
setRootPixel();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    {/* 【必须固定 system】Arco 按 UA 判平台,给组件挂 .ios / .android / .pc 类,
        而它的样式【只写了 .ios 和 .android 两套,没有 .pc】。
        在桌面浏览器里打开(开发自测、企微 PC 端)会被判成 pc,于是 button /
        dialog / image / input / popover / tabs 这 6 个组件【完全没有样式】——
        弹窗表现为一个白框、文字直接继承 52px 的根字号,占满整屏。

        固定成 ios:本项目的设计语言本来就是 iOS 风(流程页沿用旧版的浅色 iOS 卡片),
        而且移动端产品不该因为"用什么浏览器打开"而变形。 */}
    <ContextProvider system="ios">
      <HashRouter>
        <App />
      </HashRouter>
    </ContextProvider>
  </StrictMode>,
);
