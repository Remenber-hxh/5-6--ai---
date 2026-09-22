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
  /** 这张拍的是哪台设备(扫码时带上)。空 = 手动路径拍的 */
  assetId?: string;
  /** 这台设备归哪个模板(后端按 assetId 查出来的)。空 = 不知道 */
  assetTemplateId?: string;
  /**
   * 这个模板一条记录本来就覆盖多台设备(抄表这种:六张照片六台表)。
   * 空/false = 不知道或不是 —— 这时按老规矩,多台设备就拦。
   */
  multiDevice?: boolean;
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
  /**
   * 这个读数是哪台设备的(抄表类字段才有)。
   *
   * 一条抄表记录抄四块电表加两块水表,而照片上没有 Z1~Z4 任何标识 ——
   * AI 只能按上传顺序猜,中间夹一张读不出的就整体错位一格,而且不报错。
   * 所以这一行要能点开改。assetOptions 由后端按台账现算,不落库。
   */
  assetName?: string;
  assetOptions?: string[];
  /**
   * 这个读数是从哪张照片、哪一块读出来的。
   * bbox 是归一化的 [左, 上, 右, 下],确认页据此把那一小块裁出来摆在行旁边。
   *
   * 【为什么要摆出来】现场是按顺序拍的,AI 也按顺序配,中间夹一张读不出的
   * 就整体错位一格。而确认页上只有一个光秃秃的数字,人没有参照物,
   * 只能凭记忆去对六张照片 —— 所以实际发生的是不对,直接确认。
   */
  sourceImageId?: string;
  bbox?: number[];
}

export interface ImageInfo {
  id: string;
  fileName: string;
  path: string;
}

export interface Recommendation {
  priority: string; // high / medium / low
  category?: string;
  text: string;
  basis?: string;
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
  aiRecommendations?: Recommendation[];
  aiSummaryError?: string;
  submitted: boolean;
}

export interface TemplateDTO {
  id: string;
  name: string;
  project: string;
  assetType: string;
  /** 每单最少几张照片。0 = 不限。后端提交时也会复核 */
  minImages?: number;
  /** 单次上传上限 */
  maxImages?: number;
}

/** 服务器上尚未成单的离线照片 */
export async function listOfflineShots(
  signal?: AbortSignal,
): Promise<OfflineShotDTO[]> {
  // pending=1 让服务端只发未成单的。原来是全量发下来、客户端 filter 掉,
  // 那部分字节纯属白传(实测这个接口 82KB)。
  const body = await api<{ shots: OfflineShotDTO[] | null }>(
    "/api/inspection/offline-shots?limit=200&pending=1",
    { signal },
  );
  return (body.shots || []).filter((s) => !s.recordId);
}

/** 用服务器上已有的照片做场景识别(不重传) */
export async function classifyOfflineShots(shotIds: string[]) {
  return api<{ classify: ClassifyResult; shots: OfflineShotDTO[] }>(
    "/api/inspection/offline-shots/classify",
    { method: "POST", body: JSON.stringify({ shotIds }) },
  );
}

export async function listTemplates(
  signal?: AbortSignal,
): Promise<TemplateDTO[]> {
  const body = await api<{ templates: TemplateDTO[] | null }>(
    "/api/report/templates",
    { signal },
  );
  return body.templates || [];
}

/** 成单:按 ID 认领离线照片(照片已在服务器上) */
export async function createRecordFromShots(
  templateId: string,
  shotIds: string[],
  /** 复检时带上目标点位,保证新记录和原设备落在同一个点位 */
  pointId?: string,
) {
  return api<RecordDTO>("/api/inspection/records", {
    method: "POST",
    body: JSON.stringify({
      templateId,
      offlineShotIds: shotIds,
      ...(pointId ? { pointId } : {}),
    }),
  });
}

export async function getRecord(
  id: string,
  signal?: AbortSignal,
): Promise<RecordDTO> {
  return api<RecordDTO>(`/api/inspection/records/${id}`, { signal });
}

