import { ArrowLeftOutlined } from "@ant-design/icons";
import { Button, Card, Empty, Skeleton, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import { AssetEntry, AssetSnapshotEntry, listAssetSnapshots, listAssets } from "../api/mgmt";
import AssetOpenItems from "../components/AssetOpenItems";
import AssetPhotoTimeline from "../components/AssetPhotoTimeline";
import AssetProfileCard from "../components/AssetProfileCard";
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
 *
 * ── 版面 ──
 *
 * 【一块判断 + 两栏】以前是六张等重的通栏卡片自上而下堆着,眼睛没有入口:
 * 顶上一块红色 Alert 说"1 项未了结",六十像素外又一句"需要处理 · 1 项已逾期" ——
 * 同一件事用两种视觉语言说两遍,结果两块都变弱。现在判断合成一块摆在最上面,
 * 红色只出现一次。
 *
 * 【事实归右栏且粘顶】设备档案只有五行短字段,摊在 1600px 通栏里九成是空白;
 * 而它恰恰是读下面那些东西的前提 —— 知道维保周期 5 天、投运不到一年,
 * 再去看轨迹和照片才知道该不该紧张。所以它跟着滚动一直在视野里。
 */

/** 距今多少天。【零值时间戳要挡】Go 的空时间序列化成 0001-01-01,
 *  直接算会得到七十多万天 —— 这一页上已经出现过一次"739855 天前"。 */
function daysAgo(iso?: string): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t) || t < Date.UTC(2000, 0, 1)) return null;
  const d = Math.floor((Date.now() - t) / 86400000);
  return d >= 0 ? d : null;
}

