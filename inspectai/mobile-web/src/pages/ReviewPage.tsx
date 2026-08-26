import { Button, Dialog, Toast } from "@/ui";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import CenterLoading from "@/components/CenterLoading";
import FlowHeader from "@/components/FlowHeader";
import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import { useAuth } from "@/store/auth";
import {
  OfflineShotDTO,
  deleteOfflineShots,
  listOfflineShots,
  listTemplates,
} from "@/api/inspection";
import { useResource } from "@/hooks/useResource";
import { getRetakeTarget } from "@/store/retake";

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

// 已上传照片 → 选中 → 识别 → 确认模板 → 成单
// (扫码/复检进来的会跳过"确认模板"那一步,见 ClassifyPage)
export default function ReviewPage() {
  const nav = useNavigate();
  const [picked, setPicked] = useState<Set<string>>(new Set());
  // 存序号而非单张元数据:查看器现在能在整组待上传照片间左右翻。-1 = 未打开
  const [viewing, setViewing] = useState(-1);
  const [busy, setBusy] = useState(false);
  const user = useAuth((s) => s.user);

  const {
    data,
    loading,
    reload: load,
  } = useResource((signal) => listOfflineShots(signal), [], {
    errorText: "加载失败,请下拉重试",
  });
  const shots = data ?? [];

  // 扫码或复检进来时模板是【已知】的,可以在这一步就说清还差几张 ——
  // 而不是等填完一整张表、点提交才被后端打回来。普通流程要等 AI 分完场景
  // 才知道用哪个模板,那条路只能靠提交时的复核兜底。
  const [minImages, setMinImages] = useState(0);
  useEffect(() => {
    const known = getRetakeTarget();
    if (!known?.templateId) return;
    listTemplates()
      .then((tpls) => {
        const hit = tpls.find((t) => t.id === known.templateId);
        setMinImages(hit?.minImages || 0);
      })
      .catch(() => void 0);
  }, []);
  const shortOf = minImages > 0 ? Math.max(0, minImages - picked.size) : 0;

  // 默认全选:绝大多数情况就是把刚传的这批一起成单
  useEffect(() => {
    if (data) setPicked(new Set(data.map((s) => s.id)));
  }, [data]);

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
    if (shortOf > 0) {
      Toast.show({
        content: `这类巡检至少要 ${minImages} 张照片,还差 ${shortOf} 张`,
        duration: 3000,
      });
      return;
    }
    // 【一次只能提交一台设备的照片】扫码拍的照片自己记着是哪台(assetId)。
    // 不拦的话:扫 A 拍几张、走到 B 扫 B 再拍几张,这里一全选,六张全落到
    // 一条记录上 —— 而扫码流程跳过了 AI 场景分类,连"这些照片不像同一个场景"
    // 的兜底提示都没有,错得悄无声息。
    const devices = new Set(
      pickedIds.map((id) => shots.find((x) => x.id === id)?.assetId || ""),
    );
    if (devices.size > 1) {
      Toast.show({
      content: "选中的照片来自不同设备,请一次只提交一台",
      duration: 3000,
      });
      return;
    }
    // 识别与模板确认是独立一屏(旧版 sceneClassify),不塞在选图页里。
    // 选中项走 URL 参数而非 router state —— state 在刷新后会丢,用户一刷新
    // 就得重选一遍。
    nav(`/classify?shots=${encodeURIComponent(pickedIds.join(","))}`);
  }

  if (loading) {
    return <CenterLoading />;
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
            {minImages > 0 && (
              <span className={shortOf > 0 ? "rv-short" : "rv-ok"}>
                {shortOf > 0 ? ` · 还差 ${shortOf} 张` : " · 张数已够"}
              </span>
            )}
          </span>
          <button
            className="sel-btn"
            onClick={() =>
              setPicked(
                picked.size === shots.length
                  ? new Set()
                  : new Set(shots.map((s) => s.id)),
              )
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
                {/* 格子只有 110px,原图是 900×1600 —— 一页 20 张就是 1.1MB。
                    ?w=240 让后端出小图(2 倍屏够用),大图留给点开看的查看器。 */}
                <img
                  src={`/api/inspection/offline-shots/${s.id}/image?w=240`}
                  alt=""
                  loading="lazy"
                />
                <span className="cell-check">
                  {picked.has(s.id) ? "✓" : ""}
                </span>
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

      <PhotoViewer
        photos={photos}
        index={viewing}
        onClose={() => setViewing(-1)}
      />

      <div className="flow-foot foot-row">
        <button
          className="foot-del"
          disabled={!picked.size || busy}
          onClick={() => void onDelete()}
        >
          删除{picked.size ? ` (${picked.size})` : ""}
        </button>
        {/* 【只写"识别"】原来是"AI 识别场景"。扫码进来时场景早就定了,
            这一步只做字段识别 —— 按钮却还说要识别场景,和实际做的事对不上。
            两条路都写"识别"都准确:普通流程它含场景+字段,扫码流程它就是字段。 */}
        <Button className="btn-primary foot-main" onClick={toClassify}>
          识别
        </Button>
      </div>
    </div>
  );
}
