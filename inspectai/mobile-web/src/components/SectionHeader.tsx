// ===== 列表分组标题 =====
//
// 旧版是「● 需跟进 · 3」—— 一个状态色圆点 + 组名 + 条数(app.js renderAssets)。
// 新版重构时标题【一条 CSS 都没有】,靠继承 body 拿到 15px 纯黑,和设备名同
// 字号同颜色,于是分组完全读不出来。
//
// 圆点不是装饰:一页里"需跟进"和"健康"两组,红点/绿点让人不用读字就能定位到
// 要处理的那一段。工地上戴着手套、赶时间的人,靠的就是这种一眼能抓住的东西。

export type SectionTone = "risk" | "ok" | "muted";

export interface SectionHeaderProps {
  title: string;
  /** 条数;不传则不显示 */
  count?: number;
  tone?: SectionTone;
}

export default function SectionHeader({
  title,
  count,
  tone = "muted",
}: SectionHeaderProps) {
  return (
    <div className={`section-head ${tone}`}>
      <span className="sh-dot" aria-hidden />
      <span className="sh-title">{title}</span>
      {count !== undefined && <span className="sh-count">· {count}</span>}
    </div>
  );
}
