const API_BASE_KEY = "inspectai_admin_api_base";
const API_TOKEN_KEY = "inspectai_admin_token";
const ADMIN_PLANS_KEY = "inspectai_admin_plans";
const ADMIN_TASKS_KEY = "inspectai_admin_tasks";

const pageLabels = {
  dashboard: "首页",
  profile: "个人首页",
  plan: "巡检计划",
  task: "巡检任务",
  record: "巡检记录",
  ledger: "资产台账",
  device: "设备管理",
  exception: "异常复核",
  approval: "修改审批",
  data: "数据看板",
  report: "统计报表",
  users: "用户与权限",
  logs: "操作日志",
  system: "系统管理",
};

function defaultApiBase() {
  // 生产场景：浏览器从 nginx 反代进来（80/443），后端在容器内，
  // 同源就能访问 /api/* — 不能再硬编码 :18080，否则去访问不存在的端口。
  // 本地开发场景：admin 静态服务在 18081，go-backend 在 18080，需要跨端口。
  const { protocol, hostname, port } = window.location;
  if ((protocol === "http:" || protocol === "https:") && hostname) {
    if (port === "18081") {
      return `${protocol}//${hostname}:18080`;
    }
    return window.location.origin;
  }
  return "http://127.0.0.1:18080";
}

