import { Card, Select, Space, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";

import { OperationLog, listOperationLogs } from "../api/mgmt";
import { fmtTime } from "../lib/status";

const ACTION_LABEL: Record<string, string> = {
  login: "登录",
  create: "创建",
  update: "更新",
  delete: "删除",
  approve: "审批通过",
  reject: "审批驳回",
  dispatch: "派发",
};

// 操作日志:留痕每位成员的改动与时间(只读)
export default function Logs() {
  const [logs, setLogs] = useState<OperationLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [actor, setActor] = useState("");
  const [action, setAction] = useState("");

  useEffect(() => {
    listOperationLogs()
      .then(setLogs)
      .finally(() => setLoading(false));
  }, []);

  const actors = useMemo(
    () => Array.from(new Set(logs.map((l) => l.actorName).filter(Boolean))) as string[],
    [logs],
  );
  const actions = useMemo(
    () => Array.from(new Set(logs.map((l) => l.action).filter(Boolean))) as string[],
    [logs],
  );
  const rows = useMemo(
    () => logs.filter((l) => (!actor || l.actorName === actor) && (!action || l.action === action)),
    [logs, actor, action],
  );

  return (
    <Card
      title="操作日志"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="按人筛选"
            style={{ width: 140 }}
            options={actors.map((a) => ({ value: a, label: a }))}
            onChange={(v) => setActor(v || "")}
          />
          <Select
            allowClear
            placeholder="按动作筛选"
            style={{ width: 150 }}
            options={actions.map((a) => ({ value: a, label: ACTION_LABEL[a] || a }))}
            onChange={(v) => setAction(v || "")}
          />
        </Space>
      }
    >
      <Table<OperationLog>
        rowKey={(l) => l.id || `${l.createdAt}_${l.targetId}_${l.action}`}
        loading={loading}
        dataSource={rows}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        columns={[
          { title: "时间", width: 160, render: (_, l) => fmtTime(l.createdAt, true) },
          { title: "操作人", dataIndex: "actorName", width: 120 },
          {
            title: "动作",
            width: 110,
            render: (_, l) => <Tag>{ACTION_LABEL[l.action || ""] || l.action || "—"}</Tag>,
          },
          { title: "对象类型", dataIndex: "targetType", width: 120 },
          { title: "对象", dataIndex: "targetId", ellipsis: true },
          {
            title: "详情",
            ellipsis: true,
            render: (_, l) => (l.detail ? JSON.stringify(l.detail) : "—"),
          },
        ]}
      />
    </Card>
  );
}
