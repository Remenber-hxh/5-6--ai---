// ===== 设备二维码:内容格式 =====
//
// 一台设备一个码，贴在设备上。内容是一条 URL：
//
//     https://<域名>/#/scan?a=<urlencode(资产ID)>
//
// 【为什么直接把资产 ID 放进去，而不是发一个短码再查库】
// 资产 ID 本身就是「项目::模板::编号」，三样东西齐全，扫完不用联网
// 就能拼出这次巡检需要的全部上下文。机房里常常没信号——这是这个项目
// 反复遇到的现实，二维码要是必须联网才能解，那它在最该用的地方最不好用。
//
// 【为什么是 URL 而不是纯文本】自带双通道：
//   · 用 app 内的扫码 → 本地解析，离线可用
//   · 用微信/系统相机扫 → 直接打开移动端这一页，不用先装什么
// 纯文本的码在微信里扫出来只是一串乱码，现场的人不知道该干嘛。
//
// 【改这里要连着改哪】后台生成二维码的地方（admin-web 的 assetQR.ts）
// 必须用同一套格式。两边对不上的结果是：码印出来了，扫不出东西。

/** 资产 ID 拆出来的三段。 */
export interface AssetQRTarget {
  /** 完整资产 ID：项目::模板::编号 */
  assetId: string;
  project: string;
  templateId: string;
  /** 设备编号。后端认资产身份靠的就是它（写进表单的 asset_no） */
  assetNo: string;
}

/** 扫码页的路由路径。二维码 URL 与 App 路由都用它，避免两处写死。 */
export const SCAN_ROUTE = "/scan";
/** 资产 ID 在 URL 上的参数名。 */
export const SCAN_PARAM = "a";

/**
 * 把资产 ID 拆成三段。
 *
 * 【第三段之后不再切】编号里可能带 "::"（历史数据里出现过用它拼多段 key 的），
 * 所以只按前两个分隔符切，剩下的整体当编号。切多了会把编号截断，
 * 而截断后的编号在后端匹配不上任何设备——表现是"扫了没反应"。
 */
export function splitAssetID(assetId: string): AssetQRTarget | null {
  const id = (assetId || "").trim();
  if (!id) return null;
  const first = id.indexOf("::");
  if (first < 0) return null;
  const second = id.indexOf("::", first + 2);
  if (second < 0) return null;
  const project = id.slice(0, first);
  const templateId = id.slice(first + 2, second);
  const assetNo = id.slice(second + 2);
  if (!project || !templateId || !assetNo) return null;
  return { assetId: id, project, templateId, assetNo };
}

/**
 * 从扫到的原始文本里取出资产 ID。
 *
 * 【要容忍三种写法】现场的码可能来自不同批次、也可能被人手抄：
 *   1. 完整 URL：https://host/#/scan?a=xxx
 *   2. 只有查询串：#/scan?a=xxx
 *   3. 裸的资产 ID：会议中心::elevator_no_room::KT-7
 * 认得越宽，现场越少出现"这个码扫不出来"。但只认这三种——
 * 不做模糊猜测，扫到别人家的二维码就该老实说不认识。
 */
export function parseScannedText(raw: string): AssetQRTarget | null {
  const text = (raw || "").trim();
  if (!text) return null;

  // 先试 URL / 带 hash 的形式：找 a= 参数
  const qIndex = text.indexOf(SCAN_PARAM + "=");
  if (qIndex >= 0 && (text.includes("?") || text.includes("&"))) {
    const tail = text.slice(qIndex + SCAN_PARAM.length + 1);
    const value = tail.split("&")[0];
    try {
      const decoded = decodeURIComponent(value);
      const hit = splitAssetID(decoded);
      if (hit) return hit;
    } catch {
      // 编码坏了就当没匹配上，继续往下试裸 ID
    }
  }
  // 再试裸 ID
  return splitAssetID(text);
}

/**
 * 生成这台设备的二维码内容。
 *
 * origin 传当前站点（例如 https://inspect.example.com）。传空则退回相对形式，
 * 只能被 app 自己扫——【印之前一定要确认 origin 是对的】，
 * 印错了域名的贴纸要全部撕下来重贴，而这件事贴之前看不出来。
 */
export function buildScanURL(assetId: string, origin: string): string {
  const q = `#${SCAN_ROUTE}?${SCAN_PARAM}=${encodeURIComponent(assetId)}`;
  const base = (origin || "").replace(/\/+$/, "");
  return base ? `${base}/${q}` : q;
}