const state = {
  apiBase: localStorage.getItem(API_BASE_KEY) || defaultApiBase(),
  token: localStorage.getItem(API_TOKEN_KEY) || "",
  page: new URLSearchParams(location.search).get("page") || "dashboard",
  assets: [],
  records: [],
  requests: [],
  points: [],
  templates: [],
  users: [],
  operationLogs: [],
  customPlans: loadLocalArray(ADMIN_PLANS_KEY),
  customTasks: loadLocalArray(ADMIN_TASKS_KEY),
  currentUser: null,
  health: null,
  selectedProject: "",
  selectedAssetId: "",
  selectedRecordId: "",
  assetDetails: {}, // §3 资产详情缓存：{ id → { history, total, page, pageSize, trend } }
  confirmLogs: {},  // §4 记录的字段确认留痕：{ recordId → [log,...] }
  dataInsights: {}, // 阶段一 数据看板缓存：{ key → { overview, items, summary, model, generatedAt } }
  loadErrors: {},   // P1-4 接口失败留痕：{ "scope:id" → 错误消息 }；为空表示无错
  recordPage: 0,
  filters: {
    assetType: "",
    status: "",
    keyword: "",
  },
  recordFilters: {
    project: "",
    template: "",
    status: "",
    flowStatus: "",
    keyword: "",
  },
  planFilters: {
    project: "",
    frequency: "",
    status: "",
    keyword: "",
  },
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => Array.from(document.querySelectorAll(selector));

function normalizeButtons(root = document) {
  root.querySelectorAll("button:not([type])").forEach((button) => {
    button.type = "button";
  });
}

function loadLocalArray(key) {
  try {
    const parsed = JSON.parse(localStorage.getItem(key) || "[]");
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

function saveLocalArray(key, value) {
  localStorage.setItem(key, JSON.stringify(value || []));
}

function clientId(prefix) {
  return `${prefix}_${Date.now()}_${Math.random().toString(16).slice(2, 8)}`;
}

function escapeHTML(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function fmtTime(value, mode = "minute") {
  if (!value) return "-";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "-";
  const options = mode === "date"
    ? { year: "numeric", month: "2-digit", day: "2-digit" }
    : { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false };
  return date.toLocaleString("zh-CN", options).replace(/\//g, "-");
}

function todayKey() {
  return new Date().toISOString().slice(0, 10);
}

function apiHeaders(json = false) {
  const user = state.currentUser || {};
  const headers = {
    "X-User-Role": user.roleCode || "supervisor",
    "X-User-Name": encodeURIComponent(user.displayName || "张管理员"),
  };
  if (state.token) headers["X-InspectAI-Token"] = state.token;
  if (json) headers["Content-Type"] = "application/json";
  return headers;
}

async function api(path, options = {}) {
  const res = await fetch(state.apiBase + path, {
    ...options,
    headers: { ...apiHeaders(Boolean(options.body)), ...(options.headers || {}) },
    credentials: "include",
  });
  const text = await res.text();
  let data = {};
  try {
    data = text ? JSON.parse(text) : {};
  } catch {
    data = { raw: text };
  }
  if (!res.ok) throw new Error(data.message || data.error || `请求失败 ${res.status}`);
  return data;
}

async function safeApi(path, fallback) {
  try {
    return await api(path);
  } catch {
    return fallback;
  }
}

// §3 资产详情：拉「分页历史快照 + 后端规则算的字段趋势」。
// 历史用 asset_snapshots 表，不再受巡检记录 100/200 窗口限制；趋势数值字段才有意义。
const assetDetailInflight = new Set();
async function loadAssetDetail(assetId) {
  if (!assetId || assetId in state.assetDetails || assetDetailInflight.has(assetId)) return;
  assetDetailInflight.add(assetId);
  const errKey = `assetDetail:${assetId}`;
  try {
    const id = encodeURIComponent(assetId);
    // P1-4 改用 api() 真抛错(safeApi 隐式吞掉 → 永远显示"暂无数据"误导用户)
    const [hist, rep, events] = await Promise.all([
      api(`/api/assets/${id}/records?page=1&pageSize=20`),
      api(`/api/assets/${id}/report?range=90d`),
      api(`/api/assets/${id}/status-events?range=30d`),
    ]);
    state.assetDetails[assetId] = {
      history: hist.records || [],
      total: hist.total || 0,
      page: hist.page || 1,
      pageSize: hist.pageSize || 20,
      trend: rep.fields || [],
      events,
    };
    delete state.loadErrors[errKey];
  } catch (err) {
    state.assetDetails[assetId] = { history: [], total: 0, page: 1, pageSize: 20, trend: [], events: null };
    state.loadErrors[errKey] = err && err.message || "接口请求失败";
  } finally {
    assetDetailInflight.delete(assetId);
  }
  render();
}

// §4 字段确认留痕：拉某条记录的所有 field_confirm_logs，展示给主管做防惰性追溯。
const confirmLogsInflight = new Set();
async function loadConfirmLogs(recordId) {
  if (!recordId || recordId in state.confirmLogs || confirmLogsInflight.has(recordId)) return;
  confirmLogsInflight.add(recordId);
  const errKey = `confirmLogs:${recordId}`;
  try {
    const data = await api(`/api/inspection/records/${encodeURIComponent(recordId)}/confirm-logs`);
    state.confirmLogs[recordId] = data.logs || [];
    delete state.loadErrors[errKey];
  } catch (err) {
    state.confirmLogs[recordId] = [];
    state.loadErrors[errKey] = err && err.message || "接口请求失败";
  } finally {
    confirmLogsInflight.delete(recordId);
  }
  render();
}

async function login(username, password) {
  const res = await fetch(state.apiBase + "/api/auth/login", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    credentials: "include",
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || data.error || `登录失败 ${res.status}`);
  state.token = data.token || "";
  state.currentUser = data.user || null;
  localStorage.setItem(API_TOKEN_KEY, state.token);
  applyRoleBodyClass();
  updateUserBadge();
  return data;
}

async function loadMe() {
  // F9 修复 · 不再编 legacy_supervisor 假身份，404 直接抛错让上层走登录页
  const data = await api("/api/auth/me");
  state.currentUser = data.user || null;
  applyRoleBodyClass();
  updateUserBadge();
  return state.currentUser;
}

// C-Phase A2:根据 user.roleCode 切 body class,控制 nav 哪些项可见。
// 默认主管视角(5 项);admin / manager 走管理员视角追加 3 项。
function applyRoleBodyClass() {
  const role = (state.currentUser && state.currentUser.roleCode) || "supervisor";
  document.body.classList.remove("role-admin", "role-manager", "role-supervisor", "role-inspector");
  document.body.classList.add(`role-${role}`);
  // admin / manager 共享同一套扩展 nav
  if (role === "admin" || role === "manager") {
    document.body.classList.add("role-admin");
  }
}

async function logout() {
  try {
    await api("/api/auth/logout", { method: "POST" });
  } catch {}
  state.token = "";
  state.currentUser = null;
  document.body.classList.remove("role-admin", "role-manager", "role-supervisor", "role-inspector");
  localStorage.removeItem(API_TOKEN_KEY);
  renderLogin();
  updateUserBadge();
}

function mediaUrl(path) {
  if (!path) return "";
  if (/^https?:\/\//i.test(path)) return path;
  const normalized = String(path).replace(/\\/g, "/");
  const idx = normalized.indexOf("/storage/");
  let storagePath = idx >= 0 ? normalized.slice(idx + "/storage/".length) : normalized;
  storagePath = storagePath.replace(/^\/?storage\//, "").replace(/^\/+/, "");
  return `${state.apiBase}/storage/${encodeURI(storagePath)}`;
}

function toast(message) {
  const el = $("#toast");
  el.textContent = message;
  el.classList.add("show");
  clearTimeout(el._timer);
  el._timer = setTimeout(() => el.classList.remove("show"), 2200);
}

function normalizeStatus(status = "") {
  const raw = String(status || "");
  if (raw.includes("正常") || raw === "normal") return "normal";
  if (raw.includes("异常") || raw === "danger") return "danger";
  if (raw.includes("维修") || raw === "repair") return "repair";
  if (raw.includes("复核") || raw.includes("待") || raw === "warning") return "warning";
  return "unknown";
}

const ASSET_STATUS_OPTIONS = ["正常", "待复核", "异常", "待维修", "未巡检", "未知"];
const RECORD_RESULT_OPTIONS = ["正常", "待复核", "异常"];
const RECORD_FLOW_OPTIONS = ["未开始", "识别中", "已识别", "需重拍", "人工填写", "已提交"];
const PLAN_STATUS_OPTIONS = ["启用", "暂停", "草稿", "已停用"];

function statusLabel(value = "") {
  const map = {
    normal: "正常",
    warning: "待复核",
    danger: "异常",
    repair: "待维修",
    unknown: "未知",
    not_started: "未开始",
    processing: "识别中",
    recognized: "已识别",
    retake_required: "需重拍",
    manual_required: "人工填写",
    pending: "待复核",
    approved: "已通过",
    rejected: "已拒绝",
    withdrawn: "已撤回",
    asset: "资产",
    record: "巡检记录",
  };
  return map[value] || value || "-";
}

function statusClass(status = "") {
  const level = normalizeStatus(status);
  if (level === "normal") return "normal";
  if (level === "danger") return "danger";
  if (level === "repair" || level === "warning") return "warning";
  return "unknown";
}

function shortId(id = "") {
  const parts = String(id).split("::");
  return parts[parts.length - 1] || id || "-";
}

function logActionTone(action = "") {
  const s = String(action).toLowerCase();
  if (s.includes("login")) return "tone-success";
  if (s.includes("logout")) return "tone-muted";
  if (s.includes("delete") || s.includes("reject") || s.includes("disable")) return "tone-danger";
  if (s.includes("approve") || s.includes("create")) return "tone-success";
  if (s.includes("patch") || s.includes("update") || s.includes("modify")) return "tone-info";
  return "tone-muted";
}

function locationText(asset) {
  const pointId = asset.pointId;
  if (pointId) {
    const point = (state.points || []).find((p) => p.id === pointId);
    if (point) {
      const left = point.project || asset.project || "";
      const right = point.name || point.location || "";
      if (left && right) return `${left} · ${right}`;
      return right || left || pointId;
    }
  }
  return asset.project || asset.projectCode || pointId || "-";
}

function assetKey(asset) {
  return asset.assetKey || shortId(asset.id);
}

function primaryReading(record) {
  const field = (record.fields || []).find((item) => item.value || item.aiValue);
  if (!field) return "";
  return `${field.label || field.code}=${field.value || field.aiValue}`;
}

const ABNORMAL_VALUE_RE = /异常|告警|故障|离线|不合格|超标|漏水|渗漏|报警|破损|损坏|缺失|跳闸|烧毁/;

function recordLevel(record) {
  // 异常只认「人工确认后的字段值」，不扫 AI 总结 / 报告 / 解释文本：
  // 旧实现会把总结里的"未发现异常"、报告里的"需人工复核"误判成异常/待复核，
  // 导致正常的已提交记录全被标成异常或待复核、normal 一条都露不出来。
  const valueText = (record.fields || []).map((field) => String(field.value || "")).join(" ");
  if (ABNORMAL_VALUE_RE.test(valueText)) return "danger";
  // 已提交：提交时已拦截所有 needsReview / 必填缺失，无异常值即视为正常。
  if (record.submitted) return "normal";
  // 未提交：仍需人工介入的（低置信待确认 / 需重拍）标为待复核，其余按识别流程状态展示。
  if ((record.fields || []).some((field) => field.needsReview)) return "warning";
  if (record.recognitionStatus === "retake_required") return "warning";
  return "normal";
}

// 是否已产出可判定结果（已提交 / AI 已识别 / 已有字段值）。未到结果阶段不强标"正常"。
function hasInspectionResult(record) {
  if (record.submitted || record.recognitionStatus === "recognized") return true;
  return (record.fields || []).some((field) => String(field.value || "").trim());
}

// 业务结果状态：正常 / 待复核 / 异常 / ""（尚无结果）。主管真正关心的是这一轴。
function recordResultStatus(record) {
  if (!hasInspectionResult(record)) return "";
  const level = recordLevel(record);
  if (level === "danger") return "异常";
  if (level === "warning") return "待复核";
  return "正常";
}

// 流程状态：AI 识别 / 提交流程走到哪一步，与结果无关。
function recordFlowStatus(record) {
  if (record.submitted) return "已提交";
  if (record.manualRequired || record.recognitionStatus === "manual_required") return "人工填写";
  const map = { not_started: "未开始", queued: "识别中", processing: "识别中", recognized: "已识别", retake_required: "需重拍" };
  return map[record.recognitionStatus] || "未开始";
}

// 主状态（单标签展示位复用）：结果优先，无结果时回退流程状态。
function recordStatus(record) {
  return recordResultStatus(record) || recordFlowStatus(record);
}

function projects() {
  const names = [
    ...state.assets.map((item) => item.project || item.projectCode),
    ...state.records.map((item) => item.project),
    ...state.points.map((item) => item.project),
    ...state.templates.map((item) => item.project),
    ...state.customPlans.map((item) => item.project),
    ...state.customTasks.map((item) => item.project),
  ].filter(Boolean);
  return [...new Set(names)];
}

function filteredAssets() {
  const keyword = state.filters.keyword.trim().toLowerCase();
  return state.assets.filter((asset) => {
    if (state.selectedProject && asset.project !== state.selectedProject && asset.projectCode !== state.selectedProject) return false;
    if (state.filters.assetType && asset.assetType !== state.filters.assetType) return false;
    if (state.filters.status) {
      const targetLevel = normalizeStatus(state.filters.status);
      const assetLevel = asset.statusLevel || normalizeStatus(asset.lastStatus);
      if (asset.lastStatus !== state.filters.status && assetLevel !== targetLevel) return false;
    }
    if (!keyword) return true;
    return [
      asset.id,
      assetKey(asset),
      asset.assetName,
      asset.assetType,
      asset.project,
      asset.pointId,
      locationText(asset),
      asset.lastSummary,
    ].join(" ").toLowerCase().includes(keyword);
  });
}

function filteredRecords() {
  return state.records.filter((record) => {
    if (!state.selectedProject) return true;
    return record.project === state.selectedProject;
  });
}

function recordSearchText(record) {
  return [
    record.id,
    record.project,
    record.pointName,
    record.pointId,
    record.templateName,
    record.templateId,
    record.inspector,
    record.inspectorName,
    record.createdBy,
    record.aiSummary,
    record.report,
    record.retakeReason,
    primaryReading(record),
    ...(record.fields || []).flatMap((field) => [
      field.code,
      field.label,
      field.value,
      field.aiValue,
      field.manualValue,
      field.reason,
    ]),
  ].filter(Boolean).join(" ").toLowerCase();
}

function filteredRecordRows() {
  const filters = state.recordFilters;
  const project = filters.project.trim();
  const template = filters.template.trim();
  const result = filters.status.trim();                 // 结果状态筛选：正常 / 待复核 / 异常
  const flowStatus = (filters.flowStatus || "").trim();  // 流程状态筛选：未开始 … 已提交
  const keyword = filters.keyword.trim().toLowerCase();
  return state.records.filter((record) => {
    if (state.selectedProject && !project && record.project !== state.selectedProject) return false;
    if (project && record.project !== project) return false;
    if (template && record.templateName !== template && record.templateId !== template) return false;
    if (result && recordResultStatus(record) !== result) return false;
    if (flowStatus && recordFlowStatus(record) !== flowStatus) return false;
    if (!keyword) return true;
    return recordSearchText(record).includes(keyword);
  });
}

function filteredRequests() {
  return state.requests.filter((request) => {
    if (!state.selectedProject) return true;
    if (request.project === state.selectedProject || request.projectCode === state.selectedProject) return true;
    return state.assets.some((asset) => {
      const sameProject = asset.project === state.selectedProject || asset.projectCode === state.selectedProject;
      return sameProject && (asset.id === request.targetId || asset.lastRecordId === request.targetId);
    });
  });
}

function statusCounts(assets = filteredAssets()) {
  return assets.reduce((acc, asset) => {
    const level = asset.statusLevel || normalizeStatus(asset.lastStatus);
    acc.total += 1;
    acc[level] = (acc[level] || 0) + 1;
    return acc;
  }, { total: 0, normal: 0, warning: 0, danger: 0, repair: 0, unknown: 0 });
}

function latest(items, dateField = "createdAt") {
  return [...items].sort((a, b) => new Date(b[dateField] || 0) - new Date(a[dateField] || 0))[0];
}

function collectPhotosFromRecord(record) {
  return (record.images || []).map((image) => mediaUrl(image.path)).filter(Boolean);
}

function truncateText(str, n) {
  const s = String(str || "");
  return s.length > n ? s.slice(0, n) + "…" : s;
}

function formatNum(n) {
  if (typeof n !== "number" || !isFinite(n)) return "-";
  const abs = Math.abs(n);
  if (abs >= 1000) return n.toFixed(0);
  if (abs >= 10) return n.toFixed(1);
  return n.toFixed(2);
}

function renderSparkline(points) {
  if (!points || points.length < 2) return "";
  const vals = points.map((p) => Number(p.value));
  const min = Math.min(...vals), max = Math.max(...vals);
  const range = max - min || 1;
  const w = 180, h = 32, padY = 3;
  const step = w / (points.length - 1);
  const d = vals.map((v, i) => {
    const x = (i * step).toFixed(1);
    const y = (h - padY - ((v - min) / range) * (h - 2 * padY)).toFixed(1);
    return (i ? "L" : "M") + x + "," + y;
  }).join(" ");
  return `<svg viewBox="0 0 ${w} ${h}" width="100%" height="${h}" preserveAspectRatio="none"><path d="${d}" fill="none" stroke="#246bfe" stroke-width="1.5"/></svg>`;
}

// P1-2 状态事件统计卡 — 电梯/状态类资产的核心展示,代替数值字段折线
function renderStatusEventsCard(events) {
  const total = events.inspections || 0;
  const pct = (n) => total > 0 ? Math.round((n / total) * 100) : 0;
  const repeatFields = events.repeatFields || [];
  return `
    <div class="status-events">
      <h4>状态事件统计 <small>近 30 天 · ${total} 次巡检</small></h4>
      <div class="se-bars">
        <div class="se-bar"><span class="se-label">正常</span><div class="se-track"><div class="se-fill se-normal" style="width:${pct(events.normal)}%"></div></div><b class="se-num">${events.normal || 0}</b></div>
        <div class="se-bar"><span class="se-label">待复核</span><div class="se-track"><div class="se-fill se-warning" style="width:${pct(events.warning)}%"></div></div><b class="se-num">${events.warning || 0}</b></div>
        <div class="se-bar"><span class="se-label">异常</span><div class="se-track"><div class="se-fill se-danger" style="width:${pct(events.danger)}%"></div></div><b class="se-num">${events.danger || 0}</b></div>
      </div>
      <div class="se-meta-row">
        <span>补拍 <b>${events.retakeCount || 0}</b></span>
        <span>·</span>
        <span>无法判定 <b>${events.uncertainCount || 0}</b></span>
        <span>·</span>
        <span>未看图确认 <b class="${(events.noPhotoConfirm || 0) > 0 ? "danger-num" : ""}">${events.noPhotoConfirm || 0}</b></span>
      </div>
      ${repeatFields.length ? `
        <div class="se-repeat">
          <b class="se-repeat-title">重复异常字段</b>
          <div class="se-repeat-list">
            ${repeatFields.map((f) => `<span class="se-repeat-pill">${escapeHTML(f.fieldLabel || f.fieldKey)} <em>×${f.count}</em></span>`).join("")}
          </div>
        </div>
      ` : ""}
    </div>
  `;
}

// §3 字段趋势卡片：本次/上次/近7均 + 变化率徽章 + 简易折线
function renderTrendCard(field) {
  const cur = formatNum(field.current);
  const prev = formatNum(field.previous);
  const avg = formatNum(field.avgRecent);
  let rate = "-";
  let rateClass = "";
  if (typeof field.changeRate === "number" && isFinite(field.changeRate)) {
    rate = (field.changeRate >= 0 ? "+" : "") + (field.changeRate * 100).toFixed(1) + "%";
    rateClass = field.overThreshold ? "danger" : (field.changeRate > 0 ? "up" : (field.changeRate < 0 ? "down" : ""));
  }
  return `
    <div class="trend-card">
      <div class="trend-card-head">
        <span class="trend-label">${escapeHTML(field.fieldLabel || field.fieldKey)}</span>
        <span class="trend-rate ${rateClass}">${escapeHTML(rate)}</span>
      </div>
      <div class="trend-spark">${renderSparkline(field.points || [])}</div>
      <div class="trend-meta">
        <span>本次 <b>${escapeHTML(cur)}</b></span>
        <span>上次 <b>${escapeHTML(prev)}</b></span>
        <span>近7均 <b>${escapeHTML(avg)}</b></span>
      </div>
    </div>
  `;
}

function collectAssetPhotos(asset, history = []) {
  const paths = [];
  if (asset?.coverImage?.path) paths.push(asset.coverImage.path);
  if (asset?.lastPhotoPath) paths.push(asset.lastPhotoPath);
  for (const record of history) {
    for (const image of record.images || []) {
      if (image.path) paths.push(image.path);
    }
  }
  return [...new Set(paths)].map(mediaUrl);
}

async function loadData(showToast = true) {
  try {
    const [assets, records, requests, points, templates, health, users, operationLogs] = await Promise.all([
      api("/api/assets"),
      api("/api/inspection/records"),
      api("/api/change-requests"),
      safeApi("/api/inspection/points", { points: [] }),
      safeApi("/api/report/templates", { templates: [] }),
      safeApi("/health", null),
      safeApi("/api/users", { users: [] }),
      safeApi("/api/operation-logs?limit=80", { logs: [] }),
    ]);
    state.assets = assets.assets || [];
    state.records = records.records || [];
    state.assetDetails = {}; // §3 资产详情(历史+趋势)缓存随数据刷新清空
    state.confirmLogs = {};  // §4 字段确认留痕缓存随数据刷新清空
    state.dataInsights = {}; // 阶段一 数据看板/洞察缓存随数据刷新清空
    state.requests = requests.requests || [];
    state.points = points.points || [];
    state.templates = templates.templates || [];
    state.health = health;
    state.users = users.users || [];
    state.operationLogs = operationLogs.logs || [];
    if (!state.selectedAssetId || !state.assets.some((asset) => asset.id === state.selectedAssetId)) {
      state.selectedAssetId = latest(filteredAssets(), "updatedAt")?.id || state.assets[0]?.id || "";
    }
    if (!state.selectedRecordId || !state.records.some((record) => record.id === state.selectedRecordId)) {
      state.selectedRecordId = latest(filteredRecords())?.id || state.records[0]?.id || "";
    }
    renderProjectOptions();
    render();
    if (showToast) toast("后台数据已刷新");
  } catch (error) {
    renderError(error.message);
  }
}

function renderProjectOptions() {
  const select = $("#projectSelect");
  const current = state.selectedProject || select.value;
  const options = [`<option value="">全部项目</option>`]
    .concat(projects().map((project) => `<option value="${escapeHTML(project)}">${escapeHTML(project)}</option>`));
  select.innerHTML = options.join("");
  state.selectedProject = projects().includes(current) ? current : "";
  select.value = state.selectedProject;
}

function setPage(page, push = true) {
  state.page = pageLabels[page] ? page : "dashboard";
  if (push) {
    const url = new URL(location.href);
    url.searchParams.set("page", state.page);
    history.pushState({ page: state.page }, "", url);
  }
  render();
}

function setLoginScreen(enabled) {
  document.body.classList.toggle("login-screen", Boolean(enabled));
}

function render() {
  setLoginScreen(false);
  $$(".nav button").forEach((btn) => btn.classList.toggle("active", btn.dataset.page === state.page));
  $("#pendingBadge").textContent = filteredRequests().filter((item) => item.status === "pending").length;
  updateUserBadge();
  const renderer = pageRenderers[state.page] || renderDashboardPage;
  renderer();
  normalizeButtons();
}

function updateUserBadge() {
  const el = $(".admin-user");
  if (!el) return;
  const user = state.currentUser || { displayName: "未登录" };
  const name = user.displayName || user.username || "未登录";
  const initial = name.slice(0, 1) || "?";
  const todos = myTodoCount();
  const dot = todos > 0 ? `<i class="user-dot" aria-label="${todos} 个待办">${todos > 9 ? "9+" : todos}</i>` : "";
  el.innerHTML = `<b>${escapeHTML(initial)}</b><span>${escapeHTML(name)}</span>${dot}`;
  el.title = todos > 0 ? `${todos} 个待办` : "当前登录用户";
}

function myAssignedTasks() {
  const user = state.currentUser;
  if (!user) return [];
  const name = user.displayName || user.username;
  return (state.customTasks || []).filter((t) => t.owner === name || t.owner === user.username);
}

function myOpenTasks() {
  // 仍需处理的：待执行 + 进行中
  return myAssignedTasks().filter((t) => t.status === "待执行" || t.status === "进行中");
}

function myCompletedTodayTasks() {
  const today = todayKey();
  return myAssignedTasks().filter((t) => t.status === "已完成" && String(t.completedAt || "").startsWith(today));
}

function myAssignedPlans() {
  const user = state.currentUser;
  if (!user) return [];
  const name = user.displayName || user.username;
  return (state.customPlans || []).filter((p) => p.status === "启用" && (p.owner === name || p.owner === user.username));
}

function myTodoCount() {
  const user = state.currentUser;
  if (!user) return 0;
  let total = myOpenTasks().length;
  if (["admin", "manager", "supervisor"].includes(user.roleCode || "")) {
    total += (state.requests || []).filter((r) => r.status === "pending").length;
  }
  return total;
}

function renderLogin(message = "") {
  setLoginScreen(true);
  $$(".nav button").forEach((btn) => btn.classList.remove("active"));
  $("#pendingBadge").textContent = "0";
  $("#pageMain").innerHTML = `
    <section class="login-panel">
      <form class="login-card" id="loginForm">
        <div class="login-card-head">
          <strong>JADEAST <span>智巡后台</span></strong>
          <b>后台登录</b>
        </div>
        <label>账号<input name="username" autocomplete="username" value="admin" placeholder="请输入账号" required></label>
        <label>密码
          <div class="password-field">
            <input id="loginPassword" name="password" type="password" autocomplete="current-password" placeholder="请输入密码" required>
            <button class="password-toggle" id="passwordToggleBtn" type="button" aria-label="显示密码" aria-pressed="false">
              <svg viewBox="0 0 24 24" aria-hidden="true">
                <path d="M2.4 12s3.5-6 9.6-6 9.6 6 9.6 6-3.5 6-9.6 6-9.6-6-9.6-6Z"></path>
                <circle cx="12" cy="12" r="3.2"></circle>
              </svg>
            </button>
          </div>
        </label>
        ${message ? `<p class="login-error">${escapeHTML(message)}</p>` : ""}
        <button class="primary" type="submit">登录后台</button>
      </form>
    </section>
  `;
  $("#pageAside").innerHTML = sideInfo("登录说明", [
    ["当前接口", state.apiBase],
    ["第一阶段", "账号密码登录 + 会话令牌"],
    ["后续扩展", "企业微信 OAuth 与组织架构绑定"],
  ]);
  normalizeButtons();
  bindLoginPasswordToggle();
}

function bindLoginPasswordToggle() {
  const input = $("#loginPassword");
  const button = $("#passwordToggleBtn");
  if (!input || !button) return;
  button.addEventListener("click", () => {
    const visible = input.type === "password";
    input.type = visible ? "text" : "password";
    button.classList.toggle("visible", visible);
    button.setAttribute("aria-label", visible ? "隐藏密码" : "显示密码");
    button.setAttribute("aria-pressed", String(visible));
    input.focus();
  });
}

function renderError(message) {
  $("#pageMain").innerHTML = `
    <section class="page-hero">
      <div><span>后台连接异常</span><h1>无法读取后端数据</h1><p>${escapeHTML(message)}</p></div>
      <button id="retryLoadBtn">重新连接</button>
    </section>
  `;
  $("#pageAside").innerHTML = sideInfo("排查建议", [
    ["Go API", state.apiBase],
    ["后台端口", "18081"],
    ["处理方式", "先确认 18080 /health 是否可访问"],
  ]);
  $("#retryLoadBtn").addEventListener("click", () => loadData());
  normalizeButtons();
}

function metrics(items) {
  return `<section class="metric-row">${items.map((item) => `
    <article class="metric">
      <span>${escapeHTML(item.label)}</span>
      <b>${escapeHTML(item.value)}</b>
      <small class="${item.good ? "good" : item.bad ? "bad" : ""}">${escapeHTML(item.sub || "")}</small>
    </article>
  `).join("")}</section>`;
}

function sideInfo(title, rows) {
  return `
    <div class="detail-head"><h2>${escapeHTML(title)}</h2></div>
    <div class="side-stack">
      ${rows.map(([key, value]) => `
        <div class="info-card"><b>${escapeHTML(key)}</b><span>${escapeHTML(value)}</span></div>
      `).join("")}
    </div>
  `;
}

// ============================================================
// M27 · 全站实时动态 aside
// ============================================================
function relativeTime(iso) {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (!isFinite(t)) return "—";
  const diff = Date.now() - t;
  if (diff < 60_000) return "刚刚";
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)} 分钟前`;
  if (diff < 86400_000) return `${Math.floor(diff / 3600_000)} 小时前`;
  const days = Math.floor(diff / 86400_000);
  if (days < 7) return `${days} 天前`;
  return fmtTime(iso);
}

function liveActivityCard(opts = {}) {
  const limit = opts.limit || 9;
  const logs = (state.operationLogs || []).slice(0, limit);
  const head = `
    <div class="live-activity-head">
      <div class="live-activity-title">
        <span class="live-dot"></span>
        <b>实时动态</b>
      </div>
      <span class="live-activity-sub">全站 · ${logs.length} 条</span>
    </div>`;
  if (!logs.length) {
    return `<div class="live-activity">${head}<div class="live-activity-empty">暂无最近动作</div></div>`;
  }
  return `
    <div class="live-activity">
      ${head}
      <ul class="live-activity-list">
        ${logs.map(renderLiveActivityItem).join("")}
      </ul>
    </div>
  `;
}

function renderLiveActivityItem(log) {
  const tone = logActionTone(log.action);
  const label = actionLabel(log.action);
  const actor = log.actorName || "系统";
  const initial = (actor || "?").slice(0, 1);
  const time = relativeTime(log.createdAt);
  const target = log.targetType ? `${TARGET_LABEL[log.targetType] || log.targetType} ${shortId(log.targetId || "")}` : "";
  return `
    <li class="live-row tone-${tone}" data-live-target="${escapeHTML(log.targetType || "")}" data-live-target-id="${escapeHTML(log.targetId || "")}">
      <span class="live-avatar">${escapeHTML(initial)}</span>
      <div class="live-body">
        <div class="live-line">
          <b>${escapeHTML(actor)}</b>
          <span class="live-action">${escapeHTML(label)}</span>
        </div>
        ${target ? `<div class="live-target">${escapeHTML(target)}</div>` : ""}
      </div>
      <time>${escapeHTML(time)}</time>
    </li>
  `;
}

const TARGET_LABEL = {
  asset: "资产",
  record: "记录",
  user: "用户",
  ai: "AI",
  change_request: "申请",
  changeRequest: "申请",
};

const TARGET_PAGE = {
  asset: "ledger",
  record: "record",
  user: "users",
  change_request: "approval",
  changeRequest: "approval",
};

function bindLiveActivity() {
  document.querySelectorAll(".live-row").forEach((row) => {
    const tt = row.dataset.liveTarget;
    const id = row.dataset.liveTargetId;
    const page = TARGET_PAGE[tt];
    if (!page) {
      row.classList.add("no-click");
      return;
    }
    row.classList.add("clickable");
    row.addEventListener("click", () => {
      if (tt === "asset" && id) state.selectedAssetId = id;
      if (tt === "record" && id) state.selectedRecordId = id;
      setPage(page);
    });
  });
}

function asideStack(...cards) {
  return `<div class="aside-stack">${cards.filter(Boolean).join("")}</div>`;
}

function renderDashboardPage() {
  const assets = filteredAssets();
  const records = filteredRecords();
  const requests = filteredRequests();
  const counts = statusCounts(assets);
  const today = new Date().toISOString().slice(0, 10);
  const todayRecords = records.filter((r) => String(r.createdAt || "").slice(0, 10) === today);
  const todayAuto = todayRecords.filter((r) => recordLevel(r) === "normal").length;
  const todayFlagged = todayRecords.length - todayAuto;
  const savedHours = Math.round(todayAuto * 0.07 * 10) / 10;
  const accuracy = aiAccuracy(records);
  const issuesCount = counts.warning + counts.danger + counts.repair;
  const pendingApprovals = requests.filter((item) => item.status === "pending").length;
  const issues = topIssues(assets);
  const insights = riskInsights(records, assets);
  const tileCounts = quickAccessCounts(records);

  const trendCounts = last7DayAbnormal(records);
  const todayKeyStr = todayKey();
  const todayTaskDone = (state.customTasks || []).filter((t) => t.status === "已完成" && String(t.completedAt || "").startsWith(todayKeyStr)).length;
  // 阶段一:首页加 Top 3 重点关注 mini 卡(数据看板算好的 risk_score),引流去洞察台
  const insightKey = `month::${state.selectedProject || ""}`;
  if (!(insightKey in state.dataInsights)) loadDataInsights("month", state.selectedProject || "");
  const dashInsights = state.dataInsights[insightKey] || { items: [], summary: "" };
  $("#pageMain").innerHTML = `
    ${aiHeroBanner({ todayRecords: todayRecords.length, todayAuto, todayFlagged, savedHours, issuesCount, accuracy, trendCounts, todayTaskDone })}
    ${quickAccessTiles(tileCounts)}
    ${dashboardFocusMini(dashInsights)}
    ${aiChatPanel()}
  `;
  $("#pageAside").innerHTML = dashboardAside({
    monthly: monthlyAiStats(),
    pendingExceptions: issuesCount,
    pendingApprovals,
    pendingTasks: tileCounts.tasks,
    accuracy,
    issues,
    attentionItems: dashInsights.items || [],
  });
  animateDashboardCounts();
  animateHealthRings();
  bindAiChat();
  bindHeroCursor();
}

function bindHeroCursor() {
  const hero = document.querySelector(".ai-hero.ai-hero-v2");
  const cursor = hero?.querySelector(".ai-hero-cursor");
  if (!hero || !cursor) return;
  let rafId = 0;
  let targetX = 0, targetY = 0;
  let curX = 0, curY = 0;
  let visible = false;

  function tick() {
    curX += (targetX - curX) * 0.18;
    curY += (targetY - curY) * 0.18;
    cursor.style.transform = `translate3d(${curX - 14}px, ${curY - 14}px, 0) rotate(${(curX - hero.clientWidth / 2) * 0.08}deg)`;
    rafId = requestAnimationFrame(tick);
  }

  hero.addEventListener("mouseenter", () => {
    visible = true;
    cursor.classList.add("show");
    if (!rafId) tick();
  });
  hero.addEventListener("mouseleave", () => {
    visible = false;
    cursor.classList.remove("show");
    cancelAnimationFrame(rafId);
    rafId = 0;
  });
  hero.addEventListener("mousemove", (e) => {
    const r = hero.getBoundingClientRect();
    targetX = e.clientX - r.left;
    targetY = e.clientY - r.top;
  });
}

// 首页「重点关注 Top 3」mini 卡 —— 引流到数据看板看全部。
function dashboardFocusMini(insights) {
  const items = (insights.items || []).slice(0, 3);
  if (!items.length) {
    return `
      <section class="panel focus-mini-panel">
        <div class="panel-head"><h2>近期重点关注</h2><button class="link-btn" data-page-link="data">去洞察台 →</button></div>
        <div class="empty-state">AI 正在分析…暂无重点关注资产</div>
      </section>
    `;
  }
  return `
    <section class="panel focus-mini-panel">
      <div class="panel-head">
        <h2>近期重点关注 <small>基于历史巡检与趋势</small></h2>
        <button class="link-btn" data-page-link="data">去洞察台看全部 →</button>
      </div>
      <div class="focus-mini-grid">
        ${items.map((it, idx) => `
          <article class="focus-mini focus-${it.riskLevel}" data-asset-select="${escapeHTML(it.assetId)}">
            <div class="focus-mini-rank">${idx + 1}</div>
            <div class="focus-mini-body">
              <b class="focus-mini-name">${escapeHTML(it.assetName || "—")}</b>
              <div class="focus-mini-meta">
                <span class="focus-mini-score">${it.riskScore} 分</span>
                <span class="status ${it.riskLevel || "warning"}">${escapeHTML(riskAttentionLabel(it.riskLevel))}</span>
              </div>
              <div class="focus-mini-reason">${escapeHTML((it.reasons && it.reasons[0]) || "—")}</div>
            </div>
          </article>
        `).join("")}
      </div>
    </section>
  `;
}

const AI_SUGGESTIONS = [
  "最近 30 天有哪些设备需要重点关注？",
  "本周异常比上周增加了吗？",
  "无机房电梯最近有哪些重复风险？",
  "复核率怎么样？有谁没看图就确认？",
  "目前有哪些待复核记录需要处理？",
  "今天应该优先处理什么？",
];

function aiChatPanel() {
  return `
    <section class="panel ai-chat">
      <div class="panel-head">
        <h2><img class="hi" src="./assets/ai-spark.svg" alt="">AI 智能问答</h2>
        <span class="ai-chat-model">DeepSeek-V4 · 台账分析</span>
      </div>
      <div class="ai-chat-body" id="aiChatBody">
        <div class="ai-chat-empty">问 AI 看数据、查异常、要建议；下方点话题即可发起对话。</div>
      </div>
      <div class="ai-chat-suggestions" id="aiChatSuggestions">
        ${AI_SUGGESTIONS.map((s) => `<button type="button" class="ai-chat-chip" data-ai-suggest="${escapeHTML(s)}">${escapeHTML(s)}</button>`).join("")}
      </div>
      <form class="ai-chat-form" id="aiChatForm" autocomplete="off">
        <input id="aiChatInput" type="text" placeholder="输入你想问的，例如『派给我哪些任务还没完成』" maxlength="200" />
        <button type="submit" class="ai-chat-send" id="aiChatSend">发送</button>
      </form>
    </section>
  `;
}

const AI_CHAT_STATE = { history: [], busy: false };

function bindAiChat() {
  const form = document.getElementById("aiChatForm");
  if (!form) return;
  const body = document.getElementById("aiChatBody");
  const input = document.getElementById("aiChatInput");
  const sendBtn = document.getElementById("aiChatSend");
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    const text = input.value.trim();
    if (!text || AI_CHAT_STATE.busy) return;
    input.value = "";
    sendAiChat(text);
  });
  document.querySelectorAll("[data-ai-suggest]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const t = btn.dataset.aiSuggest;
      if (!t || AI_CHAT_STATE.busy) return;
      sendAiChat(t);
    });
  });
  // restore history on re-render
  if (AI_CHAT_STATE.history.length) {
    body.innerHTML = AI_CHAT_STATE.history.map(renderChatBubble).join("");
    body.scrollTop = body.scrollHeight;
  }
}

function renderChatBubble(m) {
  const cls = m.role === "user" ? "user" : "ai";
  const html = (m.text || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\n/g, "<br>");
  return `<div class="ai-chat-msg ${cls}"><div class="ai-chat-bubble">${html}</div></div>`;
}

async function sendAiChat(message) {
  const body = document.getElementById("aiChatBody");
  const sendBtn = document.getElementById("aiChatSend");
  if (!body) return;
  // remove empty placeholder
  body.querySelector(".ai-chat-empty")?.remove();
  AI_CHAT_STATE.busy = true;
  sendBtn.disabled = true;
  sendBtn.textContent = "思考中…";

  AI_CHAT_STATE.history.push({ role: "user", text: message });
  body.insertAdjacentHTML("beforeend", renderChatBubble({ role: "user", text: message }));
  // typing placeholder
  body.insertAdjacentHTML("beforeend", `<div class="ai-chat-msg ai" id="aiChatTyping"><div class="ai-chat-bubble typing"><i></i><i></i><i></i></div></div>`);
  body.scrollTop = body.scrollHeight;

  try {
    const res = await api("/api/management-ai/chat", {
      method: "POST",
      body: JSON.stringify({
        message,
        history: AI_CHAT_STATE.history.slice(-6),
        project: state.selectedProject || "",
        range: "30d",
      }),
    });
    const reply = res.reply || "AI 没有给出回复。";
    AI_CHAT_STATE.history.push({ role: "ai", text: reply });
    document.getElementById("aiChatTyping")?.remove();
    body.insertAdjacentHTML("beforeend", renderChatBubble({ role: "ai", text: reply }));
  } catch (err) {
    document.getElementById("aiChatTyping")?.remove();
    body.insertAdjacentHTML("beforeend", renderChatBubble({ role: "ai", text: `出错了：${err.message || "未知错误"}` }));
  } finally {
    AI_CHAT_STATE.busy = false;
    sendBtn.disabled = false;
    sendBtn.textContent = "发送";
    body.scrollTop = body.scrollHeight;
  }
}

function animateDashboardCounts() {
  document.querySelectorAll(".dash-anim").forEach((el) => {
    const target = parseFloat(el.dataset.count);
    if (!isFinite(target)) return;
    const decimals = parseInt(el.dataset.decimals || "0", 10);
    const suffix = el.dataset.suffix || "";
    const dur = 800;
    const t0 = performance.now();
    function tick(t) {
      const k = Math.min(1, (t - t0) / dur);
      const eased = 1 - Math.pow(1 - k, 3);
      el.textContent = (target * eased).toFixed(decimals) + suffix;
      if (k < 1) requestAnimationFrame(tick);
    }
    requestAnimationFrame(tick);
  });
}

function animateHealthRings() {
  document.querySelectorAll(".health-ring .ring-svg circle:last-child").forEach((el) => {
    const len = parseFloat(getComputedStyle(el).getPropertyValue("--ring-len")) || 0;
    el.style.transition = "stroke-dasharray 900ms cubic-bezier(.4,0,.2,1) 200ms";
    requestAnimationFrame(() => {
      el.style.strokeDasharray = `${len} 999`;
    });
  });
}

function openAdminLightbox(src) {
  if (!src) return;
  closeAdminLightbox();
  const box = document.createElement("div");
  box.className = "admin-lightbox";
  box.innerHTML = `<button class="admin-lightbox-close" aria-label="关闭">×</button><img src="${src}" alt="" />`;
  document.body.appendChild(box);
  document.addEventListener("keydown", lightboxKeyHandler);
}
function closeAdminLightbox() {
  document.querySelectorAll(".admin-lightbox").forEach((el) => el.remove());
  document.removeEventListener("keydown", lightboxKeyHandler);
}
function lightboxKeyHandler(e) {
  if (e.key === "Escape") closeAdminLightbox();
}

const HELLO = (() => {
  const h = new Date().getHours();
  if (h < 6) return "深夜了";
  if (h < 11) return "早上好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
})();

function last7DayAbnormal(records) {
  const days = Array.from({ length: 7 }, (_, i) => {
    const d = new Date(); d.setDate(d.getDate() - (6 - i));
    return d.toISOString().slice(0, 10);
  });
  return days.map((day) => records.filter((r) => String(r.createdAt || "").slice(0, 10) === day && recordLevel(r) !== "normal").length);
}

function aiHeroBanner({ todayRecords, todayAuto, todayFlagged, savedHours, issuesCount, accuracy, trendCounts, todayTaskDone = 0 }) {
  const accText = accuracy && accuracy.sample ? `${accuracy.value}%` : "—";
  // sparkline: 7 day abnormal trend
  const w = 168, h = 44;
  const max = Math.max(1, ...(trendCounts || [0]));
  const pts = (trendCounts || []).map((v, i) => {
    const x = 4 + (i * (w - 8)) / 6;
    const y = h - 6 - (v / max) * (h - 14);
    return `${x},${y}`;
  }).join(" ");
  return `
    <section class="ai-hero ai-hero-v2">
      <div class="ai-hero-text">
        <div class="ai-hero-hi">${HELLO}，张管理员</div>
        <div class="ai-hero-line">
          今日 AI 交付
          <b><span class="dash-anim" data-count="${todayRecords}">0</span></b><em>条巡检</em>
          <b><span class="dash-anim" data-count="${issuesCount}">0</span></b><em>项异常</em>
          <b><span class="dash-anim" data-count="${todayTaskDone}">0</span></b><em>项完成</em>
        </div>
        <div class="ai-hero-sub">其中 ${todayAuto} 条 AI 自动确认 · ${todayTaskDone} 个任务今日已闭环</div>
      </div>
      <aside class="ai-hero-side" aria-hidden="true">
        <span class="ai-hero-orb ai-hero-orb-1"></span>
        <span class="ai-hero-orb ai-hero-orb-2"></span>
        <span class="ai-hero-orb ai-hero-orb-3"></span>
      </aside>
      <svg class="ai-hero-cursor" viewBox="0 0 28 28" aria-hidden="true">
        <defs>
          <linearGradient id="heroTriGrad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stop-color="#7effd2"/>
            <stop offset="100%" stop-color="#25d5ff"/>
          </linearGradient>
        </defs>
        <polygon points="14,3 25,24 3,24" fill="url(#heroTriGrad)" opacity="0.85"/>
      </svg>
    </section>
  `;
}

function healthCards(counts) {
  const total = counts.total || 1;
  const items = [
    { key: "normal", label: "正常资产", count: counts.normal, pct: Math.round((counts.normal / total) * 100), color: "#12a968", desc: "AI 判定运行正常" },
    { key: "warn",   label: "需关注",   count: counts.warning + counts.danger, pct: Math.round(((counts.warning + counts.danger) / total) * 100), color: "#f59e0b", desc: "AI 检出异常或建议复核" },
    { key: "repair", label: "待维修",   count: counts.repair, pct: Math.round((counts.repair / total) * 100), color: "#ef4b3f", desc: "需现场处理" },
  ];
  const ring = (pct, color) => {
    const r = 28, c = 2 * Math.PI * r;
    const len = (pct / 100) * c;
    return `<svg viewBox="0 0 72 72" class="ring-svg">
      <circle cx="36" cy="36" r="${r}" fill="none" stroke="rgba(15,35,55,0.06)" stroke-width="6"/>
      <circle cx="36" cy="36" r="${r}" fill="none" stroke="${color}" stroke-width="6"
        stroke-linecap="round" transform="rotate(-90 36 36)"
        style="--ring-len:${len.toFixed(2)}; stroke-dasharray:0 999;"/>
    </svg>`;
  };
  return `
    <section class="health-grid">
      ${items.map((it) => `
        <article class="health-card health-${it.key}">
          <div class="health-bar" style="background:${it.color}"></div>
          <div class="health-body">
            <div class="health-ring">${ring(it.pct, it.color)}<b><span class="dash-anim" data-count="${it.pct}" data-suffix="%">0%</span></b></div>
            <div class="health-info">
              <span class="health-label">${it.label}</span>
              <b class="health-count"><span class="dash-anim" data-count="${it.count}">0</span> <em>项</em></b>
              <span class="health-desc">${it.desc}</span>
            </div>
          </div>
        </article>
      `).join("")}
    </section>
  `;
}

function topIssues(assets) {
  const PRI = { danger: "high", repair: "high", warning: "mid" };
  // F3 修复 · 真按 status / lastStatus / assetType 拼提示，不再用 adjectives 假数组
  const STATUS_HINT = {
    danger: "AI 标记为异常状态",
    warning: "AI 提交待复核",
    repair: "AI 标记为待维修",
  };
  return assets
    .filter((a) => ["danger", "warning", "repair"].includes(normalizeStatus(a.lastStatus)))
    .slice(0, 4)
    .map((a) => {
      const lvl = normalizeStatus(a.lastStatus);
      const hint = STATUS_HINT[lvl] || "AI 检出待处理项";
      const typeText = a.assetType ? `（${a.assetType}）` : "";
      const note = a.lastSummary
        ? a.lastSummary
        : `${hint}${typeText}，最近巡检 ${fmtTime(a.lastInspectedAt) || "—"}，请尽快复核`;
      return {
        asset: a,
        priority: PRI[lvl] || "low",
        aiNote: note,
      };
    });
}

function aiFindingsPanel(issues) {
  if (!issues.length) {
    return `
      <section class="panel ai-findings">
        <div class="panel-head"><h2><img class="hi" src="./assets/ai-spark.svg" alt="">AI 关键发现</h2></div>
        <div class="ai-finding-empty">😊 今日所有资产 AI 判定正常，没有需要你关注的事项</div>
      </section>`;
  }
  const labelMap = { high: "高优先级", mid: "中优先级", low: "低优先级" };
  return `
    <section class="panel ai-findings">
      <div class="panel-head">
        <h2><img class="hi" src="./assets/ai-spark.svg" alt="">AI 关键发现</h2>
        <button class="link-btn" data-page-link="exception">全部 →</button>
      </div>
      <div class="finding-list">
        ${issues.map((it) => `
          <article class="finding finding-${it.priority}" data-asset-select="${escapeHTML(it.asset.id)}">
            <span class="finding-bar"></span>
            <div class="finding-body">
              <div class="finding-head">
                <span class="finding-pri">${labelMap[it.priority]}</span>
                <h3>${escapeHTML(it.asset.assetName || "资产")}</h3>
              </div>
              <p class="finding-note"><span class="ai-tag">AI 分析</span>${escapeHTML(it.aiNote)}</p>
            </div>
            <button class="finding-cta" data-asset-select="${escapeHTML(it.asset.id)}">去处理 →</button>
          </article>
        `).join("")}
      </div>
    </section>
  `;
}

function riskInsights(records, assets) {
  const counts = statusCounts(assets);
  const total = assets.length || 1;
  const abnormalCount = counts.warning + counts.danger;
  const abnormalRate = Math.round((abnormalCount / total) * 100);

  // F2 修复 · 真算本周 vs 上周异常记录数环比
  const now = Date.now();
  const dayMs = 24 * 3600 * 1000;
  const isAbnormal = (r) => recordLevel(r) !== "normal";
  const ts = (r) => new Date(r.createdAt || r.submittedAt || 0).getTime();
  const thisWeek = records.filter((r) => isAbnormal(r) && ts(r) > now - 7 * dayMs).length;
  const lastWeek = records.filter((r) => {
    const t = ts(r);
    return isAbnormal(r) && t > now - 14 * dayMs && t <= now - 7 * dayMs;
  }).length;
  let trendDelta;
  if (lastWeek > 0) {
    const diff = Math.round(((thisWeek - lastWeek) / lastWeek) * 100);
    if (diff === 0) trendDelta = "，与上周持平";
    else if (diff > 0) trendDelta = `，较上周上升 <b>${diff}%</b>`;
    else trendDelta = `，较上周下降 <b>${Math.abs(diff)}%</b>`;
  } else if (thisWeek > 0) {
    trendDelta = "，上周无异常基线，不计算环比";
  } else {
    trendDelta = "，连续两周无异常记录";
  }

  const typeMap = {};
  const GENERIC = /综合|未分类|其它|其他|通用|默认|default|other/i;
  assets.filter((a) => normalizeStatus(a.lastStatus) !== "normal").forEach((a) => {
    let key = a.assetType || "";
    if (!key || GENERIC.test(key)) {
      key = a.assetName ? a.assetName.replace(/[_\d#·\-\s]+\d*$/, "") || a.assetName : "";
    }
    if (!key) return;
    typeMap[key] = (typeMap[key] || 0) + 1;
  });
  const topType = Object.entries(typeMap).sort((a, b) => b[1] - a[1])[0];
  const focusType = topType ? topType[0] : "";
  const focusPct = topType ? Math.round((topType[1] / (abnormalCount || 1)) * 100) : 0;
  return {
    trendText: `异常率 <b>${abnormalRate}%</b>${trendDelta}`,
    focusText: abnormalCount && focusType
      ? `${focusType} 类异常占比 <b>${focusPct}%</b>，是本周重点关注对象`
      : `本周未发现需重点关注的异常资产`,
    suggestText: abnormalCount && focusType
      ? `建议加强「${focusType}」类资产的巡检频次，其余类目可维持当前节奏`
      : `各类资产状态稳定，可按既定计划维持当前巡检节奏`,
  };
}

function aiRiskReport(insights) {
  const time = new Date().toTimeString().slice(0, 5);
  return `
    <section class="panel ai-risk">
      <div class="ai-risk-side">
        <img src="./assets/ai-brain.svg" alt="" class="ai-brain" />
        <span class="ai-risk-label">AI 研判</span>
      </div>
      <div class="ai-risk-body">
        <div class="ai-risk-head">
          <h2>AI 风险研判</h2>
          <span class="ai-risk-time">本周报告 · 生成于 ${time}</span>
        </div>
        <div class="ai-risk-row">
          <img src="./assets/icon-trend.svg" alt="" />
          <div><span>整体趋势</span><p>${insights.trendText}</p></div>
        </div>
        <div class="ai-risk-row">
          <img src="./assets/icon-target.svg" alt="" />
          <div><span>重点关注</span><p>${insights.focusText}</p></div>
        </div>
        <div class="ai-risk-row">
          <img src="./assets/icon-bulb.svg" alt="" />
          <div><span>AI 建议</span><p>${insights.suggestText}</p></div>
        </div>
      </div>
    </section>
  `;
}

function quickAccessCounts(records) {
  return {
    plans: planRows().length,
    tasks: (typeof taskGroups === "function" ? taskGroups() : { pending: [] }).pending?.length || state.tasks?.filter?.((t) => t.status !== "completed").length || 0,
    records: records.length,
    assets: state.assets.length,
    devices: new Set(state.assets.map((a) => a.assetType).filter(Boolean)).size,
  };
}

function quickAccessTiles(c) {
  const tiles = [
    { key: "plan",   label: "巡检计划", num: c.plans,   unit: "项启用", sub: "巡检与配置", page: "plan",   icon: "icon-plan.svg",   color: "#4f7cff" },
    { key: "task",   label: "派发任务", num: c.tasks,   unit: "项待派", sub: "今日待派发", page: "task",   icon: "icon-task.svg",   color: "#12a968" },
    { key: "record", label: "巡检记录", num: c.records, unit: "条记录", sub: "本周累计",   page: "record", icon: "icon-record.svg", color: "#f59e0b" },
    { key: "asset",  label: "资产台账", num: c.assets,  unit: "项资产", sub: "全量资产",   page: "ledger", icon: "icon-asset.svg",  color: "#8b5cf6" },
    { key: "device", label: "设备管理", num: c.devices, unit: "类设备", sub: "设备类型",   page: "device", icon: "icon-device.svg", color: "#0ea5e9" },
    { key: "board",  label: "数据看板", num: null,       unit: "", sub: "报表与分析", page: "data",   icon: "icon-board.svg",  color: "#06b6d4" },
  ];
  return `
    <section class="panel quick-tiles-panel">
      <div class="panel-head"><h2>巡检管理 · 快速入口</h2></div>
      <div class="quick-tiles">
        ${tiles.map((t) => `
          <button class="quick-tile" data-page-link="${t.page}" style="--tile-color: ${t.color}; --tile-color-bg: ${t.color}1a; --tile-color-border: ${t.color}33; --tile-color-shadow: ${t.color}26;">
            <div class="qt-left">
              <span class="qt-ico"><span class="qt-ico-glyph" style="--icon-url: url('./assets/${t.icon}')"></span></span>
              <span class="qt-title">${t.label}</span>
            </div>
            <div class="qt-right">
              ${t.num != null ? `<b class="qt-num">${t.num}</b><em class="qt-unit">${t.unit}</em>` : `<span class="qt-arrow-only">→</span>`}
            </div>
          </button>
        `).join("")}
      </div>
    </section>
  `;
}

function monthlyAiStats() {
  const records = state.records || [];
  const month = new Date().toISOString().slice(0, 7);
  const monthly = records.filter((r) => String(r.createdAt || "").slice(0, 7) === month);
  const abnormal = monthly.filter((r) => recordLevel(r) !== "normal").length;
  const accuracy = aiAccuracy(monthly);
  const savedHours = Math.round((monthly.length - abnormal) * 0.07 * 10) / 10;
  return {
    processed: monthly.length || records.length,
    abnormal,
    savedHours,
    accuracy: accuracy.sample ? accuracy.value : null,
  };
}

function dashboardAside({ monthly, pendingExceptions, pendingApprovals, pendingTasks, accuracy, issues, attentionItems }) {
  const accText = accuracy.sample ? `${accuracy.value}%` : "—";
  return `
    <div class="aside-stack">
      <div class="ai-month-card">
        <div class="month-head">
          <div><b>AI 本月成绩</b><span>来自巡检与识别累计</span></div>
        </div>
        <div class="month-rows">
          <div><span>处理巡检</span><b><span class="dash-anim" data-count="${monthly.processed}">0</span></b></div>
          <div><span>发现异常</span><b><span class="dash-anim" data-count="${monthly.abnormal}">0</span></b></div>
          <div><span>识别准确</span><b>${accText}</b></div>
        </div>
      </div>
      <div class="todo-card">
        <div class="todo-head"><b>你的待办</b></div>
        <a class="todo-row danger"  data-page-link="exception">
          <img src="./assets/icon-warning.svg" alt=""/>
          <span>异常复核</span><b>${pendingExceptions}</b><em>立即处理 →</em>
        </a>
        <a class="todo-row warn" data-page-link="approval">
          <img src="./assets/icon-bulb.svg" alt=""/>
          <span>修改审批</span><b>${pendingApprovals}</b><em>去审批 →</em>
        </a>
        <a class="todo-row info"  data-page-link="task">
          <img src="./assets/icon-task.svg" alt=""/>
          <span>巡检任务</span><b>${pendingTasks}</b><em>查看 →</em>
        </a>
      </div>
      ${asideFindings(issues || [], attentionItems || [])}
    </div>
  `;
}

function riskAttentionLabel(level) {
  return level === "danger" ? "高风险" : (level === "warning" ? "需关注" : "低风险");
}

function asideFindings(issues, attentionItems = []) {
  const PRI_LABEL = { high: "高", mid: "中", low: "低" };
  if (issues && issues.length > 0) {
    return `
      <div class="aside-findings">
        <div class="aside-findings-head"><b>实时异常</b><span>当前状态</span></div>
        <ul class="aside-findings-list">
          ${issues.slice(0, 4).map((it) => `
            <li class="aside-finding f-${it.priority}" data-asset-select="${escapeHTML(it.asset.id)}">
              <span class="aside-finding-pri">${PRI_LABEL[it.priority] || "·"}</span>
              <div class="aside-finding-body">
                <b>${escapeHTML(it.asset.assetName || "资产")}</b>
                <span>${escapeHTML(String(it.aiNote || "").slice(0, 36))}${(it.aiNote || "").length > 36 ? "…" : ""}</span>
              </div>
              <em>→</em>
            </li>
          `).join("")}
        </ul>
      </div>
    `;
  }
  if (attentionItems.length > 0) {
    return `
      <div class="aside-findings attention">
        <div class="aside-findings-head"><b>历史风险提醒</b><span>AI 趋势</span></div>
        <ul class="aside-findings-list">
          ${attentionItems.slice(0, 4).map((it) => {
            const pri = it.riskLevel === "danger" ? "high" : (it.riskLevel === "warning" ? "mid" : "low");
            return `
              <li class="aside-finding f-${pri}" data-asset-select="${escapeHTML(it.assetId)}">
                <span class="aside-finding-pri">${PRI_LABEL[pri]}</span>
                <div class="aside-finding-body">
                  <b>${escapeHTML(it.assetName || "资产")}</b>
                  <span>${escapeHTML((it.reasons && it.reasons[0]) || "建议查看历史巡检变化")}</span>
                </div>
                <em>${it.riskScore}</em>
              </li>
            `;
          }).join("")}
        </ul>
      </div>
    `;
  }
  if (!issues || issues.length === 0) {
    return `
      <div class="aside-findings empty">
        <div class="aside-findings-head"><b>AI 关键发现</b></div>
        <div class="aside-findings-empty">当前资产状态正常，暂无实时异常或历史风险提醒。</div>
      </div>`;
  }
  return "";
}

function renderPlanPage() {
  const rows = filteredPlanRows();
  const todayPlans = rows.filter((row) => row.frequency.includes("每日"));
  if (!state.selectedPlanId || !rows.some((r) => r.id === state.selectedPlanId)) {
    state.selectedPlanId = rows[0]?.id || "";
  }
  $("#pageMain").innerHTML = `
    ${metrics([
      { label: "计划数量", value: rows.length, sub: "来自点位与模板" },
      { label: "启用计划", value: rows.filter((row) => row.status === "启用").length, sub: "当前有效", good: true },
      { label: "今日执行", value: todayPlans.length, sub: "每日计划" },
      { label: "AI 模板", value: state.templates.filter((tpl) => tpl.hasAI).length, sub: "已配置视觉识别", good: true },
    ])}
    <section class="panel plan-table-panel">
      <div class="panel-head plan-panel-head">
        <div class="panel-title-block"><h2>巡检计划</h2><p>点击行可在右侧编辑该计划。</p></div>
        <div class="panel-actions plan-toolbar">${planFiltersHTML()}<button data-drawer="plan">新建计划</button></div>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>计划名称</th><th>项目</th><th>巡检点位</th><th>频次</th><th>责任人</th><th>下次执行</th><th>状态</th></tr></thead>
          <tbody>${rows.map((row) => `
            <tr class="plan-row ${row.id === state.selectedPlanId ? "selected" : ""}" data-plan-id="${escapeHTML(row.id)}">
              <td>${escapeHTML(row.name)}</td><td>${escapeHTML(row.project)}</td><td>${escapeHTML(row.point)}</td>
              <td>${escapeHTML(row.frequency)}</td><td>${escapeHTML(row.owner)}</td><td>${escapeHTML(row.next)}</td>
              <td><span class="status ${statusClass(row.status === "启用" ? "正常" : row.status)}">${escapeHTML(row.status)}</span></td>
            </tr>
          `).join("") || emptyRow(7, "暂无巡检计划")}</tbody>
        </table>
      </div>
    </section>
  `;
  const selected = rows.find((r) => r.id === state.selectedPlanId) || null;
  $("#pageAside").innerHTML = asideStack(planEditCard(selected));
  bindPlanFilters();
  bindPlanRowClicks();
  bindPlanEditForm();
}

function bindPlanRowClicks() {
  document.querySelectorAll(".plan-row").forEach((tr) => {
    tr.addEventListener("click", () => {
      state.selectedPlanId = tr.dataset.planId;
      render();
    });
  });
}

function planEditCard(plan) {
  if (!plan) {
    return `
      <div class="plan-edit-card empty">
        <div class="plan-edit-head"><b>计划详情</b></div>
        <div class="plan-edit-empty">点击左侧计划行查看 / 修改</div>
      </div>`;
  }
  const isSeed = String(plan.id).startsWith("seed_");
  const freqOptions = ["每日 09:00", "每日 14:00", "每周一", "每周一 09:00", "每月 1 日", "每月 15 日", "自定义"];
  const statusOptions = ["启用", "暂停", "未配置"];
  const aiOptions = ["视觉识别 + 人工确认", "视觉识别 自动写入", "纯人工填报"];
  return `
    <div class="plan-edit-card">
      <div class="plan-edit-head">
        <b>${isSeed ? "新建计划" : "编辑计划"}</b>
        <span class="plan-edit-tag ${isSeed ? "seed" : "custom"}">${isSeed ? "未配置" : "已配置"}</span>
      </div>
      ${plan.lastCompletedAt ? `
        <div class="plan-last-done">
          <span class="pld-icon">✓</span>
          <div>
            <b>${escapeHTML(plan.lastCompletedBy || "—")} 已执行</b>
            <span>${escapeHTML(relativeTime(plan.lastCompletedAt))} · ${escapeHTML(fmtTime(plan.lastCompletedAt))}</span>
          </div>
        </div>
      ` : ""}
      <form id="planEditForm" class="plan-edit-form" data-plan-id="${escapeHTML(plan.id)}">
        <label class="pe-field">
          <span>计划名称</span>
          <input name="name" value="${escapeHTML(plan.name)}" required />
        </label>
        <div class="pe-row">
          <label class="pe-field">
            <span>项目</span>
            <input name="project" value="${escapeHTML(plan.project)}" readonly />
          </label>
          <label class="pe-field">
            <span>点位</span>
            <input name="point" value="${escapeHTML(plan.point)}" readonly />
          </label>
        </div>
        <label class="pe-field">
          <span>模板</span>
          <input name="templateName" value="${escapeHTML(plan.templateName || plan.name)}" readonly />
        </label>
        <div class="pe-row">
          <label class="pe-field">
            <span>频次</span>
            <select name="frequency">
              ${freqOptions.map((f) => `<option value="${escapeHTML(f)}" ${plan.frequency === f ? "selected" : ""}>${escapeHTML(f)}</option>`).join("")}
              ${plan.frequency && !freqOptions.includes(plan.frequency) ? `<option value="${escapeHTML(plan.frequency)}" selected>${escapeHTML(plan.frequency)}</option>` : ""}
            </select>
          </label>
          <label class="pe-field">
            <span>状态</span>
            <select name="status">
              ${statusOptions.map((s) => `<option value="${s}" ${plan.status === s ? "selected" : ""}>${s}</option>`).join("")}
            </select>
          </label>
        </div>
        <div class="pe-row">
          <label class="pe-field">
            <span>责任人</span>
            <select name="owner">
              <option value="">未指派</option>
              ${(state.users || []).map((u) => {
                const display = u.displayName || u.username;
                const isSelected = plan.owner === display || plan.owner === u.username;
                return `<option value="${escapeHTML(display)}" ${isSelected ? "selected" : ""}>${escapeHTML(display)}（${escapeHTML(u.roleName || u.roleCode || "")}）</option>`;
              }).join("")}
              ${plan.owner && plan.owner !== "-" && !(state.users || []).some((u) => (u.displayName || u.username) === plan.owner) ? `<option value="${escapeHTML(plan.owner)}" selected>${escapeHTML(plan.owner)}（自定义）</option>` : ""}
            </select>
          </label>
          <label class="pe-field">
            <span>下次执行</span>
            <input name="nextRun" value="${escapeHTML(plan.next === "-" ? "" : plan.next)}" placeholder="如：明日 09:00" />
          </label>
        </div>
        <label class="pe-field">
          <span>AI 策略</span>
          <select name="aiPolicy">
            ${aiOptions.map((a) => `<option value="${escapeHTML(a)}" ${plan.aiPolicy === a ? "selected" : ""}>${escapeHTML(a)}</option>`).join("")}
          </select>
        </label>
        <label class="pe-field">
          <span>说明</span>
          <textarea name="remark" rows="2" placeholder="现场要求 / 拍照标准 / 异常处理口径">${escapeHTML(plan.remark || (isSeed ? "" : plan.desc || ""))}</textarea>
        </label>
        <div class="pe-actions">
          ${!isSeed ? `<button type="button" class="pe-delete" id="planEditDeleteBtn">删除</button>` : ""}
          <button type="submit" class="pe-save">${isSeed ? "启用计划" : "保存修改"}</button>
        </div>
      </form>
    </div>
  `;
}

function bindPlanEditForm() {
  const form = document.getElementById("planEditForm");
  if (!form) return;
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    savePlanFromAside(form);
  });
  document.getElementById("planEditDeleteBtn")?.addEventListener("click", () => {
    if (!confirm("确认删除该计划？")) return;
    const id = form.dataset.planId;
    state.customPlans = state.customPlans.filter((p) => p.id !== id);
    saveLocalArray(ADMIN_PLANS_KEY, state.customPlans);
    state.selectedPlanId = "";
    toast("计划已删除");
    render();
  });
}

function savePlanFromAside(form) {
  const data = Object.fromEntries(new FormData(form).entries());
  const id = form.dataset.planId;
  const isSeed = String(id).startsWith("seed_");
  const rows = filteredPlanRows();
  const src = rows.find((r) => r.id === id) || {};
  const payload = {
    id: isSeed ? `plan_${Date.now()}_${Math.random().toString(36).slice(2, 6)}` : id,
    name: (data.name || "").trim() || src.name,
    project: src.project,
    pointId: src.pointId || "",
    pointName: src.point,
    templateId: src.templateId || "",
    templateName: src.templateName || src.name,
    frequency: data.frequency || "每日 09:00",
    owner: (data.owner || "").trim() || "-",
    nextRun: (data.nextRun || "").trim() || "-",
    status: data.status || "启用",
    aiPolicy: data.aiPolicy || "视觉识别 + 人工确认",
    remark: (data.remark || "").trim(),
    createdAt: isSeed ? new Date().toISOString() : (src.createdAt || new Date().toISOString()),
    updatedAt: new Date().toISOString(),
  };
  if (isSeed) {
    state.customPlans.unshift(payload);
    toast("计划已启用");
  } else {
    const idx = state.customPlans.findIndex((p) => p.id === id);
    if (idx >= 0) {
      state.customPlans[idx] = payload;
      toast("计划已保存");
    } else {
      state.customPlans.unshift(payload);
      toast("计划已新增");
    }
  }
  saveLocalArray(ADMIN_PLANS_KEY, state.customPlans);
  syncPlanTask(payload);
  state.selectedPlanId = payload.id;
  updateUserBadge();
  render();
}

// M30 · 计划 -> 任务 双向联动
function syncPlanTask(plan) {
  const taskId = `task_from_${plan.id}`;
  const idx = (state.customTasks || []).findIndex((t) => t.id === taskId);
  if (plan.status !== "启用") {
    // 计划暂停/未配置 → 删除自动生成的任务（手工建的不动）
    if (idx >= 0) {
      state.customTasks.splice(idx, 1);
      saveLocalArray(ADMIN_TASKS_KEY, state.customTasks);
    }
    return;
  }
  const existing = idx >= 0 ? state.customTasks[idx] : null;
  const task = {
    id: taskId,
    title: plan.name,
    planId: plan.id,
    planName: plan.name,
    project: plan.project,
    pointId: plan.pointId,
    pointName: plan.pointName,
    templateId: plan.templateId,
    templateName: plan.templateName || plan.name,
    owner: plan.owner,
    frequency: plan.frequency,
    dueAt: plan.nextRun || "—",
    aiPolicy: plan.aiPolicy,
    remark: plan.remark,
    priority: "普通",
    source: "plan",
    // ↓ 保留任务进度相关字段，不被 plan 同步覆盖
    status: existing?.status || "待执行",
    createdAt: existing?.createdAt || new Date().toISOString(),
    completedAt: existing?.completedAt || null,
    completedBy: existing?.completedBy || null,
    updatedAt: new Date().toISOString(),
  };
  if (idx >= 0) {
    state.customTasks[idx] = task;
  } else {
    state.customTasks.unshift(task);
  }
  saveLocalArray(ADMIN_TASKS_KEY, state.customTasks);
}

function planRows() {
  const templateById = new Map(state.templates.map((tpl) => [tpl.id, tpl]));
  const customRows = (state.customPlans || [])
    .filter((plan) => !state.selectedProject || plan.project === state.selectedProject)
    .map((plan) => ({
      id: plan.id,
      name: plan.name,
      project: plan.project || "-",
      point: plan.pointName || "-",
      pointId: plan.pointId || "",
      templateId: plan.templateId || "",
      templateName: plan.templateName || "",
      frequency: plan.frequency || "-",
      owner: plan.owner || "-",
      next: plan.nextRun || "-",
      status: plan.status || "启用",
      aiPolicy: plan.aiPolicy || "视觉识别 + 人工确认",
      desc: plan.remark || `${plan.pointName || "点位"}，使用 ${plan.templateName || "日报模板"}。`,
      remark: plan.remark || "",
      lastCompletedAt: plan.lastCompletedAt || null,
      lastCompletedBy: plan.lastCompletedBy || null,
      source: "manual",
    }));
  const source = state.points.length ? state.points : state.templates.map((tpl) => ({
    id: tpl.id,
    project: tpl.project,
    name: tpl.name,
    location: tpl.assetType,
    templateId: tpl.id,
  }));
  // F4 修复 · seedRows 是基于真 points+templates 的"未配置"占位计划，
  // 频次/责任人/下次执行没有真实排程，统一显示 "-" 并标 status="未配置"
  const seedRows = source
    .filter((point) => !state.selectedProject || point.project === state.selectedProject)
    .map((point) => {
      const tpl = templateById.get(point.templateId) || {};
      return {
        id: `seed_${point.id || point.templateId || point.name}`,
        name: tpl.name || point.name || "巡检计划",
        project: point.project || tpl.project || "-",
        point: point.name || point.location || "-",
        pointId: point.id || "",
        templateId: tpl.id || point.templateId || "",
        templateName: tpl.name || "",
        frequency: "-",
        owner: "-",
        next: "-",
        status: "未配置",
        aiPolicy: "视觉识别 + 人工确认",
        desc: `${point.location || point.name || "点位"}，使用 ${tpl.name || "默认"} 模板。如需启用，请在「新建计划」中补全责任人与频次。`,
        source: "seed",
      };
    });
  return [...customRows, ...seedRows];
}

function planSearchText(row) {
  return [
    row.id,
    row.name,
    row.project,
    row.point,
    row.pointId,
    row.templateId,
    row.templateName,
    row.frequency,
    row.owner,
    row.next,
    row.status,
    row.aiPolicy,
    row.desc,
  ].filter(Boolean).join(" ").toLowerCase();
}

function filteredPlanRows() {
  const rows = planRows();
  const filters = state.planFilters;
  const project = filters.project.trim();
  const frequency = filters.frequency.trim();
  const status = filters.status.trim();
  const keyword = filters.keyword.trim().toLowerCase();
  return rows.filter((row) => {
    if (project && row.project !== project) return false;
    if (frequency && row.frequency !== frequency && !row.frequency.includes(frequency)) return false;
    if (status && row.status !== status) return false;
    if (!keyword) return true;
    return planSearchText(row).includes(keyword);
  });
}

function planFiltersHTML() {
  const rows = planRows();
  const projectOptions = [...new Set([...projects(), ...rows.map((row) => row.project)].filter(Boolean))];
  const frequencyOptions = [...new Set(["每日", "每周", "每月", ...rows.map((row) => row.frequency).filter(Boolean)])];
  const statusOptions = [...new Set([...PLAN_STATUS_OPTIONS, ...rows.map((row) => row.status).filter(Boolean)])];
  return `
    <div class="filters">
      <select id="planProjectFilter"><option value="">全部项目</option>${projectOptions.map((project) => `<option value="${escapeHTML(project)}" ${state.planFilters.project === project ? "selected" : ""}>${escapeHTML(project)}</option>`).join("")}</select>
      <select id="planFrequencyFilter"><option value="">全部频次</option>${frequencyOptions.map((frequency) => `<option value="${escapeHTML(frequency)}" ${state.planFilters.frequency === frequency ? "selected" : ""}>${escapeHTML(frequency)}</option>`).join("")}</select>
      <select id="planStatusFilter"><option value="">全部状态</option>${statusOptions.map((status) => `<option value="${escapeHTML(status)}" ${state.planFilters.status === status ? "selected" : ""}>${escapeHTML(status)}</option>`).join("")}</select>
      <input id="planKeywordInput" type="search" placeholder="搜索计划 / 点位" value="${escapeHTML(state.planFilters.keyword)}" />
    </div>
  `;
}

function bindPlanFilters() {
  const refresh = () => renderPlanPage();
  $("#planProjectFilter")?.addEventListener("change", (event) => {
    state.planFilters.project = event.target.value;
    refresh();
  });
  $("#planFrequencyFilter")?.addEventListener("change", (event) => {
    state.planFilters.frequency = event.target.value;
    refresh();
  });
  $("#planStatusFilter")?.addEventListener("change", (event) => {
    state.planFilters.status = event.target.value;
    refresh();
  });
  $("#planKeywordInput")?.addEventListener("input", (event) => {
    state.planFilters.keyword = event.target.value;
    clearTimeout(bindPlanFilters.keywordTimer);
    bindPlanFilters.keywordTimer = setTimeout(() => {
      const cursor = state.planFilters.keyword.length;
      refresh();
      requestAnimationFrame(() => {
        const input = $("#planKeywordInput");
        if (!input) return;
        input.focus();
        input.setSelectionRange(cursor, cursor);
      });
    }, 180);
  });
}

function renderTaskPage() {
  const tasks = taskGroups();
  const allTasks = [...tasks.pending, ...tasks.processing, ...tasks.done, ...tasks.overdue];
  if (!state.selectedTaskId || !allTasks.some((t) => t.id === state.selectedTaskId)) {
    state.selectedTaskId = allTasks[0]?.id || "";
  }
  $("#pageMain").innerHTML = `
    <section class="panel">
      <div class="panel-head">
        <div><h2>巡检任务</h2><p>点击任意任务卡可在右侧查看详情 / 更改状态。</p></div>
        <button data-drawer="task">派发任务</button>
      </div>
      <div class="task-board panel-body-grid">
        ${taskColumn("待执行", tasks.pending, "warning")}
        ${taskColumn("进行中", tasks.processing, "normal")}
        ${taskColumn("已完成", tasks.done, "normal")}
        ${taskColumn("逾期", tasks.overdue, "danger")}
      </div>
    </section>
  `;
  const selected = (state.customTasks || []).find((t) => t.id === state.selectedTaskId) || allTasks.find((t) => t.id === state.selectedTaskId);
  $("#pageAside").innerHTML = asideStack(taskDetailCard(selected));
  bindTaskCardClicks();
  bindTaskDetailActions();
}

function bindTaskCardClicks() {
  document.querySelectorAll(".task-card").forEach((el) => {
    const id = el.dataset.taskId;
    if (!id) return;
    el.classList.add("clickable");
    if (id === state.selectedTaskId) el.classList.add("selected");
    el.addEventListener("click", () => {
      state.selectedTaskId = id;
      render();
    });
  });
}

function taskDetailCard(task) {
  if (!task) {
    return `
      <div class="task-detail-card empty">
        <div class="task-detail-head"><b>任务详情</b></div>
        <div class="task-detail-empty">点击左侧任意任务卡查看详情</div>
      </div>`;
  }
  const isPlanTask = String(task.id).startsWith("task_from_") || task.source === "plan";
  const statusFlow = ["待执行", "进行中", "已完成"];
  const curIdx = statusFlow.indexOf(task.status);
  return `
    <div class="task-detail-card">
      <div class="task-detail-head">
        <div>
          <b>任务详情</b>
          <span class="td-status ${taskStatusTone(task.status)}">${escapeHTML(task.status)}</span>
        </div>
        ${isPlanTask ? `<button class="td-source-link" data-plan-link="${escapeHTML(task.planId || "")}" title="跳到对应计划">← 来自计划</button>` : ""}
      </div>
      <div class="td-title">${escapeHTML(task.title || task.planName || "任务")}</div>
      <div class="td-meta-list">
        ${task.project ? `<div><span>项目</span><b>${escapeHTML(task.project)}</b></div>` : ""}
        ${task.pointName ? `<div><span>点位</span><b>${escapeHTML(task.pointName)}</b></div>` : ""}
        ${task.templateName ? `<div><span>模板</span><b>${escapeHTML(task.templateName)}</b></div>` : ""}
        ${task.owner ? `<div><span>责任人</span><b>${escapeHTML(task.owner)}</b></div>` : ""}
        ${task.frequency ? `<div><span>频次</span><b>${escapeHTML(task.frequency)}</b></div>` : ""}
        ${task.dueAt ? `<div><span>截止</span><b>${escapeHTML(task.dueAt)}</b></div>` : ""}
        ${task.aiPolicy ? `<div><span>AI 策略</span><b>${escapeHTML(task.aiPolicy)}</b></div>` : ""}
      </div>
      ${task.remark ? `<div class="td-remark"><span>说明</span><p>${escapeHTML(task.remark)}</p></div>` : ""}
      ${task.completedAt ? `
        <div class="td-completed">
          <i></i>
          <div>
            <b>${escapeHTML(task.completedBy || "—")} 已完成</b>
            <span>${escapeHTML(fmtTime(task.completedAt))} · ${relativeTime(task.completedAt)}</span>
          </div>
        </div>
      ` : ""}
      <div class="td-flow">
        ${statusFlow.map((s, i) => `<div class="td-step ${i <= curIdx ? "done" : ""} ${i === curIdx ? "current" : ""}"><i></i><span>${s}</span></div>`).join("")}
      </div>
      ${state.customTasks?.some((t) => t.id === task.id) ? `
        <div class="td-actions">
          ${task.status === "待执行" ? `<button class="td-btn primary" data-task-action="start">开始执行</button>` : ""}
          ${task.status === "进行中" ? `<button class="td-btn primary" data-task-action="done">标记完成</button>` : ""}
          ${task.status !== "已完成" ? `<button class="td-btn ghost" data-task-action="cancel">取消任务</button>` : `<button class="td-btn ghost" data-task-action="reopen">重新打开</button>`}
        </div>
      ` : `<div class="td-readonly">来自巡检记录，不可在此修改状态</div>`}
    </div>
  `;
}

function taskStatusTone(status) {
  if (status === "已完成") return "tone-success";
  if (status === "进行中") return "tone-info";
  if (status === "逾期") return "tone-danger";
  return "tone-warning";
}

function bindTaskDetailActions() {
  document.querySelectorAll("[data-task-action]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const id = state.selectedTaskId;
      const action = btn.dataset.taskAction;
      const idx = state.customTasks.findIndex((t) => t.id === id);
      if (idx < 0) return;
      const map = { start: "进行中", done: "已完成", cancel: "已取消", reopen: "待执行" };
      const newStatus = map[action] || state.customTasks[idx].status;
      const task = state.customTasks[idx];
      task.status = newStatus;
      task.updatedAt = new Date().toISOString();
      if (action === "done") {
        task.completedAt = task.updatedAt;
        task.completedBy = state.currentUser?.displayName || state.currentUser?.username || "—";
        // 联动：在计划上记录最近一次完成
        if (task.planId) {
          const planIdx = state.customPlans.findIndex((p) => p.id === task.planId);
          if (planIdx >= 0) {
            state.customPlans[planIdx].lastCompletedAt = task.completedAt;
            state.customPlans[planIdx].lastCompletedBy = task.completedBy;
            saveLocalArray(ADMIN_PLANS_KEY, state.customPlans);
          }
        }
      }
      if (action === "reopen") {
        task.completedAt = null;
        task.completedBy = null;
      }
      saveLocalArray(ADMIN_TASKS_KEY, state.customTasks);
      toast(`任务已${newStatus}`);
      updateUserBadge();
      render();
    });
  });
  document.querySelectorAll("[data-plan-link]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const planId = btn.dataset.planLink;
      if (planId) state.selectedPlanId = planId;
      setPage("plan");
    });
  });
}

function taskGroups() {
  const records = filteredRecords();
  const today = todayKey();
  const manualTasks = (state.customTasks || [])
    .filter((task) => !state.selectedProject || task.project === state.selectedProject)
    .map((task) => ({
      title: task.title || task.planName || "临时巡检任务",
      meta: `${task.project || "-"} · ${task.pointName || "-"} · ${task.owner || "-"} · 截止 ${task.dueAt || "-"}`,
      status: task.status || "待执行",
      priority: task.priority || "普通",
      id: task.id,
      source: "manual",
    }));
  const done = records.filter((record) => record.submitted).slice(0, 4).map(recordToTask);
  const processing = records.filter((record) => !record.submitted && record.recognitionStatus !== "not_started").slice(0, 4).map(recordToTask);
  // F5 修复 · 待执行不再用 points 假造，只接 manualTasks。无数据时给一条引导。
  const pending = [];
  const overdue = filteredAssets()
    .filter((asset) => normalizeStatus(asset.lastStatus) === "danger")
    .slice(0, 3)
    .map((asset) => ({ title: asset.assetName, meta: `${assetKey(asset)} · ${fmtTime(asset.lastInspectedAt)} · 需复核`, status: "逾期" }));
  manualTasks.forEach((task) => {
    if (task.status === "进行中") processing.unshift(task);
    else if (task.status === "已完成") done.unshift(task);
    else if (task.status === "逾期") overdue.unshift(task);
    else pending.unshift(task);
  });
  return { pending, processing, done, overdue };
}

function recordToTask(record) {
  return {
    title: record.templateName || record.pointName || "巡检任务",
    meta: `${record.pointName || "-"} · ${fmtTime(record.createdAt)} · ${statusLabel(record.recognitionStatus)}`,
    status: record.submitted ? "完成" : statusLabel(record.recognitionStatus),
  };
}

function taskColumn(title, items, level) {
  return `
    <article class="task-column">
      <h2>${escapeHTML(title)}</h2>
      ${items.map((item) => `
        <div class="task-card" ${item.id ? `data-task-id="${escapeHTML(item.id)}"` : ""}>
          <b>${escapeHTML(item.title)}</b>
          <span>${escapeHTML(item.meta)}</span>
          <em class="status ${level === "danger" ? "danger" : level === "warning" ? "warning" : "normal"}">${escapeHTML(item.status)}</em>
        </div>
      `).join("") || `<div class="empty-state small">暂无${escapeHTML(title)}任务</div>`}
    </article>
  `;
}

function renderRecordPage() {
  const all = filteredRecordRows();
  const pageSize = 20;
  const totalPages = Math.max(1, Math.ceil(all.length / pageSize));
  if (state.recordPage > totalPages - 1) state.recordPage = totalPages - 1;
  if (state.recordPage < 0) state.recordPage = 0;
  const start = state.recordPage * pageSize;
  const records = all.slice(start, start + pageSize);
  const selected = all.find((record) => record.id === state.selectedRecordId) || all[0] || null;
  if (selected) state.selectedRecordId = selected.id;
  $("#pageMain").innerHTML = `
    <section class="panel record-table-panel">
      <div class="panel-head record-panel-head">
        <div class="panel-title-block"><h2>巡检记录</h2><p>照片、识别字段、AI 总结、人工确认和提交结果全部可追溯。</p></div>
        <div class="panel-actions record-toolbar">${recordFiltersHTML()}<button id="exportRecordsBtn">导出记录</button></div>
      </div>
      <div class="record-list">
        ${records.map((record) => {
          const result = recordResultStatus(record);
          const flow = recordFlowStatus(record);
          const main = result || flow;
          return `
          <article data-record-select="${escapeHTML(record.id)}" class="${record.id === state.selectedRecordId ? "selected-card" : ""}">
            <time>${fmtTime(record.createdAt)}</time>
            <div><b>${escapeHTML(record.pointName || record.templateName || "巡检记录")}</b><span>${escapeHTML(primaryReading(record) || record.aiSummary || record.report || "-")}</span></div>
            <div class="status-col"><em class="status ${statusClass(main)}">${escapeHTML(main)}</em>${result ? `<small class="flow-tag">${escapeHTML(flow)}</small>` : ""}</div>
          </article>`;
        }).join("") || `<div class="empty-state">暂无巡检记录</div>`}
      </div>
      <div class="pager">
        <span>共 ${all.length} 条 · 第 ${state.recordPage + 1}/${totalPages} 页</span>
        <span class="pager-ctrls">
          <button data-record-pager="prev" ${state.recordPage === 0 ? "disabled" : ""}>‹ 上一页</button>
          <button data-record-pager="next" ${state.recordPage >= totalPages - 1 ? "disabled" : ""}>下一页 ›</button>
        </span>
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = renderRecordSide(selected);
  $("#exportRecordsBtn").addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    exportRecordsWorkbook();
  });
  bindRecordFilters();
  $$("[data-record-pager]").forEach((btn) => {
    btn.addEventListener("click", (event) => {
      event.preventDefault();
      event.stopPropagation();
      if (btn.disabled) return;
      state.recordPage += btn.dataset.recordPager === "prev" ? -1 : 1;
      renderRecordPage();
    });
  });
}

function recordFiltersHTML() {
  const projectOptions = projects();
  const templateMap = new Map();
  state.templates.forEach((template) => {
    const value = template.name || template.templateName || template.id;
    if (value) templateMap.set(value, template.name || template.templateName || template.id);
  });
  state.records.forEach((record) => {
    const value = record.templateName || record.templateId;
    if (value) templateMap.set(value, record.templateName || record.templateId);
  });
  const opt = (val, cur) => `<option value="${escapeHTML(val)}" ${cur === val ? "selected" : ""}>${escapeHTML(val)}</option>`;
  return `
    <div class="filters">
      <select id="recordProjectFilter"><option value="">全部项目</option>${projectOptions.map((project) => `<option value="${escapeHTML(project)}" ${state.recordFilters.project === project ? "selected" : ""}>${escapeHTML(project)}</option>`).join("")}</select>
      <select id="recordTemplateFilter"><option value="">全部模板</option>${Array.from(templateMap.entries()).map(([value, label]) => `<option value="${escapeHTML(value)}" ${state.recordFilters.template === value ? "selected" : ""}>${escapeHTML(label)}</option>`).join("")}</select>
      <select id="recordStatusFilter"><option value="">全部结果</option>${RECORD_RESULT_OPTIONS.map((s) => opt(s, state.recordFilters.status)).join("")}</select>
      <select id="recordFlowFilter"><option value="">全部流程</option>${RECORD_FLOW_OPTIONS.map((s) => opt(s, state.recordFilters.flowStatus)).join("")}</select>
      <input id="recordKeywordInput" type="search" placeholder="搜索巡检人 / 点位" value="${escapeHTML(state.recordFilters.keyword)}" />
    </div>
  `;
}

function bindRecordFilters() {
  const refresh = () => {
    state.recordPage = 0;
    state.selectedRecordId = filteredRecordRows()[0]?.id || "";
    renderRecordPage();
  };
  $("#recordProjectFilter")?.addEventListener("change", (event) => {
    state.recordFilters.project = event.target.value;
    refresh();
  });
  $("#recordTemplateFilter")?.addEventListener("change", (event) => {
    state.recordFilters.template = event.target.value;
    refresh();
  });
  $("#recordStatusFilter")?.addEventListener("change", (event) => {
    state.recordFilters.status = event.target.value;
    refresh();
  });
  $("#recordFlowFilter")?.addEventListener("change", (event) => {
    state.recordFilters.flowStatus = event.target.value;
    refresh();
  });
  $("#recordKeywordInput")?.addEventListener("input", (event) => {
    state.recordFilters.keyword = event.target.value;
    clearTimeout(bindRecordFilters.keywordTimer);
    bindRecordFilters.keywordTimer = setTimeout(() => {
      const cursor = state.recordFilters.keyword.length;
      refresh();
      requestAnimationFrame(() => {
        const input = $("#recordKeywordInput");
        if (!input) return;
        input.focus();
        input.setSelectionRange(cursor, cursor);
      });
    }, 180);
  });
}

function renderLedgerPage() {
  const assets = filteredAssets();
  const counts = statusCounts(assets);
  $("#pageMain").innerHTML = `
    ${metrics([
      { label: "资产总数", value: counts.total, sub: "台/套" },
      { label: "正常资产", value: counts.normal, sub: `${ratio(counts.normal, counts.total)}%`, good: true },
      { label: "异常资产", value: counts.danger, sub: "需处理", bad: counts.danger > 0 },
      { label: "待复核", value: counts.warning + counts.repair, sub: "人工确认" },
    ])}
    ${assetTablePanel(assets, "资产台账", "点击任意资产查看详情并维护台账。", true, `<button id="exportAssetsBtn">导出台账</button>`)}
  `;
  $("#pageAside").innerHTML = renderAssetSide(selectedAsset() || assets[0]);
  $("#exportAssetsBtn").addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    exportAssetsWorkbook();
  });
  bindAssetFilters();
}

function assetTablePanel(assets, title, desc, withFilters, action = "") {
  return `
    <section class="panel asset-table-panel">
      <div class="panel-head asset-panel-head">
        <div class="panel-title-block"><h2>${escapeHTML(title)}</h2><p>${escapeHTML(desc)}</p></div>
        <div class="panel-actions asset-toolbar">${withFilters ? assetFiltersHTML() : `<button class="link-btn" data-page-link="ledger">全部台账</button>`}${action}</div>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th></th><th>设备编号</th><th>设备名称</th><th>设备类型</th><th>安装位置</th><th>状态</th><th>最近巡检时间</th><th>操作</th></tr></thead>
          <tbody>${assets.map((asset) => `
            <tr data-asset-select="${escapeHTML(asset.id)}" class="${asset.id === state.selectedAssetId ? "selected" : ""}">
              <td><i></i></td>
              <td>${escapeHTML(assetKey(asset))}</td>
              <td>${escapeHTML(asset.assetName || "未命名资产")}</td>
              <td>${escapeHTML(asset.assetType || "-")}</td>
              <td>${escapeHTML(locationText(asset))}</td>
              <td><span class="status ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</span></td>
              <td>${fmtTime(asset.lastInspectedAt)}</td>
              <td><button class="view-link" data-asset-select="${escapeHTML(asset.id)}">查看</button></td>
            </tr>
          `).join("") || emptyRow(8, "暂无资产数据")}</tbody>
        </table>
      </div>
      <div class="pager"><span>共 ${assets.length} 条</span><span>20 条/页</span></div>
    </section>
  `;
}

function assetFiltersHTML() {
  const types = [...new Set(state.assets.map((asset) => asset.assetType).filter(Boolean))];
  const statuses = [...new Set([...ASSET_STATUS_OPTIONS, ...state.assets.map((asset) => asset.lastStatus).filter(Boolean)])];
  return `
    <div class="filters">
      <select id="assetTypeFilter"><option value="">全部设备类型</option>${types.map((type) => `<option value="${escapeHTML(type)}" ${state.filters.assetType === type ? "selected" : ""}>${escapeHTML(type)}</option>`).join("")}</select>
      <select id="statusFilter"><option value="">全部状态</option>${statuses.map((status) => `<option value="${escapeHTML(status)}" ${state.filters.status === status ? "selected" : ""}>${escapeHTML(status)}</option>`).join("")}</select>
      <input id="keywordInput" type="search" placeholder="请输入设备编号 / 名称" value="${escapeHTML(state.filters.keyword)}" />
    </div>
  `;
}

function bindAssetFilters() {
  const refresh = () => {
    const first = filteredAssets()[0];
    state.selectedAssetId = first?.id || "";
    renderLedgerPage();
  };
  $("#assetTypeFilter")?.addEventListener("change", (event) => {
    state.filters.assetType = event.target.value;
    refresh();
  });
  $("#statusFilter")?.addEventListener("change", (event) => {
    state.filters.status = event.target.value;
    refresh();
  });
  $("#keywordInput")?.addEventListener("input", (event) => {
    state.filters.keyword = event.target.value;
    clearTimeout(bindAssetFilters.keywordTimer);
    bindAssetFilters.keywordTimer = setTimeout(() => {
      const cursor = state.filters.keyword.length;
      refresh();
      requestAnimationFrame(() => {
        const input = $("#keywordInput");
        if (!input) return;
        input.focus();
        input.setSelectionRange(cursor, cursor);
      });
    }, 180);
  });
}

function renderDevicePage() {
  const assets = filteredAssets();
  $("#pageMain").innerHTML = `
    <section class="panel">
      <div class="panel-head">
        <div><h2>设备管理</h2><p>维护设备档案、绑定点位、巡检模板、二维码和启停状态。</p></div>
        <button data-drawer="device">新增设备</button>
      </div>
      <div class="device-grid panel-body-grid">
        ${assets.map((asset) => `
          <article data-asset-select="${escapeHTML(asset.id)}">
            <div><b>${escapeHTML(asset.assetName || "未命名设备")}</b><em class="status ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</em></div>
            <p>设备编号：${escapeHTML(assetKey(asset))}</p>
            <p>设备类型：${escapeHTML(asset.assetType || "-")}</p>
            <span>绑定点位：${escapeHTML(locationText(asset))}</span>
          </article>
        `).join("") || `<div class="empty-state">暂无设备档案</div>`}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = "";
}

function renderExceptionPage() {
  const assets = filteredAssets().filter((asset) => ["warning", "danger", "repair"].includes(asset.statusLevel || normalizeStatus(asset.lastStatus)));
  const pending = filteredRequests().filter((request) => request.status === "pending");
  const allAssets = filteredAssets();
  const insightKey = `month::${state.selectedProject || ""}`;
  if (!(insightKey in state.dataInsights)) loadDataInsights("month", state.selectedProject || "");
  const attentionItems = (state.dataInsights[insightKey]?.items || []).slice(0, 3);
  const activeCount = assets.length + pending.length;
  $("#pageMain").innerHTML = `
    <section class="panel exception-workbench">
      <div class="exception-overview">
        <div class="exception-title">
          <span>异常管理</span>
          <h2>异常复核</h2>
          <p>仅展示需要人工介入的实时异常、字段缺失和低置信度记录。</p>
        </div>
        <div class="exception-stats">
          <article><span>待处理</span><b>${activeCount}</b><em>项</em></article>
          <article><span>异常资产</span><b>${assets.length}</b><em>台</em></article>
          <article><span>正常资产</span><b>${Math.max(0, allAssets.length - assets.length)}</b><em>台</em></article>
        </div>
      </div>
      <div class="risk-grid panel-body-grid">
        ${assets.map((asset) => `
          <article class="${statusClass(asset.lastStatus) === "danger" ? "bad" : "warn"}" data-asset-select="${escapeHTML(asset.id)}">
            <b>${escapeHTML(asset.assetName || "异常资产")}</b>
            <span>${escapeHTML(assetKey(asset))} · ${fmtTime(asset.lastInspectedAt)}</span>
            <p>${escapeHTML(asset.lastSummary || "该资产需要人工复核。")}</p>
            <button data-asset-normal="${escapeHTML(asset.id)}">标记正常</button>
            <button class="ghost" data-asset-select="${escapeHTML(asset.id)}">查看证据</button>
          </article>
        `).join("")}
        ${pending.map((request) => `
          <article class="warn" data-request-open="${escapeHTML(request.id)}">
            <b>${escapeHTML(statusLabel(request.targetType))}修改申请</b>
            <span>${escapeHTML(request.requestedBy || "-")} · ${fmtTime(request.requestedAt)}</span>
            <p>${escapeHTML(request.reason || "待主管复核。")}</p>
            <button data-request-open="${escapeHTML(request.id)}">进入审批</button>
            <button class="ghost" data-request-open="${escapeHTML(request.id)}">查看详情</button>
          </article>
        `).join("")}
        ${!assets.length && !pending.length ? `
          <div class="exception-empty-state">
            <span class="exception-empty-icon">✓</span>
            <div><b>当前没有待处理异常</b><p>实时异常队列已清空。历史趋势关注项会保留在下方，便于提前排查。</p></div>
            <button class="ghost" data-page-link="data">查看智能洞察</button>
          </div>` : ""}
      </div>
    </section>
    ${attentionItems.length ? `
      <section class="panel exception-watchlist">
        <div class="panel-head">
          <div><h2>历史风险关注</h2><p>以下内容来自历史巡检趋势，不等同于当前异常。</p></div>
          <button class="link-btn" data-page-link="data">查看完整洞察 →</button>
        </div>
        <div class="exception-watch-grid">
          ${attentionItems.map((it) => `
            <article data-asset-select="${escapeHTML(it.assetId)}">
              <div><span class="status ${it.riskLevel || "warning"}">${escapeHTML(riskAttentionLabel(it.riskLevel))}</span><b>${escapeHTML(it.assetName || "资产")}</b></div>
              <p>${escapeHTML((it.reasons && it.reasons[0]) || "建议查看历史巡检变化")}</p>
              <em>${it.riskScore} 分</em>
            </article>
          `).join("")}
        </div>
      </section>
    ` : ""}
  `;
  $("#pageAside").innerHTML = "";
}

function renderApprovalPage() {
  const requests = filteredRequests();
  $("#pageMain").innerHTML = `
    <section class="panel">
      <div class="panel-head">
        <div><h2>修改审批</h2><p>人工修正必须留痕，审批后更新最终字段并保留 AI 原始识别。</p></div>
        <button data-drawer="approval">审批设置</button>
      </div>
      <div class="approval-list panel-body-grid">
        ${requests.map((request) => `
          <article>
            <div>
              <span>申请人：${escapeHTML(request.requestedBy || "-")} · ${fmtTime(request.requestedAt)}</span>
              <b>${escapeHTML(statusLabel(request.targetType))}数据修改</b>
              <p>${escapeHTML(request.reason || "未填写申请原因")}</p>
            </div>
            <div class="diff"><em>${escapeHTML(statusLabel(request.status))}</em><strong>${escapeHTML(request.targetId || "-")}</strong></div>
            <footer>
              ${request.status === "pending" ? `<button data-request-review="${escapeHTML(request.id)}" data-action="approve">通过</button><button class="danger" data-request-review="${escapeHTML(request.id)}" data-action="reject">驳回</button>` : `<button data-request-open="${escapeHTML(request.id)}">查看</button>`}
            </footer>
          </article>
        `).join("") || `<div class="empty-state">暂无修改申请</div>`}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = "";
}

const DATA_PERIODS = [
  { key: "today", label: "今日", days: 1 },
  { key: "week",  label: "本周", days: 7 },
  { key: "month", label: "本月", days: 30 },
  { key: "quarter", label: "本季度", days: 90 },
  { key: "all",   label: "全部", days: 9999 },
];

function periodFilter(records, period) {
  const def = DATA_PERIODS.find((p) => p.key === period) || DATA_PERIODS[2];
  if (def.key === "all") return records;
  const cutoff = Date.now() - def.days * 24 * 3600 * 1000;
  return records.filter((r) => new Date(r.createdAt || 0).getTime() > cutoff);
}

function periodSparkline(records, period, days = 14) {
  const buckets = Array.from({ length: days }, (_, i) => {
    const d = new Date(); d.setDate(d.getDate() - (days - 1 - i));
    return d.toISOString().slice(0, 10);
  });
  return buckets.map((day) => records.filter((r) => String(r.createdAt || "").slice(0, 10) === day).length);
}

function sparkSvg(points, color = "var(--blue)", w = 90, h = 28) {
  if (!points || !points.length) return "";
  const max = Math.max(1, ...points);
  const step = w / (points.length - 1 || 1);
  const path = points.map((v, i) => {
    const x = i * step;
    const y = h - 3 - (v / max) * (h - 6);
    return (i === 0 ? "M" : "L") + x.toFixed(1) + "," + y.toFixed(1);
  }).join(" ");
  const area = path + ` L${w},${h} L0,${h} Z`;
  return `<svg viewBox="0 0 ${w} ${h}" preserveAspectRatio="none" class="kpi-spark">
    <defs><linearGradient id="kpiGrad-${color.replace(/[^a-z0-9]/gi, "")}" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="${color}" stop-opacity="0.32"/>
      <stop offset="100%" stop-color="${color}" stop-opacity="0"/>
    </linearGradient></defs>
    <path d="${area}" fill="url(#kpiGrad-${color.replace(/[^a-z0-9]/gi, "")})"/>
    <path d="${path}" fill="none" stroke="${color}" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"/>
  </svg>`;
}

// ===== 阶段一 数据看板 = 智能洞察台 =====
// 前端时间 tab(today/week/month/quarter/all) → 后端 range key
function periodToRangeKey(period) {
  switch (period) {
    case "today":   return "1d";   // P2-4 修:今日真的只看今天,后端 parseRangeDays 已支持 1d
    case "week":    return "7d";
    case "month":   return "30d";
    case "quarter": return "90d";
    case "all":     return "365d";
    default:        return "30d";
  }
}

const dataInsightsInflight = new Set();
async function loadDataInsights(period, project = "", force = false) {
  const key = `${period}::${project}`;
  if (!force && key in state.dataInsights) return;
  if (dataInsightsInflight.has(key)) return;
  dataInsightsInflight.add(key);
  const rangeKey = periodToRangeKey(period);
  const errKey = `dataInsights:${key}`;
  try {
    const q = new URLSearchParams({ range: rangeKey });
    if (project) q.set("project", project);
    // P1-4 改用 api():失败要让用户知道,不再隐式回退空数组
    const [snap, attn] = await Promise.all([
      api("/api/management-ai/snapshot?" + q.toString()),
      api(`/api/management-ai/attention?${q.toString()}&limit=5${force ? "&refresh=1" : ""}`),
    ]);
    state.dataInsights[key] = {
      overview:         snap.overview || {},
      repeatedIssues:   snap.repeatedIssues || [],
      inspectorQuality: snap.inspectorQuality || [],
      pendingReviews:   snap.pendingReviews || { needsReview: [], pendingApprovals: [] },
      numericDrifts:    snap.numericDrifts || [],
      items:            attn.items || [],
      summary:          attn.summary || "",
      model:            attn.model || "",
      isMock:           attn.isMock === true, // 阶段一 ai-service 是 mock,前端用这个改底栏标签
      generatedAt:      attn.generatedAt || snap.generatedAt || new Date().toISOString(),
      rangeKey,
    };
    delete state.loadErrors[errKey];
  } catch (err) {
    state.dataInsights[key] = { overview: {}, repeatedIssues: [], inspectorQuality: [], items: [], summary: "", model: "", rangeKey, numericDrifts: [] };
    state.loadErrors[errKey] = err && err.message || "接口请求失败";
  } finally {
    dataInsightsInflight.delete(key);
  }
  render();
}

// P1-4 错误三态:加载失败显示警告条,让用户知道是接口挂了还是真没数据
function renderLoadErrorBanner(scopeKey) {
  const msg = state.loadErrors && state.loadErrors[scopeKey];
  if (!msg) return "";
  return `
    <div class="load-error-banner">
      <span class="leb-icon">⚠</span>
      <span class="leb-msg">加载失败:${escapeHTML(msg)}。后端规则结果仍可看,请稍后刷新或检查服务。</span>
    </div>
  `;
}

// ① Hero(标题 + AI 全局摘要 + 时间 tabs + 项目筛选 + 重新分析)
function renderInsightHero(insights, periodDef) {
  const project = state.selectedProject || "全部项目";
  const updated = insights.generatedAt ? fmtTime(insights.generatedAt) : "—";
  const summary = insights.summary || (insights.items.length === 0
    ? "暂无足够数据生成摘要。先安排一次新巡检 / 提交几条记录,洞察 AI 才有内容可读。"
    : "等待 AI 总结…");
  return `
    <section class="insight-hero">
      <div class="insight-hero-top">
        <div class="insight-hero-title">
          <span class="insight-hero-kicker">智能洞察 · ${escapeHTML(periodDef.label)} · ${escapeHTML(project)}</span>
          <h1>智能洞察台</h1>
          <span class="insight-hero-meta">数据更新 ${escapeHTML(updated)} · ${escapeHTML(insights.isMock ? "DeepSeek-V4 · 预览模式" : (insights.model ? "DeepSeek-V4" : "—"))}</span>
        </div>
        <button class="insight-hero-refresh" data-action="refresh-insights">重新分析</button>
      </div>
      <div class="insight-hero-summary">
        ${escapeHTML(summary)}
      </div>
      <div class="data-period-tabs" role="tablist">
        ${DATA_PERIODS.map((p) => `
          <button class="data-period-chip ${p.key === state.dataPeriod ? "active" : ""}" data-period="${p.key}" type="button">${escapeHTML(p.label)}</button>
        `).join("")}
      </div>
    </section>
  `;
}

// ② Risk panorama 4 KPI(AI 综合,不是裸数字)
function renderRiskKpi(insights) {
  const ov = insights.overview || {};
  const top = insights.items[0];
  const riskIndex = top ? Math.min(100, top.riskScore) : 0;
  const focusCount = insights.items.length;
  const drift = ov.driftFieldCount || 0;
  const lazy = Math.round((ov.lazyConfirmRate || 0) * 100);
  const deltaTxt = (ov.abnormalRecent != null && ov.abnormalPrev != null)
    ? (ov.abnormalRecent > ov.abnormalPrev ? `↑ +${ov.abnormalRecent - ov.abnormalPrev} vs 上期` :
       ov.abnormalRecent < ov.abnormalPrev ? `↓ -${ov.abnormalPrev - ov.abnormalRecent} vs 上期` : `↔ 与上期持平`)
    : "—";
  const riskClass = riskIndex >= 60 ? "danger" : (riskIndex >= 25 ? "warning" : "normal");
  return `
    <section class="risk-kpi-row">
      <article class="risk-kpi ${riskClass}">
        <span class="risk-kpi-label">风险指数</span>
        <b class="risk-kpi-value">${riskIndex}<em>/100</em></b>
        <span class="risk-kpi-sub">${escapeHTML(deltaTxt)}</span>
      </article>
      <article class="risk-kpi">
        <span class="risk-kpi-label">重点关注资产</span>
        <b class="risk-kpi-value">${focusCount}<em>台</em></b>
        <span class="risk-kpi-sub">AI 综合判定</span>
      </article>
      <article class="risk-kpi ${drift > 0 ? "warning" : ""}">
        <span class="risk-kpi-label">字段漂移项</span>
        <b class="risk-kpi-value">${drift}<em>项</em></b>
        <span class="risk-kpi-sub">数值字段超阈值</span>
      </article>
      <article class="risk-kpi ${lazy >= 30 ? "warning" : ""}">
        <span class="risk-kpi-label">未看图确认率</span>
        <b class="risk-kpi-value">${lazy}<em>%</em></b>
        <span class="risk-kpi-sub">人工复核质量信号</span>
      </article>
    </section>
  `;
}

// ③ 重点关注 Top 5
function renderFocusBoard(insights) {
  if (!insights.items.length) {
    return `<section class="focus-board"><div class="focus-board-head"><h2>今日重点关注</h2><span>暂无需重点关注的资产</span></div></section>`;
  }
  const items = insights.items || [];
  const featured = items.find((item) => {
    const asset = state.assets.find((a) => a.id === item.assetId);
    return /电梯|elevator/i.test(`${item.assetName || ""} ${item.assetId || ""} ${asset?.assetName || ""} ${asset?.assetType || ""}`);
  }) || items[0];
  const remaining = items.filter((item) => item !== featured).slice(0, 4);
  return `
    <section class="focus-board">
      <div class="focus-board-head">
        <div><h2>近期重点关注</h2><small>AI 基于历史巡检、异常频次和字段漂移综合判断</small></div>
        <span class="focus-board-count">${items.length} 台需关注</span>
      </div>
      <article class="focus-feature focus-${featured.riskLevel}" data-asset-select="${escapeHTML(featured.assetId)}">
        <div class="focus-feature-top"><span>建议优先复核</span><strong>${featured.riskScore}<em>分</em></strong></div>
        <h3>${escapeHTML(featured.assetName || "—")}</h3>
        <div class="focus-feature-reasons">${(featured.reasons || []).slice(0, 3).map((reason) => `<span>${escapeHTML(reason)}</span>`).join("")}</div>
        ${featured.action ? `<p>下一步：${escapeHTML(featured.action)}</p>` : ""}
      </article>
      <div class="focus-grid focus-grid-compact">
        ${remaining.map((it, idx) => `
          <article class="focus-card focus-${it.riskLevel}" data-asset-select="${escapeHTML(it.assetId)}">
            <div class="focus-card-rank">${idx + 2}</div>
            <div class="focus-card-body">
              <div class="focus-card-head">
                <b class="focus-card-name">${escapeHTML(it.assetName || "—")}</b>
                <span class="focus-card-score">${it.riskScore}<em>分</em></span>
                <span class="status ${it.riskLevel || "warning"}">${escapeHTML(riskAttentionLabel(it.riskLevel))}</span>
              </div>
              <p class="focus-card-summary">${escapeHTML((it.reasons && it.reasons[0]) || "建议查看历史巡检变化")}</p>
            </div>
          </article>
        `).join("")}
      </div>
    </section>
  `;
}

// ④ 设备状态时间热力图 — 用户核心诉求"时间维度直观看到设备的各种状态"
// 客户端从 state.records 聚合(近 30/60d 按日格)。简单可靠;后续可换 asset_snapshots 后端数据。
function renderStatusHeatmap(periodDef, insights = {}) {
  const days = Math.min(periodDef.days, 30); // 阶段一固定显示最近 30 天,避免格子太多
  const today = new Date();
  const dayKeys = Array.from({ length: days }, (_, i) => {
    const d = new Date(today); d.setDate(d.getDate() - (days - 1 - i));
    return d.toISOString().slice(0, 10);
  });
  const allAssets = filteredAssets();
  if (!allAssets.length) {
    return `<section class="status-heatmap-panel"><div class="panel-head"><h2>设备状态时间轴</h2><small>近 ${days} 天</small></div><div class="empty-state">暂无资产</div></section>`;
  }
  const focusIds = new Set((insights.items || []).map((item) => item.assetId));
  const assets = [...allAssets].sort((a, b) => {
    const focusDiff = Number(focusIds.has(b.id)) - Number(focusIds.has(a.id));
    if (focusDiff) return focusDiff;
    const elevatorDiff = Number(/电梯|elevator/i.test(`${b.assetType || ""} ${b.assetName || ""}`))
      - Number(/电梯|elevator/i.test(`${a.assetType || ""} ${a.assetName || ""}`));
    if (elevatorDiff) return elevatorDiff;
    return String(b.lastInspectedAt || "").localeCompare(String(a.lastInspectedAt || ""));
  }).slice(0, 6);
  // 给每台资产建一个 day → 最严重级别的 map
  const bucket = new Map();
  for (const a of assets) bucket.set(a.id, {});
  for (const r of state.records) {
    if (!r.submitted) continue;
    const k = String(r.createdAt || "").slice(0, 10);
    if (!k) continue;
    const lvl = recordLevel(r);
    for (const a of assets) {
      const touches = (r.id === a.lastRecordId)
        || (a.pointId && r.pointId === a.pointId);
      if (!touches) continue;
      const m = bucket.get(a.id);
      const prev = m[k];
      // 严重度优先:danger > warning > normal
      const rank = { danger: 3, warning: 2, normal: 1 };
      if (!prev || rank[lvl] > rank[prev]) m[k] = lvl;
    }
  }
  const rows = assets.map((a) => {
    const m = bucket.get(a.id) || {};
    const cells = dayKeys.map((k) => {
      const lvl = m[k];
      const cls = lvl === "danger" ? "danger" : (lvl === "warning" ? "warning" : (lvl === "normal" ? "normal" : "empty"));
      const tip = lvl ? `${k} · ${lvl === "danger" ? "异常" : (lvl === "warning" ? "待复核" : "正常")}` : `${k} · 未巡检`;
      return `<span class="hm-cell hm-${cls}" title="${escapeHTML(tip)}"></span>`;
    }).join("");
    return `
      <div class="hm-row" data-asset-select="${escapeHTML(a.id)}">
        <span class="hm-name" title="${escapeHTML(a.assetName)}">${escapeHTML(a.assetName)}</span>
        <span class="hm-track">${cells}</span>
      </div>
    `;
  }).join("");
  const startLabel = dayKeys[0];
  const endLabel = dayKeys[dayKeys.length - 1];
  return `
    <section class="status-heatmap-panel">
      <div class="panel-head">
        <div><h2>重点资产状态时间轴</h2><small>优先展示风险资产与代表设备，最多 6 台</small></div>
        <small>${escapeHTML(startLabel)} → ${escapeHTML(endLabel)} · ■正常 ▲待复核 ●异常 □未巡</small>
      </div>
      <div class="hm-grid">${rows}</div>
      <div class="hm-legend">
        <span><i class="hm-cell hm-normal"></i>正常</span>
        <span><i class="hm-cell hm-warning"></i>待复核</span>
        <span><i class="hm-cell hm-danger"></i>异常</span>
        <span><i class="hm-cell hm-empty"></i>未巡检</span>
      </div>
    </section>
  `;
}

// ⑤ 字段漂移看板:左列数值字段(变化率徽章 + sparkline 暂略,显示 cur/prev),右列重复异常状态字段
function renderDriftBoard(insights) {
  const numeric = insights.numericDrifts || [];
  const repeated = insights.repeatedIssues || [];
  if (!numeric.length && !repeated.length) return "";
  const numericHTML = numeric.length ? `
    <div class="drift-col">
      <h4>数值字段漂移 <small>近 30 天 · 变化率 |Δ| 排序</small></h4>
      <ul class="drift-list">
        ${numeric.slice(0, 6).map((d) => {
          const pct = (d.changeRate >= 0 ? "+" : "") + (d.changeRate * 100).toFixed(1) + "%";
          const cls = d.overThreshold ? "danger" : (d.changeRate >= 0 ? "up" : "down");
          return `<li class="drift-item">
            <span class="drift-name">${escapeHTML(d.assetName)} · ${escapeHTML(d.fieldLabel || d.fieldKey)}</span>
            <span class="drift-vals">本次 ${formatNum(d.current)} · 上次 ${formatNum(d.previous)}</span>
            <span class="drift-rate ${cls}">${escapeHTML(pct)}</span>
          </li>`;
        }).join("")}
      </ul>
    </div>` : `<div class="drift-col"><h4>数值字段漂移</h4><div class="empty-state">无数值字段或无足够历史</div></div>`;
  const repeatedHTML = repeated.length ? `
    <div class="drift-col">
      <h4>状态字段重复异常 <small>同字段累计 ≥2 次</small></h4>
      <ul class="drift-list">
        ${repeated.slice(0, 6).map((r) => `
          <li class="drift-item" data-asset-select="${escapeHTML(r.assetId)}">
            <span class="drift-name">${escapeHTML(r.assetName)} · ${escapeHTML(r.fieldLabel || r.fieldKey)}</span>
            <span class="drift-vals">${escapeHTML(r.lastTime || "")}</span>
            <span class="drift-rate danger">×${r.count}</span>
          </li>`).join("")}
      </ul>
    </div>` : `<div class="drift-col"><h4>状态字段重复异常</h4><div class="empty-state">本期未发现重复异常</div></div>`;
  return `
    <section class="drift-board">
      <div class="drift-board-head"><h2>字段漂移看板</h2><small>规则计算 · 不依赖 AI</small></div>
      <div class="drift-grid">
        ${numericHTML}
        ${repeatedHTML}
      </div>
    </section>
  `;
}

// ⑥ 巡检员质量榜:补拍/无判/未看图/快速确认 等防惰性指标按人聚合
function renderInspectorQuality(insights) {
  const rows = insights.inspectorQuality || [];
  if (!rows.length) return "";
  return `
    <section class="inspector-quality">
      <div class="iq-head"><h2>巡检员质量榜</h2><small>近 30 天 · 留痕指标</small></div>
      <table class="iq-table">
        <thead>
          <tr>
            <th>巡检员</th>
            <th>留痕数</th>
            <th>补拍</th>
            <th>无法判定</th>
            <th>未看图确认</th>
            <th>快速确认 &lt;2s</th>
            <th>平均停留</th>
          </tr>
        </thead>
        <tbody>
          ${rows.slice(0, 10).map((r) => `
            <tr>
              <td><b>${escapeHTML(r.operator)}</b></td>
              <td>${r.total}</td>
              <td class="${r.retakeCount > 0 ? "warn" : ""}">${r.retakeCount}</td>
              <td class="${r.uncertainCount > 0 ? "warn" : ""}">${r.uncertainCount}</td>
              <td class="${r.noPhotoConfirm > 0 ? "danger" : ""}">${r.noPhotoConfirm}</td>
              <td class="${r.fastConfirmCount > 0 ? "danger" : ""}">${r.fastConfirmCount}</td>
              <td>${r.avgDurationMs > 0 ? (r.avgDurationMs / 1000).toFixed(1) + "s" : "—"}</td>
            </tr>
          `).join("")}
        </tbody>
      </table>
    </section>
  `;
}

// ⑦ 异常本期 vs 上期 + AI 一句话解读
function renderPeriodCompare(insights) {
  const ov = insights.overview || {};
  const cur = ov.abnormalRecent || 0;
  const prev = ov.abnormalPrev || 0;
  const recCur = ov.recordRecent || 0;
  const recPrev = ov.recordPrev || 0;
  const delta = cur - prev;
  const deltaTxt = delta > 0 ? `+${delta}` : `${delta}`;
  const trendCls = delta > 0 ? "danger" : (delta < 0 ? "ok" : "neutral");
  const aiLine = delta > 0
    ? `本期异常 ${cur} 项,比上期(${prev} 项)多 ${delta} 项,需要重点关注趋势`
    : delta < 0
      ? `本期异常 ${cur} 项,比上期(${prev} 项)少 ${-delta} 项,趋势在好转`
      : `本期异常 ${cur} 项,与上期持平`;
  return `
    <section class="period-compare">
      <div class="pc-head"><h2>异常本期 vs 上期</h2><small>同长度时间窗对比</small></div>
      <div class="pc-grid">
        <article class="pc-card">
          <span class="pc-label">本期异常</span>
          <b class="pc-value ${cur > 0 ? "danger" : ""}">${cur}</b>
          <span class="pc-sub">${recCur} 条巡检中</span>
        </article>
        <article class="pc-card">
          <span class="pc-label">上期异常</span>
          <b class="pc-value">${prev}</b>
          <span class="pc-sub">${recPrev} 条巡检中</span>
        </article>
        <article class="pc-card pc-delta-card">
          <span class="pc-label">环比</span>
          <b class="pc-value pc-${trendCls}">${escapeHTML(deltaTxt)}</b>
          <span class="pc-sub">${trendCls === "danger" ? "↑ 上升" : (trendCls === "ok" ? "↓ 下降" : "持平")}</span>
        </article>
      </div>
      <div class="pc-ai-line">AI: ${escapeHTML(aiLine)}</div>
    </section>
  `;
}

// ⑧ 辅助区(降级保底):旧 4 KPI / 准确率环 / 闭环率环 / 资产类型分布
function renderInsightAux(records, assets, requests, periodDef) {
  const counts = statusCounts(assets);
  const accuracy = aiAccuracy(records);
  const closedPct = closedRate();
  const submittedCnt = records.filter((r) => r.submitted).length;
  const abnormalCnt = counts.warning + counts.danger + counts.repair;
  const submittedRate = records.length ? Math.round((submittedCnt / records.length) * 100) : 0;
  const pendingApprovals = requests.filter((r) => r.status === "pending").length;
  const closedCnt = requests.filter((r) => r.status === "approved" || r.status === "rejected").length;
  const typeMap = {};
  assets.forEach((a) => {
    const k = a.assetType || "未分类";
    typeMap[k] = (typeMap[k] || 0) + 1;
  });
  const typeList = Object.entries(typeMap).sort((a, b) => b[1] - a[1]).slice(0, 6);
  const typeMax = Math.max(1, ...typeList.map(([, n]) => n));
  const TYPE_COLORS = ["#12a968", "#246bfe", "#f59e0b", "#8b5cf6", "#ef4b3f", "#06b6d4"];
  const accuracyDisplay = accuracy.sample < 5
    ? `<div class="ring muted" style="--value:0">—</div><p class="ring-note">样本不足（${accuracy.sample}/5）</p>`
    : `<div class="ring" style="--value:${accuracy.value}">${accuracy.value}%</div><p class="ring-note">基于 ${accuracy.sample} 个已确认字段</p>`;
  return `
    <section class="insight-aux">
      <div class="insight-aux-head"><h2>基础指标 <small>规则版 · 降级保底</small></h2></div>
      <div class="insight-aux-grid">
        <article class="aux-card">
          <span class="aux-card-label">资产总数</span>
          <b class="aux-card-value">${counts.total}</b>
          <span class="aux-card-sub">正常 ${counts.normal} · 异常 ${counts.danger}</span>
        </article>
        <article class="aux-card">
          <span class="aux-card-label">本期巡检</span>
          <b class="aux-card-value">${records.length}</b>
          <span class="aux-card-sub">提交率 ${submittedRate}%</span>
        </article>
        <article class="aux-card">
          <span class="aux-card-label">待复核 / 异常</span>
          <b class="aux-card-value ${abnormalCnt > 0 ? "danger" : ""}">${abnormalCnt}</b>
          <span class="aux-card-sub">${abnormalCnt > 0 ? "需关注" : "全清"}</span>
        </article>
        <article class="aux-card">
          <span class="aux-card-label">闭环率</span>
          <b class="aux-card-value">${closedPct}%</b>
          <span class="aux-card-sub">${closedCnt} 结案 / ${pendingApprovals} 待审</span>
        </article>
      </div>
      <div class="insight-aux-rings">
        <article class="ring-card">
          <div class="ring-head"><h3>AI 识别准确率</h3></div>
          ${accuracyDisplay}
        </article>
        <article class="ring-card">
          <div class="ring-head"><h3>异常闭环率</h3></div>
          <div class="ring green" style="--value:${closedPct}">${closedPct}%</div>
          <p class="ring-note">复核 → 审批 → 写入台账</p>
        </article>
        <article class="ring-card aux-type-card">
          <div class="ring-head"><h3>资产类型 Top ${typeList.length}</h3></div>
          <div class="type-bars">
            ${typeList.map(([name, n], i) => {
              const pct = Math.round((n / typeMax) * 100);
              const color = TYPE_COLORS[i % TYPE_COLORS.length];
              return `<div class="type-bar"><span class="type-name">${escapeHTML(name)}</span><div class="type-track"><div class="type-fill" style="width:${pct}%; background:${color}"></div></div><span class="type-num"><b>${n}</b></span></div>`;
            }).join("") || `<div class="empty-state">暂无</div>`}
          </div>
        </article>
      </div>
    </section>
  `;
}

// ⑨ 底栏元信息
function renderInsightFooter(insights) {
  const upd = insights.generatedAt ? fmtTime(insights.generatedAt) : "—";
  // mock 期把 model 显示成「DeepSeek-V4 · 预览」,不暴露 deepseek-v4-pro/flash 的具体名
  const modelLabel = insights.isMock
    ? "DeepSeek-V4 · 预览模式"
    : (insights.model ? `DeepSeek-V4(${insights.model})` : "—");
  return `
    <footer class="insight-footer">
      <span>数据更新 ${escapeHTML(upd)}</span>
      <span>·</span>
      <span>${escapeHTML(modelLabel)}</span>
      <span>·</span>
      <span>range ${escapeHTML(insights.rangeKey || "—")}</span>
    </footer>
  `;
}

function renderDataPage() {
  if (!state.dataPeriod) state.dataPeriod = "month";
  const periodDef = DATA_PERIODS.find((p) => p.key === state.dataPeriod) || DATA_PERIODS[2];
  const project = state.selectedProject || "";
  const cacheKey = `${state.dataPeriod}::${project}`;
  if (!(cacheKey in state.dataInsights)) loadDataInsights(state.dataPeriod, project);
  const insights = state.dataInsights[cacheKey] || { overview: {}, items: [], summary: "", model: "—", inspectorQuality: [], rangeKey: periodToRangeKey(state.dataPeriod) };

  const assets = filteredAssets();
  const allRecords = filteredRecords();
  const records = periodFilter(allRecords, state.dataPeriod);
  const requests = filteredRequests();

  $("#pageMain").innerHTML = `
    ${renderLoadErrorBanner(`dataInsights:${cacheKey}`)}
    ${renderInsightHero(insights, periodDef)}
    ${renderRiskKpi(insights)}
    ${renderFocusBoard(insights)}
    ${renderStatusHeatmap(periodDef, insights)}
    ${renderDriftBoard(insights)}
    ${renderInspectorQuality(insights)}
    ${renderPeriodCompare(insights)}
    ${renderInsightAux(records, assets, requests, periodDef)}
    ${renderInsightFooter(insights)}
  `;
  $("#pageAside").innerHTML = "";
  document.querySelectorAll("[data-period]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.dataPeriod = btn.dataset.period;
      render();
    });
  });
  document.querySelector("[data-action=refresh-insights]")?.addEventListener("click", async () => {
    delete state.dataInsights[cacheKey];
    await loadDataInsights(state.dataPeriod, project, true);
  });
}


