import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { HashRouter } from "react-router-dom";

import setRootPixel from "@arco-design/mobile-react/tools/flexible";

import App from "./App";
import "./styles/global.css";

// Arco 是 rem 自适应体系(基准 1rem = 50px @ 375 设计稿),必须先设根字号,
// 否则它所有组件都按 16px 根字号算 —— 按钮文字会缩成 4.8px。
// 本项目自身样式全用 px,不受根字号影响,所以两套可以共存。
setRootPixel();

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <HashRouter>
      <App />
    </HashRouter>
  </StrictMode>,
);
