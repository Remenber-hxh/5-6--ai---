import ArcoLoading from "@arco-design/mobile-react/esm/loading";
import "@arco-design/mobile-react/esm/loading/style/css";

// ===== Loading 适配层 =====
//
// 原来是手写的 .spinner(一个 border 转圈)。换 Arco 后拿到 arc/spin/circle/dot
// 四种形态和可控的半径/线宽,配色也能对齐主题。
//
// 默认用 arc:在深色玻璃背景上比整圈 border 干净,不会糊成一个灰环。

export interface LoadingProps {
  /** 直径,默认 20 */
  size?: number;
  /** 描边色,默认翡翠 */
  color?: string;
  className?: string;
}

export function Loading({
  size = 20,
  color = "#43e5ac",
  className,
}: LoadingProps) {
  return (
    <ArcoLoading
      className={className}
      type="arc"
      radius={size / 2}
      stroke={2}
      color={color}
      filleted
    />
  );
}