function roleText(user = {}) {
  return user.roleName || ({
    admin: "系统管理员",
    manager: "管理人员",
    supervisor: "复核审批人员",
    inspector: "一线巡检员",
  }[user.roleCode] || user.roleCode || "-");
}

function roleClass(roleCode = "") {
  return {
    admin: "danger",
    manager: "normal",
    supervisor: "warning",
    inspector: "unknown",
  }[roleCode] || "unknown";
}

function usersTableHTML(action = "") {
  const users = state.users || [];
  return `
    <section class="panel">
      <div class="panel-head"><div><h2>用户与权限</h2><p>当前账号 ${users.length || 0} 个，后续可接企业微信组织架构。</p></div><div class="panel-actions">${action}</div></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>账号</th><th>姓名</th><th>角色</th><th>部门</th><th>状态</th><th>最近登录</th><th>操作</th></tr></thead>
          <tbody>
            ${users.map((user) => `
              <tr data-user-row="${escapeHTML(user.id)}">
                <td>${escapeHTML(user.username)}</td>
                <td>${escapeHTML(user.displayName)}</td>
                <td><span class="pill ${roleClass(user.roleCode)}">${escapeHTML(roleText(user))}</span></td>
                <td>${escapeHTML(user.departmentName || "-")}</td>
                <td><span class="status ${user.status === "active" ? "normal" : "warning"}">${escapeHTML(user.status === "active" ? "启用" : "停用")}</span></td>
                <td>${fmtTime(user.lastLoginAt)}</td>
                <td class="user-row-actions">
                  <button class="mini-btn" data-user-action="edit" data-user-id="${escapeHTML(user.id)}">编辑</button>
                  <button class="mini-btn" data-user-action="reset" data-user-id="${escapeHTML(user.id)}">重置密码</button>
                  <button class="mini-btn ${user.status === "active" ? "danger" : ""}" data-user-action="toggle" data-user-id="${escapeHTML(user.id)}">${user.status === "active" ? "停用" : "启用"}</button>
                </td>
              </tr>
            `).join("") || `<tr><td colspan="7">暂无用户数据</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

// 角色 / 部门下拉数据缓存
const identityMeta = { roles: null, departments: null };

async function ensureIdentityMeta() {
  if (!identityMeta.roles) {
    const data = await safeApi("/api/roles", { roles: [] });
    identityMeta.roles = (data.roles || []).filter((r) => r && r.code);
    if (identityMeta.roles.length === 0) {
      identityMeta.roles = [
        { code: "admin", name: "系统管理员" },
        { code: "manager", name: "管理人员" },
        { code: "supervisor", name: "复核审批人员" },
        { code: "inspector", name: "一线巡检员" },
      ];
    }
  }
  if (!identityMeta.departments) {
    const data = await safeApi("/api/departments", { departments: [] });
    identityMeta.departments = data.departments || [];
    if (identityMeta.departments.length === 0) {
      identityMeta.departments = [{ id: "dept_default", name: "默认部门" }];
    }
  }
  return identityMeta;
}

function bindUsersPageActions() {
  const main = $("#pageMain");
  if (!main) return;
  main.querySelector("#addUserBtn")?.addEventListener("click", () => openUserModal());
  main.querySelectorAll("[data-user-action]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const id = btn.getAttribute("data-user-id");
      const action = btn.getAttribute("data-user-action");
      const user = (state.users || []).find((u) => u.id === id);
      if (!user) return;
      if (action === "edit") openUserModal(user);
      else if (action === "reset") openResetPasswordModal(user);
      else if (action === "toggle") toggleUserStatus(user);
    });
  });
}

