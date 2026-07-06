import { Button, Card, Col, Popconfirm, Row, Space, Statistic, Table, Tag, message } from "antd";
import { useEffect, useMemo, useState } from "react";

import {
  EngineeringPlan,
  EngineeringTask,
  dispatchPlan,
  listPlans,
  listTasks,
  setTaskStatus,
} from "../api/mgmt";
import { fmtTime } from "../lib/status";

const planTag = (s?: string) => {
  if (s === "执行中" || s === "进行中") return <Tag color="blue">执行中</Tag>;
  if (s === "待整改") return <Tag color="red">待整改</Tag>;
  if (s === "已完成") return <Tag color="green">已完成</Tag>;
  return <Tag>待执行</Tag>;
};

const taskTag = (s?: string) => {
  if (s === "进行中") return <Tag color="blue">进行中</Tag>;
  if (s === "待整改") return <Tag color="red">待整改</Tag>;
  if (s === "已完成") return <Tag color="green">已完成</Tag>;
  if (s === "逾期") return <Tag color="volcano">逾期</Tag>;
  if (s === "已取消") return <Tag>已取消</Tag>;
  return <Tag>待执行</Tag>;
};

// 计划→任务闭环:待执行 →(派发)→ 进行中(下发移动端)→ 已完成;异常→待整改
export default function Plan() {
  const [plans, setPlans] = useState<EngineeringPlan[]>([]);
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [statusFilter, setStatusFilter] = useState("");

  async function load() {
    setLoading(true);
    try {
      const [ps, ts] = await Promise.all([listPlans(), listTasks()]);
      setPlans(ps);
      setTasks(ts);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  const stats = useMemo(() => {
    const c = { 待执行: 0, 执行中: 0, 待整改: 0, 已完成: 0 } as Record<string, number>;
    plans.forEach((p) => {
      const s = p.status === "进行中" ? "执行中" : p.status || "待执行";
      if (s in c) c[s] += 1;
      else c["待执行"] += 1;
    });
    return c;
  }, [plans]);

  const taskRows = useMemo(
    () => tasks.filter((t) => !statusFilter || t.status === statusFilter),
    [tasks, statusFilter],
  );

  async function onDispatch(p: EngineeringPlan) {
    try {
      await dispatchPlan(p);
      message.success("已派发并下发到移动端,任务进入「进行中」");
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "派发失败");
    }
  }

  async function onTaskAction(t: EngineeringTask, status: string) {
    try {
      await setTaskStatus(t.id, status);
      message.success(`任务已${status}`);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  return (
    <div>
      <Row gutter={16} style={{ marginBottom: 16 }}>
        {(["待执行", "执行中", "待整改", "已完成"] as const).map((k) => (
          <Col span={6} key={k}>
            <Card
              size="small"
              hoverable
              onClick={() => setStatusFilter(statusFilter === k ? "" : k === "执行中" ? "进行中" : k)}
            >
              <Statistic title={k} value={stats[k]} valueStyle={k === "待整改" && stats[k] > 0 ? { color: "#cf1322" } : undefined} />
            </Card>
          </Col>
        ))}
      </Row>
      <Card title="巡检计划" style={{ marginBottom: 16 }}>
        <Table<EngineeringPlan>
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={plans}
          pagination={{ pageSize: 10 }}
          columns={[
            { title: "计划内容", dataIndex: "workContent" },
            { title: "项目", dataIndex: "project", width: 130 },
            { title: "负责人", dataIndex: "ownerName", width: 110 },
            { title: "截止", dataIndex: "planEnd", width: 120 },
            { title: "状态", width: 100, render: (_, p) => planTag(p.status) },
            {
              title: "操作",
              width: 140,
              render: (_, p) =>
                !p.status || p.status === "待执行" ? (
                  <Popconfirm title="派发执行任务并下发移动端?" onConfirm={() => onDispatch(p)}>
                    <Button type="primary" size="small">
                      派发执行任务
                    </Button>
                  </Popconfirm>
                ) : null,
            },
          ]}
        />
      </Card>
      <Card title={`执行任务${statusFilter ? ` · ${statusFilter}` : ""}`}>
        <Table<EngineeringTask>
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={taskRows}
          pagination={{ pageSize: 10 }}
          columns={[
            { title: "任务", dataIndex: "title" },
            { title: "责任人", dataIndex: "assigneeName", width: 110 },
            { title: "截止", dataIndex: "dueAt", width: 120 },
            { title: "完成时间", width: 150, render: (_, t) => (t.completedAt ? fmtTime(t.completedAt, true) : "—") },
            { title: "状态", width: 100, render: (_, t) => taskTag(t.status) },
            {
              title: "操作",
              width: 180,
              render: (_, t) => (
                <Space>
                  {(t.status === "待执行" || !t.status) && (
                    <Button size="small" onClick={() => onTaskAction(t, "进行中")}>
                      下发
                    </Button>
                  )}
                  {(t.status === "进行中" || t.status === "待整改") && (
                    <Button size="small" onClick={() => onTaskAction(t, "已完成")}>
                      完成
                    </Button>
                  )}
                  {t.status === "已完成" && (
                    <Button size="small" onClick={() => onTaskAction(t, "待执行")}>
                      重开
                    </Button>
                  )}
                </Space>
              ),
            },
          ]}
        />
      </Card>
    </div>
  );
}