/** 触发 AI 字段识别(异步任务) */
export async function startAnalysis(id: string) {
  return api<{ taskId?: string }>(`/api/inspection/records/${id}/ai-tasks`, {
    method: "POST",
  });
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
  return api<FieldValue>(
    `/api/inspection/records/${recordId}/fields/${encodeURIComponent(code)}`,
    {
      method: "PATCH",
      body: JSON.stringify({ value, version, ...opts }),
    },
  );
}

/**
 * 只改「这个读数属于哪台设备」,不动读数本身。
 *
 * 【必须不带 value】后端的 value 是指针:不传 = 这次没提交读数。
 * 传个空字符串的话会走进"改值"分支,把读数清空 —— 而请求照样返回 200。
 */
/**
 * 把一个读数整体挪到另一台设备名下(读数 + 照片 + 置信度 + AI 原值一起搬)。
 *
 * 【为什么是一个接口而不是两次 PATCH】搬家要么整个成、要么整个不成。
 * 拆成"清掉这边"+"写到那边"两次请求,中间断一次就变成读数两边都有、
 * 或者两边都没有 —— 而两次请求各自都返回 200,现场看不出发生过什么。
 *
 * 返回整条记录:搬家动了两格,只回一格的话另一格在界面上还是旧的。
 */
export async function moveReading(
  recordId: string,
  fromCode: string,
  toCode: string,
): Promise<RecordDTO> {
  return api<RecordDTO>(`/api/inspection/records/${recordId}/fields/move`, {
    method: "POST",
    body: JSON.stringify({ fromCode, toCode }),
  });
}

/**
 * 两格的读数、照片整组对调。
 *
 * 【为什么不能用两次 move 凑】move 的规矩是"目标格有读数就拒绝"。
 * 两格都有数时它一步都走不了,得先寄存再搬两次 —— 中间断一次就停在
 * 说不清的中间态上。对调必须是一次请求。
 */
export async function swapReadings(
  recordId: string,
  aCode: string,
  bCode: string,
): Promise<RecordDTO> {
  return api<RecordDTO>(`/api/inspection/records/${recordId}/fields/swap`, {
    method: "POST",
    body: JSON.stringify({ aCode, bCode }),
  });
}

/**
 * 认领一张还没人要的照片:告诉系统"这一格的读数来自这张图"。
 *
 * 【为什么要回整条记录,不只回这一格】一张照片只能归一格,后端在挂上去的
 * 同时会把别的格子对它的引用清掉。只拿这一格的话,那一格在界面上还摆着
 * 同一张照片 —— 看上去像两台表抄出了两个数。
 */
export async function patchFieldSource(
  recordId: string,
  code: string,
  sourceImageId: string,
  version: number,
): Promise<RecordDTO> {
  await api<FieldValue>(
    `/api/inspection/records/${recordId}/fields/${encodeURIComponent(code)}`,
    { method: "PATCH", body: JSON.stringify({ sourceImageId, version }) },
  );
  return getRecord(recordId);
}

export async function patchFieldAsset(
  recordId: string,
  code: string,
  assetName: string,
  version: number,
): Promise<FieldValue> {
  return api<FieldValue>(
    `/api/inspection/records/${recordId}/fields/${encodeURIComponent(code)}`,
    {
      method: "PATCH",
      body: JSON.stringify({ assetName, version }),
    },
  );
}

/**
 * 一次性把"这一格是哪台表"和"读数来自哪张照片"都写上。
 *
 * 【为什么要合成一次】分两次调的话,第一次改完版本号就变了,第二次拿着旧版本号
 * 会被当成冲突打回 —— 表现是"选了设备,照片没挂上",而第一步已经生效了一半。
 */
export async function patchFieldAssetSource(
  recordId: string,
  code: string,
  assetName: string,
  sourceImageId: string,
  version: number,
): Promise<RecordDTO> {
  await api<FieldValue>(
    `/api/inspection/records/${recordId}/fields/${encodeURIComponent(code)}`,
    { method: "PATCH", body: JSON.stringify({ assetName, sourceImageId, version }) },
  );
  return getRecord(recordId);
}

/** 转人工填写(AI 多次识别不稳时) */
export async function enableManual(id: string) {
  return api<RecordDTO>(`/api/inspection/records/${id}/manual`, {
    method: "POST",
  });
}

