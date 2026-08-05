import { Toast } from "@/ui";
import { useEffect, useState } from "react";

import ChangeRequestSheet from "@/components/ChangeRequestSheet";
import { useNavigate, useParams } from "react-router-dom";

import CenterLoading from "@/components/CenterLoading";
import FlowHeader from "@/components/FlowHeader";
import StatusTag from "@/components/StatusTag";
import { AssetSnapshotDTO, getAsset, listAssetRecords } from "@/api/inspection";
import { useResource } from "@/hooks/useResource";

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
  const [page, setPage] = useState(1);
  const [crOpen, setCrOpen] = useState(false);

  // 换设备(id 变)时 useResource 会作废上一次的飞行请求 —— 原来没有这层,
  // 快速连点两台设备可能被先发后到的旧响应盖掉
  const { data, loading } = useResource(
    async (signal) => {
      const [a, rec] = await Promise.all([
        getAsset(id, signal),
        listAssetRecords(id, 1, signal),
      ]);
      return { asset: a, snapshots: rec.snapshots, totalPages: rec.totalPages };
    },
    [id],
    { errorText: "设备信息加载失败" },
  );

  // 首页数据来自 useResource,翻页追加的部分放在本地 —— id 变了要重置
  const [more, setMore] = useState<AssetSnapshotDTO[]>([]);
  useEffect(() => {
    setMore([]);
    setPage(1);
  }, [id]);

  const asset = data?.asset ?? null;
  const snaps = [...(data?.snapshots ?? []), ...more];
  const totalPages = data?.totalPages ?? 1;

  async function loadMore() {
    const next = page + 1;
    try {
      const rec = await listAssetRecords(id, next);
      setMore((cur) => [...cur, ...rec.snapshots]);
      setPage(next);
    } catch {
      Toast.show({ content: "加载更多失败" });
    }
  }

  if (loading) {
    return <CenterLoading />;
  }
  if (!asset) {
    return (
      <div className="center-screen">
        <p className="screen-sub">设备不存在</p>
        {/* 这是深色屏,不能用浅色页的 .task-back */}
        <button className="btn-dark-ghost" onClick={() => nav("/ledger")}>
          返回台账
        </button>
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <FlowHeader title={asset.assetName} onBack={() => nav("/ledger")} />

      <div className="scroll-area flow-body">
        <p className="flow-caption">
          {asset.project || "—"}
          {asset.assetType ? ` · ${asset.assetType}` : ""}
        </p>
        {/* 当前状态 */}
        <div className="ad-card">
          <div className="ad-row">
            <span className="ad-k">当前状态</span>
            <StatusTag text={asset.lastStatus || "未巡检"} />
          </div>
          <div className="ad-row">
            <span className="ad-k">累计巡检</span>
            <span className="ad-v">{asset.inspectionCount} 次</span>
          </div>
          <div className="ad-row">
            <span className="ad-k">最近巡检</span>
            <span className="ad-v">
              {fmtWhen(asset.lastInspectedAt) || "未巡检"}
            </span>
          </div>
          {asset.lastInspector && (
            <div className="ad-row">
              <span className="ad-k">最近巡检人</span>
              <span className="ad-v">{asset.lastInspector}</span>
            </div>
          )}
          {asset.lastSummary && (
            <div className="ad-summary">{asset.lastSummary}</div>
          )}
        </div>

        {/* 申请修改:旧版有、新版一直缺的发起端。审批闭环原来是断的 ——
            主管能批能驳,巡检员却发不出申请,发现填错了只能找人口头说。 */}
        <button className="ad-action" onClick={() => setCrOpen(true)}>
          申请修改
        </button>

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
                  <StatusTag text={s.status || "—"} />
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

      {asset && (
        <ChangeRequestSheet
          visible={crOpen}
          onClose={() => setCrOpen(false)}
          asset={asset}
          history={snaps}
          onSubmitted={() => nav("/approvals")}
        />
      )}
    </div>
  );
}
