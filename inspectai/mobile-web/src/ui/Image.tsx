import ArcoImage from "@arco-design/mobile-react/esm/image";
import "@arco-design/mobile-react/esm/image/style/css";

// ===== Image 适配层 =====
//
// 替掉手写的 <img>。换来三样在弱网现场真有用的东西:
//   加载占位 —— 巡检照片是从服务器取的,现场信号差时会白一片
//   失败兜底 —— 原来加载失败就是个裂图,现在有明确的"加载失败"态
//   自动重试 —— retryTime,断续网络下不用用户手动刷新
//
// 【不要拿它替 PhotoViewer】那个是带水印的证据查看器(水印只在查看时叠加,
// 原图不动),Arco 的 ImagePreview 没有这个能力,替了会丢证据链的展示。

export interface ImageProps {
  src: string;
  alt?: string;
  /** 圆角,数字按 px */
  radius?: number;
  /** 默认 cover:缩略图要填满方框,不留边 */
  fit?: "cover" | "contain";
  className?: string;
}

export function Image({
  src,
  alt = "",
  radius = 12,
  fit = "cover",
  className,
}: ImageProps) {
  return (
    <ArcoImage
      className={className}
      src={src}
      alt={alt}
      fit={fit}
      radius={radius}
      showLoading
      showError
      // 断续网络下自动重试两次,省得用户以为照片丢了
      retryTime={2}
    />
  );
}
