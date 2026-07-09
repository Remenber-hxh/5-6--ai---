// Word 导出:与旧版 exportWordDoc 同方案 —— MSO HTML 包装成 .doc,零依赖,Word/WPS 直开
function esc(s: string): string {
  return String(s || "").replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");
}

export function exportWordDoc(filename: string, title: string, bodyHtml: string) {
  const head =
    '<html xmlns:o="urn:schemas-microsoft-com:office:office" ' +
    'xmlns:w="urn:schemas-microsoft-com:office:word" ' +
    'xmlns="http://www.w3.org/TR/REC-html40"><head><meta charset="utf-8">' +
    `<title>${esc(title)}</title>` +
    "<!--[if gte mso 9]><xml><w:WordDocument><w:View>Print</w:View>" +
    "<w:Zoom>100</w:Zoom><w:DoNotOptimizeForBrowser/></w:WordDocument></xml><![endif]-->" +
    "<style>" +
    "@page{size:A4;margin:2.2cm;}" +
    'body{font-family:"Microsoft YaHei","SimSun",sans-serif;font-size:11pt;color:#222;line-height:1.75;}' +
    "h1{font-size:19pt;margin:0 0 4pt;color:#0c3b30;}" +
    "h2{font-size:13.5pt;margin:16pt 0 6pt;color:#0a6b54;border-bottom:1pt solid #cfe8df;padding-bottom:3pt;}" +
    "table{border-collapse:collapse;width:100%;margin:8pt 0;}" +
    "td,th{border:1pt solid #bcbcbc;padding:5pt 8pt;font-size:10.5pt;}" +
    "th{background:#eef6f3;text-align:left;}" +
    "p{margin:0 0 8pt;}" +
    "</style></head><body>";
  const blob = new Blob(["﻿", head + bodyHtml + "</body></html>"], {
    type: "application/msword;charset=utf-8",
  });
  const a = document.createElement("a");
  a.href = URL.createObjectURL(blob);
  a.download = /\.docx?$/.test(filename) ? filename : `${filename}.doc`;
  a.click();
  URL.revokeObjectURL(a.href);
}

const pct1 = (x?: number) => ((x || 0) * 100).toFixed(1) + "%";
const dt10 = (s?: string) => (s || "").slice(0, 10);
const deltaTxt = (a?: number, b?: number) => {
  const v = (a || 0) - (b || 0);
  return v > 0 ? `▲ ${v}` : v < 0 ? `▼ ${-v}` : "持平";
};

