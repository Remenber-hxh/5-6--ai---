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
};

const state = {
  templates: [],
  points: [],
  assets: [],
  assetSummary: null,
  scene: "camera",
  classifyResult: null,    // { templateId, confidence, ..., tmpDir }
  pendingImageIds: [],
  record: null,
  pollTimer: null,
  forceManual: false,
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
function isSupervisor() { return state.userRole === "supervisor"; }

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

const PROGRESS = { login: 0, camera: 0, loading: 15, classify: 30, form: 60, preview: 85, ledger: 100, asset: 100, approvals: 100 };
const TITLES = {
  login: "登录",
  camera: "智巡",
  loading: "AI 识别中",
  classify: "确认场景",
  form: "确认日报字段",
  preview: "提交日报",
  ledger: "资产台账",
  asset: "资产详情",
  approvals: "待审批",
};

function setScene(name) {
  if (name === "camera") hideRetakeModal();
  state.scene = name;
  const appRoot = $("#app");
  if (appRoot) appRoot.dataset.scene = name;
  $$(".scene").forEach(el => el.classList.remove("active"));
  document.getElementById("scene" + cap(name))?.classList.add("active");
  $("#pageTitle").textContent = TITLES[name] || "智巡";

  const showProgress = !["camera", "ledger", "asset", "approvals"].includes(name);
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
    $("#topAction").textContent = "台账";
    $("#topAction").onclick = () => goLedger();
  } else if (name === "camera") {
    $("#topAction").hidden = true;
  } else {
    $("#topAction").hidden = true;
  }

  // 审批入口：仅主管 + 仅在台账页显示
  $("#topApprovals").hidden = !(isSupervisor() && name === "ledger");

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
    case "loading":
      footer.hidden = true;
      break;
    case "classify": {
      const r = state.classifyResult;
      if (r && !r.needsManualPick && r.templateId !== "unknown") {
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
      primary.textContent = state.record?.submitted ? "查看台账" : "提交日报";
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
      secondary.textContent = "返回台账";
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
    await classifyAndProceed(files);
  });
}
bindFilePicker("#cameraInput");
bindFilePicker("#uploadInput");

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
  $("#loadingMsg").textContent = "AI 正在识别场景…";
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
    const body = {
      templateId,
      inspector: state.inspector,
    };
    if (state.classifyResult?.tmpDir) {
      body.tmpDir = state.classifyResult.tmpDir;
      body.imageIds = state.pendingImageIds;
    }
    state.record = await API.createRecord(body);
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
  $("#loadingMsg").textContent = "AI 正在识别字段…";
  $("#loadingSub").textContent = `分析 ${state.record.images.length} 张图片，约需 5-15 秒`;
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
        await renderForm();
        setScene("form");
        toast("识别完成，请逐项确认");
      } else if (task.status === "failed") {
        clearInterval(state.pollTimer);
        state.record = await API.getRecord(state.record.id);
        if (state.record.manualRequired) {
          await renderForm();
          setScene("form");
          showManualHint();
          toast("AI 多次未稳定识别，已转人工填写");
        } else {
          showRetakeModal(task.errorMessage || state.record.retakeReason || "识别不稳定，请重拍");
        }
      } else {
        $("#loadingSub").textContent = `任务进度 ${task.progress?.processed || 0}/${task.progress?.total || 0}`;
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
  setScene("camera");
});
$("#retakeManualBtn").addEventListener("click", async () => {
  hideRetakeModal();
  await setManualMode();
  if (state.record?.id) showManualHint();
});

function showManualHint() {
  $("#manualHint").hidden = false;
}

// ===== 表单（屏 4） =====

