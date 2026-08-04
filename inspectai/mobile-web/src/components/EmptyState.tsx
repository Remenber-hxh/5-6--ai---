import type { ReactNode } from "react";

// ===== 空态 =====
//
// 4 个页面各手写了一份一模一样的结构。抽出来不是为了省几行,是为了
// 【别再各写各的】—— 状态胶囊就是活教训:两处独立手写,最后一个 11px/500、
// 一个 12px/700,谁也不知道对方长什么样。
//
// 组件库里没有对应的东西(逐个核过 56 个组件,没有 Empty/Result),
// 所以自己写一个,放在业务组件层。

export interface EmptyStateProps {
  /** 一个字符或图标。清零类用 ✓,查无结果用 ∅ —— 别用同一个,语气不同 */
  icon?: ReactNode;
  title: string;
  /** 出路:告诉用户下一步能做什么,不要只说"暂无数据" */
  hint?: ReactNode;
  children?: ReactNode;
}

export default function EmptyState({
  icon = "∅",
  title,
  hint,
  children,
}: EmptyStateProps) {
  return (
    <div className="empty-state">
      <span className="es-badge">{icon}</span>
      <span className="es-title">{title}</span>
      {hint && <span className="es-hint">{hint}</span>}
      {children}
    </div>
  );
}