// 日报数据 → Word 正文(与旧版 buildDailyReportHtml 完全对齐)
export function buildDailyHtml(d: any): string {
  const c = d.conclusion || {},
    ex = d.execution || {},
    as = d.assetStatus || {},
    rq = d.reviewQuality || {},
    cmp = d.compare || {},
    ns = d.nextStep || {};
  const riskL = (l: string) => (l === "danger" ? "高" : l === "warning" ? "中" : "低");
  const pct0 = (x?: number) => ((x || 0) * 100).toFixed(0) + "%";
  let h = `<h1>智巡 · 巡检日报</h1>`;
  h += `<p>${esc(d.date || "")} · ${esc(d.project || "全部项目")} · 数据范围 今日 00:00–现在 · 口径 已提交巡检 + 待复核 + 待审批 + 今日任务</p>`;
  h += `<h2>今日结论</h2><p>${c.hasAbnormal ? "今日有异常" : "今日无异常"} · 异常 <b>${c.abnormalCount ?? 0}</b> · 待处理 <b>${c.pendingCount ?? 0}</b> · 今日闭环 <b>${c.closedCount ?? 0}</b></p><p>${esc(d.summary || "")}</p>`;
  h += `<h2>巡检执行（任务）</h2><table><tr><th>计划</th><th>已完成</th><th>进行中</th><th>未开始</th><th>逾期</th><th>完成率</th></tr><tr><td>${ex.plan ?? 0}</td><td>${ex.done ?? 0}</td><td>${ex.processing ?? 0}</td><td>${ex.notStarted ?? 0}</td><td>${ex.overdue ?? 0}</td><td>${pct0(ex.completeRate)}</td></tr></table>`;
  h += `<h2>资产 / 记录状态（今日）</h2><table><tr><th>今日巡检</th><th>正常</th><th>异常</th><th>待复核</th><th>需补图</th><th>人工填写</th></tr><tr><td>${as.inspected ?? 0}</td><td>${as.normal ?? 0}</td><td>${as.abnormal ?? 0}</td><td>${as.pendingReview ?? 0}</td><td>${as.needRetake ?? 0}</td><td>${as.manualFill ?? 0}</td></tr></table>`;
  h += `<h2>人工复核质量</h2><table><tr><td>AI 识别成功</td><td>${rq.aiSuccess ?? 0}</td><td>人工修正字段</td><td>${rq.manualEdits ?? 0}</td></tr><tr><td>低置信字段</td><td>${rq.lowConf ?? 0}</td><td>补图次数</td><td>${rq.retakes ?? 0}</td></tr><tr><td>未看图确认</td><td>${rq.noPhotoConfirm ?? 0}</td><td>需主管复核</td><td>${rq.needSupervisor ?? 0}</td></tr></table>`;
  h += `<h2>较昨日</h2><table><tr><td>巡检数变化</td><td>${deltaTxt(cmp.recordDelta ?? 0, 0)}</td><td>异常数变化</td><td>${deltaTxt(cmp.abnormalDelta ?? 0, 0)}</td></tr></table>`;
  const rep: any[] = cmp.repeatedIssues || [];
  if (rep.length)
    h += `<p>重复异常：${rep.slice(0, 4).map((x) => esc((x.assetName || "") + (x.fieldLabel ? " · " + x.fieldLabel : ""))).join("；")}</p>`;
  const focus = (ns.focusAssets || []).map(esc).join("、");
  h += `<h2>下一步</h2><p>待办转入 <b>${ns.carryOver ?? 0}</b> 项 · 待审批 <b>${ns.approvals ?? 0}</b> 项${focus ? " · 重点盯：" + focus : ""}</p>`;
  const ab: any[] = d.abnormalList || [];
  h += `<h2>异常处理清单（${ab.length}）</h2>`;
  if (ab.length) {
    h += `<table><tr><th>点位</th><th>异常字段</th><th>值</th><th>风险</th><th>责任人</th><th>截止</th><th>状态</th><th>记录号</th></tr>`;
    ab.forEach((x) => {
      h += `<tr><td>${esc(x.point)}</td><td>${esc(x.field)}</td><td>${esc(x.value)}</td><td>${riskL(x.risk)}</td><td>${esc(x.assignee)}</td><td>${esc(x.dueAt || "—")}</td><td>${esc(x.status)}</td><td>${esc(x.recordNo)}</td></tr>`;
    });
    h += `</table>`;
  } else h += `<p>今日无异常 / 待处理记录。</p>`;
  const nm = d.normalSummary || {},
    items: any[] = nm.items || [];
  h += `<h2>正常记录摘要（${nm.count ?? 0}）</h2>`;
  if (items.length) {
    h += `<table><tr><th>点位</th><th>模板</th><th>巡检人</th><th>提交</th></tr>`;
    items.slice(0, 12).forEach((x) => {
      h += `<tr><td>${esc(x.point)}</td><td>${esc(x.template)}</td><td>${esc(x.inspector)}</td><td>${esc(x.submittedAt)}</td></tr>`;
    });
    h += `</table>`;
  } else h += `<p>今日暂无正常记录。</p>`;
  return h;
}

