import { Toast } from "antd-mobile";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { AssetDTO, AssetSnapshotDTO, getAsset, listAssetRecords } from "@/api/inspection";

function tone(status: string): string {
  switch (status) {
    case "正常":
      return "ok";
    case "异常":
      return "danger";
    case "待复核":
      return "warn";
    case "待维修":
      return "repair";
    default:
      return "unknown";
  }
}

function fmtWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

// 资产详情(旧版 sceneAsset):当前状态 + 巡检历史(按快照分页翻完整历史)
export default function AssetDetailPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [asset, setAsset] = useState<AssetDTO | null>(null);
  const [snaps, setSnaps] = useState<AssetSnapshotDTO[]>([]);
  const [page, setPage] = useState(1);
  const [totalPages, setTotalPages] = useState(1);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    void (async () => {
      try {
        const [a, rec] = await Promise.all([getAsset(id), listAssetRecords(id, 1)]);
        setAsset(a);
        setSnaps(rec.snapshots);
        setTotalPages(rec.totalPages);
      } catch {
        Toast.show({ content: "设备信息加载失败" });
      } finally {
        setLoading(false);
      }
    })();
  }, [id]);

  async function loadMore() {
    const next = page + 1;
    try {
      const rec = await listAssetRecords(id, next);
      setSnaps((cur) => [...cur, ...rec.snapshots]);
      setPage(next);
    } catch {
      Toast.show({ content: "加载更多失败" });
    }
  }

  if (loading) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }
  if (!asset) {
    return (
      <div className="center-screen">
        <p className="screen-sub">设备不存在</p>
        <button className="task-back" onClick={() => nav("/ledger")}>
          返回台账
        </button>
      </div>
    );
  }

  const t = tone(asset.lastStatus);

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">{asset.assetName}</h1>
        <p className="flow-sub">
          {asset.project || "—"}
          {asset.assetType ? ` · ${asset.assetType}` : ""}
        </p>
      </div>

      <div className="scroll-area flow-body">
        {/* 当前状态 */}
        <div className="ad-card">
          <div className="ad-row">
            <span className="ad-k">当前状态</span>
            <span className={`ar-tag ar-${t}`}>{asset.lastStatus || "未巡检"}</span>
          </div>
          <div className="ad-row">
            <span className="ad-k">累计巡检</span>
            <span className="ad-v">{asset.inspectionCount} 次</span>
          </div>
          <div className="ad-row">
            <span className="ad-k">最近巡检</span>
            <span className="ad-v">{fmtWhen(asset.lastInspectedAt) || "未巡检"}</span>
          </div>
          {asset.lastInspector && (
            <div className="ad-row">
              <span className="ad-k">最近巡检人</span>
              <span className="ad-v">{asset.lastInspector}</span>
            </div>
          )}
          {asset.lastSummary && <div className="ad-summary">{asset.lastSummary}</div>}
        </div>

        {/* 巡检历史 */}
        <div className="ad-title">巡检历史</div>
        {snaps.length === 0 ? (
          <div className="task-empty">暂无历史记录</div>
        ) : (
          <div className="ad-history">
            {snaps.map((s) => (
              <div className="hist-item" key={s.id}>
                <div className="hist-head">
                  <span className="hist-when">{fmtWhen(s.createdAt)}</span>
                  <span className={`ar-tag ar-${tone(s.status)}`}>{s.status || "—"}</span>
                </div>
                {s.inspector && <div className="hist-who">{s.inspector}</div>}
                {s.summary && <div className="hist-body">{s.summary}</div>}
              </div>
            ))}
          </div>
        )}

        {page < totalPages && (
          <button className="lg-clear" onClick={() => void loadMore()}>
            加载更多({page}/{totalPages})
          </button>
        )}
      </div>

      <div className="flow-foot">
        <button className="task-back" onClick={() => nav("/ledger")}>
          返回台账
        </button>
      </div>
    </div>
  );
}
