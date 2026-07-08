/* 智巡 — 前端逻辑

   流程（拍照优先）：
     camera → loading → classify → form → preview → ledger
     失败弹窗：retake (3 次后转 manual)
*/

const AUTH_TOKEN_KEY = "inspectai_auth_token";

function authToken() {
  try { return localStorage.getItem(AUTH_TOKEN_KEY) || ""; } catch { return ""; }
}

function saveAuthToken(token) {
  try {
    if (token) localStorage.setItem(AUTH_TOKEN_KEY, token);
    else localStorage.removeItem(AUTH_TOKEN_KEY);
  } catch {}
}

const API = {
  async json(path, opts = {}, canRetryAuth = true) {
    // 注入访问令牌；角色只在本地无令牌演示模式下作为降级提示使用。
    // HTTP header 不允许非 ASCII，所以 name 经 URL encode 传输
    const headers = new Headers(opts.headers || {});
    headers.set("X-User-Role", state.userRole || "inspector");
    headers.set("X-User-Name", encodeURIComponent(state.userName || "匿名"));
    const token = authToken();
    if (token) headers.set("X-InspectAI-Token", token);
    const res = await fetch(path, { ...opts, headers, credentials: "include" });
    let data = {};
    try { data = await res.json(); } catch {}
    if (res.status === 401 && canRetryAuth) {
      const input = prompt("请输入系统访问令牌");
      if (input && input.trim()) {
        saveAuthToken(input.trim());
        return API.json(path, opts, false);
      }
    }
    if (!res.ok) {
      throw new Error(data.message || data.error || `请求失败 (${res.status})`);
    }
    return data;
  },
  async classify(files) {
    const fd = new FormData();
    for (const f of files) fd.append("files", f);
    return API.json("/api/scene/classify", { method: "POST", body: fd });
  },
  templates() { return API.json("/api/report/templates"); },
  points() { return API.json("/api/inspection/points"); },
  assets() { return API.json("/api/assets"); },
  getAsset(id) { return API.json(`/api/assets/${encodeURIComponent(id)}`); },
  patchAsset(id, body) {
    return API.json(`/api/assets/${encodeURIComponent(id)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  createRecord(body) {
    return API.json("/api/inspection/records", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  getRecord(id) { return API.json(`/api/inspection/records/${id}`); },
  uploadImages(id, files) {
    const fd = new FormData();
    for (const f of files) fd.append("files", f);
    return API.json(`/api/inspection/records/${id}/images`, { method: "POST", body: fd });
  },
  startAnalysis(id) {
    return API.json(`/api/inspection/records/${id}/ai-tasks`, { method: "POST" });
  },
  latestTask(id) { return API.json(`/api/inspection/records/${id}/ai/latest`); },
  // §4 防惰性留痕：除 value/version 外可带 action(confirm/correct/uncertain)
  // + 该字段停留时长 durationMs + 是否看过原图 viewedPhoto。后端按这些写 field_confirm_logs。
  patchField(id, code, value, version, opts = {}) {
    const body = { value, version };
    if (opts.action) body.action = opts.action;
    if (typeof opts.durationMs === "number" && opts.durationMs > 0) body.durationMs = opts.durationMs;
    if (opts.viewedPhoto) body.viewedPhoto = true;
    return API.json(`/api/inspection/records/${id}/fields/${encodeURIComponent(code)}`, {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  // ===== 修改申请（审批流） =====
  createChangeRequest(body) {
    return API.json("/api/change-requests", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    });
  },
  listChangeRequests(params = {}) {
    const q = new URLSearchParams(params).toString();
    return API.json("/api/change-requests" + (q ? "?" + q : ""));
  },
  approveChangeRequest(id, reviewNote) {
    return API.json(`/api/change-requests/${id}/approve`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ reviewNote: reviewNote || "" }),
    });
  },
  rejectChangeRequest(id, reviewNote) {
    return API.json(`/api/change-requests/${id}/reject`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ reviewNote }),
    });
  },
  withdrawChangeRequest(id) {
    return API.json(`/api/change-requests/${id}/withdraw`, { method: "POST" });
  },
  uploadDraftPhotos(files) {
    const fd = new FormData();
    for (const f of files) fd.append("files", f);
    return API.json("/api/change-requests/draft-photos", { method: "POST", body: fd });
  },
  enableManual(id) {
    return API.json(`/api/inspection/records/${id}/manual`, { method: "POST" });
  },
  submit(id) {
    return API.json(`/api/inspection/records/${id}/submit`, {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey() },
    });
  },
  // ===== 工程巡检任务（闭环入口） =====
  engineeringTasks() { return API.json("/api/engineering/tasks"); },
  updateEngineeringTaskStatus(id, body) {
    return API.json(`/api/engineering/tasks/${encodeURIComponent(id)}/status`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body || {}),
    });
  },
};

const ACTIVE_TASK_KEY = "activeEngineeringTask";
function loadActiveTask() {
  try { return JSON.parse(localStorage.getItem(ACTIVE_TASK_KEY) || "null"); }
  catch { return null; }
}

const state = {
  templates: [],
  points: [],
  assets: [],
  assetSummary: null,
  engineeringTasks: [],
  activeEngineeringTask: loadActiveTask(),
  activeEngineeringTaskId: (loadActiveTask() || {}).id || null,
  scene: "camera",
  classifyResult: null,    // { templateId, confidence, ..., tmpDir }
  pendingImageIds: [],
  record: null,
  pollTimer: null,
  loadingTimer: null,      // 等待剧本轮播定时器
  loadingStepsHTML: "",    // 默认三步列表的原始 HTML(classify 用)
  forceManual: false,
  retakePending: false,
  retakeTarget: null,      // 异常资产「重新拍照复检」上下文:{ templateId, pointId, assetNo, assetName }
  inspector: localStorage.getItem("inspector") || "巡检员",
  userRole: localStorage.getItem("userRole") || "inspector",   // inspector / supervisor
  userName: localStorage.getItem("userName") || (localStorage.getItem("inspector") || "巡检员"),
  // 资产详情与审批数据
  currentAsset: null,
  currentAssetHistory: [],
  currentAssetRequests: [],
  approvalQueue: [],
  pendingApprovalCount: 0,
};

// URL ?role=supervisor 切换；移动端窄屏强制 inspector（codex: 移动端不做主管功能）
(function applyRoleFromURL() {
  const params = new URLSearchParams(location.search);
  const r = params.get("role");
  if (r === "supervisor" || r === "inspector") {
    state.userRole = r;
    localStorage.setItem("userRole", r);
    // 清掉 URL 参数避免刷新带着
    params.delete("role");
    const newQs = params.toString();
    history.replaceState(null, "", location.pathname + (newQs ? "?" + newQs : ""));
  }
  // 窄屏（手机端）强制 inspector，杜绝误进入主管入口
  if (window.matchMedia("(max-width: 600px)").matches && state.userRole !== "inspector") {
    state.userRole = "inspector";
    localStorage.setItem("userRole", "inspector");
  }
})();
function canReview() { return ["admin", "manager", "supervisor"].includes(state.userRole); }
function isSupervisor() { return canReview(); }

// ===== 工具 =====

const $ = (sel) => document.querySelector(sel);
const $$ = (sel) => document.querySelectorAll(sel);

function escapeHTML(s = "") {
  return String(s)
    .replaceAll("&", "&amp;").replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;").replaceAll('"', "&quot;").replaceAll("'", "&#39;");
}

function idempotencyKey() {
  if (crypto.randomUUID) return crypto.randomUUID();
  return `key_${Date.now()}_${Math.random().toString(36).slice(2)}`;
}

function toast(msg) {
  const el = $("#toast");
  el.textContent = msg;
  el.classList.add("show");
  clearTimeout(el._timer);
  el._timer = setTimeout(() => el.classList.remove("show"), 2400);
}

// ===== 等待剧本:analyze 实测约 30 秒(生成耗时压不动),
// 用阶段文案轮播把冷场变成"AI 工作直播";不给死数字承诺 =====
const LOADING_SCRIPTS = {
  classify: [
    { t: 0, msg: "AI 正在识别场景…" },
    { t: 4, msg: "正在匹配日报模板…" },
  ],
  analyze: [
    { t: 0,  msg: "正在逐张查看照片…",       step: 0 },
    { t: 7,  msg: "正在核对模板判定规则…",   step: 1 },
    { t: 16, msg: "正在生成巡检结论…",       step: 2 },
    { t: 30, msg: "即将完成，正在整理字段…", step: 2 },
  ],
};
const ANALYZE_STEPS = ["逐张查看照片", "核对模板判定规则", "生成巡检结论"];

function startLoadingScript(kind) {
  stopLoadingScript();
  const script = LOADING_SCRIPTS[kind];
  const ol = $("#loadingSteps");
  if (!script || !ol) return;
  if (!state.loadingStepsHTML) state.loadingStepsHTML = ol.innerHTML;
  if (kind === "analyze") {
    ol.classList.add("scripted");
    ol.innerHTML = ANALYZE_STEPS.map((t, i) =>
      `<li class="step" data-i="${i}"><span class="step-dot"></span><span class="step-text">${t}</span></li>`
    ).join("");
  } else {
    ol.classList.remove("scripted");
    ol.innerHTML = state.loadingStepsHTML;
  }
  const started = Date.now();
  const tick = () => {
    const sec = (Date.now() - started) / 1000;
    let cur = script[0];
    for (const it of script) if (sec >= it.t) cur = it;
    const msgEl = $("#loadingMsg");
    if (msgEl && msgEl.textContent !== cur.msg) {
      msgEl.classList.remove("msg-in");
      void msgEl.offsetWidth; // 重新触发进场动画
      msgEl.textContent = cur.msg;
      msgEl.classList.add("msg-in");
    }
    if (kind === "analyze" && typeof cur.step === "number") {
      ol.querySelectorAll(".step").forEach((li) => {
        const i = Number(li.dataset.i);
        li.classList.toggle("on", i === cur.step);
        li.classList.toggle("done", i < cur.step);
      });
    }
  };
  tick();
  state.loadingTimer = setInterval(tick, 500);
}

function stopLoadingScript() {
  clearInterval(state.loadingTimer);
  state.loadingTimer = null;
}

const PROGRESS = { login: 0, camera: 0, tasks: 0, loading: 15, classify: 30, form: 60, preview: 85, ledger: 100, asset: 100, approvals: 100 };
const TITLES = {
  login: "登录",
  camera: "智巡",
  tasks: "我的任务",
  loading: "AI 识别中",
  classify: "确认场景",
  form: "确认日报字段",
  preview: "提交日报",
  ledger: "设备健康",
  asset: "设备档案",
  approvals: "待审批",
};

function setScene(name) {
  stopLoadingScript();
  if (name === "camera") hideRetakeModal();
  state.scene = name;
  const appRoot = $("#app");
  if (appRoot) appRoot.dataset.scene = name;
  $$(".scene").forEach(el => el.classList.remove("active"));
  document.getElementById("scene" + cap(name))?.classList.add("active");
  $("#pageTitle").textContent = TITLES[name] || "智巡";

  const showProgress = !["camera", "tasks", "ledger", "asset", "approvals"].includes(name);
  $("#progress").hidden = !showProgress;
  $("#progressBar").style.width = (PROGRESS[name] || 0) + "%";

  // back button visibility
  $("#backBtn").hidden = (name === "camera" || name === "login");

  // top action button
  const userWindowBtn = $("#userWindowBtn");
  if (userWindowBtn) {
    userWindowBtn.hidden = name !== "camera";
    userWindowBtn.textContent = state.userRole === "inspector"
      ? (state.userName || "\u5de1\u68c0\u5458")
      : "\u5de1\u68c0\u5458";
  }
  if (name === "form" || name === "preview") {
    $("#topAction").hidden = false;
    $("#topAction").textContent = "设备健康";
    $("#topAction").onclick = () => goLedger();
  } else if (name === "camera") {
    $("#topAction").hidden = true;
    renderTaskBanner();
  } else {
    $("#topAction").hidden = true;
  }

  // 审批入口：仅主管 + 仅在台账页显示
  $("#topApprovals").hidden = !(canReview() && name === "ledger");

  // footer button per scene
  setFooter(name);
  window.scrollTo(0, 0);
  // 切场景后让 footer 重新评估是否显示（短页面 / 已到底则立即出现）
  requestAnimationFrame(updateFooterVisibility);
}

// ===== Footer 滚动显隐：滚到底部才滑出，主区域 padding 永久占位防遮挡 =====
function updateFooterVisibility() {
  const footer = document.getElementById("footer");
  if (!footer || footer.hidden) return;
  const el = document.scrollingElement || document.documentElement;
  // 距底 < 32px 视为到底（safari 弹性滚动留余量）
  const distToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
  if (distToBottom < 32) footer.classList.add("visible");
  else footer.classList.remove("visible");
}
let _footerScrollTicking = false;
window.addEventListener("scroll", () => {
  if (_footerScrollTicking) return;
  _footerScrollTicking = true;
  requestAnimationFrame(() => {
    _footerScrollTicking = false;
    updateFooterVisibility();
  });
}, { passive: true });
window.addEventListener("resize", updateFooterVisibility, { passive: true });

function cap(s) { return s.charAt(0).toUpperCase() + s.slice(1); }

function setFooter(name) {
  const footer = $("#footer");
  const primary = $("#primaryBtn");
  const secondary = $("#secondaryBtn");
  primary.onclick = null;
  secondary.onclick = null;
  secondary.hidden = true;

  switch (name) {
    case "camera":
    case "tasks":
    case "loading":
      footer.hidden = true;
      break;
    case "classify": {
      const r = state.classifyResult;
      if (state.retakeTarget?.templateId) {
        // 复检:模板已知(目标资产的模板),无视分类结果直接进入填报
        footer.hidden = false;
        primary.textContent = `继续复检填报`;
        primary.disabled = false;
        primary.onclick = () => startRecordWithTemplate(state.retakeTarget.templateId);
        secondary.hidden = true;
      } else if (r && !r.needsManualPick && r.templateId !== "unknown") {
        footer.hidden = false;
        primary.textContent = `开始填报【${r.templateName}】`;
        primary.disabled = false;
        primary.onclick = () => startRecordWithTemplate(r.templateId);
        secondary.textContent = "选其他模板";
        secondary.hidden = false;
        secondary.onclick = () => showTemplateList(true);
      } else {
        footer.hidden = true;
      }
      break;
    }
    case "form": {
      footer.hidden = false;
      primary.textContent = "保存并预览日报";
      primary.disabled = false;
      primary.onclick = () => saveAndPreview();
      break;
    }
    case "preview": {
      footer.hidden = false;
      primary.textContent = state.record?.submitted
        ? (inspectionStatus(state.record) === "异常" ? "查看设备健康 · 有需跟进" : "查看设备健康")
        : "提交日报";
      primary.disabled = false;
      primary.onclick = () => state.record?.submitted ? goLedger() : submitRecord();
      secondary.textContent = "返回修改";
      secondary.hidden = state.record?.submitted;
      secondary.onclick = () => setScene("form");
      break;
    }
    case "ledger": {
      footer.hidden = false;
      primary.textContent = "新建巡检";
      primary.disabled = false;
      primary.onclick = () => goCamera();
      break;
    }
    case "asset": {
      footer.hidden = false;
      primary.textContent = "申请修改";
      primary.disabled = false;
      primary.onclick = () => openChangeRequestSheet();
      secondary.textContent = "返回设备健康";
      secondary.hidden = false;
      secondary.onclick = () => goLedger();
      break;
    }
    case "approvals": {
      footer.hidden = true;
      break;
    }
  }
}

// ===== 拍照入口 =====

function bindFilePicker(inputId) {
  $(inputId).addEventListener("change", async (e) => {
    const files = Array.from(e.target.files || []);
    e.target.value = "";  // 允许同一文件重选
    if (!files.length) return;
    if (state.retakePending && state.record?.id) {
      await uploadRetakeImages(files);
      return;
    }
    await classifyAndProceed(files);
  });
}
bindFilePicker("#cameraInput");
bindFilePicker("#uploadInput");

async function uploadRetakeImages(files) {
  setScene("loading");
  $("#loadingMsg").textContent = "正在补充照片…";
  $("#loadingSub").textContent = `向本次巡检补充 ${files.length} 张图片`;
  try {
    const result = await API.uploadImages(state.record.id, files);
    state.record = result.record || await API.getRecord(state.record.id);
    state.retakePending = false;
    await beginAnalysis();
  } catch (err) {
    state.retakePending = true;
    toast(err.message);
    setScene("camera");
  }
}

// 兜底：鼠标/触屏点击走原生 label + file input；键盘操作时手动触发。
// 不在 click 里 preventDefault，否则企业微信 WebView 可能拦截系统相机唤起。
(function ensureShutterClick() {
  const wrap = document.querySelector(".shutter-wrap");
  const input = document.getElementById("cameraInput");
  if (!wrap || !input) return;
  wrap.addEventListener("click", (e) => {
    if (e.target === input) return;
    input.click();
  });
  wrap.addEventListener("keydown", (e) => {
    if (e.key !== "Enter" && e.key !== " ") return;
    e.preventDefault();
    input.click();
  });
})();

// ===== 登录 / 注销 =====

async function fetchMe() {
  try {
    const res = await fetch("/api/auth/me", {
      credentials: "include",
      headers: authToken() ? { "X-InspectAI-Token": authToken() } : {},
    });
    if (!res.ok) return null;
    const data = await res.json();
    return data.user || null;
  } catch { return null; }
}

function isRealUser(user) {
  // handleMe 在无登录时会回退一个 local_xxx 占位用户。视为未登录。
  return !!user && !!user.id && !String(user.id).startsWith("local_") && user.status !== "local";
}

function applyUser(user) {
  state.currentUser = user;
  state.userName = user?.displayName || user?.username || "巡检员";
  state.userRole = user?.roleCode || "inspector";
  try {
    localStorage.setItem("userName", state.userName);
    localStorage.setItem("userRole", state.userRole);
  } catch {}
  const btn = $("#userWindowBtn");
  if (btn) btn.textContent = state.userName;
}

async function doLogin(username, password) {
  const res = await fetch("/api/auth/login", {
    method: "POST",
    credentials: "include",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error(data.message || data.error || `登录失败 (${res.status})`);
  if (data.token) saveAuthToken(data.token);
  applyUser(data.user);
  // 登录后预加载模板和点位（init 里的逻辑因走登录页跳过了）
  try {
    const [t, p] = await Promise.all([API.templates(), API.points()]);
    state.templates = t.templates || [];
    state.points = p.points || [];
  } catch {}
  refreshApprovalCount();
  return data.user;
}

async function doLogout() {
  try {
    await fetch("/api/auth/logout", {
      method: "POST",
      credentials: "include",
      headers: authToken() ? { "X-InspectAI-Token": authToken() } : {},
    });
  } catch {}
  saveAuthToken("");
  state.currentUser = null;
  try { localStorage.removeItem("userName"); } catch {}
  setScene("login");
}

(function bindLoginForm() {
  const form = document.getElementById("loginForm");
  if (!form) return;
  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const u = document.getElementById("loginUser").value.trim();
    const p = document.getElementById("loginPass").value;
    const errBox = document.getElementById("loginErr");
    errBox.hidden = true;
    if (!u || !p) {
      errBox.textContent = "请输入账号和密码";
      errBox.hidden = false;
      return;
    }
    const submitBtn = form.querySelector(".login-submit");
    submitBtn.disabled = true;
    submitBtn.textContent = "登录中…";
    try {
      await doLogin(u, p);
      setScene("camera");
    } catch (err) {
      errBox.textContent = err.message || "登录失败";
      errBox.hidden = false;
    } finally {
      submitBtn.disabled = false;
      submitBtn.textContent = "登录";
    }
  });
})();

// 点击右上角姓名 → 询问是否退出
(function bindUserBtn() {
  const btn = document.getElementById("userWindowBtn");
  if (!btn) return;
  btn.addEventListener("click", () => {
    if (!state.currentUser) return;
    if (confirm(`确认退出登录？\n当前账号：${state.userName}`)) {
      doLogout();
    }
  });
})();

async function classifyAndProceed(files) {
  setScene("loading");
  startLoadingScript("classify");
  $("#loadingSub").textContent = `已选择 ${files.length} 张图片`;
  try {
    const result = await API.classify(files);
    state.classifyResult = result.classify || result;
    if (result.tmpDir && state.classifyResult && !state.classifyResult.tmpDir) {
      state.classifyResult.tmpDir = result.tmpDir;
    }
    state.pendingImageIds = (result.images || []).map(i => i.id);
    showClassifyResult();
  } catch (err) {
    toast(err.message);
    setScene("camera");
  }
}

function showClassifyResult() {
  const r = state.classifyResult;
  setScene("classify");
  const wrap = $("#classifyResult");
  if (!r || r.needsManualPick || r.templateId === "unknown") {
    wrap.classList.add("warn");
    wrap.innerHTML = `
      <div class="label">AI 不太确定</div>
      <div class="name">请手动选择模板</div>
      <div class="conf">${escapeHTML(r?.reason || "未识别到典型设备")}</div>
    `;
    showTemplateList(false);
  } else {
    wrap.classList.remove("warn");
    wrap.innerHTML = `
      <div class="label">AI 识别为</div>
      <div class="name">${escapeHTML(r.templateName)}</div>
      <div class="conf">置信度 ${Math.round(r.confidence * 100)}% · ${escapeHTML(r.reason || "")}</div>
    `;
    $("#templateList").innerHTML = "";
  }
}

function showTemplateList(showAll) {
  const list = $("#templateList");
  const tpls = (state.templates || []).filter(t => showAll || t.featured);
  list.innerHTML = tpls.map(t => `
    <div class="template-row" data-template="${escapeHTML(t.id)}">
      <div class="icon">${escapeHTML(t.name.charAt(0))}</div>
      <div class="meta">
        <div class="row1">${escapeHTML(t.name)}</div>
        <div class="row2">${escapeHTML(t.project)} · ${t.fields.length} 项 · ${t.hasAI ? "AI 识别" : "人工填写"}</div>
      </div>
      <div class="arrow">›</div>
    </div>
  `).join("");
  list.querySelectorAll(".template-row").forEach(row => {
    row.addEventListener("click", () => startRecordWithTemplate(row.dataset.template));
  });
}

async function startRecordWithTemplate(templateId) {
  try {
    const rt = state.retakeTarget;
    // 复检:强制目标模板 + 带目标点位,保证新记录落到同一台资产
    if (rt && rt.templateId) templateId = rt.templateId;
    const body = {
      templateId,
      inspector: state.inspector,
    };
    if (rt && rt.pointId) body.pointId = rt.pointId;
    if (state.classifyResult?.tmpDir) {
      body.tmpDir = state.classifyResult.tmpDir;
      body.imageIds = state.pendingImageIds;
    }
    if (state.activeEngineeringTaskId) {
      body.engineeringTaskId = state.activeEngineeringTaskId;
    }
    state.record = await API.createRecord(body);
    state.retakePending = false;
    localStorage.setItem("activeRecord", state.record.id);
    if (state.forceManual) {
      state.forceManual = false;
      await setManualMode();
      return;
    }
    if (state.record.images?.length > 0) {
      // 已经从 tmpDir 接管图片，直接发起识别
      await beginAnalysis();
    } else {
      // 无图（手动选模板没拍照）→ 进表单走 manual
      await setManualMode();
    }
  } catch (err) {
    toast(err.message);
    setScene("classify");
  }
}

async function setManualMode() {
  if (!state.record?.id) {
    state.forceManual = true;
    state.classifyResult = {
      ...(state.classifyResult || {}),
      needsManualPick: true,
      templateId: "unknown",
      reason: state.classifyResult?.reason || "请先选择日报模板",
    };
    showClassifyResult();
    toast("请先选择日报模板，再进入人工填写");
    return;
  }
  state.record = await API.enableManual(state.record.id);
  await renderForm();
  setScene("form");
}

async function beginAnalysis() {
  setScene("loading");
  startLoadingScript("analyze");
  $("#loadingSub").textContent = `已上传 ${state.record.images.length} 张照片 · 多字段并行识别`;
  try {
    const taskOrFallback = await API.startAnalysis(state.record.id);
    if (taskOrFallback && taskOrFallback.action === "manual_fallback") {
      state.record = taskOrFallback.record;
      toast("该模板暂未启用 AI，请人工填写");
      await renderForm();
      setScene("form");
      return;
    }
    pollTask();
  } catch (err) {
    toast(err.message);
    setScene("camera");
  }
}

function pollTask() {
  clearInterval(state.pollTimer);
  state.pollTimer = setInterval(async () => {
    try {
      const task = await API.latestTask(state.record.id);
      if (task.status === "succeeded") {
        clearInterval(state.pollTimer);
        state.record = await API.getRecord(state.record.id);
        state.retakePending = false;
        await renderForm();
        setScene("form");
        toast("识别完成，请逐项确认");
      } else if (task.status === "failed") {
        clearInterval(state.pollTimer);
        state.record = await API.getRecord(state.record.id);
        if (state.record.manualRequired) {
          state.retakePending = false;
          await renderForm();
          setScene("form");
          showManualHint();
          toast("AI 多次未稳定识别，已转人工填写");
        } else {
          showRetakeModal(task.errorMessage || state.record.retakeReason || "识别不稳定，请重拍");
        }
      } else {
        const total = task.progress?.total || 0;
        if (total > 1) {
          $("#loadingSub").textContent = `已完成 ${task.progress?.processed || 0}/${total} 张照片`;
        }
      }
    } catch (err) {
      clearInterval(state.pollTimer);
      toast(err.message);
    }
  }, 1200);
}

// ===== 失败弹窗 =====

function showRetakeModal(reason) {
  if (!state.record?.id) {
    state.forceManual = true;
    state.classifyResult = {
      ...(state.classifyResult || {}),
      needsManualPick: true,
      templateId: "unknown",
      reason: reason || "请先选择日报模板",
    };
    showClassifyResult();
    return;
  }
  $("#retakeDesc").textContent = reason;
  $("#retakeAttempt").textContent = `已尝试 ${state.record.captureAttempts || 0} / 3 次`;
  $("#retakeModal").hidden = false;
}
function hideRetakeModal() { $("#retakeModal").hidden = true; }

$("#retakeAgainBtn").addEventListener("click", () => {
  hideRetakeModal();
  state.retakePending = true;
  setScene("camera");
});
$("#retakeManualBtn").addEventListener("click", async () => {
  hideRetakeModal();
  state.retakePending = false;
  await setManualMode();
  if (state.record?.id) showManualHint();
});

function showManualHint() {
  $("#manualHint").hidden = false;
}

// ===== 表单（屏 4） =====

async function renderForm() {
  const rec = state.record;
  // 复检:把目标资产编号写进 asset_no,保证提交后归属同一台设备(多资产模板兜底)
  if (state.retakeTarget?.assetNo) {
    const f = rec.fields.find(x => x.code === "asset_no");
    if (f) { f.value = state.retakeTarget.assetNo; f.source = "manual"; f.confidence = 1; }
  }
  $("#formContext").innerHTML = renderContext(rec);
  $("#manualHint").hidden = !rec.manualRequired;
  const groups = $("#formGroups");
  const imagesHTML = (rec.images && rec.images.length) ? `
    <div class="group-title">巡检照片（${rec.images.length} 张）</div>
    <div class="group photo-strip" id="formPhotoStrip">
      ${rec.images.map((img, i) => `
        <button type="button" class="photo-thumb" data-img-idx="${i}" title="${escapeHTML(img.fileName || ('图 ' + (i+1)))}">
          <img src="${storageURL(thumbPath(img))}" alt="图 ${i+1}" loading="lazy" />
        </button>
      `).join("")}
    </div>
  ` : "";
  // 只统计置信度 <95% 的 AI 字段;≥95% 视为可信,不需人工确认。
  const unconfirmedCount = rec.fields.filter(f => f.source === "ai" && String(f.value || "").trim() !== "" && (f.confidence || 0) < 0.95).length;
  const confirmAllBanner = unconfirmedCount > 0 ? `
    <div class="confirm-all-banner" id="confirmAllBanner">
      <div class="cab-msg">
        <b>${unconfirmedCount}</b> 项识别置信偏低,请核对
      </div>
      <button type="button" id="confirmAllBtn">一键确认 (${unconfirmedCount})</button>
    </div>
  ` : "";
  groups.innerHTML = `
    ${imagesHTML}
    ${confirmAllBanner}
    <div class="group-title">日报字段</div>
    <div class="group">
      ${rec.fields.map(renderField).join("")}
    </div>
  `;
  // 绑定 input 变更 → 实时 PATCH；focus 时打开停留计时器（用于 §4 留痕的 durationMs）
  groups.querySelectorAll("[data-field-input]").forEach(el => {
    el.addEventListener("focus", () => trackFieldFocus(el.dataset.fieldCode));
    el.addEventListener("change", () => saveField(el.dataset.fieldCode));
  });
  // 复检:asset_no 已带入 → 落库持久化,确保归属同一台
  if (state.retakeTarget?.assetNo && groups.querySelector('[data-field-input][data-field-code="asset_no"]')) {
    saveField("asset_no");
  }
  updateRetakeBanner();
  // P0-4 一键确认按钮:批量给所有 source=ai 字段写一条 confirm-batch 留痕,主管能识别"批量过的"
  document.getElementById("confirmAllBtn")?.addEventListener("click", () => confirmAllAIFields());
  // 缩略图点击 → 弹大图
  const strip = $("#formPhotoStrip");
  if (strip) {
    strip.addEventListener("click", (e) => {
      const btn = e.target.closest(".photo-thumb");
      if (!btn) return;
      const idx = parseInt(btn.dataset.imgIdx, 10) || 0;
      openLightbox(state.record?.images || [], idx);
    });
  }
}

// §4 防惰性留痕的客户端轻量埋点：每个字段的最近一次 focus 时间 + 表单期间是否打开过原图。
// 在 saveField / markUncertain 时算出 durationMs、连同 viewedPhoto 一起 PATCH 给后端。
const FieldAudit = { focusAt: {}, viewedPhoto: false };
function trackFieldFocus(code) {
  if (code) FieldAudit.focusAt[code] = Date.now();
}
function popFieldDurationMs(code) {
  const t = FieldAudit.focusAt[code];
  if (!t) return 0;
  delete FieldAudit.focusAt[code];
  return Date.now() - t;
}

// ===== 图片大图预览 =====
const Lightbox = { images: [], index: 0 };
function openLightbox(images, index) {
  if (!images || !images.length) return;
  Lightbox.images = images;
  Lightbox.index = Math.max(0, Math.min(index, images.length - 1));
  FieldAudit.viewedPhoto = true; // 表单期间打开过原图 → 留痕里标 viewedPhoto=true
  renderLightbox();
  $("#imageLightbox").hidden = false;
  document.body.style.overflow = "hidden";
}
function closeLightbox() {
  $("#imageLightbox").hidden = true;
  document.body.style.overflow = "";
  // 释放大图引用
  $("#lightboxImg").src = "";
}
function lightboxStep(delta) {
  const n = Lightbox.images.length;
  if (n <= 1) return;
  Lightbox.index = (Lightbox.index + delta + n) % n;
  renderLightbox();
}
function renderLightbox() {
  const img = Lightbox.images[Lightbox.index];
  if (!img) return;
  const url = storageURL(thumbPath(img));
  $("#lightboxImg").src = url;
  $("#lightboxImg").alt = img.fileName || ("图 " + (Lightbox.index + 1));
  const multi = Lightbox.images.length > 1;
  $("#lightboxPrev").hidden = !multi;
  $("#lightboxNext").hidden = !multi;
}

// 把后端 path（可能是 ..\storage\... 或绝对路径）转成 /storage/ 后面那段
function thumbPath(img, recordIDFallback) {
  const raw = (img.path || "").replace(/\\/g, "/");
  const idx = raw.indexOf("/storage/");
  if (idx >= 0) return raw.substring(idx + "/storage/".length);
  // 兜底：uploads/{recordID}/{fileName}
  return `uploads/${recordIDFallback || state.record?.id || ""}/${img.fileName || ""}`;
}

function storageURL(path) {
  const base = "/storage/" + encodeURI(path || "");
  return base;
}

function renderContext(rec) {
  const status = recognitionStatusText(rec.recognitionStatus);
  const cls = rec.manualRequired ? "warn" : (rec.recognitionStatus === "recognized" ? "" : "warn");
  return `
    <div class="avatar">${escapeHTML((rec.project || "巡").charAt(0))}</div>
    <div class="meta">
      <div class="row1">${escapeHTML(rec.project)} · ${escapeHTML(rec.pointName)}</div>
      <div class="row2">${escapeHTML(rec.templateName)} · ${escapeHTML(rec.inspector)}</div>
    </div>
    <div class="badge ${cls}">${escapeHTML(status)}</div>
  `;
}

function recognitionStatusText(s) {
  return ({
    not_started: "未识别",
    processing: "识别中",
    recognized: "已识别",
    retake_required: "待重拍",
    manual_required: "人工填写",
  })[s] || s || "-";
}

function renderField(field) {
  const isLong = field.kind === "text" && (field.label.includes("说明") || field.label.includes("备注") || field.label.includes("记录"));
  const pillClass = pillFor(field);
  const pillText = pillTextFor(field);
  const reqMark = field.required ? `<span class="req">*</span>` : "";

  let control = "";
  if (field.kind === "choice") {
    const opts = ['<option value="">请选择</option>']
      .concat((field.options || []).map(o =>
        `<option value="${escapeHTML(o)}" ${o === field.value ? "selected" : ""}>${escapeHTML(o)}</option>`));
    control = `<select data-field-input data-field-code="${escapeHTML(field.code)}">${opts.join("")}</select>`;
  } else if (field.kind === "number") {
    control = `<input data-field-input data-field-code="${escapeHTML(field.code)}" type="number" step="any" value="${escapeHTML(field.value)}" placeholder="请输入数值" />`;
  } else if (isLong) {
    return `
      <div class="field block">
        <div class="label">${escapeHTML(field.label)}${reqMark} ${pillText ? `<span class="ai-pill ${pillClass}">${pillText}</span>` : ""}</div>
        <textarea data-field-input data-field-code="${escapeHTML(field.code)}" placeholder="可选填写">${escapeHTML(field.value)}</textarea>
      </div>
    `;
  } else {
    control = `<input data-field-input data-field-code="${escapeHTML(field.code)}" value="${escapeHTML(field.value)}" placeholder="请输入" />`;
  }

  return `
    <div class="field">
      <div class="label">${escapeHTML(field.label)}${reqMark}</div>
      <div class="value">
        ${pillText ? `<span class="ai-pill ${pillClass}">${pillText}</span>` : ""}
        ${control}
      </div>
    </div>
  `;
}

// ===== 浏览器 GPS + 反查地名（填到「巡检地点」字段） =====
// 优先 Nominatim（OpenStreetMap）— 街道/POI 级别，免 key
// fallback BigDataCloud — 粗粒度但快
async function reverseGeocode(lat, lng) {
  // 1) Nominatim：street/POI 级别
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 5000);
    const url = `https://nominatim.openstreetmap.org/reverse?format=json&lat=${lat}&lon=${lng}&zoom=18&addressdetails=1&accept-language=zh-CN`;
    const res = await fetch(url, { signal: ctrl.signal, headers: { Accept: "application/json" } });
    clearTimeout(timer);
    if (res.ok) {
      const d = await res.json();
      const a = d.address || {};
      // 拼 [省][市][区][街道][门牌][小区/POI] — 顺序按从大到小
      const raw = [
        a.state || a.province,
        a.city || a.town || a.county,
        a.district || a.city_district || a.suburb,
        a.road || a.street || a.pedestrian || a.path,
        a.house_number,
        a.neighbourhood || a.residential || a.quarter || a.amenity || a.building,
      ].filter(s => s && String(s).trim());
      // 去掉相邻重复
      const dedup = [];
      for (const p of raw) if (dedup[dedup.length - 1] !== p) dedup.push(p);
      const place = dedup.join("");
      if (place) return place;
      if (d.display_name) return d.display_name;
    }
  } catch {}
  // 2) BigDataCloud fallback：到县/区级
  try {
    const ctrl = new AbortController();
    const timer = setTimeout(() => ctrl.abort(), 3000);
    const url = `https://api.bigdatacloud.net/data/reverse-geocode-client?latitude=${lat}&longitude=${lng}&localityLanguage=zh`;
    const res = await fetch(url, { signal: ctrl.signal });
    clearTimeout(timer);
    if (res.ok) {
      const d = await res.json();
      const parts = [d.principalSubdivision, d.city, d.locality].filter(s => s && String(s).trim());
      const place = parts.join("");
      if (place) return place;
    }
  } catch {}
  return null;
}

function requestLocation(fieldCode) {
  if (!navigator.geolocation) {
    toast("当前浏览器不支持定位");
    return;
  }
  const btn = document.querySelector(`[data-locate-for="${fieldCode}"]`);
  const input = document.querySelector(`[data-field-input][data-field-code="${fieldCode}"]`);
  if (btn) { btn.disabled = true; btn.classList.add("locating"); }
  const restore = () => { if (btn) { btn.disabled = false; btn.classList.remove("locating"); } };

  navigator.geolocation.getCurrentPosition(
    async (pos) => {
      const { latitude: lat, longitude: lng, accuracy } = pos.coords;
      const place = await reverseGeocode(lat, lng);
      const accNum = Number.isFinite(accuracy) ? Math.round(accuracy) : null;
      const accTag = accNum != null ? `（±${accNum}m）` : "";
      let text;
      if (place) {
        text = place + accTag;
      } else {
        const ns = lat >= 0 ? "N" : "S";
        const ew = lng >= 0 ? "E" : "W";
        text = `${Math.abs(lat).toFixed(4)}°${ns}, ${Math.abs(lng).toFixed(4)}°${ew}${accTag}`;
      }
      if (input) {
        input.value = text;
        input.dispatchEvent(new Event("change", { bubbles: true }));
        if (place) {
          toast(accNum != null && accNum > 300
            ? `已定位：${place} · 精度较低 ±${accNum}m，可手动修正`
            : `已定位：${place}`);
        } else {
          toast("已填入坐标（地名解析失败）");
        }
      }
      restore();
    },
    (err) => {
      const msg = err.code === 1 ? "未授予定位权限"
                : err.code === 2 ? "定位不可用"
                : err.code === 3 ? "定位超时"
                : "定位失败";
      toast(msg);
      restore();
    },
    // 高精度模式 + 不缓存（每次重新定位）
    { enableHighAccuracy: true, timeout: 12000, maximumAge: 0 }
  );
}

function pillFor(f) {
  if (f.source === "human-confirmed" || f.source === "human-edited") return "edited";
  if (f.source === "ai" && f.needsReview) return "review";
  if (f.source === "ai" && !f.needsReview) return "confirmed";
  if (f.source === "manual" && !f.value) return "empty";
  return "";
}
function pillTextFor(f) {
  if (f.source === "human-confirmed") return "已确认";
  if (f.source === "human-edited") return "已修改";
  if (f.source === "ai" && f.confidence) return `AI ${Math.round(f.confidence * 100)}%`;
  if (f.source === "ai" && String(f.value || "").trim()) return "AI 识别";
  return "";
}

async function saveField(code) {
  const el = document.querySelector(`[data-field-input][data-field-code="${CSS.escape(code)}"]`);
  if (!el) return;
  const field = state.record.fields.find(f => f.code === code);
  if (!field) return;
  const opts = {
    durationMs: popFieldDurationMs(code),
    viewedPhoto: FieldAudit.viewedPhoto,
  };
  try {
    const updated = await API.patchField(state.record.id, code, el.value, field.version, opts);
    Object.assign(field, updated);
    FieldAudit.viewedPhoto = false; // 留痕一次后重置「是否看图」标记
    // 刷新 pill 显示
    const fieldEl = el.closest(".field");
    const pill = fieldEl.querySelector(".ai-pill");
    if (pill) {
      pill.className = "ai-pill " + pillFor(field);
      pill.textContent = pillTextFor(field);
    }
  } catch (err) {
    toast(err.message);
  }
}

// P0-4 一键确认所有 source=ai 字段:批量调 patchField (action=confirm-batch),
// 让主管能在 confirm_logs 里看出"这是批量过的,不是逐项检查的",对应放进抽检池。
async function confirmAllAIFields() {
  if (!state.record) return;
  const aiFields = state.record.fields.filter(f =>
    f.source === "ai" && String(f.value || "").trim() !== "" && (f.confidence || 0) < 0.95
  );
  if (aiFields.length === 0) {
    toast("没有需要确认的字段");
    return;
  }
  if (!confirm(`确认这 ${aiFields.length} 项?`)) return;
  const btn = document.getElementById("confirmAllBtn");
  if (btn) { btn.disabled = true; btn.textContent = `处理中… 0/${aiFields.length}`; }
  let ok = 0;
  for (const field of aiFields) {
    try {
      const opts = {
        action: "confirm-batch",
        durationMs: popFieldDurationMs(field.code),
        viewedPhoto: FieldAudit.viewedPhoto,
      };
      const updated = await API.patchField(state.record.id, field.code, field.value, field.version, opts);
      Object.assign(field, updated);
      ok++;
      if (btn) btn.textContent = `处理中… ${ok}/${aiFields.length}`;
    } catch (err) {
      toast(`字段 ${field.label} 确认失败:${err.message || err}`);
    }
  }
  FieldAudit.viewedPhoto = false;
  toast(`已批量确认 ${ok} / ${aiFields.length} 个字段`);
  renderForm(); // 重渲染 -> banner 应消失或更新数量
}

async function saveAndPreview() {
  // 检查必填
  const missing = state.record.fields.filter(f => f.required && !String(f.value || "").trim());
  if (missing.length) {
    toast(`必填字段未完成：${missing[0].label}`);
    return;
  }
  // 拉一次最新数据
  state.record = await API.getRecord(state.record.id);
  renderPreview();
  setScene("preview");
}

// ===== 预览 + 提交（屏 5） =====

function renderPreview() {
  const rec = state.record;
  $("#previewContext").innerHTML = renderContext(rec);
  $("#previewFields").innerHTML = rec.fields.map(f => `
    <div class="field">
      <div class="label">${escapeHTML(f.label)}</div>
      <div class="value">${escapeHTML(f.value || "-")}</div>
    </div>
  `).join("");

  if (rec.aiSummary) {
    $("#summaryCard").hidden = false;
    $("#summaryBody").innerHTML = summaryBodyHTML(rec);
    const st = inspectionStatus(rec);
    const badge = $("#summaryStatus");
    if (badge) {
      badge.hidden = false;
      badge.textContent = st;
      badge.className = "sum-status " + ({ "正常": "ok", "异常": "bad", "待复核": "warn" }[st] || "warn");
    }
  } else {
    $("#summaryCard").hidden = true;
  }
  if (Array.isArray(rec.aiRecommendations)) {
    $("#recosCard").hidden = false;
    if (rec.aiRecommendations.length === 0 && rec.submitted) {
      $("#recosList").innerHTML = `<div class="empty">AI 本次未生成行动建议</div>`;
    } else if (rec.aiRecommendations.length === 0) {
      $("#recosCard").hidden = true;
    } else {
      const priOrder = { high: 0, medium: 1, low: 2 };
      const sortedRecos = [...rec.aiRecommendations].sort(
        (a, b) => (priOrder[a.priority] ?? 3) - (priOrder[b.priority] ?? 3));
      $("#recosList").innerHTML = sortedRecos.map(r => `
        <div class="reco">
          <div class="pri ${escapeHTML(r.priority || 'low')}">${priorityText(r.priority)}</div>
          <div class="body">
            <div class="text">${escapeHTML(r.text)}</div>
            <div class="meta">
              <span class="cat">${escapeHTML(r.category || '建议')}</span>
              依据：${escapeHTML(r.basis || '基于本次字段')}
            </div>
          </div>
        </div>
      `).join("");
    }
  }
  if (rec.aiSummaryError) {
    $("#aiErrorBanner").hidden = false;
    $("#aiErrorBanner").textContent = `AI 总结生成异常：${rec.aiSummaryError}（已使用本地兜底文本）`;
  } else {
    $("#aiErrorBanner").hidden = true;
  }
}

function priorityText(p) {
  return ({ high: "高", medium: "中", low: "低" })[p] || "·";
}

// 与后端 isOccurrenceLabel / inferOverallStatus 一致：判断本次巡检整体状态
function isOccurrenceLabel(label = "") {
  if (label.includes("有无") || label.includes("是否有")) return true;
  if (label.includes("无异") || label.includes("无报警") || label.includes("无漏") || label.includes("无故障")) return false;
  return ["异常", "是否漏水", "是否报警", "有异响", "有异味"].some((k) => label.includes(k));
}
// 把「字段=值」翻成异常结论(待跟进块展示用):灭火器材未过期=否 → 灭火器材已过期
function anomalyText(label = "", value = "") {
  const v = String(value).trim();
  if (["异常", "缺失", "破损", "故障"].includes(v)) return label + v;
  if (v === "是" || v === "有") {
    if (label.startsWith("是否")) return label.slice(2);
    if (label.startsWith("有无")) return "有" + label.slice(2);
    return label; // "有异响"之类本身就是异常表述
  }
  // v === "否":正向问句取反成异常结论
  const neg = [
    ["干净无杂物", "有杂物"], ["无异响", "有异响"], ["无异味", "有异味"], // 长词优先,避免被短词抢先
    ["未过期", "已过期"], ["无卡阻", "有卡阻"], ["无杂物", "有杂物"],
    ["完好", "破损"], ["有效", "失效"], ["齐全", "缺失"],
    ["正常", "异常"], ["干净", "不整洁"], ["整洁", "不整洁"], ["顺畅", "不顺畅"],
  ];
  for (const [a, b] of neg) if (label.includes(a)) return label.replace(a, b);
  return label + "不符合";
}
// 预览页 AI 总结正文：异常项拎成红色「待跟进」小块，正文去掉模板腔(待跟进/异常提示/低风险总结前后缀)
function summaryBodyHTML(rec) {
  const flags = [];
  for (const f of (rec.fields || [])) {
    const v = String(f.value || "").trim();
    if (!v) continue;
    let bad = false;
    if (["异常", "缺失", "破损", "故障"].includes(v)) bad = true;
    else if (v === "否") bad = !isOccurrenceLabel(f.label);
    else if (v === "是" || v === "有") bad = isOccurrenceLabel(f.label);
    if (bad) flags.push({ label: f.label, value: v });
  }
  const text = String(rec.aiSummary || "")
    .replace(/^待跟进[^：:]*[：:][^。]*。\s*/, "")
    .replace(/^异常提示[:：][^。]*。\s*/, "")
    .replace(/低风险总结[:：][^。]*。?\s*$/, "")
    .trim();
  let html = "";
  if (flags.length) {
    html += `<div class="sum-flags"><div class="sum-flags-h">待跟进 · ${flags.length} 项</div>`
      + flags.map(f => `<div class="sum-flag"><b>${escapeHTML(anomalyText(f.label, f.value))}</b></div>`).join("")
      + `</div>`;
  }
  if (text) html += `<div class="sum-text">${escapeHTML(text)}</div>`;
  return html || `<div class="sum-text">${escapeHTML(rec.aiSummary || "")}</div>`;
}

function inspectionStatus(rec) {
  let abnormal = false, unfilled = false;
  for (const f of (rec.fields || [])) {
    const v = String(f.value || "").trim();
    if (!v) { if (f.required) unfilled = true; continue; }
    if (["异常", "缺失", "破损", "故障"].includes(v)) abnormal = true;
    else if (v === "否") { if (!isOccurrenceLabel(f.label)) abnormal = true; }
    else if (v === "是" || v === "有") { if (isOccurrenceLabel(f.label)) abnormal = true; }
  }
  return abnormal ? "异常" : (unfilled ? "待复核" : "正常");
}

async function submitRecord() {
  $("#primaryBtn").disabled = true;
  $("#primaryBtn").textContent = "AI 总结生成中…";
  try {
    state.record = await API.submit(state.record.id);
    renderPreview();
    setScene("preview");
    localStorage.removeItem("activeRecord");
    const wasRetake = state.retakeTarget;
    state.retakeTarget = null;   // 复检提交完毕,清掉上下文
    updateRetakeBanner();
    if (wasRetake) {
      toast(`复检已提交，「${wasRetake.assetName}」健康档案已更新`);
    } else if (state.activeEngineeringTaskId) {
      toast("提交成功，巡检任务已闭环");
      setActiveTask(null);
    } else {
      toast("提交成功，设备健康档案已更新");
    }
  } catch (err) {
    toast(err.message);
    $("#primaryBtn").disabled = false;
    $("#primaryBtn").textContent = "提交日报";
  }
}

// ===== 台账（屏 6） =====

async function goLedger() {
  setScene("ledger");
  try {
    const data = await API.assets();
    state.assets = data.assets || [];
    state.assetSummary = data.summary || data.totalSummary || null;
  } catch (err) { toast(err.message); }
  renderLedgerOverview();
  renderLedgerGroups();
  bindOverviewFilters();
  renderAssets();
  renderFilterBar();
  highlightActiveFilter();
}

// ===== 台账过滤器（多字段组合：project + assetType + level + today 同时生效） =====
// state.assetFilter 形如：{ project?, assetType?, level?, today? }
// 空对象 = 清空所有
function setAssetFilter(input) {
  state.expandedGroups = state.expandedGroups || { project: true, assetType: true };
  const cur = state.assetFilter || {};
  let next;

  if (!input || input.kind === "all") {
    // 「资产总数」/「清空」 → 重置全部
    next = {};
    state.expandedGroups = { project: true, assetType: true };
  } else if (typeof input === "object" && !("kind" in input)) {
    // 直接传完整 filter 对象（filter-bar 移除单个 chip 用）
    next = { ...input };
  } else {
    // toggle 单个字段（点 lo-card / lg-pill / fb-chip）
    const { kind, value } = input;
    next = { ...cur };
    if (kind === "today") {
      if (cur.today) delete next.today; else next.today = true;
    } else {
      if (cur[kind] === value) delete next[kind]; else next[kind] = value;
    }
    // 选中分组 pill → 折叠该分组
    if ((kind === "project" || kind === "assetType") && next[kind]) {
      state.expandedGroups[kind] = false;
    }
  }

  state.assetFilter = next;
  renderLedgerGroups();
  highlightActiveFilter();
  renderAssets();
  renderFilterBar();
}

function applyAssetFilter(assets) {
  const f = state.assetFilter || {};
  if (Object.keys(f).length === 0) return assets;
  const now = new Date();
  const today = `${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}-${String(now.getDate()).padStart(2,'0')}`;
  return assets.filter(a => {
    if (f.level === "normal" && !((a.statusLevel === "normal") || (a.lastStatus === "正常"))) return false;
    if (f.level === "risk" && !(["warning","danger","repair"].includes(a.statusLevel) || ["异常","待复核","待维修"].includes(a.lastStatus))) return false;
    if (f.today && (a.lastInspectedAt || "").slice(0,10) !== today) return false;
    if (f.project && !(a.project === f.project || a.projectCode === f.project)) return false;
    if (f.assetType && a.assetType !== f.assetType) return false;
    return true;
  });
}

function bindOverviewFilters() {
  const map = [
    { id: "loTotal",  filter: { kind: "all" } },
    { id: "loNormal", filter: { kind: "level", value: "normal" } },
    { id: "loRisk",   filter: { kind: "level", value: "risk" } },
    { id: "loToday",  filter: { kind: "today" } },
  ];
  for (const { id, filter } of map) {
    const card = document.getElementById(id)?.closest(".lo-card");
    if (!card || card.dataset._bound) continue;
    card.dataset._bound = "1";
    card.style.cursor = "pointer";
    card.addEventListener("click", () => setAssetFilter(filter));
  }
}

function highlightActiveFilter() {
  const f = state.assetFilter || {};
  const empty = Object.keys(f).length === 0;
  const matches = {
    loTotal:  empty,
    loNormal: f.level === "normal",
    loRisk:   f.level === "risk",
    loToday:  !!f.today,
  };
  for (const [id, match] of Object.entries(matches)) {
    const card = document.getElementById(id)?.closest(".lo-card");
    if (card) card.classList.toggle("selected", match);
  }
  document.querySelectorAll(".lg-pill[data-filter-kind]").forEach(pill => {
    const match = f[pill.dataset.filterKind] === pill.dataset.filterValue;
    pill.classList.toggle("selected", match);
  });
}

function renderFilterBar() {
  const f = state.assetFilter || {};
  let bar = document.getElementById("filterBar");
  if (!bar) {
    const list = document.getElementById("assetList");
    if (!list) return;
    bar = document.createElement("div");
    bar.id = "filterBar";
    bar.className = "filter-bar";
    list.parentNode.insertBefore(bar, list);
  }
  const empty = Object.keys(f).length === 0;
  if (empty) {
    bar.hidden = true; bar.innerHTML = "";
    return;
  }
  // 每个激活字段一个可关闭 chip
  const chips = [];
  if (f.level === "normal") chips.push({ kind: "level", text: "正常" });
  if (f.level === "risk")   chips.push({ kind: "level", text: "异常 / 待复核" });
  if (f.today)              chips.push({ kind: "today", text: "今日新增" });
  if (f.project)            chips.push({ kind: "project",   text: `项目 · ${f.project}` });
  if (f.assetType)          chips.push({ kind: "assetType", text: `类型 · ${f.assetType}` });
  const filtered = applyAssetFilter(state.assets || []);
  bar.hidden = false;
  bar.innerHTML = `
    <span class="fb-label">筛选</span>
    <span class="fb-chips">
      ${chips.map(c => `<span class="fb-chip" data-remove-kind="${escapeHTML(c.kind)}">${escapeHTML(c.text)}<span class="fb-x">×</span></span>`).join("")}
    </span>
    <span class="fb-count">${filtered.length} / ${(state.assets||[]).length}</span>
    <button type="button" class="fb-clear">清空</button>
  `;
  bar.querySelectorAll("[data-remove-kind]").forEach(chip => {
    chip.addEventListener("click", () => {
      const k = chip.dataset.removeKind;
      const next = { ...(state.assetFilter || {}) };
      delete next[k];
      setAssetFilter(next);
    });
  });
  bar.querySelector(".fb-clear").addEventListener("click", () => setAssetFilter({ kind: "all" }));
}

function renderLedgerOverview() {
  const assets = state.assets || [];
  // 用本地日期：lastInspectedAt 是带时区的 RFC3339（如 +08:00），slice(0,10) 取的是本地日历日。
  // 这里也要本地日，避免凌晨 0-8 点用 UTC 把"今日"算到昨天。
  const now = new Date();
  const today = `${now.getFullYear()}-${String(now.getMonth()+1).padStart(2,'0')}-${String(now.getDate()).padStart(2,'0')}`;
  let normal = 0, risk = 0, todayCnt = 0;
  const summary = state.assetSummary;
  if (summary) {
    normal = summary.normal || 0;
    // 需跟进口径(两端统一)：异常+待复核+待维修；未巡检(unknown)不算需跟进
    risk = (summary.warning || 0) + (summary.danger || 0) + (summary.repair || 0);
  } else {
    for (const a of assets) {
      const s = a.lastStatus || "";
      if (s === "正常") normal++;
      else if (s === "异常" || s === "待复核" || s === "待维修") risk++;
    }
  }
  for (const a of assets) {
    if ((a.lastInspectedAt || "").slice(0, 10) === today) {
      todayCnt++;
    }
  }
  const total = $("#loTotal"); if (total) total.textContent = summary?.total ?? assets.length;
  const ok = $("#loNormal"); if (ok) ok.textContent = normal;
  const wn = $("#loRisk"); if (wn) wn.textContent = risk;
  const td = $("#loToday"); if (td) td.textContent = todayCnt;
}

function renderLedgerGroups() {
  const wrap = $("#ledgerGroups");
  if (!wrap) return;
  const summary = state.assetSummary;
  if (!summary) {
    wrap.innerHTML = "";
    return;
  }
  const f = state.assetFilter || {};
  state.expandedGroups = state.expandedGroups || { project: false, assetType: false };

  const buildCard = (title, kind, groups, sizeUnit) => {
    const activeValue = f[kind];     // 多字段 schema：直接读
    const expanded = !!state.expandedGroups[kind];
    const total = groups?.length || 0;
    const meta = activeValue
      ? `<em class="lg-active">已选：${escapeHTML(activeValue)}</em>`
      : `<em>${total} ${sizeUnit}</em>`;
    return `
      <div class="ledger-group-card${expanded ? "" : " collapsed"}" data-group="${kind}">
        <div class="lg-head">
          <span>${title}</span>
          ${meta}
          <button type="button" class="lg-toggle" data-toggle-group="${kind}" aria-expanded="${expanded}">${expanded ? "收起" : "展开"}</button>
        </div>
        ${expanded ? `<div class="lg-list">${renderGroupPills(groups || [], kind)}</div>` : ""}
      </div>
    `;
  };
  wrap.innerHTML = buildCard("按项目", "project", summary.byProject, "组")
                 + buildCard("按类型", "assetType", summary.byAssetType, "类");

  // pill 点击 → 多字段 toggle（setAssetFilter 内部已处理同值再点 = 取消）
  wrap.querySelectorAll(".lg-pill[data-filter-kind]").forEach(pill => {
    pill.addEventListener("click", () => {
      setAssetFilter({ kind: pill.dataset.filterKind, value: pill.dataset.filterValue });
    });
  });
  // 折叠分组的「展开/收起」按钮
  wrap.querySelectorAll("[data-toggle-group]").forEach(btn => {
    btn.addEventListener("click", (e) => {
      e.stopPropagation();
      const kind = btn.dataset.toggleGroup;
      state.expandedGroups[kind] = !state.expandedGroups[kind];
      renderLedgerGroups();
      highlightActiveFilter();
    });
  });
}

function renderGroupPills(groups, kind) {
  if (!groups.length) return `<div class="lg-empty">暂无分类数据</div>`;
  return groups.slice(0, 6).map(g => {
    const statusText = Object.entries(g.byStatus || {})
      .map(([k, v]) => `${k}${v}`)
      .join(" / ");
    const value = g.label || g.key || "未分类";
    return `
      <div class="lg-pill" data-filter-kind="${escapeHTML(kind)}" data-filter-value="${escapeHTML(value)}" role="button" tabindex="0">
        <b>${escapeHTML(value)}</b>
        <span>${g.total || 0} 项${statusText ? " · " + escapeHTML(statusText) : ""}</span>
      </div>
    `;
  }).join("");
}

// 需跟进严重度：异常 > 待维修 > 待复核；不在表内 = 健康段
const FOLLOWUP_SEVERITY = { "异常": 0, "待维修": 1, "待复核": 2 };

// 卡片总结净化：健康时去掉 AI 的"异常提示：…"格式前缀(否则正常设备却显示"发现X项异常")，
// 需跟进时红字提炼异常字段。让卡片文案与状态徽标一致、干净。
function assetCardSummary(a) {
  const raw = String(a.lastSummary || "").trim();
  if (a.lastStatus in FOLLOWUP_SEVERITY) {
    const m = raw.match(/发现\s*\d+\s*项[^：:]*[：:]\s*([^。]+)/);
    const fields = m ? m[1].trim() : "";
    return { tone: "warn", text: fields ? `需跟进：${fields}` : "存在待跟进项，待复查" };
  }
  const body = raw
    .replace(/^异常提示[:：][^。]*。\s*/, "")
    .replace(/低风险总结[:：][^。]*。?\s*$/, "")
    .trim();
  return { tone: "ok", text: body || "本次巡检正常" };
}

function assetRowHTML(a) {
  const cover = a.coverImage
    ? `<div class="asset-cover"><img src="${storageURL(thumbPath(a.coverImage, a.lastRecordId))}" alt="" loading="lazy" /></div>`
    : `<div class="asset-cover empty"></div>`;
  const subParts = [a.project, a.assetType];
  if (a.lastInspector) subParts.push("最近 " + a.lastInspector);
  return `
    <div class="asset-row" data-id="${escapeHTML(a.id)}">
      ${cover}
      <div class="meta">
        <div class="name">${escapeHTML(a.assetName)}</div>
        <div class="sub">${subParts.map(escapeHTML).join(" · ")}</div>
        ${(() => { const s = assetCardSummary(a); return `<div class="summary ${s.tone}">${escapeHTML(s.text)}</div>`; })()}
      </div>
      <div class="asset-side">
        <div class="status ${escapeHTML(a.lastStatus || '')}">${escapeHTML(a.lastStatus || '未巡检')}</div>
        <div class="count-badge">${a.inspectionCount} 次</div>
      </div>
    </div>
  `;
}

function renderAssets() {
  const list = $("#assetList");
  if (!state.assets.length) {
    list.innerHTML = `<div class="empty-tip">还没有设备档案<br>提交一条巡检即自动建档</div>`;
    return;
  }
  const filtered = applyAssetFilter(state.assets);
  if (!filtered.length) {
    list.innerHTML = `<div class="empty-tip">没有符合当前筛选的设备<br>点击上方「已巡设备」清空筛选</div>`;
    return;
  }
  // 两段式：需跟进置顶（按严重度，再按最近巡检时间），健康段按时间倒序
  const followup = [];
  const healthy = [];
  for (const a of filtered) {
    (a.lastStatus in FOLLOWUP_SEVERITY ? followup : healthy).push(a);
  }
  const byTime = (x, y) => (y.lastInspectedAt || "").localeCompare(x.lastInspectedAt || "");
  followup.sort((x, y) =>
    (FOLLOWUP_SEVERITY[x.lastStatus] - FOLLOWUP_SEVERITY[y.lastStatus]) || byTime(x, y));
  healthy.sort(byTime);

  let html = "";
  if (followup.length) {
    html += `<div class="asset-section warn">需跟进 · ${followup.length}</div>`;
    html += followup.map(assetRowHTML).join("");
  }
  if (healthy.length) {
    html += `<div class="asset-section ok">健康 · ${healthy.length}</div>`;
    html += healthy.map(assetRowHTML).join("");
  }
  list.innerHTML = html;
  // 行点击 → 设备档案（只读）
  list.querySelectorAll(".asset-row").forEach(row => {
    row.addEventListener("click", () => openAssetDetail(row.dataset.id));
  });
}

// ===== 资产详情（只读 scene） =====

const ASSET_STATUSES = ["正常", "异常", "待复核", "待维修"];

async function openAssetDetail(assetId) {
  setScene("asset");
  state.assetHistoryExpanded = false;
  state.assetReqExpanded = false;
  $("#assetDetailBody").innerHTML = `<div class="empty-tip">加载中…</div>`;
  try {
    const data = await API.getAsset(assetId);
    state.currentAsset = data.asset;
    state.currentAssetHistory = data.history || [];
    // 一线人员看自己的申请，主管/管理员看该资产相关申请。
    const requestQuery = canReview() ? {} : { mine: "1" };
    const my = await API.listChangeRequests(requestQuery);
    state.currentAssetRequests = (my.requests || []).filter(r =>
      (r.targetType === "asset" && r.targetId === assetId) ||
      (r.targetType === "record" && state.currentAssetHistory.some(h => h.id === r.targetId))
    );
    renderAssetDetail();
  } catch (err) {
    toast("加载失败：" + err.message);
    goLedger();
  }
}

function renderAssetDetail() {
  const a = state.currentAsset;
  const history = state.currentAssetHistory;
  const myReqs = state.currentAssetRequests || [];

  const HISTORY_LIMIT = 7;
  const histShown = state.assetHistoryExpanded ? history : history.slice(0, HISTORY_LIMIT);
  const histMoreBtn = history.length > HISTORY_LIMIT
    ? `<button type="button" class="more-btn" data-more="history">${state.assetHistoryExpanded ? "收起" : `查看更多 ${history.length - HISTORY_LIMIT} 次`}</button>` : "";
  const historyHTML = history.length ? histShown.map((r, i) => {
    const when = (r.submittedAt || r.createdAt || "").substring(0,16).replace("T"," ");
    const photos = (r.images || []).map((img, j) => `
      <button type="button" class="photo-thumb" data-rec="${i}" data-img="${j}">
        <img src="${storageURL(thumbPath(img, r.id))}" alt="" loading="lazy" />
      </button>
    `).join("");
    const fields = (r.fields || []).filter(f => f.value != null && f.value !== "").map(f => `
      <div class="kv"><em>${escapeHTML(f.label)}</em><span>${escapeHTML(String(f.value))}</span></div>
    `).join("");
    return `
      <details class="history-item"${i === 0 ? " open" : ""}>
        <summary>
          <span class="when">${escapeHTML(when)}</span>
          <span class="who">${escapeHTML(r.inspector || "")}</span>
          <span class="badge">${(r.images || []).length} 图</span>
        </summary>
        <div class="history-body">
          ${photos ? `<div class="photo-strip" data-history-photos="${i}">${photos}</div>` : ""}
          ${fields ? `<div class="kv-grid">${fields}</div>` : ""}
          ${r.aiSummary ? `<div class="kv-summary">${escapeHTML(r.aiSummary)}</div>` : ""}
        </div>
      </details>
    `;
  }).join("") : `<div class="empty-tip" style="text-align:left;padding:14px 0">暂无历史巡检</div>`;

  const reqTitle = canReview() ? "相关申请" : "我的申请";
  const reqShown = state.assetReqExpanded ? myReqs : myReqs.slice(0, 1);
  const reqMoreBtn = myReqs.length > 1
    ? `<button type="button" class="more-btn" data-more="req">${state.assetReqExpanded ? "收起" : `查看全部 ${myReqs.length} 条`}</button>` : "";
  const reqHTML = myReqs.length ? `
    <div class="section-head">${reqTitle}</div>
    <div class="cr-list">
      ${reqShown.map(c => `
        <div class="cr-item cr-${escapeHTML(c.status)}">
          <div class="cr-row">
            <span class="cr-status">${crStatusText(c.status)}</span>
            <span class="cr-when">${(c.requestedAt || "").substring(0,16).replace("T"," ")}</span>
          </div>
          <div class="cr-patch">${escapeHTML(describePatch(c))}</div>
          ${c.reason ? `<div class="cr-reason">${escapeHTML(c.reason)}</div>` : ""}
          ${c.reviewNote ? `<div class="cr-note">${escapeHTML(c.reviewNote)}</div>` : ""}
          ${c.status === "pending" ? `<div class="cr-actions"><button type="button" class="btn-ghost" data-withdraw="${escapeHTML(c.id)}">撤回</button></div>` : ""}
        </div>
      `).join("")}
      ${reqMoreBtn}
    </div>
  ` : "";

  const followup = a.lastStatus in FOLLOWUP_SEVERITY;
  const lastWhen = (a.lastInspectedAt || "").substring(5, 16).replace("T", " ");
  const sum = assetCardSummary(a);
  $("#assetDetailBody").innerHTML = `
    <div class="asset-hero ${followup ? "warn" : "ok"}">
      <div class="ah-top">
        <div class="ah-name">${escapeHTML(a.assetName || "设备")}</div>
        <span class="ah-status">${followup ? "需跟进" : "健康"}</span>
      </div>
      <div class="ah-sub">${escapeHTML(a.assetType || "设备")}${a.project ? " · " + escapeHTML(a.project) : ""}</div>
      <div class="ah-rows">
        <div><span>累计巡检</span><b>${a.inspectionCount} 次</b></div>
        <div><span>最近体检</span><b>${(a.lastInspectedAt || "").substring(0,16).replace("T"," ") || "—"}</b></div>
        <div><span>巡检人</span><b>${escapeHTML(a.lastInspector || "—")}</b></div>
      </div>
    </div>
    ${followup ? `<button type="button" class="asset-retake-btn" id="assetRetakeBtn">重新拍照复检</button>` : ""}
    ${a.lastSummary ? `
      <div class="asset-sum-card">
        <div class="ascard-h">最近总结</div>
        <div class="ascard-body ${sum.tone}">${escapeHTML(sum.text)}</div>
      </div>` : ""}
    <div class="section-head">体检记录 · 共 ${history.length} 次（只读）</div>
    ${historyHTML}
    ${histMoreBtn}
    ${reqHTML}
  `;

  // 重新拍照复检 → 带上下文跳首页拍照
  document.getElementById("assetRetakeBtn")?.addEventListener("click", retakeForAsset);
  // 照片点击 → lightbox
  $("#assetDetailBody").querySelectorAll("[data-history-photos]").forEach(strip => {
    strip.addEventListener("click", (e) => {
      const btn = e.target.closest(".photo-thumb");
      if (!btn) return;
      const recIdx = parseInt(btn.dataset.rec, 10);
      const imgIdx = parseInt(btn.dataset.img, 10) || 0;
      const rec = (state.currentAssetHistory || [])[recIdx];
      if (rec && rec.images) openLightbox(rec.images, imgIdx);
    });
  });
  // 查看更多（体检记录 / 我的申请）
  $("#assetDetailBody").querySelectorAll("[data-more]").forEach(btn => {
    btn.addEventListener("click", () => {
      if (btn.dataset.more === "history") state.assetHistoryExpanded = !state.assetHistoryExpanded;
      else state.assetReqExpanded = !state.assetReqExpanded;
      renderAssetDetail();
    });
  });
  // 撤回按钮
  $("#assetDetailBody").querySelectorAll("[data-withdraw]").forEach(btn => {
    btn.addEventListener("click", async () => {
      try {
        await API.withdrawChangeRequest(btn.dataset.withdraw);
        toast("已撤回");
        openAssetDetail(state.currentAsset.id);
      } catch (err) { toast("撤回失败：" + err.message); }
    });
  });
}

function crStatusText(s) {
  return { pending: "待审批", approved: "已通过", rejected: "已驳回", withdrawn: "已撤回" }[s] || s;
}
// 把 ID 转可读名：资产 → asset_name / 记录 → 用 ID 后段日期片段
function friendlyTargetLabel(cr) {
  if (cr.targetType === "asset") {
    const a = (state.assets || []).find(x => x.id === cr.targetId);
    if (a) return a.assetName || a.assetKey || cr.targetId;
    // 兜底从 id 切第三段
    const parts = String(cr.targetId).split("::");
    return parts[parts.length - 1] || cr.targetId;
  }
  if (cr.targetType === "record") {
    // 从 record_xxx_xxx 取倒数第二段（unix nano）转日期
    const m = String(cr.targetId).match(/_(\d{16,19})_/);
    if (m) {
      const d = new Date(Math.floor(Number(m[1]) / 1e6));
      if (!Number.isNaN(d.getTime())) {
        return `巡检 ${d.getMonth()+1}-${d.getDate()} ${String(d.getHours()).padStart(2,"0")}:${String(d.getMinutes()).padStart(2,"0")}`;
      }
    }
    return cr.targetId;
  }
  return cr.targetId;
}

function describePatch(cr) {
  const p = cr.patch || {};
  if (cr.targetType === "asset") {
    const parts = [];
    if (p.assetName) parts.push(`资产名 → ${p.assetName}`);
    if (p.lastStatus) parts.push(`状态 → ${p.lastStatus}`);
    if (p.lastSummary !== undefined) parts.push(`总结 → ${truncateStr(p.lastSummary, 24)}`);
    return parts.join(" · ") || "（无明细）";
  }
  if (cr.targetType === "record") {
    // 旧申请 patch 没存 label，从对应记录回查字段标签
    const rec = (state.currentAssetHistory || []).find(r => r.id === cr.targetId);
    const labelMap = {};
    (rec?.fields || []).forEach(f => { if (f.code) labelMap[f.code] = f.label; });
    const parts = [];
    if (Array.isArray(p.fields)) {
      for (const f of p.fields) parts.push(`${f.label || labelMap[f.code] || f.code} → ${truncateStr(f.value, 16)}`);
    }
    if (p.inspector) parts.push(`巡检人 → ${p.inspector}`);
    if (p.aiSummary !== undefined) parts.push(`AI 总结 → ${truncateStr(p.aiSummary, 24)}`);
    if (p.addImages && Array.isArray(p.addImages.imageIds)) {
      parts.push(`补交 ${p.addImages.imageIds.length} 张照片`);
    }
    return parts.join(" · ") || "（无明细）";
  }
  return "";
}
function truncateStr(s, n) { s = String(s ?? ""); return s.length > n ? s.slice(0, n) + "…" : s; }

// ===== 修改申请弹层 =====

function openChangeRequestSheet() {
  const a = state.currentAsset;
  if (!a) return;
  const history = state.currentAssetHistory || [];
  // 申请修改的本质=纠正这次巡检填错/AI 识别错的字段，所以默认对象=最近一次巡检记录（不是资产）。
  const sorted = [...history].sort((x, y) =>
    String(y.submittedAt || y.createdAt || "").localeCompare(String(x.submittedAt || x.createdAt || "")));
  const records = sorted.map(r => ({
    kind: "record",
    id: r.id,
    label: `巡检：${(r.submittedAt || r.createdAt || "").substring(0, 16).replace("T", " ")} · ${r.inspector || ""}`,
  }));
  // 记录在前、资产台账在后（资产是主管直接改状态的台账动作，降级）
  const targets = [...records, { kind: "asset", id: a.id, label: `资产台账：${a.assetName || a.id}` }];
  const lr = records.find(r => r.id === a.lastRecordId);
  const defaultSel = lr ? `record::${lr.id}` : (records[0] ? `record::${records[0].id}` : `asset::${a.id}`);
  $("#changeReqTarget").innerHTML = targets.map(t => {
    const val = `${t.kind}::${t.id}`;
    return `<option value="${escapeHTML(val)}"${val === defaultSel ? " selected" : ""}>${escapeHTML(t.label)}</option>`;
  }).join("");
  $("#changeReqReason").value = "";
  renderChangeReqFields();
  renderReasonChips();
  $("#changeReqSheet").hidden = false;
  // 有异常 → 自动定位到第一个待复核字段
  requestAnimationFrame(() => {
    const flag = document.querySelector("#changeReqFields .crf-flag");
    if (flag) flag.scrollIntoView({ block: "center", behavior: "smooth" });
  });
}

// 修改理由快选（能选不手打）：点一下填进理由框，可再手改
function renderReasonChips() {
  const box = document.getElementById("changeReqReasonChips");
  if (!box) return;
  const reasons = ["AI 识别有误", "现场已整改", "补拍补录", "误判，实际正常"];
  box.innerHTML = reasons.map(r => `<span class="cr-reason-chip" data-reason="${escapeHTML(r)}">${escapeHTML(r)}</span>`).join("");
  box.querySelectorAll("[data-reason]").forEach(chip => {
    chip.addEventListener("click", () => { $("#changeReqReason").value = chip.dataset.reason; });
  });
}
function closeChangeRequestSheet() {
  $("#changeReqSheet").hidden = true;
}
// asset.id 本身含 "::"，所以分隔符必须只 split 第一段
function splitTargetSel(sel) {
  const idx = (sel || "").indexOf("::");
  if (idx < 0) return ["", ""];
  return [sel.slice(0, idx), sel.slice(idx + 2)];
}

// 字段是否"待复核"(异常)——与台账/总结同一套是/否语义
function crfFieldIsBad(f) {
  const v = String(f.value || "").trim();
  if (!v) return false;
  if (["异常", "缺失", "破损", "故障"].includes(v)) return true;
  if (v === "否") return !isOccurrenceLabel(f.label);
  if (v === "是" || v === "有") return isOccurrenceLabel(f.label);
  return false;
}

// 申请修改里每个字段的输入控件——与"填写表单"同款：choice→下拉、number→数字、说明→文本域、其余→文本
function crfFieldControl(f) {
  const v = String(f.value || "");
  const code = escapeHTML(f.code);
  if (f.kind === "choice") {
    const opts = ['<option value="">请选择</option>'].concat(
      (f.options || []).map(o => `<option value="${escapeHTML(o)}" ${o === v ? "selected" : ""}>${escapeHTML(o)}</option>`));
    return `<select data-code="${code}">${opts.join("")}</select>`;
  }
  if (f.kind === "number") {
    return `<input data-code="${code}" type="number" step="any" value="${escapeHTML(v)}" placeholder="请输入数值" />`;
  }
  if (f.kind === "text" && /说明|备注|记录/.test(f.label)) {
    return `<textarea data-code="${code}">${escapeHTML(v)}</textarea>`;
  }
  return `<input data-code="${code}" type="text" value="${escapeHTML(v)}" placeholder="请输入" />`;
}

function renderChangeReqFields() {
  const sel = $("#changeReqTarget").value || "";
  const [kind, id] = splitTargetSel(sel);
  const box = $("#changeReqFields");
  if (kind === "asset") {
    const a = state.currentAsset;
    box.innerHTML = `
      <div class="kv-edit">
        <label>资产名称</label>
        <input type="text" id="crf_assetName" value="${escapeHTML(a.assetName || "")}" />
      </div>
      <div class="kv-edit">
        <label>当前状态</label>
        <div class="status-chips" id="crf_statusChips">
          ${ASSET_STATUSES.map(s => `<span class="chip ${s === a.lastStatus ? 'selected' : ''}" data-status="${s}">${s}</span>`).join("")}
        </div>
      </div>
      <div class="kv-edit kv-edit-full">
        <label>状态备注 / 最近总结</label>
        <textarea id="crf_lastSummary">${escapeHTML(a.lastSummary || "")}</textarea>
      </div>
    `;
    let selected = a.lastStatus;
    $("#crf_statusChips").querySelectorAll(".chip").forEach(c => {
      c.addEventListener("click", () => {
        $("#crf_statusChips").querySelectorAll(".chip").forEach(x => x.classList.remove("selected"));
        c.classList.add("selected");
        selected = c.dataset.status;
      });
    });
    box._collectPatch = () => {
      const patch = {};
      const newName = $("#crf_assetName").value.trim();
      const newSummary = $("#crf_lastSummary").value;
      if (newName && newName !== a.assetName) patch.assetName = newName;
      if (selected && selected !== a.lastStatus) patch.lastStatus = selected;
      if (newSummary !== (a.lastSummary || "")) patch.lastSummary = newSummary;
      return patch;
    };
  } else if (kind === "record") {
    const rec = state.currentAssetHistory.find(r => r.id === id);
    if (!rec) { box.innerHTML = ""; return; }
    const all = rec.fields || [];
    const bad = all.filter(crfFieldIsBad);
    const ok = all.filter(f => !crfFieldIsBad(f));
    const row = (f, flag) => `
      <div class="kv-edit ${flag ? "crf-flag" : ""}">
        <label>${escapeHTML(f.label)}${flag ? `<span class="crf-badge">待复核</span>` : ""}</label>
        ${crfFieldControl(f)}
      </div>`;
    box.innerHTML = `
      <div class="kv-edit-grid">
        ${bad.length ? `<div class="crf-sec warn">需复核 · ${bad.length} 项</div>${bad.map(f => row(f, true)).join("")}` : ""}
        ${ok.length ? `<div class="crf-sec">其余字段</div>${ok.map(f => row(f, false)).join("")}` : ""}
        <div class="kv-edit kv-edit-full">
          <label>补交照片 · 拍照（审批通过后并入本次巡检）</label>
          <input type="file" id="crf_photos" accept="image/*" capture="environment" multiple />
          <div class="crf-photo-hint" id="crf_photoHint"></div>
        </div>
      </div>
    `;
    $("#crf_photos").addEventListener("change", (e) => {
      const n = e.target.files?.length || 0;
      $("#crf_photoHint").textContent = n ? `已选 ${n} 张，提交时上传` : "";
    });
    box._collectPatch = () => {
      const patch = { fields: [] };
      box.querySelectorAll("[data-code]").forEach(el => {
        const code = el.dataset.code;
        const newVal = el.value;
        const orig = rec.fields.find(f => f.code === code);
        if (newVal !== String(orig?.value || "")) patch.fields.push({ code, label: orig?.label || code, value: newVal });
      });
      if (!patch.fields.length) delete patch.fields;
      return patch;
    };
    box._photoFiles = () => {
      const el = $("#crf_photos");
      return el && el.files ? Array.from(el.files) : [];
    };
  }
}
async function submitChangeRequest() {
  const sel = $("#changeReqTarget").value || "";
  const [kind, id] = splitTargetSel(sel);
  const reason = $("#changeReqReason").value.trim();
  if (!reason) { toast("请填写修改理由"); $("#changeReqReason").focus(); return; }
  const box = $("#changeReqFields");
  const patch = box._collectPatch ? box._collectPatch() : {};
  const photoFiles = box._photoFiles ? box._photoFiles() : [];
  if ((!patch || !Object.keys(patch).length) && photoFiles.length === 0) {
    toast("没有任何修改"); return;
  }
  // 先上传补交照片（如有）→ 拿到 tmpDir + imageIds 塞进 patch.addImages
  if (photoFiles.length > 0) {
    try {
      const up = await API.uploadDraftPhotos(photoFiles);
      patch.addImages = {
        tmpDir: up.tmpDir,
        imageIds: (up.files || []).map(f => f.id),
      };
    } catch (err) {
      toast("照片上传失败：" + err.message);
      return;
    }
  }
  try {
    await API.createChangeRequest({ targetType: kind, targetId: id, patch, reason });
    toast("已提交，等待主管审批");
    closeChangeRequestSheet();
    openAssetDetail(state.currentAsset.id);
  } catch (err) {
    toast("提交失败：" + err.message);
  }
}

// ===== 主管：审批队列 =====

async function openApprovalQueue() {
  setScene("approvals");
  $("#approvalsBody").innerHTML = `<div class="empty-tip">加载中…</div>`;
  try {
    const data = await API.listChangeRequests({ status: "pending" });
    state.approvalQueue = data.requests || [];
    renderApprovalQueue();
  } catch (err) {
    toast("加载失败：" + err.message);
  }
}
function renderApprovalQueue() {
  const list = state.approvalQueue || [];
  if (!list.length) {
    $("#approvalsBody").innerHTML = `<div class="empty-tip">当前无待审批申请</div>`;
    return;
  }
  $("#approvalsBody").innerHTML = list.map(c => {
    const targetLabel = friendlyTargetLabel(c);
    const targetType = c.targetType === "asset" ? "资产" : "巡检记录";
    return `
    <div class="cr-card" data-id="${escapeHTML(c.id)}">
      <div class="cr-row">
        <b>${escapeHTML(c.requestedBy || "")}</b>
        <span class="cr-when">${(c.requestedAt || "").substring(0,16).replace("T"," ")}</span>
      </div>
      <div class="cr-target">
        <span class="cr-tt">${targetType}</span>
        <span class="cr-tn">${escapeHTML(targetLabel)}</span>
        ${c.targetType === "asset"
          ? `<button type="button" class="cr-view" data-view-asset="${escapeHTML(c.targetId)}">查看</button>`
          : ""}
      </div>
      <div class="cr-patch">${escapeHTML(describePatch(c))}</div>
      <div class="cr-reason">理由：${escapeHTML(c.reason || "")}</div>
      <div class="cr-actions">
        <input type="text" class="cr-note" placeholder="审批意见（驳回必填）" />
        <button type="button" class="btn-ghost" data-reject="${escapeHTML(c.id)}">驳回</button>
        <button type="button" class="btn-primary" data-approve="${escapeHTML(c.id)}">通过</button>
      </div>
    </div>
    `;
  }).join("");
  $("#approvalsBody").querySelectorAll("[data-view-asset]").forEach(btn => {
    btn.addEventListener("click", () => openAssetDetail(btn.dataset.viewAsset));
  });
  $("#approvalsBody").querySelectorAll("[data-approve]").forEach(btn => {
    btn.addEventListener("click", async () => {
      const card = btn.closest(".cr-card");
      const note = card.querySelector(".cr-note").value.trim();
      try {
        await API.approveChangeRequest(btn.dataset.approve, note);
        toast("已通过");
        openApprovalQueue();
        refreshApprovalCount();
      } catch (err) { toast("失败：" + err.message); }
    });
  });
  $("#approvalsBody").querySelectorAll("[data-reject]").forEach(btn => {
    btn.addEventListener("click", async () => {
      const card = btn.closest(".cr-card");
      const note = card.querySelector(".cr-note").value.trim();
      if (!note) { toast("驳回时请填写理由"); return; }
      try {
        await API.rejectChangeRequest(btn.dataset.reject, note);
        toast("已驳回");
        openApprovalQueue();
        refreshApprovalCount();
      } catch (err) { toast("失败：" + err.message); }
    });
  });
}
async function refreshApprovalCount() {
  // 不再控制 hidden（由 setScene 统一管理：仅主管 + 仅台账页）
  if (!isSupervisor()) {
    state.pendingApprovalCount = 0;
    renderApprovalBadge();
    return;
  }
  try {
    const data = await API.listChangeRequests({ status: "pending" });
    state.pendingApprovalCount = (data.requests || []).length;
    renderApprovalBadge();
  } catch {
    state.pendingApprovalCount = 0;
    renderApprovalBadge();
  }
}
function renderApprovalBadge() {
  const btn = $("#topApprovals");
  const n = state.pendingApprovalCount || 0;
  btn.innerHTML = n > 0
    ? `审批 <span class="badge-dot">${n > 99 ? "99+" : n}</span>`
    : `审批`;
  btn.classList.toggle("has-pending", n > 0);
}

// ===== 修改申请弹层事件绑定 =====
$("#changeReqClose").addEventListener("click", closeChangeRequestSheet);
$("#changeReqCancel").addEventListener("click", closeChangeRequestSheet);
$("#changeReqSubmit").addEventListener("click", submitChangeRequest);
$("#changeReqTarget").addEventListener("change", renderChangeReqFields);
// 顶栏审批入口（仅 supervisor）
$("#topApprovals").addEventListener("click", openApprovalQueue);

// ===== Lightbox 事件绑定 =====
$("#lightboxClose").addEventListener("click", closeLightbox);
$("#lightboxPrev").addEventListener("click", (e) => { e.stopPropagation(); lightboxStep(-1); });
$("#lightboxNext").addEventListener("click", (e) => { e.stopPropagation(); lightboxStep(1); });
// 点遮罩（非图片本体、非按钮）关闭
$("#imageLightbox").addEventListener("click", (e) => {
  if (e.target.id === "imageLightbox" || e.target.id === "lightboxStage") closeLightbox();
});
// 阻止点图本身关闭
$("#lightboxImg").addEventListener("click", (e) => e.stopPropagation());
// ESC 关闭、← → 切换
document.addEventListener("keydown", (e) => {
  if ($("#imageLightbox").hidden) return;
  if (e.key === "Escape") { closeLightbox(); e.preventDefault(); }
  else if (e.key === "ArrowLeft") { lightboxStep(-1); e.preventDefault(); }
  else if (e.key === "ArrowRight") { lightboxStep(1); e.preventDefault(); }
});

// ===== 导航 =====

function goCamera() {
  clearInterval(state.pollTimer);
  hideRetakeModal();
  state.record = null;
  state.classifyResult = null;
  state.pendingImageIds = [];
  state.forceManual = false;
  state.retakePending = false;
  state.retakeTarget = null;   // 普通新建巡检:清掉残留的复检上下文
  localStorage.removeItem("activeRecord");
  setScene("camera");
  updateRetakeBanner();
}

// 异常资产「重新拍照复检」:跳首页拍照,并带上 模板/点位/编号 上下文,
// 使重拍提交后落到同一台资产、把异常闭环。
function retakeForAsset() {
  const a = state.currentAsset;
  if (!a) return;
  goCamera();                 // 先复位(会清 retakeTarget),再设目标
  state.retakeTarget = {
    templateId: a.templateId || "",
    pointId: a.pointId || "",
    assetNo: a.assetName || "",
    assetName: a.assetName || "设备",
  };
  updateRetakeBanner();
  toast(`复检：对准「${state.retakeTarget.assetName}」重新拍照即可`);
}

// 顶部复检横幅:retakeTarget 存在时显示,提示巡检员当前是在为某台设备复检
function updateRetakeBanner() {
  let el = document.getElementById("retakeBanner");
  const t = state.retakeTarget;
  if (!t) { if (el) el.hidden = true; return; }
  if (!el) {
    el = document.createElement("div");
    el.id = "retakeBanner";
    el.className = "retake-banner";
    el.innerHTML = `<span class="rb-text"></span><button type="button" class="rb-cancel">取消复检</button>`;
    document.body.appendChild(el);
    el.querySelector(".rb-cancel").addEventListener("click", () => { state.retakeTarget = null; updateRetakeBanner(); toast("已取消复检"); });
  }
  el.querySelector(".rb-text").textContent = `复检中：${t.assetName}（编号已带入,重拍后自动更新这台设备）`;
  el.hidden = false;
}

$("#backBtn").addEventListener("click", () => {
  switch (state.scene) {
    case "tasks": setScene("camera"); break;
    case "loading": setScene("camera"); break;
    case "classify": setScene("camera"); break;
    case "form": setScene("camera"); break;
    case "preview": setScene("form"); break;
    case "ledger": setScene("camera"); break;
    case "asset": setScene("ledger"); break;
    case "approvals": setScene("ledger"); break;
    default: setScene("camera");
  }
});

function startManualPick() {
  state.classifyResult = { needsManualPick: true, templateId: "unknown" };
  state.pendingImageIds = [];
  setScene("classify");
  showClassifyResult();
}
$("#manualPickBtn").addEventListener("click", startManualPick);
const boardManualBtn = document.getElementById("boardManualBtn");
if (boardManualBtn) boardManualBtn.addEventListener("click", startManualPick);

// ===== 我的任务（工程任务闭环入口） =====

function setActiveTask(task) {
  state.activeEngineeringTask = task || null;
  state.activeEngineeringTaskId = task ? task.id : null;
  if (task) localStorage.setItem(ACTIVE_TASK_KEY, JSON.stringify(task));
  else localStorage.removeItem(ACTIVE_TASK_KEY);
  renderTaskBanner();
}

function formatTaskDate(s) {
  if (!s) return "";
  return String(s).slice(0, 10);
}

function taskStatusClass(status) {
  switch (status) {
    case "已完成": return "done";
    case "进行中": return "doing";
    case "逾期": return "overdue";
    case "已取消": return "canceled";
    default: return "pending";
  }
}

// 我的待办：未关闭(非已完成/已取消)，且指派给我或暂未指派；
// 若没有任何匹配，则回退展示全部未关闭任务，避免空列表。
function myOpenTasks() {
  const me = state.userName || state.inspector;
  // 只有「已下发」的任务才到巡检员手机：进行中(下发即进行中) / 待整改 / 逾期。
  // 待执行/待下发=管理员还没下发，移动端不显示。
  const open = (state.engineeringTasks || []).filter(t => ["进行中", "待整改", "逾期"].includes(t.status));
  const mine = open.filter(t => !t.assigneeName || t.assigneeName === me);
  return mine.length ? mine : open;
}

async function goTasks() {
  setScene("tasks");
  const list = $("#taskList");
  list.innerHTML = `<div class="task-loading">任务加载中…</div>`;
  try {
    const data = await API.engineeringTasks();
    state.engineeringTasks = data.tasks || [];
  } catch (err) {
    toast(err.message);
    state.engineeringTasks = [];
  }
  renderTasks();
}

const TASK_STATUS_ORDER = { "逾期": 0, "待执行": 1, "进行中": 2 };

function renderTasks() {
  const list = $("#taskList");
  const tasks = myOpenTasks();
  if (!tasks.length) {
    list.innerHTML = `<div class="task-empty">暂无待办巡检任务</div>`;
    return;
  }
  // 逾期最先、再待执行、再进行中；同状态按截止时间近的在前
  const sorted = [...tasks].sort((a, b) =>
    (TASK_STATUS_ORDER[a.status] ?? 3) - (TASK_STATUS_ORDER[b.status] ?? 3) ||
    String(a.dueAt || "9999").localeCompare(String(b.dueAt || "9999")));
  const counts = { pending: 0, doing: 0, overdue: 0 };
  sorted.forEach(t => {
    if (t.status === "逾期") counts.overdue++;
    else if (t.status === "进行中") counts.doing++;
    else counts.pending++;
  });
  const summary = `
    <div class="task-summary">
      <div class="ts-item"><b>${counts.pending}</b><span>待执行</span></div>
      <div class="ts-item"><b>${counts.doing}</b><span>进行中</span></div>
      <div class="ts-item${counts.overdue ? " warn" : ""}"><b>${counts.overdue}</b><span>逾期</span></div>
    </div>`;
  list.innerHTML = summary + sorted.map(t => {
    const title = t.title || t.workContent || "巡检任务";
    const showDesc = t.workContent && !title.includes(t.workContent);
    const overdue = t.status === "逾期";
    return `
    <article class="task-card">
      <div class="task-card-head">
        <span class="task-status task-status-${taskStatusClass(t.status)}">${escapeHTML(t.status || "待执行")}</span>
        ${t.dueAt ? `<span class="task-due${overdue ? " overdue" : ""}">截止 ${escapeHTML(formatTaskDate(t.dueAt))}</span>` : ""}
      </div>
      <h3 class="task-title">${escapeHTML(title)}</h3>
      <div class="task-meta">${escapeHTML(t.project || "未填项目")}${t.category ? " · " + escapeHTML(t.category) : ""}</div>
      ${showDesc ? `<p class="task-desc">${escapeHTML(t.workContent)}</p>` : ""}
      <button class="task-start" data-task-start="${escapeHTML(t.id)}">开始巡检</button>
    </article>`;
  }).join("");
  list.querySelectorAll("[data-task-start]").forEach(btn => {
    btn.addEventListener("click", () => startTaskExecution(btn.dataset.taskStart));
  });
}

async function startTaskExecution(taskId) {
  const task = (state.engineeringTasks || []).find(t => t.id === taskId);
  if (!task) { toast("任务不存在或已变更"); return; }
  setActiveTask(task);
  // 乐观地把任务置为「进行中」，失败不阻断巡检（提交时后端仍会自动闭环）
  if (task.status === "待执行" || task.status === "逾期") {
    try {
      await API.updateEngineeringTaskStatus(taskId, { status: "进行中" });
      task.status = "进行中";
      setActiveTask(task);
    } catch (_) { /* 容忍 */ }
  }
  goCamera();
  toast("已关联任务，拍照即开始巡检");
}

function renderTaskBanner() {
  const banner = $("#taskBanner");
  if (!banner) return;
  const t = state.activeEngineeringTask;
  const hasTask = Boolean(state.activeEngineeringTaskId && t);
  // 有任务时给相机页打标记：隐藏品牌标牌与拍照说明，让位给任务+取景框
  document.getElementById("sceneCamera")?.classList.toggle("task-active", hasTask);
  if (hasTask) {
    banner.hidden = false;
    banner.innerHTML = `
      <span class="tb-dot" aria-hidden="true"></span>
      <div class="tb-main">
        <span class="tb-label">正在执行任务</span>
        <span class="tb-title">${escapeHTML(t.title || t.workContent || "巡检任务")}</span>
      </div>
      <button class="tb-clear" type="button" id="taskBannerClear">取消关联</button>`;
    const clearBtn = document.getElementById("taskBannerClear");
    if (clearBtn) clearBtn.addEventListener("click", () => { setActiveTask(null); toast("已取消任务关联"); });
  } else {
    banner.hidden = true;
    banner.innerHTML = "";
  }
}

$("#myTasksBtn")?.addEventListener("click", goTasks);

// ===== 启动 =====

async function init() {
  hideRetakeModal();
  // 优先校验登录态：未登录直接走登录页，不去拉接口免得 401 弹 prompt
  const me = await fetchMe();
  if (!isRealUser(me)) {
    setScene("login");
    return;
  }
  applyUser(me);

  // 根据 role 显示/隐藏审批入口（移动端永远 inspector）
  refreshApprovalCount();
  try {
    const [t, p] = await Promise.all([API.templates(), API.points()]);
    state.templates = t.templates || [];
    state.points = p.points || [];
  } catch (err) {
    toast("初始化失败：" + err.message);
  }

  // 恢复未完成的记录（如果浏览器刷新）
  const activeID = localStorage.getItem("activeRecord");
  if (activeID) {
    try {
      state.record = await API.getRecord(activeID);
      if (state.record.submitted) {
        localStorage.removeItem("activeRecord");
        setScene("camera");
      } else if (state.record.recognitionStatus === "recognized" || state.record.manualRequired) {
        await renderForm();
        setScene("form");
      } else if (state.record.recognitionStatus === "retake_required") {
        state.retakePending = true;
        setScene("camera");
        showRetakeModal(state.record.retakeReason || "识别不稳定，请重拍");
      } else if (state.record.recognitionStatus === "processing") {
        setScene("loading");
        startLoadingScript("analyze");
        $("#loadingSub").textContent = "正在恢复识别任务";
        pollTask();
      } else {
        localStorage.removeItem("activeRecord");
        state.record = null;
        setScene("camera");
      }
      return;
    } catch {
      localStorage.removeItem("activeRecord");
    }
  }
  setScene("camera");
}

init();