async function openUserModal(user = null) {
  await ensureIdentityMeta();
  const isEdit = !!user;
  const modal = createUserModal(user);
  document.body.appendChild(modal);
  modal.querySelector("[data-modal-close]").addEventListener("click", () => modal.remove());
  modal.addEventListener("click", (e) => { if (e.target === modal) modal.remove(); });
  modal.querySelector("form").addEventListener("submit", async (e) => {
    e.preventDefault();
    const form = e.target;
    const payload = {
      username: form.username.value.trim(),
      displayName: form.displayName.value.trim(),
      phone: form.phone.value.trim(),
      roleCode: form.roleCode.value,
      departmentId: form.departmentId.value,
      weworkUserId: form.weworkUserId.value.trim(),
    };
    if (!isEdit) payload.password = form.password.value.trim();
    if (!payload.username || !payload.displayName) {
      toast("账号和姓名必填");
      return;
    }
    if (!isEdit && (!payload.password || payload.password.length < 6)) {
      toast("初始密码至少 6 位");
      return;
    }
    try {
      if (isEdit) {
        await api(`/api/users/${encodeURIComponent(user.id)}`, {
          method: "PUT",
          body: JSON.stringify(payload),
        });
        toast("用户已更新");
      } else {
        await api("/api/users", {
          method: "POST",
          body: JSON.stringify(payload),
        });
        toast("用户已创建");
      }
      modal.remove();
      await loadData();
    } catch (err) {
      toast(err.message || "保存失败");
    }
  });
}

