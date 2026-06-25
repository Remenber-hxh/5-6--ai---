const API_BASE_KEY = "inspectai_admin_api_base";
const API_TOKEN_KEY = "inspectai_admin_token";
const ADMIN_PLANS_KEY = "inspectai_admin_plans";
const ADMIN_TASKS_KEY = "inspectai_admin_tasks";

const pageLabels = {
  dashboard: "首页",
  profile: "个人首页",
  plan: "巡检计划",
  record: "巡检记录",
  ledger: "资产台账",
  approval: "审批中心",
  data: "数据看板",
  report: "统计报表",
  users: "用户与权限",
  logs: "操作日志",
  system: "系统管理",
};

function normalizedPage(page) {
  if (page === "task") return "plan";
  if (page === "exception") return "approval";
  if (page === "device") return "ledger";
  return pageLabels[page] ? page : "dashboard";
}

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

function initialApiBase() {
  const fallback = defaultApiBase();
  const stored = localStorage.getItem(API_BASE_KEY);
  const { hostname, port } = window.location;
  const isLocalAdmin = port === "18081" && ["127.0.0.1", "localhost"].includes(hostname);
  if (isLocalAdmin) {
    if (stored !== fallback) localStorage.setItem(API_BASE_KEY, fallback);
    return fallback;
  }
  if (!stored) return fallback;
  try {
    const url = new URL(stored);
    return url.origin;
  } catch {
    localStorage.removeItem(API_BASE_KEY);
    return fallback;
  }
}

