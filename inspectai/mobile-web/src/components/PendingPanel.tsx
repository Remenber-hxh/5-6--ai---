import { Dialog, Toast } from "antd-mobile";
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
function ShotCard({ shot }: { shot: PendingShot }) {
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

  return (
    <div className={`shot-card shot-${shot.status}`}>
      <img src={url} alt="" className="shot-img" />

      {shot.status === "uploading" && (
        <span className="shot-overlay">
          <span className="spinner" />
        </span>
      )}
      {shot.status === "blocked" && <span className="shot-flag flag-blocked">需处理</span>}
      {shot.status === "failed" && <span className="shot-flag flag-failed">待重试</span>}

      <span className="shot-time">{fmtTime(shot.capturedAt)}</span>

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
    </div>
  );
}

export default function PendingPanel() {
  const { shots, online, persisted, usedBytes, flush } = usePending();
  const [flushing, setFlushing] = useState(false);

  const stats = useMemo(() => {
    const blocked = shots.filter((s) => s.status === "blocked").length;
    const uploading = shots.filter((s) => s.status === "uploading").length;
    const bytes = shots.reduce((sum, s) => sum + (Number(s.size) || s.blob?.size || 0), 0);
    return { blocked, uploading, bytes };
  }, [shots]);

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

  return (
    <div className="pending-panel">
      <div className="pending-head">
        <div className="pending-title">
          <span className="pending-count">{shots.length}</span>
          <span className="pending-label">张待上传</span>
          <span className="pending-size">{fmtSize(stats.bytes)}</span>
        </div>
        {online ? (
          <button className="pending-action" onClick={() => void onFlush()} disabled={flushing}>
            {stats.uploading > 0 || flushing ? "上传中…" : "立即上传"}
          </button>
        ) : (
          <span className="pending-wait">来网自动上传</span>
        )}
      </div>

      <div className="pending-strip">
        {shots.map((s) => (
          <ShotCard key={s.id} shot={s} />
        ))}
      </div>

      {stats.blocked > 0 && (
        <div className="pending-alert">
          {stats.blocked} 张需要处理{firstError ? `:${firstError}` : ""}
        </div>
      )}
      {!persisted && usedBytes > 0 && (
        <div className="pending-note">
          提示:本机未授予持久化存储,空间紧张时系统可能清理本地照片,建议尽快联网上传
        </div>
      )}
    </div>
  );
}
