import { Card, Descriptions, Drawer, Input, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { AssetEntry, listAssets } from "../api/mgmt";

const levelTag = (a: AssetEntry) => {
  const s = a.lastStatus || "";
  if (s === "异常" || a.statusLevel === "danger") return <Tag color="red">异常</Tag>;
  if (s === "待复核" || a.statusLevel === "warning") return <Tag color="orange">待复核</Tag>;
  if (s === "待维修" || a.statusLevel === "repair") return <Tag color="gold">维修中</Tag>;
  return <Tag color="green">正常</Tag>;
};

export default function Ledger() {
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [kw, setKw] = useState("");
  const [current, setCurrent] = useState<AssetEntry | null>(null);
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    listAssets().then((list) => {
      setAssets(list);
      const focus = params.get("focus");
      if (focus) {
        const hit = list.find((a) => a.id === focus);
        if (hit) setCurrent(hit);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rows = useMemo(
    () =>
      assets.filter(
        (a) =>
          !kw ||
          (a.assetName || "").includes(kw) ||
          (a.assetKey || "").includes(kw) ||
          (a.project || "").includes(kw),
      ),
    [assets, kw],
  );

  return (
    <Card
      title="资产台账"
      extra={<Input.Search allowClear placeholder="搜设备名 / 编号 / 项目" style={{ width: 260 }} onSearch={setKw} />}
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
          { title: "最近巡检", dataIndex: "lastInspectedAt", width: 170 },
        ]}
      />
      <Drawer
        title={current?.assetName || "资产详情"}
        open={!!current}
        width={440}
        onClose={() => {
          setCurrent(null);
          if (params.get("focus")) setParams({});
        }}
      >
        {current && (
          <Descriptions column={1} size="small">
            <Descriptions.Item label="编号">{current.assetKey || current.id}</Descriptions.Item>
            <Descriptions.Item label="类型">{current.assetType || "—"}</Descriptions.Item>
            <Descriptions.Item label="项目">{current.project || "—"}</Descriptions.Item>
            <Descriptions.Item label="点位">{current.pointName || "—"}</Descriptions.Item>
            <Descriptions.Item label="状态">{levelTag(current)}</Descriptions.Item>
            <Descriptions.Item label="最近巡检">{current.lastInspectedAt || "—"}</Descriptions.Item>
          </Descriptions>
        )}
      </Drawer>
    </Card>
  );
}
