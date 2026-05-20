const state = {
  points: [],
  templates: [],
  assets: [],
  selectedPointId: null,
  selectedFiles: [],
  record: null,
  task: null,
  pollTimer: null,
};

const $ = (selector) => document.querySelector(selector);
const $$ = (selector) => Array.from(document.querySelectorAll(selector));

const stageIds = {
  setup: "#stage-setup",
  photo: "#stage-photo",
  review: "#stage-review",
  submit: "#stage-submit",
  ledger: "#stage-ledger",
};

function escapeHTML(value = "") {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

function toast(message) {
  const el = $("#toast");
  el.textContent = message;
  el.classList.add("show");
  clearTimeout(el._timer);
  el._timer = setTimeout(() => el.classList.remove("show"), 2400);
}

async function api(path, options = {}) {
  const res = await fetch(path, options);
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    throw new Error(data.message || data.error || "请求失败");
  }
  return data;
}

function setStage(name) {
  $$(".stage").forEach((stage) => stage.classList.remove("active"));
  $$(".step").forEach((step) => step.classList.remove("active"));
  $(stageIds[name])?.classList.add("active");
  $(`.step[data-stage="${name}"]`)?.classList.add("active");
}

function statusText(status) {
  return (
    {
      not_started: "未开始",
      processing: "识别中",
      recognized: "已识别，待确认",
      retake_required: "需要重新拍照",
      manual_required: "人工填写",
      queued: "排队中",
      succeeded: "识别完成",
      failed: "识别失败",
      superseded: "已被新任务替代",
      normal: "正常",
      abnormal: "异常",
      review: "待复核",
      unknown: "未识别",
    }[status] || status || "-"
  );
}

function confidenceText(value) {
  if (!value) return "未识别";
  return `${Math.round(value * 100)}%`;
}

function pushTimeline(title, detail) {
  const entry = document.createElement("div");
  entry.className = "timeline-entry";
  entry.innerHTML = `<strong>${escapeHTML(title)}</strong><span>${escapeHTML(detail)}</span>`;
  $("#timeline").prepend(entry);
}

function selectedPoint() {
  return state.points.find((point) => point.id === state.selectedPointId);
}

function templateName(id) {
  return state.templates.find((tpl) => tpl.id === id)?.name || id || "-";
}

function renderPoints() {
  const wrap = $("#pointList");
  wrap.innerHTML = "";
  state.points.forEach((point) => {
    const button = document.createElement("button");
    button.className = `point-option ${state.selectedPointId === point.id ? "selected" : ""}`;
    button.type = "button";
    button.innerHTML = `
      <strong>${escapeHTML(point.name)}</strong>
      <span>${escapeHTML(point.project)} / ${escapeHTML(point.location)}</span>
      <small>${escapeHTML(templateName(point.templateId))}</small>
    `;
    button.addEventListener("click", () => {
      state.selectedPointId = point.id;
      renderPoints();
    });
    wrap.appendChild(button);
  });
}

function renderMeta() {
  const rec = state.record;
  const values = rec
    ? [
        rec.id,
        rec.templateName,
        statusText(rec.recognitionStatus),
        `${rec.captureAttempts || 0} / 3`,
      ]
    : ["-", "-", "-", "-"];
  $("#metaList").querySelectorAll("dd").forEach((dd, index) => {
    dd.textContent = values[index];
  });
}

function renderRecordCard() {
  const rec = state.record;
  const card = $("#recordCard");
  if (!rec) {
    card.innerHTML = `<p>请先创建巡检记录。</p>`;
    return;
  }
  card.innerHTML = `
    <div>
      <small>当前记录</small>
      <strong>${escapeHTML(rec.pointName)}</strong>
      <span>${escapeHTML(rec.templateName)}</span>
    </div>
    <div>
      <small>识别状态</small>
      <strong>${escapeHTML(statusText(rec.recognitionStatus))}</strong>
      <span>${rec.captureAttempts || 0} / 3 次</span>
    </div>
  `;
}

function renderPreviews() {
  const wrap = $("#previewGrid");
  wrap.innerHTML = "";
  state.selectedFiles.slice(0, 3).forEach((file) => {
    const item = document.createElement("div");
    item.className = "preview";
    const img = document.createElement("img");
    img.src = URL.createObjectURL(file);
    img.alt = file.name;
    const caption = document.createElement("span");
    caption.textContent = file.name;
    item.append(img, caption);
    wrap.appendChild(item);
  });
}

