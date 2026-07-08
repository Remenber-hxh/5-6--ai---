import { Button, Card, Empty, Select, Skeleton, Space, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";

import { OperationLog, listOperationLogs } from "../api/mgmt";
import { actionColor, actionLabel, fmtDetail, targetLabel } from "../lib/oplog";
import { fmtTime } from "../lib/status";

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

  if (loading && logs.length === 0) {
    return (
      <Card title="操作日志">
        <Skeleton active paragraph={{ rows: 8 }} />
      </Card>
    );
  }

  const hasFilter = Boolean(actor || action);

  return (
    <Card
      title="操作日志"
      size="small"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="按人筛选"
            style={{ width: 140 }}
            value={actor || undefined}
            options={actors.map((a) => ({ value: a, label: a }))}
            onChange={(v) => setActor(v || "")}
          />
          <Select
            allowClear
            placeholder="按动作筛选"
            style={{ width: 170 }}
            value={action || undefined}
            options={actions.map((a) => ({ value: a, label: actionLabel(a) }))}
            onChange={(v) => setAction(v || "")}
          />
        </Space>
      }
    >
      <Table<OperationLog>
        size="middle"
        rowKey={(l) => l.id || `${l.createdAt}_${l.targetId}_${l.action}`}
        loading={loading}
        dataSource={rows}
        locale={{
          emptyText: (
            <Empty description={hasFilter ? "没有匹配的日志" : "暂无操作日志"}>
              {hasFilter && (
                <Button
                  onClick={() => {
                    setActor("");
                    setAction("");
                  }}
                >
                  清除筛选
                </Button>
              )}
            </Empty>
          ),
        }}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        columns={[
          { title: "时间", width: 150, render: (_, l) => fmtTime(l.createdAt, true) },
          { title: "操作人", dataIndex: "actorName", width: 110 },
          {
            title: "动作",
            width: 130,
            render: (_, l) => <Tag color={actionColor(l.action)}>{actionLabel(l.action)}</Tag>,
          },
          { title: "对象类型", width: 110, render: (_, l) => targetLabel(l.targetType) },
          { title: "对象", dataIndex: "targetId", width: 200, ellipsis: true },
          {
            title: "详情",
            ellipsis: true,
            render: (_, l) => {
              const s = fmtDetail(l.detail);
              return s ? (
                <span title={s} style={{ color: "#5b6b78" }}>
                  {s}
                </span>
              ) : (
                "—"
              );
            },
          },
        ]}
      />
    </Card>
  );
}
