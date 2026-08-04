import ArcoButton from "@arco-design/mobile-react/esm/button";
import "@arco-design/mobile-react/esm/button/style/css";
import type { CSSProperties, MouseEvent, ReactNode } from "react";

// ===== Button 适配层 =====
//
// 差异:antd-mobile 用 `block` 表示满宽;Arco 默认就是块级,行内才加 `inline`。
// 保留 block 属性让业务代码不用改,在这里翻译。

export interface ButtonProps {
  children?: ReactNode;
  /** 满宽。Arco 默认块级,故此属性只在为 false 时转成 inline */
  block?: boolean;
  loading?: boolean;
  disabled?: boolean;
  className?: string;
  style?: CSSProperties;
  onClick?: (e: MouseEvent<Element>) => void;
  /** 传给 Arco 的样式类型,默认 primary */
  type?: "primary" | "ghost" | "default";
}

export function Button({
  block = true,
  type = "primary",
  children,
  ...rest
}: ButtonProps) {
  return (
    <ArcoButton inline={!block} type={type} {...rest}>
      {children}
    </ArcoButton>
  );
}
