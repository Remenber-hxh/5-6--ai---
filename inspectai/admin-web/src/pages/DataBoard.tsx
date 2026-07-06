import { Card, Col, Drawer, Progress, Row, Statistic, Table, Tag } from "antd";
import ReactECharts from "echarts-for-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { AttentionItem, listAttention, listRecords } from "../api/mgmt";
import { api } from "../api/client";
import { InspectionRecord, fmtTime, recordBusinessStatus } from "../lib/status";

interface Overview {
  assetTotal?: number;
  recordRecent?: number;
  abnormalRecent?: number;
  pendingReviews?: number;
  pendingApprovals?: number;
  lazyConfirmRate?: number;
}

const riskTag = (level?: string) =>
  level === "danger" ? (
    <Tag color="red">高风险</Tag>
  ) : level === "warning" ? (
    <Tag color="orange">需关注</Tag>
  ) : (
    <Tag color="green">正常</Tag>
  );

// 数据看板:总览 + 近30天趋势(ECharts)+ 重点关注(风险分可点开评分卡)
export default function DataBoard() {
  const nav = useNavigate();
  const [ov, setOv] = useState<Overview>({});
  const [attention, setAttention] = useState<AttentionItem[]>([]);
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [riskCard, setRiskCard] = useState<AttentionItem | null>(null);

  useEffect(() => {
    api<{ overview?: Overview }>("/api/management-ai/snapshot?range=30d")
      .then((d) => setOv(d.overview || {}))
      .catch(() => void 0);
    listAttention(8).then(setAttention).catch(() => void 0);
    listRecords().then(setRecords).catch(() => void 0);
  }, []);

  // 近 30 天按日聚合:巡检量 / 异常量(客户端聚合,与旧版口径一致)
  const trendOption = useMemo(() => {
    const days: string[] = [];
    const total: number[] = [];
    const abnormal: number[] = [];
    for (let i = 29; i >= 0; i--) {
      const d = new Date();
      d.setDate(d.getDate() - i);
      const key = d.toISOString().slice(0, 10);
      days.push(key.slice(5));
      const daily = records.filter((r) => (r.createdAt || "").slice(0, 10) === key);
      total.push(daily.length);
      abnormal.push(daily.filter((r) => recordBusinessStatus(r) === "异常").length);
    }
    return {
      tooltip: { trigger: "axis" },
      legend: { data: ["巡检量", "异常量"], top: 4, right: 8 },
      grid: { left: 40, right: 16, top: 36, bottom: 28 },
      xAxis: { type: "category", data: days, axisLabel: { interval: 4 } },
      yAxis: { type: "value", minInterval: 1 },
      series: [
        { name: "巡检量", type: "line", smooth: true, data: total, color: "#12a968", areaStyle: { opacity: 0.08 } },
        { name: "异常量", type: "line", smooth: true, data: abnormal, color: "#ef4444" },
      ],
    };
  }, [records]);

  const cards = [
    { title: "资产总数", value: ov.assetTotal },
    { title: "近 30 天巡检", value: ov.recordRecent },
    { title: "近 30 天异常", value: ov.abnormalRecent },
    { title: "待复核 / 待审批", value: `${ov.pendingReviews ?? 0} / ${ov.pendingApprovals ?? 0}` },
  ];

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        {cards.map((c) => (
          <Col span={6} key={c.title}>
            <Card size="small">
              <Statistic title={c.title} value={c.value ?? "—"} />
            </Card>
          </Col>
        ))}
      </Row>
      <Card title="近 30 天巡检趋势" style={{ marginBottom: 16 }} size="small">
        <ReactECharts option={trendOption} style={{ height: 260 }} notMerge />
      </Card>
      <Card title="近期重点关注" size="small">
        <Table<AttentionItem>
          rowKey="assetId"
          size="small"
          dataSource={attention}
          pagination={false}
          columns={[
            { title: "设备", dataIndex: "assetName" },
            {
              title: "风险分",
              width: 110,
              render: (_, a) => (
                <a onClick={() => setRiskCard(a)} title="点击看评分依据">
                  {a.riskScore ?? "—"} / 100
                </a>
              ),
            },
            { title: "等级", width: 90, render: (_, a) => riskTag(a.riskLevel) },
            { title: "原因", render: (_, a) => (a.reasons || []).slice(0, 2).join("；") },
            {
              title: "操作",
              width: 100,
              render: (_, a) => <a onClick={() => nav(`/ledger?focus=${encodeURIComponent(a.assetId)}`)}>查看台账</a>,
            },
          ]}
        />
      </Card>
      <Drawer title="风险评分卡" open={!!riskCard} width={420} onClose={() => setRiskCard(null)}>
        {riskCard && (
          <div>
            <div style={{ display: "flex", alignItems: "baseline", gap: 10, marginBottom: 16 }}>
              <b style={{ fontSize: 15 }}>{riskCard.assetName}</b>
              <span style={{ fontSize: 24, fontWeight: 700 }}>
                {riskCard.riskScore}
                <small style={{ color: "#8aa0b0", fontWeight: 400 }}> /100</small>
              </span>
              {riskTag(riskCard.riskLevel)}
            </div>
            {(riskCard.breakdown || []).map((f) => (
              <div key={f.label} style={{ marginBottom: 14 }}>
                <div style={{ display: "flex", justifyContent: "space-between", fontSize: 13 }}>
                  <span>{f.label}</span>
                  <b>
                    {f.score}
                    <small style={{ color: "#8aa0b0", fontWeight: 400 }}> /{f.max}</small>
                  </b>
                </div>
                <Progress
                  percent={f.max ? Math.round((f.score / f.max) * 100) : 0}
                  showInfo={false}
                  strokeColor={{ from: "#f59e0b", to: "#ef4444" }}
                  size="small"
                />
                <div style={{ fontSize: 12, color: "#64748b" }}>{f.basis || "—"}</div>
              </div>
            ))}
            <div style={{ fontSize: 11.5, color: "#8aa0b0", borderTop: "1px solid #eef1f5", paddingTop: 10 }}>
              百分制:≥70 高风险,40~69 需关注;识别质量(补拍/人工修正)不计入设备风险。生成于 {fmtTime(new Date().toISOString(), true)}
            </div>
          </div>
        )}
      </Drawer>
    </div>
  );
}
