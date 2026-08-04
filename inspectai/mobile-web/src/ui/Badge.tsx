import ArcoBadge from "@arco-design/mobile-react/esm/badge";
import "@arco-design/mobile-react/esm/badge/style/css";

// ===== Badge 适配层 =====
//
// 【语义和 antd 相反,踩过】antd 的 Badge 是包在 children 外面装饰它;
// Arco 的 Badge【本身就是那个红色药丸】,children 是【替换】药丸里的文字。
// 把磁贴内容当 children 传进去,整个磁贴会变成一个红药丸(实测踩了)。
//
// 正确用法:Badge 当兄弟节点放在 position:relative 的容器里,自己定位。
// 相比原来手写的 .badge-dot,换来的是 maxCount 逻辑、出现/消失的缩放过渡,
// 以及和设计系统一致的药丸样式。
//
// count <= 0 时不渲染:空角标比没有角标更糟 —— 会训练人忽略它。

export interface BadgeProps {
  /** 计数;<= 0 不渲染 */
  count?: number;
  /** 超过则显示 N+ */
  maxCount?: number;
  /** 定位类名,由调用方给(父级需 position: relative) */
  className?: string;
}

export function Badge({ count = 0, maxCount = 99, className }: BadgeProps) {
  if (count <= 0) return null;
  return (
    <ArcoBadge
      className={className}
      text={count}
      maxCount={maxCount}
      absolute
    />
  );
}
