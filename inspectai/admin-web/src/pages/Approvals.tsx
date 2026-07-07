import { Button, Card, Descriptions, Drawer, Popconfirm, Segmented, Space, Table, Tag, message } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { ChangeRequest, listChangeRequests, reviewChangeRequest } from "../api/mgmt";
import { fmtTime } from "../lib/status";

const statusTag = (s?: string) =>
  s === "pending" ? (
    <Tag color="orange">待审批</Tag>
  ) : s === "approved" ? (
    <Tag color="green">已通过</Tag>
  ) : s === "rejected" ? (
    <Tag color="red">已驳回</Tag>
  ) : (
    <Tag>{s || "—"}</Tag>
  );

export default function Approvals() {
  const [rows, setRows] = useState<ChangeRequest[]>([]);
  const [current, setCurrent] = useState<ChangeRequest | null>(null);
  const [loading, setLoading] = useState(false);
  const [filter, setFilter] = useState("待审批");
  const nav = useNavigate();

  async function load() {
    setLoading(true);
    try {
      setRows(await listChangeRequests());
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function review(id: string, action: "approve" | "reject") {
    try {
      await reviewChangeRequest(id, action);
      message.success(action === "approve" ? "申请已通过" : "申请已驳回");
      setCurrent(null);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  const shown = useMemo(() => {
    if (filter === "待审批") return rows.filter((r) => r.status === "pending");
    if (filter === "已处理") return rows.filter((r) => r.status !== "pending");
    return rows;
  }, [rows, filter]);

  return (
    <Card
      title="审批中心"
      extra={<Segmented options={["待审批", "已处理", "全部"]} value={filter} onChange={(v) => setFilter(String(v))} />}
    >
      <Table<ChangeRequest>
        rowKey="id"
        size="middle"
        loading={loading}
        dataSource={shown}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        onRow={(r) => ({ onClick: () => setCurrent(r), style: { cursor: "pointer" } })}
        columns={[
          { title: "时间", dataIndex: "createdAt", width: 150, render: (v) => fmtTime(v, true) },
          { title: "类型", dataIndex: "type", width: 120 },
          { title: "设备", dataIndex: "assetName" },
          { title: "申请人", dataIndex: "requesterName", width: 110 },
          { title: "理由", dataIndex: "reason", ellipsis: true },
          { title: "状态", width: 100, render: (_, r) => statusTag(r.status) },
          {
            title: "操作",
            width: 160,
            render: (_, r) =>
              r.status === "pending" ? (
                <Space onClick={(e) => e.stopPropagation()}>
                  <Popconfirm title="确认通过该申请?" onConfirm={() => review(r.id, "approve")}>
                    <Button type="primary" size="small">
                      通过
                    </Button>
                  </Popconfirm>
                  <Popconfirm title="确认驳回该申请?" onConfirm={() => review(r.id, "reject")}>
                    <Button danger size="small">
                      驳回
                    </Button>
                  </Popconfirm>
                </Space>
              ) : null,
          },
        ]}
      />
      <Drawer title="审批详情" open={!!current} width={460} onClose={() => setCurrent(null)}>
        {current && (
          <>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="类型">{current.type || "—"}</Descriptions.Item>
              <Descriptions.Item label="设备">{current.assetName || "—"}</Descriptions.Item>
              <Descriptions.Item label="申请人">{current.requesterName || "—"}</Descriptions.Item>
              {current.fieldLabel && (
                <Descriptions.Item label="修改字段">
                  {current.fieldLabel}:{current.oldValue || "—"} → {current.newValue || "—"}
                </Descriptions.Item>
              )}
              <Descriptions.Item label="理由">{current.reason || "—"}</Descriptions.Item>
              <Descriptions.Item label="状态">{statusTag(current.status)}</Descriptions.Item>
              {current.reviewNote && (
                <Descriptions.Item label="审批备注">{current.reviewNote}</Descriptions.Item>
              )}
            </Descriptions>
            {current.recordId && (
              <Button style={{ marginTop: 12 }} onClick={() => nav("/record?focus=" + encodeURIComponent(current.recordId!))}>
                查看原始记录 →
              </Button>
            )}
            {current.status === "pending" && (
              <Space style={{ marginTop: 16 }}>
                <Popconfirm title="确认通过该申请?" onConfirm={() => review(current.id, "approve")}>
                  <Button type="primary">通过申请</Button>
                </Popconfirm>
                <Popconfirm title="确认驳回该申请?" onConfirm={() => review(current.id, "reject")}>
                  <Button danger>驳回申请</Button>
                </Popconfirm>
              </Space>
            )}
          </>
        )}
      </Drawer>
    </Card>
  );
}
