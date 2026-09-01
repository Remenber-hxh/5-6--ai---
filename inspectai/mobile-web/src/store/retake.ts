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
  /**
   * 这个上下文是【为什么】设上的。机制一样,但对用户来说是两件事:
   *
   *   recheck 台账里某台是异常/待整改,现场复查后重拍销账 —— 这才是"复检"
   *   scan    扫了设备上的二维码,只是把设备锁定,是【一次正常巡检】
   *
   * 【必须分开】不分的话,扫码巡检会在首页挂一条写着"复检"的横幅、
   * 提交后弹"复检已提交"—— 数据上明明是正常巡检,界面却说是复检。
   * 巡检员会以为自己刚才做的是复查,主管看记录时也会被这句话误导。
   *
   * 缺省当 recheck:老数据(升级前存在 localStorage 里的)没有这个字段,
   * 而在加扫码之前,这个上下文只有复检一种来源。
   */
  mode?: "recheck" | "scan";
  templateId: string;
  pointId: string;
  /** 设备编号,提交时写进 asset_no —— 后端按它认归属 */
  assetNo: string;
  /**
   * 完整资产 ID(项目::模板::编号)。扫码时有,台账复检时也有。
   *
   * 【为什么除了编号还要存它】照片按设备分组要用完整 ID —— 编号在不同项目里
   * 可能重复(各楼的编号各自排),只按编号分组会把两个项目的照片并到一起。
   */
  assetId?: string;
  /** 只用于界面提示 */
  assetName: string;
  /**
   * 设上的时刻(毫秒)。用来判过期。
   *
   * 【为什么必须有】清除只发生在"提交成功"和"手动取消"两处,中途放弃
   * (退出微信、切走不回来、拍到一半发现拍错了)就永远留着。于是第二天
   * 的一次普通巡检会被悄悄强制成昨天那台设备的模板和编号 —— 照片挂错设备,
   * 而界面上看不出任何异常。
   *
   * 已经踩到过一次:残留上下文的 templateId 失效,建记录报「未找到日报模板」,
   * 而屏幕上明明显示 AI 识别成功。换台手机就好了,因为残留在本机。
   */
  setAt?: number;
}

/**
 * 多久算过期。
 *
 * 【半天】一次复检从点"复检"到提交完,正常是几分钟,极端情况(现场没信号、
 * 中途去处理别的事)也就一两个小时。跨天还没提交的,几乎可以肯定是放弃了 ——
 * 而留着它的代价是把不相干的巡检记录挂到那台设备头上。
 */
const MAX_AGE_MS = 12 * 60 * 60 * 1000;

export function setRetakeTarget(t: RetakeTarget | null) {
  try {
    if (t) localStorage.setItem(KEY, JSON.stringify({ ...t, setAt: Date.now() }));
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
    if (!t || !t.templateId) return null;
    // 【没有 setAt 的一律当过期】那是加过期判断之前存下的,而现在装着这种
    // 上下文的手机里,躺的正是已经出过问题的那一批。代价不对称:
    // 误清 = 用户重新点一次"复检";误留 = 照片挂到别的设备上,谁都看不出来。
    if (!t.setAt || Date.now() - t.setAt > MAX_AGE_MS) {
      setRetakeTarget(null);
      return null;
    }
    return t;
  } catch {
    return null;
  }
}

export function clearRetakeTarget() {
  setRetakeTarget(null);
}