function agoText(iso?: string): string {
  const d = daysAgo(iso);
  if (d === null) return "";
  if (d === 0) return "今天";
  if (d === 1) return "昨天";
  return `${d} 天前`;
}

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

  const meta: string[] = [];
  if (asset.lastInspectedAt) {
    const ago = agoText(asset.lastInspectedAt);
    meta.push(`最近巡检 ${fmtTime(asset.lastInspectedAt)}${ago ? `(${ago})` : ""}`);
  } else {
    meta.push("从未巡检");
  }
  if (trail.length > 0) meta.push(`历史 ${trail.length} 次`);

  return (
    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
      <Button type="text" icon={<ArrowLeftOutlined />} onClick={() => nav("/ledger")} style={{ paddingLeft: 0, alignSelf: "flex-start" }}>
        返回台账
      </Button>

      {/* ══ 它现在要不要管 ══
          【身份 + 判断 + 未了结,合成一块】这三样回答的是同一个问题。
          拆成三块的话,人得在"这是什么设备""它怎么了""有谁在处理"
          之间来回跳,而它们本来就是一句话。 */}
      <Card size="small">
        <div style={{ display: "flex", gap: 20, flexWrap: "wrap", alignItems: "flex-start" }}>
          {cover && (
            <img
              src={mediaUrl(cover)}
              alt=""
              style={{ width: 176, height: 132, objectFit: "cover", borderRadius: 10, flex: "none" }}
            />
          )}
          <div style={{ flex: 1, minWidth: 280, display: "flex", flexDirection: "column", gap: 10 }}>
            <div style={{ display: "flex", alignItems: "baseline", gap: 10, flexWrap: "wrap" }}>
              <h2 style={{ margin: 0, fontSize: 22, letterSpacing: "-0.01em" }}>
                {asset.assetName || asset.assetKey}
              </h2>
              <Tag color={statusTagColor(asset.lastStatus || "")}>{asset.lastStatus || "未知"}</Tag>
            </div>

            {/* 【身份降成一行细字】项目/类型/编号是查证用的,不是判断用的。
                以前它们是三个和状态同重的 Tag,和上面那个真正要紧的状态标签
                抢注意力 —— 一排四个方块,人分不出哪个是结论。 */}
            <div style={{ fontSize: 12.5, color: C.textFaint, display: "flex", gap: 8, flexWrap: "wrap" }}>
              {[asset.project, asset.assetType, asset.assetKey && `编号 ${asset.assetKey}`]
                .filter(Boolean)
                .map((s, i) => (
                  <span key={i}>
                    {i > 0 && <span style={{ marginRight: 8, color: C.muted }}>·</span>}
                    {s}
                  </span>
                ))}
            </div>

            {/* ── 判断区:结论做标题,未了结的事做明细,红色只出现一次 ── */}
            <div
              style={{
                borderTop: `1px solid ${C.line}`,
                paddingTop: 10,
                display: "flex",
                flexDirection: "column",
                gap: 8,
              }}
            >
              <AssetVerdict assetId={asset.id} />
              <AssetOpenItems assetId={asset.id} bare />
            </div>

            <div
              style={{
                fontSize: 12.5,
                color: C.textFaint,
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {meta.join(" · ")}
            </div>

            {asset.lastSummary && <Summary text={asset.lastSummary} />}
          </div>
        </div>
      </Card>

      {/* ══ 主栏叙事 + 右栏事实 ══ */}
      <div
        style={{
          display: "flex",
          gap: 14,
          alignItems: "flex-start",
          flexWrap: "wrap",
        }}
      >
        <div style={{ flex: "1 1 560px", minWidth: 0, display: "flex", flexDirection: "column", gap: 14 }}>
          {/* 【趋势排在轨迹前面】轨迹说"巡过几次、每次什么状态",
              趋势说"这台设备在往哪个方向走" —— 后者是单次巡检永远看不出来的。
              没有数值字段的设备(电梯这类全是是/否项)整块不显示。 */}
          <AssetTrend assetId={asset.id} card />

          {/* 【照片在曲线之后、轨迹之前】曲线是给懂读数的人看的,
              照片是给所有人看的 —— 但它不能顶到最上面:先"要不要管",
              再"哪里在变",最后才是"长什么样"。 */}
          <AssetPhotoTimeline assetId={asset.id} />

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
        </div>

        {/* ── 右栏:事实 ──
            【粘顶】它是读左边那些东西的前提 —— 知道维保周期 5 天、
            投运不到一年,再看轨迹和照片才知道该不该紧张。滚下去就看不到的话,
            人得往回翻,而翻回来时已经忘了自己想核对什么。 */}
        <div style={{ flex: "0 1 320px", minWidth: 260, position: "sticky", top: 12 }}>
          <AssetProfileCard asset={asset} onSaved={setAsset} />
        </div>
      </div>
    </div>
  );
}

/**
 * 巡检摘要:默认两行,点开看全文。
 *
 * 【为什么要收起来】这段话是模型写的,结构固定:先把十来项正常的逐个念一遍,
 * 把唯一异常的那句放在最后("……均正常;但'设备无异响、异味'项为'否'")。
 * 全文摊开的话,页面上最长的一段文字讲的几乎全是"没事",而真正要紧的半句
 * 排在末尾 —— 眼睛得走完整段才找得到它。
 *
 * 【不自动截断句子】按字数硬切会把"存在异响"切成"存在异",比不截更糟;
 * 用 CSS 行数裁剪,展开后一个字不少。
 */
function Summary({ text }: { text: string }) {
  const [open, setOpen] = useState(false);
  // 【短摘要不给按钮】两行以内的文字后面挂一个"展开",点下去什么都不变。
  const long = text.length > 88;

  return (
    <div style={{ fontSize: 13.5, lineHeight: 1.75, color: C.textSub }}>
      <div
        style={
          open || !long
            ? undefined
            : {
                display: "-webkit-box",
                WebkitLineClamp: 2,
                WebkitBoxOrient: "vertical",
                overflow: "hidden",
              }
        }
      >
        {text}
      </div>
      {long && (
        <a
          onClick={() => setOpen((v) => !v)}
          style={{ fontSize: 12.5, color: C.textFaint, cursor: "pointer" }}
        >
          {open ? "收起" : "展开全文"}
        </a>
      )}
    </div>
  );
}
