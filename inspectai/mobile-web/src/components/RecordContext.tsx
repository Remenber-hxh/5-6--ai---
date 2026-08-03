import { Avatar, Cell } from "@/ui";

import type { RecordDTO } from "@/api/inspection";

// ===== 记录上下文卡 =====
//
// 照旧版 renderContext(app.js:1009):
//   圆形徽章(项目首字) + 主行「项目 · 点位」+ 副行「模板 · 巡检员」+ 右侧识别状态
//
// 比一行灰色小字强在:同样的四段信息,分了主次、有了锚点,一眼扫得清。
// 之前新版把它压成一行「模板 · 项目 · 点位 · N/M 项已填」,四段挤在一起反而难读。
//
// 用组件库的 Cell:icon / label / desc / 右侧内容四个插槽正好一一对上,
// 分割线、内边距、点按反馈都由它保证,不用再写一套。

/** 识别状态 → 文案。照搬旧版 recognitionStatusText(app.js:1022) */
const STATUS_TEXT: Record<string, string> = {
  not_started: "未识别",
  processing: "识别中",
  recognized: "已识别",
  retake_required: "待重拍",
  manual_required: "人工填写",
};

export default function RecordContext({ rec }: { rec: RecordDTO }) {
  const status = STATUS_TEXT[rec.recognitionStatus] || rec.recognitionStatus || "-";
  // 只有"已识别"是绿的;其余(含人工填写、待重拍)都是提醒态 —— 旧版同样口径:
  // 别把"还需要人做点什么"显示成一切正常。
  const tone = rec.recognitionStatus === "recognized" && !rec.manualRequired ? "ok" : "warn";

  return (
    <Cell
      className="rec-ctx"
      bordered={false}
      icon={<Avatar name={rec.project || "巡"} size="small" />}
      label={`${rec.project || ""} · ${rec.pointName || ""}`}
      desc={`${rec.templateName || ""} · ${rec.inspector || ""}`}
    >
      <span className={`ctx-badge ${tone}`}>{status}</span>
    </Cell>
  );
}
