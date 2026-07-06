// 业务状态口径:与旧版 admin-frontend 的 recordBusinessStatus 完全一致
export interface RecordField {
  code?: string;
  label?: string;
  value?: string;
  aiValue?: string;
  confidence?: number;
  needsReview?: boolean;
}

export interface InspectionRecord {
  id: string;
  recordNo?: string;
  pointId?: string;
  pointName?: string;
  templateName?: string;
  inspector?: string;
  project?: string;
  manualRequired?: boolean;
  recognitionStatus?: string;
  submitted?: boolean;
  submittedAt?: string;
  createdAt?: string;
  aiSummary?: string;
  report?: string;
  fields?: RecordField[];
  images?: { url?: string; path?: string }[];
}

const ABNORMAL_VALUE_RE =
  /异常|告警|故障|离线|不合格|超标|漏水|渗漏|报警|破损|损坏|缺失|跳闸|烧毁/;

function recordLevel(r: InspectionRecord): "danger" | "warning" | "normal" {
  const valueText = (r.fields || []).map((f) => String(f.value || "")).join(" ");
  if (ABNORMAL_VALUE_RE.test(valueText)) return "danger";
  if (r.submitted) return "normal";
  if ((r.fields || []).some((f) => f.needsReview)) return "warning";
  if (r.recognitionStatus === "retake_required") return "warning";
  return "normal";
}

function hasInspectionResult(r: InspectionRecord): boolean {
  if (r.submitted || r.recognitionStatus === "recognized") return true;
  return (r.fields || []).some((f) => String(f.value || "").trim());
}

export function recordBusinessStatus(r: InspectionRecord): string {
  if (r.manualRequired || r.recognitionStatus === "manual_required") return "人工填写";
  if (r.recognitionStatus === "retake_required") return "需补图";
  const level = recordLevel(r);
  if (level === "danger") return "异常";
  if (level === "warning") return "待复核";
  if (r.submitted) return "已完成";
  if (hasInspectionResult(r)) return "正常";
  return "待复核";
}

// 图片地址口径与旧版 mediaUrl 一致:后端以 /storage/ 提供上传文件
export function mediaUrl(path?: string): string {
  if (!path) return "";
  if (/^https?:\/\//i.test(path)) return path;
  const normalized = String(path).replace(/\\/g, "/");
  const idx = normalized.indexOf("/storage/");
  let p = idx >= 0 ? normalized.slice(idx + "/storage/".length) : normalized;
  p = p.replace(/^\/?storage\//, "").replace(/^\/+/, "");
  return `/storage/${encodeURI(p)}`;
}

// 时间展示:ISO 串 → "MM-DD HH:mm"(带年份场景用 full)
export function fmtTime(iso?: string, full = false): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d.getTime())) return iso.slice(0, 16).replace("T", " ");
  const p = (n: number) => String(n).padStart(2, "0");
  const md = `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
  return full ? `${d.getFullYear()}-${md}` : md;
}

// antd Tag 颜色映射(红色只给真正异常,与设计规范一致)
export function statusTagColor(status: string): string {
  switch (status) {
    case "异常":
      return "red";
    case "待复核":
    case "需补图":
      return "orange";
    case "人工填写":
      return "gold";
    case "已完成":
    case "正常":
      return "green";
    default:
      return "default";
  }
}
