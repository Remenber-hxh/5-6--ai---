import { Dialog, Toast } from "@/ui";
import { useEffect, useMemo, useState } from "react";

import { PendingShot } from "@/lib/offlineStore";
import { usePending } from "@/store/pending";

function fmtSize(bytes: number): string {
  // 早期记录可能没有 size,算出 NaN 会直接显示给用户 —— 兜底成 0
  if (!Number.isFinite(bytes) || bytes <= 0) return "";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

function fmtTime(iso: string): string {
  return new Date(iso).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" });
}

/** 单张卡片。Blob URL 在卸载时回收,连拍多张不漏内存 */
function ShotCard({
  shot,
  selecting,
  selected,
  onToggle,
}: {
  shot: PendingShot;
  selecting: boolean;
  selected: boolean;
  onToggle: () => void;
}) {
  const url = useMemo(() => URL.createObjectURL(shot.blob), [shot.blob]);
  useEffect(() => () => URL.revokeObjectURL(url), [url]);
  const { remove, retry } = usePending();

  async function onDelete() {
    const ok = await Dialog.confirm({
      content: "删除这张照片?本地原图会一并清掉,不可恢复。",
      confirmText: "删除",
      cancelText: "取消",
    });
    if (ok) await remove(shot.id);
  }

  // 多选模式下整张卡片可点选;上传中的不参与选择,避免删到正在传的
  const pickable = selecting && shot.status !== "uploading";

  return (
    <div
      className={`shot-card shot-${shot.status}${selected ? " shot-picked" : ""}`}
      onClick={pickable ? onToggle : undefined}
    >
      <img src={url} alt="" className="shot-img" />

      {shot.status === "uploading" && (
        <span className="shot-overlay">
          <span className="spinner" />
        </span>
      )}
      {shot.status === "blocked" && <span className="shot-flag flag-blocked">需处理</span>}
      {shot.status === "failed" && <span className="shot-flag flag-failed">待重试</span>}

      <span className="shot-time">{fmtTime(shot.capturedAt)}</span>

      {selecting ? (
        pickable && <span className="shot-pick">{selected ? "✓" : ""}</span>
      ) : (
        <>
          {shot.status !== "uploading" && (
            <button className="shot-x" onClick={() => void onDelete()} aria-label="删除">
              ×
            </button>
          )}
          {shot.status === "blocked" && (
            <button className="shot-retry" onClick={() => void retry(shot.id)}>
              重试
            </button>
          )}
        </>
      )}
    </div>
  );
}

export default function PendingPanel() {
  const { shots, online, persisted, usedBytes, flush, unblock, removeMany } = usePending();
  const [flushing, setFlushing] = useState(false);
  const [selecting, setSelecting] = useState(false);
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const stats = useMemo(() => {
    const blocked = shots.filter((s) => s.status === "blocked").length;
    const uploading = shots.filter((s) => s.status === "uploading").length;
    const stuck = shots.filter((s) => s.status === "blocked" || s.status === "failed").length;
    const bytes = shots.reduce((sum, s) => sum + (Number(s.size) || s.blob?.size || 0), 0);
    return { blocked, uploading, stuck, bytes };
  }, [shots]);

  // 照片传完自动消失,退出多选模式免得留个空壳
  useEffect(() => {
    if (selecting && shots.length === 0) setSelecting(false);
  }, [selecting, shots.length]);

  function toggle(id: string) {
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function exitSelecting() {
    setSelecting(false);
    setPicked(new Set());
  }

  async function onFlush() {
    if (flushing) return;
    setFlushing(true);
    try {
      const n = await flush();
      Toast.show({ content: n > 0 ? `已上传 ${n} 张` : "暂无可上传的照片", position: "bottom" });
    } finally {
      setFlushing(false);
    }
  }

  async function deletePicked() {
    if (!picked.size) return;
    // 未上传成功的原图删了就没了,必须说清楚代价再删
    const ok = await Dialog.confirm({
      content: `删除选中的 ${picked.size} 张照片?这些照片还没上传成功,本地原图会一并清掉,不可恢复。`,
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    const n = await removeMany([...picked]);
    Toast.show({ content: `已删除 ${n} 张`, position: "bottom" });
    exitSelecting();
  }

  if (!shots.length) {
    return (
      <div className="pending-panel pending-idle">
        <span className="pending-idle-text">
          拍下的照片会先存在这台手机上{online ? ",联网后自动上传" : ""}
        </span>
      </div>
    );
  }

  const firstError = shots.find((s) => s.status === "blocked")?.lastError;
  const selectable = shots.filter((s) => s.status !== "uploading");

  return (
    <div className="pending-panel">
      <div className="pending-head">
        {selecting ? (
          <>
            <div className="pending-title">
              <span className="pending-count">{picked.size}</span>
              <span className="pending-label">张已选</span>
            </div>
            <span className="sel-acts">
              <button
                className="sel-btn"
                onClick={() =>
                  setPicked(
                    picked.size === selectable.length
                      ? new Set()
                      : new Set(selectable.map((s) => s.id)),
                  )
                }
              >
                {picked.size === selectable.length ? "取消全选" : "全选"}
              </button>
              {/* 只删卡住的:最常见的诉求,不用一张张挑 */}
              {stats.stuck > 0 && (
                <button
                  className="sel-btn"
                  onClick={() =>
                    setPicked(
                      new Set(
                        shots
                          .filter((s) => s.status === "blocked" || s.status === "failed")
                          .map((s) => s.id),
                      ),
                    )
                  }
                >
                  选失败的
                </button>
              )}
              <button className="sel-btn danger" disabled={!picked.size} onClick={() => void deletePicked()}>
                删除
              </button>
              <button className="sel-btn" onClick={exitSelecting}>
                取消
              </button>
            </span>
          </>
        ) : (
          <>
            <div className="pending-title">
              <span className="pending-count">{shots.length}</span>
              <span className="pending-label">张待上传</span>
              <span className="pending-size">{fmtSize(stats.bytes)}</span>
            </div>
            <span className="sel-acts">
              <button className="sel-btn" onClick={() => setSelecting(true)}>
                选择
              </button>
              {online ? (
                <button className="pending-action" onClick={() => void onFlush()} disabled={flushing}>
                  {stats.uploading > 0 || flushing ? "上传中…" : "立即上传"}
                </button>
              ) : (
                <span className="pending-wait">来网自动上传</span>
              )}
            </span>
          </>
        )}
      </div>

      <div className="pending-strip">
        {shots.map((s) => (
          <ShotCard
            key={s.id}
            shot={s}
            selecting={selecting}
            selected={picked.has(s.id)}
            onToggle={() => toggle(s.id)}
          />
        ))}
      </div>

      {stats.blocked > 0 && !selecting && (
        <div className="pending-alert">
          <span className="pa-msg">
            {stats.blocked} 张需要处理{firstError ? `:${firstError}` : ""}
          </span>
          {/* 一次解除全部,而不是让用户逐张点重试 */}
          <button
            className="pa-btn"
            onClick={async () => {
              const n = await unblock(false);
              Toast.show({
                content: n > 0 ? `已重新排队 ${n} 张` : "没有可重试的照片",
                position: "bottom",
              });
            }}
          >
            全部重试
          </button>
        </div>
      )}
      {!persisted && usedBytes > 0 && !selecting && (
        <div className="pending-note">
          提示:本机未授予持久化存储,空间紧张时系统可能清理本地照片,建议尽快联网上传
        </div>
      )}
    </div>
  );
}
