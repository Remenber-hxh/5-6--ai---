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

export interface AssetImageInfo {
  id?: string;
  fileName?: string;
  path?: string;
  url?: string;
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
  lastSummary?: string;
  lastInspectedAt?: string;
  lastRecordId?: string;
  lastPhotoPath?: string;
  coverImagePath?: string;
  coverImage?: AssetImageInfo;
}

export function uploadAssetCover(assetId: string, file: File) {
  const body = new FormData();
  body.append("file", file);
  return api<{ asset: AssetEntry }>(`/api/assets/${encodeURIComponent(assetId)}/cover`, {
    method: "POST",
    body,
  }).then((d) => d.asset);
}

/**
 * 修改申请。
 *
 * 【字段名以 Go 的 ChangeRequest 为准】这个接口以前写的是 type / createdAt /
 * requesterName / assetName —— 后端返回的却是 targetType / requestedAt /
 * requestedBy / (没有名字,只有 targetId)。四个全对不上,于是审批列表的
 * 时间、类型、设备、申请人【整整四列都是空的】,而理由和状态碰巧对上了,
 * 所以页面看起来"能用",只是查不出是谁在改哪台设备。
 * TypeScript 也帮不上忙:这些字段全是可选的,拼错只会得到 undefined。
 */
export interface ChangeRequest {
  id: string;
  /** "asset" / "record" */
  targetType?: string;
  targetId?: string;
  /** 后端动态填充的可读名称(设备名 / 设备编号),不入库 */
  targetName?: string;
  /** 待应用的补丁。record 是 { fields: [...] },asset 是 { assetName?, lastStatus?, lastSummary? } */
  patch?: Record<string, unknown>;
  status?: string; // pending / approved / rejected / withdrawn
  reason?: string;
  requestedBy?: string;
  requestedAt?: string;
  reviewedBy?: string;
  reviewedAt?: string;
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

export function dailyReport() {
  return api<any>("/api/management-ai/report?type=daily");
}

export interface ConfirmLog {
  action?: string; // confirm / correct / uncertain
  fieldLabel?: string;
  fieldKey?: string;
  aiValue?: string;
  finalValue?: string;
  aiConfidence?: number;
  viewedPhoto?: boolean;
  durationMs?: number;
  operator?: string;
  createdAt?: string;
}

export function listConfirmLogs(recordId: string) {
  return api<{ logs: ConfirmLog[] }>(
    `/api/inspection/records/${encodeURIComponent(recordId)}/confirm-logs`,
  ).then((d) => d.logs || []);
}

export function listAssets() {
  return api<{ assets: AssetEntry[] }>("/api/assets").then((d) => d.assets || []);
}

/** 按 id 取单条巡检记录。溯源卡只要一条,不该为它拉整份列表。 */
export function getRecord(id: string) {
  return api<InspectionRecord>(
    `/api/inspection/records/${encodeURIComponent(id)}`,
  );
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

// ===== 巡检计划 / 工程任务 =====
export interface EngineeringPlan {
  id: string;
  workContent?: string;
  project?: string;
  category?: string;
  ownerName?: string;
  /** 负责人的账号 ID。空 = 没绑账号(外委人员),此时只有 ownerName 有意义。 */
  ownerId?: string;
  planStart?: string;
  planEnd?: string;
  status?: string; // 待执行 / 执行中 / 待整改 / 已完成
  cycleText?: string;
  scopeDesc?: string;
  budgetAmount?: number;
  latestTaskId?: string;
  source?: string;
  // 【下面这些以前没写进类型里,但接口一直在返回】保存是整行覆盖的,
  // 编辑时没带上的字段会被清空。类型里没有 → 编辑表单拿不到 → 存回去就丢了。
  // 表现是"我只改了个负责人,预算和备注就没了",而且不报错。
  remark?: string;
  sequenceNo?: string;
  businessType?: string;
  subType?: string;
  budgetText?: string;
  riskLevel?: string;
  /** yearly / monthly / weekly / daily / adhoc(临时,对外部项目组) */
  planType?: string;
  /** 每日计划专用:一周哪几天执行,"1,2,3,4,5"(1=周一 … 7=周日)。空=每天 */
  weekdays?: string;
  /** 每日计划要巡的设备。完成情况按它自动判定,所以每日计划必填 */
  assetIds?: string[];
}

export interface EngineeringTask {
  id: string;
  planItemId?: string;
  title?: string;
  project?: string;
  assigneeName?: string;
  dueAt?: string;
  status?: string; // 待执行 / 进行中 / 待整改 / 已完成 / 逾期
  taskType?: string;
  assetId?: string;
  completedAt?: string;
  createdAt?: string;
}

export function listPlans() {
  return api<{ plans: EngineeringPlan[] }>("/api/engineering/plans").then((d) => d.plans || []);
}

// 新建/编辑计划(旧版同为 POST upsert;字段口径一致)
// 保存计划。后端是整行覆盖(upsert 把每一列都写成传来的值),
// 所以【编辑时必须把这条计划原有的字段一起传回去】—— 少传一个就等于清空它。
export function savePlan(p: Partial<EngineeringPlan> & { workContent: string }) {
  return api("/api/engineering/plans", {
    method: "POST",
    // 【status / source 只是新建时的兜底】写在展开前面,p 里带了就以 p 为准。
    // 反过来写死的话,改一次负责人就会把「已完成」打回「待执行」,
    // 于是「派发执行任务」按钮又冒出来,同一条计划能派发第二次。
    body: JSON.stringify({ status: "待执行", source: "manual", ...p }),
  });
}

// ===== 每日未巡提醒 =====

export interface DailyPushDigest {
  date: string;
  weekday: number;
  total: number;
  done: number;
  pending: number;
  /** 计划里点了、但台账里已经查不到的设备数 */
  missing: number;
  groups: { project: string; pending: number; lines: { ownerName: string; assets: string[] }[] }[];
  /** 逐字的原文 —— 预览要给到"会发出去的那些字",不是"大概长这样" */
  text: string;
  wouldSend: boolean;
  skipReason?: string;
}

/** 只算不发。定时和真发是下一步 —— 先让文案在页面上跑准。 */
export function previewDailyPush(silentWhenDone: boolean) {
  return api<DailyPushDigest>(
    `/api/engineering/plans/daily-push/preview?silentWhenDone=${silentWhenDone ? 1 : 0}`,
  );
}

// ===== 负责人绑定 =====

export interface OwnerBindingCandidate {
  userId: string;
  displayName: string;
  username: string;
  departmentName?: string;
  roleName?: string;
}

export interface OwnerBindingGroup {
  ownerName: string;
  planIds: string[];
  planCount: number;
  candidates?: OwnerBindingCandidate[];
  /** 名字对上了,但这个人看不到那几条计划的项目 —— 绑了会被拒 */
  blockedPlanIds?: string[];
  blockedNote?: string;
}

export interface OwnerBindingReport {
  matched: OwnerBindingGroup[];
  ambiguous: OwnerBindingGroup[];
  unmatched: OwnerBindingGroup[];
  alreadyBound: number;
  noOwner: number;
  totalPlans: number;
}

/** 只读:算出名字和账号的对应关系,不改任何数据 */
export function getOwnerBindingReport() {
  return api<OwnerBindingReport>("/api/engineering/plans/owner-binding");
}

/** 写入:只接受显式的 planId → userId 清单;后端先全量校验再动手 */
export function applyOwnerBindings(bindings: { planId: string; userId: string }[]) {
  return api<{ applied: number }>("/api/engineering/plans/owner-binding", {
    method: "POST",
    body: JSON.stringify({ bindings }),
  });
}

/**
 * 删除计划 / 任务。
 *
 * 【删除不是取消】取消是"这活不做了",记录还在、谁什么时候取消的查得到;
 * 删除是这条从来没存在过。所以只给"建错了"用 —— 后端会拦住已经有
 * 巡检记录或已派发任务的那些。
 */
export function deletePlan(id: string) {
  return api(`/api/engineering/plans/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function deleteTask(id: string) {
  return api(`/api/engineering/tasks/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// 资产标记正常(恢复正常三路径之一,自动销账)
export function markAssetNormal(a: AssetEntry) {
  return api(`/api/assets/${encodeURIComponent(a.id)}`, {
    method: "PATCH",
    body: JSON.stringify({
      assetName: a.assetName,
      lastStatus: "正常",
      lastSummary: "后台复核后标记正常。",
    }),
  });
}

export function listTasks() {
  return api<{ tasks: EngineeringTask[] }>("/api/engineering/tasks").then((d) => d.tasks || []);
}

export function setTaskStatus(id: string, status: string) {
  return api(`/api/engineering/tasks/${encodeURIComponent(id)}/status`, {
    method: "POST",
    body: JSON.stringify({ status }),
  });
}

// 派发执行:创建(或复用)计划的执行任务并置「进行中」——两步契约与旧版一致
export async function dispatchPlan(plan: EngineeringPlan) {
  const res = await api<{ task?: { id?: string } }>("/api/engineering/tasks", {
    method: "POST",
    body: JSON.stringify({
      planItemId: plan.id,
      title: `${plan.workContent || "工程计划"} 执行任务`,
      assigneeName: plan.ownerName || "",
      dueAt: plan.planEnd || "",
      taskType: "巡检计划执行",
      status: "进行中",
      source: "manual",
    }),
  });
  const taskId = res?.task?.id;
  // 创建任务不会重算计划状态,必须再调一次状态更新
  if (taskId) await setTaskStatus(taskId, "进行中");
  return taskId;
}

// 主管直接修改台账字段(其余角色走修改申请审批流)
export function updateAsset(id: string, patch: { assetName?: string; lastStatus?: string; lastSummary?: string }) {
  return api<{ asset: AssetEntry }>(`/api/assets/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(patch),
  });
}

// 删除资产(主管;巡检记录保留作历史证据,有在途整改任务会被后端拦下)
export function deleteAsset(id: string) {
  return api<{ deleted: boolean }>(`/api/assets/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// 手工新增资产建档(主管;设备先入台账、未巡检,巡检数从 0 起)
export function createAsset(payload: {
  project: string;
  assetKey: string;
  assetName: string;
  assetType?: string;
  templateId?: string;
  summary?: string;
}) {
  return api<{ asset: AssetEntry }>("/api/assets", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then((d) => d.asset);
}

// ===== 用户 / 日志 / 系统 =====
export interface UserEntry {
  id: string;
  username?: string;
  displayName?: string;
  roleCode?: string;
  roleName?: string;
  departmentId?: string;
  departmentName?: string;
  status?: string;
  createdAt?: string;
  /** 数据范围。空 = 按角色推导(默认)。与角色正交:角色管"能做什么",这里管"能看多少"。 */
  dataScope?: string;
}

export interface Department {
  id: string;
  name: string;
  parentId?: string;
}

export function listDepartments() {
  return api<{ departments: Department[] }>("/api/departments").then((d) => d.departments || []);
}

// ===== 角色×能力 权限矩阵(仅 admin) =====
export interface PermDef {
  key: string;
  label: string;
  desc: string;
  locked: boolean;
}

export function getPermissions() {
  return api<{ catalog: PermDef[]; matrix: Record<string, string[]> }>("/api/permissions");
}

export function savePermissions(matrix: Record<string, string[]>) {
  return api<{ matrix: Record<string, string[]> }>("/api/permissions", {
    method: "PUT",
    body: JSON.stringify({ matrix }),
  });
}

/** 一个人能看到哪些项目。seesAll 为真时 projects 是空的 —— 别当成"一个都看不到"。 */
export interface ProjectScopeDTO {
  seesAll: boolean;
  projects?: string[];
  blocked?: boolean;
}

export function listUsers() {
  return api<{ users: UserEntry[] }>("/api/users").then((d) => d.users || []);
}

/**
 * 用户 + 每人的项目可见范围。建计划挑负责人时要用后者过滤。
 *
 * 【范围由后端算好,前端不要拿 dataScope 自己推】那等于把规则复制一份到前端,
 * 两边迟早说不一样的话 —— 而不一致的表现是"这个人明明有权限却选不到",
 * 或者更糟:选得到、派下去了、他打开什么都没有。
 */
export function listUsersWithScope() {
  return api<{ users: UserEntry[]; projectScopes?: Record<string, ProjectScopeDTO> }>(
    "/api/users",
  ).then((d) => ({ users: d.users || [], scopes: d.projectScopes || {} }));
}

export interface RoleEntry {
  id: string;
  code: string;
  name: string;
  description?: string;
  builtin?: boolean; // 内置角色:可改名不可删;admin 全锁
}

export function listRoles() {
  return api<{ roles: RoleEntry[] }>("/api/roles").then((d) => d.roles || []);
}

export function createRole(payload: { name: string; description?: string }) {
  return api<{ role: RoleEntry }>("/api/roles", { method: "POST", body: JSON.stringify(payload) });
}

export function updateRole(id: string, payload: { name: string; description?: string }) {
  return api<{ role: RoleEntry }>(`/api/roles/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify(payload),
  });
}

export function deleteRole(id: string) {
  return api<{ deleted: boolean }>(`/api/roles/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function createUser(payload: { username: string; displayName: string; roleCode: string; password: string; departmentId?: string; dataScope?: string }) {
  return api<{ user: UserEntry }>("/api/users", { method: "POST", body: JSON.stringify(payload) });
}

export function updateUser(id: string, payload: { username?: string; displayName: string; roleCode: string; departmentId?: string; dataScope?: string }) {
  return api(`/api/users/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(payload) });
}

export function resetUserPassword(id: string, password: string) {
  return api(`/api/users/${encodeURIComponent(id)}/password`, {
    method: "POST",
    body: JSON.stringify({ password }),
  });
}

export function setUserStatus(id: string, status: "active" | "disabled") {
  return api(`/api/users/${encodeURIComponent(id)}/status`, {
    method: "POST",
    body: JSON.stringify({ status }),
  });
}

export interface OperationLog {
  id?: string;
  actorName?: string;
  action?: string;
  targetType?: string;
  targetId?: string;
  createdAt?: string;
  detail?: Record<string, unknown>;
}

export function listOperationLogs() {
  return api<{ logs: OperationLog[] }>("/api/operation-logs?limit=100").then((d) => d.logs || []);
}

export function getHealth() {
  return api<Record<string, unknown>>("/health");
}

// ===== 提示词模板 =====
export interface PromptField {
  code: string;
  label: string;
  group?: string;
  mode?: string;
  yesWhen?: string;
  noWhen?: string;
  skipWhen?: string;
  note?: string;
}

export interface PromptTemplate {
  id: string;
  name?: string;
  scene?: string;
  expectedPhotos?: string;
  fields: PromptField[];
}

export function listPromptTemplates() {
  return api<{ templates: PromptTemplate[]; modes: { value: string; label: string }[] }>(
    "/api/prompt/templates",
  );
}

export function getPromptTemplate(id: string) {
  return api<PromptTemplate>(`/api/prompt/templates/${encodeURIComponent(id)}`);
}

export function savePromptTemplate(t: PromptTemplate) {
  return api(`/api/prompt/templates/${encodeURIComponent(t.id)}`, {
    method: "PUT",
    body: JSON.stringify(t),
  });
}

export function renderPromptTemplate(id: string) {
  return api<{ prompt: string }>(`/api/prompt/templates/${encodeURIComponent(id)}/render`).then(
    (d) => d.prompt || "",
  );
}

// ===== 重点关注(数据看板) =====
export function listAttention(limit = 8) {
  // 后端两条路径键名不同:实时计算返回 items,缓存回退返回 attention;summary 为 AI 洞察综述
  return api<{ items?: AttentionItem[]; attention?: AttentionItem[]; summary?: string }>(
    `/api/management-ai/attention?limit=${limit}`,
  ).then((d) => ({ items: d.items || d.attention || [], summary: d.summary || "" }));
}

export interface RepeatedIssue {
  assetId?: string;
  assetName?: string;
  fieldKey?: string;
  fieldLabel?: string;
  count?: number;
  issue?: string;
  lastTime?: string;
}

export interface InspectorQualityRow {
  operator?: string;
  total?: number;
  noPhotoConfirm?: number;
  corrections?: number;
  avgDurationMs?: number;
}

export interface RiskFactor {
  label: string;
  score: number;
  max: number;
  basis?: string;
}

export interface AttentionItem {
  assetId: string;
  assetName?: string;
  riskScore?: number;
  riskLevel?: string;
  title?: string;
  reasons?: string[];
  breakdown?: RiskFactor[];
  action?: string;
  lastRecordId?: string;
  lastPhotoPath?: string;
  coverImagePath?: string;
  coverImage?: AssetImageInfo;
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

// ===== 注册码 =====
//
// 巡检员自助注册的门槛。码上带角色和租户,注册出来的账号直接落到对的位置。
// 【管理员角色的码后端会拒签】—— 一张能自助注册出管理员的码流出去,
// 整个租户的数据就没门槛了。

export interface RegistrationCodeEntry {
  id: string;
  code: string;
  roleCode: string;
  departmentId?: string;
  note?: string;
  /** 0 = 不限次数 */
  maxUses: number;
  usedCount: number;
  /** 空 = 不过期 */
  expiresAt?: string;
  disabled: boolean;
  createdBy?: string;
  createdAt: string;
}

export function listRegistrationCodes() {
  return api<{ codes: RegistrationCodeEntry[] }>("/api/registration-codes").then(
    (d) => d.codes || [],
  );
}

export function createRegistrationCode(payload: {
  roleCode: string;
  maxUses: number;
  expiresInDays: number;
  note?: string;
  departmentId?: string;
}) {
  return api<{ code: RegistrationCodeEntry }>("/api/registration-codes", {
    method: "POST",
    body: JSON.stringify(payload),
  }).then((d) => d.code);
}

export function setRegistrationCodeDisabled(id: string, disabled: boolean) {
  return api<{ ok: boolean; disabled: boolean }>(
    `/api/registration-codes/${encodeURIComponent(id)}/disable`,
    { method: "POST", body: JSON.stringify({ disabled }) },
  );
}

/**
 * 某台设备的巡检轨迹。
 *
 * 【必须走这个接口,不能在前端筛全量记录】记录上的 pointId 是【巡检点位/模板】
 * (比如"无机房电梯"),整栋楼的无机房电梯共用同一个 pointId —— 按它筛出来的
 * 是"所有同类设备的记录",不是这一台的。后台的巡检轨迹一度就是这么串台的:
 * KT-3 的轨迹里列着 FT-11、FT-12 的巡检时间。
 *
 * 资产快照(asset_snapshots)才是按 assetId 记的,这个接口查的就是它。
 */
export interface AssetSnapshotEntry {
  id: string;
  assetId: string;
  /** 对应的巡检记录 ID —— 点进去看详情要用这个,不是快照自己的 id */
  recordId: string;
  status: string;
  statusLevel?: string;
  summary?: string;
  inspector?: string;
  createdAt: string;
}

export function listAssetSnapshots(assetId: string, pageSize = 5) {
  return api<{ records: AssetSnapshotEntry[]; total: number }>(
    `/api/assets/${encodeURIComponent(assetId)}/records?page=1&pageSize=${pageSize}`,
  ).then((d) => ({ records: d.records || [], total: d.total || 0 }));
}

/**
 * 后台首页「今日快照」的四个数字。
 *
 * 【为什么单开一个】原来首页是把资产/记录/任务/审批四份【全量列表】下载下来,
 * 在浏览器里 filter().length 出四个整数 —— 其中记录那份实测 654 KB。
 * 而这是登录后第一个加载的页面。
 *
 * 而且原来那四个请求的错误全被 .catch(() => void 0) 吞掉:记录拉失败,
 * 首页就显示"今日巡检 0 次",不报错、不提示。领导看到会去问巡检员为什么
 * 没干活,而实际上是接口挂了。现在这个接口失败会抛出来,由页面显示"取不到"。
 */
export interface HomeCounts {
  approvals: number;
  abnormalAssets: number;
  todayRecords: number;
  rectifyTasks: number;
}

export function getHomeCounts() {
  return api<HomeCounts>("/api/management-ai/today");
}

// ===== 项目 =====
//
// 项目 = 一个现场。一个人可以同时属于多个项目;总部(数据范围「全部数据」)
// 不受项目限制,能看到所有项目的台账 —— 这是领导明确要的。
//
// 【项目名不能改】后端 assets / records 等业务表按中文项目名互相关联,
// 所以接口只给登记和停用,没有改名。

export interface ProjectEntry {
  id: string;
  name: string;
  code?: string;
  note?: string;
  disabled?: boolean;
  assetCount: number;
  memberCount: number;
  createdAt?: string;
}

export function listProjects() {
  return api<{ projects: ProjectEntry[] }>("/api/projects").then((d) => d.projects || []);
}

export function createProject(payload: { name: string; note?: string }) {
  return api("/api/projects", { method: "POST", body: JSON.stringify(payload) });
}

export function updateProject(id: string, payload: { note?: string; disabled: boolean }) {
  return api(`/api/projects/${encodeURIComponent(id)}`, { method: "PUT", body: JSON.stringify(payload) });
}

export function listUserProjects(userId: string) {
  return api<{ projectIds: string[] }>(`/api/users/${encodeURIComponent(userId)}/projects`).then(
    (d) => d.projectIds || [],
  );
}

export function setUserProjects(userId: string, projectIds: string[]) {
  return api(`/api/users/${encodeURIComponent(userId)}/projects`, {
    method: "PUT",
    body: JSON.stringify({ projectIds }),
  });
}

// ===== 部门 =====
//
// 【项目一直只有一条种子「默认部门」】没有写接口,后台的部门下拉永远只有一项。
// 部门名在租户内唯一 —— 两个同名部门在下拉里根本分不出来。

export function createDepartment(payload: { name: string; parentId?: string }) {
  return api<{ department: Department }>("/api/departments", {
    method: "POST",
    body: JSON.stringify(payload),
  });
}

export function updateDepartment(id: string, name: string) {
  return api(`/api/departments/${encodeURIComponent(id)}`, {
    method: "PUT",
    body: JSON.stringify({ name }),
  });
}

export function deleteDepartment(id: string) {
  return api(`/api/departments/${encodeURIComponent(id)}`, { method: "DELETE" });
}

// ===== 删除 =====
//
// 后端的规矩是【有东西引用就拒绝,并说清该怎么办】,不连带删除。
// 所以这些调用失败时要把后端的话原样显示出来 —— 那句话里有下一步动作
// (例如"请改用停用"),自己编一句"删除失败"等于把它丢掉了。

export function deleteUser(id: string) {
  return api(`/api/users/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function deleteProject(id: string) {
  return api(`/api/projects/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export function deleteRegistrationCode(code: string) {
  return api(`/api/registration-codes/${encodeURIComponent(code)}`, { method: "DELETE" });
}

// ===== 巡检模板的字段规则 =====
//
// 模板本身写死在后端代码里(字段、类型、选项、AI 提示词)。这里只配一件事:
// 每个字段【必填还是选填】。改这一项原来要改代码重新部署,而它是业务规则。
//
// asset_no 是台账认归属的字段,后端锁死不可改成选填 —— 放开之后记录会挂不到
// 任何设备上,而这种错要过很久对账时才发现。界面上它显示为锁定。

export interface TemplateFieldDTO {
  code: string;
  label: string;
  kind: string;
  required: boolean;
  source: string;
  options?: string[];
  manualOnly?: boolean;
}

export interface ReportTemplateDTO {
  id: string;
  name: string;
  project?: string;
  assetType?: string;
  maxImages?: number;
  /** 每单最少几张照片。0 = 不限 */
  minImages?: number;
  fields: TemplateFieldDTO[];
}

export function listReportTemplates() {
  return api<{ templates: ReportTemplateDTO[] }>("/api/report/templates").then(
    (d) => d.templates || [],
  );
}

/** 覆盖某个模板的必填配置。只传要改的字段;传空对象 = 全部改回代码默认值。 */
export function saveTemplateFields(
  id: string,
  required: Record<string, boolean>,
  /** 每单最少几张照片。不传 = 不改这一项 */
  minImages?: number,
) {
  return api(`/api/report/templates/${encodeURIComponent(id)}/fields`, {
    method: "PUT",
    body: JSON.stringify(minImages === undefined ? { required } : { required, minImages }),
  });
}

// ===== 今日应巡 =====
//
// 「完成」是自动判定的:这些设备今天有没有巡检快照 —— 不给现场加打勾的动作。

export interface DailyAssetStatus {
  assetId: string;
  assetName: string;
  project?: string;
  done: boolean;
  doneAt?: string;
  /** 台账里已经查不到这台设备(计划录入后被删) */
  missing?: boolean;
  /**
   * 设备类型(有机房电梯 / 扶梯 …)。
   *
   * 【用来区分同名设备】台账允许两台都叫 K01,只是类型不同。
   * 清单里不写类型的话,两行长得一模一样 —— 到了现场不知道该巡哪一台。
   */
  assetType?: string;
}

export interface DailyPlanStatus {
  planId: string;
  title: string;
  project?: string;
  ownerName?: string;
  total: number;
  done: number;
  assets: DailyAssetStatus[];
  /** 这条计划没指定设备,算不出完成率 */
  noAssets?: boolean;
}

export interface TodayBoard {
  date: string;
  weekday: number; // 1=周一 … 7=周日
  total: number;
  done: number;
  plans: DailyPlanStatus[];
}

export function getTodayBoard() {
  return api<TodayBoard>("/api/engineering/plans/today");
}

/** 计划类型的中文名。和后端 planTypeNames 一一对应 */
export const PLAN_TYPES = [
  { value: "daily", label: "每日巡检" },
  { value: "weekly", label: "周计划" },
  { value: "monthly", label: "月度计划" },
  { value: "yearly", label: "年度计划" },
  { value: "adhoc", label: "临时计划" },
] as const;

export const WEEKDAY_OPTIONS = [
  { value: "1", label: "一" },
  { value: "2", label: "二" },
  { value: "3", label: "三" },
  { value: "4", label: "四" },
  { value: "5", label: "五" },
  { value: "6", label: "六" },
  { value: "7", label: "日" },
] as const;
