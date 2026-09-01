import { Card, Space, Tag, message } from "antd";
import ReactECharts from "echarts-for-react";
import { useCallback, useEffect, useState } from "react";

import { AssetTrend as AssetTrendData, TrendSeries, getAssetTrend } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 单台设备的读数趋势。
 *
 * 【这是 AI 巡检相对人工的真正增量】慢性劣化单次巡检永远看不出来:
 * 水箱水位每周降一点、电池电压每月低一点、控制柜温度悄悄爬上去 ——
 * 每一次巡检看都"正常",连起来看才是问题。
 *
 * 数据底座(field_observations)早就在攒,只是一直没有界面读它。
 */

function fmtDay(iso: string) {
  // 只要月-日:一条曲线上十几个点,带年份会挤成一团
  return (iso || "").slice(5, 10);
}

function fmtNum(v: number) {
  // 读数常见到小数点后两位;整数不补零,免得 20 显示成 20.00
  return Number.isInteger(v) ? String(v) : v.toFixed(2);
}

function Chart({ s }: { s: TrendSeries }) {
  const option = {
    grid: { left: 44, right: 14, top: 18, bottom: 26 },
    xAxis: {
      type: "category",
      data: s.points.map((p) => fmtDay(p.at)),
      axisLine: { lineStyle: { color: C.line } },
      axisTick: { show: false },
      axisLabel: { color: C.textFaint, fontSize: 11 },
    },
    yAxis: {
      type: "value",
      scale: true,
      splitLine: { lineStyle: { color: C.line } },
      axisLabel: { color: C.textFaint, fontSize: 11 },
    },
    tooltip: {
      trigger: "axis",
      formatter: (ps: { dataIndex: number }[]) => {
        const p = s.points[ps[0].dataIndex];
        return `${p.at.slice(0, 16).replace("T", " ")}<br/>${s.fieldLabel}:<b>${fmtNum(p.value)}</b>`;
      },
    },
    series: [
      {
        type: "line",
        data: s.points.map((p) => p.value),
        smooth: false,
        showSymbol: true,
        symbolSize: 6,
        lineStyle: { width: 2, color: s.drifting ? C.warn : C.ok },
        // 【异常点单独染色,不换整条线】整条线变红会让人以为这台设备一直有问题;
        // 真相是只有那几次读数偏了。
        itemStyle: {
          color: (p: { dataIndex: number }) =>
            s.points[p.dataIndex].outlier ? C.danger : s.drifting ? C.warn : C.ok,
        },
        areaStyle: { opacity: 0.06, color: s.drifting ? C.warn : C.ok },
        // 基线画一条虚线 —— 没有它,人看不出"这次比平时高多少"
        markLine: {
          silent: true,
          symbol: "none",
          label: { formatter: "平时", color: C.textFaint, fontSize: 10, position: "insideEndTop" },
          lineStyle: { type: "dashed", color: C.textFaint, width: 1 },
          data: [{ yAxis: s.baseline }],
        },
      },
    ],
  };

  return (
    <div style={{ border: `1px solid ${C.line}`, borderRadius: 10, padding: "12px 14px" }}>
      <div style={{ display: "flex", alignItems: "baseline", gap: 8, flexWrap: "wrap" }}>
        <span style={{ fontWeight: 600, fontSize: 13.5 }}>{s.fieldLabel}</span>
        {s.drifting && <Tag color="orange">偏离平时</Tag>}
        <span
          style={{
            marginLeft: "auto",
            fontSize: 12,
            color: C.textFaint,
            fontVariantNumeric: "tabular-nums",
          }}
        >
          最新 <b style={{ color: s.drifting ? C.warn : C.text }}>{fmtNum(s.latest)}</b>
          {" · 平时 "}
          {fmtNum(s.baseline)}
          {typeof s.deviation === "number" && Math.abs(s.deviation) >= 1 && (
            <>
              {" · "}
              <span style={{ color: s.drifting ? C.warn : C.textSub }}>
                {s.deviation > 0 ? "+" : ""}
                {s.deviation.toFixed(0)}%
              </span>
            </>
          )}
        </span>
      </div>
      <ReactECharts option={option} style={{ height: 150 }} notMerge />
    </div>
  );
}

/**
 * 【标题和卡片都由这个组件自己出】没有趋势时整块要消失 ——
 * 标题或卡片写在外面的话,组件返回 null 也挡不住它,
 * 屏幕上会剩一个空的"读数趋势"框,比不显示更难看。
 *
 * heading:轻量标题(侧边抽屉用)。card:整张卡片(整页用)。
 */
export default function AssetTrend({
  assetId,
  heading,
  card,
  style,
}: {
  assetId: string;
  heading?: string;
  card?: boolean;
  // style:外间距也要交给组件自己出。
  //
  // 【为什么不能包一层 div 加 marginTop】组件返回 null 时那层 div 还在,
  // 16px 的上边距照样占着 —— 上下两个按钮之间就凭空多出一截空白,
  // 而屏幕上什么都没有。电梯类设备全是是/否项、根本没有数值趋势,
  // 于是这个空隙在大多数设备上都能看到。
  style?: React.CSSProperties;
}) {
  const [data, setData] = useState<AssetTrendData | null>(null);

  const load = useCallback(async (id: string) => {
    try {
      setData(await getAssetTrend(id));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "趋势加载失败");
      setData(null);
    }
  }, []);

  useEffect(() => {
    if (assetId) void load(assetId);
  }, [assetId, load]);

  // 【没有趋势就什么都不渲染】不给标题、不给卡片、不给"暂无数据"。
  //
  // 大多数设备的模板本来就没有数值字段(电梯是一堆是/否项),
  // 给它们每台都摆一句"画不出趋势"只是占地方 —— 那句话既不需要人做什么,
  // 也不是他打开这一页想知道的事。有图才说话。
  if (!data || data.series.length === 0) return null;

  const body = (
    <Space direction="vertical" size={10} style={{ width: "100%", ...(card ? undefined : style) }}>
      {heading && <div style={{ fontWeight: 600 }}>{heading}</div>}
      {data.series.map((s) => (
        <Chart key={s.fieldKey} s={s} />
      ))}
    </Space>
  );

  return card ? (
    <Card size="small" title="读数趋势" style={style}>
      {body}
    </Card>
  ) : (
    body
  );
}
