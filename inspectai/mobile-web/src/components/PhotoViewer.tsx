import { Toast } from "@/ui";
import { useEffect, useRef, useState } from "react";

// ===== 带水印的照片查看器 =====
//
// 设计原则:原图完整保留作证据(EXIF 不动),水印只在「查看 / 导出」时渲染为叠层。
// 好处:水印可重画、可纠错,而原始证据不被破坏。
//
// 必须讲清楚的边界:客户端画上去的水印是给人看的,不是证据。
// 真正构成证据链的是服务器盖的收到时间 + 账号绑定 + 操作日志。
// 水印的价值在于:让复核的人一眼看到时间/人/地点,以及提高随手造假的成本。

export interface PhotoMeta {
  url: string;
  fileName?: string;
  /** 拍摄时间(手机盖,可伪造) */
  capturedAt?: string;
  /** 服务器收到时间(权威) */
  receivedAt?: string;
  inspector?: string;
  project?: string;
  location?: string;
  lat?: number;
  lng?: number;
}

function fmt(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

/** 离线时长:拍摄与上传之间的差,公开展示不隐藏 */
function gapText(captured?: string, received?: string): string {
  if (!captured || !received) return "";
  const a = new Date(captured).getTime();
  const b = new Date(received).getTime();
  if (!a || !b || b <= a) return "";
  const mins = Math.round((b - a) / 60000);
  if (mins < 1) return "";
  return mins < 60 ? `离线 ${mins} 分钟后上传` : `离线 ${Math.round(mins / 60)} 小时后上传`;
}

/** 水印文本行 —— 查看与导出共用同一份,保证所见即所得 */
function watermarkLines(meta: PhotoMeta): string[] {
  const lines: string[] = [];
  if (meta.capturedAt) lines.push(`拍摄 ${fmt(meta.capturedAt)}`);
  const gap = gapText(meta.capturedAt, meta.receivedAt);
  if (meta.receivedAt) lines.push(`上传 ${fmt(meta.receivedAt)}${gap ? ` · ${gap}` : ""}`);
  if (meta.inspector) lines.push(`巡检 ${meta.inspector}`);
  if (meta.project || meta.location) lines.push([meta.project, meta.location].filter(Boolean).join(" · "));
  if (typeof meta.lat === "number" && typeof meta.lng === "number") {
    lines.push(`定位 ${meta.lat.toFixed(5)}, ${meta.lng.toFixed(5)}`);
  }
  return lines;
}

/** 把原图 + 水印画进 canvas,导出为可保存的图片 */
async function renderWatermarked(meta: PhotoMeta): Promise<Blob | null> {
  const img = new Image();
  img.crossOrigin = "anonymous";
  await new Promise<void>((resolve, reject) => {
    img.onload = () => resolve();
    img.onerror = () => reject(new Error("图片加载失败"));
    img.src = meta.url;
  });

  const canvas = document.createElement("canvas");
  canvas.width = img.naturalWidth;
  canvas.height = img.naturalHeight;
  const ctx = canvas.getContext("2d");
  if (!ctx) return null;
  ctx.drawImage(img, 0, 0);

  // 字号随图片尺寸缩放,保证大图小图上水印观感一致
  const lines = watermarkLines(meta);
  const scale = Math.max(canvas.width, canvas.height) / 1000;
  const fontSize = Math.max(14, Math.round(18 * scale));
  const pad = Math.round(fontSize * 0.9);
  const lineH = Math.round(fontSize * 1.5);
  const blockH = lines.length * lineH + pad * 2;

  // 底部渐变遮罩:保证浅色照片上文字也读得清
  const grad = ctx.createLinearGradient(0, canvas.height - blockH * 1.4, 0, canvas.height);
  grad.addColorStop(0, "rgba(0,0,0,0)");
  grad.addColorStop(1, "rgba(0,0,0,0.72)");
  ctx.fillStyle = grad;
  ctx.fillRect(0, canvas.height - blockH * 1.4, canvas.width, blockH * 1.4);

  ctx.font = `600 ${fontSize}px -apple-system, "PingFang SC", sans-serif`;
  ctx.textBaseline = "alphabetic";
  let y = canvas.height - pad - (lines.length - 1) * lineH;
  for (const line of lines) {
    ctx.fillStyle = "rgba(255,255,255,0.96)";
    ctx.fillText(line, pad, y);
    y += lineH;
  }

  // 右下角品牌标,表明来源
  ctx.font = `800 ${Math.round(fontSize * 0.85)}px -apple-system, sans-serif`;
  ctx.fillStyle = "rgba(67,229,172,0.9)";
  const mark = "智巡 JADEAST";
  ctx.fillText(mark, canvas.width - ctx.measureText(mark).width - pad, canvas.height - pad);

  return new Promise((resolve) => canvas.toBlob(resolve, "image/jpeg", 0.92));
}

export default function PhotoViewer({ meta, onClose }: { meta: PhotoMeta; onClose: () => void }) {
  const [exporting, setExporting] = useState(false);
  const lines = watermarkLines(meta);
  const closeRef = useRef<HTMLButtonElement>(null);

  // Esc 关闭 + 打开时焦点落在关闭按钮,键盘可操作
  useEffect(() => {
    closeRef.current?.focus();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function onExport() {
    if (exporting) return;
    setExporting(true);
    try {
      const blob = await renderWatermarked(meta);
      if (!blob) throw new Error("导出失败");
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = (meta.fileName || "photo").replace(/\.[^.]+$/, "") + "_水印.jpg";
      a.click();
      URL.revokeObjectURL(url);
      Toast.show({ content: "已保存带水印的照片", position: "bottom" });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "导出失败" });
    } finally {
      setExporting(false);
    }
  }

  return (
    <div className="viewer" role="dialog" aria-label="查看照片">
      <button ref={closeRef} className="viewer-close" onClick={onClose} aria-label="关闭">
        ×
      </button>
      <div className="viewer-stage">
        <img src={meta.url} alt={meta.fileName || ""} />
        {/* 水印是叠层,不改动原图 */}
        <div className="viewer-mark">
          {lines.map((l) => (
            <div key={l}>{l}</div>
          ))}
          <span className="viewer-brand">智巡 JADEAST</span>
        </div>
      </div>
      <div className="viewer-foot">
        <button className="viewer-btn" onClick={() => void onExport()} disabled={exporting}>
          {exporting ? "导出中…" : "保存带水印的照片"}
        </button>
      </div>
    </div>
  );
}