/** 提交。幂等键防弱网重复提交产生重复记录 */
export async function submitRecord(id: string): Promise<RecordDTO> {
  return api<RecordDTO>(`/api/inspection/records/${id}/submit`, {
    method: "POST",
    headers: { "Idempotency-Key": `sub_${id}_${Date.now().toString(36)}` },
  });
}

/**
 * 删除未提交的记录(草稿)。
 * 后端只接受未提交的:已提交的进了台账,要撤销走数据修改申请。
 */
export async function deleteDraftRecord(id: string): Promise<void> {
  await api<void>(`/api/inspection/records/${id}`, { method: "DELETE" });
}

/** 没提交完的记录 */
export interface DraftBrief {
  id: string;
  templateName: string;
  project: string;
  pointName: string;
  assetNo: string;
  createdAt: string;
  imageCount: number;
  fieldsFilled: number;
  fieldsTotal: number;
}

/**
 * 自己还没提交的记录。
 *
 * 【为什么不复用 listRecords 再前端筛】已提交的远多于草稿,先取最近 N 条
 * 再挑未提交的,人多干几天活草稿就掉出窗口 —— 表现是"我明明有一条没交完的,
 * 就是不显示",而且越忙越容易发生。后端这条路径在 SQL 里筛。
 */
export async function listDrafts(signal?: AbortSignal): Promise<DraftBrief[]> {
  const body = await api<{ drafts: DraftBrief[] }>("/api/inspection/drafts", { signal });
  return body.drafts || [];
}

// ===== 工程任务(我的任务) =====

export interface EngineeringTaskDTO {
  id: string;
  title?: string;
  workContent?: string;
  assetName?: string;
  project?: string;
  category?: string;
  /** 负责人。后端返回全量任务、不按人过滤,由客户端筛自己的(沿用旧版做法) */
  assigneeName?: string;
  status: string; // 待执行 / 待整改 / 进行中 / 逾期 / 已完成 / 已取消
  dueAt?: string;
  templateId?: string;
}

/**
 * 底栏角标的两个数字。
 *
 * 原来是把 offline-shots 和 engineering/tasks 两个完整列表拉下来取 length ——
 * 一次 120KB,而底栏每切一次标签就重来一遍(实测切 5 次 2MB)。
 * 后端现在直接给数,几十字节。口径由后端保证与两个列表页一致。
 */
export async function getBadgeCounts(
  signal?: AbortSignal,
): Promise<{ shots: number; tasks: number }> {
  const body = await api<{ shots?: number; tasks?: number }>(
    "/api/inspection/badge-counts",
    { signal },
  );
  return { shots: body.shots || 0, tasks: body.tasks || 0 };
}

// ===== 今日待巡 =====
//
// 「这台设备今天还没有巡检快照」—— 是算出来的状态,不是一条记录。
// 所以它和工程任务不是一回事:没有 ID、没有状态流转,拍照提交就自动销账。

export interface TodayAssetDTO {
  assetId: string;
  assetName: string;
  project?: string;
  done?: boolean;
  doneAt?: string;
  /** 台账里已经查不到了(计划录入后被删)。点不了,但要显示出来 */
  missing?: boolean;
  /** 下面三样用于"点一下直接去巡这台",和扫码锁定设备是同一套上下文 */
  templateId?: string;
  assetKey?: string;
  pointId?: string;
}

export interface TodayBoardDTO {
  date: string;
  weekday: number;
  total: number;
  done: number;
  plans: {
    planId: string;
    title?: string;
    project?: string;
    ownerName?: string;
    assets: TodayAssetDTO[] | null;
    noAssets?: boolean;
  }[] | null;
}

export async function getTodayBoard(signal?: AbortSignal): Promise<TodayBoardDTO> {
  return api<TodayBoardDTO>("/api/inspection/today", { signal });
}