const state = {
  apiBase: initialApiBase(),
  token: localStorage.getItem(API_TOKEN_KEY) || "",
  page: normalizedPage(new URLSearchParams(location.search).get("page") || "dashboard"),
  assets: [],
  records: [],
  requests: [],
  points: [],
  templates: [],
  users: [],
  operationLogs: [],
  engineeringPlans: [],
  engineeringTasks: [],
  customPlans: loadLocalArray(ADMIN_PLANS_KEY),
  customTasks: loadLocalArray(ADMIN_TASKS_KEY),
  currentUser: null,
  health: null,
  selectedProject: "",
  selectedAssetId: "",
  selectedRecordId: "",
  selectedPlanStatus: "",
  selectedPlanTaskKey: "",
  selectedPlanTaskIndex: -1,
  assetDetails: {}, // §3 资产详情缓存：{ id → { history, total, page, pageSize, trend } }
  confirmLogs: {},  // §4 记录的字段确认留痕：{ recordId → [log,...] }
  dataInsights: {}, // 阶段一 数据看板缓存：{ key → { overview, items, summary, model, generatedAt } }
  loadErrors: {},   // P1-4 接口失败留痕：{ "scope:id" → 错误消息 }；为空表示无错
  handledDeepLink: "",
  approvalStatus: new URLSearchParams(location.search).get("approvalStatus") || "pending",
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

function businessCode(value = "", fallback = "NA") {
  const code = String(value).trim().toUpperCase().replace(/[^A-Z0-9]/g, "");
  return (code || fallback).slice(0, 12);
}

function recordProjectCode(project = "") {
  const map = { "会议中心": "HYZX", "紫涵雅集": "ZHYJ" };
  return map[project] || businessCode(project, "PROJ");
}

function recordPointCode(record = {}) {
  const map = {
    p_elevator_no_room: "WJDT",
    p_elevator_machine_room: "YJDT",
    p_escalator: "FT",
    p_power_room: "BDS",
    p_ups: "UPS",
    p_fire_pump: "XFBF",
    p_water_pump: "SHSB",
    p_hot_water: "RSJF",
    p_zihan_energy: "NHCB",
    p_zihan_daily: "ZHXJ",
  };
  return map[record.pointId] || businessCode(record.pointId || record.pointName || record.templateId, "POINT");
}

function recordNo(record = {}) {
  if (record.recordNo) return record.recordNo;
  const date = new Date(record.createdAt || Date.now());
  const day = Number.isNaN(date.getTime())
    ? "00000000"
    : `${date.getFullYear()}${String(date.getMonth() + 1).padStart(2, "0")}${String(date.getDate()).padStart(2, "0")}`;
  const suffix = businessCode(String(record.id || "").split("_").pop(), "0000").slice(-4).padStart(4, "0");
  return `ZX-${day}-${recordProjectCode(record.project)}-${recordPointCode(record)}-${suffix}`;
}

function todayKey() {
  return new Date().toISOString().slice(0, 10);
}

function apiHeaders(json = false) {
  const user = state.currentUser || {};
  const headers = {
    "X-User-Role": user.roleCode || "supervisor",
    "X-User-Name": encodeURIComponent(user.displayName || user.username || "管理员"),
  };
  if (state.token) headers["X-InspectAI-Token"] = state.token;
  if (json) headers["Content-Type"] = "application/json";
  return headers;
}

async function api(path, options = {}) {
  let res;
  try {
    res = await fetch(state.apiBase + path, {
      ...options,
      headers: { ...apiHeaders(Boolean(options.body)), ...(options.headers || {}) },
      credentials: "include",
    });
  } catch (error) {
    const fallback = defaultApiBase();
    if (state.apiBase !== fallback) {
      state.apiBase = fallback;
      localStorage.setItem(API_BASE_KEY, fallback);
      res = await fetch(state.apiBase + path, {
        ...options,
        headers: { ...apiHeaders(Boolean(options.body)), ...(options.headers || {}) },
        credentials: "include",
      });
    } else {
      throw error;
    }
  }
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
  updateUserBadge();
  return data;
}

async function loadMe() {
  // F9 修复 · 不再编 legacy_supervisor 假身份，404 直接抛错让上层走登录页
  const data = await api("/api/auth/me");
  state.currentUser = data.user || null;
  updateUserBadge();
  return state.currentUser;
}

async function logout() {
  try {
    await api("/api/auth/logout", { method: "POST" });
  } catch {}
  state.token = "";
  state.currentUser = null;
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
  if (raw.includes("执行中") || raw.includes("已完成") || raw.includes("启用")) return "normal";
  if (raw.includes("复核") || raw.includes("补图") || raw.includes("人工填写") || raw.includes("待") || raw === "warning") return "warning";
  return "unknown";
}

const ASSET_STATUS_OPTIONS = ["正常", "待复核", "异常", "待维修", "未巡检", "未知"];
const RECORD_STATUS_OPTIONS = ["正常", "异常", "待复核", "需补图", "人工填写", "已完成"];
const PLAN_STATUS_OPTIONS = ["启用", "执行中", "待执行", "已完成", "未排期", "暂停", "草稿", "已停用"];

function statusLabel(value = "") {
  const map = {
    normal: "正常",
    warning: "待复核",
    danger: "异常",
    repair: "待维修",
    unknown: "未知",
    not_started: "待复核",
    processing: "待复核",
    recognized: "正常",
    retake_required: "需补图",
    manual_required: "人工填写",
    pending: "待复核",
    approved: "已通过",
    rejected: "已驳回",
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
      if (left && right && right !== (asset.assetType || "")) return `${left} · ${right}`;
      return left || right || pointId;
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
  // 导致正常闭环记录全被标成异常或待复核、normal 一条都露不出来。
  const valueText = (record.fields || []).map((field) => String(field.value || "")).join(" ");
  if (ABNORMAL_VALUE_RE.test(valueText)) return "danger";
  // 闭环记录：提交时已拦截所有 needsReview / 必填缺失，无异常值即视为正常。
  if (record.submitted) return "normal";
  // 未闭环：仍需人工介入的（低置信待确认 / 需补图）标为待复核，其余按识别结果展示。
  if ((record.fields || []).some((field) => field.needsReview)) return "warning";
  if (record.recognitionStatus === "retake_required") return "warning";
  return "normal";
}

// 是否已产出可判定结果（闭环完成 / AI 有结果 / 已有字段值）。未到结果阶段不强标"正常"。
function hasInspectionResult(record) {
  if (record.submitted || record.recognitionStatus === "recognized") return true;
  return (record.fields || []).some((field) => String(field.value || "").trim());
}

function recordBusinessStatus(record) {
  if (record.manualRequired || record.recognitionStatus === "manual_required") return "人工填写";
  if (record.recognitionStatus === "retake_required") return "需补图";
  const level = recordLevel(record);
  if (level === "danger") return "异常";
  if (level === "warning") return "待复核";
  if (record.submitted) return "已完成";
  if (hasInspectionResult(record)) return "正常";
  return "待复核";
}

// 兼容旧调用：记录对外只展示业务状态，不再拆成“识别流程状态”。
function recordResultStatus(record) {
  return recordBusinessStatus(record);
}

function recordFlowStatus(record) {
  return recordBusinessStatus(record);
}

function recordStatus(record) {
  return recordBusinessStatus(record);
}

function projects() {
  const names = [
    ...state.assets.map((item) => item.project || item.projectCode),
    ...state.records.map((item) => item.project),
    ...state.points.map((item) => item.project),
    ...state.templates.map((item) => item.project),
    ...state.customPlans.map((item) => item.project),
    ...state.customTasks.map((item) => item.project),
    ...state.engineeringPlans.map((item) => item.project),
    ...state.engineeringTasks.map((item) => item.project),
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
    recordNo(record),
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
  const result = filters.status.trim();
  const keyword = filters.keyword.trim().toLowerCase();
  return state.records.filter((record) => {
    if (state.selectedProject && !project && record.project !== state.selectedProject) return false;
    if (project && record.project !== project) return false;
    if (template && record.templateName !== template && record.templateId !== template) return false;
    if (result && recordBusinessStatus(record) !== result) return false;
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
    const [assets, records, requests, points, templates, health, users, operationLogs, engineeringPlans, engineeringTasks] = await Promise.all([
      api("/api/assets"),
      api("/api/inspection/records"),
      api("/api/change-requests"),
      safeApi("/api/inspection/points", { points: [] }),
      safeApi("/api/report/templates", { templates: [] }),
      safeApi("/health", null),
      safeApi("/api/users", { users: [] }),
      safeApi("/api/operation-logs?limit=80", { logs: [] }),
      safeApi("/api/engineering/plans", { plans: [] }),
      safeApi("/api/engineering/tasks", { tasks: [] }),
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
    state.engineeringPlans = engineeringPlans.plans || [];
    state.engineeringTasks = engineeringTasks.tasks || [];
    if (!state.selectedAssetId || !state.assets.some((asset) => asset.id === state.selectedAssetId)) {
      state.selectedAssetId = latest(filteredAssets(), "updatedAt")?.id || state.assets[0]?.id || "";
    }
    if (!state.selectedRecordId || !state.records.some((record) => record.id === state.selectedRecordId)) {
      state.selectedRecordId = latest(filteredRecords())?.id || state.records[0]?.id || "";
    }
    renderProjectOptions();
    render();
    applyDeepLink();
    if (showToast) toast("后台数据已刷新");
  } catch (error) {
    renderError(error.message);
  }
}

function renderProjectOptions() {
  const select = $("#projectSelect");
  if (!select) return; // 顶栏项目筛选已移除，全局默认“全部项目”
  const current = state.selectedProject || select.value;
  const options = [`<option value="">全部项目</option>`]
    .concat(projects().map((project) => `<option value="${escapeHTML(project)}">${escapeHTML(project)}</option>`));
  select.innerHTML = options.join("");
  state.selectedProject = projects().includes(current) ? current : "";
  select.value = state.selectedProject;
}

function setPage(page, push = true) {
  state.page = normalizedPage(page);
  if (push) {
    const url = new URL(location.href);
    url.searchParams.set("page", state.page);
    history.pushState({ page: state.page }, "", url);
  }
  render();
}

function applyDeepLink() {
  const params = new URLSearchParams(location.search);
  const requestId = params.get("requestId") || params.get("approvalId");
  const recordId = params.get("recordId");
  const assetId = params.get("assetId");
  const key = `${requestId || ""}|${recordId || ""}|${assetId || ""}`;
  if (!key.replaceAll("|", "") || state.handledDeepLink === key) return;

  if (requestId) {
    const request = state.requests.find((item) => item.id === requestId);
    if (!request) return;
    state.handledDeepLink = key;
    state.approvalStatus = request.status || "all";
    if (state.page !== "approval") {
      state.page = "approval";
      render();
    }
    openRequestDrawer(requestId);
    return;
  }

  if (recordId) {
    const record = state.records.find((item) => item.id === recordId);
    if (!record) return;
    state.handledDeepLink = key;
    state.selectedRecordId = recordId;
    if (state.page !== "record") {
      state.page = "record";
      render();
    } else {
      render();
    }
    return;
  }

  if (assetId) {
    const asset = state.assets.find((item) => item.id === assetId);
    if (!asset) return;
    state.handledDeepLink = key;
    state.selectedAssetId = assetId;
    if (state.page !== "ledger") {
      state.page = "ledger";
      render();
    } else {
      render();
    }
  }
}

function setLoginScreen(enabled) {
  document.body.classList.toggle("login-screen", Boolean(enabled));
}

function render() {
  setLoginScreen(false);
  $$(".nav button").forEach((btn) => btn.classList.toggle("active", btn.dataset.page === state.page));
  // 当前页导航项滚入侧栏可视区(只滚侧栏自身,不带动整页)
  const activeNav = $(".nav button.active");
  const navHost = activeNav?.closest(".sidebar");
  if (activeNav && navHost) {
    const host = navHost.getBoundingClientRect();
    const item = activeNav.getBoundingClientRect();
    const pad = 14;
    if (item.top < host.top + pad) navHost.scrollTop -= host.top + pad - item.top;
    else if (item.bottom > host.bottom - pad) navHost.scrollTop += item.bottom - (host.bottom - pad);
  }
  const pendingApprovalCount = filteredRequests().filter((item) => item.status === "pending").length;
  $("#pendingBadge").textContent = pendingApprovalCount;
  $("#pendingBadge").hidden = pendingApprovalCount <= 0;
  const approvalShortcutCount = $("#approvalShortcutCount");
  if (approvalShortcutCount) approvalShortcutCount.textContent = pendingApprovalCount;
  const approvalShortcut = $("#approvalShortcut");
  if (approvalShortcut) approvalShortcut.classList.toggle("has-pending", pendingApprovalCount > 0);
  updateUserBadge();
  const renderer = pageRenderers[state.page] || renderDashboardPage;
  renderer();
  // 渲染完探测 aside 是否为空,给 body 加 .aside-empty,CSS 收掉 360px 那一列
  const asideEmpty = !$("#pageAside") || $("#pageAside").innerHTML.trim() === "";
  document.body.classList.toggle("aside-empty", asideEmpty);
  normalizeButtons();
}

function updateUserBadge() {
  const el = $(".admin-user");
  if (!el) return;
  const user = state.currentUser || { displayName: "未登录" };
  const name = user.displayName || user.username || "未登录";
  const initial = name.slice(0, 1) || "?";
  const role = user.roleName || user.roleCode || "管理员";
  const todos = myTodoCount();
  const dot = todos > 0 ? `<i class="user-dot" aria-label="${todos} 个待办">${todos > 9 ? "9+" : todos}</i>` : "";
  el.innerHTML = `<b>${escapeHTML(initial)}</b><span class="su-text"><span class="su-name">${escapeHTML(name)}</span><span class="su-role">${escapeHTML(role)}</span></span>${dot}`;
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

// 审批页专属动态：只看「修改申请」相关事件，中文、去掉原始 ID，与左侧申请卡互补不冲突
function approvalActivityCard() {
  const logs = (state.operationLogs || [])
    .filter((l) => String(l.action || "").startsWith("change_request"))
    .slice(0, 8);
  const head = `
    <div class="live-activity-head">
      <div class="live-activity-title"><span class="live-dot"></span><b>审批动态</b></div>
      <span class="live-activity-sub">近期 · ${logs.length} 条</span>
    </div>`;
  if (!logs.length) {
    return `<div class="live-activity">${head}<div class="live-activity-empty">暂无审批动态</div></div>`;
  }
  const rows = logs.map((log) => {
    const tone = logActionTone(log.action);
    const actor = log.actorName || "系统";
    const cr = matchRequestForLog(log);
    const cls = `live-row tone-${tone}${cr ? " live-clickable" : ""}`;
    const attr = cr ? ` data-cr-preview="${escapeHTML(cr.id)}"` : "";
    return `
      <li class="${cls}"${attr}>
        <span class="live-avatar">${escapeHTML((actor || "?").slice(0, 1))}</span>
        <div class="live-body">
          <div class="live-line"><b>${escapeHTML(actor)}</b><span class="live-action">${escapeHTML(actionLabel(log.action))}</span></div>
          <div class="live-time">${escapeHTML(relativeTime(log.createdAt))}</div>
        </div>
        ${cr ? `<span class="live-peek">预览</span>` : ""}
      </li>`;
  }).join("");
  return `<div class="live-activity">${head}<ul class="live-activity-list">${rows}</ul></div>`;
}

// 把一条审批动态日志匹配到对应的修改申请（按目标 + 时间最近）
function matchRequestForLog(log) {
  if (!log || !String(log.action || "").startsWith("change_request")) return null;
  const cands = (state.requests || []).filter((r) => r.targetId === log.targetId);
  if (!cands.length) return null;
  const t = new Date(log.createdAt).getTime();
  return cands.slice().sort((a, b) =>
    Math.abs(new Date(a.requestedAt).getTime() - t) - Math.abs(new Date(b.requestedAt).getTime() - t))[0];
}

function bindApprovalActivityPreview() {
  document.querySelectorAll("#pageAside [data-cr-preview]").forEach((row) => {
    row.addEventListener("click", () => showApprovalPreview(row.dataset.crPreview, row));
  });
}

// 企业微信式快速预览：点审批动态 → 小窗看申请内容
function showApprovalPreview(crId, anchor) {
  const r = (state.requests || []).find((x) => x.id === crId);
  if (!r) return;
  closeApprovalPreview();
  const target = approvalTargetSummary(r);
  const statusCls = r.status === "approved" ? "normal" : r.status === "rejected" ? "danger" : "warning";
  const overlay = document.createElement("div");
  overlay.className = "cr-preview-overlay";
  overlay.innerHTML = `
    <div class="cr-preview-pop" role="dialog">
      <div class="crp-head">
        <span class="status ${statusCls}">${escapeHTML(statusLabel(r.status))}</span>
        <button class="crp-close" aria-label="关闭">×</button>
      </div>
      <div class="crp-title">${escapeHTML(target.title)}</div>
      <div class="crp-meta">${escapeHTML(r.requestedBy || "-")} · ${escapeHTML(fmtTime(r.requestedAt))}</div>
      <div class="crp-row"><span>变更</span><b>${escapeHTML(approvalPatchSummary(r))}</b></div>
      <div class="crp-row"><span>理由</span><b>${escapeHTML(r.reason || "-")}</b></div>
      ${r.reviewNote ? `<div class="crp-row"><span>审批</span><b>${escapeHTML(r.reviewNote)}</b></div>` : ""}
      <div class="crp-actions">
        ${r.status === "pending" ? `<button class="primary" data-request-review="${escapeHTML(r.id)}" data-action="approve">通过</button><button class="danger-btn" data-request-review="${escapeHTML(r.id)}" data-action="reject">驳回</button>` : ""}
        <button class="btn-ghost crp-detail" data-request-open="${escapeHTML(r.id)}">完整详情</button>
      </div>
    </div>`;
  document.body.appendChild(overlay);
  const pop = overlay.querySelector(".cr-preview-pop");
  const rect = anchor.getBoundingClientRect();
  const w = pop.offsetWidth || 280;
  let left = rect.left - w - 10;
  if (left < 8) left = rect.right + 10;
  let top = Math.min(rect.top, window.innerHeight - pop.offsetHeight - 12);
  pop.style.left = `${Math.max(8, left)}px`;
  pop.style.top = `${Math.max(8, top)}px`;
  overlay.addEventListener("click", (e) => { if (e.target === overlay) closeApprovalPreview(); });
  pop.querySelector(".crp-close").addEventListener("click", closeApprovalPreview);
  pop.querySelector(".crp-detail")?.addEventListener("click", () => { closeApprovalPreview(); openRequestDrawer(r.id); });
  // 通过/驳回由全局委托执行动作，这里只负责点完关掉小窗
  pop.querySelectorAll("[data-request-review]").forEach((b) => b.addEventListener("click", () => setTimeout(closeApprovalPreview, 0)));
}
function closeApprovalPreview() {
  document.querySelector(".cr-preview-overlay")?.remove();
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
  const tileCounts = quickAccessCounts(records);

  // 首页 = 纯暗色 Agent 控制台(辉光地平线版);不再渲染旧的问候横幅/快速入口/本月成绩。
  $("#pageMain").innerHTML = aiChatPanel();
  $("#pageAside").innerHTML = "";
  bindAiChat();
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

const AI_SUGGESTIONS = [
  "最近 30 天有哪些设备需要重点关注？",
  "本周异常比上周增加了吗？",
  "无机房电梯最近有哪些重复风险？",
  "复核率怎么样？有谁没看图就确认？",
  "目前有哪些待复核记录需要处理？",
  "今天应该优先处理什么？",
];

// Agent 首页:@页面跳转(复用全局 data-page-link → setPage)
const AGENT_NAV = [
  { page: "plan",     label: "巡检计划" },
  { page: "record",   label: "巡检记录" },
  { page: "ledger",   label: "资产台账" },
  { page: "approval", label: "审批中心" },
  { page: "data",     label: "数据看板" },
];
// 问 agent 任何问题 → 按意图判断目标页,回复下方给一个「前往 X」跳转 chip(全前端,不依赖 AI)
const NAV_INTENTS = [
  { page: "approval", label: "审批中心", re: /审批|待审|申请|修改单|工单|复核率|没看图/ },
  { page: "ledger",   label: "资产台账", re: /资产|台账|档案|设备健康|哪些设备|重点关注|定位.*设备/ },
  { page: "data",     label: "数据看板", re: /看板|趋势|漂移|报表|分析|图表|对比|统计/ },
  { page: "record",   label: "巡检记录", re: /巡检记录|日报|记录/ },
  { page: "plan",     label: "巡检计划", re: /计划|排期|派发|复查任务|任务|跟进/ },
  { page: "users",    label: "用户与权限", re: /用户|权限|账号|人员|角色/ },
  { page: "logs",     label: "操作日志", re: /日志|操作记录|谁改|谁操作|审计/ },
  { page: "system",   label: "系统管理", re: /系统|配置|服务|接口|密钥|企业微信|通知/ },
  { page: "profile",  label: "个人首页", re: /个人|我的资料|退出登录/ },
];
function navIntent(text) {
  const t = String(text || "");
  for (const n of NAV_INTENTS) if (n.re.test(t)) return n;
  return null;
}
// 4 个建议动作(点了直接发问)
const AGENT_ACTS = [
  { q: "查看本周巡检计划", label: "查看本周巡检计划", tone: "b", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="4.5" width="18" height="17" rx="2"/><path d="M16 2.5v4M8 2.5v4M3 9.5h18"/></svg>' },
  { q: "目前有哪些待审批工单需要处理？", label: "查询待审批工单", tone: "p", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M9 13h6M9 17h4"/></svg>' },
  { q: "最近 30 天有哪些设备需要重点关注？", label: "定位异常设备", tone: "t", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3"/><path d="M12 1v3M12 20v3M1 12h3M20 12h3"/></svg>' },
  { q: "本周异常比上周增加了吗？", label: "分析本月异常趋势", tone: "o", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="M7 14l4-4 3 3 5-6"/></svg>' },
];

// 首页 = 暗色 Agent 控制台(辉光地平线):居中问候 + 建议动作 + 大输入(@页面跳转)
function aiChatPanel() {
  const acts = AGENT_ACTS.map((a) => `<button type="button" class="ah-act" data-ai-suggest="${escapeHTML(a.q)}"><span class="ah-ic ${a.tone}">${a.svg}</span>${escapeHTML(a.label)}</button>`).join("");
  const ments = AGENT_NAV.map((n) => `<button type="button" class="ah-ment" data-page-link="${n.page}">@ ${escapeHTML(n.label)}</button>`).join("");
  return `
    <section class="agent-home agent-dark">
      <div class="ah-aura"></div>
      <div class="ah-horizon"></div>
      <div class="ah-body" id="aiChatBody">
        <div class="ai-chat-empty ah-hero">
          <div class="ah-hi">你好，我是 <span class="en">智巡 Agent</span></div>
          <div class="ah-sub">您身边的智能巡检助手，随时为您服务</div>
          <div class="ah-ask">你可以这样问我</div>
          <div class="ah-acts">${acts}</div>
        </div>
      </div>
      <form class="ah-composer ai-chat-form" id="aiChatForm" autocomplete="off">
        <div class="ah-crow">
          <input id="aiChatInput" type="text" maxlength="200" placeholder="请输入您的问题，或直接 @相关页面，如：@巡检记录" />
          <button type="submit" class="ah-send" id="aiChatSend" aria-label="发送">
            <svg viewBox="0 0 24 24" fill="none" stroke="#04241b" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 2 11 13M22 2l-7 20-4-9-9-4 20-7z"/></svg>
          </button>
        </div>
        <div class="ah-ments"><span class="ah-ments-lbl">可以试试</span>${ments}</div>
      </form>
      <div class="ah-foot">
        <span class="ah-dis">AI 生成的内容仅供参考，请以实际数据为准</span>
        <button type="button" class="ah-clear" id="aiClearBtn">清空对话</button>
      </div>
    </section>
  `;
}

const AI_CHAT_STATE = { history: [], busy: false, seq: 0 };

// 从 AI 回复里抽取 <<ACTION>>…<<END>> 动作提议块,正文去掉该块。只接受已支持的动作类型。
function extractActionProposal(reply) {
  const raw = reply || "";
  const m = raw.match(/<<ACTION>>\s*([\s\S]*?)\s*<<END>>/);
  if (!m) return { text: raw.trim(), proposal: null };
  let proposal = null;
  try {
    const obj = JSON.parse(m[1].trim());
    if (obj && obj.type === "create_recheck_task" && obj.asset) proposal = obj;
  } catch (e) { proposal = null; }
  const text = raw.replace(/<<ACTION>>[\s\S]*?<<END>>/, "").trim();
  return { text, proposal };
}

// 动作提议 → 确认卡(可调整责任人/截止);已派发后渲染 done 态
function renderActionProposal(m) {
  const p = m.proposal;
  if (!p || m.proposalDismissed) return "";
  const asset = escapeHTML(p.asset || "");
  if (m.proposalDone) {
    return `<div class="ai-action-proposal done">
      <div class="aap-head"><span class="aap-tag done">已派发</span><b>${escapeHTML(m.proposalResult || "复查任务已派发")}</b></div>
      <button type="button" class="aap-goto" data-task-id="${escapeHTML(m.proposalTaskId || "")}" data-asset="${asset}">查看任务详情 →</button>
    </div>`;
  }
  const assignee = escapeHTML(p.assignee || "");
  const dueAt = escapeHTML(p.dueAt || "");
  const reason = escapeHTML(p.reason || "");
  return `<div class="ai-action-proposal" data-msg-id="${escapeHTML(m.id || "")}" data-action-type="${escapeHTML(p.type)}" data-action-asset="${asset}">
    <div class="aap-head"><span class="aap-tag">建议动作</span><b>派复检任务</b></div>
    <div class="aap-target">目标设备 <span class="ai-ref-static">${asset}</span></div>
    ${reason ? `<div class="aap-reason">${reason}</div>` : ""}
    <div class="aap-fields">
      <label>责任人<input class="aap-input" data-k="assignee" value="${assignee}" placeholder="默认上次巡检人"></label>
      <label>截止<input class="aap-input" data-k="dueAt" type="date" value="${dueAt}"></label>
    </div>
    <div class="aap-actions">
      <button type="button" class="aap-confirm">确认派发</button>
      <button type="button" class="aap-dismiss">忽略</button>
    </div>
  </div>`;
}

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
  document.getElementById("aiClearBtn")?.addEventListener("click", () => {
    if (AI_CHAT_STATE.busy) return;
    AI_CHAT_STATE.history = [];
    render();
  });
  bindAiScaffoldActions(body);
  // restore history on re-render
  if (AI_CHAT_STATE.history.length) {
    body.innerHTML = AI_CHAT_STATE.history.map(renderChatBubble).join("");
    body.scrollTop = body.scrollHeight;
  }
}

function renderChatBubble(m) {
  const cls = m.role === "user" ? "user" : "ai";
  let html = (m.text || "")
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/\*\*(.+?)\*\*/g, "<strong>$1</strong>")
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\n/g, "<br>");
  // AI 回复里的设备编号 / 记录号自动转成可点链接(命中真实数据才加,避免死链)
  if (cls === "ai") html = linkifyRefs(html);
  const card = cls === "ai" ? renderActionProposal(m) : "";
  // 按提问意图给一个「前往 X 页」跳转 chip(复用全局 data-page-link)
  const jump = (cls === "ai" && m.navJump)
    ? `<button type="button" class="ai-jump-chip" data-page-link="${m.navJump.page}">前往${escapeHTML(m.navJump.label)}<i>→</i></button>`
    : "";
  return `<div class="ai-chat-msg ${cls}"><div class="ai-chat-bubble">${html}</div>${card}${jump}</div>`;
}

// 把回复正文里出现的真实资产编号 / 记录号包成 .ai-ref 链接(等宽蓝字 + 可点跳转)
function linkifyRefs(html) {
  const assetByCode = new Map();
  (state.assets || []).forEach((a) => { const k = assetKey(a); if (k) assetByCode.set(k, a); });
  const recByNo = new Map();
  (state.records || []).forEach((r) => { const n = recordNo(r); if (n) recByNo.set(n, r); });
  if (!assetByCode.size && !recByNo.size) return html;
  return html.replace(/[A-Z][A-Z0-9]*(?:-[A-Z0-9]+)+/g, (token) => {
    const a = assetByCode.get(token);
    if (a) {
      return `<span class="ai-ref" data-ref="asset" data-ref-id="${escapeHTML(a.id)}" data-ref-keyword="${escapeHTML(token)}" data-ref-asset-type="${escapeHTML(a.assetType || "")}" data-ref-asset-status="${escapeHTML(a.lastStatus || "")}">${token}</span>`;
    }
    const r = recByNo.get(token);
    if (r) {
      return `<span class="ai-ref" data-ref="record" data-ref-id="${escapeHTML(r.id)}" data-ref-keyword="${escapeHTML(token)}" data-ref-record-project="${escapeHTML(r.project || "")}" data-ref-record-template="${escapeHTML(r.templateName || r.templateId || "")}">${token}</span>`;
    }
    return token;
  });
}

function firstAttentionAsset() {
  return filteredAssets()
    .filter((asset) => ["danger", "warning", "repair"].includes(normalizeStatus(asset.lastStatus)))
    .sort((a, b) => assetPriorityRank(b.lastStatus) - assetPriorityRank(a.lastStatus) || new Date(b.updatedAt || 0) - new Date(a.updatedAt || 0))[0] || null;
}

function assetPriorityRank(status = "") {
  const level = normalizeStatus(status);
  return { repair: 4, danger: 3, warning: 2, unknown: 1, normal: 0 }[level] || 0;
}

function latestProblemRecord() {
  return filteredRecords()
    .filter((record) => ["异常", "待复核", "需补图", "人工填写"].includes(recordBusinessStatus(record)))
    .sort((a, b) => new Date(b.submittedAt || b.createdAt || 0) - new Date(a.submittedAt || a.createdAt || 0))[0] || null;
}

function latestPendingApproval() {
  return filteredRequests()
    .filter((item) => item.status === "pending")
    .sort((a, b) => new Date(b.requestedAt || 0) - new Date(a.requestedAt || 0))[0] || null;
}

function currentTaskSnapshot() {
  const groups = taskGroups();
  return {
    pending: groups.pending.length,
    processing: groups.processing.length,
    overdue: groups.overdue.length,
    done: groups.done.length,
    first: [...groups.overdue, ...groups.processing, ...groups.pending][0] || null,
  };
}

function buildAiScaffold(message, reply, response = {}) {
  const text = `${message || ""} ${reply || ""}`.toLowerCase();
  const approvals = filteredRequests().filter((item) => item.status === "pending");
  const attention = filteredAssets().filter((asset) => ["danger", "warning", "repair"].includes(normalizeStatus(asset.lastStatus)));
  const asset = firstAttentionAsset();
  const record = latestProblemRecord();
  const approval = latestPendingApproval();
  const tasks = currentTaskSnapshot();
  const actions = [];
  const evidence = [];

  if (attention.length) evidence.push(`需跟进资产 ${attention.length} 台`);
  if (approvals.length) evidence.push(`待审批 ${approvals.length} 条`);
  if (tasks.processing || tasks.pending || tasks.overdue) evidence.push(`在途任务 ${tasks.processing + tasks.pending + tasks.overdue} 项`);
  if (record) evidence.push(`最近问题记录 ${record.templateName || record.pointName || "巡检记录"}`);
  if (!evidence.length) evidence.push("当前无高优先级阻塞项");

  const wantsApproval = /审批|复核|申请|修改/.test(text);
  const wantsTask = /任务|计划|派发|下发|完成/.test(text);
  const wantsRecord = /记录|日报|巡检|异常/.test(text);
  const wantsTrend = /趋势|历史|对比|最近|30|风险|看板/.test(text);

  if ((wantsApproval || approvals.length) && approval) {
    const target = approvalTargetSummary(approval);
    actions.push({
      label: "定位待审批",
      page: "approval",
      requestId: approval.id,
      approvalStatus: "pending",
      target: target.title,
      reason: approval.reason || approvalPatchSummary(approval),
      tone: "warn",
    });
  }
  if ((wantsRecord || record) && record) {
    const status = recordBusinessStatus(record);
    actions.push({
      label: "定位问题记录",
      page: "record",
      recordId: record.id,
      recordProject: record.project || "",
      recordTemplate: record.templateName || record.templateId || "",
      recordStatus: status,
      recordKeyword: recordNo(record),
      target: `${record.pointName || record.templateName || "巡检记录"} · ${recordNo(record)}`,
      reason: `${status} · ${truncateText(primaryReading(record) || record.aiSummary || record.report || "查看字段与图片证据", 42)}`,
      tone: status === "异常" ? "danger" : "warn",
    });
  }
  if ((wantsTask || tasks.first) && tasks.first?.id) {
    actions.push({
      label: "定位执行任务",
      page: "plan",
      taskId: tasks.first.id,
      planStatus: planStatusBucket(tasks.first.status),
      target: tasks.first.title || "巡检任务",
      reason: `${tasks.first.status || "待处理"} · ${truncateText(tasks.first.meta || "查看责任人与截止时间", 42)}`,
      tone: tasks.first.status === "逾期" || tasks.first.status === "待整改" ? "danger" : "info",
    });
  } else if (wantsTask) {
    actions.push({
      label: "筛选计划",
      page: "plan",
      planStatus: tasks.processing ? "processing" : "pending",
      target: tasks.processing ? "进行中任务" : "待执行任务",
      reason: "进入计划页后只看当前状态桶",
      tone: "info",
    });
  }
  if (asset) {
    actions.push({
      label: "定位资产档案",
      page: "ledger",
      assetId: asset.id,
      assetType: asset.assetType || "",
      assetStatus: asset.lastStatus || "",
      assetKeyword: assetKey(asset),
      target: `${asset.assetName || "资产"} · ${assetKey(asset)}`,
      reason: `${asset.lastStatus || "待复核"} · ${truncateText(asset.lastSummary || locationText(asset), 42)}`,
      tone: normalizeStatus(asset.lastStatus) === "danger" ? "danger" : "primary",
    });
  }
  if (wantsTrend || attention.length) {
    actions.push({
      label: "看趋势结论",
      page: "data",
      target: "数据看板",
      reason: attention.length ? "核对近期异常频次与字段漂移" : "查看历史巡检变化",
      tone: "neutral",
    });
  }
  if (!actions.length) {
    actions.push(
      { label: "筛选资产台账", page: "ledger", target: "全部资产", reason: "按状态和设备类型继续排查", tone: "primary" },
      { label: "筛选巡检记录", page: "record", target: "最近巡检", reason: "查看日报、字段和照片证据", tone: "neutral" },
    );
  }

  const externalActions = Array.isArray(response.actions) ? response.actions : [];
  const merged = [...externalActions, ...actions].filter(Boolean);
  const deduped = [];
  const seen = new Set();
  merged.forEach((action) => {
    const key = [action.page, action.assetId, action.recordId, action.taskId, action.requestId, action.approvalStatus, action.label].join("|");
    if (seen.has(key)) return;
    seen.add(key);
    deduped.push(action);
  });

  return {
    title: attention.length || approvals.length || tasks.overdue ? "下一步处置" : "可继续核对",
    summary: scaffoldSummary(attention.length, approvals.length, tasks, deduped[0]),
    evidence: evidence.slice(0, 4),
    actions: deduped.slice(0, 3),
  };
}

function scaffoldSummary(attentionCount, approvalCount, tasks, primaryAction) {
  if (primaryAction?.target) return `优先看「${primaryAction.target}」，原因：${primaryAction.reason || "存在待处理信息"}。`;
  if (approvalCount) return `先处理 ${approvalCount} 条审批，避免现场记录卡住。`;
  if (tasks.overdue) return `有 ${tasks.overdue} 项需跟进任务，建议先确认责任人与截止时间。`;
  if (attentionCount) return `有 ${attentionCount} 台资产需要跟进，建议先看资产档案和最近记录。`;
  if (tasks.processing || tasks.pending) return `当前有 ${tasks.processing + tasks.pending} 项任务在流转，建议查看计划进度。`;
  return "当前可从台账、记录和趋势三个入口继续核对。";
}

function renderAiScaffold(scaffold) {
  const evidence = (scaffold.evidence || []).map((item) => `<span>${escapeHTML(item)}</span>`).join("");
  const actions = (scaffold.actions || []).map((action) => `
    <button type="button"
      class="ai-action-card tone-${escapeHTML(action.tone || "neutral")}"
      data-ai-action-page="${escapeHTML(action.page || "dashboard")}"
      data-ai-action-request="${escapeHTML(action.requestId || "")}"
      data-ai-action-asset="${escapeHTML(action.assetId || "")}"
      data-ai-action-asset-type="${escapeHTML(action.assetType || "")}"
      data-ai-action-asset-status="${escapeHTML(action.assetStatus || "")}"
      data-ai-action-asset-keyword="${escapeHTML(action.assetKeyword || "")}"
      data-ai-action-record="${escapeHTML(action.recordId || "")}"
      data-ai-action-record-project="${escapeHTML(action.recordProject || "")}"
      data-ai-action-record-template="${escapeHTML(action.recordTemplate || "")}"
      data-ai-action-record-status="${escapeHTML(action.recordStatus || "")}"
      data-ai-action-record-keyword="${escapeHTML(action.recordKeyword || "")}"
      data-ai-action-task="${escapeHTML(action.taskId || "")}"
      data-ai-action-plan-status="${escapeHTML(action.planStatus || "")}"
      data-ai-action-approval="${escapeHTML(action.approvalStatus || "")}"
      data-ai-action-target="${escapeHTML(action.target || action.label || "")}">
      <b>${escapeHTML(action.label || "查看")}</b>
      <span>${escapeHTML(action.target || pageLabels[action.page] || "目标页面")}</span>
      ${action.reason ? `<em>${escapeHTML(action.reason)}</em>` : ""}
    </button>
  `).join("");
  return `
    <div class="ai-scaffold">
      <div class="ai-scaffold-head">
        <b>${escapeHTML(scaffold.title || "下一步")}</b>
        <p>${escapeHTML(scaffold.summary || "")}</p>
      </div>
      <div class="ai-scaffold-evidence">${evidence}</div>
      <div class="ai-scaffold-actions">${actions}</div>
    </div>
  `;
}

function bindAiScaffoldActions(root) {
  if (!root || root.dataset.aiScaffoldBound) return;
  root.dataset.aiScaffoldBound = "1";
  root.addEventListener("click", async (event) => {
    // 动作提议卡:确认派发 / 忽略 / 查看任务
    const aap = event.target.closest(".ai-action-proposal");
    if (aap) {
      if (event.target.closest(".aap-dismiss")) {
        event.preventDefault();
        const m = AI_CHAT_STATE.history.find((x) => x.id === aap.dataset.msgId);
        if (m) m.proposalDismissed = true;
        aap.remove();
        return;
      }
      const goto = event.target.closest(".aap-goto");
      if (goto) {
        event.preventDefault();
        const taskId = goto.dataset.taskId || "";
        if (taskId) {
          // 定位到「复查任务」列表里那一行(行高亮 + 右侧详情卡),并滚动到可见
          state.selectedTaskId = taskId;
          state.selectedPlanStatus = ""; // 全部:确保复查任务区块可见
          state.selectedPlanId = "";
          setPage("plan");
          requestAnimationFrame(() => {
            const row = document.querySelector(".recheck-row.selected");
            if (row) row.scrollIntoView({ behavior: "smooth", block: "center" });
          });
        } else {
          const a = (state.assets || []).find((x) => assetKey(x) === goto.dataset.asset);
          if (a) state.selectedAssetId = a.id;
          state.selectedPlanStatus = "";
          setPage("plan");
        }
        return;
      }
      const confirmBtn = event.target.closest(".aap-confirm");
      if (confirmBtn) {
        event.preventDefault();
        const asset = (state.assets || []).find((x) => assetKey(x) === aap.dataset.actionAsset);
        if (!asset) { toast(`未找到设备「${aap.dataset.actionAsset}」,无法派发`); return; }
        const params = {};
        aap.querySelectorAll(".aap-input").forEach((inp) => {
          const v = (inp.value || "").trim();
          if (v) params[inp.dataset.k] = v;
        });
        const msgId = aap.dataset.msgId;
        confirmBtn.disabled = true;
        confirmBtn.textContent = "派发中…";
        try {
          const res = await api("/api/management-ai/act", {
            method: "POST",
            body: JSON.stringify({ type: aap.dataset.actionType, targetId: asset.id, params }),
          });
          const m = AI_CHAT_STATE.history.find((x) => x.id === msgId);
          const detail = res.task ? `(责任人 ${res.task.assigneeName || "—"} · 截止 ${res.task.dueAt || "—"})` : "";
          if (m) {
            m.proposalDone = true;
            m.proposalResult = (res.message || "复查任务已派发") + detail;
            m.proposalTaskId = res.task?.id || "";
          }
          toast(res.message || "复查任务已派发");
          await loadData(false); // 刷新数据,任务进入计划池;re-render 用 done 态重绘卡片
        } catch (err) {
          toast(`派发失败:${err.message || "未知错误"}`);
          confirmBtn.disabled = false;
          confirmBtn.textContent = "确认派发";
        }
        return;
      }
      return;
    }
    const ref = event.target.closest(".ai-ref");
    if (ref) {
      event.preventDefault();
      event.stopPropagation();
      const keyword = ref.dataset.refKeyword || "";
      if (ref.dataset.ref === "record") {
        if (ref.dataset.refId) state.selectedRecordId = ref.dataset.refId;
        state.recordFilters.project = ref.dataset.refRecordProject || "";
        state.recordFilters.template = ref.dataset.refRecordTemplate || "";
        state.recordFilters.status = "";
        state.recordFilters.keyword = keyword;
        state.recordPage = 0;
        setPage("record");
      } else {
        if (ref.dataset.refId) state.selectedAssetId = ref.dataset.refId;
        state.filters.assetType = ref.dataset.refAssetType || "";
        state.filters.status = "";
        state.filters.keyword = keyword;
        setPage("ledger");
      }
      if (keyword) toast(`已定位：${keyword}`);
      return;
    }
    const btn = event.target.closest("[data-ai-action-page]");
    if (!btn) return;
    event.preventDefault();
    event.stopPropagation();
    const page = btn.dataset.aiActionPage || "dashboard";
    const requestId = btn.dataset.aiActionRequest || "";
    const assetId = btn.dataset.aiActionAsset || "";
    const recordId = btn.dataset.aiActionRecord || "";
    const taskId = btn.dataset.aiActionTask || "";
    const approvalStatus = btn.dataset.aiActionApproval || "";
    const planStatus = btn.dataset.aiActionPlanStatus || "";
    if (assetId) state.selectedAssetId = assetId;
    if (recordId) state.selectedRecordId = recordId;
    if (taskId) state.selectedTaskId = taskId;
    if (approvalStatus) state.approvalStatus = approvalStatus;
    if (page === "ledger") {
      state.filters.assetType = btn.dataset.aiActionAssetType || "";
      state.filters.status = btn.dataset.aiActionAssetStatus || "";
      state.filters.keyword = btn.dataset.aiActionAssetKeyword || "";
    }
    if (page === "record") {
      state.recordFilters.project = btn.dataset.aiActionRecordProject || "";
      state.recordFilters.template = btn.dataset.aiActionRecordTemplate || "";
      state.recordFilters.status = btn.dataset.aiActionRecordStatus || "";
      state.recordFilters.keyword = btn.dataset.aiActionRecordKeyword || "";
      state.recordPage = 0;
    }
    if (page === "plan") {
      state.selectedPlanStatus = planStatus || "";
      state.planFilters.keyword = "";
    }
    setPage(page);
    if (requestId) requestAnimationFrame(() => openRequestDrawer(requestId));
    const target = btn.dataset.aiActionTarget;
    if (target) toast(`已定位：${target}`);
  });
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
  body.lastElementChild?.classList.add("just-in");
  // typing placeholder
  body.insertAdjacentHTML("beforeend", `<div class="ai-chat-msg ai just-in" id="aiChatTyping"><div class="ai-chat-bubble typing"><i></i><i></i><i></i></div></div>`);
  body.scrollTop = body.scrollHeight;

  try {
    const res = await api("/api/management-ai/chat", {
      method: "POST",
      body: JSON.stringify({
        message,
        history: AI_CHAT_STATE.history.slice(-6).map((item) => ({ role: item.role, text: item.text })),
        project: state.selectedProject || "",
        range: "30d",
      }),
    });
    const reply = res.reply || "AI 没有给出回复。";
    const { text, proposal } = extractActionProposal(reply);
    const msg = { role: "ai", text, proposal, navJump: navIntent(message), id: "aim_" + (++AI_CHAT_STATE.seq) };
    AI_CHAT_STATE.history.push(msg);
    document.getElementById("aiChatTyping")?.remove();
    body.insertAdjacentHTML("beforeend", renderChatBubble(msg));
    body.lastElementChild?.classList.add("just-in");
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
  const userName = state.currentUser?.displayName || state.currentUser?.username || "管理员";
  const normalCount = Math.max(0, Number(todayRecords || 0) - Number(issuesCount || 0));
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
        <div class="ai-hero-hi">${HELLO}，${escapeHTML(userName)}</div>
        <div class="ai-hero-line">
          今日 AI 交付
          <b><span class="dash-anim" data-count="${todayRecords}">0</span></b><em>条巡检</em>
          <b><span class="dash-anim" data-count="${issuesCount}">0</span></b><em>项异常</em>
          <b><span class="dash-anim" data-count="${normalCount}">0</span></b><em>项正常</em>
        </div>
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
    // 需跟进口径与移动端「设备健康」一致：异常 + 待复核 + 待维修
    { key: "warn",   label: "需跟进",   count: counts.warning + counts.danger + counts.repair, pct: Math.round(((counts.warning + counts.danger + counts.repair) / total) * 100), color: "#f59e0b", desc: "异常 / 待复核 / 待维修" },
    { key: "repair", label: "待维修",   count: counts.repair, pct: Math.round((counts.repair / total) * 100), color: "#ef4b3f", desc: "需跟进中需现场处理的部分" },
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
        <button class="link-btn" data-page-link="approval">全部 →</button>
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
    approvals: filteredRequests().filter((item) => item.status === "pending").length,
    assets: state.assets.length,
  };
}

function quickAccessTiles(c) {
  const tiles = [
    { key: "plan",   label: "巡检计划", num: c.plans,   unit: "项计划", sub: c.tasks ? `${c.tasks} 项待执行` : "计划与任务", page: "plan",   icon: "icon-plan.svg",   color: "#4f7cff" },
    { key: "record", label: "巡检记录", num: c.records, unit: "条记录", sub: "本周累计",   page: "record", icon: "icon-record.svg", color: "#f59e0b" },
    { key: "approval", label: "审批中心", num: c.approvals, unit: "项待审", sub: "修改申请", page: "approval", icon: "nav-approval.svg", color: "#ef4b3f" },
    { key: "asset",  label: "资产台账", num: c.assets,  unit: "项资产", sub: "全量资产",   page: "ledger", icon: "icon-asset.svg",  color: "#8b5cf6" },
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

function dashboardAside({ monthly, pendingExceptions, pendingApprovals, pendingTasks, accuracy }) {
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
        <a class="todo-row danger" data-page-link="approval">
          <img src="./assets/icon-warning.svg" alt=""/>
          <span>异常待处理</span><b>${pendingExceptions}</b><em>进审批 →</em>
        </a>
        <a class="todo-row warn" data-page-link="approval">
          <img src="./assets/icon-bulb.svg" alt=""/>
          <span>审批中心</span><b>${pendingApprovals}</b><em>去审批 →</em>
        </a>
        <a class="todo-row info" data-page-link="plan">
          <img src="./assets/icon-task.svg" alt=""/>
          <span>计划执行</span><b>${pendingTasks}</b><em>看计划 →</em>
        </a>
      </div>
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
  // 顶部统计卡与下方计划列表同源：都基于计划行（按计划状态分桶），点击卡片即筛选列表。
  // seed 占位行（"未配置"）不是真实计划：不进统计、不进状态筛选，只在不筛选时垫底展示。
  const baseRows = filteredPlanRows();
  const realRows = baseRows.filter((r) => r.source !== "seed");
  const counts = planBucketCounts(realRows);
  const rows = state.selectedPlanStatus
    ? realRows.filter((r) => planStatusBucket(r.status) === state.selectedPlanStatus)
    : baseRows;
  if (!state.selectedPlanId || !rows.some((r) => r.id === state.selectedPlanId)) {
    state.selectedPlanId = rows[0]?.id || "";
  }
  const recheckTasks = standaloneOpenTasks();
  // 复查任务属于「需跟进」桶:只在「需跟进」和「全部」(未筛选)时展示,其余筛选页隐藏
  const showRecheck = state.selectedPlanStatus === "" || state.selectedPlanStatus === "overdue";
  $("#pageMain").innerHTML = `
    ${planStatusEntryBoard(counts, recheckTasks.length)}
    ${showRecheck ? recheckTaskSection(recheckTasks) : ""}
    ${planTableSection(rows)}
  `;
  const selectedPlan = rows.find((r) => r.id === state.selectedPlanId) || null;
  // 点了"查看任务进度"→侧栏显示与移动端挂钩的那条工程任务进度；否则显示计划详情
  const selectedTask = state.selectedTaskId
    ? engineeringTaskRows().find((t) => t.id === state.selectedTaskId)
    : null;
  $("#pageAside").innerHTML = selectedTask
    ? asideStack(taskDetailCard(selectedTask))
    : asideStack(planEditCard(selectedPlan));
  bindPlanStatusEntries();
  bindPlanFilters();
  bindPlanRowClicks();
  bindRecheckRowClicks();
  bindPlanEditForm();
  if (selectedTask) bindTaskDetailActions();
}

// 无计划归属的在途任务(异常复查 / AI 派单 / 自动兜底):工程计划列表里看不到,单列出来
function standaloneOpenTasks() {
  return (state.engineeringTasks || [])
    .filter((t) => t && !t.planItemId && !["已完成", "已取消"].includes(t.status))
    .filter((t) => !state.selectedProject || t.project === state.selectedProject)
    .sort((a, b) => String(a.dueAt || "").localeCompare(String(b.dueAt || "")));
}

function recheckTaskSection(tasks) {
  if (!tasks.length) return "";
  return `
    <section class="panel recheck-task-panel">
      <div class="panel-head">
        <div class="panel-title-block"><h2>复查任务</h2></div>
        <span class="recheck-hint">异常检出 / AI 派单生成,未挂工程计划;复检合格后自动销账</span>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>设备 / 任务</th><th>项目</th><th>责任人</th><th>截止</th><th>状态</th></tr></thead>
          <tbody>${tasks.map((t) => `
            <tr class="recheck-row ${t.id === state.selectedTaskId ? "selected" : ""}" data-task-id="${escapeHTML(t.id)}">
              <td>${escapeHTML(t.title || "异常复查")}</td>
              <td>${escapeHTML(t.project || "-")}</td>
              <td>${escapeHTML(t.assigneeName || "-")}</td>
              <td>${escapeHTML(t.dueAt || "-")}</td>
              <td><span class="status ${statusClass(t.status)}">${escapeHTML(planBucketLabel(t.status) || "需跟进")}</span></td>
            </tr>
          `).join("")}</tbody>
        </table>
      </div>
    </section>
  `;
}

function bindRecheckRowClicks() {
  document.querySelectorAll(".recheck-row").forEach((tr) => {
    tr.addEventListener("click", () => {
      state.selectedTaskId = tr.dataset.taskId;
      state.selectedPlanId = "";
      render();
    });
  });
}

// 计划状态 → 与顶部四张卡完全一致的归类词（让表格状态和卡片挂得上钩）
function planBucketLabel(status = "") {
  if (String(status).includes("未配置")) return "未配置";
  return { pending: "待执行", processing: "进行中", overdue: "需跟进", done: "已完成" }[planStatusBucket(status)] || status;
}

// 计划状态 → 统计桶（与四张卡一一对应）
function planStatusBucket(status = "") {
  const s = String(status);
  if (s.includes("已完成")) return "done";
  if (s.includes("待执行")) return "pending";
  if (s.includes("执行中") || s.includes("进行中") || s.includes("启用")) return "processing";
  return "overdue"; // 需跟进：未排期 / 暂停 / 草稿 / 已停用 / 逾期 等
}

function planBucketCounts(rows) {
  const c = { pending: 0, processing: 0, overdue: 0, done: 0 };
  (rows || []).forEach((r) => { c[planStatusBucket(r.status)] += 1; });
  return c;
}

function planTableSection(rows) {
  return `
    <section class="panel plan-table-panel plan-table-restored">
      <div class="panel-head plan-panel-head">
        <div class="panel-title-block"><h2>巡检任务</h2></div>
        <div class="panel-actions plan-toolbar">${planFiltersHTML()}<button data-drawer="plan">新建计划</button></div>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>计划名称</th><th>项目</th><th>类型 / 点位</th><th>周期</th><th>责任人</th><th>计划节点</th><th>状态</th></tr></thead>
          <tbody>${rows.map((row) => `
            <tr class="plan-row ${row.id === state.selectedPlanId ? "selected" : ""}" data-plan-id="${escapeHTML(row.id)}">
              <td>${escapeHTML(row.name)}</td><td>${escapeHTML(row.project)}</td><td>${escapeHTML(row.point)}</td>
              <td>${escapeHTML(row.frequency)}</td><td>${escapeHTML(row.owner)}</td><td>${escapeHTML(row.next)}</td>
              <td><span class="status ${statusClass(row.status === "启用" ? "正常" : row.status)}">${escapeHTML(planBucketLabel(row.status))}</span></td>
            </tr>
          `).join("") || emptyRow(7, "暂无巡检任务")}</tbody>
        </table>
      </div>
    </section>
  `;
}

function planStatusEntryBoard(counts, recheckCount = 0) {
  const configs = ["pending", "processing", "overdue", "done"].map(planStatusConfig);
  // 需跟进口径并入无计划归属的在途复查任务,避免"有待整改却显示 0"
  const nums = configs.map((c) => (counts[c.key] || 0) + (c.key === "overdue" ? recheckCount : 0));
  const total = nums.reduce((a, b) => a + b, 0) || 1;
  return `
    <section class="plan-entry-strip" aria-label="计划状态筛选">
      ${configs.map((config, i) => {
        const n = nums[i];
        const pct = Math.round((n / total) * 100);
        return `
        <button class="plan-entry-card tone-${escapeHTML(config.tone)} ${state.selectedPlanStatus === config.key ? "active" : ""}" type="button" data-plan-status="${escapeHTML(config.key)}">
          <span>${escapeHTML(config.label)}</span>
          <b>${escapeHTML(n)}</b>
          <i class="plan-entry-bar" style="width:${pct}%"></i>
        </button>
      `;
      }).join("")}
    </section>
  `;
}

function planStatusConfig(key) {
  const map = {
    pending: { key: "pending", label: "待执行", sub: "未开始", tone: "pending" },
    processing: { key: "processing", label: "进行中", sub: "现场处理", tone: "running" },
    overdue: { key: "overdue", label: "需跟进", sub: "复核 / 异常", tone: "attention" },
    done: { key: "done", label: "已完成", sub: "结果入库", tone: "done" },
  };
  return map[key] || map.pending;
}

function planExecutionGroups(tasks) {
  const clean = (items, bucket) => (items || [])
    .filter((item) => item && item.status !== "已取消")
    .map((item) => ({ ...item, bucket }));
  const pending = clean(tasks.pending, "pending");
  const processing = clean(tasks.processing, "processing");
  const done = clean(tasks.done, "done");
  const overdue = clean(tasks.overdue, "overdue");
  return {
    pending,
    processing,
    done,
    overdue,
    all: [...pending, ...processing, ...done, ...overdue],
  };
}

function bindPlanStatusEntries() {
  document.querySelectorAll("[data-plan-status]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const key = btn.dataset.planStatus;
      // 点同一张卡 → 取消筛选；否则按该状态筛选下方计划列表
      state.selectedPlanStatus = state.selectedPlanStatus === key ? "" : key;
      state.selectedPlanId = "";
      state.selectedTaskId = "";
      render();
    });
  });
}

function openPlanStatusDrawer(key) {
  const config = planStatusConfig(key);
  const groups = planExecutionGroups(taskGroups());
  const items = groups[config.key] || [];
  openDrawer(`${config.label}任务`, `
    <div class="plan-status-drawer tone-${escapeHTML(config.tone)}">
      <div class="plan-status-drawer-head">
        <b>${escapeHTML(items.length)}</b>
        <span>${escapeHTML(config.sub)}</span>
      </div>
      <div class="plan-status-list">
        ${items.map((item, index) => planStatusTaskRow(item, config.key, index)).join("") || `<div class="plan-status-empty">暂无任务</div>`}
      </div>
    </div>
  `);
  bindPlanStatusDrawerItems(config.key);
}

function planStatusTaskRow(item, key, index) {
  const meta = taskMetaSegments(item);
  return `
    <button class="plan-status-task" type="button" data-plan-task-key="${escapeHTML(key)}" data-plan-task-index="${escapeHTML(index)}">
      <div>
        <b>${escapeHTML(item.title || item.planName || "执行任务")}</b>
        <span>${meta.map((part) => `<i>${escapeHTML(part)}</i>`).join("")}</span>
      </div>
      <em>${escapeHTML(item.status || "-")}</em>
    </button>
  `;
}

function taskMetaSegments(item) {
  if (!item) return ["-"];
  const direct = [
    item.project,
    item.pointName || item.planName,
    item.owner,
    item.dueAt ? `截止 ${item.dueAt}` : "",
  ].filter(Boolean);
  if (direct.length) return direct;
  return String(item.meta || "-").split("·").map((part) => part.trim()).filter(Boolean).slice(0, 4);
}

function bindPlanStatusDrawerItems(key) {
  document.querySelectorAll("[data-plan-task-key]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const groups = planExecutionGroups(taskGroups());
      const item = groups[key]?.[Number(btn.dataset.planTaskIndex)];
      if (!item) return;
      state.selectedTaskId = item.id || "";
      state.selectedPlanStatus = key;
      state.selectedPlanTaskKey = key;
      state.selectedPlanTaskIndex = Number(btn.dataset.planTaskIndex);
      $("#pageAside").innerHTML = asideStack(taskDetailCard(item));
      document.body.classList.remove("aside-empty");
      closeDrawer();
      bindTaskDetailActions();
    });
  });
}

function selectedPlanTaskFromGroups(groups) {
  const key = state.selectedPlanTaskKey;
  const index = Number(state.selectedPlanTaskIndex);
  if (!key || index < 0) return null;
  return groups[key]?.[index] || null;
}

function bindPlanRowClicks() {
  document.querySelectorAll(".plan-row").forEach((tr) => {
    tr.addEventListener("click", () => {
      state.selectedPlanId = tr.dataset.planId;
      state.selectedTaskId = ""; // 选计划行 → 侧栏回到计划详情
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
  if (plan.source === "engineering") {
    return `
      <div class="plan-edit-card">
        <div class="plan-edit-head">
          <b>工程计划详情</b>
          <span class="plan-edit-tag custom">${escapeHTML(planBucketLabel(plan.status) || "待执行")}</span>
        </div>
        <div class="td-title">${escapeHTML(plan.name)}</div>
        <div class="td-meta-list">
          <div><span>项目</span><b>${escapeHTML(plan.project)}</b></div>
          <div><span>类别</span><b>${escapeHTML(plan.category || plan.point || "-")}</b></div>
          <div><span>责任人</span><b>${escapeHTML(plan.owner)}</b></div>
          <div><span>周期</span><b>${escapeHTML(plan.frequency)}</b></div>
          <div><span>计划节点</span><b>${escapeHTML([plan.planStart, plan.planEnd].filter(Boolean).join(" 至 ") || plan.next)}</b></div>
          <div><span>预算</span><b>${escapeHTML(plan.budgetAmount ? `${Number(plan.budgetAmount).toLocaleString()} 元` : "-")}</b></div>
        </div>
        ${plan.desc ? `<div class="td-remark"><span>工作内容</span><p>${escapeHTML(plan.desc)}</p></div>` : ""}
        ${plan.remark ? `<div class="td-remark"><span>备注</span><p>${escapeHTML(plan.remark)}</p></div>` : ""}
        <div class="td-actions">
          <button class="td-btn primary" data-eng-plan-dispatch="${escapeHTML(plan.backendId || plan.id)}">派发执行任务</button>
          ${plan.latestTaskId ? `<button class="td-btn ghost" data-eng-task-link="${escapeHTML(plan.latestTaskId)}">查看任务进度</button>` : ""}
        </div>
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
  document.querySelectorAll("[data-eng-plan-dispatch]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const planId = btn.dataset.engPlanDispatch;
      const plan = state.engineeringPlans.find((item) => item.id === planId);
      if (!plan) return;
      try {
        // 创建（或复用）该计划的执行任务，并直接置「进行中」= 一键下发到移动端
        const res = await api("/api/engineering/tasks", {
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
        // 始终调一次状态更新：后端「创建任务」不会重算计划状态，只有「更新状态」才会，
        // 否则计划会停在「待执行」不跳「进行中」。
        if (taskId) {
          await api(`/api/engineering/tasks/${encodeURIComponent(taskId)}/status`, {
            method: "POST", body: JSON.stringify({ status: "进行中" }),
          });
        }
        toast("已派发并下发到移动端，任务进入「进行中」");
        await loadData(false);
        if (taskId) state.selectedTaskId = taskId;
        setPage("plan", false);
      } catch (error) {
        toast(error.message || "任务派发失败");
      }
    });
  });
  document.querySelectorAll("[data-eng-task-link]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.selectedTaskId = btn.dataset.engTaskLink; // 跳到与移动端挂钩的工程任务
      render();
    });
  });
  const form = document.getElementById("planEditForm");
  if (!form) return;
  form.addEventListener("submit", (e) => {
    e.preventDefault();
    savePlanFromAside(form);
  });
  document.getElementById("planEditDeleteBtn")?.addEventListener("click", () => {
    const id = form.dataset.planId;
    // 工程计划在后端，没有删除 API——不能假装删掉
    if ((state.engineeringPlans || []).some((p) => p.id === id)) {
      toast("工程计划与任务闭环挂钩，暂不支持删除");
      return;
    }
    if (!confirm("确认删除该计划？")) return;
    state.customPlans = state.customPlans.filter((p) => p.id !== id);
    saveLocalArray(ADMIN_PLANS_KEY, state.customPlans);
    state.selectedPlanId = "";
    toast("计划已删除");
    render();
  });
}

async function savePlanFromAside(form) {
  const data = Object.fromEntries(new FormData(form).entries());
  const id = form.dataset.planId;
  const isSeed = String(id).startsWith("seed_");
  const rows = filteredPlanRows();
  const src = rows.find((r) => r.id === id) || {};
  // 工程计划：合并编辑项后回写后端（整对象 upsert，避免清掉未编辑字段）。
  // 旧实现会把工程计划复制一份进 localStorage，造成真假两条计划并存。
  if (src.source === "engineering") {
    const full = (state.engineeringPlans || []).find((p) => p.id === id);
    if (!full) { toast("计划数据未加载，请刷新后重试"); return; }
    const merged = {
      ...full,
      workContent: (data.name || "").trim() || full.workContent,
      ownerName: (data.owner || "").trim() || full.ownerName,
      cycleText: data.frequency || full.cycleText,
      planEnd: (data.nextRun || "").trim() || full.planEnd,
      remark: (data.remark || "").trim(),
      // status 不回写：侧栏下拉是 启用/暂停 旧词表，工程计划状态由任务闭环自动推进
      status: full.status,
    };
    try {
      await api("/api/engineering/plans", { method: "POST", body: JSON.stringify(merged) });
      toast("计划已保存");
      await loadData(false);
      state.selectedPlanId = id;
      render();
    } catch (error) {
      toast(error.message || "计划保存失败");
    }
    return;
  }
  // seed 占位行「新建计划」：与抽屉一致，创建后端计划 + 下发首期任务
  if (isSeed) {
    const name = (data.name || "").trim() || src.name || "巡检计划";
    const owner = (data.owner || "").trim() || "巡检员";
    const nextRun = (data.nextRun || "").trim();
    try {
      const planRes = await api("/api/engineering/plans", {
        method: "POST",
        body: JSON.stringify({
          workContent: name,
          project: src.project || "-",
          category: src.point || src.templateName || "巡检点位",
          ownerName: owner,
          cycleText: data.frequency || "每日 09:00",
          planEnd: nextRun,
          scopeDesc: src.desc || "",
          remark: (data.remark || "").trim(),
          status: "待执行",
          source: "manual",
        }),
      });
      const planId = planRes?.plan?.id || "";
      if (planId) {
        await api("/api/engineering/tasks", {
          method: "POST",
          body: JSON.stringify({
            planItemId: planId,
            title: `${name} 巡检任务`,
            assigneeName: owner,
            dueAt: nextRun,
            taskType: "巡检计划执行",
            status: "待执行", // 后台「待执行」，点「下发」→进行中并上移动端
            source: "manual",
          }),
        });
      }
      toast("计划已创建，任务在「待执行」；到任务详情点「下发到移动端」即进入进行中并下发给巡检员");
      await loadData(false);
      state.selectedPlanId = planId;
      state.selectedTaskId = "";
      render();
    } catch (error) {
      toast(error.message || "计划创建失败");
    }
    return;
  }
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

function engineeringPlanRows() {
  return (state.engineeringPlans || [])
    .filter((plan) => !state.selectedProject || plan.project === state.selectedProject)
    .map((plan) => ({
      id: plan.id,
      backendId: plan.id,
      name: plan.workContent || "工程计划事项",
      project: plan.project || "-",
      point: plan.category || "-",
      pointId: "",
      templateId: "",
      templateName: plan.category || "工程计划",
      frequency: plan.cycleText || [plan.planStart, plan.planEnd].filter(Boolean).join(" 至 ") || "-",
      owner: plan.ownerName || "-",
      next: plan.planEnd || "-",
      status: plan.status || "待执行",
      aiPolicy: "工程计划闭环 + AI 资料分析",
      desc: plan.scopeDesc || plan.remark || "",
      remark: plan.remark || "",
      source: "engineering",
      category: plan.category || "",
      budgetAmount: plan.budgetAmount || 0,
      planStart: plan.planStart || "",
      planEnd: plan.planEnd || "",
      riskLevel: plan.riskLevel || "normal",
      latestTaskId: plan.latestTaskId || "",
    }));
}

function planRows() {
  const templateById = new Map(state.templates.map((tpl) => [tpl.id, tpl]));
  const engineeringRows = engineeringPlanRows();
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
  // customRows（localStorage 旧版"启用"计划）已弃用：新建计划统一走后端工程计划，
  // 这里不再并入，避免后台出现游离的"启用"假计划。
  void customRows;
  return [...engineeringRows, ...seedRows];
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
        ${taskColumn("逾期 / 待整改", tasks.overdue, "danger")}
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
  const editableTask = state.customTasks?.some((t) => t.id === task.id) || task.source === "engineering";
  const statusFlow = ["待执行", "进行中", "已完成"];
  // 待整改/逾期 按进行中档位点亮；待下发(历史遗留)归到待执行档
  const flowStatus = (task.status === "待整改" || task.status === "逾期") ? "进行中"
    : (task.status === "待下发" ? "待执行" : task.status);
  const curIdx = statusFlow.indexOf(flowStatus);
  return `
    <div class="task-detail-card">
      <div class="task-detail-head">
        <div>
          <b>任务详情</b>
          <span class="td-status ${taskStatusTone(task.status)}">${escapeHTML(task.status)}</span>
        </div>
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
        ${task.recordId ? `<div><span>关联记录</span><b>${escapeHTML(task.recordId)}</b></div>` : ""}
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
      ${task.recordId ? `
        <div class="td-actions">
          <button class="td-btn ghost" data-record-link="${escapeHTML(task.recordId)}">查看关联巡检记录</button>
        </div>
      ` : ""}
      <div class="td-flow">
        ${statusFlow.map((s, i) => `<div class="td-step ${i <= curIdx ? "done" : ""} ${i === curIdx ? "current" : ""}"><i></i><span>${s}</span></div>`).join("")}
      </div>
      ${editableTask ? `
        <div class="td-actions">
          ${["待执行", "待下发"].includes(task.status) ? `<span class="td-hint">待派发：在计划详情点「派发执行任务」即下发到移动端</span>` : ""}
          ${["进行中", "待整改", "逾期"].includes(task.status) ? `<span class="td-hint">已下发，巡检员可在移动端执行</span><button class="td-btn primary" data-task-action="done">标记完成</button>` : ""}
          ${task.status !== "已完成" ? `<button class="td-btn ghost" data-task-action="cancel">取消任务</button>` : `<button class="td-btn ghost" data-task-action="reopen">重新打开</button>`}
        </div>
      ` : `<div class="td-readonly">来自巡检记录，不可在此修改状态</div>`}
    </div>
  `;
}

function taskStatusTone(status) {
  if (status === "已完成") return "tone-success";
  if (status === "进行中") return "tone-info";
  if (status === "逾期" || status === "待整改") return "tone-danger";
  return "tone-warning";
}

function bindTaskDetailActions() {
  document.querySelectorAll("[data-task-action]").forEach((btn) => {
    btn.addEventListener("click", async () => {
      const id = state.selectedTaskId;
      const action = btn.dataset.taskAction;
      const map = { dispatch: "进行中", start: "进行中", done: "已完成", cancel: "已取消", reopen: "待执行" };
      const grouped = taskGroups();
      const current = [...grouped.pending, ...grouped.processing, ...grouped.done, ...grouped.overdue].find((t) => t.id === id);
      if (current?.source === "engineering") {
        const newStatus = map[action] || current.status;
        try {
          await api(`/api/engineering/tasks/${encodeURIComponent(id)}/status`, {
            method: "POST",
            body: JSON.stringify({ status: newStatus }),
          });
          toast(`任务已${newStatus}`);
          await loadData(false);
        } catch (error) {
          toast(error.message || "任务状态更新失败");
        }
        return;
      }
      const idx = state.customTasks.findIndex((t) => t.id === id);
      if (idx < 0) return;
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
  document.querySelectorAll("[data-record-link]").forEach((btn) => {
    btn.addEventListener("click", () => {
      const recordId = btn.dataset.recordLink;
      if (recordId) state.selectedRecordId = recordId;
      setPage("record");
    });
  });
}

function engineeringTaskRows() {
  return (state.engineeringTasks || [])
    .filter((task) => !state.selectedProject || task.project === state.selectedProject)
    .map((task) => ({
      id: task.id,
      title: task.title || task.workContent || "工程执行任务",
      planId: task.planItemId || "",
      planName: task.workContent || "",
      project: task.project || "-",
      pointName: task.category || "-",
      templateName: task.workContent || "",
      owner: task.assigneeName || "-",
      frequency: task.taskType || "工程计划执行",
      dueAt: task.dueAt || "-",
      aiPolicy: [task.evidenceStatus, task.aiStatus].filter(Boolean).join(" / ") || "待分析",
      remark: task.closeNote || "",
      priority: task.status === "逾期" ? "紧急" : "普通",
      source: "engineering",
      recordId: task.recordId || "",
      assetId: task.assetId || "",
      meta: `${task.project || "-"} · ${task.category || "-"} · ${task.assigneeName || "-"} · 截止 ${task.dueAt || "-"}`,
      status: task.status || "待执行",
      completedAt: task.completedAt || null,
      completedBy: task.reviewerName || task.assigneeName || "",
    }));
}

function taskGroups() {
  const records = filteredRecords();
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
  const engineeringTasks = engineeringTaskRows();
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
  engineeringTasks.forEach((task) => {
    if (task.status === "进行中") processing.unshift(task);
    else if (task.status === "已完成") done.unshift(task);
    else if (task.status === "逾期" || task.status === "待整改") overdue.unshift(task);
    else pending.unshift(task);
  });
  return { pending, processing, done, overdue };
}

function recordToTask(record) {
  return {
    title: record.templateName || record.pointName || "巡检任务",
    meta: `${record.pointName || "-"} · ${fmtTime(record.createdAt)} · ${recordBusinessStatus(record)}`,
    status: recordBusinessStatus(record),
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
          const main = recordBusinessStatus(record);
          return `
          <article data-record-select="${escapeHTML(record.id)}" class="${record.id === state.selectedRecordId ? "selected-card" : ""}">
            <time>${fmtTime(record.createdAt)}</time>
            <div><b>${escapeHTML(record.pointName || record.templateName || "巡检记录")}</b><span>${escapeHTML(recordNo(record))} · ${escapeHTML(primaryReading(record) || record.aiSummary || record.report || "-")}</span></div>
            <div class="status-col"><em class="status ${statusClass(main)}">${escapeHTML(main)}</em></div>
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
      <select id="recordStatusFilter"><option value="">全部状态</option>${RECORD_STATUS_OPTIONS.map((s) => opt(s, state.recordFilters.status)).join("")}</select>
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
    ${ledgerStatsBar(counts)}
    ${assetTablePanel(assets, "资产台账", "", true, `<button id="exportAssetsBtn">导出台账</button>`)}
  `;
  $("#pageAside").innerHTML = renderAssetSide(selectedAsset() || assets[0]);
  $("#exportAssetsBtn").addEventListener("click", (event) => {
    event.preventDefault();
    event.stopPropagation();
    exportAssetsWorkbook();
  });
  bindAssetFilters();
}

function ledgerStatsBar(counts) {
  const items = [
    { label: "资产总数", value: counts.total, sub: "台/套" },
    { label: "正常资产", value: counts.normal, sub: `${ratio(counts.normal, counts.total)}%`, tone: "good" },
    { label: "异常资产", value: counts.danger, sub: "需处理", tone: counts.danger > 0 ? "danger" : "good" },
    { label: "待复核", value: counts.warning + counts.repair, sub: "人工确认", tone: (counts.warning + counts.repair) > 0 ? "warn" : "" },
  ];
  return `
    <section class="ledger-stat-strip">
      ${items.map((item) => `
        <article class="ledger-stat ${item.tone || ""}">
          <span>${escapeHTML(item.label)}</span>
          <b>${escapeHTML(item.value)}</b>
        </article>
      `).join("")}
    </section>
  `;
}

function assetTablePanel(assets, title, desc, withFilters, action = "") {
  return `
    <section class="panel asset-table-panel">
      <div class="panel-head asset-panel-head">
        <div class="panel-title-block"><h2>${escapeHTML(title)}</h2>${desc ? `<p>${escapeHTML(desc)}</p>` : ""}</div>
        <div class="panel-actions asset-toolbar">${withFilters ? assetFiltersHTML() : `<button class="link-btn" data-page-link="ledger">全部台账</button>`}${action}</div>
      </div>
      <div class="table-wrap">
        <table>
          <thead><tr><th></th><th>资产</th><th>设备类型</th><th>安装位置</th><th>状态</th><th>最近巡检时间</th><th>操作</th></tr></thead>
          <tbody>${assets.map((asset) => `
            <tr data-asset-select="${escapeHTML(asset.id)}" class="${asset.id === state.selectedAssetId ? "selected" : ""}">
              <td><i></i></td>
              <td class="asset-cell">${((name, code) => name !== code ? '<b>' + escapeHTML(name) + '</b><span class="asset-code">' + escapeHTML(code) + '</span>' : '<b>' + escapeHTML(code) + '</b>')(asset.assetName || "未命名资产", assetKey(asset))}</td>
              <td>${escapeHTML(asset.assetType || "-")}</td>
              <td>${escapeHTML(locationText(asset))}</td>
              <td><span class="status ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</span></td>
              <td>${fmtTime(asset.lastInspectedAt)}</td>
              <td><button class="view-link" data-asset-detail="${escapeHTML(asset.id)}" type="button">查看</button></td>
            </tr>
          `).join("") || emptyRow(7, "暂无资产数据")}</tbody>
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

function approvalCounts(requests) {
  return requests.reduce((acc, request) => {
    acc.total += 1;
    acc[request.status || "unknown"] = (acc[request.status || "unknown"] || 0) + 1;
    return acc;
  }, { total: 0, pending: 0, approved: 0, rejected: 0, withdrawn: 0, unknown: 0 });
}

function approvalRows() {
  const status = state.approvalStatus || "pending";
  return filteredRequests()
    .filter((request) => status === "all" || request.status === status)
    .sort((a, b) => {
      const weight = (request) => request.status === "pending" ? 0 : 1;
      return weight(a) - weight(b) || new Date(b.requestedAt || 0) - new Date(a.requestedAt || 0);
    });
}

function approvalTargetSummary(request) {
  if (request.targetType === "asset") {
    const asset = state.assets.find((item) => item.id === request.targetId);
    if (asset) {
      return {
        title: asset.assetName || "资产台账",
        meta: `设备编号 ${assetKey(asset)} · ${locationText(asset)}`,
      };
    }
    return { title: "资产台账", meta: request.targetId || "-" };
  }
  const record = state.records.find((item) => item.id === request.targetId);
  if (record) {
    return {
      title: record.templateName || record.pointName || "巡检记录",
      meta: `${recordNo(record)} · ${record.pointName || record.project || "-"} · ${fmtTime(record.createdAt)}`,
    };
  }
  return { title: "巡检记录", meta: request.targetId || "-" };
}

function approvalPatchSummary(request) {
  const patch = request.patch || {};
  const parts = [];
  // 字段订正：直接显示「字段名 → 新值」，这是审批的核心
  if (Array.isArray(patch.fields) && patch.fields.length) {
    const items = patch.fields.slice(0, 3).map((f) => `${f.label || f.code} → ${f.value}`);
    parts.push(items.join("、") + (patch.fields.length > 3 ? " 等" : ""));
  }
  if (patch.assetName) parts.push(`资产名称 → ${patch.assetName}`);
  if (patch.lastStatus) parts.push(`资产状态 → ${patch.lastStatus}`);
  if (patch.inspector) parts.push(`巡检人 → ${patch.inspector}`);
  if (patch.addImages && Array.isArray(patch.addImages.imageIds)) {
    parts.push(`补交照片 ${patch.addImages.imageIds.length} 张`);
  }
  // 台账摘要 / AI 总结这类长文本不塞进卡片摘要，去详情看
  return parts.join("；") || "查看详情确认变更内容";
}

function approvalPatchDetailHTML(request) {
  const patch = request.patch || {};
  const items = [];
  if (Array.isArray(patch.fields)) {
    patch.fields.slice(0, 8).forEach((field) => {
      items.push([field.label || field.code || "字段", field.value ?? "-"]);
    });
  }
  if (patch.addImages && Array.isArray(patch.addImages.imageIds)) items.push(["补交照片", `${patch.addImages.imageIds.length} 张`]);
  if (patch.assetName) items.push(["资产名称", patch.assetName]);
  if (patch.lastStatus) items.push(["资产状态", patch.lastStatus]);
  if (patch.lastSummary !== undefined) items.push(["台账摘要", patch.lastSummary || "-"]);
  if (patch.inspector) items.push(["巡检人", patch.inspector]);
  if (patch.aiSummary !== undefined) items.push(["AI总结", patch.aiSummary || "-"]);
  return `
    <div class="approval-patch-grid">
      ${items.map(([label, value]) => `<div><span>${escapeHTML(label)}</span><b>${escapeHTML(String(value))}</b></div>`).join("") || `<div><span>变更</span><b>${escapeHTML(approvalPatchSummary(request))}</b></div>`}
    </div>
  `;
}

function approvalCardHTML(request) {
  const target = approvalTargetSummary(request);
  const status = request.status || "unknown";
  const pending = status === "pending";
  return `
    <article class="approval-card approval-${escapeHTML(status)}" data-request-open="${escapeHTML(request.id)}">
      <div class="approval-card-main">
        <div class="approval-card-top">
          <span class="status ${status === "approved" ? "normal" : status === "rejected" ? "danger" : "warning"}">${escapeHTML(statusLabel(status))}</span>
          <em>${escapeHTML(shortId(request.id))}</em>
        </div>
        <b>${escapeHTML(target.title)}</b>
        <p>${escapeHTML(target.meta)}</p>
        <div class="approval-reason">申请原因：${escapeHTML(request.reason || "未填写")}</div>
        <div class="approval-patch">${escapeHTML(approvalPatchSummary(request))}</div>
      </div>
      <footer>
        <button class="btn-ghost" data-request-open="${escapeHTML(request.id)}">查看详情</button>
        ${pending ? `<button data-request-review="${escapeHTML(request.id)}" data-action="approve">通过申请</button><button class="danger" data-request-review="${escapeHTML(request.id)}" data-action="reject">驳回申请</button>` : ""}
      </footer>
    </article>
  `;
}

function approvalAssetExceptionHTML(asset) {
  return `
    <article class="approval-card approval-asset-exception" data-asset-select="${escapeHTML(asset.id)}">
      <div class="approval-card-main">
        <div class="approval-card-top">
          <span class="status ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "待复核")}</span>
          <em>${escapeHTML(assetKey(asset))}</em>
        </div>
        <b>${escapeHTML(asset.assetName || "异常资产")}</b>
        <p>${escapeHTML(locationText(asset))} · ${fmtTime(asset.lastInspectedAt)}</p>
        <div class="approval-patch">${escapeHTML(truncateText(asset.lastSummary || "待复核", 52))}</div>
      </div>
      <footer>
        <button class="btn-ghost" data-asset-detail="${escapeHTML(asset.id)}">查看档案</button>
        <button data-asset-request="${escapeHTML(asset.id)}">转审批</button>
      </footer>
    </article>
  `;
}

function renderApprovalPage() {
  const all = filteredRequests();
  const counts = approvalCounts(all);
  const rows = approvalRows();
  const filters = [
    ["pending", "待审批", counts.pending],
    ["all", "全部", counts.total],
    ["approved", "已通过", counts.approved],
    ["rejected", "已驳回", counts.rejected],
  ];
  $("#pageMain").innerHTML = `
    <section class="panel approval-workbench">
      <div class="panel-head approval-head">
        <div><h2>审批中心</h2></div>
        <div class="approval-head-actions">
          <button class="btn-ghost" id="exportApprovalsBtn">导出</button>
          <button data-drawer="approval">审批设置</button>
        </div>
      </div>
      <div class="approval-filter-bar">
        ${filters.map(([key, label, count]) => `<button class="${state.approvalStatus === key ? "active" : ""}" data-approval-filter="${key}">${label}<b>${count}</b></button>`).join("")}
      </div>
      <div class="approval-list approval-list-modern">
        ${rows.map(approvalCardHTML).join("")}
        ${!rows.length ? `<div class="empty-state">当前筛选下暂无修改申请</div>` : ""}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = asideStack(approvalActivityCard());
  $("#exportApprovalsBtn")?.addEventListener("click", exportApprovalsWorkbook);
  bindApprovalActivityPreview();
}

function exportApprovalsWorkbook() {
  const rows = filteredRequests();
  exportExcel(
    "智巡-审批记录",
    "JADEAST 智巡审批记录",
    ["序号", "申请编号", "申请时间", "申请人", "目标类型", "目标", "变更内容", "申请理由", "状态", "审批人", "审批意见", "审批时间"],
    rows.map((r, index) => [
      index + 1,
      r.id,
      fmtTime(r.requestedAt),
      r.requestedBy || "-",
      statusLabel(r.targetType),
      approvalTargetSummary(r).title,
      approvalPatchSummary(r),
      r.reason || "-",
      statusLabel(r.status),
      r.reviewedBy || r.reviewer || "-",
      r.reviewNote || "-",
      r.reviewedAt ? fmtTime(r.reviewedAt) : "-",
    ]),
    [["导出口径", "按当前页签筛选导出修改申请，含变更内容、理由与审批结果"]],
  );
  toast(`已导出 ${rows.length} 条审批记录 Excel`);
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

// ===== 数据趋势图:可切换 巡检量&异常量 / 异常率 / 风险走势 =====
// 客户端按周期聚合 records:≤30天按天,更长按周。
function buildTrendSeries(records, period) {
  let totalDays, bucketDays;
  if (period === "today" || period === "week") { totalDays = 7; bucketDays = 1; }
  else if (period === "month") { totalDays = 30; bucketDays = 1; }
  else { totalDays = 84; bucketDays = 7; }
  const bucketCount = Math.round(totalDays / bucketDays);
  const today = new Date(); today.setHours(0, 0, 0, 0);
  const inspect = [], abnormal = [], labels = [];
  for (let i = 0; i < bucketCount; i++) {
    const end = new Date(today); end.setDate(end.getDate() - (bucketCount - 1 - i) * bucketDays);
    const start = new Date(end); start.setDate(start.getDate() - (bucketDays - 1));
    const s = start.getTime(), e = end.getTime() + 86399999;
    const inB = records.filter((r) => { const t = new Date(r.createdAt || 0).getTime(); return t >= s && t <= e; });
    inspect.push(inB.length);
    abnormal.push(inB.filter((r) => recordLevel(r) !== "normal").length);
    labels.push((end.getMonth() + 1) + "/" + end.getDate());
  }
  const rate = inspect.map((cnt, i) => (cnt ? Math.round((abnormal[i] / cnt) * 100) : 0));
  return { labels, inspect, abnormal, rate, bucketDays };
}

const TREND_METRICS = [
  { key: "volume", label: "巡检量 / 异常量" },
  { key: "rate",   label: "异常率" },
  { key: "risk",   label: "风险参考" },
];

function renderTrendChart(records, period, insights = {}, assets = []) {
  const metric = state.trendMetric || "volume";
  const s = buildTrendSeries(records, period);
  const ov = insights.overview || {};
  const W = 820, H = 250, padL = 34, padT = 16, padB = 26;
  const innerH = H - padT - padB;
  const n = s.labels.length;
  const stepX = (W - padL - 10) / Math.max(1, n - 1);
  const xOf = (i) => padL + i * stepX;
  const yOf = (v, maxV) => padT + innerH - (maxV ? (v / maxV) * innerH : 0);

  let lines, maxV, yFmt;
  if (metric === "volume") {
    maxV = Math.max(1, ...s.inspect);
    lines = [
      { values: s.inspect, color: "#246bfe", name: "巡检量", fill: true },
      { values: s.abnormal, color: "#ef4b3f", name: "异常量", fill: false },
    ];
    yFmt = (v) => String(Math.round(v));
  } else if (metric === "rate") {
    maxV = Math.max(10, ...s.rate);
    lines = [{ values: s.rate, color: "#f59e0b", name: "异常率 %", fill: true }];
    yFmt = (v) => Math.round(v) + "%";
  } else {
    const risk = s.rate.map((r) => Math.min(100, Math.round(r * 1.2)));
    maxV = 100;
    lines = [{ values: risk, color: "#f59e0b", name: "风险参考", fill: true }];
    yFmt = (v) => String(Math.round(v));
  }

  const grid = [0, 0.25, 0.5, 0.75, 1].map((f) => {
    const y = padT + innerH - f * innerH;
    return `<line x1="${padL}" y1="${y.toFixed(1)}" x2="${W - 10}" y2="${y.toFixed(1)}" class="trend-grid"/>` +
           `<text x="${padL - 6}" y="${(y + 3).toFixed(1)}" class="trend-ytick">${escapeHTML(yFmt(maxV * f))}</text>`;
  }).join("");

  const xEvery = Math.max(1, Math.ceil(n / 8));
  const xLabels = s.labels.map((lb, i) => {
    if (i % xEvery !== 0 && i !== n - 1) return "";
    return `<text x="${xOf(i).toFixed(1)}" y="${H - 7}" class="trend-xtick">${escapeHTML(lb)}</text>`;
  }).join("");

  const linesSvg = lines.map((ln, idx) => {
    const path = ln.values.map((v, i) => (i === 0 ? "M" : "L") + xOf(i).toFixed(1) + "," + yOf(v, maxV).toFixed(1)).join(" ");
    const gid = `trendFill-${metric}-${idx}`;
    const area = ln.fill
      ? `<defs><linearGradient id="${gid}" x1="0" y1="0" x2="0" y2="1"><stop offset="0%" stop-color="${ln.color}" stop-opacity="0.18"/><stop offset="100%" stop-color="${ln.color}" stop-opacity="0"/></linearGradient></defs><path d="${path} L${xOf(n - 1).toFixed(1)},${H - padB} L${padL},${H - padB} Z" fill="url(#${gid})"/>`
      : "";
    const dots = ln.values.map((v, i) => `<circle class="trend-dot" cx="${xOf(i).toFixed(1)}" cy="${yOf(v, maxV).toFixed(1)}" r="2.3" fill="${ln.color}"><title>${escapeHTML(s.labels[i])} · ${escapeHTML(ln.name)}：${escapeHTML(yFmt(v))}</title></circle>`).join("");
    const lastIndex = ln.values.length - 1;
    const lastY = yOf(ln.values[lastIndex], maxV);
    const lastLabelY = Math.max(padT + 10, lastY - 8);
    const latest = `<text class="trend-latest" x="${(xOf(lastIndex) - 4).toFixed(1)}" y="${lastLabelY.toFixed(1)}" fill="${ln.color}">${escapeHTML(yFmt(ln.values[lastIndex]))}</text>`;
    return `${area}<path class="trend-series" d="${path}" fill="none" stroke="${ln.color}" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" vector-effect="non-scaling-stroke"/>${dots}${latest}`;
  }).join("");

  const legend = lines.map((ln) => `<span class="trend-legend-item"><i style="background:${ln.color}"></i>${escapeHTML(ln.name)}</span>`).join("");

  // 核心 KPI 融入趋势图卡片(替代原顶端独立 KPI 带)
  // 全部取 insights.overview —— 与 hero AI 摘要同源,避免数字打架
  const focusCount = (insights.items || []).length;
  const headKpis = [
    { label: "本期巡检", value: ov.recordRecent != null ? ov.recordRecent : records.length, unit: "次", cls: "" },
    { label: "异常", value: ov.abnormalRecent != null ? ov.abnormalRecent : 0, unit: "次", cls: (ov.abnormalRecent > 0) ? "danger" : "" },
    { label: "待复核", value: ov.pendingReviews != null ? ov.pendingReviews : 0, unit: "项", cls: (ov.pendingReviews > 0) ? "warning" : "" },
    { label: "重点关注", value: focusCount, unit: "台", cls: (focusCount > 0) ? "warning" : "" },
  ];

  return `
    <section class="trend-panel">
      <div class="trend-head">
        <div class="trend-title">
          <div>
            <h2>巡检趋势</h2>
          </div>
        </div>
        <div class="trend-switch" role="tablist">
          ${TREND_METRICS.map((m) => `<button class="trend-chip ${m.key === metric ? "active" : ""}" data-trend-metric="${m.key}" type="button">${escapeHTML(m.label)}</button>`).join("")}
        </div>
      </div>
      <div class="trend-kpis">
        ${headKpis.map((k) => `<div class="trend-kpi"><span class="tk-label">${escapeHTML(k.label)}</span><b class="${k.cls}">${escapeHTML(String(k.value))}<em>${escapeHTML(k.unit || "")}</em></b></div>`).join("")}
      </div>
      <div class="trend-legend">${legend}</div>
      <div class="trend-canvas"><svg viewBox="0 0 ${W} ${H}" preserveAspectRatio="xMidYMid meet" class="trend-svg">${grid}${linesSvg}${xLabels}</svg></div>
    </section>
  `;
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

// ① 数据看板工具栏:范围、项目和更新时间。
function renderInsightHero(insights, periodDef) {
  const updated = insights.generatedAt ? fmtTime(insights.generatedAt) : "—";
  return `
    <section class="insight-hero insight-toolbar">
      <div class="insight-hero-top">
        <div class="insight-hero-title">
          <h1>趋势分析</h1>
        </div>
        <div class="insight-toolbar-actions">
          <span class="insight-hero-meta">更新于 ${escapeHTML(updated)}</span>
          <button class="insight-hero-refresh" data-action="refresh-insights">重新分析</button>
        </div>
      </div>
      <div class="data-period-tabs" role="tablist">
        ${DATA_PERIODS.map((p) => `
          <button class="data-period-chip ${p.key === state.dataPeriod ? "active" : ""}" data-period="${p.key}" type="button">${escapeHTML(p.label)}</button>
        `).join("")}
      </div>
    </section>
  `;
}

// ② AI 解读:趋势主图右侧仅保留三条结论和一条行动建议。
function renderTrendInsight(insights) {
  const summary = insights.summary || (insights.items.length === 0
    ? "当前暂无重点关注资产。"
    : "正在生成趋势解读。");
  const points = String(summary).split(/[。；\n]/).map((s) => s.trim()).filter(Boolean);
  const conclusions = points.filter((p) => !/^(建议|下一步)/.test(p)).slice(0, 3);
  const top = insights.items[0] || {};
  const advice = points.find((p) => /^(建议|下一步)/.test(p))
    || top.action
    || "结合重点资产列表安排复核。";
  return `
    <aside class="trend-ai-panel">
      <div class="trend-ai-head">
        <div><span>AI 解读</span><h2>趋势结论</h2></div>
      </div>
      <div class="trend-ai-focus">
        <span>需关注资产</span>
        <b>${insights.items.length}<em>台</em></b>
      </div>
      <ul class="trend-ai-list">
        ${(conclusions.length ? conclusions : ["当前数据较少，建议先完成巡检记录积累。"]).map((p) => `<li>${escapeHTML(humanizeFieldNames(p))}</li>`).join("")}
      </ul>
      <div class="trend-ai-action">
        <span>建议动作</span>
        <p>${escapeHTML(humanizeFieldNames(advice))}</p>
      </div>
    </aside>
  `;
}

// ③ 核心指标带:合并原顶部 AI 综合 + 底部基础统计,一行看全(去重 KPI)
function renderRiskKpi(insights, records = [], assets = [], requests = []) {
  const ov = insights.overview || {};
  const top = insights.items[0];
  const riskIndex = top ? Math.min(100, top.riskScore) : 0;
  const focusCount = insights.items.length;
  const counts = statusCounts(assets);
  const abnormalCnt = counts.warning + counts.danger + counts.repair;
  const closedPct = closedRate();
  const acc = aiAccuracy(records);
  const accTxt = acc.sample < 5 ? "—" : `${acc.value}%`;
  const riskClass = riskIndex >= 60 ? "danger" : (riskIndex >= 25 ? "warning" : "normal");
  const deltaTxt = (ov.abnormalRecent != null && ov.abnormalPrev != null)
    ? (ov.abnormalRecent > ov.abnormalPrev ? `↑ +${ov.abnormalRecent - ov.abnormalPrev} vs 上期` :
       ov.abnormalRecent < ov.abnormalPrev ? `↓ -${ov.abnormalPrev - ov.abnormalRecent} vs 上期` : `↔ 与上期持平`)
    : "—";
  const kpis = [
    { label: "本期巡检", value: records.length, unit: "条", sub: `资产 ${counts.total} 台`, cls: "" },
    { label: "异常 / 待复核", value: abnormalCnt, unit: "项", sub: abnormalCnt > 0 ? "需处理" : "全清", cls: abnormalCnt > 0 ? "danger" : "" },
    { label: "风险指数", value: riskIndex, unit: "/100", sub: deltaTxt, cls: riskClass },
    { label: "重点关注", value: focusCount, unit: "台", sub: "AI 综合判定", cls: focusCount > 0 ? "warning" : "" },
    { label: "闭环率", value: closedPct, unit: "%", sub: "复核→审批→台账", cls: "" },
    { label: "AI 识别准确率", value: accTxt, unit: "", sub: acc.sample < 5 ? `样本 ${acc.sample}/5` : `${acc.sample} 字段`, cls: "" },
  ];
  return `
    <section class="risk-kpi-row core-kpi-row">
      ${kpis.map((k) => `
        <article class="risk-kpi ${k.cls}">
          <span class="risk-kpi-label">${escapeHTML(k.label)}</span>
          <b class="risk-kpi-value">${escapeHTML(String(k.value))}<em>${escapeHTML(k.unit)}</em></b>
          <span class="risk-kpi-sub">${escapeHTML(k.sub)}</span>
        </article>
      `).join("")}
    </section>
  `;
}

// 把 AI 文本里的字段 code(如 buttons_display)映射成中文 label,避免技术字段名暴露给业务方
function fieldLabelMap() {
  const map = {};
  (state.records || []).forEach((r) => (r.fields || []).forEach((f) => {
    if (f.code && f.label) map[f.code] = f.label;
  }));
  return map;
}
function humanizeFieldNames(text) {
  if (!text) return text;
  const map = fieldLabelMap();
  return String(text).replace(/[a-z][a-z0-9]*(?:_[a-z0-9]+)+/g, (tok) => (map[tok] ? `「${map[tok]}」` : tok));
}

// ③ 重点关注 Top 5
function renderFocusBoard(insights) {
  if (!insights.items.length) {
    return `<section class="focus-board"><div class="focus-board-head"><h2>近期重点关注</h2></div><div class="empty-state">暂无需重点关注的资产</div></section>`;
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
        <div><h2>近期重点关注</h2></div>
        <span class="focus-board-count">${items.length} 台需关注</span>
      </div>
      <article class="focus-feature focus-${featured.riskLevel}" data-asset-select="${escapeHTML(featured.assetId)}">
        <div class="focus-feature-top"><span>建议优先复核</span><strong>${featured.riskScore}<em>分</em></strong></div>
        <h3>${escapeHTML(featured.assetName || "—")}</h3>
        <div class="focus-feature-reasons">${(featured.reasons || []).slice(0, 3).map((reason) => `<span>${escapeHTML(humanizeFieldNames(reason))}</span>`).join("")}</div>
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
              <p class="focus-card-summary">${escapeHTML(humanizeFieldNames((it.reasons && it.reasons[0]) || "建议查看历史巡检变化"))}</p>
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
    return `<section class="status-heatmap-panel"><div class="panel-head"><h2>资产状态周视图</h2></div><div class="empty-state">暂无资产</div></section>`;
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
        <div><h2>资产状态周视图</h2></div>
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
      <h4>数值字段漂移</h4>
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
      <h4>状态字段重复异常</h4>
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
      <div class="drift-board-head"><h2>字段漂移看板</h2></div>
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
      <div class="iq-head"><h2>巡检员质量榜</h2></div>
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
              <td class="${r.total ? "" : "zero"}">${r.total}</td>
              <td class="${r.retakeCount > 0 ? "warn" : "zero"}">${r.retakeCount}</td>
              <td class="${r.uncertainCount > 0 ? "warn" : "zero"}">${r.uncertainCount}</td>
              <td class="${r.noPhotoConfirm > 0 ? "danger" : "zero"}">${r.noPhotoConfirm}</td>
              <td class="${r.fastConfirmCount > 0 ? "danger" : "zero"}">${r.fastConfirmCount}</td>
              <td class="${r.avgDurationMs > 0 ? "" : "zero"}">${r.avgDurationMs > 0 ? (r.avgDurationMs / 1000).toFixed(1) + "s" : "—"}</td>
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
      <div class="pc-head"><h2>异常本期 vs 上期</h2></div>
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
  // 基础统计已并入核心指标带,这里只保留独有的「资产类型分布」
  const typeMap = {};
  assets.forEach((a) => {
    const k = a.assetType || "未分类";
    typeMap[k] = (typeMap[k] || 0) + 1;
  });
  const typeList = Object.entries(typeMap).sort((a, b) => b[1] - a[1]).slice(0, 6);
  const typeMax = Math.max(1, ...typeList.map(([, n]) => n));
  const TYPE_COLORS = ["#12a968", "#246bfe", "#f59e0b", "#8b5cf6", "#ef4b3f", "#06b6d4"];
  return `
    <section class="insight-aux">
      <div class="insight-aux-head"><h2>资产类型分布</h2></div>
      <div class="type-bars type-bars-wide">
        ${typeList.map(([name, n], i) => {
          const pct = Math.round((n / typeMax) * 100);
          const color = TYPE_COLORS[i % TYPE_COLORS.length];
          return `<div class="type-bar"><span class="type-name">${escapeHTML(name)}</span><div class="type-track"><div class="type-fill" style="width:${pct}%; background:${color}"></div></div><span class="type-num"><b>${n}</b></span></div>`;
        }).join("") || `<div class="empty-state">暂无</div>`}
      </div>
    </section>
  `;
}

// ⑨ 底栏元信息
function renderInsightFooter(insights) {
  const upd = insights.generatedAt ? fmtTime(insights.generatedAt) : "—";
  // mock 期把 model 显示成「DeepSeek-V4 · 预览」,不暴露 deepseek-v4-pro/flash 的具体名
  const modelLabel = insights.isMock ? "DeepSeek 大模型 · 预览模式" : "DeepSeek 大模型驱动";
  const rangeLabel = { "1d": "近 1 天", "7d": "近 7 天", "30d": "近 30 天", "90d": "近 90 天", "365d": "近一年" }[insights.rangeKey] || "";
  return `
    <footer class="insight-footer">
      <span>数据更新 ${escapeHTML(upd)}</span>
      <span>·</span>
      <span>${escapeHTML(modelLabel)}</span>
      ${rangeLabel ? `<span>·</span><span>${escapeHTML(rangeLabel)}</span>` : ""}
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
    <div class="trend-command-grid">
      ${renderTrendChart(records, state.dataPeriod, insights, assets)}
      ${renderTrendInsight(insights)}
    </div>
    <div class="board-divider"><span>资产与质量明细</span></div>
    ${renderStatusHeatmap(periodDef, insights)}
    ${renderFocusBoard(insights)}
    ${renderInspectorQuality(insights)}
    ${renderDriftBoard(insights)}
    ${renderInsightFooter(insights)}
  `;
  $("#pageAside").innerHTML = "";
  document.querySelectorAll("[data-period]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.dataPeriod = btn.dataset.period;
      render();
    });
  });
  document.querySelectorAll("[data-trend-metric]").forEach((btn) => {
    btn.addEventListener("click", () => {
      state.trendMetric = btn.dataset.trendMetric;
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
      { label: "异常待处理", value: abnormal, sub: "资产状态", bad: abnormal > 0 },
      { label: "操作日志", value: logs.length, sub: "本人动作" },
    ])}
    <section class="page-grid two profile-grid">
      <article class="panel profile-panel">
        <div class="panel-head"><h2>权限范围</h2></div>
        <div class="permission-list profile-permissions">
          ${permissionItem("资产台账", "查看 / 修改 / 追踪状态", true)}
          ${permissionItem("审批中心", "处理异常与修改申请", user.roleCode !== "inspector")}
          ${permissionItem("审批中心", "审批字段修正与台账修改", user.roleCode === "admin" || user.roleCode === "manager" || user.roleCode === "supervisor")}
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
          <span>审批中心</span><b>${pending}</b><em>去审批 →</em>
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
  "change_request.create": "提交修改申请",
  "change_request.approve": "审批通过",
  "change_request.reject": "审批驳回",
  "change_request.withdraw": "撤回申请",
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
  task: renderPlanPage,
  record: renderRecordPage,
  ledger: renderLedgerPage,
  device: renderLedgerPage,
  exception: renderApprovalPage,
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

// 右栏:精简预览 —— 关键信息一屏看完,点"查看完整档案"开抽屉看全部
function renderAssetSide(asset) {
  if (!asset) return `<div class="detail-head"><h2>资产详情</h2></div><div class="empty-state">暂无资产数据</div>`;
  const localHist = state.records.filter((record) => record.id === asset.lastRecordId || record.pointId === asset.pointId);
  const photos = collectAssetPhotos(asset, localHist);
  // 串联工程闭环：该资产存在「待整改」任务时直接给出跳转，不用去计划页翻找
  const followTasks = engineeringTaskRows().filter((t) => t.assetId === asset.id && t.status === "待整改");
  const followHTML = followTasks.length ? `
      <div class="asset-follow">
        ${followTasks.map((t) => `
          <button class="asset-follow-item" data-task-jump="${escapeHTML(t.id)}" type="button">
            <span class="pill danger">待整改</span>
            <span class="af-title">${escapeHTML(t.title)}</span>
            <em>查看任务 →</em>
          </button>
        `).join("")}
      </div>` : "";
  return `
    <div class="detail-head asset-detail-head"><h2>资产预览</h2></div>
    <div class="asset-preview">
      <div class="asset-preview-photo">${photos[0] ? `<img src="${escapeHTML(photos[0])}" alt="">` : `<div class="asset-preview-noimg">暂无照片</div>`}</div>
      <div class="asset-preview-title"><h3>${escapeHTML(asset.assetName || "未命名资产")}</h3><span class="pill ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</span></div>
      <div class="kv-list asset-preview-kv">
        <div><span>设备编号</span><b>${escapeHTML(assetKey(asset))}</b></div>
        <div><span>设备类型</span><b>${escapeHTML(asset.assetType || "-")}</b></div>
        <div><span>安装位置</span><b>${escapeHTML(locationText(asset))}</b></div>
        <div><span>最近巡检</span><b>${fmtTime(asset.lastInspectedAt)}</b></div>
        <div><span>巡检次数</span><b>${asset.inspectionCount || 0} 次</b></div>
      </div>
      <div class="asset-preview-summary">${escapeHTML(truncateText(asset.lastSummary || "暂无管理摘要。", 90))}</div>
      ${followHTML}
      <button class="asset-preview-btn" data-asset-detail="${escapeHTML(asset.id)}" type="button">查看完整档案 →</button>
    </div>
  `;
}

function renderAssetSummaryCard(asset) {
  const raw = String(asset.lastSummary || "").trim();
  const sentences = raw.split(/[。；;]/).map((item) => item.trim()).filter(Boolean);
  const main = sentences[0] || "暂无管理摘要";
  const key = sentences.find((item) => item !== main && /关键|数据|巡检|编号|温度|电压|电流|读数|状态|记录/.test(item))
    || `最近巡检 ${fmtTime(asset.lastInspectedAt)}，累计 ${asset.inspectionCount || 0} 次`;
  const isNormal = normalizeStatus(asset.lastStatus) === "normal";
  const focus = isNormal ? "当前台账状态正常" : `当前状态：${asset.lastStatus || "未巡检"}`;
  return `
    <section class="asset-summary-card asset-summary-grid">
      <article>
        <span>当前结论</span>
        <p>${escapeHTML(main)}</p>
      </article>
      <article>
        <span>关键数据</span>
        <p>${escapeHTML(key)}</p>
      </article>
      <article>
        <span>关注点</span>
        <p>${escapeHTML(focus)}</p>
      </article>
    </section>
  `;
}

// 完整档案 —— 抽屉弹窗里展示,空间充足
function renderAssetFull(asset) {
  if (!asset) return `<div class="empty-state">暂无资产数据</div>`;
  if (!(asset.id in state.assetDetails)) loadAssetDetail(asset.id); // 未命中则后台拉历史+趋势，回来重渲染
  const detail = state.assetDetails[asset.id];
  const snapshots = detail ? detail.history : [];
  const trend = detail ? detail.trend : [];
  const total = detail ? detail.total : 0;
  const localHist = state.records.filter((record) => record.id === asset.lastRecordId || record.pointId === asset.pointId);
  const photos = collectAssetPhotos(asset, localHist);
  const trendHTML = trend.length ? `
    <div class="asset-trend">
      <h4>字段趋势 <small>近 90 天 · 后端规则计算</small></h4>
      <div class="trend-cards">${trend.map(renderTrendCard).join("")}</div>
    </div>` : "";
  const events = detail && detail.events;
  const eventsHTML = (events && events.inspections > 0) ? renderStatusEventsCard(events) : "";
  return `
    ${renderLoadErrorBanner(`assetDetail:${asset.id}`)}
    <div class="asset-card asset-card-v2 asset-card-full">
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
      ${renderAssetSummaryCard(asset)}
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
  const recMain = recordBusinessStatus(record);
  return `
    <div class="detail-head"><h2>记录详情</h2></div>
    ${renderLoadErrorBanner(`confirmLogs:${record.id}`)}
    <div class="side-stack">
      <div class="info-card"><b>记录编号</b><span>${escapeHTML(recordNo(record))}</span></div>
      <div class="info-card"><b>AI 总结 <em class="status ${statusClass(recMain)}">${escapeHTML(recMain)}</em></b><span>${escapeHTML(record.aiSummary || record.report || "暂无总结")}</span></div>
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
      <div class="panel-head"><h2>待处理队列</h2><button class="link-btn" data-page-link="approval">全部</button></div>
      <div class="queue-list">${items.map((item) => `<div class="queue-item" ${item.assetId ? `data-asset-select="${escapeHTML(item.assetId)}"` : ""} ${item.requestId ? `data-request-open="${escapeHTML(item.requestId)}"` : ""}><i class="dot ${item.level === "danger" ? "danger" : "warning"}"></i><div><span class="queue-title">${escapeHTML(item.title)}</span><span class="queue-meta">${escapeHTML(item.meta)}</span></div><button class="queue-action">待处理</button></div>`).join("") || `<div class="empty-state">暂无待处理事项</div>`}</div>
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

async function createAssetReviewRequest(id) {
  const asset = state.assets.find((item) => item.id === id);
  if (!asset) return;
  const existing = state.requests.find((item) => item.status === "pending" && item.targetType === "asset" && item.targetId === id);
  if (existing) {
    openRequestDrawer(existing.id);
    return;
  }
  const summary = asset.lastSummary || "主管复核后确认正常";
  const request = await api("/api/change-requests", {
    method: "POST",
    body: JSON.stringify({
      targetType: "asset",
      targetId: id,
      patch: { lastStatus: "正常", lastSummary: summary },
      reason: "异常复核：确认正常",
    }),
  });
  toast("处理单已生成");
  await loadData(false);
  if (request?.id) openRequestDrawer(request.id);
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
    <h3>变更内容</h3>${approvalPatchDetailHTML(request)}
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

function planPointOptions(proj) {
  const list = (state.points || []).filter((p) => !proj || p.project === proj);
  if (!list.length) return `<option value="">该项目暂无点位，请先维护</option>`;
  return list.map((point) => optionHTML(point.id, `${point.name || point.location || "-"}`)).join("");
}

function planTemplateOptions(proj) {
  const list = (state.templates || []).filter((t) => !proj || t.project === proj);
  if (!list.length) return `<option value="">该项目暂无模板</option>`;
  return list.map((tpl) => optionHTML(tpl.id, `${tpl.name || "-"} / ${tpl.assetType || "-"}`)).join("");
}

function openPlanDrawer() {
  const projectList = projects();
  const initialProject = state.selectedProject || projectList[0] || "";
  openDrawer("新建巡检计划", `
    <form class="drawer-form" id="adminPlanForm">
      <section class="drawer-section">
        <h3>计划信息</h3>
        <div class="form-grid">
          <label>计划名称<input name="name" placeholder="如：能耗抄表每日巡检" required></label>
          <label>所属项目<select name="project" required>${projectList.map((name) => optionHTML(name, name, initialProject)).join("") || optionHTML("默认项目", "默认项目")}</select></label>
          <label>巡检点位<select name="pointId">${planPointOptions(initialProject)}</select></label>
          <label>日报模板<select name="templateId">${planTemplateOptions(initialProject)}</select></label>
          <label>执行频次<select name="frequency" required>
            ${["每日 09:00", "每日 18:00", "每周一 09:00", "每月 1 日 09:00", "临时计划"].map((item) => optionHTML(item, item)).join("")}
          </select></label>
          <label>首次执行<input name="firstRun" type="datetime-local" value="${defaultDateTimeInput(18)}" required></label>
          <label>责任人<select name="owner">${ownerOptions(state.currentUser?.displayName || "张三")}</select></label>
          <label>AI 策略<select name="aiPolicy">
            ${["视觉识别 + 人工确认", "视觉识别 + 审批闭环", "仅人工填写", "先识别失败再人工兜底"].map((item) => optionHTML(item, item)).join("")}
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
  // 项目联动：切换项目时，点位与模板自动过滤为该项目的
  const planForm = document.getElementById("adminPlanForm");
  const projSel = planForm?.querySelector('select[name="project"]');
  projSel?.addEventListener("change", () => {
    const proj = projSel.value;
    const pointSel = planForm.querySelector('select[name="pointId"]');
    const tplSel = planForm.querySelector('select[name="templateId"]');
    if (pointSel) pointSel.innerHTML = planPointOptions(proj);
    if (tplSel) tplSel.innerHTML = planTemplateOptions(proj);
  });
}

function openTaskDrawer() {
  const plans = planRows();
  openDrawer("新增计划执行项", `
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

// 新建计划走后端：创建工程计划 + 下发首期任务，移动端「我的任务」即刻可见、可执行。
// （旧实现写 localStorage customPlans，状态"启用"却永远生不出任务，是个死胡同。）
async function createAdminPlan(form) {
  const template = state.templates.find((tpl) => tpl.id === form.get("templateId")) || {};
  const point = state.points.find((item) => item.id === form.get("pointId")) || {};
  const project = String(form.get("project") || point.project || template.project || "默认项目");
  const name = String(form.get("name") || template.name || point.name || "巡检计划").trim();
  const owner = String(form.get("owner") || "巡检员");
  const nextRun = displayDateTimeInput(form.get("firstRun"));
  try {
    const planRes = await api("/api/engineering/plans", {
      method: "POST",
      body: JSON.stringify({
        workContent: name,
        project,
        category: point.name || point.location || template.assetType || "巡检点位",
        ownerName: owner,
        cycleText: String(form.get("frequency") || "每日 09:00"),
        planEnd: nextRun === "-" ? "" : nextRun,
        scopeDesc: `${point.name || point.location || "点位"}，使用 ${template.name || "默认"} 模板。`,
        remark: String(form.get("remark") || "").trim(),
        status: "待执行",
        source: "manual",
      }),
    });
    const planId = planRes?.plan?.id || "";
    if (planId) {
      await api("/api/engineering/tasks", {
        method: "POST",
        body: JSON.stringify({
          planItemId: planId,
          title: `${name} 巡检任务`,
          assigneeName: owner,
          dueAt: nextRun === "-" ? "" : nextRun,
          taskType: "巡检计划执行",
          status: "待执行",
          source: "manual",
        }),
      });
    }
    closeDrawer();
    await loadData(false);
    state.selectedPlanId = planId;
    state.selectedTaskId = "";
    setPage("plan", false);
    toast("计划已创建，任务在「待执行」；到任务详情点「下发到移动端」即进入进行中并下发给巡检员");
  } catch (error) {
    toast(error.message || "计划创建失败");
  }
}

async function createAdminTask(form) {
  const plans = planRows();
  const plan = plans[Number(form.get("planIndex"))] || {};
  if (plan.source === "engineering") {
    await api("/api/engineering/tasks", {
      method: "POST",
      body: JSON.stringify({
        planItemId: plan.backendId || plan.id,
        title: String(form.get("title") || `${plan.name || "工程计划"}执行任务`).trim(),
        assigneeName: String(form.get("owner") || plan.owner || "巡检员"),
        dueAt: displayDateTimeInput(form.get("dueAt")),
        status: String(form.get("status") || "待执行"),
        closeNote: String(form.get("remark") || "").trim(),
      }),
    });
    closeDrawer();
    await loadData(false);
    setPage("plan", false);
    toast("执行项已加入计划");
    return;
  }
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
  setPage("plan", false);
  toast("执行项已加入计划");
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
    ["序号", "记录编号", "巡检时间", "所属项目", "巡检点位", "日报模板", "巡检人", "业务状态", "拍照次数", "主要识别", "字段明细", "AI 总结", "AI 建议"],
    rows.map((record, index) => [
      index + 1,
      recordNo(record),
      fmtTime(record.createdAt),
      record.project,
      record.pointName,
      record.templateName,
      record.inspector,
      recordBusinessStatus(record),
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

const SIDEBAR_COLLAPSED_KEY = "inspectai_admin_sidebar_collapsed";
function applySidebarCollapsed(collapsed) {
  document.body.classList.toggle("sidebar-collapsed", collapsed);
  localStorage.setItem(SIDEBAR_COLLAPSED_KEY, collapsed ? "1" : "0");
  // 收起时文字隐藏，用原生 title 作 hover 提示；展开时去掉避免多余气泡
  $$(".nav button").forEach((btn) => {
    if (collapsed) btn.title = btn.textContent.trim();
    else btn.removeAttribute("title");
  });
  const toggle = $("#sidebarToggle");
  if (toggle) toggle.setAttribute("aria-expanded", collapsed ? "false" : "true");
}

function bindEvents() {
  $$(".nav button").forEach((btn) => btn.addEventListener("click", () => setPage(btn.dataset.page)));
  $("#sidebarToggle")?.addEventListener("click", () =>
    applySidebarCollapsed(!document.body.classList.contains("sidebar-collapsed")));
  applySidebarCollapsed(localStorage.getItem(SIDEBAR_COLLAPSED_KEY) === "1");
  $("#projectSelect")?.addEventListener("change", (event) => {
    state.selectedProject = event.target.value;
    state.selectedAssetId = filteredAssets()[0]?.id || "";
    state.selectedRecordId = filteredRecords()[0]?.id || "";
    render();
  });
  $("#refreshBtn").addEventListener("click", () => loadData());
  $("#approvalShortcut")?.addEventListener("click", () => {
    state.approvalStatus = "pending";
    setPage("approval");
  });
  $("#systemConfigBtn").addEventListener("click", () => setPage("system"));
  $("#drawerClose").addEventListener("click", closeDrawer);
  $("#drawerMask").addEventListener("click", closeDrawer);
  window.addEventListener("popstate", () => {
    state.page = normalizedPage(new URLSearchParams(location.search).get("page") || "dashboard");
    render();
    applyDeepLink();
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
    const approvalFilter = event.target.closest("[data-approval-filter]")?.dataset.approvalFilter;
    const assetId = event.target.closest("[data-asset-select]")?.dataset.assetSelect;
    const assetDetailId = event.target.closest("[data-asset-detail]")?.dataset.assetDetail;
    const assetRequestId = event.target.closest("[data-asset-request]")?.dataset.assetRequest;
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
    if (assetRequestId) {
      event.preventDefault();
      event.stopPropagation();
      await createAssetReviewRequest(assetRequestId);
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
    const taskJump = event.target.closest("[data-task-jump]")?.dataset.taskJump;
    if (taskJump) {
      event.preventDefault();
      event.stopPropagation();
      state.selectedTaskId = taskJump; // 台账 → 计划页对应任务（与「查看任务进度」同机制）
      setPage("plan");
      return;
    }
    if (pageLink) {
      event.preventDefault();
      event.stopPropagation();
      setPage(pageLink);
      return;
    }
    if (approvalFilter) {
      event.preventDefault();
      event.stopPropagation();
      state.approvalStatus = approvalFilter;
      const url = new URL(location.href);
      url.searchParams.set("page", "approval");
      url.searchParams.set("approvalStatus", approvalFilter);
      history.replaceState({ page: "approval" }, "", url);
      render();
      return;
    }
    if (userBadge) {
      event.preventDefault();
      event.stopPropagation();
      setPage("profile");
      return;
    }
    if (assetDetailId) {
      event.preventDefault();
      event.stopPropagation();
      state.selectedAssetId = assetDetailId; // 同步选中,保证抽屉内编辑表单 selectedAsset() 正确
      const da = state.assets.find((a) => a.id === assetDetailId);
      if (da) {
        if (!(da.id in state.assetDetails)) await loadAssetDetail(da.id);
        openDrawer((da.assetName || "资产") + " · 完整档案", renderAssetFull(da));
      }
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
      try {
        await createAdminTask(new FormData(event.target));
      } catch (error) {
        toast(error.message || "任务派发失败");
      }
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