function renderImages() {
  const wrap = $("#imageList");
  const images = state.record?.images || [];
  if (!images.length) {
    wrap.innerHTML = `<p class="empty">尚未上传照片。</p>`;
    return;
  }
  wrap.innerHTML = images
    .map((image, index) => `<span>已上传 ${index + 1}：${escapeHTML(image.fileName)}</span>`)
    .join("");
}

function renderAnalysisCard() {
  const rec = state.record;
  const task = state.task;
  const card = $("#analysisCard");
  if (!rec) {
    card.innerHTML = `<p>暂无识别结果。</p>`;
    return;
  }

  const model = task?.analysis?.model
    ? `${task.analysis.model.provider} / ${task.analysis.model.name}`
    : "等待识别";
  const warnings = task?.analysis?.warnings?.length
    ? `<div class="warning-line">${task.analysis.warnings.map(escapeHTML).join("；")}</div>`
    : "";

  card.innerHTML = `
    <div>
      <small>识别状态</small>
      <strong>${escapeHTML(statusText(rec.recognitionStatus))}</strong>
    </div>
    <div>
      <small>模型来源</small>
      <strong>${escapeHTML(model)}</strong>
    </div>
    <div>
      <small>处理进度</small>
      <strong>${task?.progress?.processed || 0} / ${task?.progress?.total || rec.images?.length || 0}</strong>
    </div>
    ${warnings}
  `;
}

function fieldControl(field) {
  const value = escapeHTML(field.value || "");
  if (field.kind === "choice") {
    const options = [`<option value="">请选择</option>`]
      .concat(
        (field.options || []).map((option) => {
          const selected = option === field.value ? "selected" : "";
          return `<option value="${escapeHTML(option)}" ${selected}>${escapeHTML(option)}</option>`;
        }),
      )
      .join("");
    return `<select data-field-input>${options}</select>`;
  }
  if (field.kind === "number") {
    return `<input data-field-input type="number" step="any" value="${value}" placeholder="请输入或确认读数" />`;
  }
  if (field.label.includes("说明") || field.label.includes("记录") || field.label.includes("进度")) {
    return `<textarea data-field-input rows="3" placeholder="请输入内容">${value}</textarea>`;
  }
  return `<input data-field-input value="${value}" placeholder="请输入或确认内容" />`;
}

function renderFieldList() {
  const wrap = $("#fieldList");
  const fields = state.record?.fields || [];
  if (!fields.length) {
    wrap.innerHTML = `<p class="empty">还没有日报字段，请先创建记录。</p>`;
    return;
  }
  wrap.innerHTML = fields
    .map((field) => {
      const reviewClass = field.needsReview ? "needs-review" : "confirmed";
      const reviewText = field.needsReview ? "待确认" : "已确认";
      const aiLine = field.aiValue
        ? `<small>AI识别：${escapeHTML(field.aiValue)} / 置信度 ${confidenceText(field.confidence)}</small>`
        : `<small>来源：${escapeHTML(field.source || "manual")} / ${confidenceText(field.confidence)}</small>`;
      return `
        <article class="field-card ${reviewClass}" data-code="${escapeHTML(field.code)}" data-version="${field.version || 1}">
          <div class="field-head">
            <div>
              <strong>${escapeHTML(field.label)}</strong>
              ${aiLine}
            </div>
            <span>${reviewText}</span>
          </div>
          ${fieldControl(field)}
          <button class="field-confirm" type="button" data-confirm-field>确认此字段</button>
        </article>
      `;
    })
    .join("");
}

function renderReport() {
  const rec = state.record;
  $("#reportText").value = rec?.report || "";
  $("#summaryCard").innerHTML = rec?.aiSummary
    ? `<strong>AI总结</strong><p>${escapeHTML(rec.aiSummary)}</p>`
    : `<p>提交后会基于人工确认字段生成AI总结。</p>`;
}

