import AssetTypeIcon from "@/components/AssetTypeIcon";
import StatusTag from "@/components/StatusTag";
import { sinceText } from "@/lib/assetCover";

import type { AssetDTO } from "@/api/inspection";

// ===== 台账里的一台设备 =====
//
// 照旧版 assetRowHTML(app.js:1668) 的结构。旧版还有第三行(橙字写明"哪一项
// 要跟进"),做过一版,产品定了不要 —— 列表只留"是哪台、什么状态",具体问题
// 进详情页看。提炼那段正则和它修好的 bug 记在 git 里(commit 6fdcf21),
// 哪天要放到详情页可以捡回来,别重写一遍。
//
//   第一行  设备名 ………………………… 状态胶囊
//   第二行  项目 · 类型 · 巡检人 · 多久前(独占整行宽)
//
// 【状态胶囊从右侧一列挪到名字那一行】原来右边是一整列:胶囊在上、
// 「63 次」在下。那一列的宽度由胶囊决定(51px),而且从上到下都占着 ——
// 中间只剩 182px,副行放不下,K03 那一行就把「朱佳伟」截成了省略号。
// 挪上去之后副行独占整行,182 → 约 243px。
//
// 【「63 次」换成「3 天前」】一条记录派生好几台设备,每台都 +1,同一个
// 模板下的设备次数全一样(综合巡检那 5 台全是 68)。"多久没巡了"才是
// 每台都不一样、而且真能用来决定先去看哪台的信息。

export interface AssetRowProps {
  asset: AssetDTO;
  /** 封面图地址;为空/null 时显示设备类型图标 */
  cover?: string | null;
  onClick?: () => void;
}

export default function AssetRow({ asset, cover, onClick }: AssetRowProps) {
  // 项目名整页往往都一样,但类型和巡检人不是 —— 几段拼起来才够区分两台
  // 只差一个字符的设备(K07 / K7 就是这么被认错的)。
  const since = sinceText(asset.lastInspectedAt);
  const sub = [asset.project, asset.assetType, asset.lastInspector, since]
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
        <span className="ar-top">
          <span className="ar-name">{asset.assetName}</span>
          <StatusTag text={asset.lastStatus || "未巡检"} />
        </span>
        <span className="ar-sub">{sub || "—"}</span>
      </span>
    </button>
  );
}
