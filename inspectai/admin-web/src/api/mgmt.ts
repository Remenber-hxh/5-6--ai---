// 管理域 API:与 Go 后端既有契约一一对应(勿改字段名)
import { api } from "./client";
import type { InspectionRecord } from "../lib/status";

export interface ChatSource {
  type: "record" | "asset" | "standard" | "official";
  title?: string;
  summary?: string;
  recordId?: string;
  assetId?: string;
  detail?: string;
  url?: string;
}

export interface ChatResponse {
  reply?: string;
  sources?: ChatSource[];
  isMock?: boolean;
}

export interface ActionProposal {
  type: string; // create_recheck_task
  asset: string; // 可读编号
  assignee?: string;
  dueAt?: string;
  reason?: string;
}

export interface AssetEntry {
  id: string;
  assetKey?: string;
  assetName?: string;
  assetType?: string;
  project?: string;
  pointId?: string;
  pointName?: string;
  statusLevel?: string;
  lastStatus?: string;
  lastInspectedAt?: string;
  lastRecordId?: string;
}

export interface ChangeRequest {
  id: string;
  type?: string;
  status?: string; // pending / approved / rejected
  reason?: string;
  requesterName?: string;
  assetId?: string;
  assetName?: string;
  recordId?: string;
  createdAt?: string;
  fieldKey?: string;
  fieldLabel?: string;
  oldValue?: string;
  newValue?: string;
  reviewNote?: string;
}

export function chat(message: string, history: { role: string; text: string }[]) {
  return api<ChatResponse>("/api/management-ai/chat", {
    method: "POST",
    body: JSON.stringify({ message, history }),
  });
}

export function act(type: string, targetId: string, params: Record<string, string>) {
  return api<{ message?: string; task?: { id?: string; assigneeName?: string; dueAt?: string } }>(
    "/api/management-ai/act",
    { method: "POST", body: JSON.stringify({ type, targetId, params }) },
  );
}

export function weeklyReport() {
  return api<any>("/api/management-ai/report?type=weekly");
}

export function listAssets() {
  return api<{ assets: AssetEntry[] }>("/api/assets").then((d) => d.assets || []);
}

export function listRecords() {
  return api<{ records: InspectionRecord[] }>("/api/inspection/records").then(
    (d) => d.records || [],
  );
}

export function listChangeRequests() {
  return api<{ requests: ChangeRequest[] }>("/api/change-requests").then(
    (d) => d.requests || [],
  );
}

export function reviewChangeRequest(id: string, action: "approve" | "reject") {
  return api(`/api/change-requests/${encodeURIComponent(id)}/${action}`, {
    method: "POST",
    body: JSON.stringify({
      reviewNote: action === "approve" ? "后台审批通过" : "后台审批拒绝",
    }),
  });
}

// AI 回复里的动作提议块:<<ACTION>>{json}<<END>>
export function extractActionProposal(reply: string): {
  text: string;
  proposal: ActionProposal | null;
} {
  const m = reply.match(/<<ACTION>>([\s\S]*?)<<END>>/);
  if (!m) return { text: reply, proposal: null };
  let proposal: ActionProposal | null = null;
  try {
    proposal = JSON.parse(m[1].trim()) as ActionProposal;
  } catch {
    proposal = null;
  }
  return { text: reply.replace(m[0], "").trim(), proposal };
}