async function renderForm() {
  const rec = state.record;
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
  // P0-4 防惰性闭环:统计还没人工 patch 过的 AI 字段。提交接口对这些字段会拦截。
  const unconfirmedCount = rec.fields.filter(f => f.source === "ai" && String(f.value || "").trim() !== "").length;
  const confirmAllBanner = unconfirmedCount > 0 ? `
    <div class="confirm-all-banner" id="confirmAllBanner">
      <div class="cab-msg">
        <b>${unconfirmedCount}</b> 个 AI 识别字段还未确认 · 请逐项核对,或一键确认正常字段(会留痕)
      </div>
      <button type="button" id="confirmAllBtn">一键确认正常 (${unconfirmedCount})</button>
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
  // 「我无法判定」按钮 → 标记此字段为 uncertain，等主管复核
  groups.querySelectorAll("[data-mark-uncertain]").forEach(btn => {
    btn.addEventListener("click", () => markUncertain(btn.dataset.markUncertain));
  });
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
        <button class="mark-uncertain" type="button" data-mark-uncertain="${escapeHTML(field.code)}">我无法判定</button>
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
      <button class="mark-uncertain" type="button" data-mark-uncertain="${escapeHTML(field.code)}">我无法判定</button>
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
  if (f.source === "ai") return "AI 识别";
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
    f.source === "ai" && String(f.value || "").trim() !== ""
  );
  if (aiFields.length === 0) {
    toast("没有需要确认的 AI 字段");
    return;
  }
  if (!confirm(`一键确认 ${aiFields.length} 个 AI 字段为「正常」?\n所有动作会留痕,主管可追溯。`)) return;
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

// §4 mark_uncertain：人工无法判定，保留待复核交主管抽查；后端会写一条 uncertain 留痕。
async function markUncertain(code) {
  if (!state.record) return;
  const field = state.record.fields.find(f => f.code === code);
  if (!field) return;
  const el = document.querySelector(`[data-field-input][data-field-code="${CSS.escape(code)}"]`);
  const currentValue = el ? el.value : (field.value || "");
  const opts = {
    action: "uncertain",
    durationMs: popFieldDurationMs(code),
    viewedPhoto: FieldAudit.viewedPhoto,
  };
  try {
    const updated = await API.patchField(state.record.id, code, currentValue, field.version, opts);
    Object.assign(field, updated);
    FieldAudit.viewedPhoto = false;
    toast(`${field.label} 已标记为待主管复核`);
    const fieldEl = el ? el.closest(".field") : null;
    if (fieldEl) {
      const pill = fieldEl.querySelector(".ai-pill");
      if (pill) { pill.className = "ai-pill warn"; pill.textContent = "待主管复核"; }
    }
  } catch (err) {
    toast(err.message);
  }
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
    $("#summaryBody").textContent = rec.aiSummary;
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
      $("#recosList").innerHTML = rec.aiRecommendations.map(r => `
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

async function submitRecord() {
  $("#primaryBtn").disabled = true;
  $("#primaryBtn").textContent = "AI 总结生成中…";
  try {
    state.record = await API.submit(state.record.id);
    renderPreview();
    setScene("preview");
    toast("提交成功，已更新台账");
    localStorage.removeItem("activeRecord");
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
    risk = (summary.warning || 0) + (summary.danger || 0) + (summary.repair || 0) + (summary.unknown || 0);
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

function renderAssets() {
  const list = $("#assetList");
  if (!state.assets.length) {
    list.innerHTML = `<div class="empty-tip">暂无资产记录<br>提交一条巡检后会自动建立台账</div>`;
    return;
  }
  const filtered = applyAssetFilter(state.assets);
  if (!filtered.length) {
    list.innerHTML = `<div class="empty-tip">没有符合当前筛选的资产<br>点击上方「资产总数」清空筛选</div>`;
    return;
  }
  list.innerHTML = filtered.map(a => {
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
          ${a.lastSummary ? `<div class="summary">${escapeHTML(a.lastSummary)}</div>` : ""}
        </div>
        <div class="asset-side">
          <div class="status ${escapeHTML(a.lastStatus || '')}">${escapeHTML(a.lastStatus || '未巡检')}</div>
          <div class="count-badge">${a.inspectionCount} 次</div>
        </div>
      </div>
    `;
  }).join("");
  // 行点击 → 资产详情页（只读）
  list.querySelectorAll(".asset-row").forEach(row => {
    row.addEventListener("click", () => openAssetDetail(row.dataset.id));
  });
}

// ===== 资产详情（只读 scene） =====

const ASSET_STATUSES = ["正常", "异常", "待复核", "待维修"];

async function openAssetDetail(assetId) {
  setScene("asset");
  $("#assetDetailBody").innerHTML = `<div class="empty-tip">加载中…</div>`;
  try {
    const data = await API.getAsset(assetId);
    state.currentAsset = data.asset;
    state.currentAssetHistory = data.history || [];
    // 仅主管视角拉申请历史；普通用户不展示「审批」相关字段
    if (isSupervisor()) {
      const my = await API.listChangeRequests({ mine: "1" });
      state.currentAssetRequests = (my.requests || []).filter(r =>
        (r.targetType === "asset" && r.targetId === assetId) ||
        (r.targetType === "record" && state.currentAssetHistory.some(h => h.id === r.targetId))
      );
    } else {
      state.currentAssetRequests = [];
    }
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

  const historyHTML = history.length ? history.map((r, i) => {
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

  const reqHTML = myReqs.length ? `
    <div class="section-head">我的修改申请</div>
    <div class="cr-list">
      ${myReqs.map(c => `
        <div class="cr-item cr-${escapeHTML(c.status)}">
          <div class="cr-row">
            <span class="cr-status">${crStatusText(c.status)}</span>
            <span class="cr-when">${(c.requestedAt || "").substring(0,16).replace("T"," ")}</span>
          </div>
          <div class="cr-patch">${escapeHTML(describePatch(c))}</div>
          <div class="cr-reason">理由：${escapeHTML(c.reason || "")}</div>
          ${c.reviewNote ? `<div class="cr-note">主管：${escapeHTML(c.reviewNote)}</div>` : ""}
          ${c.status === "pending" ? `<div class="cr-actions"><button type="button" class="btn-ghost" data-withdraw="${escapeHTML(c.id)}">撤回</button></div>` : ""}
        </div>
      `).join("")}
    </div>
  ` : "";

  $("#assetDetailBody").innerHTML = `
    <div class="asset-meta">
      <div class="row"><span>资产名称</span><b>${escapeHTML(a.assetName || "")}</b></div>
      <div class="row"><span>项目</span><b>${escapeHTML(a.project || "")}</b></div>
      <div class="row"><span>类型</span><b>${escapeHTML(a.assetType || "")}</b></div>
      <div class="row"><span>当前状态</span><b class="status ${escapeHTML(a.lastStatus || '')}">${escapeHTML(a.lastStatus || '未巡检')}</b></div>
      <div class="row"><span>累计巡检</span><b>${a.inspectionCount} 次</b></div>
      <div class="row"><span>最近巡检</span><b>${(a.lastInspectedAt || "").substring(0,16).replace("T"," ")}</b></div>
      ${a.lastInspector ? `<div class="row"><span>最近巡检人</span><b>${escapeHTML(a.lastInspector)}</b></div>` : ""}
      ${a.lastSummary ? `<div class="row col"><span>最近总结</span><div class="multi">${escapeHTML(a.lastSummary)}</div></div>` : ""}
    </div>
    <div class="section-head">巡检记录 · 共 ${history.length} 次（只读）</div>
    ${historyHTML}
    ${reqHTML}
  `;

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
    const parts = [];
    if (Array.isArray(p.fields)) {
      for (const f of p.fields) parts.push(`${f.code} → ${truncateStr(f.value, 16)}`);
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
  // 构造目标下拉：1 个 asset + N 条 record
  const targets = [
    { kind: "asset", id: a.id, label: `资产：${a.assetName || a.id}` },
    ...history.map(r => ({
      kind: "record",
      id: r.id,
      label: `巡检：${(r.submittedAt || r.createdAt || "").substring(0,16).replace("T"," ")} · ${r.inspector || ""}`,
    }))
  ];
  $("#changeReqTarget").innerHTML = targets.map((t, i) => `
    <option value="${t.kind}::${escapeHTML(t.id)}"${i === 0 ? " selected" : ""}>${escapeHTML(t.label)}</option>
  `).join("");
  $("#changeReqReason").value = "";
  renderChangeReqFields();
  $("#changeReqSheet").hidden = false;
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
    const fields = (rec.fields || []).map(f => `
      <div class="kv-edit">
        <label>${escapeHTML(f.label)}</label>
        ${f.kind === "longtext" || (f.value || "").length > 20
          ? `<textarea data-code="${escapeHTML(f.code)}">${escapeHTML(String(f.value || ""))}</textarea>`
          : `<input type="text" data-code="${escapeHTML(f.code)}" value="${escapeHTML(String(f.value || ""))}" />`}
      </div>
    `).join("");
    box.innerHTML = `
      <div class="kv-edit-grid">
        <div class="kv-edit">
          <label>巡检人</label>
          <input type="text" id="crf_inspector" value="${escapeHTML(rec.inspector || "")}" />
        </div>
        ${fields}
        <div class="kv-edit kv-edit-full">
          <label>AI 总结（人工覆盖）</label>
          <textarea id="crf_aiSummary">${escapeHTML(rec.aiSummary || "")}</textarea>
        </div>
        <div class="kv-edit kv-edit-full">
          <label>补交照片（审批通过后并入本次巡检）</label>
          <input type="file" id="crf_photos" accept="image/*" multiple />
          <div class="crf-photo-hint" id="crf_photoHint"></div>
        </div>
      </div>
    `;
    // 文件选择反馈
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
        if (newVal !== String(orig?.value || "")) patch.fields.push({ code, value: newVal });
      });
      if (!patch.fields.length) delete patch.fields;
      const ins = $("#crf_inspector").value.trim();
      if (ins && ins !== rec.inspector) patch.inspector = ins;
      const sm = $("#crf_aiSummary").value;
      if (sm !== (rec.aiSummary || "")) patch.aiSummary = sm;
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
  localStorage.removeItem("activeRecord");
  setScene("camera");
}

$("#backBtn").addEventListener("click", () => {
  switch (state.scene) {
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
