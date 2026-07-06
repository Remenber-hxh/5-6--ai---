import { Card, Col, Descriptions, Drawer, Empty, Input, Row, Select, Space, Tag } from "antd";
import { motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AssetEntry, listAssets, listRecords } from "../api/mgmt";
import { InspectionRecord, fmtTime, mediaUrl, recordBusinessStatus, statusTagColor } from "../lib/status";

const levelTag = (a: AssetEntry) => {
  const s = a.lastStatus || "";
  if (s === "异常" || a.statusLevel === "danger") return <Tag color="red">异常</Tag>;
  if (s === "待复核" || a.statusLevel === "warning") return <Tag color="orange">待复核</Tag>;
  if (s === "待维修" || a.statusLevel === "repair") return <Tag color="gold">维修中</Tag>;
  return <Tag color="green">正常</Tag>;
};

// 资产台账:卡片网格(带设备照片预览,与旧版一致)+ 详情抽屉(巡检轨迹)
export default function Ledger() {
  const nav = useNavigate();
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [kw, setKw] = useState("");
  const [type, setType] = useState("");
  const [current, setCurrent] = useState<AssetEntry | null>(null);
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    Promise.all([listAssets(), listRecords()]).then(([as, rs]) => {
      setAssets(as);
      setRecords(rs);
      const focus = params.get("focus");
      if (focus) {
        const hit = as.find((a) => a.id === focus);
        if (hit) setCurrent(hit);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const types = useMemo(
    () => Array.from(new Set(assets.map((a) => a.assetType).filter(Boolean))) as string[],
    [assets],
  );

  // 资产 → 最近一张现场照片(取该点位最新带图记录)
  const photoOf = useMemo(() => {
    const map: Record<string, string> = {};
    const sorted = [...records].sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""));
    for (const a of assets) {
      const rec = sorted.find((r) => (r.pointId === a.pointId || r.id === a.lastRecordId) && r.images?.length);
      const p = rec?.images?.[0];
      if (p) map[a.id] = mediaUrl(p.path || p.url);
    }
    return map;
  }, [assets, records]);

  const rows = useMemo(
    () =>
      assets.filter(
        (a) =>
          (!type || a.assetType === type) &&
          (!kw ||
            (a.assetName || "").includes(kw) ||
            (a.assetKey || "").includes(kw) ||
            (a.project || "").includes(kw)),
      ),
    [assets, kw, type],
  );

  const trail = useMemo(() => {
    if (!current) return [];
    return records
      .filter((r) => r.pointId === current.pointId || r.id === current.lastRecordId)
      .sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""))
      .slice(0, 5);
  }, [current, records]);

  return (
    <Card
      title={`资产台账(${rows.length} 台)`}
      extra={
        <Space>
          <Select
            allowClear
            placeholder="类型"
            style={{ width: 150 }}
            options={types.map((t) => ({ value: t, label: t }))}
            onChange={(v) => setType(v || "")}
          />
          <Input.Search allowClear placeholder="搜设备名 / 编号 / 项目" style={{ width: 240 }} onSearch={setKw} />
        </Space>
      }
    >
      {rows.length === 0 ? (
        <Empty description="没有匹配的资产" />
      ) : (
        <Row gutter={[16, 16]}>
          {rows.map((a, i) => (
            <Col key={a.id} xs={24} sm={12} lg={8} xl={6}>
              <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: Math.min(i * 0.04, 0.4), duration: 0.3 }}
              >
                <Card
                  hoverable
                  size="small"
                  onClick={() => setCurrent(a)}
                  cover={
                    photoOf[a.id] ? (
                      <img
                        src={photoOf[a.id]}
                        alt=""
                        style={{ height: 130, objectFit: "cover" }}
                        loading="lazy"
                      />
                    ) : (
                      <div
                        style={{
                          height: 130,
                          display: "grid",
                          placeItems: "center",
                          background: "#f0f3f7",
                          color: "#9db0be",
                          fontSize: 12,
                        }}
                      >
                        暂无现场照片
                      </div>
                    )
                  }
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <b style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {a.assetName}
                    </b>
                    {levelTag(a)}
                  </div>
                  <div style={{ color: "#8aa0b0", fontSize: 12, marginTop: 4 }}>
                    {a.assetType || "—"} · {a.project || "—"}
                  </div>
                  <div style={{ color: "#8aa0b0", fontSize: 12 }}>最近巡检 {fmtTime(a.lastInspectedAt)}</div>
                </Card>
              </motion.div>
            </Col>
          ))}
        </Row>
      )}
      <Drawer
        title={current?.assetName || "资产详情"}
        open={!!current}
        width={470}
        onClose={() => {
          setCurrent(null);
          if (params.get("focus")) setParams({});
        }}
      >
        {current && (
          <>
            {photoOf[current.id] && (
              <img
                src={photoOf[current.id]}
                alt=""
                style={{ width: "100%", height: 180, objectFit: "cover", borderRadius: 8, marginBottom: 14 }}
              />
            )}
            <Descriptions column={1} size="small">
              <Descriptions.Item label="编号">{current.assetKey || current.id}</Descriptions.Item>
              <Descriptions.Item label="类型">{current.assetType || "—"}</Descriptions.Item>
              <Descriptions.Item label="项目">{current.project || "—"}</Descriptions.Item>
              <Descriptions.Item label="点位">{current.pointName || "—"}</Descriptions.Item>
              <Descriptions.Item label="状态">{levelTag(current)}</Descriptions.Item>
              <Descriptions.Item label="最近巡检">{fmtTime(current.lastInspectedAt, true)}</Descriptions.Item>
            </Descriptions>
            <div style={{ margin: "16px 0 8px", fontWeight: 600 }}>巡检轨迹(近 {trail.length} 条)</div>
            {trail.length === 0 ? (
              <div style={{ color: "#8aa0b0", fontSize: 13 }}>暂无巡检记录</div>
            ) : (
              trail.map((r) => {
                const s = recordBusinessStatus(r);
                return (
                  <div
                    key={r.id}
                    onClick={() => nav(`/record?focus=${encodeURIComponent(r.id)}`)}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 10,
                      padding: "8px 0",
                      borderBottom: "1px solid #f0f2f5",
                      cursor: "pointer",
                      fontSize: 13,
                    }}
                  >
                    <span style={{ color: "#8aa0b0", flex: "none" }}>{fmtTime(r.createdAt)}</span>
                    <Tag color={statusTagColor(s)} style={{ margin: 0 }}>
                      {s}
                    </Tag>
                    <span
                      style={{
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        color: "#5b6b78",
                      }}
                    >
                      {(r.aiSummary || r.report || r.recordNo || "").slice(0, 30)}
                    </span>
                  </div>
                );
              })
            )}
          </>
        )}
      </Drawer>
    </Card>
  );
}
