// ===== 读数区特写:这个数是从哪张图的哪一块读出来的 =====
//
// 【为什么要有这个东西】现场抄表是按顺序拍的:四块电表一字排开挨个拍。
// AI 也按顺序配,中间夹一张读不出的,后面就整体错位一格 —— 读数一个不差、
// 全填错了格子,而且哪儿都不报错。
//
// 而确认页上只有一个光秃秃的数字。人要核对,得凭记忆去对六张照片,
// 点开大图、翻页、再退回来。线上实测的结果是:三千多个字段里,
// 人改过 AI 的值 0 次 —— 不是 AI 准,是这个核对动作太贵,没人做。
//
// 把读数区那一小块直接摆在行旁边,核对就变成扫一眼。
//
// 【用 CSS 背景裁,不生成新图片】服务端裁一遍要多存一份文件、多一个接口、
// 还要处理清理和回源。而浏览器拿归一化的框就能裁 —— 原图本来就要加载
// (照片条在同一屏),这里是零额外请求。

export interface ReadingCropProps {
  /** 原图地址 */
  url: string;
  /** 归一化的 [左, 上, 右, 下] */
  bbox: number[];
  /** 点一下看大图 */
  onOpen?: () => void;
}

/** 框往外放一圈再裁 —— 模型给的框常贴着数字边缘,裁太紧会把首尾字符切掉半个。 */
const PAD = 0.18;

export default function ReadingCrop({ url, bbox, onOpen }: ReadingCropProps) {
  if (!url || !bbox || bbox.length !== 4) return null;

  let [x0, y0, x1, y1] = bbox;
  // 模型偶尔会把两个角写反,自己兜住 —— 反了的话下面算出来的宽高是负数,
  // 裁出来是一片空白,而界面上看不出是数据的问题还是图没加载出来。
  if (x0 > x1) [x0, x1] = [x1, x0];
  if (y0 > y1) [y0, y1] = [y1, y0];

  const bw = x1 - x0;
  const bh = y1 - y0;
  if (!(bw > 0) || !(bh > 0)) return null;

  x0 = Math.max(0, x0 - bw * PAD);
  y0 = Math.max(0, y0 - bh * PAD);
  x1 = Math.min(1, x1 + bw * PAD);
  y1 = Math.min(1, y1 + bh * PAD);

  const w = x1 - x0;
  const h = y1 - y0;
  // 背景百分比定位的含义是"把图的 p% 处对齐到容器的 p% 处",
  // 所以起点要除以 (1 - 裁剪尺寸);占满整边时分母为 0,退回 0。
  const px = w >= 1 ? 0 : (x0 / (1 - w)) * 100;
  const py = h >= 1 ? 0 : (y0 / (1 - h)) * 100;

  return (
    <button
      type="button"
      className="fld-crop"
      onClick={onOpen}
      aria-label="查看这个读数对应的照片"
      style={{
        backgroundImage: `url(${url})`,
        backgroundSize: `${100 / w}% ${100 / h}%`,
        backgroundPosition: `${px}% ${py}%`,
      }}
    />
  );
}
