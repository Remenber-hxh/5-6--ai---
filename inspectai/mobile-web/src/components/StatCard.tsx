// ===== 概览数字卡 =====
//
// 台账顶部四个数字、任务页三个数字,原来是两处各写一遍的 JSX。抽出来的理由
// 不只是少写几行:它们是【筛选开关】,而"点了之后长什么样"这件事必须两处
// 一致 —— 否则用户在一页学会的操作,到另一页就不认识了。
//
// 【怎么让人知道能点】不写"点击可筛选"这种说明文字(不是这套设计的做法),
// 靠的是选中一项后其余变淡 —— 由外层容器加 .filtering 类统一控制。
// 第一次误触就能看懂,没选中时四张卡完全干净、不打扰。

export type StatTone = "plain" | "ok" | "warn" | "info";

export interface StatCardProps {
  value: number | string;
  label: string;
  tone?: StatTone;
  /** 选中态(当前正按这一项筛选) */
  active?: boolean;
  /** 不传则渲染成不可点的静态卡 */
  onClick?: () => void;
  /** 数字为 0 时点了只会得到空列表,置灰禁用 */
  disabled?: boolean;
}

export default function StatCard({
  value,
  label,
  tone = "plain",
  active,
  onClick,
  disabled,
}: StatCardProps) {
  const cls = ["lo-card", active ? "on" : "", onClick ? "" : "is-static"]
    .filter(Boolean)
    .join(" ");
  return (
    <button className={cls} onClick={onClick} disabled={disabled || !onClick}>
      <span className={`lo-num ${tone}`}>{value}</span>
      <span className="lo-label">{label}</span>
    </button>
  );
}
