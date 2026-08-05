import AssetTypeIcon from "@/components/AssetTypeIcon";
import StatusTag from "@/components/StatusTag";

import type { AssetDTO } from "@/api/inspection";

// ===== 台账里的一台设备 =====
//
// 照旧版 assetRowHTML(app.js:1668) 的结构。旧版还有第三行(橙字写明"哪一项
// 要跟进"),做过一版,产品定了不要 —— 列表只留"是哪台、什么状态",具体问题
// 进详情页看。提炼那段正则和它修好的 bug 记在 git 里(commit 6fdcf21),
// 哪天要放到详情页可以捡回来,别重写一遍。
//
//   第一行  设备名
//   第二行  项目 · 类型 · 最近 巡检人
//   右  侧  状态胶囊 + 巡检次数

export interface AssetRowProps {
  asset: AssetDTO;
  /** 封面图地址;为空/null 时显示设备类型图标 */
  cover?: string | null;
  onClick?: () => void;
}

export default function AssetRow({ asset, cover, onClick }: AssetRowProps) {
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
