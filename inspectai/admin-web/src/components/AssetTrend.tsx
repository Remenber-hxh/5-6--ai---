import { Empty, Space, Tag, Typography, message } from "antd";
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

export default function AssetTrend({ assetId }: { assetId: string }) {
  const [data, setData] = useState<AssetTrendData | null>(null);
  const [loading, setLoading] = useState(false);

  const load = useCallback(async (id: string) => {
    setLoading(true);
    try {
      setData(await getAssetTrend(id));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "趋势加载失败");
      setData(null);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (assetId) void load(assetId);
  }, [assetId, load]);

  if (loading && !data) {
    return <div style={{ color: C.textFaint, fontSize: 13, padding: "12px 0" }}>加载中…</div>;
  }
  if (!data) return null;

  // 【三种"没图"要分开说】它们要人做的事完全不同:
  //   模板没数值字段 → 去改模板
  //   有字段还没攒够 → 再巡几次就有了
  //   有图           → 看图
  // 合成一句"暂无趋势数据"等于什么都没说。
  if (!data.hasNumericField) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="这类设备的模板里没有数值字段,画不出读数趋势"
        style={{ padding: "18px 0" }}
      />
    );
  }
  if (data.series.length === 0) {
    return (
      <div style={{ color: C.textSub, fontSize: 13, padding: "14px 0" }}>
        读数还不够画趋势 —— 至少要 3 次巡检。
        {data.singleReading?.length ? (
          <>
            {" "}
            已有读数的字段:
            <Typography.Text type="secondary">{data.singleReading.join("、")}</Typography.Text>
          </>
        ) : null}
      </div>
    );
  }

  return (
    <Space direction="vertical" size={10} style={{ width: "100%" }}>
      {data.series.map((s) => (
        <Chart key={s.fieldKey} s={s} />
      ))}
    </Space>
  );
}
