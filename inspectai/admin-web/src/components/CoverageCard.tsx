import { Card, Empty, Space, Tag, Typography } from "antd";
import ReactECharts from "echarts-for-react";
import { useMemo } from "react";
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
 * 【为什么这里的环形图站得住,而计划页那个不行】
 * 这里是四个有意义的类别(构成),环形图正是干这个的;
 * 计划页那个是"完成 / 未完成"两片 —— 人比较扇形角度很差,
 * 一个大数字比它强得多。图表要用在它比数字强的地方。
 *
 * 【"从未巡检"必须单独一档,不能并进"30 天以上"】两者是不同的问题:
 * 一台刚建档还没排上的设备,和一台巡过、然后被遗忘了三个月的设备,
 * 处理方式完全不同。并在一起就没法分头处理。
 */

type BucketKey = "fresh" | "aging" | "stale" | "never";

const BUCKETS: { key: BucketKey; label: string; color: string; hint: string }[] = [
  { key: "fresh", label: "7 天内", color: C.ok, hint: "正常节奏" },
  // 中间这一档刻意用灰:它不是问题,只是"该留意了"。
  // 给它颜色会和真正需要处理的两档抢注意力。
  { key: "aging", label: "8–30 天", color: "#c3ced6", hint: "该安排了" },
  { key: "stale", label: "超过 30 天", color: C.warn, hint: "已经掉队" },
  { key: "never", label: "从未巡检", color: C.danger, hint: "建了档但一次都没巡过" },
];

function daysSince(iso?: string): number | null {
  if (!iso) return null;
  const t = new Date(iso).getTime();
  // 【解析不出来当"从未"】历史数据里有格式不对的时间戳。当成"最近巡过"
  // 会把一台其实没人管的设备藏进绿色里 —— 这个方向的错更危险。
  if (Number.isNaN(t)) return null;
  return Math.floor((Date.now() - t) / 86400000);
}

export default function CoverageCard({ assets }: { assets: AssetEntry[] }) {
  const nav = useNavigate();

  const { counts, needAction } = useMemo(() => {
    const counts: Record<BucketKey, number> = { fresh: 0, aging: 0, stale: 0, never: 0 };
    const needAction: { asset: AssetEntry; days: number | null }[] = [];
    for (const a of assets) {
      const d = daysSince(a.lastInspectedAt);
      if (d === null) {
        counts.never++;
        needAction.push({ asset: a, days: null });
      } else if (d <= 7) counts.fresh++;
      else if (d <= 30) counts.aging++;
      else {
        counts.stale++;
        needAction.push({ asset: a, days: d });
      }
    }
    // 越久的排前面;"从未"排最前(days=null 视为无穷大)
    needAction.sort((x, y) => (y.days ?? 1e9) - (x.days ?? 1e9));
    return { counts, needAction };
  }, [assets]);

  if (!assets.length) {
    return (
      <Card title="巡检覆盖" size="small">
        <Empty image={Empty.PRESENTED_IMAGE_SIMPLE} description="台账里还没有设备" />
      </Card>
    );
  }

  const option = {
    tooltip: { trigger: "item", formatter: "{b}:{c} 台({d}%)" },
    legend: { bottom: 0, icon: "circle", itemWidth: 8, itemHeight: 8, textStyle: { fontSize: 12 } },
    series: [
      {
        type: "pie",
        // 【环形而不是实心饼】中间那个洞不是装饰:总数放在那里,
        // 读者不用把四段加起来才知道基数是多少。
        radius: ["52%", "76%"],
        center: ["50%", "44%"],
        avoidLabelOverlap: true,
        // 标签关掉:四段的名字图例里已经有了,再画一圈引线会把小卡片挤满
        label: { show: false },
        labelLine: { show: false },
        data: BUCKETS.map((b) => ({
          name: b.label,
          value: counts[b.key],
          itemStyle: { color: b.color, borderColor: "#fff", borderWidth: 2 },
        })),
      },
    ],
    graphic: {
      type: "text",
      left: "center",
      top: "38%",
      style: {
        text: String(assets.length),
        fontSize: 26,
        fontWeight: 700,
        fill: C.text,
        textAlign: "center",
      },
    },
  };

  return (
    <Card
      title="巡检覆盖"
      size="small"
      extra={
        <Typography.Text type="secondary" style={{ fontSize: 12 }}>
          按最近一次巡检距今
        </Typography.Text>
      }
    >
      <div
        style={{
          display: "grid",
          // 【左图右单】图说"整体什么样",单说"具体是哪几台"。
          // 只有图的话,看完只会得到一个感受;要能直接去处理才有用。
          gridTemplateColumns: "minmax(0, 260px) minmax(0, 1fr)",
          gap: 20,
          alignItems: "start",
        }}
      >
        <ReactECharts option={option} style={{ height: 220 }} notMerge />

        <div>
          {needAction.length === 0 ? (
            <div style={{ padding: "24px 0", color: C.textSub, fontSize: 13 }}>
              全部设备都在 30 天内巡过 —— 没有被遗忘的。
            </div>
          ) : (
            <>
              <Space size={8} style={{ marginBottom: 10 }} wrap>
                <span style={{ fontWeight: 600, color: C.text, fontSize: 13 }}>需要安排</span>
                <Tag color="orange">{needAction.length} 台</Tag>
              </Space>
              <div style={{ maxHeight: 190, overflowY: "auto" }}>
                {needAction.slice(0, 12).map(({ asset, days }) => (
                  <div
                    key={asset.id}
                    className="today-row"
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
                    <span style={{ fontWeight: 600, minWidth: 120 }}>
                      {asset.assetName || asset.assetKey}
                    </span>
                    <Typography.Text type="secondary" style={{ fontSize: 12, flex: 1 }}>
                      {[asset.project, asset.assetType].filter(Boolean).join(" · ")}
                    </Typography.Text>
                    {days === null ? (
                      <Tag color="red">从未巡检</Tag>
                    ) : (
                      <span style={{ color: C.warn, fontVariantNumeric: "tabular-nums" }}>
                        {days} 天前
                      </span>
                    )}
                  </div>
                ))}
              </div>
              {needAction.length > 12 && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  还有 {needAction.length - 12} 台,去台账里按状态筛
                </Typography.Text>
              )}
            </>
          )}
        </div>
      </div>
    </Card>
  );
}