export async function listEngineeringTasks(
  signal?: AbortSignal,
): Promise<EngineeringTaskDTO[]> {
  // scope=open-mine:服务端按"在办 + 该给我看的"筛好再发。
  // 原来是全量发下来、页面在客户端再筛一遍 —— 于是角标(全租户 5 条)和
  // 页面(派给我的 2 条)对不上。口径只留后端一份。
  const body = await api<{ tasks: EngineeringTaskDTO[] | null }>(
    "/api/engineering/tasks?scope=open-mine",
    { signal },
  );
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
  const body = await api<{ deleted: number }>(
    "/api/inspection/offline-shots/delete",
    {
      method: "POST",
      body: JSON.stringify({ shotIds }),
    },
  );
  return body.deleted;
}

// ===== 资产台账(设备健康) =====

export interface AssetDTO {
  id: string;
  assetName: string;
  /** 复检要用:跳过 AI 分类、强制落到同一个模板 */
  templateId?: string;
  pointId?: string;
  /** 后端认资产身份的那个键,复检时写进表单的 asset_no */
  assetKey?: string;
  assetType?: string;
  project?: string;
  projectCode?: string;
  lastStatus: string; // 正常 / 异常 / 待复核 / 待维修 / 未巡检
  statusLevel?: string; // normal / warning / danger / repair / unknown
  /**
   * 为什么是这个状态 —— 一句人话。只在需要跟进的状态下有值。
   * 后端按"那条记录 + 那一格"现算,不入库(见 asset_status_reason.go)。
   */
  statusReason?: string;
  lastSummary?: string;
  lastInspectedAt?: string;
  lastInspector?: string;
  inspectionCount: number;
  coverImage?: { path?: string } | null;
}

export interface AssetGroup {
  value: string;
  count: number;
}

export interface AssetSummary {
  total?: number;
  normal?: number;
  warning?: number;
  danger?: number;
  repair?: number;
  unknown?: number;
  projects?: AssetGroup[];
  assetTypes?: AssetGroup[];
}

export async function listAssets(
  signal?: AbortSignal,
): Promise<{ assets: AssetDTO[]; summary: AssetSummary | null }> {
  const body = await api<{
    assets: AssetDTO[] | null;
    summary?: AssetSummary;
    totalSummary?: AssetSummary;
  }>("/api/assets", { signal });
  return {
    assets: body.assets || [],
    summary: body.summary || body.totalSummary || null,
  };
}

/** 单台资产详情 */
export async function getAsset(
  id: string,
  signal?: AbortSignal,
): Promise<AssetDTO> {
  const body = await api<{ asset: AssetDTO }>(
    `/api/assets/${encodeURIComponent(id)}`,
    { signal },
  );
  return body.asset;
}

export interface AssetSnapshotDTO {
  id: string;
  recordId: string;
  status: string;
  summary?: string;
  inspector?: string;
  createdAt: string;
}

/**
 * 按资产翻完整巡检历史(查快照表,不受记录列表窗口限制)。
 *
 * 【字段名对不上导致历史一直是空的】
 * 后端这个接口返回的数组叫 `records`(handleAssetRecords),这里原来读的是
 * `snapshots` —— 永远拿到 undefined,于是设备详情页对一台巡检过 24 次的电梯
 * 也显示"暂无历史记录"。
 *
 * 这类错不会报错也不会白屏:字段取不到就是 undefined,`|| []` 一兜底,页面
 * 看起来"正常地空着"。数组里每一项的结构其实是完全对得上的,只有外层这一
 * 个键名不同。
 */
export async function listAssetRecords(
  id: string,
  page = 1,
  signal?: AbortSignal,
): Promise<{ snapshots: AssetSnapshotDTO[]; totalPages: number }> {
  const body = await api<{
    records: AssetSnapshotDTO[] | null;
    totalPages?: number;
  }>(`/api/assets/${encodeURIComponent(id)}/records?page=${page}&pageSize=20`, {
    signal,
  });
  return { snapshots: body.records || [], totalPages: body.totalPages || 1 };
}

// ===== 数据修改审批 =====

export interface ChangeRequestDTO {
  id: string;
  targetType: string; // asset / record
  targetId: string;
  targetLabel?: string;
  patch?: Record<string, unknown>;
  reason?: string;
  status: string;
  requestedBy?: string;
  requestedAt?: string;
}

