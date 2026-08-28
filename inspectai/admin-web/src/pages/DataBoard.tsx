import { Card, Col, Drawer, Progress, Row, Table, Tag } from "antd";
import ReactECharts from "echarts-for-react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { AssetEntry, AttentionItem, InspectorQualityRow, RepeatedIssue, listAssets, listAttention, listRecords } from "../api/mgmt";
import { useUi } from "../store/ui";
import { api } from "../api/client";
import CountUp from "../components/CountUp";
import CoverageCard from "../components/CoverageCard";
import { InspectionRecord, fmtTime, recordBusinessStatus } from "../lib/status";

interface Overview {
  assetTotal?: number;
  recordRecent?: number;
  abnormalRecent?: number;
  pendingReviews?: number;
  pendingApprovals?: number;
  lazyConfirmRate?: number;
}

interface DriftEntry {
  assetId?: string;
  assetName?: string;
  fieldKey?: string;
  fieldLabel?: string;
  changeRate?: number;
}

const legendDot = (bg: string): React.CSSProperties => ({
  display: "inline-block",
  width: 10,
  height: 10,
  borderRadius: 2,
  background: bg,
  margin: "0 4px 0 10px",
  verticalAlign: "-1px",
});

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
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [drifts, setDrifts] = useState<DriftEntry[]>([]);
  const [summary, setSummary] = useState("");
  const [repeated, setRepeated] = useState<RepeatedIssue[]>([]);
  const [quality, setQuality] = useState<InspectorQualityRow[]>([]);
  const { project } = useUi();
  const [riskCard, setRiskCard] = useState<AttentionItem | null>(null);

  useEffect(() => {
    const q = project ? "&project=" + encodeURIComponent(project) : "";
    api<{
      overview?: Overview;
      numericDrifts?: DriftEntry[];
      repeatedIssues?: RepeatedIssue[];
      inspectorQuality?: InspectorQualityRow[];
    }>("/api/management-ai/snapshot?range=30d" + q)
      .then((d) => {
        setOv(d.overview || {});
        setDrifts(d.numericDrifts || []);
        setRepeated(d.repeatedIssues || []);
        setQuality(d.inspectorQuality || []);
      })
      .catch(() => void 0);
    listAttention(8)
      .then((d) => {
        setAttention(d.items);
        setSummary(d.summary);
      })
      .catch(() => void 0);
    listRecords().then(setRecords).catch(() => void 0);
    // 台账用于「巡检覆盖」。取不到就让那张卡显示空态,不拖垮整页。
    listAssets().then(setAssets).catch(() => void 0);
  }, [project]);

  // 【覆盖卡也要跟顶栏的项目筛选走】页面上别的块都筛了,这一块不筛的话
  // 同一屏里两个数字对不上,而看的人只会觉得"这系统数字乱"。
  const scopedAssets = useMemo(
    () => assets.filter((a) => !project || a.project === project),
    [assets, project],
  );

  // 状态热力图:近 30 天每日格,按当日最差业务状态着色
  const heatCells = useMemo(() => {
    const cells: { day: string; count: number; level: 0 | 1 | 2 | 3 }[] = [];
    for (let i = 29; i >= 0; i--) {
      const d = new Date();
      d.setDate(d.getDate() - i);
      const key = d.toISOString().slice(0, 10);
      const daily = records.filter((r) => (!project || r.project === project) && (r.createdAt || "").slice(0, 10) === key);
      let level: 0 | 1 | 2 | 3 = daily.length ? 1 : 0;
      const statuses = daily.map(recordBusinessStatus);
      if (statuses.some((s) => s === "待复核" || s === "需补图")) level = 2;
      if (statuses.some((s) => s === "异常")) level = 3;
      cells.push({ day: key.slice(5), count: daily.length, level });
    }
    return cells;
  }, [records, project]);

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
      const daily = records.filter((r) => (!project || r.project === project) && (r.createdAt || "").slice(0, 10) === key);
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
  }, [records, project]);

  const cards: { title: string; num?: number; text?: string }[] = [
    { title: "资产总数", num: ov.assetTotal },
    { title: "近 30 天巡检", num: ov.recordRecent },
    { title: "近 30 天异常", num: ov.abnormalRecent },
    { title: "待复核 / 待审批", text: `${ov.pendingReviews ?? 0} / ${ov.pendingApprovals ?? 0}` },
  ];

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        {cards.map((c) => (
          <Col span={6} key={c.title}>
            <Card size="small">
              <div style={{ color: "rgba(0,0,0,0.45)", fontSize: 14, marginBottom: 4 }}>{c.title}</div>
              <div style={{ fontSize: 24, color: "rgba(0,0,0,0.88)" }}>
                {c.text !== undefined ? c.text : c.num !== undefined ? <CountUp value={c.num} /> : "—"}
              </div>
            </Card>
          </Col>
        ))}
      </Row>
      {/* 【放在趋势图上面】趋势回答"最近干了多少",覆盖回答"有没有被漏掉的"。
          后者是更容易出事、也更少被问到的那一个,所以给它更靠前的位置。 */}
      <div style={{ marginBottom: 16 }}>
        <CoverageCard assets={scopedAssets} />
      </div>

      <Card title="近 30 天巡检趋势" style={{ marginBottom: 16 }} size="small">
        <ReactECharts option={trendOption} style={{ height: 260 }} notMerge />
      </Card>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        <Col span={14}>
          <Card title="设备状态热力图(近 30 天)" size="small">
            <div style={{ display: "flex", gap: 4, flexWrap: "wrap", padding: "6px 0" }}>
              {heatCells.map((c) => (
                <div
                  key={c.day}
                  title={`${c.day} · 巡检 ${c.count} 次`}
                  style={{
                    width: 22,
                    height: 22,
                    borderRadius: 4,
                    background:
                      c.level === 3
                        ? "#ef4444"
                        : c.level === 2
                          ? "#f59e0b"
                          : c.level === 1
                            ? "#12a968"
                            : "#eef1f5",
                    opacity: c.level === 1 ? Math.min(0.35 + c.count * 0.12, 1) : 1,
                  }}
                />
              ))}
            </div>
            <div style={{ fontSize: 12, color: "#8aa0b0" }}>
              <span style={legendDot("#eef1f5")} /> 无巡检 <span style={legendDot("#12a968")} /> 正常{" "}
              <span style={legendDot("#f59e0b")} /> 待复核/补图 <span style={legendDot("#ef4444")} /> 有异常
            </div>
          </Card>
        </Col>
        <Col span={10}>
          <Card title="数值字段漂移" size="small">
            {drifts.length === 0 ? (
              <div style={{ color: "#8aa0b0", fontSize: 13, padding: "8px 0" }}>近期无明显数值漂移</div>
            ) : (
              drifts.slice(0, 6).map((d, i) => {
                const pct = Math.round((d.changeRate || 0) * 100);
                return (
                  <div
                    key={`${d.assetId}_${d.fieldKey}_${i}`}
                    style={{
                      display: "flex",
                      justifyContent: "space-between",
                      padding: "7px 0",
                      borderBottom: "1px solid #f0f2f5",
                      fontSize: 13,
                    }}
                  >
                    <span>
                      {d.assetName} · {d.fieldLabel || d.fieldKey}
                    </span>
                    <b style={{ color: Math.abs(pct) >= 10 ? "#d4380d" : "#5b6b78" }}>
                      {pct > 0 ? "+" : ""}
                      {pct}%
                    </b>
                  </div>
                );
              })
            )}
          </Card>
        </Col>
      </Row>
      {summary && (
        <Card size="small" style={{ marginBottom: 16 }}>
          <b style={{ color: "#12a968" }}>AI 洞察:</b> {summary}
        </Card>
      )}
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
      <Row gutter={16} style={{ marginTop: 16 }}>
        <Col span={12}>
          <Card title="重复异常" size="small">
            <Table<RepeatedIssue>
              rowKey={(r) => (r.assetId || "") + "_" + (r.fieldKey || "")}
              size="small"
              dataSource={repeated}
              pagination={false}
              columns={[
                { title: "设备", dataIndex: "assetName" },
                { title: "问题", render: (_, r) => r.fieldLabel || r.fieldKey || r.issue || "—" },
                { title: "次数", dataIndex: "count", width: 70 },
                { title: "最近", width: 120, render: (_, r) => fmtTime(r.lastTime) },
              ]}
            />
          </Card>
        </Col>
        <Col span={12}>
          <Card title="巡检质量(复核行为)" size="small">
            <Table<InspectorQualityRow>
              rowKey={(r) => r.operator || ""}
              size="small"
              dataSource={quality}
              pagination={false}
              columns={[
                { title: "巡检员", dataIndex: "operator" },
                { title: "确认总数", dataIndex: "total", width: 90 },
                {
                  title: "未看图确认",
                  width: 110,
                  render: (_, r) =>
                    (r.noPhotoConfirm || 0) > 0 ? (
                      <b style={{ color: "#d4380d" }}>{r.noPhotoConfirm}</b>
                    ) : (
                      0
                    ),
                },
                { title: "人工修正", dataIndex: "corrections", width: 90 },
              ]}
            />
          </Card>
        </Col>
      </Row>
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
