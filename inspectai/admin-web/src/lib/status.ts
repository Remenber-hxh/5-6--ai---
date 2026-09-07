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
  captureAttempts?: number;
  recognitionStatus?: string;
  submitted?: boolean;
  submittedAt?: string;
  createdAt?: string;
  aiSummary?: string;
  report?: string;
  fields?: RecordField[];
  images?: { url?: string; path?: string }[];
  /** 后端算好的业务状态。见 go-backend/cmd/server/record_status.go */
  businessStatus?: string;
}

// 业务状态不再由前端计算 —— 见下。

/**
 * 记录的业务状态。
 *
 * 【规则搬到后端了】原来这里有一份完整实现(异常词表 + 优先级判断)。
 * 搬走的原因不是嫌前端算得慢,而是后端也需要它 —— 按状态导出、看板按
 * 状态聚合,都得知道什么叫"异常"。留两份实现的话,迟早出现【导出里的
 * 状态和页面上显示的不一样】,而没人会想到是两套代码在算同一件事。
 *
 * 现在唯一实现在 go-backend/cmd/server/record_status.go,规则由
 * record_status_test.go 逐条钉住。前端只负责显示。
 *
 * (顺带查出来的:后端另有一份"日报口径"在跑,和界面口径有 5 处不一样。
 * 那是已知的、有意保留的分歧,同样钉在那个测试里。)
 */
export function recordBusinessStatus(r: InspectionRecord): string {
  return r.businessStatus || "";
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