function createUserModal(user = null) {
  const isEdit = !!user;
  const roles = identityMeta.roles || [];
  const departments = identityMeta.departments || [];
  const u = user || { roleCode: "inspector", departmentId: "dept_default" };
  const overlay = document.createElement("div");
  overlay.className = "user-modal-overlay";
  overlay.innerHTML = `
    <div class="user-modal">
      <div class="user-modal-head">
        <h3>${isEdit ? "编辑账号" : "新建账号"}</h3>
        <button type="button" data-modal-close aria-label="关闭">×</button>
      </div>
      <form class="user-modal-form">
        <label>账号 <input name="username" value="${escapeHTML(u.username || "")}" ${isEdit ? "disabled" : "required"} autocomplete="off"></label>
        <label>姓名 <input name="displayName" value="${escapeHTML(u.displayName || "")}" required></label>
        <label>角色
          <select name="roleCode" required>
            ${roles.map((r) => `<option value="${escapeHTML(r.code)}" ${r.code === u.roleCode ? "selected" : ""}>${escapeHTML(r.name)}</option>`).join("")}
          </select>
        </label>
        <label>部门
          <select name="departmentId">
            ${departments.map((d) => `<option value="${escapeHTML(d.id)}" ${d.id === u.departmentId ? "selected" : ""}>${escapeHTML(d.name)}</option>`).join("")}
          </select>
        </label>
        <label>手机号 <input name="phone" value="${escapeHTML(u.phone || "")}" placeholder="选填"></label>
        <label>企业微信 UserID <input name="weworkUserId" value="${escapeHTML(u.weworkUserId || "")}" placeholder="选填，未来对接 SSO 用"></label>
        ${isEdit ? "" : `<label>初始密码 <input name="password" type="text" placeholder="至少 6 位" required minlength="6"></label>`}
        <div class="user-modal-actions">
          <button type="button" data-modal-close class="btn-ghost">取消</button>
          <button type="submit" class="btn-primary">${isEdit ? "保存" : "创建"}</button>
        </div>
      </form>
    </div>
  `;
  return overlay;
}

