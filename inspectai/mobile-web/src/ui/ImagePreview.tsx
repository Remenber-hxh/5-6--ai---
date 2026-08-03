import ArcoImagePreview from "@arco-design/mobile-react/esm/image-preview";
import "@arco-design/mobile-react/esm/image-preview/style/css";
import type { ReactNode } from "react";

// ===== 大图预览适配层 =====
//
// 现场要从照片里看的是表盘读数、铭牌编号、裂缝走向 —— 铺满 390 宽的屏幕
// 也未必看得清。之前那版查看器只能"把图铺满",放不大;要看下一张得先退出
// 再点开。这两件事手写起来都不便宜(缩放要处理双指、边界回弹、双击复位)。
//
// Arco ImagePreview 直接给:双指缩放 / 双击放大 / 左右翻页 / 下拉关闭。
//
// 另外 images[].extraNode 是【每张图自己的叠层】,水印挂这里 —— 翻到哪张就
// 显示哪张的时间/人/定位,不用自己跟着当前页码切。
// 位置上它是 .preview-image-wrap-container 的直接子节点(index.js:732),
// 在缩放变换【之外】:放大图片时水印仍贴在屏幕底部,不会跟着糊掉。
// 那个容器没写 position,叠层要定位得自己补 relative。
//
// 用组件式而不是命令式 ImagePreview.open():命令式渲染到 React 树外,得像
// Dialog.confirm 那样手动把 ARCO_CONTEXT 传进去(见 arcoContext.ts);
// 组件式在树内声明(内部仍用 Portal 挂载),ContextProvider 的平台设定自然生效。
//
// 扫过样式:image-preview 与它依赖的 carousel 都【没有】.ios/.android 分支
// (grep 计数 0),不像 dialog 那样受平台判定影响。
//
// index < 0 即关闭 —— 组件内部的 visible 就是这么算的(index.js:97),
// 所以不另设 visible 开关,免得两个来源打架。

export interface PreviewPhoto {
  src: string;
  /** 这张图专属的叠层(水印等),随翻页切换 */
  extraNode?: ReactNode;
}

export interface ImagePreviewProps {
  photos: PreviewPhoto[];
  /** 当前打开第几张;< 0 表示关闭 */
  index: number;
  onClose: () => void;
  /** 翻页回调,给外部同步"当前是第几张" */
  onIndexChange?: (index: number) => void;
  /** 覆盖整个预览层的额外内容(关闭键、工具栏),不随翻页切换 */
  extra?: ReactNode;
}

export function ImagePreview({ photos, index, onClose, onIndexChange, extra }: ImagePreviewProps) {
  return (
    <ArcoImagePreview
      images={photos}
      openIndex={index}
      // close 是必填(点遮罩/下拉触发),onClose 是收尾回调;两个都指向同一个出口,
      // 少了 close 会出现"划下去了但状态没清,再点缩略图打不开"。
      close={onClose}
      onClose={onClose}
      onAfterChange={onIndexChange}
      // 关掉自带的圆点指示器:它落在左下角,正好压住底部工具栏;
      // 而且十几张照片摊成一排圆点也数不清,不如直接写「3 / 13」。
      showIndicator={false}
      extra={extra}
    />
  );
}
