// ===== 状态胶囊 =====
//
// 5 个页面 6 处各自手写。已经出过事:上下文卡的「已识别」和 AI 卡的「正常」
// 都是状态胶囊,一个 11px/500、一个 12px/700 —— 因为是两处独立手写的,
// 谁也不知道对方长什么样。这就是不做组件的代价。
//
// 样式复用已有的 .tag / .tag-*(全站 11px/600 那套),不另起一套类名 ——
// 台账列表已经在用它了,再发明一套只会多出第三种规格。
//
// 【色档口径集中在这里】
// 状态文案 → 颜色的映射散在各页面时,同一个"待复核"在台账是橙的、在预览页
// 可能是灰的。新增状态只改这一处。

export type Tone = "ok" | "warn" | "danger" | "repair" | "unknown" | "brand";

/**
 * 巡检结论 / 资产状态 / 任务状态 → 色档。
 *
 * 这份表是把台账、任务、设备详情三处原有的映射【原样合并】过来的。
 * 一处有意的偏离:「待整改」原来是 brand(蓝),读起来像"提示"而不是"要
 * 处理",主管扫列表时不够跳 —— 2026-08-04 改成 warn(橙)。
 */
const TONE_BY_TEXT: Record<string, Tone> = {
  正常: "ok",
  健康: "ok",
  已识别: "ok",
  已完成: "ok",
  进行中: "ok", // 来自任务页:已经在做了,不是待办
  异常: "danger",
  逾期: "danger",
  待维修: "repair",
  待复核: "warn",
  待重拍: "warn",
  人工填写: "warn",
  识别中: "warn",
  待整改: "warn", // 蓝色读起来像"提示",这是要人去处理的,得跳出来
  待执行: "brand",
  // 未开始不是问题,别用橙色吓人 —— 巡检员扫一眼要能分清
  // "需要我处理"和"还没轮到"
  未识别: "unknown",
  未巡检: "unknown",
};

/** 未知状态一律 warn:拿不准时偏保守,别把没定论的显示成正常。 */
export function toneOf(text: string): Tone {
  return TONE_BY_TEXT[text.trim()] ?? "warn";
}

export interface StatusTagProps {
  text: string;
  /** 不传则按文案自动判色 */
  tone?: Tone;
  className?: string;
}

export default function StatusTag({ text, tone, className }: StatusTagProps) {
  return (
    <span
      className={`tag tag-${tone ?? toneOf(text)}${className ? " " + className : ""}`}
    >
      {text}
    </span>
  );
}
