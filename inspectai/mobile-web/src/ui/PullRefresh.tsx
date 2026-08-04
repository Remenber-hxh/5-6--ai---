import ArcoPullRefresh from "@arco-design/mobile-react/esm/pull-refresh";
import "@arco-design/mobile-react/esm/pull-refresh/style/css";
import type { ReactNode } from "react";

// ===== PullRefresh 适配层 =====
//
// 首页的待办计数(任务/待审批/待处理照片)原本只在进入页面时拉一次。
// 巡检员想看最新数只能退出去再进来 —— 手机上「下拉刷新」是肌肉记忆,
// 缺了它整个页面感觉是死的。这是本次真正补上的能力,不是换皮。
//
// 文案走中文;type 用 ios(阻尼回弹),android 那套是圆环下坠,
// 和本项目的深色科技风不搭。

export interface PullRefreshProps {
  children: ReactNode;
  onRefresh: () => Promise<void>;
  className?: string;
  disabled?: boolean;
}

export function PullRefresh({
  children,
  onRefresh,
  className,
  disabled,
}: PullRefreshProps) {
  return (
    <ArcoPullRefresh
      className={className}
      type="ios"
      disabled={disabled}
      onRefresh={onRefresh}
      initialText="下拉刷新"
      pullingText="下拉刷新"
      loosingText="松开刷新"
      loadingText="正在刷新"
      finishText="已是最新"
    >
      {children}
    </ArcoPullRefresh>
  );
}
