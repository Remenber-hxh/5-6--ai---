import ArcoTag from "@arco-design/mobile-react/esm/tag";
import "@arco-design/mobile-react/esm/tag/style/css";

import type { ReactNode } from "react";

// ===== 标签 / 可选片 =====
//
// 用在「修改理由」的快选上:四个常见理由点一下就填进理由框,不想手打的
// 直接点,想补充的再改。旧版的做法(renderReasonChips),搬过来 ——
// 戴着手套在机房里打字是件很难受的事,能点就别让人打。
//
// 【注意 Tag 不带选中态】它是展示型组件,没有 checked/selected。
// 需要"选中"语义时由调用方自己控 type:选中用 primary(实心蓝),
// 未选中用 hollow(描边)。别指望组件替你记状态。
//
// 平台分支只有一处:.arco-tag.android .tag-text{padding-top:.02rem} —— 安卓
// 的 1px 微调。我们在 ContextProvider 固定 system="ios"(见 arcoContext.ts),
// 走不到那条,不影响。

export interface TagProps {
  children: ReactNode;
  /** 实心 / 描边;选中态由调用方切换 */
  type?: "primary" | "hollow" | "solid";
  size?: "small" | "medium" | "large";
  onClick?: () => void;
  className?: string;
}

export function Tag({ children, type = "hollow", size = "small", onClick, className }: TagProps) {
  return (
    <ArcoTag className={className} type={type} size={size} onClick={onClick} filleted>
      {children}
    </ArcoTag>
  );
}
