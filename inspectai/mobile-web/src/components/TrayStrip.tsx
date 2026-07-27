import { Dialog } from "antd-mobile";
import { useEffect, useMemo, useState } from "react";

import { PendingShot } from "@/lib/offlineStore";
import { useTray } from "@/store/tray";

/** 一张缩略图。Blob URL 需在卸载时回收,否则连拍几十张会漏内存 */
function Thumb({ shot, onRemove }: { shot: PendingShot; onRemove: () => void }) {
  const url = useMemo(() => URL.createObjectURL(shot.blob), [shot.blob]);
  useEffect(() => () => URL.revokeObjectURL(url), [url]);

  const time = new Date(shot.capturedAt).toLocaleTimeString("zh-CN", {
    hour: "2-digit",
    minute: "2-digit",
  });

  return (
    <div className="thumb">
      <img src={url} alt={shot.fileName} />
      <span className="thumb-time">{time}</span>
      {shot.status === "failed" && <span className="thumb-badge thumb-failed">失败</span>}
      {shot.status === "uploading" && <span className="thumb-badge">上传中</span>}
      <button className="thumb-del" onClick={onRemove} aria-label="删除这张">
        ×
      </button>
    </div>
  );
}

export default function TrayStrip({ shots }: { shots: PendingShot[] }) {
  const remove = useTray((s) => s.remove);
  const online = useTray((s) => s.online);
  const [removing, setRemoving] = useState(false);

  if (!shots.length) {
    return (
      <div className="tray tray-empty">
        <span>托盘是空的 · 拍下的照片会先存在这台手机上</span>
      </div>
    );
  }

  async function confirmRemove(shot: PendingShot) {
    if (removing) return;
    const ok = await Dialog.confirm({
      content: "删除这张照片?本地存的原图会一并清掉,不可恢复。",
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    setRemoving(true);
    try {
      await remove(shot.id);
    } finally {
      setRemoving(false);
    }
  }

  return (
    <div className="tray">
      <div className="tray-head">
        <span className="tray-count">托盘 {shots.length} 张</span>
        <span className="tray-hint">{online ? "联网后自动上传识别" : "离线暂存,来网自动上传"}</span>
      </div>
      <div className="tray-strip">
        {shots.map((s) => (
          <Thumb key={s.id} shot={s} onRemove={() => void confirmRemove(s)} />
        ))}
      </div>
    </div>
  );
}
