import { Card, Table, Tag } from "antd";
import { useEffect, useState } from "react";

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

  useEffect(() => {
    listOperationLogs()
      .then(setLogs)
      .finally(() => setLoading(false));
  }, []);

  return (
    <Card title="操作日志">
      <Table<OperationLog>
        rowKey={(l) => l.id || `${l.createdAt}_${l.targetId}_${l.action}`}
        loading={loading}
        dataSource={logs}
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
