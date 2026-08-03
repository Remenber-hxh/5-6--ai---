import { Button, Dialog, Toast } from "@/ui";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import { useAuth } from "@/store/auth";
import { OfflineShotDTO, deleteOfflineShots, listOfflineShots } from "@/api/inspection";

/** 拍摄与上传的时间差:离线越久差越大。公开展示,不隐藏 */
function offlineGap(shot: OfflineShotDTO): string {
  const cap = new Date(shot.capturedAt).getTime();
  const rec = new Date(shot.receivedAt).getTime();
  if (!cap || !rec || rec <= cap) return "";
  const mins = Math.round((rec - cap) / 60000);
  if (mins < 1) return "";
  if (mins < 60) return `离线 ${mins} 分钟`;
  return `离线 ${Math.round(mins / 60)} 小时`;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
}

// 已上传照片 → 选中 → AI 识别场景 → 确认模板 → 成单
export default function ReviewPage() {
  const nav = useNavigate();
  const [shots, setShots] = useState<OfflineShotDTO[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [loading, setLoading] = useState(true);
  // 存序号而非单张元数据:查看器现在能在整组待上传照片间左右翻。-1 = 未打开
  const [viewing, setViewing] = useState(-1);
  const [busy, setBusy] = useState(false);
  const user = useAuth((s) => s.user);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const list = await listOfflineShots();
      setShots(list);
      // 默认全选:绝大多数情况就是把刚传的这批一起成单
      setPicked(new Set(list.map((s) => s.id)));
    } catch {
      Toast.show({ content: "加载失败,请下拉重试" });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const pickedIds = useMemo(() => [...picked], [picked]);

  // 网格和查看器用同一份、同一顺序 —— 否则点第 3 张会翻到别的图上
  const photos: PhotoMeta[] = shots.map((s) => ({
    url: `/api/inspection/offline-shots/${s.id}/image`,
    fileName: s.fileName,
    capturedAt: s.capturedAt,
    receivedAt: s.receivedAt,
    inspector: user?.displayName || user?.username,
    lat: s.lat,
    lng: s.lng,
  }));

  function toggle(id: string) {
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  async function onDelete() {
    if (!pickedIds.length || busy) return;
    // 服务器上的原图删了就没了,必须把代价说清楚
    const ok = await Dialog.confirm({
      content: `删除选中的 ${pickedIds.length} 张照片?服务器上的原图会一并清掉,不可恢复。`,
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    setBusy(true);
    try {
      const n = await deleteOfflineShots(pickedIds);
      Toast.show({ content: `已删除 ${n} 张`, position: "bottom" });
      setPicked(new Set());
      await load();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "删除失败" });
    } finally {
      setBusy(false);
    }
  }

  function toClassify() {
    if (!pickedIds.length) {
      Toast.show({ content: "请先选择照片" });
      return;
    }
    // 识别与模板确认是独立一屏(旧版 sceneClassify),不塞在选图页里。
    // 选中项走 URL 参数而非 router state —— state 在刷新后会丢,用户一刷新
    // 就得重选一遍。
    nav(`/classify?shots=${encodeURIComponent(pickedIds.join(","))}`);
  }

  if (loading) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  if (!shots.length) {
    return (
      <div className="center-screen">
        <h1 className="screen-title" style={{ textAlign: "center" }}>
          没有待处理的照片
        </h1>
        <p className="screen-sub" style={{ textAlign: "center" }}>
          先去拍照,联网后照片会自动上传到这里
        </p>
        <Button block className="btn-ghost" onClick={() => nav("/")}>
          去拍照
        </Button>
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <FlowHeader title="选择照片" onBack={() => nav("/")} step="review" />

      <div className="scroll-area flow-body">
        <div className="flow-sub-row flow-caption">
          <span>
            已上传 {shots.length} 张 · 选中 {picked.size} 张
          </span>
          <button
            className="sel-btn"
            onClick={() =>
              setPicked(picked.size === shots.length ? new Set() : new Set(shots.map((s) => s.id)))
            }
          >
            {picked.size === shots.length ? "全不选" : "全选"}
          </button>
        </div>

        <div className="shot-grid">
          {shots.map((s, i) => {
            const gap = offlineGap(s);
            return (
              <button
                key={s.id}
                className={picked.has(s.id) ? "grid-cell picked" : "grid-cell"}
                onClick={() => toggle(s.id)}
              >
                <img src={`/api/inspection/offline-shots/${s.id}/image`} alt="" loading="lazy" />
                <span className="cell-check">{picked.has(s.id) ? "✓" : ""}</span>
                <span className="cell-meta">
                  {fmtTime(s.capturedAt)}
                  {gap && <em className="cell-gap">{gap}</em>}
                </span>
                <span
                  className="cell-zoom"
                  role="button"
                  aria-label="查看大图"
                  onClick={(e) => {
                    e.stopPropagation(); // 看图不改变选中状态
                    setViewing(i);
                  }}
                >
                  ⛶
                </span>
              </button>
            );
          })}
        </div>

      </div>

      <PhotoViewer photos={photos} index={viewing} onClose={() => setViewing(-1)} />

      <div className="flow-foot foot-row">
        <button className="foot-del" disabled={!picked.size || busy} onClick={() => void onDelete()}>
          删除{picked.size ? ` (${picked.size})` : ""}
        </button>
        <Button className="btn-primary foot-main" onClick={toClassify}>
          AI 识别场景
        </Button>
      </div>
    </div>
  );
}