function renderAssets() {
  const wrap = $("#assetList");
  if (!state.assets.length) {
    wrap.innerHTML = `<p class="empty">暂无资产台账。提交一条巡检记录后会自动生成。</p>`;
    return;
  }
  wrap.innerHTML = state.assets
    .map(
      (asset) => `
        <article class="asset-card">
          <div>
            <strong>${escapeHTML(asset.assetName)}</strong>
            <span>${escapeHTML(asset.project)} / ${escapeHTML(asset.assetType)}</span>
          </div>
          <em class="${asset.status === "异常" ? "danger" : ""}">${escapeHTML(asset.status)}</em>
          <p>${escapeHTML(asset.summary || "暂无总结")}</p>
        </article>
      `,
    )
    .join("");
}

function renderAll() {
  renderMeta();
  renderRecordCard();
  renderImages();
  renderAnalysisCard();
  renderFieldList();
  renderReport();
  renderAssets();
}

async function refreshRecord() {
  if (!state.record) return;
  state.record = await api(`/api/inspection/records/${state.record.id}`);
  localStorage.setItem("inspectai.activeRecord", state.record.id);
  renderAll();
}

async function refreshAssets() {
  const data = await api("/api/assets");
  state.assets = data.assets || [];
  renderAssets();
}

function requiredFieldsReady() {
  const fields = state.record?.fields || [];
  const missing = fields.filter((field) => field.required && !String(field.value || "").trim());
  const unconfirmed = fields.filter((field) => field.required && field.needsReview);
  if (missing.length) {
    toast(`仍有必填字段未填写：${missing[0].label}`);
    return false;
  }
  if (unconfirmed.length) {
    toast(`仍有字段未确认：${unconfirmed[0].label}`);
    return false;
  }
  return true;
}

function showRetakeModal(reason) {
  const attempts = state.record?.captureAttempts || 0;
  $("#retakeReason").textContent = reason || "图片无法稳定识别，请重新拍摄。";
  $("#attemptLine").textContent = `已尝试 ${attempts} / 3 次`;
  $("#retakeModal").classList.add("show");
  $("#retakeModal").setAttribute("aria-hidden", "false");
}

function hideRetakeModal() {
  $("#retakeModal").classList.remove("show");
  $("#retakeModal").setAttribute("aria-hidden", "true");
}

async function startAnalysis() {
  const task = await api(`/api/inspection/records/${state.record.id}/ai-tasks`, { method: "POST" });
  state.task = task;
  renderAll();
  pushTimeline("AI识别已发起", task.id);
  setStage("review");

  clearInterval(state.pollTimer);
  state.pollTimer = setInterval(pollLatestTask, 900);
}

async function pollLatestTask() {
  if (!state.record) return;
  try {
    const task = await api(`/api/inspection/records/${state.record.id}/ai/latest`);
    state.task = task;
    renderAll();

    if (task.status === "succeeded") {
      clearInterval(state.pollTimer);
      await refreshRecord();
      pushTimeline("识别完成", "已生成日报字段，等待人工逐项确认");
      toast("识别完成，请确认字段");
    }

    if (task.status === "failed") {
      clearInterval(state.pollTimer);
      await refreshRecord();
      if (task.errorCode === "manual_required" || state.record.manualRequired) {
        pushTimeline("进入人工填写", task.errorMessage || "已达到重拍上限");
        toast("已进入人工填写");
        setStage("review");
      } else {
        pushTimeline("需要重新拍照", task.errorMessage || "识别结果不稳定");
        showRetakeModal(task.errorMessage);
      }
    }
  } catch (err) {
    clearInterval(state.pollTimer);
    toast(err.message);
  }
}

