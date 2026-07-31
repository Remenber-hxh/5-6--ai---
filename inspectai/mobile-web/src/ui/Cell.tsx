import ArcoCell from "@arco-design/mobile-react/esm/cell";
import "@arco-design/mobile-react/esm/cell/style/css";
import type { ReactNode } from "react";

// ===== Cell 适配层 =====
//
// 手机上的"设置页"就是一组组分割线列表 —— 微信/支付宝/系统设置全是这个形态。
// 之前个人页是一张居中玻璃卡 + 一个退出按钮,不像 app,像个登录后的落地页。
//
// Cell 负责:左图标 / 主文案 / 右侧值 / 箭头 / 分割线 / 点按反馈,
// 组内首尾圆角由 Cell.Group 保证 —— 这些自己写要写一堆,还容易各页不一致。

export interface CellProps {
  label: ReactNode;
  /** 副标题(第二行小字) */
  desc?: ReactNode;
  /** 右侧值 */
  text?: string;
  icon?: ReactNode;
  /** 右侧自定义内容(优先于 text) */
  children?: ReactNode;
  onClick?: () => void;
  /** 可点则显示箭头;不传 onClick 时默认不可点 */
  showArrow?: boolean;
  className?: string;
}

export function Cell({ label, desc, text, icon, children, onClick, showArrow, className }: CellProps) {
  const clickable = Boolean(onClick);
  return (
    <ArcoCell
      className={className}
      label={label}
      desc={desc}
      text={text}
      icon={icon}
      clickable={clickable}
      showArrow={showArrow ?? clickable}
      onClick={onClick}
    >
      {children}
    </ArcoCell>
  );
}

export interface CellGroupProps {
  header?: ReactNode;
  children: ReactNode;
  className?: string;
}

export function CellGroup({ header, children, className }: CellGroupProps) {
  return (
    <ArcoCell.Group className={className} header={header} bordered={false}>
      {children}
    </ArcoCell.Group>
  );
}
