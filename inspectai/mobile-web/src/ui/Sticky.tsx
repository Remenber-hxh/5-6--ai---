import ArcoSticky from "@arco-design/mobile-react/esm/sticky";
import "@arco-design/mobile-react/esm/sticky/style/css";
import type { ReactNode } from "react";

// ===== 吸顶 =====
//
// 给长列表的分组标题用。设备健康页有 34 台设备分「需跟进 / 健康」两组,
// 滚到一半时屏幕上全是设备行,不知道当前在哪一组 —— 尤其"需跟进"那组
// 正是要重点看的。
//
// 【必须指定滚动容器】这个 app 的滚动不在 window 上,而在 .scroll-area
// 这类内部容器里(body 本身 overflow:hidden)。不传 getContainer 的话
// Sticky 监听的是 window 的滚动事件,永远不会触发。
//
// 扫过样式:sticky 没有 .ios/.android 分支。

export interface StickyProps {
  children: ReactNode;
  /** 距容器顶部多少像素时吸住 */
  topOffset?: number;
  className?: string;
}

export function Sticky({ children, topOffset = 0, className }: StickyProps) {
  return (
    <ArcoSticky
      className={className}
      position="top"
      topOffset={topOffset}
      getContainer={() => {
        // 就近找滚动容器;找不到就退回 window(至少不报错)
        return (
          (document.querySelector(".scroll-area") as HTMLElement) || window
        );
      }}
    >
      {children}
    </ArcoSticky>
  );
}
