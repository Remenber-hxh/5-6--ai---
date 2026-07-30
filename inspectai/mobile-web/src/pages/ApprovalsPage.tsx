import { Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  ChangeRequestDTO,
  approveChangeRequest,
  listPendingChangeRequests,
  rejectChangeRequest,
} from "@/api/inspection";
import { useAuth } from "@/store/auth";

/** 把 patch 翻成人话。审批人要能一眼看懂"改的是什么" */
function describePatch(cr: ChangeRequestDTO): string {
  const p = (cr.patch || {}) as Record<string, string>;
  const cut = (s: string, n = 24) => (s.length > n ? s.slice(0, n) + "…" : s);
  const parts: string[] = [];
  if (cr.targetType === "asset") {
    if (p.assetName) parts.push(`资产名 → ${p.assetName}`);
    if (p.lastStatus) parts.push(`状态 → ${p.lastStatus}`);
    if (p.lastSummary !== undefined) parts.push(`总结 → ${cut(String(p.lastSummary))}`);
  } else {
    // 记录类:patch 里是字段码 → 新值。没有标签就直接显示字段码,总比不显示强
    Object.entries(p).forEach(([k, v]) => parts.push(`${k} → ${cut(String(v))}`));
  }
  return parts.join(" · ") || "(无明细)";
}

function fmtWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getMonth() + 1}/${d.getDate()} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

function Card({ cr, onDone }: { cr: ChangeRequestDTO; onDone: () => void }) {
  const nav = useNavigate();
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState(false);

  async function act(kind: "approve" | "reject") {
    if (busy) return;
    // 驳回必须写理由:被驳回的人有权知道为什么,这也是留痕的一部分
    if (kind === "reject" && !note.trim()) {
      Toast.show({ content: "驳回时请填写理由" });
      return;
    }
    setBusy(true);
    try {
      if (kind === "approve") await approveChangeRequest(cr.id, note.trim());
      else await rejectChangeRequest(cr.id, note.trim());
      Toast.show({ content: kind === "approve" ? "已通过" : "已驳回", position: "bottom" });
      onDone();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "操作失败" });
      setBusy(false);
    }
  }

  return (
    <div className="cr-card">
      <div className="cr-head">
        <b className="cr-who">{cr.requestedBy || "—"}</b>
        <span className="cr-when">{fmtWhen(cr.requestedAt)}</span>
      </div>

      <div className="cr-target">
        <span className="cr-tt">{cr.targetType === "asset" ? "资产" : "巡检记录"}</span>
        <span className="cr-tn">{cr.targetLabel || cr.targetId}</span>
        {cr.targetType === "asset" && (
          <button
            className="cr-view"
            onClick={() => nav(`/asset/${encodeURIComponent(cr.targetId)}`)}
          >
            查看
          </button>
        )}
      </div>

      <div className="cr-patch">{describePatch(cr)}</div>
      {cr.reason && <div className="cr-reason">理由:{cr.reason}</div>}

      <input
        className="cr-note"
        value={note}
        placeholder="审批意见(驳回必填)"
        onChange={(e) => setNote(e.target.value)}
      />
      <div className="cr-acts">
        <button className="cr-btn reject" disabled={busy} onClick={() => void act("reject")}>
          驳回
        </button>
        <button className="cr-btn approve" disabled={busy} onClick={() => void act("approve")}>
          通过
        </button>
      </div>
    </div>
  );
}

// 待审批(旧版 sceneApprovals):数据修改申请的通过/驳回
export default function ApprovalsPage() {
  const nav = useNavigate();
  const user = useAuth((s) => s.user);
  const [list, setList] = useState<ChangeRequestDTO[] | null>(null);

  const load = useCallback(async () => {
    try {
      setList(await listPendingChangeRequests());
    } catch {
      Toast.show({ content: "加载失败" });
      setList([]);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // 前端门控只是体验:后端对通过/驳回同样按 approval_review 能力校验
  const canReview = ["admin", "manager", "supervisor"].includes(user?.roleCode || "");
  if (!canReview) {
    return (
      <div className="center-screen">
        <h1 className="screen-title" style={{ textAlign: "center" }}>
          没有审批权限
        </h1>
        <p className="screen-sub" style={{ textAlign: "center" }}>
          数据修改审批由复核主管及以上处理
        </p>
        <button className="task-back" onClick={() => nav("/")}>
          返回拍照
        </button>
      </div>
    );
  }

  if (!list) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">待审批</h1>
        <p className="flow-sub">数据修改申请 {list.length} 条</p>
      </div>

      <div className="scroll-area flow-body">
        {list.length === 0 ? (
          <div className="task-empty">当前无待审批申请</div>
        ) : (
          list.map((cr) => <Card key={cr.id} cr={cr} onDone={() => void load()} />)
        )}
      </div>

      <div className="flow-foot">
        <button className="task-back" onClick={() => nav("/")}>
          返回拍照
        </button>
      </div>
    </div>
  );
}
