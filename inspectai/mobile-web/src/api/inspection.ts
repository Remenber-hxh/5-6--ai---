// 巡检流程 API —— 与旧版 frontend/ 走同一批后端接口,契约保持一致。

import { api, getToken } from "@/api/client";

export interface OfflineShotDTO {
  id: string;
  fileName: string;
  sizeBytes: number;
  /** 手机声称的拍摄时间(可伪造,仅供参考) */
  capturedAt: string;
  /** 服务器收到时间(权威)。与 capturedAt 的差 = 离线时长,界面公开展示 */
  receivedAt: string;
  lat?: number;
  lng?: number;
  recordId?: string;
  status: string;
}

export interface ClassifyResult {
  templateId: string;
  templateName: string;
  confidence?: number;
  needsManualPick?: boolean;
  error?: string;
}

export interface FieldValue {
  code: string;
  label: string;
  kind: string;
  required: boolean;
  value: string;
  aiValue: string;
  source: string;
  options?: string[];
  confidence: number;
  needsReview: boolean;
  reason?: string;
  version: number;
}

export interface ImageInfo {
  id: string;
  fileName: string;
  path: string;
}

export interface RecordDTO {
  id: string;
  recordNo?: string;
  project: string;
  pointName: string;
  templateId: string;
  templateName: string;
  inspector: string;
  recognitionStatus: string;
  manualRequired?: boolean;
  /** 已尝试拍摄次数,达 3 次转人工(与旧版口径一致) */
  captureAttempts?: number;
  retakeReason?: string;
  fields: FieldValue[];
  images: ImageInfo[];
  report?: string;
  aiSummary?: string;
  aiSummaryTags?: string[];
  submitted: boolean;
}

export interface TemplateDTO {
  id: string;
  name: string;
  project: string;
  assetType: string;
}

/** 服务器上尚未成单的离线照片 */
export async function listOfflineShots(): Promise<OfflineShotDTO[]> {
  const body = await api<{ shots: OfflineShotDTO[] | null }>("/api/inspection/offline-shots?limit=200");
  return (body.shots || []).filter((s) => !s.recordId);
}

/** 用服务器上已有的照片做场景识别(不重传) */
export async function classifyOfflineShots(shotIds: string[]) {
  return api<{ classify: ClassifyResult; shots: OfflineShotDTO[] }>(
    "/api/inspection/offline-shots/classify",
    { method: "POST", body: JSON.stringify({ shotIds }) },
  );
}

export async function listTemplates(): Promise<TemplateDTO[]> {
  const body = await api<{ templates: TemplateDTO[] | null }>("/api/report/templates");
  return body.templates || [];
}

/** 成单:按 ID 认领离线照片(照片已在服务器上) */
export async function createRecordFromShots(templateId: string, shotIds: string[]) {
  return api<RecordDTO>("/api/inspection/records", {
    method: "POST",
    body: JSON.stringify({ templateId, offlineShotIds: shotIds }),
  });
}

export async function getRecord(id: string): Promise<RecordDTO> {
  return api<RecordDTO>(`/api/inspection/records/${id}`);
}

/** 触发 AI 字段识别(异步任务) */
export async function startAnalysis(id: string) {
  return api<{ taskId?: string }>(`/api/inspection/records/${id}/ai-tasks`, { method: "POST" });
}

export interface PatchFieldOpts {
  /** confirm 直接确认 / correct 改过值 —— 后端据此写字段确认留痕 */
  action?: "confirm" | "correct" | "uncertain";
  durationMs?: number;
  viewedPhoto?: boolean;
}

/**
 * 更新单个字段。
 *
 * 注意:后端返回的是【这一个字段】,不是整条记录 —— 曾把它当整条记录塞进
 * state,导致 rec.fields 变成 undefined、页面白屏。与旧版 frontend/ 的
 * `Object.assign(field, updated)` 语义保持一致:只合并这一个字段。
 */
export async function patchField(
  recordId: string,
  code: string,
  value: string,
  version: number,
  opts: PatchFieldOpts = {},
): Promise<FieldValue> {
  return api<FieldValue>(`/api/inspection/records/${recordId}/fields/${encodeURIComponent(code)}`, {
    method: "PATCH",
    body: JSON.stringify({ value, version, ...opts }),
  });
}

/** 转人工填写(AI 多次识别不稳时) */
export async function enableManual(id: string) {
  return api<RecordDTO>(`/api/inspection/records/${id}/manual`, { method: "POST" });
}

/** 提交。幂等键防弱网重复提交产生重复记录 */
export async function submitRecord(id: string): Promise<RecordDTO> {
  const res = await fetch(`/api/inspection/records/${id}/submit`, {
    method: "POST",
    headers: {
      "X-InspectAI-Token": getToken(),
      "Idempotency-Key": `sub_${id}_${Date.now().toString(36)}`,
    },
  });
  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) throw new Error(body.message || "提交失败");
  return body as RecordDTO;
}

// ===== 工程任务(我的任务) =====

export interface EngineeringTaskDTO {
  id: string;
  title?: string;
  workContent?: string;
  assetName?: string;
  project?: string;
  status: string; // 待执行 / 进行中 / 逾期 / 已完成
  dueAt?: string;
  templateId?: string;
}

export async function listEngineeringTasks(): Promise<EngineeringTaskDTO[]> {
  const body = await api<{ tasks: EngineeringTaskDTO[] | null }>("/api/engineering/tasks");
  return body.tasks || [];
}

/** 开始执行:乐观置为进行中,失败不阻断巡检(提交时后端仍会自动闭环) */
export async function startEngineeringTask(id: string): Promise<void> {
  await api(`/api/engineering/tasks/${id}`, {
    method: "PATCH",
    body: JSON.stringify({ status: "进行中" }),
  }).catch(() => void 0);
}

/** 批量删除未成单的离线照片。已并入巡检记录的不会被删(那是记录的证据) */
export async function deleteOfflineShots(shotIds: string[]): Promise<number> {
  const body = await api<{ deleted: number }>("/api/inspection/offline-shots/delete", {
    method: "POST",
    body: JSON.stringify({ shotIds }),
  });
  return body.deleted;
}
