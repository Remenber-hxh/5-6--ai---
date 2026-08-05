import AssetTypeIcon from "@/components/AssetTypeIcon";
import StatusTag from "@/components/StatusTag";

import type { AssetDTO } from "@/api/inspection";

// ===== 台账里的一台设备 =====
//
// 照旧版 assetRowHTML(app.js:1668) 搬回来的三层信息。重构时新版只留了两行,
// 把「谁巡的」和「什么问题」丢了 —— 而主管扫这一页,要的正是"哪台有问题、
// 什么问题"。丢了这两样,这一页就只剩一串设备名。
//
//   第一行  设备名
//   第二行  项目 · 类型 · 最近 巡检人
//   第三行  需跟进：<具体字段>        ← 只在需要跟进时出现
//   右  侧  状态胶囊 + 巡检次数
//
// 【和旧版有意不同的一处】
// 旧版对健康设备也渲染第三行(那句巡检流水叙述)。那行对"这台没事"没有增量,
// 却让每一行都变高,一屏少看好几台。这里改成:健康的设备只有两行,
// 需跟进的才多出红字。安静的地方保持安静,有事的地方才喊。

/** 需跟进的三种状态。不在表内的都算健康段(旧版同一口径) */
const FOLLOWUP = new Set(["异常", "待维修", "待复核"]);

/**
 * AI 总结里"到底哪项要跟进"的三种写法。
 *
 * 【为什么是三条而不是照搬旧版那一条】
 * 旧版 assetCardSummary(app.js:1654)只认「发现 N 项…：…」。拿线上 35 台设备
 * 的真实文案跑了一遍:3 台需跟进的【一台都没匹配上】,全都落到兜底那句
 * "存在待跟进项,待复查"。也就是说这个红字在旧版里早就是坏的 —— 模型的输出
 * 格式换过,正则没跟着换,而兜底话看起来又"正常",所以一直没人发现。
 *
 * 现在按真实文案分三种:
 *   待跟进 1 项：设备无异响、异味=否。
 *   …发现「选层按钮及显示正常」异常，其余项目正常…
 *   异常提示：… 巡检发现需复核项：开关门运行偶发轻微卡滞…
 * 三条按顺序试,都不中才回到兜底。
 *
 * 【这类提炼注定会再次过时,而且不会报错】模型换个说法就可能三条全落空,
 * 落空时页面显示的是那句兜底话 —— 看着完全正常,没人会发现红字失效了。
 * 旧版就是这么坏了很久的。
 *
 * 正确的做法是给这个函数配几条真实文案的单元测试,可 mobile-web 目前【没有
 * 测试框架】(package.json 里没有 test 脚本)。在补上之前,这里只能靠这段
 * 注释提醒:改模型提示词、换总结格式时,回来看一眼这三条正则还中不中。
 */
const NOTE_PATTERNS = [
  /(?:待跟进|需跟进|发现)\s*\d+\s*项[^：:]*[：:]\s*([^。；;]+)/,
  /(?:发现|存在)[「“"]([^」”"]+)[」”"]\s*(?:异常|问题|不合格)/,
  /(?:需复核项|待跟进项|异常项|异常)[：:]\s*([^。；;]+)/,
];

export function followupNote(a: AssetDTO): string {
  if (!FOLLOWUP.has(a.lastStatus || "")) return "";
  const raw = String(a.lastSummary || "").trim();
  for (const re of NOTE_PATTERNS) {
    const hit = raw.match(re)?.[1]?.trim();
    if (hit) return `需跟进：${hit}`;
  }
  return "存在待跟进项,待复查";
}

export interface AssetRowProps {
  asset: AssetDTO;
  /** 封面图地址;为空/null 时显示设备类型图标 */
  cover?: string | null;
  onClick?: () => void;
}

export default function AssetRow({ asset, cover, onClick }: AssetRowProps) {
  const note = followupNote(asset);
  // 项目名整页往往都一样,但类型和巡检人不是 —— 三段拼起来才够区分两台
  // 只差一个字符的设备(K07 / K7 就是这么被认错的)。
  const sub = [
    asset.project,
    asset.assetType,
    asset.lastInspector && `最近 ${asset.lastInspector}`,
  ]
    .filter(Boolean)
    .join(" · ");

  return (
    <button className="asset-row" onClick={onClick}>
      {cover ? (
        <img className="ar-cover" src={cover} alt="" loading="lazy" />
      ) : (
        // 没封面照就出类型图标。空灰框占了 63% 的行,看着像图挂了。
        <span className="ar-cover ar-cover-icon" aria-hidden>
          <AssetTypeIcon type={asset.assetType} />
        </span>
      )}

      <span className="ar-main">
        <span className="ar-name">{asset.assetName}</span>
        <span className="ar-sub">{sub || "—"}</span>
        {note && <span className="ar-note">{note}</span>}
      </span>

      <span className="ar-side">
        <StatusTag text={asset.lastStatus || "未巡检"} />
        {asset.inspectionCount > 0 && (
          <span className="ar-count">{asset.inspectionCount} 次</span>
        )}
      </span>
    </button>
  );
}
