// 设备二维码内容的生成端。
//
// 【格式的权威定义在移动端】mobile-web/src/lib/assetQR.ts —— 那边负责解析，
// 这边负责生成。两边对不上的结果是：码印出来了，扫不出东西，而且要贴到现场
// 才会发现。改任何一边都必须同步另一边。
//
// 内容形如：
//     https://<域名>/#/scan?a=<urlencode(资产ID)>
//
// 资产 ID 本身是「项目::模板::编号」，三段齐全，所以扫码方不用联网查库
// 就能拼出巡检需要的全部上下文——机房里常常没信号。

export const SCAN_ROUTE = "/scan";
export const SCAN_PARAM = "a";

/**
 * 生成这台设备的二维码内容。
 *
 * origin 是【移动端】的访问地址，不一定等于后台自己的地址：
 * 线上两者同域（后台在 /v2/，移动端在 /），本地开发却是两个端口。
 * 所以打印前必须让人确认这个值——印错域名的贴纸得全部撕下来重贴，
 * 而这件事在贴上去之前完全看不出来。
 */
export function buildScanURL(assetId: string, origin: string): string {
  const q = `#${SCAN_ROUTE}?${SCAN_PARAM}=${encodeURIComponent(assetId)}`;
  const base = (origin || "").replace(/\/+$/, "");
  return base ? `${base}/${q}` : q;
}
