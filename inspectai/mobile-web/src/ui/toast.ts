import ArcoToast from "@arco-design/mobile-react/esm/toast";
import "@arco-design/mobile-react/esm/toast/style/css";
import type { ReactNode } from "react";

import { ARCO_CONTEXT } from "./arcoContext";

// ===== Toast 适配层 =====
//
// 对外保留 antd-mobile 的 `Toast.show({content, duration, position})` 签名,
// 业务代码不用改写法。两处差异在这里吸收:
//
//   1. 方法名:antd-mobile 是 .show,Arco 是 .toast / .info
//   2. 位置参数:position → direction
//   3. 【重要】antd-mobile 的 Toast 自动替换上一条;Arco 不会 ——
//      不手动关掉前一个,连续操作会叠出一堆提示挡住界面。

export interface ToastConfig {
  content: ReactNode;
  /** 毫秒;0 = 不自动关闭 */
  duration?: number;
  position?: "top" | "bottom" | "center";
}

let current: { close: () => void } | null = null;

function show(config: ToastConfig | string) {
  const cfg: ToastConfig =
    typeof config === "string" ? { content: config } : config;
  // 先关上一条,补上 Arco 缺的单实例语义
  try {
    current?.close();
  } catch {
    /* 已被自身超时关闭,忽略 */
  }
  current = ArcoToast.toast(
    {
      content: cfg.content,
      duration: cfg.duration ?? 2000,
      direction: cfg.position ?? "center",
    },
    // 和 Dialog 同理:命令式 API 渲染到树外 portal,拿不到 <ContextProvider>
    // 里的 system,不传就退回没有样式的 pc 分支
    ARCO_CONTEXT,
  );
}

export const Toast = {
  show,
  /** 提示已关闭时的兜底清理(退出登录、页面卸载可调) */
  clear() {
    try {
      current?.close();
    } catch {
      /* ignore */
    }
    current = null;
  },
};
