import ArcoSwipeAction from "@arco-design/mobile-react/esm/swipe-action";
import "@arco-design/mobile-react/esm/swipe-action/style/css";
import type { ReactNode } from "react";

// ===== 左滑操作 =====
//
// 待处理照片原来要删一张:点「选择」进多选 → 勾选 → 点删除,三步。
// 左滑一下就能删单张,多选留给"删一批"。
//
// 【删除必须自己带确认】
// 这里不做自动确认 —— 服务器上的原图删了就没了,而左滑是个很容易误触的
// 手势(列表本身也能横向滚)。onClick 里弹 Dialog 再删,由调用方负责。
//
// 扫过样式:swipe-action 没有 .ios/.android 分支。

export interface SwipeActionItem {
  text: string;
  /** 返回 false 可阻止面板自动收起(比如弹了确认框还没选) */
  onClick: () => void | boolean | Promise<void | boolean>;
  /** danger 用于删除类:红底,让人在滑出来的一瞬间就知道这是不可逆的 */
  tone?: "danger" | "default";
}

export interface SwipeActionProps {
  right?: SwipeActionItem[];
  children: ReactNode;
  className?: string;
}

const TONE_STYLE: Record<string, React.CSSProperties> = {
  danger: { background: "#ff3b30", color: "#fff" },
  default: { background: "var(--lt-fill)", color: "var(--lt-fg)" },
};

export function SwipeAction({ right, children, className }: SwipeActionProps) {
  return (
    <ArcoSwipeAction
      className={className}
      rightActions={(right || []).map((a) => ({
        text: a.text,
        style: TONE_STYLE[a.tone || "default"],
        onClick: a.onClick,
      }))}
      // 点别处就收起:滑开一半忘了收,下一次点击会被当成"点动作按钮"
      closeOnTouchOutside
    >
      {children}
    </ArcoSwipeAction>
  );
}