// 周报数据 → Word 正文(与旧版 buildWeeklyReportHtml 完全对齐)
export function buildWeeklyHtml(d: any): string {
  const m = d.metrics || {};
  const riskLabel = (l: string) =>
    l === "danger" ? "高风险" : l === "warning" ? "需关注" : l === "repair" ? "维修中" : "正常";
  let h = `<h1>智巡 · 本周巡检周报</h1>`;
  h += `<h2>一、本周结论</h2><p>${esc(d.summary || "")}</p>`;
  h += `<h2>二、核心指标（${dt10(d.rangeStart)} ~ ${dt10(d.rangeEnd)}）</h2>`;
  h += `<table><tr><th>指标</th><th>本周</th><th>上周</th><th>环比</th></tr>`;
  const row = (label: string, a?: number, b?: number) =>
    `<tr><td>${label}</td><td>${a ?? 0}</td><td>${b ?? 0}</td><td>${deltaTxt(a, b)}</td></tr>`;
  h += row("巡检记录数", m.recordRecent, m.recordPrev);
  h += row("巡检资产数", m.assetInspectedRecent, m.assetInspectedPrev);
  h += row("异常记录数", m.abnormalRecent, m.abnormalPrev);
  h += row("已闭环数", m.closedRecent, m.closedPrev);
  h += `<tr><td>待复核 / 待审批</td><td colspan="3">${m.pendingReviews ?? 0} 条 / ${m.pendingApprovals ?? 0} 条</td></tr>`;
  h += `<tr><td>需补图 / 未看图确认率</td><td colspan="3">${m.needRetake ?? 0} 条 / ${pct1(m.lazyConfirmRate)}</td></tr>`;
  h += `</table>`;
  const risk: any[] = (d.topRisk || []).slice(0, 5);
  if (risk.length) {
    h += `<h2>三、重点关注资产</h2><table><tr><th>资产</th><th>风险</th><th>主要问题</th><th>AI 依据</th><th>建议动作</th></tr>`;
    risk.forEach((a) => {
      h += `<tr><td>${esc(a.assetName)}</td><td>${riskLabel(a.riskLevel)}</td><td>${esc(a.mainIssue || "")}</td><td>${esc(a.aiBasis || "")}</td><td>${esc(a.suggestedAction || "")}</td></tr>`;
    });
    h += `</table>`;
  }
  const ic: any[] = d.issueClosure || [];
  h += `<h2>四、异常闭环情况（${ic.length}）</h2>`;
  if (ic.length) {
    h += `<table><tr><th>异常项</th><th>发现</th><th>来源记录</th><th>状态</th><th>责任人</th><th>截止</th><th>处理建议</th></tr>`;
    ic.forEach((x) => {
      h += `<tr><td>${esc(x.issueName)}${x.value ? "=" + esc(x.value) : ""}</td><td>${esc(x.foundAt)}</td><td>${esc(x.recordNo)}</td><td>${esc(x.status)}</td><td>${esc(x.assignee)}</td><td>${esc(x.dueAt || "—")}</td><td>${esc(x.suggestion || "")}</td></tr>`;
    });
    h += `</table>`;
  } else h += `<p>本周无未闭环异常。</p>`;
  const qs = d.qualitySummary || {};
  h += `<h2>五、巡检质量与 AI 协同</h2><table><tr><th>维度</th><th>本周</th><th>风险判断</th></tr>`;
  const qrow = (label: string, val?: number, judge?: string) =>
    `<tr><td>${label}</td><td>${val ?? 0}</td><td>${judge}</td></tr>`;
  h += qrow("AI 识别成功记录", qs.aiSuccess, "正常");
  h += qrow("人工修正字段", qs.manualEdits, "字段规则可优化");
  h += qrow("低置信度字段", qs.lowConfidenceFields, "需补参考图");
  h += qrow("补图次数", qs.retakes, "拍摄规范需加强");
  h += qrow("未看图确认", qs.noPhotoConfirm, (qs.noPhotoConfirm || 0) > 0 ? "需主管抽查" : "良好");
  h += qrow("重复异常字段", qs.repeatedFieldIssues, "应纳入重点规则");
  h += `</table>`;
  const na: any[] = d.nextActions || [];
  if (na.length) {
    h += `<h2>六、下周工作安排</h2><table><tr><th>工作项</th><th>对象</th><th>负责人</th><th>时间</th><th>触发依据</th></tr>`;
    na.forEach((x) => {
      h += `<tr><td>${esc(x.workItem)}</td><td>${esc(x.target)}</td><td>${esc(x.assignee)}</td><td>${esc(x.time)}</td><td>${esc(x.trigger)}</td></tr>`;
    });
    h += `</table>`;
  }
  const srcs = ((d.traceability || {}).sources || []).join("、");
  h += `<h2>七、数据溯源</h2><p>本周报基于${esc(srcs || "系统巡检数据")}自动生成；每条异常可回溯到记录编号、资产编号与图片证据。</p>`;
  return h;
}
