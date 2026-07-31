import ArcoNoticeBar from "@arco-design/mobile-react/esm/notice-bar";
import "@arco-design/mobile-react/esm/notice-bar/style/css";
import type { ReactNode } from "react";

// ===== NoticeBar 适配层 =====
//
// 首页原来用两处手写元素表达状态:.task-banner(当前任务)和底部一行常驻小字
// (「拍下的照片会先存在这台手机上」)。后者常驻但信息密度极低,在线时纯占位。
//
// 换成 NoticeBar 的好处:文字超长自动跑马灯(任务名可能很长)、可关闭、
// 左右插槽统一。marquee="overflow" = 只有放不下才滚,不会平白晃眼。

export interface NoticeBarProps {
  children: ReactNode;
  /** 左侧图标/圆点 */
  leftContent?: ReactNode;
  /** 右侧操作(如「退出任务」) */
  rightContent?: ReactNode;
  /** 语气:info 正常 / warn 需注意(离线、被拦下) */
  tone?: "info" | "warn";
  onClick?: () => void;
}

export function NoticeBar({ children, leftContent, rightContent, tone = "info", onClick }: NoticeBarProps) {
  return (
    <ArcoNoticeBar
      className={tone === "warn" ? "app-notice app-notice-warn" : "app-notice"}
      leftContent={leftContent}
      rightContent={rightContent}
      // 【必须显式关掉】Arco 默认 marquee="overflow":文字一行放不下就跑马灯滚动。
      // 这里两种用途都是要看懂的正事(当前任务 / 离线影响),滚动只会更难读 ——
      // 关掉后 wrapable(默认 true)接手,直接换行。
      marquee="none"
      // 【必须显式关掉】默认 closeable=true 会渲染一个 ×。
      // 离线是【状态】不是通知,关掉它不会让网络变好,只会让人看不见自己在离线。
      closeable={false}
      onClick={onClick}
    >
      {children}
    </ArcoNoticeBar>
  );
}