/**
 * 某台设备相关的修改申请(直接改这台的 + 改它任意一次巡检记录的)。
 *
 * 【走后端算,不在前端筛】旧版是把全部申请拉下来自己筛,有两个静默失效:
 * 列表有 200 条上限,老申请会被截掉;而"这条记录属不属于这台设备"要靠前端
 * 已加载的那一页历史判断,没翻到的页一律匹配不上。两个都只是"少显示",
 * 页面看起来一切正常。
 *
 * 权限在后端:一线人员只拿到自己发起的,管理角色拿到这台设备的全部。
 */
export async function listAssetChangeRequests(
  assetId: string,
  signal?: AbortSignal,
): Promise<ChangeRequestDTO[]> {
  const body = await api<{ requests: ChangeRequestDTO[] | null }>(
    `/api/assets/${encodeURIComponent(assetId)}/change-requests`,
    { signal },
  );
  return body.requests || [];
}

export async function listPendingChangeRequests(
  signal?: AbortSignal,
): Promise<ChangeRequestDTO[]> {
  const body = await api<{ requests: ChangeRequestDTO[] | null }>(
    "/api/change-requests?status=pending",
    {
      signal,
    },
  );
  return body.requests || [];
}

export async function approveChangeRequest(id: string, reviewNote: string) {
  return api(`/api/change-requests/${id}/approve`, {
    method: "POST",
    body: JSON.stringify({ reviewNote }),
  });
}

export async function rejectChangeRequest(id: string, reviewNote: string) {
  return api(`/api/change-requests/${id}/reject`, {
    method: "POST",
    body: JSON.stringify({ reviewNote }),
  });
}

/**
 * 上传自己的头像。
 *
 * 不走 api():那个 helper 强制设 Content-Type: application/json,
 * 而 multipart 的 boundary 必须由浏览器自己生成 —— 手动设 Content-Type
 * 会让后端 ParseMultipartForm 直接失败。
 *
 * 返回 storage 根下的相对路径,用 avatarURL() 转成可访问地址。
 */
export async function uploadMyAvatar(blob: Blob): Promise<string> {
  const fd = new FormData();
  fd.append("file", new File([blob], "avatar.jpg", { type: "image/jpeg" }));
  // client 现在会识别 FormData 并跳过 JSON Content-Type,不用再绕开它裸写 fetch
  const body = await api<{ avatar?: string }>("/api/auth/me/avatar", {
    method: "POST",
    body: fd,
  });
  return String(body.avatar || "");
}

/**
 * 发起数据修改申请。
 *
 * 【为什么目标可以是记录也可以是资产】
 * 申请修改的本质是纠正"这次巡检填错/AI 认错的字段",所以默认目标是【最近
 * 一次巡检记录】而不是资产台账 —— 改记录会连带把台账重算,改台账只是主管
 * 直接改状态、不留巡检依据。旧版同一口径(app.js:1935)。
 *
 * patch.fields 只放【改动过】的字段:全量提交的话审批的人看不出来动了什么,
 * 得自己逐项比对。
 */
export async function createChangeRequest(body: {
  targetType: "record" | "asset";
  targetId: string;
  patch: Record<string, unknown>;
  reason: string;
}) {
  return api("/api/change-requests", {
    method: "POST",
    body: JSON.stringify(body),
  });
}

/**
 * 补交照片:先传到临时目录拿 tmpDir + imageIds,再随 patch.addImages 一起提交。
 * 审批通过后后端才把它们并入那次巡检 —— 没通过的照片不会污染巡检证据。
 */
export async function uploadDraftPhotos(
  files: File[],
): Promise<{ tmpDir: string; imageIds: string[] }> {
  const fd = new FormData();
  for (const f of files) fd.append("files", f);
  const res = await fetch("/api/change-requests/draft-photos", {
    method: "POST",
    headers: { "X-InspectAI-Token": getToken() },
    body: fd,
  });
  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) throw new Error(body.message || "照片上传失败");
  return {
    tmpDir: String(body.tmpDir || ""),
    imageIds: ((body.files as { id: string }[]) || []).map((f) => f.id),
  };
}
