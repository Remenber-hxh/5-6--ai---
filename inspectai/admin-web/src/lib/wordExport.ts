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

// 日报数据 → Word 正文(字段与 /report?type=daily 返回一致)
export function buildDailyHtml(d: any): string {
  const c = d.conclusion || {};
  const ex = d.execution || {};
  const as = d.assetStatus || {};
  let h = `<h1>智巡 · 巡检日报</h1><p>${esc(d.date || "")} · ${esc(d.project || "全部项目")}</p>`;
  h += `<h2>今日结论</h2><p>${c.hasAbnormal ? "今日有异常" : "今日无异常"} · 异常 ${c.abnormalCount ?? 0} · 待处理 ${c.pendingCount ?? 0} · 今日闭环 ${c.closedCount ?? 0}</p><p>${esc(d.summary || "")}</p>`;
  h += `<h2>巡检执行</h2><table><tr><th>计划</th><th>已完成</th><th>进行中</th><th>逾期</th></tr><tr><td>${ex.plan ?? 0}</td><td>${ex.done ?? 0}</td><td>${ex.processing ?? 0}</td><td>${ex.overdue ?? 0}</td></tr></table>`;
  h += `<h2>记录状态</h2><table><tr><th>今日巡检</th><th>正常</th><th>异常</th><th>待复核</th></tr><tr><td>${as.inspected ?? 0}</td><td>${as.normal ?? 0}</td><td>${as.abnormal ?? 0}</td><td>${as.pendingReview ?? 0}</td></tr></table>`;
  const ab: any[] = d.abnormalList || [];
  if (ab.length) {
    h += `<h2>异常处理清单(${ab.length})</h2><table><tr><th>点位</th><th>异常字段</th><th>责任人</th><th>状态</th><th>记录号</th></tr>`;
    ab.forEach((x) => {
      h += `<tr><td>${esc(x.point)}</td><td>${esc(x.field)}</td><td>${esc(x.assignee)}</td><td>${esc(x.status)}</td><td>${esc(x.recordNo)}</td></tr>`;
    });
    h += `</table>`;
  }
  return h;
}

// 周报数据 → Word 正文(字段与 /report?type=weekly 返回一致)
export function buildWeeklyHtml(d: any): string {
  const m = d.metrics || {};
  const dt = (s: string) => (s || "").slice(0, 10);
  let h = `<h1>智巡 · 本周巡检周报</h1><p>周期:${dt(d.rangeStart)} ~ ${dt(d.rangeEnd)}</p>`;
  h += `<h2>一、本周结论</h2><p>${esc(d.summary || "")}</p>`;
  h += `<h2>二、核心指标</h2><table><tr><th>指标</th><th>本周</th><th>上周</th></tr>`;
  const row = (label: string, a?: number, b?: number) =>
    `<tr><td>${label}</td><td>${a ?? 0}</td><td>${b ?? 0}</td></tr>`;
  h += row("巡检记录数", m.recordRecent, m.recordPrev);
  h += row("巡检资产数", m.assetInspectedRecent, m.assetInspectedPrev);
  h += row("异常记录数", m.abnormalRecent, m.abnormalPrev);
  h += row("已闭环数", m.closedRecent, m.closedPrev);
  h += `</table>`;
  const risk: any[] = d.topRisk || [];
  if (risk.length) {
    h += `<h2>三、重点关注资产</h2><table><tr><th>资产</th><th>主要问题</th><th>建议动作</th></tr>`;
    risk.slice(0, 5).forEach((a) => {
      h += `<tr><td>${esc(a.assetName)}</td><td>${esc(a.mainIssue || "")}</td><td>${esc(a.suggestedAction || "")}</td></tr>`;
    });
    h += `</table>`;
  }
  const ic: any[] = d.issueClosure || [];
  if (ic.length) {
    h += `<h2>四、异常闭环情况</h2><table><tr><th>异常项</th><th>来源记录</th><th>状态</th><th>责任人</th><th>处理建议</th></tr>`;
    ic.forEach((x) => {
      h += `<tr><td>${esc(x.issueName)}</td><td>${esc(x.recordNo)}</td><td>${esc(x.status)}</td><td>${esc(x.assignee)}</td><td>${esc(x.suggestion || "")}</td></tr>`;
    });
    h += `</table>`;
  }
  const na: any[] = d.nextActions || [];
  if (na.length) {
    h += `<h2>五、下周工作安排</h2><table><tr><th>工作项</th><th>对象</th><th>触发依据</th></tr>`;
    na.forEach((x) => {
      h += `<tr><td>${esc(x.workItem)}</td><td>${esc(x.target)}</td><td>${esc(x.trigger)}</td></tr>`;
    });
    h += `</table>`;
  }
  h += `<h2>六、数据溯源</h2><p>本周报基于巡检记录、资产台账、审批记录、AI 识别结果与现场图片自动生成;每条异常可回溯到记录编号与图片证据。</p>`;
  return h;
}
