import { ArrowLeftOutlined } from "@ant-design/icons";
import { Button, Card, Empty, Skeleton, Space, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { AssetEntry, AssetSnapshotEntry, listAssetSnapshots, listAssets } from "../api/mgmt";
import AssetOpenItems from "../components/AssetOpenItems";
import AssetPhotoTimeline from "../components/AssetPhotoTimeline";
import AssetTrend from "../components/AssetTrend";
import AssetVerdict from "../components/AssetVerdict";
import { C } from "../styles/tokens";
import { fmtTime, mediaUrl, statusTagColor } from "../lib/status";

/**
 * 设备档案:一台设备的一整页。
 *
 * 【为什么侧边抽屉不够】抽屉宽 400 上下,趋势曲线被压成一条缝,巡检轨迹
 * 只能看最近几条。而"这台设备到底怎么样"这个问题,恰恰要把读数走向和
 * 历次巡检摆在一起看 —— 那需要整页的宽度。
 *
 * 【这一页的顺序 = 回答问题的顺序】
 *   它现在什么状态  → 顶部一行
 *   它在往哪走      → 读数趋势(单次巡检看不出来的东西)
 *   都发生过什么    → 巡检轨迹
 */

export default function AssetProfile() {
  // 【资产 ID 里带 ::,路由用的是通配段】所以取 params["*"] 而不是 :id ——
  // 写成 :id 的话「会议中心::elevator::K01」会被当成多层路径,只拿到第一段。
  const params = useParams();
  const id = decodeURIComponent(params["*"] || "");
  const nav = useNavigate();
  const [asset, setAsset] = useState<AssetEntry | null>(null);
  const [trail, setTrail] = useState<AssetSnapshotEntry[]>([]);
  const [loading, setLoading] = useState(true);

  const load = useCallback(async (assetId: string) => {
    setLoading(true);
    try {
      // 台账没有"按 id 查单台"的接口,先拉列表再挑 —— 设备量在几十台的量级,
      // 为这一页单开一个接口不值当。哪天上千台了再说。
      const [all, snaps] = await Promise.all([
        listAssets(),
        listAssetSnapshots(assetId, 50).catch(() => ({ records: [], total: 0 })),
      ]);
      setAsset(all.find((a) => a.id === assetId) || null);
      setTrail(snaps.records);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (id) void load(id);
  }, [id, load]);

  if (loading) {
    return (
      <Card size="small">
        <Skeleton active paragraph={{ rows: 6 }} />
      </Card>
    );
  }

  if (!asset) {
    return (
      <Card size="small">
        <Empty description="这台设备不在台账里 —— 可能已被删除,或你没有它所属项目的权限">
          <Button onClick={() => nav("/ledger")}>返回台账</Button>
        </Empty>
      </Card>
    );
  }

  const cover = asset.coverImagePath || asset.lastPhotoPath;

  return (
    <Space direction="vertical" size={16} style={{ width: "100%" }}>
      <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => nav("/ledger")} style={{ paddingLeft: 0 }}>
        返回台账
      </Button>

      {/* ── 还有什么没了结 ──
          【放在最上面】打开这一页的人要的从来是"这台设备现在要不要我管",
          而这块就是答案本身。排在设备信息后面的话,人得往下翻才知道要不要管。
          没有未了结的事时它整块不显示 —— 页面上没有警示本身就是"正常"。 */}
      <AssetOpenItems assetId={asset.id} />

      {/* ── 它现在什么状态 ── */}
      <Card size="small">
        <div style={{ display: "flex", gap: 18, flexWrap: "wrap", alignItems: "flex-start" }}>
          {cover && (
            <img
              src={mediaUrl(cover)}
              alt=""
              style={{ width: 168, height: 126, objectFit: "cover", borderRadius: 10, flex: "none" }}
            />
          )}
          <div style={{ flex: 1, minWidth: 240, display: "flex", flexDirection: "column", gap: 8 }}>
            <div style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap" }}>
              <h2 style={{ margin: 0, fontSize: 21 }}>{asset.assetName || asset.assetKey}</h2>
              <Tag color={statusTagColor(asset.lastStatus || "")}>{asset.lastStatus || "未知"}</Tag>
            </div>
            <Space size={6} wrap>
              {asset.project && <Tag>{asset.project}</Tag>}
              {asset.assetType && <Tag>{asset.assetType}</Tag>}
              {asset.assetKey && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  编号 {asset.assetKey}
                </Typography.Text>
              )}
            </Space>
            {/* 【结论紧跟在设备名下面】这一页上最靠上的那行字,应该是
                "要不要管它",而不是又一个事实。事实往下排。 */}
            <AssetVerdict assetId={asset.id} />
            <Typography.Text type="secondary" style={{ fontSize: 13 }}>
              最近巡检 {asset.lastInspectedAt ? fmtTime(asset.lastInspectedAt) : "从未巡检"}
              {trail.length > 0 && ` · 历史 ${trail.length} 次`}
            </Typography.Text>
            {asset.lastSummary && (
              <Typography.Paragraph style={{ margin: 0, fontSize: 13.5, color: C.textSub }}>
                {asset.lastSummary}
              </Typography.Paragraph>
            )}
          </div>
        </div>
      </Card>

      {/* ── 它在往哪走 ──
          这一页里趋势排在轨迹前面:轨迹说"巡过几次、每次什么状态",
          趋势说"这台设备在往哪个方向走" —— 后者是单次巡检永远看不出来的。 */}
      <AssetTrend assetId={asset.id} card />

      {/* 【照片排在曲线之后、轨迹之前】曲线是给懂读数的人看的,
          照片是给所有人看的 —— 但它不能顶到最上面:先要知道"要不要管",
          再看"哪里在变",最后才是"长什么样"。 */}
      <AssetPhotoTimeline assetId={asset.id} />

      {/* ── 都发生过什么 ── */}
      <Card size="small" title={`巡检轨迹（${trail.length} 次）`}>
        {trail.length === 0 ? (
          <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="这台设备还没有巡检记录" />
        ) : (
          <div>
            {trail.map((t, i) => (
              <div
                key={t.id}
                className={t.recordId ? "hl-row" : undefined}
                // 【点进去要用 recordId,不是快照自己的 id】快照是"这台设备在
                // 某次巡检里的状态",记录才是那次巡检本身。传错了详情页查不到东西。
                onClick={t.recordId ? () => nav(`/record?focus=${encodeURIComponent(t.recordId)}`) : undefined}
                style={{
                  display: "flex",
                  gap: 12,
                  alignItems: "baseline",
                  padding: "11px 6px",
                  borderTop: i ? `1px solid ${C.line}` : "none",
                  cursor: t.recordId ? "pointer" : "default",
                  fontSize: 13.5,
                }}
              >
                <span
                  style={{
                    color: C.textFaint,
                    fontVariantNumeric: "tabular-nums",
                    flex: "none",
                    minWidth: 128,
                  }}
                >
                  {fmtTime(t.createdAt)}
                </span>
                <Tag color={statusTagColor(t.status || "")}>{t.status || "已完成"}</Tag>
                <span style={{ flex: 1, minWidth: 0, color: C.textSub }}>{t.summary || "—"}</span>
                {t.inspector && (
                  <Typography.Text type="secondary" style={{ fontSize: 12, flex: "none" }}>
                    {t.inspector}
                  </Typography.Text>
                )}
              </div>
            ))}
          </div>
        )}
      </Card>
    </Space>
  );
}