async function saveFieldCard(card) {
  const input = card.querySelector("[data-field-input]");
  const code = card.dataset.code;
  const version = Number(card.dataset.version || 1);
  await api(`/api/inspection/records/${state.record.id}/fields/${encodeURIComponent(code)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value: input.value, version }),
  });
}

async function enterManualMode() {
  if (!state.record) {
    toast("请先创建巡检记录");
    return;
  }
  state.record = await api(`/api/inspection/records/${state.record.id}/manual`, { method: "POST" });
  hideRetakeModal();
  renderAll();
  setStage("review");
  pushTimeline("人工填写模式", "用户主动切换，AI不再覆盖人工字段");
}

function idempotencyKey() {
  return crypto.randomUUID ? crypto.randomUUID() : `key_${Date.now()}_${Math.random()}`;
}

async function init() {
  try {
    const health = await api("/health");
    $("#healthStatus").textContent = health.status === "ok" ? "服务正常" : "服务异常";
    $("#healthDot").classList.add("ok");
  } catch {
    $("#healthStatus").textContent = "后端未启动";
    $("#healthDot").classList.remove("ok");
  }

  const [pointsData, templatesData, assetsData] = await Promise.all([
    api("/api/inspection/points"),
    api("/api/report/templates"),
    api("/api/assets"),
  ]);
  state.points = pointsData.points || [];
  state.templates = templatesData.templates || [];
  state.assets = assetsData.assets || [];
  state.selectedPointId = state.points[0]?.id || null;
  renderPoints();

  const activeRecordId = localStorage.getItem("inspectai.activeRecord");
  if (activeRecordId) {
    try {
      state.record = await api(`/api/inspection/records/${activeRecordId}`);
    } catch {
      localStorage.removeItem("inspectai.activeRecord");
    }
  }
  renderAll();
}

$("#createRecordBtn").addEventListener("click", async () => {
  const point = selectedPoint();
  if (!point) {
    toast("请选择巡检点位");
    return;
  }
  state.record = await api("/api/inspection/records", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({
      pointId: point.id,
      type: point.type,
      inspector: $("#inspector").value.trim() || "巡检员",
    }),
  });
  state.task = null;
  localStorage.setItem("inspectai.activeRecord", state.record.id);
  renderAll();
  pushTimeline("记录已创建", `${state.record.project} / ${state.record.pointName}`);
  setStage("photo");
});

$("#imageInput").addEventListener("change", (event) => {
  state.selectedFiles = Array.from(event.target.files || []).slice(0, 3);
  renderPreviews();
});

$("#uploadAnalyzeBtn").addEventListener("click", async () => {
  if (!state.record) {
    toast("请先创建巡检记录");
    return;
  }
  if (!state.selectedFiles.length) {
    toast("请先拍照或选择照片");
    return;
  }

  const form = new FormData();
  state.selectedFiles.forEach((file) => form.append("files", file));
  const data = await api(`/api/inspection/records/${state.record.id}/images`, {
    method: "POST",
    body: form,
  });
  state.record = data.record;
  state.selectedFiles = [];
  $("#imageInput").value = "";
  renderPreviews();
  renderAll();
  pushTimeline("照片已上传", `${data.images.length} 张照片进入识别流程`);
  await startAnalysis();
});

$("#manualBtn").addEventListener("click", enterManualMode);
$("#modalManualBtn").addEventListener("click", enterManualMode);

$("#retakeBtn").addEventListener("click", () => {
  hideRetakeModal();
  setStage("photo");
  $("#imageInput").click();
});

$("#fieldList").addEventListener("click", async (event) => {
  const button = event.target.closest("[data-confirm-field]");
  if (!button) return;
  const card = button.closest(".field-card");
  try {
    await saveFieldCard(card);
    await refreshRecord();
    toast("字段已确认");
  } catch (err) {
    toast(err.message);
  }
});

$("#saveAllBtn").addEventListener("click", async () => {
  if (!state.record) {
    toast("请先创建巡检记录");
    return;
  }
  const cards = $$(".field-card");
  try {
    for (const card of cards) {
      await saveFieldCard(card);
    }
    await refreshRecord();
    toast("全部字段已保存确认");
    setStage("submit");
  } catch (err) {
    toast(err.message);
  }
});

$("#submitBtn").addEventListener("click", async () => {
  if (!state.record) {
    toast("请先创建巡检记录");
    return;
  }
  if (!requiredFieldsReady()) return;
  state.record = await api(`/api/inspection/records/${state.record.id}/submit`, {
    method: "POST",
    headers: { "Idempotency-Key": idempotencyKey() },
  });
  await refreshAssets();
  renderAll();
  pushTimeline("记录已提交", "AI总结已追加，资产台账已更新");
  toast("提交完成，台账已更新");
  setStage("ledger");
});

$$(".step").forEach((step) => {
  step.addEventListener("click", () => setStage(step.dataset.stage));
});

init().catch((err) => toast(err.message));
