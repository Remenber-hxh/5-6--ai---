import type { AssetDTO } from "@/api/inspection";

/**
 * 设备封面缩略图地址。照搬旧版 thumbPath + storageURL 的口径:
 * 后端给的是磁盘路径(Windows 上是 `..\storage\assets\...` 反斜杠),
 * 统一斜杠后截掉 `/storage/` 之前的部分,再拼回 /storage/ 前缀。
 *
 * 【列表和详情共用这一份】原来只有列表页有,详情页要加顶部照片时
 * 再抄一遍的话,哪天路径规则改了只会修好一边 —— 表现是"列表有图、点进去没图"。
 */
export function coverURL(a: Pick<AssetDTO, "coverImage">): string | null {
  const raw = (a.coverImage?.path || "").replace(/\\/g, "/");
  if (!raw) return null;
  const i = raw.indexOf("/storage/");
  if (i < 0) return null;
  return "/storage/" + encodeURI(raw.substring(i + "/storage/".length));
}

/**
 * 上次巡检距今多久:「今天」「昨天」「3 天前」,超过一个月给日期。
 *
 * 【为什么替掉「63 次」】一条记录会派生好几台设备(抄表一次六台、综合巡检一次五台),
 * 每台都 +1,于是同一个模板下的设备次数全都一样 —— 数的是"这个模板被巡了几次",
 * 不是"这台被看了几次"。每行右下角挂一个一样的数,没有信息量。
 * 而"多久没巡了"每台都不一样,一眼就能看出哪台被落下了。
 *
 * 【Go 的零值时间当没有】从没巡过的设备发来的是 0001-01-01,是个合法日期,
 * 不拦的话会显示成"739000 天前"。
 */
export function sinceText(iso?: string, now: Date = new Date()): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime()) || d.getFullYear() < 2000) return "";
  // 按【日历日】算,不按 24 小时:昨晚 23 点巡的,今早 8 点看应该是"昨天",
  // 按小时算会是"今天"(不到 24 小时),和人的说法对不上。
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86_400_000);
  if (days <= 0) return "今天";
  if (days === 1) return "昨天";
  if (days < 31) return `${days} 天前`;
  return `${d.getMonth() + 1}/${d.getDate()}`;
}