async function openResetPasswordModal(user) {
  const newPwd = window.prompt(`为「${user.displayName}」(${user.username}) 重置密码：\n输入新密码（至少 6 位）`);
  if (!newPwd) return;
  if (newPwd.trim().length < 6) {
    toast("密码至少 6 位");
    return;
  }
  try {
    await api(`/api/users/${encodeURIComponent(user.id)}/password`, {
      method: "POST",
      body: JSON.stringify({ password: newPwd.trim() }),
    });
    toast(`已为 ${user.username} 重置密码`);
  } catch (err) {
    toast(err.message || "重置失败");
  }
}

async function toggleUserStatus(user) {
  const next = user.status === "active" ? "disabled" : "active";
  const verb = next === "disabled" ? "停用" : "启用";
  if (!window.confirm(`确认${verb}「${user.displayName}」(${user.username})？`)) return;
  try {
    await api(`/api/users/${encodeURIComponent(user.id)}/status`, {
      method: "POST",
      body: JSON.stringify({ status: next }),
    });
    toast(`已${verb} ${user.username}`);
    await loadData();
  } catch (err) {
    toast(err.message || `${verb}失败`);
  }
}

function operationLogsHTML(action = "") {
  const logs = state.operationLogs || [];
  return `
    <section class="panel">
      <div class="panel-head"><div><h2>操作日志</h2><p>记录登录、台账修改、审批等关键动作。</p></div><div class="panel-actions">${action}</div></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>时间</th><th>人员</th><th>动作</th><th>对象</th><th>编号</th></tr></thead>
          <tbody>
            ${logs.slice(0, 12).map((item) => `
              <tr>
                <td>${fmtTime(item.createdAt)}</td>
                <td>${escapeHTML(item.actorName || "-")}</td>
                <td><span class="log-action ${logActionTone(item.action)}">${escapeHTML(item.action || "-")}</span></td>
                <td><span class="log-target">${escapeHTML(item.targetType || "-")}</span></td>
                <td><code class="log-id">${escapeHTML(shortId(item.targetId || "-"))}</code></td>
              </tr>
            `).join("") || `<tr><td colspan="5">暂无操作日志</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function currentUserSummaryHTML() {
  const user = state.currentUser || {};
  return `
    <div class="info-card user-summary-card">
      <b>${escapeHTML(user.displayName || "未登录")}</b>
      <span>账号：${escapeHTML(user.username || "-")}</span>
      <span>角色：${escapeHTML(roleText(user))}</span>
      <span>部门：${escapeHTML(user.departmentName || "-")}</span>
      <button class="btn-ghost danger-ghost" id="logoutBtn" type="button">退出登录</button>
    </div>
  `;
}

function renderProfilePage() {
  const user = state.currentUser || {};
  const pending = filteredRequests().filter((item) => item.status === "pending").length;
  const today = todayKey();
  const todayRecords = state.records.filter((record) => String(record.createdAt || "").startsWith(today)).length;
  const abnormal = filteredAssets().filter((asset) => normalizeStatus(asset.lastStatus) !== "normal").length;
  const logs = (state.operationLogs || []).filter((item) => item.actorName === user.displayName || item.userId === user.id);
  const avatar = (user.displayName || user.username || "管").slice(0, 1);
  const role = roleText(user);
  const weekMs = 7 * 24 * 3600 * 1000;
  const weekLogins = logs.filter((l) => l.action === "login" && Date.now() - new Date(l.createdAt || 0).getTime() < weekMs).length;

  $("#pageMain").innerHTML = `
    <section class="ai-hero ai-hero-v2 profile-hero">
      <div class="ai-hero-text">
        <div class="profile-hero-row">
          <div class="profile-hero-avatar" aria-hidden="true">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
              <circle cx="12" cy="8" r="4"/>
              <path d="M4 21c0-4.4 3.6-8 8-8s8 3.6 8 8"/>
            </svg>
          </div>
          <div class="profile-hero-meta">
            <b class="profile-hero-name">${escapeHTML(user.displayName || "未登录")}</b>
            <span>${escapeHTML(role)} · 账号 ${escapeHTML(user.username || "-")}</span>
          </div>
        </div>
      </div>
      <button class="btn-ghost danger-ghost profile-hero-logout" id="profileLogoutBtn">退出登录</button>
      <aside class="ai-hero-side" aria-hidden="true">
        <span class="ai-hero-orb ai-hero-orb-1"></span>
        <span class="ai-hero-orb ai-hero-orb-2"></span>
        <span class="ai-hero-orb ai-hero-orb-3"></span>
      </aside>
      <svg class="ai-hero-cursor" viewBox="0 0 28 28" aria-hidden="true">
        <defs>
          <linearGradient id="profileTriGrad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0%" stop-color="#7effd2"/>
            <stop offset="100%" stop-color="#25d5ff"/>
          </linearGradient>
        </defs>
        <polygon points="14,3 25,24 3,24" fill="url(#profileTriGrad)" opacity="0.85"/>
      </svg>
    </section>
    ${metrics([
      { label: "待审批", value: pending, sub: "修改申请", bad: pending > 0 },
      { label: "今日巡检", value: todayRecords, sub: "移动端记录" },
      { label: "异常复核", value: abnormal, sub: "资产状态", bad: abnormal > 0 },
      { label: "操作日志", value: logs.length, sub: "本人动作" },
    ])}
    <section class="page-grid two profile-grid">
      <article class="panel profile-panel">
        <div class="panel-head"><h2>权限范围</h2></div>
        <div class="permission-list profile-permissions">
          ${permissionItem("资产台账", "查看 / 修改 / 追踪状态", true)}
          ${permissionItem("异常复核", "处理异常复核闭环", user.roleCode !== "inspector")}
          ${permissionItem("修改审批", "审批字段修正与台账修改", user.roleCode === "admin" || user.roleCode === "manager" || user.roleCode === "supervisor")}
          ${permissionItem("用户管理", "后台账号 / 角色 / 部门", user.roleCode === "admin")}
        </div>
      </article>
      <article class="panel profile-panel">
        <div class="panel-head"><h2>最近操作</h2><button class="link-btn" data-page-link="logs">全部</button></div>
        <div class="activity-list profile-activity-list">
          ${logs.slice(0, 6).map(profileActivityItem).join("") || `<div class="empty-state">暂无本人操作日志</div>`}
        </div>
      </article>
    </section>
  `;
  const openTasks = myOpenTasks();
  const doneToday = myCompletedTodayTasks();
  $("#pageAside").innerHTML = `
    <div class="aside-stack">
      <div class="my-todo-card">
        <div class="my-todo-head">
          <b>我的待办</b>
          <span class="my-todo-count ${openTasks.length > 0 ? "active" : ""}">${openTasks.length}</span>
        </div>
        ${openTasks.length === 0 ? `
          <div class="my-todo-empty">${doneToday.length > 0 ? `今日已完成 ${doneToday.length} 项 🎉` : "暂无指派给你的任务"}</div>
        ` : `
          <ul class="my-todo-list">
            ${openTasks.slice(0, 6).map((t) => `
              <li class="my-todo-row" data-task-link="${escapeHTML(t.id)}">
                <span class="my-todo-icon ${t.status === "进行中" ? "doing" : ""}"></span>
                <div class="my-todo-body">
                  <b>${escapeHTML(t.title)}</b>
                  <span>${escapeHTML(t.project || "-")} · ${escapeHTML(t.frequency || "-")} · ${escapeHTML(t.status)}</span>
                </div>
                <em>${escapeHTML(t.dueAt || "—")}</em>
              </li>
            `).join("")}
          </ul>
        `}
      </div>
      ${doneToday.length > 0 ? `
        <div class="my-done-card">
          <div class="my-done-head">
            <b>今日已完成</b>
            <span class="my-done-count">${doneToday.length}</span>
          </div>
          <ul class="my-done-list">
            ${doneToday.slice(0, 5).map((t) => `
              <li class="my-done-row" data-task-link="${escapeHTML(t.id)}">
                <span class="my-done-icon">✓</span>
                <div class="my-done-body">
                  <b>${escapeHTML(t.title)}</b>
                  <span>${escapeHTML(relativeTime(t.completedAt))} 完成</span>
                </div>
              </li>
            `).join("")}
          </ul>
        </div>
      ` : ""}
      <div class="ai-month-card profile-aside-card">
        <div class="month-head"><div><b>本周动作</b></div></div>
        <div class="month-rows">
          <div><span>本周登录</span><b>${weekLogins}</b></div>
          <div><span>累计操作</span><b>${logs.length}</b></div>
          <div><span>最近登录</span><b style="font-size:13px">${escapeHTML(fmtTime(user.lastLoginAt))}</b></div>
        </div>
      </div>
      <div class="todo-card">
        <div class="todo-head"><b>快速入口</b></div>
        <a class="todo-row info" data-page-link="approval">
          <img src="./assets/icon-bulb.svg" alt=""/>
          <span>修改审批</span><b>${pending}</b><em>去审批 →</em>
        </a>
        <a class="todo-row info" data-page-link="ledger">
          <img src="./assets/nav-asset.svg" alt=""/>
          <span>资产台账</span><b>${state.assets.length}</b><em>查看 →</em>
        </a>
      </div>
    </div>
  `;
  document.querySelectorAll("[data-task-link]").forEach((el) => {
    el.addEventListener("click", () => {
      state.selectedTaskId = el.dataset.taskLink;
      setPage("task");
    });
  });
  $("#profileLogoutBtn")?.addEventListener("click", logout);
  bindHeroCursor();
}

function profileMetric(label, value, sub, tone = "info") {
  return `<article class="profile-metric metric-${escapeHTML(tone)}"><span>${escapeHTML(label)}</span><b>${escapeHTML(value)}</b><em>${escapeHTML(sub)}</em></article>`;
}

const ACTION_LABEL = {
  login: "登录后台",
  logout: "退出登录",
  ai_chat: "AI 智能问答",
  "asset.patch": "修改资产",
  "asset.create": "新建资产",
  approval: "审批操作",
  classify: "AI 场景识别",
};
function actionLabel(action = "") {
  return ACTION_LABEL[action] || ACTION_LABEL[action.toLowerCase()] || action || "—";
}

function profileActivityItem(item) {
  const tone = logActionTone(item.action);
  const label = actionLabel(item.action);
  const target = item.targetType ? `${item.targetType} ${shortId(item.targetId || "")}` : "";
  return `
    <div class="profile-activity-row tone-${escapeHTML(tone)}">
      <i></i>
      <div class="par-main">
        <b>${escapeHTML(label)}</b>
        ${target ? `<span class="par-target">${escapeHTML(target)}</span>` : ""}
      </div>
      <time>${fmtTime(item.createdAt)}</time>
    </div>
  `;
}

function identityMetric(label, value, sub) {
  return `<article><span>${escapeHTML(label)}</span><b>${escapeHTML(value)}</b><em>${escapeHTML(sub)}</em></article>`;
}

function permissionItem(title, desc, enabled) {
  return `
    <div class="perm-row ${enabled ? "enabled" : "disabled"}">
      <span class="perm-mark" aria-hidden="true">${enabled ? "✓" : "·"}</span>
      <div class="perm-main">
        <b>${escapeHTML(title)}</b>
        <span>${escapeHTML(desc)}</span>
      </div>
      <em class="perm-chip">${enabled ? "已开放" : "未授权"}</em>
    </div>
  `;
}

function renderUsersPage() {
  const users = state.users || [];
  const roleSummary = users.reduce((acc, user) => {
    const key = roleText(user);
    acc[key] = (acc[key] || 0) + 1;
    return acc;
  }, {});
  $("#pageMain").innerHTML = `
    <section class="identity-metrics">
      ${identityMetric("账号总数", users.length, "users")}
      ${identityMetric("启用账号", users.filter((user) => user.status === "active").length, "active")}
      ${identityMetric("角色类型", Object.keys(roleSummary).length, "roles")}
      ${identityMetric("部门数量", new Set(users.map((user) => user.departmentName).filter(Boolean)).size, "departments")}
    </section>
    ${usersTableHTML(`<button id="addUserBtn">新增用户</button>`)}
    <section class="panel">
      <div class="panel-head"><h2>角色分布</h2><span>当前系统第一阶段权限模型</span></div>
      <div class="role-board">
        ${Object.entries(roleSummary).map(([name, count]) => `<div><b>${escapeHTML(name)}</b><span>${count} 人</span></div>`).join("") || `<div><b>暂无角色</b><span>等待账号初始化</span></div>`}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = asideStack(liveActivityCard());
  bindLiveActivity();
  bindUsersPageActions();
}

function renderLogsPage() {
  const logs = state.operationLogs || [];
  $("#pageMain").innerHTML = `
    <section class="identity-metrics">
      ${identityMetric("日志总数", logs.length, "最近记录")}
      ${identityMetric("登录动作", logs.filter((item) => item.action === "login").length, "login")}
      ${identityMetric("台账修改", logs.filter((item) => String(item.action || "").includes("asset")).length, "asset.patch")}
      ${identityMetric("审批动作", logs.filter((item) => String(item.action || "").includes("change_request")).length, "approval")}
    </section>
    ${operationLogsHTML(`<button id="refreshLogsBtn">刷新日志</button>`)}
    <section class="panel">
      <div class="panel-head"><h2>审计时间线</h2><span>按发生时间倒序展示</span></div>
      <div class="audit-timeline">
        ${logs.slice(0, 10).map((item) => `<div><i></i><b>${escapeHTML(item.actorName || "-")} · ${escapeHTML(item.action || "-")}</b><span>${fmtTime(item.createdAt)} / ${escapeHTML(item.targetType || "-")} / ${escapeHTML(shortId(item.targetId || "-"))}</span></div>`).join("") || `<div class="empty-state">暂无操作日志</div>`}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = asideStack(liveActivityCard());
  bindLiveActivity();
  $("#refreshLogsBtn")?.addEventListener("click", () => loadData());
}

function renderSystemPage() {
  const store = state.health?.storeKind || state.health?.store || "unknown";
  // F7 · AI 视觉服务状态来自 backend health 的 aiServiceUrl
  const aiUrl = state.health?.aiServiceUrl;
  const aiStatus = aiUrl ? "已配置" : "未配置";
  const aiLevel = aiUrl ? "normal" : "warning";
  // F8 · 文件存储真状态（基于 backend health）
  const storage = state.health?.storage || state.health?.uploadsDir;
  const storagePath = storage || "未接入";
  const storageStatus = storage ? "本地磁盘" : "未接入";
  const storageLevel = storage ? "normal" : "warning";
  $("#pageMain").innerHTML = `
    <section class="system-grid system-health-grid">
      ${systemCard("Go API 服务", state.apiBase, state.health ? "运行中" : "待检查", state.health ? "normal" : "warning")}
      ${systemCard("AI 视觉服务", aiUrl || "未配置", aiStatus, aiLevel)}
      ${systemCard("数据库", store, store === "mysql" ? "MySQL 已启用" : "当前存储", store === "mysql" ? "normal" : "warning")}
      ${systemCard("文件存储", storagePath, storageStatus, storageLevel)}
    </section>
    <section class="panel">
      <div class="panel-head"><div><h2>部署配置</h2><p>云服务器迁移前检查项。</p></div><button id="saveSystemBtn">保存配置</button></div>
      <div class="table-wrap">
        <table><tbody>
          <tr><td>公网域名</td><td>ai-demo.jadeastech.com</td><td><span class="status normal">HTTPS 已启用</span></td></tr>
          <tr><td>企业微信回调</td><td>/wework/callback</td><td><span class="status normal">预留</span></td></tr>
          <tr><td>对象存储</td><td>MinIO / OSS</td><td><span class="status warning">可替换</span></td></tr>
        </tbody></table>
      </div>
    </section>
    ${usersTableHTML()}
    ${operationLogsHTML()}
  `;
  $("#pageAside").innerHTML = asideStack(liveActivityCard(), systemConfigForm());
  bindLiveActivity();
  bindUsersPageActions();
  $("#saveSystemBtn").addEventListener("click", saveSystemConfigFromSide);
  $("#logoutBtn")?.addEventListener("click", logout);
  $("#systemConfigForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    saveSystemConfigFromSide();
  });
}

