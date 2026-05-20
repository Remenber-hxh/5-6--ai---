const API_BASE_KEY = "inspectai_admin_api_base";
const API_TOKEN_KEY = "inspectai_admin_token";

const pageLabels = {
  dashboard: "首页",
  plan: "巡检计划",
  task: "巡检任务",
  record: "巡检记录",
  ledger: "资产台账",
  device: "设备管理",
  exception: "异常复核",
  approval: "修改审批",
  data: "数据看板",
  report: "统计报表",
  system: "系统管理",
};

function defaultApiBase() {
  const { protocol, hostname } = window.location;
  if ((protocol === "http:" || protocol === "https:") && hostname) {
    return `${protocol}//${hostname}:18080`;
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
  currentUser: null,
  health: null,
  selectedProject: "",
  selectedAssetId: "",
  selectedRecordId: "",
  recordPage: 0,
  filters: {
    assetType: "",
    status: "",
    keyword: "",
  },
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => Array.from(document.querySelectorAll(selector));

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
  try {
    const data = await api("/api/auth/me");
    state.currentUser = data.user || null;
    updateUserBadge();
    return state.currentUser;
  } catch (error) {
    if (/404|not_found/i.test(error.message || "")) {
      state.currentUser = {
        id: "legacy_supervisor",
        username: "supervisor",
        displayName: "张管理员",
        roleCode: "supervisor",
        roleName: "本地主管",
        status: "legacy",
      };
      updateUserBadge();
      return state.currentUser;
    }
    throw error;
  }
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
  if (raw.includes("复核") || raw.includes("待") || raw === "warning") return "warning";
  return "unknown";
}

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

function recordLevel(record) {
  if (record.manualRequired || record.recognitionStatus === "manual_required") return "warning";
  const text = [
    record.aiSummary,
    record.report,
    record.retakeReason,
    ...(record.fields || []).map((field) => `${field.value} ${field.reason} ${field.needsReview ? "复核" : ""}`),
  ].join(" ");
  if (/异常|告警|故障|离线|不合格|超标|风险/.test(text)) return "danger";
  if (/复核|缺失|未填写|重拍|人工/.test(text)) return "warning";
  return "normal";
}

function recordStatus(record) {
  const level = recordLevel(record);
  if (level === "danger") return "异常";
  if (level === "warning") return "待复核";
  return record.submitted ? "已提交" : statusLabel(record.recognitionStatus);
}

function projects() {
  const names = [
    ...state.assets.map((item) => item.project || item.projectCode),
    ...state.records.map((item) => item.project),
    ...state.points.map((item) => item.project),
    ...state.templates.map((item) => item.project),
  ].filter(Boolean);
  return [...new Set(names)];
}

function filteredAssets() {
  const keyword = state.filters.keyword.trim().toLowerCase();
  return state.assets.filter((asset) => {
    if (state.selectedProject && asset.project !== state.selectedProject && asset.projectCode !== state.selectedProject) return false;
    if (state.filters.assetType && asset.assetType !== state.filters.assetType) return false;
    if (state.filters.status && asset.lastStatus !== state.filters.status) return false;
    if (!keyword) return true;
    return [
      asset.id,
      assetKey(asset),
      asset.assetName,
      asset.assetType,
      asset.project,
      asset.pointId,
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

function render() {
  $$(".nav button").forEach((btn) => btn.classList.toggle("active", btn.dataset.page === state.page));
  $("#pendingBadge").textContent = filteredRequests().filter((item) => item.status === "pending").length;
  updateUserBadge();
  const renderer = pageRenderers[state.page] || renderDashboardPage;
  renderer();
}

function updateUserBadge() {
  const el = $(".admin-user");
  if (!el) return;
  const user = state.currentUser || { displayName: "未登录", roleName: "请登录" };
  const name = user.displayName || user.username || "未登录";
  const initial = name.slice(0, 1) || "?";
  el.innerHTML = `<b>${escapeHTML(initial)}</b><span>${escapeHTML(name)}<small>${escapeHTML(user.roleName || user.roleCode || "")}</small></span>`;
  el.title = "当前登录用户";
}

function renderLogin(message = "") {
  $$(".nav button").forEach((btn) => btn.classList.remove("active"));
  $("#pendingBadge").textContent = "0";
  $("#pageMain").innerHTML = `
    <section class="login-panel">
      <div class="login-copy">
        <span>JADEAST 智巡后台</span>
        <h1>登录管理端</h1>
        <p>使用后台账号进入资产台账、异常复核、用户与权限管理。默认本地账号为 admin，密码为 InspectAI@2026，可在环境变量中调整。</p>
      </div>
      <form class="login-card" id="loginForm">
        <label>账号<input name="username" autocomplete="username" value="admin" required></label>
        <label>密码<input name="password" type="password" autocomplete="current-password" required></label>
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
}

function pageHero(scope, title, desc, action = "") {
  return `
    <section class="page-hero">
      <div><span>${escapeHTML(scope)}</span><h1>${escapeHTML(title)}</h1><p>${escapeHTML(desc)}</p></div>
      ${action}
    </section>
  `;
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

function renderDashboardPage() {
  const assets = filteredAssets();
  const records = filteredRecords();
  const requests = filteredRequests();
  const counts = statusCounts(assets);
  $("#pageMain").innerHTML = `
    ${pageHero("后台工作台", "运行总览", "汇总资产状态、巡检执行、异常复核和审批待办，作为主管进入后台后的第一屏。", `<button data-page-link="ledger">进入台账</button>`)}
    ${metrics([
      { label: "资产总数", value: counts.total, sub: "台/套" },
      { label: "正常资产", value: counts.normal, sub: `${ratio(counts.normal, counts.total)}%`, good: true },
      { label: "异常/待复核", value: counts.warning + counts.danger + counts.repair, sub: "需要处理", bad: counts.warning + counts.danger + counts.repair > 0 },
      { label: "待审批", value: requests.filter((item) => item.status === "pending").length, sub: "修改申请" },
    ])}
    <section class="page-grid two">
      <div class="grid-col">
        ${trendPanel(records)}
        ${assetTablePanel(assets.slice(0, 6), "资产台账", "展示最近更新的资产状态。", false)}
      </div>
      <div class="grid-col">
        ${exceptionQueuePanel(assets, requests)}
        ${recordListPanel(records.slice(0, 5), "巡检记录")}
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = renderAssetSide(selectedAsset());
}

function renderPlanPage() {
  const rows = planRows();
  const todayPlans = rows.filter((row) => row.frequency.includes("每日"));
  $("#pageMain").innerHTML = `
    ${pageHero("巡检管理", "巡检计划", "按项目、点位、频次和模板维护巡检计划，自动生成每日可执行任务。", `<button data-drawer="plan">新建计划</button>`)}
    ${metrics([
      { label: "计划数量", value: rows.length, sub: "来自点位与模板" },
      { label: "启用计划", value: rows.filter((row) => row.status === "启用").length, sub: "当前有效", good: true },
      { label: "今日执行", value: todayPlans.length, sub: "每日计划" },
      { label: "AI 模板", value: state.templates.filter((tpl) => tpl.hasAI).length, sub: "已配置视觉识别", good: true },
    ])}
    <section class="panel">
      <div class="panel-head"><h2>计划列表</h2><div class="filters"><span>全部项目</span><span>全部频次</span><span>全部状态</span><span class="search">搜索计划 / 点位</span></div></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>计划名称</th><th>项目</th><th>巡检点位</th><th>频次</th><th>责任人</th><th>下次执行</th><th>状态</th></tr></thead>
          <tbody>${rows.map((row) => `
            <tr>
              <td>${escapeHTML(row.name)}</td><td>${escapeHTML(row.project)}</td><td>${escapeHTML(row.point)}</td>
              <td>${escapeHTML(row.frequency)}</td><td>${escapeHTML(row.owner)}</td><td>${escapeHTML(row.next)}</td>
              <td><span class="status ${statusClass(row.status === "启用" ? "正常" : row.status)}">${escapeHTML(row.status)}</span></td>
            </tr>
          `).join("") || emptyRow(7, "暂无巡检计划")}</tbody>
        </table>
      </div>
    </section>
  `;
  $("#pageAside").innerHTML = `
    <div class="detail-head"><h2>计划详情</h2></div>
    <div class="side-stack">
      <div class="info-card"><b>${escapeHTML(rows[0]?.name || "暂无计划")}</b><span>${escapeHTML(rows[0]?.desc || "由点位和日报模板自动生成巡检计划。")}</span></div>
      ${steps(["计划配置", "生成任务", "执行巡检", "结果入账"], 1)}
    </div>
  `;
}

function planRows() {
  const templateById = new Map(state.templates.map((tpl) => [tpl.id, tpl]));
  const source = state.points.length ? state.points : state.templates.map((tpl) => ({
    id: tpl.id,
    project: tpl.project,
    name: tpl.name,
    location: tpl.assetType,
    templateId: tpl.id,
  }));
  return source
    .filter((point) => !state.selectedProject || point.project === state.selectedProject)
    .map((point, index) => {
      const tpl = templateById.get(point.templateId) || {};
      return {
        name: tpl.name || point.name || "巡检计划",
        project: point.project || tpl.project || "-",
        point: point.name || point.location || "-",
        frequency: index % 3 === 2 ? "每周一" : "每日 09:00",
        owner: index % 2 ? "巡检员" : "张三",
        next: index % 3 === 2 ? "下周一 09:00" : "明日 09:00",
        status: "启用",
        desc: `${point.location || point.name || "点位"}，使用 ${tpl.name || "默认"} 模板。`,
      };
    });
}

function renderTaskPage() {
  const tasks = taskGroups();
  $("#pageMain").innerHTML = `
    ${pageHero("巡检管理", "巡检任务", "展示待执行、进行中、已完成和逾期任务，主管可按点位追踪执行状态。", `<button data-drawer="task">派发任务</button>`)}
    <section class="task-board">
      ${taskColumn("待执行", tasks.pending, "warning")}
      ${taskColumn("进行中", tasks.processing, "normal")}
      ${taskColumn("已完成", tasks.done, "normal")}
      ${taskColumn("逾期", tasks.overdue, "danger")}
    </section>
  `;
  $("#pageAside").innerHTML = sideInfo("任务跟踪", [
    ["当前重点", tasks.processing[0]?.title || tasks.pending[0]?.title || "暂无任务"],
    ["字段状态", "识别后进入人工确认，确认后写入巡检记录与资产台账。"],
    ["异常规则", "缺字段、低置信度、人工修改都会进入复核或审批链路。"],
  ]);
}

function taskGroups() {
  const records = filteredRecords();
  const today = todayKey();
  const done = records.filter((record) => record.submitted).slice(0, 4).map(recordToTask);
  const processing = records.filter((record) => !record.submitted && record.recognitionStatus !== "not_started").slice(0, 4).map(recordToTask);
  const points = state.points.filter((point) => !state.selectedProject || point.project === state.selectedProject);
  const pending = points.slice(0, Math.max(2, 5 - processing.length)).map((point) => ({
    title: point.name || point.location || "巡检任务",
    meta: `${point.project || "-"} · ${point.location || "-"} · 今日待执行`,
    status: "待执行",
  }));
  const overdue = filteredAssets()
    .filter((asset) => normalizeStatus(asset.lastStatus) === "danger")
    .slice(0, 3)
    .map((asset) => ({ title: asset.assetName, meta: `${assetKey(asset)} · ${fmtTime(asset.lastInspectedAt)} · 需复核`, status: "逾期" }));
  if (!pending.length && !records.some((record) => String(record.createdAt || "").slice(0, 10) === today)) {
    pending.push({ title: "今日巡检", meta: "暂无今日巡检记录，建议派发任务", status: "待执行" });
  }
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
        <div class="task-card">
          <b>${escapeHTML(item.title)}</b>
          <span>${escapeHTML(item.meta)}</span>
          <em class="status ${level === "danger" ? "danger" : level === "warning" ? "warning" : "normal"}">${escapeHTML(item.status)}</em>
        </div>
      `).join("") || `<div class="empty-state small">暂无${escapeHTML(title)}任务</div>`}
    </article>
  `;
}

function renderRecordPage() {
  const all = filteredRecords();
  const pageSize = 20;
  const totalPages = Math.max(1, Math.ceil(all.length / pageSize));
  if (state.recordPage > totalPages - 1) state.recordPage = totalPages - 1;
  if (state.recordPage < 0) state.recordPage = 0;
  const start = state.recordPage * pageSize;
  const records = all.slice(start, start + pageSize);
  const selected = selectedRecord() || all[0];
  if (selected) state.selectedRecordId = selected.id;
  $("#pageMain").innerHTML = `
    ${pageHero("巡检管理", "巡检记录", "照片、识别字段、AI 总结、人工确认和提交结果全部可追溯。", `<button id="exportRecordsBtn">导出记录</button>`)}
    <section class="panel">
      <div class="panel-head"><h2>巡检记录列表</h2><div class="filters"><span>全部项目</span><span>全部模板</span><span>全部状态</span><span class="search">搜索巡检人 / 点位</span></div></div>
      <div class="record-list">
        ${records.map((record) => `
          <article data-record-select="${escapeHTML(record.id)}" class="${record.id === state.selectedRecordId ? "selected-card" : ""}">
            <time>${fmtTime(record.createdAt)}</time>
            <div><b>${escapeHTML(record.pointName || record.templateName || "巡检记录")}</b><span>${escapeHTML(primaryReading(record) || record.aiSummary || record.report || "-")}</span></div>
            <em class="status ${statusClass(recordStatus(record))}">${escapeHTML(recordStatus(record))}</em>
          </article>
        `).join("") || `<div class="empty-state">暂无巡检记录</div>`}
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
  $("#exportRecordsBtn").addEventListener("click", exportRecordsCsv);
  $$("[data-record-pager]").forEach((btn) => {
    btn.addEventListener("click", () => {
      if (btn.disabled) return;
      state.recordPage += btn.dataset.recordPager === "prev" ? -1 : 1;
      renderRecordPage();
    });
  });
}

function renderLedgerPage() {
  const assets = filteredAssets();
  const counts = statusCounts(assets);
  $("#pageMain").innerHTML = `
    ${pageHero("资产管理", "资产台账", "按设备、点位、状态沉淀巡检资产，形成可维护、可追溯的资产底账。", `<button id="exportAssetsBtn">导出台账</button>`)}
    ${metrics([
      { label: "资产总数", value: counts.total, sub: "台/套" },
      { label: "正常资产", value: counts.normal, sub: `${ratio(counts.normal, counts.total)}%`, good: true },
      { label: "异常资产", value: counts.danger, sub: "需处理", bad: counts.danger > 0 },
      { label: "待复核", value: counts.warning + counts.repair, sub: "人工确认" },
    ])}
    ${assetTablePanel(assets, "资产列表", "点击任意资产查看详情并维护台账。", true)}
  `;
  $("#pageAside").innerHTML = renderAssetSide(selectedAsset() || assets[0]);
  $("#exportAssetsBtn").addEventListener("click", exportAssetsCsv);
  bindAssetFilters();
}

function assetTablePanel(assets, title, desc, withFilters) {
  return `
    <section class="panel">
      <div class="panel-head">
        <div><h2>${escapeHTML(title)}</h2><p>${escapeHTML(desc)}</p></div>
        ${withFilters ? assetFiltersHTML() : `<button class="link-btn" data-page-link="ledger">全部台账</button>`}
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
  const statuses = [...new Set(state.assets.map((asset) => asset.lastStatus).filter(Boolean))];
  return `
    <div class="filters">
      <select id="assetTypeFilter"><option value="">全部设备类型</option>${types.map((type) => `<option value="${escapeHTML(type)}" ${state.filters.assetType === type ? "selected" : ""}>${escapeHTML(type)}</option>`).join("")}</select>
      <select id="statusFilter"><option value="">全部状态</option>${statuses.map((status) => `<option value="${escapeHTML(status)}" ${state.filters.status === status ? "selected" : ""}>${escapeHTML(status)}</option>`).join("")}</select>
      <input id="keywordInput" type="search" placeholder="请输入设备编号 / 名称" value="${escapeHTML(state.filters.keyword)}" />
    </div>
  `;
}

function bindAssetFilters() {
  $("#assetTypeFilter")?.addEventListener("change", (event) => {
    state.filters.assetType = event.target.value;
    renderLedgerPage();
  });
  $("#statusFilter")?.addEventListener("change", (event) => {
    state.filters.status = event.target.value;
    renderLedgerPage();
  });
  $("#keywordInput")?.addEventListener("input", (event) => {
    state.filters.keyword = event.target.value;
    renderLedgerPage();
  });
}

function renderDevicePage() {
  const assets = filteredAssets();
  $("#pageMain").innerHTML = `
    ${pageHero("资产管理", "设备管理", "维护设备档案、绑定点位、巡检模板、二维码和启停状态。", `<button data-drawer="device">新增设备</button>`)}
    <section class="device-grid">
      ${assets.map((asset) => `
        <article data-asset-select="${escapeHTML(asset.id)}">
          <div><b>${escapeHTML(asset.assetName || "未命名设备")}</b><em class="status ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</em></div>
          <p>设备编号：${escapeHTML(assetKey(asset))}</p>
          <p>设备类型：${escapeHTML(asset.assetType || "-")}</p>
          <span>绑定点位：${escapeHTML(locationText(asset))}</span>
        </article>
      `).join("") || `<div class="empty-state">暂无设备档案</div>`}
    </section>
  `;
  $("#pageAside").innerHTML = sideInfo("设备配置", [
    ["字段模板绑定", "设备可绑定多个巡检字段，AI 识别结果按字段写入巡检记录，再沉淀到资产台账。"],
    ["维护口径", "设备档案负责长期属性，巡检记录负责单次事实，资产台账负责当前状态。"],
  ]) + steps(["设备建档", "点位绑定", "模板绑定", "启用巡检"], 2);
}

function renderExceptionPage() {
  const assets = filteredAssets().filter((asset) => ["warning", "danger", "repair"].includes(asset.statusLevel || normalizeStatus(asset.lastStatus)));
  const pending = filteredRequests().filter((request) => request.status === "pending");
  $("#pageMain").innerHTML = `
    ${pageHero("异常管理", "异常复核", "对 AI 识别异常、字段缺失、低置信度读数进行人工复核，并形成工单闭环。", `<button data-drawer="exception">批量处理</button>`)}
    <section class="risk-grid">
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
      ${!assets.length && !pending.length ? `<div class="empty-state">暂无异常复核任务</div>` : ""}
    </section>
  `;
  $("#pageAside").innerHTML = renderExceptionSide(assets[0], pending[0]);
}

function renderApprovalPage() {
  const requests = filteredRequests();
  $("#pageMain").innerHTML = `
    ${pageHero("异常管理", "修改审批", "人工修正必须留痕，审批通过后更新最终字段，但保留 AI 原始识别。", `<button data-drawer="approval">审批设置</button>`)}
    <section class="approval-list">
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
    </section>
  `;
  $("#pageAside").innerHTML = `<div class="detail-head"><h2>审批流</h2></div><div class="side-stack">${steps(["巡检员提交", "字段对比", "主管审批", "更新台账"], 2)}<div class="info-card"><b>规则</b><span>人工修改不直接覆盖 AI 识别，审批后写入最终字段，并保留变更前后值。</span></div></div>`;
}

function renderDataPage() {
  const assets = filteredAssets();
  const records = filteredRecords();
  const counts = statusCounts(assets);
  const accuracy = aiAccuracy(records);
  const accuracyDisplay = accuracy.sample < 5
    ? `<div class="ring muted" style="--value:0">—</div><p class="ring-note">样本不足（${accuracy.sample}/5）</p>`
    : `<div class="ring" style="--value:${accuracy.value}">${accuracy.value}%</div><p class="ring-note">基于 ${accuracy.sample} 个已确认字段</p>`;
  $("#pageMain").innerHTML = `
    ${pageHero("数据中心", "数据看板", "展示资产状态、巡检效率、AI 识别准确率、异常闭环率和项目趋势。", `<button id="refreshDataBtn">刷新数据</button>`)}
    <section class="chart-grid">
      ${trendPanel(records)}
      <article class="ring-card"><h2>AI 识别准确率</h2>${accuracyDisplay}</article>
      <article class="ring-card"><h2>异常闭环率</h2><div class="ring green" style="--value:${closedRate()}">${closedRate()}%</div></article>
      ${metrics([
        { label: "正常资产", value: counts.normal, sub: "资产健康" },
        { label: "待复核", value: counts.warning, sub: "人工确认" },
        { label: "异常资产", value: counts.danger, sub: "需处理" },
        { label: "巡检记录", value: records.length, sub: "历史沉淀" },
      ])}
    </section>
  `;
  $("#pageAside").innerHTML = sideInfo("指标解释", [
    ["AI 识别准确率", "按人工最终确认字段与 AI 原始识别字段比对计算。"],
    ["异常闭环率", "异常从识别、复核、工单、关闭的完整比例。"],
  ]);
  $("#refreshDataBtn").addEventListener("click", () => loadData());
}

function renderReportPage() {
  const records = filteredRecords();
  const assets = filteredAssets();
  const abnormal = assets.filter((asset) => normalizeStatus(asset.lastStatus) !== "normal").length;
  $("#pageMain").innerHTML = `
    ${pageHero("数据中心", "统计报表", "按日、周、月输出巡检日报、异常分析、资产健康和人员效率报表。", `<button id="buildReportBtn">生成报表</button>`)}
    <section class="report-grid">
      ${reportCard("巡检日报", `当前项目共 ${records.length} 条巡检记录，已提交 ${records.filter((record) => record.submitted).length} 条。`)}
      ${reportCard("异常周报", `当前异常/待复核资产 ${abnormal} 项，待审批 ${filteredRequests().filter((item) => item.status === "pending").length} 条。`)}
      ${reportCard("资产健康报表", `资产总数 ${assets.length}，正常 ${statusCounts(assets).normal}，异常 ${statusCounts(assets).danger}。`)}
      ${reportCard("人员效率报表", `巡检人员 ${new Set(records.map((record) => record.inspector).filter(Boolean)).size} 人，记录持续沉淀。`)}
    </section>
  `;
  $("#pageAside").innerHTML = `<div class="detail-head"><h2>报表预览</h2></div><div class="side-stack"><div class="info-card"><b>${escapeHTML(state.selectedProject || "全部项目")}巡检日报</b><span>${escapeHTML(buildReportPreview(records, assets))}</span></div><div class="report-preview"></div></div>`;
  $("#buildReportBtn").addEventListener("click", () => toast("已根据当前数据生成报表预览"));
}

function roleText(user = {}) {
  return user.roleName || ({
    admin: "系统管理员",
    manager: "管理人员",
    supervisor: "复核审批人员",
    inspector: "一线巡检员",
  }[user.roleCode] || user.roleCode || "-");
}

function usersTableHTML() {
  const users = state.users || [];
  return `
    <section class="panel">
      <div class="panel-head"><h2>用户与权限</h2><span>当前账号 ${users.length || 0} 个，后续可接企业微信组织架构</span></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>账号</th><th>姓名</th><th>角色</th><th>部门</th><th>状态</th><th>最近登录</th></tr></thead>
          <tbody>
            ${users.map((user) => `
              <tr>
                <td>${escapeHTML(user.username)}</td>
                <td>${escapeHTML(user.displayName)}</td>
                <td>${escapeHTML(roleText(user))}</td>
                <td>${escapeHTML(user.departmentName || "-")}</td>
                <td><span class="status ${user.status === "active" ? "normal" : "warning"}">${escapeHTML(user.status || "-")}</span></td>
                <td>${fmtTime(user.lastLoginAt)}</td>
              </tr>
            `).join("") || `<tr><td colspan="6">暂无用户数据</td></tr>`}
          </tbody>
        </table>
      </div>
    </section>
  `;
}

function operationLogsHTML() {
  const logs = state.operationLogs || [];
  return `
    <section class="panel">
      <div class="panel-head"><h2>操作日志</h2><span>记录登录、台账修改、审批等关键动作</span></div>
      <div class="table-wrap">
        <table>
          <thead><tr><th>时间</th><th>人员</th><th>动作</th><th>对象</th><th>编号</th></tr></thead>
          <tbody>
            ${logs.slice(0, 12).map((item) => `
              <tr>
                <td>${fmtTime(item.createdAt)}</td>
                <td>${escapeHTML(item.actorName || "-")}</td>
                <td>${escapeHTML(item.action || "-")}</td>
                <td>${escapeHTML(item.targetType || "-")}</td>
                <td>${escapeHTML(shortId(item.targetId || "-"))}</td>
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
      <button class="danger soft" id="logoutBtn" type="button">退出登录</button>
    </div>
  `;
}

function renderSystemPage() {
  const store = state.health?.storeKind || state.health?.store || "unknown";
  $("#pageMain").innerHTML = `
    ${pageHero("数据中心", "系统管理", "展示 API、AI 服务、MySQL、文件存储、企业微信入口等运行状态与部署配置。", `<button id="saveSystemBtn">保存配置</button>`)}
    <section class="system-grid">
      ${systemCard("Go API 服务", state.apiBase, state.health ? "运行中" : "待检查", state.health ? "normal" : "warning")}
      ${systemCard("AI 视觉服务", "http://127.0.0.1:19100", "独立服务", "normal")}
      ${systemCard("数据库", store, store === "mysql" ? "MySQL 已启用" : "当前存储", store === "mysql" ? "normal" : "warning")}
      ${systemCard("文件存储", "storage/uploads", "本地存储", "warning")}
    </section>
    <section class="panel">
      <div class="panel-head"><h2>部署配置</h2><span>云服务器迁移前检查项</span></div>
      <div class="table-wrap">
        <table><tbody>
          <tr><td>公网域名</td><td>api.your-domain.com</td><td><span class="status warning">待配置证书</span></td></tr>
          <tr><td>企业微信回调</td><td>/wework/callback</td><td><span class="status normal">预留</span></td></tr>
          <tr><td>对象存储</td><td>MinIO / OSS</td><td><span class="status warning">可替换</span></td></tr>
        </tbody></table>
      </div>
    </section>
    ${usersTableHTML()}
    ${operationLogsHTML()}
  `;
  $("#pageAside").innerHTML = `<div class="detail-head"><h2>运行摘要</h2></div><div class="side-stack">${currentUserSummaryHTML()}<div class="info-card"><b>当前架构</b><span>移动端 18080，后台管理端 18081，AI 服务 19100，数据通过 Go API 落库。</span></div>${systemConfigForm()}</div>`;
  $("#saveSystemBtn").addEventListener("click", saveSystemConfigFromSide);
  $("#logoutBtn")?.addEventListener("click", logout);
  $("#systemConfigForm")?.addEventListener("submit", (event) => {
    event.preventDefault();
    saveSystemConfigFromSide();
  });
}

const pageRenderers = {
  dashboard: renderDashboardPage,
  plan: renderPlanPage,
  task: renderTaskPage,
  record: renderRecordPage,
  ledger: renderLedgerPage,
  device: renderDevicePage,
  exception: renderExceptionPage,
  approval: renderApprovalPage,
  data: renderDataPage,
  report: renderReportPage,
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
  const history = state.records.filter((record) => record.id === asset.lastRecordId || record.pointId === asset.pointId || record.templateId === asset.templateId);
  const photos = collectAssetPhotos(asset, history);
  return `
    <div class="detail-head"><h2>资产详情</h2></div>
    <div class="asset-card">
      <div class="asset-photo">${photos[0] ? `<img src="${escapeHTML(photos[0])}" alt="">` : ""}</div>
      <div class="asset-title"><h3>${escapeHTML(asset.assetName || "未命名资产")}</h3><span class="pill ${statusClass(asset.lastStatus)}">${escapeHTML(asset.lastStatus || "未巡检")}</span></div>
      <div class="kv-list">
        <div><span>设备编号</span><b>${escapeHTML(assetKey(asset))}</b></div>
        <div><span>设备类型</span><b>${escapeHTML(asset.assetType || "-")}</b></div>
        <div><span>安装位置</span><b>${escapeHTML(locationText(asset))}</b></div>
        <div><span>最近巡检</span><b>${fmtTime(asset.lastInspectedAt)}</b></div>
      </div>
      <div class="info-card"><b>台账摘要</b><span>${escapeHTML(asset.lastSummary || "暂无管理摘要。")}</span></div>
      <div class="detail-tabs"><button class="active">巡检记录</button><button>字段历史</button><button>异常记录</button><button>关联文件</button></div>
      <table class="history-table">
        <thead><tr><th>巡检时间</th><th>巡检人</th><th>识别结果</th><th>状态</th></tr></thead>
        <tbody>${history.slice(0, 5).map((record) => `
          <tr><td>${fmtTime(record.createdAt)}</td><td>${escapeHTML(record.inspector || "-")}</td><td>${escapeHTML(primaryReading(record) || statusLabel(record.recognitionStatus))}</td><td><span class="status ${statusClass(recordStatus(record))}">${escapeHTML(recordStatus(record))}</span></td></tr>
        `).join("") || emptyRow(4, "暂无历史巡检")}</tbody>
      </table>
      <div class="photo-strip"><h3>巡检照片 <small>共 ${photos.length} 张</small></h3><div class="photos">${photos.slice(0, 4).map((url) => `<img src="${escapeHTML(url)}" alt="">`).join("") || `<div class="empty-photo"></div>`}</div></div>
      <form class="edit-form" id="assetEditForm">
        <label>资产名称<input name="assetName" value="${escapeHTML(asset.assetName || "")}"></label>
        <label>资产状态<select name="lastStatus">${["正常", "异常", "待复核", "待维修"].map((status) => `<option value="${status}" ${asset.lastStatus === status ? "selected" : ""}>${status}</option>`).join("")}</select></label>
        <label>管理摘要<textarea name="lastSummary">${escapeHTML(asset.lastSummary || "")}</textarea></label>
        <button class="primary" type="submit">保存台账修改</button>
      </form>
    </div>
  `;
}

function renderRecordSide(record) {
  if (!record) return `<div class="detail-head"><h2>记录详情</h2></div><div class="empty-state">暂无巡检记录</div>`;
  const photos = collectPhotosFromRecord(record);
  return `
    <div class="detail-head"><h2>记录详情</h2></div>
    <div class="side-stack">
      <div class="info-card"><b>AI 总结</b><span>${escapeHTML(record.aiSummary || record.report || "暂无总结")}</span></div>
      <div class="photo-row">${photos.slice(0, 2).map((url) => `<img src="${escapeHTML(url)}" alt="">`).join("")}</div>
      <table class="history-table"><thead><tr><th>字段</th><th>值</th><th>置信度</th></tr></thead><tbody>${(record.fields || []).slice(0, 8).map((field) => `<tr><td>${escapeHTML(field.label || field.code)}</td><td>${escapeHTML(field.value || field.aiValue || "-")}</td><td>${Math.round((field.confidence || 0) * 100)}%</td></tr>`).join("") || emptyRow(3, "暂无字段")}</tbody></table>
    </div>
  `;
}

function renderExceptionSide(asset, request) {
  if (asset) return renderAssetSide(asset);
  if (request) return sideInfo("复核建议", [["申请原因", request.reason || "-"], ["处理方式", "通过审批后写入最终字段，保留 AI 原始识别。"]]);
  return sideInfo("复核建议", [["当前状态", "暂无待复核异常"], ["处理规则", "异常应进入台账，不覆盖原始 AI 识别结果。"]]);
}

function trendPanel(records) {
  return `
    <section class="panel chart-panel">
      <div class="panel-head">
        <div><h2>每日巡检记录</h2></div>
        <div class="legend"><span><i class="ok"></i>当日·正常</span><span><i class="warn"></i>当日·待复核</span><span><i class="bad"></i>当日·异常</span></div>
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
  const max = Math.max(1, ...all);
  const width = 460;
  const height = 150;
  const pad = { left: 32, right: 18, top: 16, bottom: 26 };
  const point = (value, index) => {
    const x = pad.left + ((width - pad.left - pad.right) / Math.max(days.length - 1, 1)) * index;
    const y = pad.top + (height - pad.top - pad.bottom) - (value / max) * (height - pad.top - pad.bottom);
    return [x, y];
  };
  const line = (values) => values.map((value, index) => point(value, index).join(",")).join(" ");
  const dots = (values, color) => values.map((value, index) => {
    const [x, y] = point(value, index);
    return `<circle cx="${x}" cy="${y}" r="3" fill="${color}"></circle>`;
  }).join("");
  return `
    <svg viewBox="0 0 ${width} ${height}">
      <g class="axis">${[0, 0.5, 1].map((scale) => {
        const y = pad.top + (height - pad.top - pad.bottom) - (height - pad.top - pad.bottom) * scale;
        return `<line x1="${pad.left}" y1="${y}" x2="${width - pad.right}" y2="${y}"></line><text x="4" y="${y + 4}">${Math.round(max * scale)}</text>`;
      }).join("")}${days.map((day, index) => `<text x="${point(0, index)[0]}" y="${height - 5}" text-anchor="middle">${day.slice(5).replace("-", "/")}</text>`).join("")}</g>
      <polyline points="${line(series.normal)}" fill="none" stroke="#12a968" stroke-width="3"></polyline>
      <polyline points="${line(series.warning)}" fill="none" stroke="#f59e0b" stroke-width="3"></polyline>
      <polyline points="${line(series.danger)}" fill="none" stroke="#ef4b3f" stroke-width="3"></polyline>
      ${dots(series.normal, "#12a968")}${dots(series.warning, "#f59e0b")}${dots(series.danger, "#ef4b3f")}
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
  return `<article><b>${escapeHTML(title)}</b><em class="status ${level}">${escapeHTML(status)}</em><p>${escapeHTML(desc)}</p></article>`;
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

function openGenericDrawer(type) {
  const copy = {
    plan: ["新建计划", "生产版本应从点位、模板、责任人和频次生成计划；当前页面已按现有点位与模板渲染。"],
    task: ["派发任务", "任务应由计划自动生成，也可以由主管临时派发到巡检员。"],
    device: ["新增设备", "设备档案应包含编号、名称、类型、点位、模板、状态和二维码绑定。"],
    exception: ["批量处理", "批量处理需保留复核结果和处理人，不直接覆盖 AI 原始识别。"],
    approval: ["审批设置", "审批流建议保留申请人、审批人、变更前值、变更后值、处理意见。"],
  }[type] || ["说明", "该能力属于后续生产增强项。"];
  openDrawer(copy[0], `<p>${escapeHTML(copy[1])}</p>`);
}

function exportCsv(name, header, rows) {
  const csv = [header, ...rows].map((row) => row.map((value) => `"${String(value ?? "").replaceAll('"', '""')}"`).join(",")).join("\r\n");
  const blob = new Blob(["\ufeff" + csv], { type: "text/csv;charset=utf-8" });
  const url = URL.createObjectURL(blob);
  const link = document.createElement("a");
  link.href = url;
  link.download = `${name}-${new Date().toISOString().slice(0, 10)}.csv`;
  document.body.appendChild(link);
  link.click();
  link.remove();
  URL.revokeObjectURL(url);
}

function exportAssetsCsv() {
  const rows = filteredAssets();
  exportCsv("inspectai-assets", ["设备编号", "设备名称", "设备类型", "项目", "安装位置", "状态", "最近巡检时间", "管理摘要"], rows.map((asset) => [
    assetKey(asset),
    asset.assetName,
    asset.assetType,
    asset.project || asset.projectCode,
    locationText(asset),
    asset.lastStatus,
    fmtTime(asset.lastInspectedAt),
    asset.lastSummary,
  ]));
  toast(`已导出 ${rows.length} 条资产`);
}

function exportRecordsCsv() {
  const rows = filteredRecords();
  exportCsv("inspectai-records", ["巡检时间", "项目", "点位", "模板", "巡检人", "状态", "主要识别", "AI总结"], rows.map((record) => [
    fmtTime(record.createdAt),
    record.project,
    record.pointName,
    record.templateName,
    record.inspector,
    recordStatus(record),
    primaryReading(record),
    record.aiSummary || record.report,
  ]));
  toast(`已导出 ${rows.length} 条记录`);
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
    const pageLink = event.target.closest("[data-page-link]")?.dataset.pageLink;
    const assetId = event.target.closest("[data-asset-select]")?.dataset.assetSelect;
    const recordId = event.target.closest("[data-record-select]")?.dataset.recordSelect;
    const requestId = event.target.closest("[data-request-open]")?.dataset.requestOpen;
    const drawerType = event.target.closest("[data-drawer]")?.dataset.drawer;
    const normalId = event.target.closest("[data-asset-normal]")?.dataset.assetNormal;
    const reviewBtn = event.target.closest("[data-request-review]");
    if (pageLink) setPage(pageLink);
    if (assetId) {
      state.selectedAssetId = assetId;
      if (state.page !== "ledger" && state.page !== "device" && state.page !== "exception") setPage("ledger");
      else render();
    }
    if (recordId) {
      state.selectedRecordId = recordId;
      if (state.page !== "record") setPage("record");
      else render();
    }
    if (requestId) openRequestDrawer(requestId);
    if (drawerType) openGenericDrawer(drawerType);
    if (normalId) await setAssetNormal(normalId);
    if (reviewBtn) await reviewRequest(reviewBtn.dataset.requestReview, reviewBtn.dataset.action || "approve");
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
  setPage(state.page, false);
  try {
    await loadMe();
    await loadData(false);
  } catch (error) {
    if (state.token) {
      state.token = "";
      localStorage.removeItem(API_TOKEN_KEY);
    }
    renderLogin(error.message);
  }
}

init();
