import type { FieldValue } from "@/api/inspection";

// 一个巡检字段算不算"异常"。
//
// ⚠️ 这份规则是 go-backend/cmd/server/handlers.go 的 isOccurrenceLabel 的
// 逐条移植,两边【必须保持一致】。后端用它决定 AI 总结里写不写"待跟进 N 项",
// 前端用它决定哪一行标红 —— 口径分叉的结果是同一屏上自相矛盾:
// 总结说"待跟进 1 项:设备无异响、异味=否",而那一行却是正常色。
// (ai-service/run.py 里的 summary_is_occurrence_label 是第三份,同样要对齐。)
//
// 难点在于"是/否"的好坏取决于问法:
//   正向问(“…完好吗”“设备无异响吗”)  → 答「否」是坏
//   反向问(“有无异响”“是否漏水”)      → 答「是」是坏
//
// 【最容易写错的一条】"设备无异响、异味"里带着"异响"两个字,只按关键词匹配
// 会把它当成反向问法,于是「否」被判成正常 —— 正好把语义翻了过来。
// 那个「无」字才是决定性的,所以 "无异/无报警/无漏/无故障" 要【先】判并返回 false。
export function isOccurrenceLabel(label: string): boolean {
  if (label.includes("有无") || label.includes("是否有")) return true;
  if (
    label.includes("无异") ||
    label.includes("无报警") ||
    label.includes("无漏") ||
    label.includes("无故障")
  ) {
    return false;
  }
  for (const kw of ["异常", "是否漏水", "是否报警", "有异响", "有异味"]) {
    if (label.includes(kw)) return true;
  }
  return false;
}

export function fieldIsBad(f: Pick<FieldValue, "label" | "value">): boolean {
  const v = String(f.value ?? "").trim();
  if (!v) return false;
  // 这几个词本身就是结论,不用看问法
  if (["异常", "缺失", "破损", "故障"].includes(v)) return true;
  const occurrence = isOccurrenceLabel(f.label);
  if (v === "否") return !occurrence;
  if (v === "是" || v === "有") return occurrence;
  return false;
}
