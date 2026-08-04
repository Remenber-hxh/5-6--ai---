import ArcoCollapse from "@arco-design/mobile-react/esm/collapse";
import "@arco-design/mobile-react/esm/collapse/style/css";
import type { ReactNode } from "react";

// ===== 折叠面板 =====
//
// 给设备健康页的筛选区用:项目 + 设备类型两组筛选片占了大半屏,
// 而管理者进这一页九成是来看列表的,筛选是偶尔才用一次。
// 默认收起,标题行显示当前筛的是什么 —— 收起来但不隐藏信息。
//
// 扫过样式:collapse 没有 .ios/.android 分支。

export interface CollapseProps {
  /** 标题行左侧 */
  title: ReactNode;
  /** 标题行右侧的摘要(收起时也要能看到当前筛选了什么) */
  extra?: ReactNode;
  /** 默认展开 */
  defaultOpen?: boolean;
  children: ReactNode;
  className?: string;
}

export function Collapse({
  title,
  extra,
  defaultOpen = false,
  children,
  className,
}: CollapseProps) {
  // Arco 的结构是 Collapse.Group 装若干 Collapse(每个 Collapse 是【一项】,
  // 每项要 value 作标识,内容走 content 而不是 children)。只需要一项也要套
  // Group —— 展开状态是 Group 管的。
  return (
    <ArcoCollapse.Group
      className={className}
      defaultActiveItems={defaultOpen ? ["1"] : []}
    >
      <ArcoCollapse
        value="1"
        header={
          <span className="cl-head">
            <span className="cl-title">{title}</span>
            {extra && <span className="cl-extra">{extra}</span>}
          </span>
        }
        content={children}
      />
    </ArcoCollapse.Group>
  );
}
