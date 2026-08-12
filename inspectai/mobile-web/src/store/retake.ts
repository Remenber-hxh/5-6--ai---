// 「重新拍照复检」的上下文。
//
// 场景:设备台账里某台是异常/待整改,巡检员现场复查后要重拍一次把它销账。
// 难点在于——重拍出来的记录必须落到【同一台设备】上,否则台账里那条异常
// 永远挂着,而旁边又多出一条新的正常记录。
//
// 所以带三样东西过去:
//   templateId  跳过 AI 分类(目标设备用哪个模板是已知的,不该再猜一次,
//               猜错了记录就落到别的模板上)
//   pointId     建记录时带上,保证点位一致
//   assetNo     写进表单的 asset_no 字段 —— 这才是后端认资产身份的那个键
//
// 存 localStorage 的原因和 activeTask 一样:巡检员点完"复检"要去拍照,
// 中途可能切后台接电话、可能被系统杀掉,回来不该丢上下文。
//
// 这是旧版 frontend/app.js 的 state.retakeTarget,新版一直缺这一条闭环。

const KEY = "inspectai_mobile_retake";

export interface RetakeTarget {
  templateId: string;
  pointId: string;
  /** 设备编号,提交时写进 asset_no —— 后端按它认归属 */
  assetNo: string;
  /** 只用于界面提示 */
  assetName: string;
}

export function setRetakeTarget(t: RetakeTarget | null) {
  try {
    if (t) localStorage.setItem(KEY, JSON.stringify(t));
    else localStorage.removeItem(KEY);
  } catch {
    /* 隐私模式下写不进就算了,不影响主流程 */
  }
}

export function getRetakeTarget(): RetakeTarget | null {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return null;
    const t = JSON.parse(raw) as RetakeTarget;
    // 没有 templateId 就等于没有复检能力(既跳不过分类,也保证不了归属),
    // 当作无效上下文丢掉,免得留下一个"看着在复检、实际没生效"的横幅。
    return t && t.templateId ? t : null;
  } catch {
    return null;
  }
}

export function clearRetakeTarget() {
  setRetakeTarget(null);
}
