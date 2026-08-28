import { Card, Empty, Space, Tag, Typography } from "antd";
import ReactECharts from "echarts-for-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { AssetEntry } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 巡检覆盖:按「最近一次巡检距今多久」把台账分档。
 *
 * 【为什么是这个问题,而不是"完成率"】完成率回答"排的活干完没有",
 * 而它只覆盖被排进计划的设备。真正会出事的是【没被任何计划盯上的那几台】——
 * 它们不出现在任何完成率里,所以完成率 100% 的同时可能有六台三个月没人碰过。
 *
 * 【为什么这里的环形图站得住】四个有意义的类别 = 构成,环形图正是干这个的。
 * 而"完成 / 未完成"两片不该用饼:人比较扇形角度很差,一个大数字比它强。
 *
 * 【图和下面的清单是同一份数据】点图上任一段,下面就只留那一段的设备。
 * 图说"整体什么样",清单说"具体是哪几台" —— 两者必须能对上,
 * 否则看图得到一个印象、看单得到另一个,人不知道该信哪个。
 */

type BucketKey = "fresh" | "aging" | "stale" | "never";

const BUCKETS: { key: BucketKey; label: string; color: string }[] = [
  { key: "fresh", label: "7 天内", color: C.ok },
  // 中间这一档刻意用灰:它不是问题,只是"该留意了"。
  // 给它颜色会和真正需要处理的两档抢注意力。
  { key: "aging", label: "8–30 天", color: "#c3ced6" },
  { key: "stale", label: "超过 30 天", color: C.warn },
  { key: "never", label: "从未巡检", color: C.danger },
];

/**
 * 距今多少天;从未巡检返回 null。
 *
 * 【零值时间戳必须当成"从未"】Go 把没巡过的设备序列化成
 * "0001-01-01T00:00:00Z",而 new Date() 能正常解析它 —— 于是算出来是
 * 739855 天前,那台设备被归进「超过 30 天」而不是「从未巡检」。
 * 界面上显示 "739855 天前",环形图里一大片橙色全是假的。
 * 这个坑不报错,只是数字荒谬到得有人盯着看才会发现(用户截图里就是它)。
 */
function daysSince(iso?: string): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  if (Number.isNaN(t)) return null;
  // 2000 年以前的一律当零值 —— 真实巡检记录不可能早于这个系统存在的年份
  if (t < Date.UTC(2000, 0, 1)) return null;
  return Math.max(0, Math.floor((Date.now() - t) / 86400000));
}

function bucketOf(days: number | null): BucketKey {
  if (days === null) return "never";
  if (days <= 7) return "fresh";
  if (days <= 30) return "aging";
  return "stale";
}