const pageRenderers = {
  dashboard: renderDashboardPage,
  profile: renderProfilePage,
  plan: renderPlanPage,
  task: renderTaskPage,
  record: renderRecordPage,
  ledger: renderLedgerPage,
  device: renderDevicePage,
  exception: renderExceptionPage,
  approval: renderApprovalPage,
  data: renderDataPage,
  users: renderUsersPage,
  logs: renderLogsPage,
  system: renderSystemPage,
};

function selectedAsset() {
  return state.assets.find((asset) => asset.id === state.selectedAssetId) || filteredAssets()[0] || state.assets[0] || null;
}

function selectedRecord() {
  return state.records.find((record) => record.id === state.selectedRecordId) || filteredRecords()[0] || state.records[0] || null;
}

function renderAssetSide(asset) {
  if (!asset) return `<div class="detail-head"><h2>资产详情</h2></div><div class="empty-state">暂无资产数据</div>`;
  if (!(asset.id in state.assetDetails)) loadAssetDetail(asset.id); // 未命中则后台拉历史+趋势，回来重渲染
  const detail = state.assetDetails[asset.id];
  const snapshots = detail ? detail.history : [];
  const trend = detail ? detail.trend : [];
  const total = detail ? detail.total : 0;
  // 照片仍从本地 records 集合里取（snapshots 只带元数据不带 images）
  const localHist = state.records.filter((record) => record.id === asset.lastRecordId || record.pointId === asset.pointId);
  const photos = collectAssetPhotos(asset, localHist);
  const trendHTML = trend.length ? `
    <div class="asset-trend">
      <h4>字段趋势 <small>近 90 天 · 后端规则计算</small></h4>
      <div class="trend-cards">${trend.map(renderTrendCard).join("")}</div>
    </div>` : "";
  // P1-2 电梯等状态类资产没数值趋势时,显示状态事件统计代替
  const events = detail && detail.events;
  const eventsHTML = (events && events.inspections > 0) ? renderStatusEventsCard(events) : "";
  return `
    <div class="detail-head asset-detail-head"><span>资产台账</span><h2>资产详情</h2></div>
    ${renderLoadErrorBanner(`assetDetail:${asset.id}`)}
    <div class="asset-card asset-card-v2">
      <section class="asset-side-hero">
        <div class="asset-photo">${photos[0] ? `<img src="${escapeHTML(photos[0])}" alt="">` : ""}</div>
        <div class="asset-side-intro">
          <div class="asset-title"><h3>${escapeHTML(asset.assetName || "未命名资产")}</h3><span class="pill ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</span></div>
          <div class="kv-list asset-side-kv">
            <div><span>设备编号</span><b>${escapeHTML(assetKey(asset))}</b></div>
            <div><span>设备类型</span><b>${escapeHTML(asset.assetType || "-")}</b></div>
            <div><span>安装位置</span><b>${escapeHTML(locationText(asset))}</b></div>
            <div><span>最近巡检</span><b>${fmtTime(asset.lastInspectedAt)}</b></div>
          </div>
        </div>
      </section>
      <div class="info-card asset-summary-card"><b>台账摘要</b><span>${escapeHTML(asset.lastSummary || "暂无管理摘要。")}</span></div>
      ${trendHTML}
      ${eventsHTML}
      <section class="asset-side-section">
        <div class="asset-side-section-head"><h3>巡检轨迹</h3><span>${total > 0 ? `共 ${total} 条，显示最近 5 条` : "历史记录"}</span></div>
        <table class="history-table">
          <thead><tr><th>巡检时间</th><th>巡检人</th><th>状态</th><th>结果摘要</th></tr></thead>
          <tbody>${snapshots.slice(0, 5).map((sn) => `
            <tr><td>${fmtTime(sn.createdAt)}</td><td>${escapeHTML(sn.inspector || "-")}</td><td><span class="status ${statusClass(sn.status)}">${escapeHTML(sn.status || "-")}</span></td><td>${escapeHTML(truncateText(sn.summary, 50))}</td></tr>
          `).join("") || emptyRow(4, detail ? "暂无历史巡检" : "正在加载历史…")}</tbody>
        </table>
      </section>
      <section class="asset-side-section">
        <div class="photo-strip" data-photos='${escapeHTML(JSON.stringify(photos))}'><h3>巡检照片 <small>共 ${photos.length} 张</small>${photos.length > 4 ? `<button class="link-btn photo-all-btn" type="button">查看全部 →</button>` : ""}</h3><div class="photos">${photos.slice(0, 4).map((url) => `<img src="${escapeHTML(url)}" alt="">`).join("") || `<div class="empty-photo"></div>`}</div></div>
      </section>
      <section class="asset-side-section asset-maintenance">
        <div class="asset-side-section-head"><h3>台账维护</h3><span>修改后保留审计记录</span></div>
        <form class="edit-form" id="assetEditForm">
          <label>资产名称<input name="assetName" value="${escapeHTML(asset.assetName || "")}"></label>
          <label>资产状态<select name="lastStatus">${["正常", "异常", "待复核", "待维修"].map((status) => `<option value="${status}" ${asset.lastStatus === status ? "selected" : ""}>${status}</option>`).join("")}</select></label>
          <label>管理摘要<textarea name="lastSummary">${escapeHTML(asset.lastSummary || "")}</textarea></label>
          <button class="primary" type="submit">保存台账修改</button>
        </form>
      </section>
    </div>
  `;
}

function renderRecordSide(record) {
  if (!record) return `<div class="detail-head"><h2>记录详情</h2></div><div class="empty-state">暂无巡检记录</div>`;
  const photos = collectPhotosFromRecord(record);
  if (!(record.id in state.confirmLogs)) loadConfirmLogs(record.id);
  const logs = state.confirmLogs[record.id] || [];
  // §4 复核留痕：展示最近 8 条字段确认/修正/标疑动作
  const confirmHTML = logs.length ? `
    <div class="info-card">
      <b>复核留痕 <small>共 ${logs.length} 次字段确认 · 防惰性留痕</small></b>
      <div class="confirm-logs">${logs.slice().reverse().slice(0, 8).map(renderConfirmLogRow).join("")}</div>
    </div>` : "";
  return `
    <div class="detail-head"><h2>记录详情</h2></div>
    ${renderLoadErrorBanner(`confirmLogs:${record.id}`)}
    <div class="side-stack">
      <div class="info-card"><b>AI 总结</b><span>${escapeHTML(record.aiSummary || record.report || "暂无总结")}</span></div>
      <div class="photo-row">${photos.slice(0, 2).map((url) => `<img src="${escapeHTML(url)}" alt="">`).join("")}</div>
      <table class="history-table"><thead><tr><th>字段</th><th>值</th><th>置信度</th></tr></thead><tbody>${(record.fields || []).slice(0, 8).map((field) => `<tr><td>${escapeHTML(field.label || field.code)}</td><td>${escapeHTML(field.value || field.aiValue || "-")}</td><td>${Math.round((field.confidence || 0) * 100)}%</td></tr>`).join("") || emptyRow(3, "暂无字段")}</tbody></table>
      ${confirmHTML}
    </div>
  `;
}

// §4 复核留痕单行：AI 原值 → 最终值，置信度，是否看图，停留时长，操作人
function renderConfirmLogRow(log) {
  const ACT = { confirm: "确认", correct: "修正", uncertain: "标疑" };
  const act = ACT[log.action] || log.action || "-";
  const actClass = log.action === "correct" ? "warning" : (log.action === "uncertain" ? "danger" : "normal");
  const conf = Math.round((log.aiConfidence || 0) * 100);
  const viewed = log.viewedPhoto ? "看图" : "未看图";
  const dur = log.durationMs ? `${(log.durationMs / 1000).toFixed(1)}s` : "-";
  return `
    <div class="confirm-row">
      <div class="confirm-row-head">
        <span class="confirm-label">${escapeHTML(log.fieldLabel || log.fieldKey || "-")}</span>
        <span class="status ${actClass}">${escapeHTML(act)}</span>
      </div>
      <div class="confirm-row-body">
        <span>AI <code>${escapeHTML(log.aiValue || "-")}</code> → 最终 <code>${escapeHTML(log.finalValue || "-")}</code></span>
        <span>置信度 ${conf}% · ${viewed} · 停留 ${dur}</span>
        <span class="confirm-meta">${escapeHTML(log.operator || "-")} · ${fmtTime(log.createdAt)}</span>
      </div>
    </div>
  `;
}

function renderExceptionSide(asset, request) {
  if (asset) return renderAssetSide(asset);
  if (request) return sideInfo("复核建议", [["申请原因", request.reason || "-"], ["处理方式", "通过审批后写入最终字段，保留 AI 原始识别。"]]);
  return sideInfo("复核建议", [["当前状态", "暂无待复核异常"], ["处理规则", "异常应进入台账，不覆盖原始 AI 识别结果。"]]);
}

function trendPanel(records, action = "") {
  return `
    <section class="panel chart-panel">
      <div class="panel-head">
        <div><h2>每日巡检记录</h2></div>
        <div class="panel-actions"><div class="legend"><span><i class="ok"></i>当日·正常</span><span><i class="warn"></i>当日·待复核</span><span><i class="bad"></i>当日·异常</span></div>${action}</div>
      </div>
      <div class="chart">${buildTrendSvg(records)}</div>
    </section>
  `;
}

function buildTrendSvg(records) {
  const days = Array.from({ length: 7 }, (_, index) => {
    const date = new Date();
    date.setDate(date.getDate() - (6 - index));
    return date.toISOString().slice(0, 10);
  });
  const series = {
    normal: days.map((day) => records.filter((record) => String(record.createdAt || "").slice(0, 10) === day && recordLevel(record) === "normal").length),
    warning: days.map((day) => records.filter((record) => String(record.createdAt || "").slice(0, 10) === day && recordLevel(record) === "warning").length),
    danger: days.map((day) => records.filter((record) => String(record.createdAt || "").slice(0, 10) === day && recordLevel(record) === "danger").length),
  };
  const all = [...series.normal, ...series.warning, ...series.danger];
  const max = Math.max(4, ...all);
  const width = 520;
  const height = 180;
  const pad = { left: 36, right: 22, top: 14, bottom: 28 };
  const point = (value, index) => {
    const x = pad.left + ((width - pad.left - pad.right) / Math.max(days.length - 1, 1)) * index;
    const y = pad.top + (height - pad.top - pad.bottom) - (value / max) * (height - pad.top - pad.bottom);
    return [x, y];
  };
  const line = (values) => values.map((value, index) => point(value, index).join(",")).join(" ");
  const ticks = 4;
  const gridLines = Array.from({ length: ticks + 1 }, (_, i) => {
    const ratio = i / ticks;
    const y = pad.top + (height - pad.top - pad.bottom) * (1 - ratio);
    const value = Math.round(max * ratio);
    return `<line class="grid" x1="${pad.left}" y1="${y}" x2="${width - pad.right}" y2="${y}"></line><text class="axis-y" x="${pad.left - 8}" y="${y + 4}" text-anchor="end">${value}</text>`;
  }).join("");
  const xLabels = days.map((day, index) => `<text class="axis-x" x="${point(0, index)[0]}" y="${height - 8}" text-anchor="middle">${day.slice(5).replace("-", "/")}</text>`).join("");
  const area = (values, fill) => {
    if (!values.some((v) => v > 0)) return "";
    const pts = values.map((value, index) => point(value, index).join(",")).join(" ");
    const first = point(values[0], 0);
    const last = point(values[values.length - 1], values.length - 1);
    return `<polygon class="trend-area" fill="${fill}" points="${pad.left},${height - pad.bottom} ${first[0]},${first[1]} ${pts.split(" ").slice(1).join(" ")} ${last[0]},${last[1]} ${width - pad.right},${height - pad.bottom}"></polygon>`;
  };
  const hotDots = (values, color, label) => values.map((value, index) => {
    const [x, y] = point(value, index);
    return `<g class="trend-dot"><circle class="dot-halo" cx="${x}" cy="${y}" r="9" fill="${color}" opacity="0"></circle><circle class="dot-core" cx="${x}" cy="${y}" r="3.5" fill="#ffffff" stroke="${color}" stroke-width="2"></circle><title>${days[index]} · ${label}：${value}</title></g>`;
  }).join("");
  return `
    <svg viewBox="0 0 ${width} ${height}" class="trend-svg" preserveAspectRatio="xMidYMid meet">
      <g class="grid-g">${gridLines}</g>
      <g class="axis-x-g">${xLabels}</g>
      ${area(series.normal, "rgba(18,169,104,0.10)")}
      <polyline points="${line(series.normal)}" fill="none" stroke="#12a968" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"></polyline>
      <polyline points="${line(series.warning)}" fill="none" stroke="#f59e0b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="4 4" opacity="0.85"></polyline>
      <polyline points="${line(series.danger)}" fill="none" stroke="#ef4b3f" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" stroke-dasharray="2 3" opacity="0.85"></polyline>
      ${hotDots(series.normal, "#12a968", "正常")}
      ${hotDots(series.warning, "#f59e0b", "待复核")}
      ${hotDots(series.danger, "#ef4b3f", "异常")}
    </svg>
  `;
}

function exceptionQueuePanel(assets, requests) {
  const items = [
    ...requests.filter((item) => item.status === "pending").map((request) => ({ title: `${statusLabel(request.targetType)}修改申请`, meta: `${request.requestedBy || "-"} · ${fmtTime(request.requestedAt)} · ${request.reason || "待审批"}`, level: "warning", requestId: request.id })),
    ...assets.filter((asset) => normalizeStatus(asset.lastStatus) !== "normal").map((asset) => ({ title: `${asset.assetName || "资产"}${asset.lastStatus || "异常"}`, meta: `${assetKey(asset)} · ${fmtTime(asset.lastInspectedAt)} · ${asset.lastSummary || "待复核"}`, level: statusClass(asset.lastStatus), assetId: asset.id })),
  ].slice(0, 5);
  return `
    <section class="panel queue-panel">
      <div class="panel-head"><h2>异常复核队列</h2><button class="link-btn" data-page-link="exception">全部</button></div>
      <div class="queue-list">${items.map((item) => `<div class="queue-item" ${item.assetId ? `data-asset-select="${escapeHTML(item.assetId)}"` : ""} ${item.requestId ? `data-request-open="${escapeHTML(item.requestId)}"` : ""}><i class="dot ${item.level === "danger" ? "danger" : "warning"}"></i><div><span class="queue-title">${escapeHTML(item.title)}</span><span class="queue-meta">${escapeHTML(item.meta)}</span></div><button class="queue-action">待复核</button></div>`).join("") || `<div class="empty-state">暂无异常复核任务</div>`}</div>
    </section>
  `;
}

function recordListPanel(records, title) {
  return `
    <section class="panel timeline-panel">
      <div class="panel-head"><h2>${escapeHTML(title)}</h2><button class="link-btn" data-page-link="record">全部记录</button></div>
      <div class="timeline-list">${records.map((record) => `<div class="timeline-item" data-record-select="${escapeHTML(record.id)}"><i class="dot ${recordLevel(record) === "danger" ? "danger" : recordLevel(record) === "warning" ? "warning" : ""}"></i><div><span class="timeline-title">${escapeHTML(record.inspector || "巡检员")} · ${escapeHTML(recordStatus(record))}</span><span class="timeline-meta">${fmtTime(record.createdAt)} | ${escapeHTML(record.pointName || record.templateName || "-")}</span></div><div class="timeline-photos">${collectPhotosFromRecord(record).slice(0, 2).map((url) => `<img src="${escapeHTML(url)}" alt="">`).join("")}</div></div>`).join("") || `<div class="empty-state">暂无巡检记录</div>`}</div>
    </section>
  `;
}

function reportCard(title, desc) {
  return `<article><b>${escapeHTML(title)}</b><span>${escapeHTML(desc)}</span><button>预览</button></article>`;
}

function systemCard(title, desc, status, level) {
  return `
    <article class="system-card ${escapeHTML(level)}">
      <div class="system-card-top">
        <b>${escapeHTML(title)}</b>
        <em class="status ${escapeHTML(level)}">${escapeHTML(status)}</em>
      </div>
      <p>${escapeHTML(desc)}</p>
    </article>
  `;
}

function steps(labels, activeIndex) {
  return `<div class="steps">${labels.map((label, index) => `<span class="${index < activeIndex ? "done" : index === activeIndex ? "active" : ""}">${escapeHTML(label)}</span>`).join("")}</div>`;
}

function emptyRow(colspan, text) {
  return `<tr><td colspan="${colspan}"><div class="empty-state small">${escapeHTML(text)}</div></td></tr>`;
}

function ratio(value, total) {
  return total ? Math.round((value / total) * 100) : 0;
}

function aiAccuracy(records) {
  const fields = records.flatMap((record) => record.fields || []);
  const sample = fields.filter((field) => field.value && field.aiValue).length;
  if (sample === 0) return { value: 0, sample: 0 };
  const confirmed = fields.filter((field) => field.value && field.aiValue && String(field.value) === String(field.aiValue)).length;
  return { value: Math.round((confirmed / sample) * 100), sample };
}

function closedRate() {
  const requests = filteredRequests();
  if (!requests.length) return 100;
  const closed = requests.filter((item) => item.status === "approved" || item.status === "rejected").length;
  return Math.round((closed / requests.length) * 100);
}

function buildReportPreview(records, assets) {
  const counts = statusCounts(assets);
  return `当前共沉淀 ${assets.length} 项资产、${records.length} 条巡检记录；正常资产 ${counts.normal} 项，异常/待复核 ${counts.warning + counts.danger + counts.repair} 项。系统保留 AI 原始识别、人工确认和审批痕迹，可作为日报与台账依据。`;
}

function systemConfigForm() {
  return `
    <form class="edit-form" id="systemConfigForm">
      <label>后端 API 地址<input name="apiBase" value="${escapeHTML(state.apiBase)}"></label>
      <label>访问令牌<input name="token" type="password" value="${escapeHTML(state.token)}" placeholder="本地无令牌可留空"></label>
      <button class="primary" type="submit">保存配置</button>
    </form>
  `;
}

function saveSystemConfigFromSide() {
  const form = $("#systemConfigForm");
  if (!form) return;
  const data = new FormData(form);
  state.apiBase = String(data.get("apiBase") || "").trim().replace(/\/$/, "") || state.apiBase;
  state.token = String(data.get("token") || "").trim();
  localStorage.setItem(API_BASE_KEY, state.apiBase);
  localStorage.setItem(API_TOKEN_KEY, state.token);
  toast("系统配置已保存");
  loadData(false);
}

