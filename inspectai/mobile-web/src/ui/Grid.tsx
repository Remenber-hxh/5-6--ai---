import ArcoGrid from "@arco-design/mobile-react/esm/grid";
import "@arco-design/mobile-react/esm/grid/style/css";
import type { ReactNode } from "react";

// ===== Grid 适配层 =====
//
// 首页「工作台」四个入口原本是手写 flex + 各自算宽度。交给 Grid 之后
// 列数、行列间距、换行由组件保证 —— 以后加第五个入口不用再动 CSS。
//
// 用 renderGrid 完全接管单元格渲染:我们的格子里有 SVG 图标 + 文字 + 红点角标,
// 不是 Arco 默认的「图片 + 标题」形态。data 里的 img/title 只是占位,
// 真正渲染走 renderGrid。

export interface GridCell {
  key: string;
  render: ReactNode;
  onClick: () => void;
}

export interface GridProps {
  cells: GridCell[];
  columns?: number;
  gutter?: number;
  className?: string;
}

export function Grid({ cells, columns = 4, gutter = 10, className }: GridProps) {
  return (
    <ArcoGrid
      className={className}
      columns={columns}
      gutter={gutter}
      // Arco 默认给格子画分割线,深色科技风里那几条灰线很脏
      border={false}
      data={cells.map((c) => ({
        img: null,
        title: null,
        onClick: c.onClick,
        renderGrid: () => c.render,
      }))}
    />
  );
}
