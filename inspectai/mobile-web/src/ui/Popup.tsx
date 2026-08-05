import ArcoPopup from "@arco-design/mobile-react/esm/popup";
import "@arco-design/mobile-react/esm/popup/style/css";

import type { ReactNode } from "react";

// ===== 底部面板 =====
//
// 「申请修改」这类操作需要一层:它有表单、要滚动、可能填到一半改主意。
// 用整页路由会把用户带离设备详情(填错了退回来还得重新找那台设备),
// 用 Dialog 又放不下一组字段。底部面板是这类"就地展开一件事"的标准形态。
//
// 交给组件库而不是自己写,省的是这些容易漏的:遮罩层、进出场动画、
// 背景锁滚(自己写常见的 bug 是面板滚到底后手指继续滑,底下的页面跟着动)、
// 安全区避让、Esc / 点遮罩关闭。
//
// 扫过样式:popup 没有 .ios/.android 分支(grep 计数 0),不受平台判定影响。

export interface PopupProps {
  visible: boolean;
  onClose: () => void;
  children: ReactNode;
  /** 从哪个方向推入,默认底部 */
  direction?: "top" | "bottom" | "left" | "right";
  className?: string;
}

export function Popup({ visible, onClose, children, direction = "bottom", className }: PopupProps) {
  return (
    <ArcoPopup
      visible={visible}
      direction={direction}
      close={onClose}
      maskClosable
      className={className}
      // 底部面板必须避开 home indicator,否则最后一个按钮点不到
      needBottomOffset
    >
      {children}
    </ArcoPopup>
  );
}