async function saveAsset(id, body) {
  await api(`/api/assets/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(body),
  });
  toast("资产台账已更新");
  await loadData(false);
}

async function setAssetNormal(id) {
  const asset = state.assets.find((item) => item.id === id);
  if (!asset) return;
  await saveAsset(id, { assetName: asset.assetName, lastStatus: "正常", lastSummary: asset.lastSummary || "后台复核后标记正常。" });
}

async function reviewRequest(id, action) {
  await api(`/api/change-requests/${encodeURIComponent(id)}/${action}`, {
    method: "POST",
    body: JSON.stringify({ reviewNote: action === "approve" ? "后台审批通过" : "后台审批拒绝" }),
  });
  toast(action === "approve" ? "申请已通过" : "申请已驳回");
  closeDrawer();
  await loadData(false);
}

function openDrawer(title, html) {
  $("#drawerBody").innerHTML = `<h2>${escapeHTML(title)}</h2>${html}`;
  $("#drawerMask").hidden = false;
  $("#drawer").hidden = false;
  normalizeButtons($("#drawer"));
}

function closeDrawer() {
  $("#drawerMask").hidden = true;
  $("#drawer").hidden = true;
  $("#drawerBody").innerHTML = "";
}

function openRequestDrawer(id) {
  const request = state.requests.find((item) => item.id === id);
  if (!request) return;
  openDrawer("修改申请详情", `
    <p>${escapeHTML(statusLabel(request.status))} · ${escapeHTML(request.requestedBy || "-")} · ${fmtTime(request.requestedAt)}</p>
    <table class="history-table"><tbody>
      <tr><th>目标类型</th><td>${escapeHTML(statusLabel(request.targetType))}</td></tr>
      <tr><th>目标 ID</th><td>${escapeHTML(request.targetId || "-")}</td></tr>
      <tr><th>申请理由</th><td>${escapeHTML(request.reason || "-")}</td></tr>
      <tr><th>复核备注</th><td>${escapeHTML(request.reviewNote || "-")}</td></tr>
    </tbody></table>
    <h3>变更内容</h3><pre>${escapeHTML(JSON.stringify(request.patch || {}, null, 2))}</pre>
    ${request.status === "pending" ? `<div class="drawer-actions"><button class="primary" data-request-review="${escapeHTML(request.id)}" data-action="approve">通过申请</button><button class="danger-btn" data-request-review="${escapeHTML(request.id)}" data-action="reject">拒绝申请</button></div>` : ""}
  `);
}

function optionHTML(value, label, selected = "") {
  return `<option value="${escapeHTML(value)}" ${String(value) === String(selected) ? "selected" : ""}>${escapeHTML(label)}</option>`;
}

function ownerOptions(selected = "") {
  const names = [
    state.currentUser?.displayName,
    ...state.users.map((user) => user.displayName || user.username),
    "张三",
    "巡检员",
  ].filter(Boolean);
  return [...new Set(names)].map((name) => optionHTML(name, name, selected)).join("");
}

function defaultDateTimeInput(addHours = 24) {
  const date = new Date(Date.now() + addHours * 60 * 60 * 1000);
  date.setMinutes(0, 0, 0);
  const pad = (num) => String(num).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function displayDateTimeInput(value) {
  return String(value || "").replace("T", " ") || "-";
}

function openPlanDrawer() {
  const projectList = projects();
  const pointList = state.points.length ? state.points : [{ id: "", name: "未配置点位", location: "请先维护点位", project: state.selectedProject || projectList[0] || "" }];
  const templateList = state.templates.length ? state.templates : [{ id: "", name: "默认日报模板", project: state.selectedProject || projectList[0] || "", assetType: "-" }];
  openDrawer("新建巡检计划", `
    <form class="drawer-form" id="adminPlanForm">
      <section class="drawer-section">
        <h3>计划信息</h3>
        <div class="form-grid">
          <label>计划名称<input name="name" placeholder="如：能耗抄表每日巡检" required></label>
          <label>所属项目<select name="project" required>${projectList.map((name) => optionHTML(name, name, state.selectedProject)).join("") || optionHTML("默认项目", "默认项目")}</select></label>
          <label>巡检点位<select name="pointId">${pointList.map((point) => optionHTML(point.id, `${point.project || "-"} · ${point.name || point.location || "-"}`)).join("")}</select></label>
          <label>日报模板<select name="templateId">${templateList.map((tpl) => optionHTML(tpl.id, `${tpl.name || "-"} / ${tpl.assetType || "-"}`)).join("")}</select></label>
          <label>执行频次<select name="frequency" required>
            ${["每日 09:00", "每日 18:00", "每周一 09:00", "每月 1 日 09:00", "临时计划"].map((item) => optionHTML(item, item)).join("")}
          </select></label>
          <label>首次执行<input name="firstRun" type="datetime-local" value="${defaultDateTimeInput(18)}" required></label>
          <label>责任人<select name="owner">${ownerOptions(state.currentUser?.displayName || "张三")}</select></label>
          <label>AI 策略<select name="aiPolicy">
            ${["视觉识别 + 人工确认", "视觉识别 + 异常复核", "仅人工填写", "先识别失败再人工兜底"].map((item) => optionHTML(item, item)).join("")}
          </select></label>
        </div>
      </section>
      <section class="drawer-section">
        <h3>执行要求</h3>
        <label>计划说明<textarea name="remark" placeholder="填写现场要求、拍照标准、异常处理口径。"></textarea></label>
        <div class="drawer-note">
          <b>生产口径</b><span>计划对象应绑定项目、点位、模板、频次和责任人；移动端按计划生成可执行任务，巡检结果回写巡检记录和资产台账。</span>
        </div>
      </section>
      <div class="drawer-actions">
        <button class="btn-ghost" type="button" data-drawer-cancel>取消</button>
        <button class="primary" type="submit">保存计划</button>
      </div>
    </form>
  `);
}

function openTaskDrawer() {
  const plans = planRows();
  openDrawer("派发巡检任务", `
    <form class="drawer-form" id="adminTaskForm">
      <section class="drawer-section">
        <h3>任务来源</h3>
        <div class="form-grid">
          <label>来源计划<select name="planIndex" required>${plans.map((plan, index) => optionHTML(index, `${plan.project} · ${plan.name} · ${plan.point}`)).join("")}</select></label>
          <label>任务标题<input name="title" placeholder="留空则按计划自动生成"></label>
          <label>巡检人<select name="owner">${ownerOptions("巡检员")}</select></label>
          <label>截止时间<input name="dueAt" type="datetime-local" value="${defaultDateTimeInput(8)}" required></label>
          <label>优先级<select name="priority">${["普通", "紧急", "高优先级"].map((item) => optionHTML(item, item)).join("")}</select></label>
          <label>任务状态<select name="status">${["待执行", "进行中"].map((item) => optionHTML(item, item)).join("")}</select></label>
        </div>
      </section>
      <section class="drawer-section">
        <h3>识别与复核要求</h3>
        <div class="form-grid">
          <label>AI 质量阈值<select name="qualityGate">${["低于 90% 进入复核", "低于 95% 进入复核", "识别失败三次转人工"].map((item) => optionHTML(item, item)).join("")}</select></label>
          <label>拍照要求<select name="photoRule">${["必须上传现场照片", "至少 2 张照片", "按模板要求上传"].map((item) => optionHTML(item, item)).join("")}</select></label>
        </div>
        <label>执行备注<textarea name="remark" placeholder="如：重点核对表盘读数、小数点位置和设备编号。"></textarea></label>
        <div class="drawer-note">
          <b>派发口径</b><span>任务应明确计划、点位、巡检人、截止时间、AI 质量阈值和复核规则，避免只生成一句说明。</span>
        </div>
      </section>
      <div class="drawer-actions">
        <button class="btn-ghost" type="button" data-drawer-cancel>取消</button>
        <button class="primary" type="submit">确认派发</button>
      </div>
    </form>
  `);
}

function openGenericDrawer(type) {
  if (type === "plan") return openPlanDrawer();
  if (type === "task") return openTaskDrawer();
  const copy = {
    device: ["新增设备", "设备档案应包含编号、名称、类型、点位、模板、状态和二维码绑定。"],
    exception: ["批量处理", "批量处理需保留复核结果和处理人，不直接覆盖 AI 原始识别。"],
    approval: ["审批设置", "审批流建议保留申请人、审批人、变更前值、变更后值、处理意见。"],
  }[type] || ["说明", "该能力属于后续生产增强项。"];
  openDrawer(copy[0], `<p>${escapeHTML(copy[1])}</p>`);
}

function createAdminPlan(form) {
  const template = state.templates.find((tpl) => tpl.id === form.get("templateId")) || {};
  const point = state.points.find((item) => item.id === form.get("pointId")) || {};
  const project = String(form.get("project") || point.project || template.project || "默认项目");
  const plan = {
    id: clientId("plan"),
    name: String(form.get("name") || template.name || point.name || "巡检计划").trim(),
    project,
    pointId: point.id || "",
    pointName: point.name || point.location || "未配置点位",
    templateId: template.id || "",
    templateName: template.name || "默认日报模板",
    frequency: String(form.get("frequency") || "每日 09:00"),
    owner: String(form.get("owner") || "巡检员"),
    nextRun: displayDateTimeInput(form.get("firstRun")),
    aiPolicy: String(form.get("aiPolicy") || "视觉识别 + 人工确认"),
    remark: String(form.get("remark") || "").trim(),
    status: "启用",
    createdAt: new Date().toISOString(),
  };
  state.customPlans.unshift(plan);
  saveLocalArray(ADMIN_PLANS_KEY, state.customPlans);
  renderProjectOptions();
  closeDrawer();
  setPage("plan", false);
  toast("巡检计划已保存");
}

function createAdminTask(form) {
  const plans = planRows();
  const plan = plans[Number(form.get("planIndex"))] || {};
  const task = {
    id: clientId("task"),
    title: String(form.get("title") || `${plan.name || "巡检"}任务`).trim(),
    planName: plan.name || "",
    project: plan.project || state.selectedProject || "默认项目",
    pointName: plan.point || "-",
    templateName: plan.templateName || "",
    owner: String(form.get("owner") || "巡检员"),
    dueAt: displayDateTimeInput(form.get("dueAt")),
    priority: String(form.get("priority") || "普通"),
    status: String(form.get("status") || "待执行"),
    qualityGate: String(form.get("qualityGate") || ""),
    photoRule: String(form.get("photoRule") || ""),
    remark: String(form.get("remark") || "").trim(),
    createdAt: new Date().toISOString(),
  };
  state.customTasks.unshift(task);
  saveLocalArray(ADMIN_TASKS_KEY, state.customTasks);
  closeDrawer();
  setPage("task", false);
  toast("巡检任务已派发");
}

function fieldSummary(record) {
  return (record.fields || [])
    .map((field) => `${field.label || field.code}：${field.value || field.aiValue || "-"}${field.needsReview ? "（需复核）" : ""}`)
    .join("；");
}

function recommendationSummary(record) {
  return (record.aiRecommendations || [])
    .map((item) => `${item.category || item.priority || "建议"}：${item.text || ""}`)
    .filter(Boolean)
    .join("；");
}

function xmlEscape(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

function xlsxCellText(value) {
  const text = String(value ?? "");
  return /^[=+\-@]/.test(text) ? `'${text}` : text;
}

function xlsxColumnName(index) {
  let name = "";
  let n = index;
  while (n > 0) {
    const mod = (n - 1) % 26;
    name = String.fromCharCode(65 + mod) + name;
    n = Math.floor((n - mod) / 26);
  }
  return name;
}

function crc32(bytes) {
  if (!crc32.table) {
    crc32.table = Array.from({ length: 256 }, (_, n) => {
      let c = n;
      for (let k = 0; k < 8; k += 1) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      return c >>> 0;
    });
  }
  let crc = 0xffffffff;
  for (const byte of bytes) crc = crc32.table[(crc ^ byte) & 0xff] ^ (crc >>> 8);
  return (crc ^ 0xffffffff) >>> 0;
}

function u16(value) {
  return Uint8Array.of(value & 0xff, (value >>> 8) & 0xff);
}

function u32(value) {
  return Uint8Array.of(value & 0xff, (value >>> 8) & 0xff, (value >>> 16) & 0xff, (value >>> 24) & 0xff);
}

function concatBytes(parts) {
  const total = parts.reduce((sum, part) => sum + part.length, 0);
  const out = new Uint8Array(total);
  let offset = 0;
  for (const part of parts) {
    out.set(part, offset);
    offset += part.length;
  }
  return out;
}

function zipDateTime(date = new Date()) {
  const year = Math.max(1980, date.getFullYear());
  return {
    time: (date.getHours() << 11) | (date.getMinutes() << 5) | Math.floor(date.getSeconds() / 2),
    date: ((year - 1980) << 9) | ((date.getMonth() + 1) << 5) | date.getDate(),
  };
}

function makeZip(files) {
  const encoder = new TextEncoder();
  const now = zipDateTime();
  const localParts = [];
  const centralParts = [];
  let offset = 0;
  for (const file of files) {
    const nameBytes = encoder.encode(file.name);
    const contentBytes = typeof file.content === "string" ? encoder.encode(file.content) : file.content;
    const crc = crc32(contentBytes);
    const localHeader = concatBytes([
      u32(0x04034b50), u16(20), u16(0x0800), u16(0), u16(now.time), u16(now.date),
      u32(crc), u32(contentBytes.length), u32(contentBytes.length), u16(nameBytes.length), u16(0), nameBytes,
    ]);
    localParts.push(localHeader, contentBytes);
    const centralHeader = concatBytes([
      u32(0x02014b50), u16(20), u16(20), u16(0x0800), u16(0), u16(now.time), u16(now.date),
      u32(crc), u32(contentBytes.length), u32(contentBytes.length), u16(nameBytes.length), u16(0), u16(0),
      u16(0), u16(0), u32(0), u32(offset), nameBytes,
    ]);
    centralParts.push(centralHeader);
    offset += localHeader.length + contentBytes.length;
  }
  const central = concatBytes(centralParts);
  const end = concatBytes([
    u32(0x06054b50), u16(0), u16(0), u16(files.length), u16(files.length), u32(central.length), u32(offset), u16(0),
  ]);
  return concatBytes([...localParts, central, end]);
}

function makeWorksheetXML(title, headers, rows, metaRows) {
  const colCount = headers.length;
  const now = new Date().toLocaleString("zh-CN", { hour12: false });
  const sheetRows = [
    { values: [title], style: 1, merge: true },
    { values: [`导出时间：${now}　数据范围：${state.selectedProject || "全部项目"}　记录数：${rows.length}`], style: 2, merge: true },
    ...metaRows.map(([key, value]) => ({ values: [`${key}：${value}`], style: 2, merge: true })),
    { values: headers, style: 3 },
    ...rows.map((row) => ({ values: row, style: 0 })),
  ];
  const lastCol = xlsxColumnName(colCount);
  const headerRow = 3 + metaRows.length;
  const widths = headers.map((header, colIndex) => {
    const max = Math.max(
      String(header || "").length,
      ...rows.slice(0, 80).map((row) => String(row[colIndex] ?? "").length),
    );
    return Math.max(10, Math.min(36, Math.ceil(max * 1.45)));
  });
  const rowXml = sheetRows.map((row, rowIndex) => {
    const r = rowIndex + 1;
    const cells = Array.from({ length: colCount }, (_, colIndex) => {
      const value = colIndex < row.values.length ? xlsxCellText(row.values[colIndex]) : "";
      const ref = `${xlsxColumnName(colIndex + 1)}${r}`;
      if (!value && row.merge && colIndex > 0) return "";
      return `<c r="${ref}" t="inlineStr" s="${row.style}"><is><t xml:space="preserve">${xmlEscape(value)}</t></is></c>`;
    }).join("");
    return `<row r="${r}">${cells}</row>`;
  }).join("");
  const merges = sheetRows
    .map((row, index) => row.merge ? `<mergeCell ref="A${index + 1}:${lastCol}${index + 1}"/>` : "")
    .filter(Boolean)
    .join("");
  return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <dimension ref="A1:${lastCol}${sheetRows.length}"/>
  <sheetViews><sheetView workbookViewId="0"><pane ySplit="${headerRow}" topLeftCell="A${headerRow + 1}" activePane="bottomLeft" state="frozen"/></sheetView></sheetViews>
  <sheetFormatPr defaultRowHeight="20"/>
  <cols>${widths.map((width, index) => `<col min="${index + 1}" max="${index + 1}" width="${width}" customWidth="1"/>`).join("")}</cols>
  <sheetData>${rowXml}</sheetData>
  ${merges ? `<mergeCells count="${sheetRows.filter((row) => row.merge).length}">${merges}</mergeCells>` : ""}
</worksheet>`;
}

function exportExcel(name, title, headers, rows, metaRows = []) {
  const worksheet = makeWorksheetXML(title, headers, rows, metaRows);
  const created = new Date().toISOString();
  const files = [
    { name: "[Content_Types].xml", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/docProps/app.xml" ContentType="application/vnd.openxmlformats-officedocument.extended-properties+xml"/><Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/><Override PartName="/xl/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.styles+xml"/></Types>` },
    { name: "_rels/.rels", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/><Relationship Id="rId3" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/extended-properties" Target="docProps/app.xml"/></Relationships>` },
    { name: "docProps/app.xml", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Properties xmlns="http://schemas.openxmlformats.org/officeDocument/2006/extended-properties" xmlns:vt="http://schemas.openxmlformats.org/officeDocument/2006/docPropsVTypes"><Application>JADEAST InspectAI</Application></Properties>` },
    { name: "docProps/core.xml", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:dcmitype="http://purl.org/dc/dcmitype/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:creator>JADEAST 智巡</dc:creator><dc:title>${xmlEscape(title)}</dc:title><dcterms:created xsi:type="dcterms:W3CDTF">${created}</dcterms:created><dcterms:modified xsi:type="dcterms:W3CDTF">${created}</dcterms:modified></cp:coreProperties>` },
    { name: "xl/workbook.xml", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="${xmlEscape(title.slice(0, 31) || "Sheet1")}" sheetId="1" r:id="rId1"/></sheets></workbook>` },
    { name: "xl/_rels/workbook.xml.rels", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/><Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>` },
    { name: "xl/styles.xml", content: `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><styleSheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><fonts count="3"><font><sz val="11"/><name val="Microsoft YaHei"/></font><font><b/><sz val="16"/><color rgb="FFFFFFFF"/><name val="Microsoft YaHei"/></font><font><b/><sz val="11"/><color rgb="FFFFFFFF"/><name val="Microsoft YaHei"/></font></fonts><fills count="5"><fill><patternFill patternType="none"/></fill><fill><patternFill patternType="gray125"/></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0F2740"/><bgColor indexed="64"/></patternFill></fill><fill><patternFill patternType="solid"><fgColor rgb="FFEFF6FF"/><bgColor indexed="64"/></patternFill></fill><fill><patternFill patternType="solid"><fgColor rgb="FF0F2740"/><bgColor indexed="64"/></patternFill></fill></fills><borders count="2"><border><left/><right/><top/><bottom/><diagonal/></border><border><left style="thin"><color rgb="FFCBD5E1"/></left><right style="thin"><color rgb="FFCBD5E1"/></right><top style="thin"><color rgb="FFCBD5E1"/></top><bottom style="thin"><color rgb="FFCBD5E1"/></bottom><diagonal/></border></borders><cellStyleXfs count="1"><xf numFmtId="49" fontId="0" fillId="0" borderId="0"/></cellStyleXfs><cellXfs count="4"><xf numFmtId="49" fontId="0" fillId="0" borderId="1" xfId="0" applyBorder="1" applyAlignment="1"><alignment vertical="top" wrapText="1"/></xf><xf numFmtId="49" fontId="1" fillId="2" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment horizontal="left" vertical="center"/></xf><xf numFmtId="49" fontId="0" fillId="3" borderId="1" xfId="0" applyFill="1" applyBorder="1" applyAlignment="1"><alignment vertical="center" wrapText="1"/></xf><xf numFmtId="49" fontId="2" fillId="4" borderId="1" xfId="0" applyFont="1" applyFill="1" applyBorder="1" applyAlignment="1"><alignment horizontal="center" vertical="center" wrapText="1"/></xf></cellXfs><cellStyles count="1"><cellStyle name="Normal" xfId="0" builtinId="0"/></cellStyles></styleSheet>` },
    { name: "xl/worksheets/sheet1.xml", content: worksheet },
  ];
  const blob = new Blob([makeZip(files)], { type: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${name}-${new Date().toISOString().slice(0, 10)}.xlsx`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function exportAssetsWorkbook() {
  const rows = filteredAssets();
  exportExcel(
    "智巡-资产台账",
    "JADEAST 智巡资产台账",
    ["序号", "资产ID", "设备编号", "设备名称", "设备类型", "所属项目", "安装位置", "当前状态", "状态等级", "最近巡检时间", "最近巡检人", "巡检次数", "管理摘要"],
    rows.map((asset, index) => [
      index + 1,
      asset.id,
      assetKey(asset),
      asset.assetName,
      asset.assetType,
      asset.project || asset.projectCode,
      locationText(asset),
      asset.lastStatus,
      asset.statusLevel || normalizeStatus(asset.lastStatus),
      fmtTime(asset.lastInspectedAt),
      asset.lastInspector || "-",
      asset.inspectionCount || 0,
      asset.lastSummary,
    ]),
    [
      ["表格说明", "资产台账记录资产当前状态；巡检记录记录每一次现场事实，两者不可混用"],
      ["导出口径", "按当前项目筛选和台账筛选条件导出"],
    ],
  );
  toast(`已导出 ${rows.length} 条资产台账 Excel`);
}

function exportRecordsWorkbook() {
  const rows = state.page === "record" ? filteredRecordRows() : filteredRecords();
  exportExcel(
    "智巡-巡检记录",
    "JADEAST 智巡巡检记录",
    ["序号", "记录ID", "巡检时间", "所属项目", "巡检点位", "日报模板", "巡检人", "识别状态", "提交状态", "拍照次数", "主要识别", "字段明细", "AI 总结", "AI 建议"],
    rows.map((record, index) => [
      index + 1,
      record.id,
      fmtTime(record.createdAt),
      record.project,
      record.pointName,
      record.templateName,
      record.inspector,
      statusLabel(record.recognitionStatus),
      record.submitted ? "已提交" : "未提交",
      record.captureAttempts || 0,
      primaryReading(record),
      fieldSummary(record),
      record.aiSummary || record.report,
      recommendationSummary(record),
    ]),
    [
      ["表格说明", "巡检记录保留每一次拍照识别、人工确认和 AI 总结，不直接覆盖资产台账"],
      ["导出口径", "按当前项目筛选和当前页记录范围导出"],
    ],
  );
  toast(`已导出 ${rows.length} 条巡检记录 Excel`);
}

function bindEvents() {
  $$(".nav button").forEach((btn) => btn.addEventListener("click", () => setPage(btn.dataset.page)));
  $("#projectSelect").addEventListener("change", (event) => {
    state.selectedProject = event.target.value;
    state.selectedAssetId = filteredAssets()[0]?.id || "";
    state.selectedRecordId = filteredRecords()[0]?.id || "";
    render();
  });
  $("#refreshBtn").addEventListener("click", () => loadData());
  $("#systemConfigBtn").addEventListener("click", () => setPage("system"));
  $("#drawerClose").addEventListener("click", closeDrawer);
  $("#drawerMask").addEventListener("click", closeDrawer);
  window.addEventListener("popstate", () => {
    state.page = new URLSearchParams(location.search).get("page") || "dashboard";
    render();
  });

  document.addEventListener("click", async (event) => {
    // U4 · 查看全部照片 / 收起
    const photoAllBtn = event.target.closest(".photo-all-btn");
    if (photoAllBtn) {
      const strip = photoAllBtn.closest(".photo-strip");
      const grid = strip?.querySelector(".photos");
      if (strip && grid) {
        const photos = JSON.parse(strip.dataset.photos || "[]");
        const expanded = strip.classList.toggle("expanded");
        grid.innerHTML = (expanded ? photos : photos.slice(0, 4))
          .map((url) => `<img src="${url.replace(/"/g, "&quot;")}" alt="">`).join("") || `<div class="empty-photo"></div>`;
        photoAllBtn.textContent = expanded ? "收起 ↑" : "查看全部 →";
      }
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    // U3 · 资产详情 tab 切换（cosmetic）
    const detailTab = event.target.closest(".detail-tabs button");
    if (detailTab) {
      detailTab.parentElement.querySelectorAll("button").forEach((b) => b.classList.remove("active"));
      detailTab.classList.add("active");
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    // P5 · admin lightbox — 资产详情大图 / 巡检照片
    const lightboxImg = event.target.closest(".asset-card .photo-strip .photos img, .photo-strip .photos img, .asset-photo img, .photo-row img, .timeline-photos img, .dash-rec-thumb img, .dash-asset-photo img");
    if (lightboxImg && !event.target.closest(".admin-lightbox")) {
      openAdminLightbox(lightboxImg.src);
      event.preventDefault();
      event.stopPropagation();
      return;
    }
    if (event.target.closest(".admin-lightbox")) {
      closeAdminLightbox();
      return;
    }
    if (event.target.closest("[data-drawer-cancel]")) {
      event.preventDefault();
      event.stopPropagation();
      closeDrawer();
      return;
    }
    const pageLink = event.target.closest("[data-page-link]")?.dataset.pageLink;
    const assetId = event.target.closest("[data-asset-select]")?.dataset.assetSelect;
    const recordId = event.target.closest("[data-record-select]")?.dataset.recordSelect;
    const requestId = event.target.closest("[data-request-open]")?.dataset.requestOpen;
    const drawerType = event.target.closest("[data-drawer]")?.dataset.drawer;
    const normalId = event.target.closest("[data-asset-normal]")?.dataset.assetNormal;
    const reviewBtn = event.target.closest("[data-request-review]");
    const userBadge = event.target.closest(".admin-user");
    if (reviewBtn) {
      event.preventDefault();
      event.stopPropagation();
      await reviewRequest(reviewBtn.dataset.requestReview, reviewBtn.dataset.action || "approve");
      return;
    }
    if (normalId) {
      event.preventDefault();
      event.stopPropagation();
      await setAssetNormal(normalId);
      return;
    }
    if (drawerType) {
      event.preventDefault();
      event.stopPropagation();
      openGenericDrawer(drawerType);
      return;
    }
    if (pageLink) {
      event.preventDefault();
      event.stopPropagation();
      setPage(pageLink);
      return;
    }
    if (userBadge) {
      event.preventDefault();
      event.stopPropagation();
      setPage("profile");
      return;
    }
    if (assetId) {
      event.preventDefault();
      event.stopPropagation();
      state.selectedAssetId = assetId;
      if (state.page !== "ledger" && state.page !== "device" && state.page !== "exception") setPage("ledger");
      else render();
      return;
    }
    if (recordId) {
      event.preventDefault();
      event.stopPropagation();
      state.selectedRecordId = recordId;
      if (state.page !== "record") setPage("record");
      else render();
      return;
    }
    if (requestId) {
      event.preventDefault();
      event.stopPropagation();
      openRequestDrawer(requestId);
      return;
    }
  });

  document.addEventListener("submit", async (event) => {
    if (event.target.id === "loginForm") {
      event.preventDefault();
      const form = new FormData(event.target);
      try {
        await login(String(form.get("username") || ""), String(form.get("password") || ""));
        await loadData(false);
      } catch (error) {
        renderLogin(error.message);
      }
      return;
    }
    if (event.target.id === "adminPlanForm") {
      event.preventDefault();
      createAdminPlan(new FormData(event.target));
      return;
    }
    if (event.target.id === "adminTaskForm") {
      event.preventDefault();
      createAdminTask(new FormData(event.target));
      return;
    }
    if (event.target.id !== "assetEditForm") return;
    event.preventDefault();
    const asset = selectedAsset();
    if (!asset) return;
    const form = new FormData(event.target);
    await saveAsset(asset.id, {
      assetName: form.get("assetName"),
      lastStatus: form.get("lastStatus"),
      lastSummary: form.get("lastSummary"),
    });
  });
}

async function init() {
  bindEvents();
  if (!state.token) {
    renderLogin();
    return;
  }
  setPage(state.page, false);
  try {
    await loadMe();
    await loadData(false);
    // M30 · 回填：保证启用计划都有对应任务，回填完重渲染
    let synced = 0;
    (state.customPlans || []).forEach((p) => {
      if (p.status === "启用") { syncPlanTask(p); synced++; }
    });
    updateUserBadge();
    if (synced) render();
  } catch (error) {
    if (state.token) {
      state.token = "";
      localStorage.removeItem(API_TOKEN_KEY);
    }
    renderLogin(error.message);
  }
}

init();