export default function CoverageCard({
  assets,
  compact,
}: {
  assets: AssetEntry[];
  /** 窄栏里用:图在上、名单在下。不是缩小,是换排布 —— 396px 里左右并排两块都读不了 */
  compact?: boolean;
}) {
  const nav = useNavigate();
  // 点图上某一段就只看那一段,再点一次取消。
  // 【图本身就是筛选器】不另设"清除筛选"按钮 —— 少一个控件少一分学习成本。
  const [pick, setPick] = useState<BucketKey | null>(null);

  const { counts, rows } = useMemo(() => {
    const counts: Record<BucketKey, number> = { fresh: 0, aging: 0, stale: 0, never: 0 };
    const rows: { asset: AssetEntry; days: number | null; bucket: BucketKey }[] = [];
    for (const a of assets) {
      const days = daysSince(a.lastInspectedAt);
      const bucket = bucketOf(days);
      counts[bucket]++;
      rows.push({ asset: a, days, bucket });
    }
    // 越久没巡的排前面;"从未"排最前(null 视为无穷大)
    rows.sort((x, y) => (y.days ?? 1e9) - (x.days ?? 1e9));
    return { counts, rows };
  }, [assets]);

  const shown = pick ? rows.filter((r) => r.bucket === pick) : rows;

  if (!assets.length) {
    return (
      <Card title="巡检覆盖" size="small">
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="台账里还没有设备" />
      </Card>
    );
  }

  const option = {
    tooltip: { trigger: "item", formatter: "{b}:{c} 台({d}%)" },
    // 【窄栏里图例必须能换行】396px 的侧栏放不下四项一行 ——
    // 不给 width 的话 echarts 不换行,直接把「从未巡检」整项截掉,
    // 而那恰恰是最要紧的一档。给了宽度它才会折成两行。
    legend: {
      bottom: 0,
      icon: "circle",
      itemWidth: 8,
      itemHeight: 8,
      itemGap: compact ? 10 : 14,
      width: compact ? "100%" : undefined,
      padding: [10, 0, 0, 0],
      textStyle: { fontSize: compact ? 11 : 12, color: "#5b6b78" },
    },
    series: [
      {
        type: "pie",
        // 【环形而不是实心饼】中间那个洞不是装饰:总数放在那里,
        // 读者不用把四段加起来才知道基数是多少。
        radius: ["52%", "76%"],
        center: ["50%", compact ? "38%" : "40%"],
        avoidLabelOverlap: true,
        label: { show: false },
        labelLine: { show: false },
        // 选中某一段时其余段压暗 —— 让"现在在看哪一段"和下面的清单对得上
        data: BUCKETS.map((b) => ({
          name: b.label,
          value: counts[b.key],
          itemStyle: {
            color: b.color,
            borderColor: "#fff",
            borderWidth: 2,
            opacity: !pick || pick === b.key ? 1 : 0.25,
          },
        })),
      },
    ],
    graphic: {
      type: "text",
      left: "center",
      top: compact ? "32%" : "34%",
      style: {
        // 选了某一段就显示那一段的台数 —— 和下面清单的条数对得上
        text: String(pick ? counts[pick] : assets.length),
        fontSize: 26,
        fontWeight: 700,
        fill: C.text,
        textAlign: "center",
      },
    },
  };

  const onChartClick = (e: { name?: string }) => {
    const hit = BUCKETS.find((b) => b.label === e.name);
    if (!hit) return;
    setPick((cur) => (cur === hit.key ? null : hit.key));
  };

  return (
    <Card title="巡检覆盖" size="small">
      <div
        style={{
          display: "grid",
          gridTemplateColumns: compact ? "minmax(0, 1fr)" : "minmax(0, 260px) minmax(0, 1fr)",
          // 【窄栏里图和清单之间要拉开】原来只有 4px,图例最后一行
          // 紧贴着第一条设备,读起来像图例也是清单的一行。
          gap: compact ? 18 : 20,
          alignItems: "start",
        }}
      >
        <ReactECharts
          option={option}
          style={{ height: compact ? 212 : 220, cursor: "pointer" }}
          onEvents={{ click: onChartClick }}
          notMerge
        />

        <div>
          {pick && (
            <Space size={8} style={{ marginBottom: 8 }}>
              {/* 【只留一个动作,不复述当前筛的是哪一档】那件事图上已经说了:
                  选中的那段是亮的、其余压暗,中心数字也跟着变。
                  再用一行字说一遍,是把同一个事实讲了两遍。 */}
              <Tag
                color={BUCKETS.find((b) => b.key === pick)?.color}
                style={{ cursor: "pointer" }}
                onClick={() => setPick(null)}
              >
                展开全部
              </Tag>
            </Space>
          )}
          <div className="cov-list" style={{ maxHeight: compact ? 260 : 190, overflowY: "auto" }}>
            {shown.map(({ asset, days, bucket }) => (
              <div
                key={asset.id}
                className="hl-row"
                // 【左右两边都跳到同一台设备的台账详情】这一屏做的所有事
                // 最后都落在"去看那台设备",两边的落点必须一致。
                onClick={() => nav(`/ledger?focus=${encodeURIComponent(asset.id)}`)}
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 10,
                  padding: "8px 4px",
                  borderBottom: `1px solid ${C.line}`,
                  cursor: "pointer",
                  fontSize: 13,
                }}
              >
                <span
                  style={{
                    width: 6,
                    height: 6,
                    flex: "0 0 6px",
                    borderRadius: 3,
                    background: BUCKETS.find((b) => b.key === bucket)?.color,
                  }}
                />
                <span style={{ fontWeight: 600, minWidth: compact ? 80 : 120 }}>
                  {asset.assetName || asset.assetKey}
                </span>
                <Typography.Text type="secondary" style={{ fontSize: 12, flex: 1 }} ellipsis>
                  {asset.assetType || ""}
                </Typography.Text>
                {days === null ? (
                  <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                    从未巡检
                  </Typography.Text>
                ) : (
                  <span style={{ color: C.textSub, fontVariantNumeric: "tabular-nums" }}>
                    {days} 天前
                  </span>
                )}
              </div>
            ))}
          </div>
        </div>
      </div>
    </Card>
  );
}
