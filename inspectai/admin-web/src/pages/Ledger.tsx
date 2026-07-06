import { Card, Descriptions, Drawer, Input, Select, Space, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AssetEntry, listAssets, listRecords } from "../api/mgmt";
import { InspectionRecord, fmtTime, recordBusinessStatus, statusTagColor } from "../lib/status";

const levelTag = (a: AssetEntry) => {
  const s = a.lastStatus || "";
  if (s === "异常" || a.statusLevel === "danger") return <Tag color="red">异常</Tag>;
  if (s === "待复核" || a.statusLevel === "warning") return <Tag color="orange">待复核</Tag>;
  if (s === "待维修" || a.statusLevel === "repair") return <Tag color="gold">维修中</Tag>;
  return <Tag color="green">正常</Tag>;
};

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

  // 巡检轨迹:该资产点位的近 5 条记录(与旧版 renderAssetSide 口径一致)
  const trail = useMemo(() => {
    if (!current) return [];
    return records
      .filter((r) => r.pointId === current.pointId || r.id === current.lastRecordId)
      .sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""))
      .slice(0, 5);
  }, [current, records]);

  return (
    <Card
      title="资产台账"
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
      <Table<AssetEntry>
        rowKey="id"
        dataSource={rows}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 台` }}
        onRow={(r) => ({ onClick: () => setCurrent(r), style: { cursor: "pointer" } })}
        columns={[
          { title: "设备", dataIndex: "assetName" },
          { title: "编号", dataIndex: "assetKey", width: 160 },
          { title: "类型", dataIndex: "assetType", width: 140 },
          { title: "项目", dataIndex: "project", width: 140 },
          { title: "状态", width: 100, render: (_, r) => levelTag(r) },
          { title: "最近巡检", width: 150, render: (_, r) => fmtTime(r.lastInspectedAt, true) },
        ]}
      />
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
                    <span style={{ color: "#8aa0b0", flex: "none" }}>
                      {fmtTime(r.createdAt)}
                    </span>
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
