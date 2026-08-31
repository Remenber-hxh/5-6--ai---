import { Card, Image, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";

import { AssetPhoto, listAssetPhotos } from "../api/mgmt";
import { mediaUrl } from "../lib/status";
import { C } from "../styles/tokens";

/**
 * 历次巡检照片。
 *
 * 【照片是最直观的"趋势"】曲线要人认得读数、理解基线;照片不用 ——
 * 锈迹、渗漏、积灰在两张图之间一眼可见。对非技术的人(领导、甲方、监管)
 * 它比任何图表都好使。也是尽职证明的核心:出事时能拿出
 * "我们每月都拍了,当时是这样"。
 *
 * 【不叫"同机位对比"】拍照的机位由现场的人决定,系统保证不了每次站在
 * 同一位置。叫"历次巡检照片"是准确的;叫"同机位对比"会让人以为系统
 * 做了对齐,那是过度承诺。
 *
 * 【横向排列 + 可左右翻的大图】对比要的是"挨着看",所以缩略图横排、
 * 点开进 PreviewGroup —— 在大图里按方向键就能在历次之间来回切,
 * 不用退出去再点下一张。
 */
export default function AssetPhotoTimeline({ assetId }: { assetId: string }) {
  const [photos, setPhotos] = useState<AssetPhoto[]>([]);

  const load = useCallback(async (id: string) => {
    try {
      setPhotos(await listAssetPhotos(id, 12));
    } catch {
      setPhotos([]); // 拿不到就不显示,不摆一个空框
    }
  }, []);

  useEffect(() => {
    if (assetId) void load(assetId);
  }, [assetId, load]);

  // 【一张照片不构成时间线】只有一次巡检时,这一块和顶部那张封面是同一张图,
  // 摆两遍只是重复。两张起才谈得上"对比"。
  if (photos.length < 2) return null;

  return (
    <Card size="small" title={`历次巡检照片(${photos.length} 次)`}>
      <div
        // 【横向滚动而不是换行】时间线换行之后,第二行的第一张紧挨着
        // 第一行的最后一张,而它们时间上隔得最远 —— 顺着读会读出错误的相邻关系。
        style={{
          display: "flex",
          gap: 12,
          overflowX: "auto",
          paddingBottom: 6,
        }}
        className="cov-list"
      >
        <Image.PreviewGroup>
          {photos.map((p) => (
            <div key={p.recordId + p.at} style={{ flex: "none", width: 150 }}>
              <Image
                src={mediaUrl(p.path)}
                width={150}
                height={112}
                style={{ objectFit: "cover", borderRadius: 8 }}
                placeholder
              />
              <div
                style={{
                  marginTop: 5,
                  fontSize: 12,
                  color: C.textSub,
                  fontVariantNumeric: "tabular-nums",
                }}
              >
                {p.at.slice(0, 10)}
              </div>
              {p.inspector && (
                <Typography.Text type="secondary" style={{ fontSize: 11 }}>
                  {p.inspector}
                </Typography.Text>
              )}
            </div>
          ))}
        </Image.PreviewGroup>
      </div>
    </Card>
  );
}
