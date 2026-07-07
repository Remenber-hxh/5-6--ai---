import { PlusOutlined } from "@ant-design/icons";
import { Button, Card, Descriptions, Drawer, Form, Input, Modal, Popconfirm, Space, Table, Tag, message } from "antd";
import { useEffect, useMemo, useState } from "react";

import {
  EngineeringPlan,
  EngineeringTask,
  dispatchPlan,
  listPlans,
  listTasks,
  savePlan,
  setTaskStatus,
} from "../api/mgmt";
import { useUi } from "../store/ui";

// ===== 旧版口径:计划状态 → 四桶 =====
type Bucket = "pending" | "processing" | "overdue" | "done";

function planStatusBucket(status = ""): Bucket {
  const s = String(status);
  if (s.includes("已完成")) return "done";
  if (s.includes("待执行")) return "pending";
  if (s.includes("执行中") || s.includes("进行中") || s.includes("启用")) return "processing";
  return "overdue"; // 需跟进:待整改 / 未排期 / 暂停 等
}

const BUCKETS: { key: Bucket; label: string; sub: string; color: string }[] = [
  { key: "pending", label: "待执行", sub: "未开始", color: "#8aa0b0" },
  { key: "processing", label: "进行中", sub: "现场处理", color: "#1499ff" },
  { key: "overdue", label: "需跟进", sub: "复核 / 异常", color: "#ef4b3f" },
  { key: "done", label: "已完成", sub: "结果入库", color: "#12a968" },
];

const bucketTag = (status?: string) => {
  const b = planStatusBucket(status);
  const map: Record<Bucket, [string, string]> = {
    pending: ["待执行", "default"],
    processing: ["进行中", "blue"],
    overdue: ["需跟进", "red"],
    done: ["已完成", "green"],
  };
  return <Tag color={map[b][1]}>{map[b][0]}</Tag>;
};

const taskTag = (s?: string) => {
  if (s === "进行中") return <Tag color="blue">进行中</Tag>;
  if (s === "待整改") return <Tag color="red">待整改</Tag>;
  if (s === "已完成") return <Tag color="green">已完成</Tag>;
  if (s === "逾期") return <Tag color="volcano">逾期</Tag>;
  if (s === "已取消") return <Tag>已取消</Tag>;
  return <Tag>待执行</Tag>;
};

// 巡检计划:完全按旧版整改——状态卡(占比条/点击筛选) + 复查任务区块 + 七列计划表 + 详情抽屉
export default function Plan() {
  const [plans, setPlans] = useState<EngineeringPlan[]>([]);
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [bucket, setBucket] = useState<"" | Bucket>("");
  const [currentPlan, setCurrentPlan] = useState<EngineeringPlan | null>(null);
  const [currentTask, setCurrentTask] = useState<EngineeringTask | null>(null);
  const [editing, setEditing] = useState<EngineeringPlan | null | "new">(null);
  const { project } = useUi();
  const [form] = Form.useForm();

  async function load() {
    setLoading(true);
    try {
      const [ps, ts] = await Promise.all([listPlans(), listTasks()]);
      setPlans(ps);
      setTasks(ts);
      setCurrentPlan((cur) => (cur ? ps.find((p) => p.id === cur.id) || null : null));
      setCurrentTask((cur) => (cur ? ts.find((t) => t.id === cur.id) || null : null));
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const realPlans = useMemo(
    () => plans.filter((p) => p.source !== "seed" && (!project || p.project === project)),
    [plans, project],
  );

  // 复查任务:未挂计划的在途任务(异常检出 / AI 派单),归「需跟进」桶
  const recheckTasks = useMemo(
    () =>
      tasks
        .filter((t) => !t.planItemId && !["已完成", "已取消"].includes(t.status || ""))
        .filter((t) => !project || t.project === project)
        .sort((a, b) => String(a.dueAt || "").localeCompare(String(b.dueAt || ""))),
    [tasks, project],
  );

  const counts = useMemo(() => {
    const c: Record<Bucket, number> = { pending: 0, processing: 0, overdue: 0, done: 0 };
    realPlans.forEach((p) => {
      c[planStatusBucket(p.status)] += 1;
    });
    c.overdue += recheckTasks.length; // 需跟进口径并入复查任务,避免"有待整改却显示 0"
    return c;
  }, [realPlans, recheckTasks]);

  const total = Math.max(counts.pending + counts.processing + counts.overdue + counts.done, 1);

  const rows = useMemo(
    () => (bucket ? realPlans.filter((p) => planStatusBucket(p.status) === bucket) : realPlans),
    [realPlans, bucket],
  );

  const planTaskOf = (p: EngineeringPlan) =>
    tasks.find((t) => t.id === p.latestTaskId) || tasks.find((t) => t.planItemId === p.id) || null;

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

  const showRecheck = (bucket === "" || bucket === "overdue") && recheckTasks.length > 0;

  return (
    <div>
      {/* 状态卡:数字 + 占比条,点击筛选(再点取消) */}
      <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 14, marginBottom: 16 }}>
        {BUCKETS.map((b) => {
          const n = counts[b.key];
          const active = bucket === b.key;
          return (
            <div
              key={b.key}
              onClick={() => setBucket(active ? "" : b.key)}
              style={{
                background: "#fff",
                borderRadius: 10,
                padding: "14px 16px 12px",
                cursor: "pointer",
                border: active ? `1.5px solid ${b.color}` : "1.5px solid transparent",
                boxShadow: "0 1px 2px rgba(15, 35, 55, 0.04)",
                transition: "border-color 0.15s",
              }}
            >
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
                <span style={{ color: "#5b6b78", fontSize: 13 }}>{b.label}</span>
                <small style={{ color: "#9db0be", fontSize: 11 }}>{b.sub}</small>
              </div>
              <div
                style={{
                  fontSize: 26,
                  fontWeight: 700,
                  margin: "2px 0 8px",
                  color: n > 0 && b.key === "overdue" ? "#cf1322" : undefined,
                }}
              >
                {n}
              </div>
              <div style={{ height: 4, borderRadius: 2, background: "#eef1f5", overflow: "hidden" }}>
                <div
                  style={{
                    width: `${Math.round((n / total) * 100)}%`,
                    height: "100%",
                    background: b.color,
                    borderRadius: 2,
                  }}
                />
              </div>
            </div>
          );
        })}
      </div>

      {/* 复查任务:未挂计划的在途任务(与旧版同区块) */}
      {showRecheck && (
        <Card
          title="复查任务"
          extra={
            <span style={{ color: "#9db0be", fontSize: 12 }}>
              异常检出 / AI 派单生成,未挂工程计划;复检合格后自动销账
            </span>
          }
          size="small"
          style={{ marginBottom: 16 }}
        >
          <Table<EngineeringTask>
            rowKey="id"
            size="small"
            dataSource={recheckTasks}
            pagination={false}
            onRow={(t) => ({ onClick: () => setCurrentTask(t), style: { cursor: "pointer" } })}
            columns={[
              { title: "设备 / 任务", render: (_, t) => t.title || "异常复查" },
              { title: "项目", dataIndex: "project", width: 130 },
              { title: "责任人", dataIndex: "assigneeName", width: 110 },
              { title: "截止", dataIndex: "dueAt", width: 120 },
              { title: "状态", width: 100, render: (_, t) => taskTag(t.status) },
            ]}
          />
        </Card>
      )}

      {/* 巡检任务表:旧版七列 */}
      <Card
        title="巡检任务"
        extra={
          <Button
            type="primary"
            icon={<PlusOutlined />}
            onClick={() => {
              setEditing("new");
              form.resetFields();
            }}
          >
            新建计划
          </Button>
        }
      >
        <Table<EngineeringPlan>
          rowKey="id"
          size="small"
          loading={loading}
          dataSource={rows}
          pagination={{ pageSize: 12, showTotal: (t) => `共 ${t} 条` }}
          onRow={(p) => ({ onClick: () => setCurrentPlan(p), style: { cursor: "pointer" } })}
          columns={[
            { title: "计划名称", dataIndex: "workContent" },
            { title: "项目", dataIndex: "project", width: 110 },
            { title: "类型 / 点位", dataIndex: "category", width: 130, render: (v) => v || "—" },
            { title: "周期", dataIndex: "cycleText", width: 120, render: (v) => v || "—" },
            { title: "责任人", dataIndex: "ownerName", width: 100 },
            {
              title: "计划节点",
              width: 170,
              render: (_, p) => [p.planStart, p.planEnd].filter(Boolean).join(" 至 ") || p.planEnd || "—",
            },
            { title: "状态", width: 90, render: (_, p) => bucketTag(p.status) },
          ]}
        />
      </Card>

      {/* 计划详情抽屉(旧版右侧详情卡) */}
      <Drawer title="工程计划详情" open={!!currentPlan} width={440} onClose={() => setCurrentPlan(null)}>
        {currentPlan && (
          <>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="计划名称">{currentPlan.workContent || "—"}</Descriptions.Item>
              <Descriptions.Item label="项目">{currentPlan.project || "—"}</Descriptions.Item>
              <Descriptions.Item label="类别">{currentPlan.category || "—"}</Descriptions.Item>
              <Descriptions.Item label="责任人">{currentPlan.ownerName || "—"}</Descriptions.Item>
              <Descriptions.Item label="周期">{currentPlan.cycleText || "—"}</Descriptions.Item>
              <Descriptions.Item label="计划节点">
                {[currentPlan.planStart, currentPlan.planEnd].filter(Boolean).join(" 至 ") || "—"}
              </Descriptions.Item>
              <Descriptions.Item label="预算">
                {currentPlan.budgetAmount ? `${Number(currentPlan.budgetAmount).toLocaleString()} 元` : "—"}
              </Descriptions.Item>
              <Descriptions.Item label="状态">{bucketTag(currentPlan.status)}</Descriptions.Item>
            </Descriptions>
            <Space style={{ marginTop: 16 }} wrap>
              {planStatusBucket(currentPlan.status) === "pending" && (
                <Popconfirm title="派发执行任务并下发移动端?" onConfirm={() => onDispatch(currentPlan)}>
                  <Button type="primary">派发执行任务</Button>
                </Popconfirm>
              )}
              {planTaskOf(currentPlan) && (
                <Button onClick={() => setCurrentTask(planTaskOf(currentPlan))}>查看任务进度</Button>
              )}
              <Button
                onClick={() => {
                  setEditing(currentPlan);
                  form.setFieldsValue({
                    workContent: currentPlan.workContent,
                    project: currentPlan.project,
                    category: currentPlan.category,
                    ownerName: currentPlan.ownerName,
                    cycleText: currentPlan.cycleText,
                    planEnd: currentPlan.planEnd,
                  });
                }}
              >
                编辑计划
              </Button>
            </Space>
          </>
        )}
      </Drawer>

      {/* 任务详情抽屉(与移动端挂钩的执行任务) */}
      <Drawer title="任务进度" open={!!currentTask} width={420} onClose={() => setCurrentTask(null)}>
        {currentTask && (
          <>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="任务">{currentTask.title || "—"}</Descriptions.Item>
              <Descriptions.Item label="项目">{currentTask.project || "—"}</Descriptions.Item>
              <Descriptions.Item label="责任人">{currentTask.assigneeName || "—"}</Descriptions.Item>
              <Descriptions.Item label="截止">{currentTask.dueAt || "—"}</Descriptions.Item>
              <Descriptions.Item label="状态">{taskTag(currentTask.status)}</Descriptions.Item>
              {currentTask.completedAt && (
                <Descriptions.Item label="完成时间">{currentTask.completedAt}</Descriptions.Item>
              )}
            </Descriptions>
            <Space style={{ marginTop: 16 }}>
              {(currentTask.status === "待执行" || !currentTask.status) && (
                <Button type="primary" onClick={() => onTaskAction(currentTask, "进行中")}>
                  下发到移动端
                </Button>
              )}
              {(currentTask.status === "进行中" || currentTask.status === "待整改") && (
                <Button onClick={() => onTaskAction(currentTask, "已完成")}>标记完成</Button>
              )}
              {currentTask.status === "已完成" && (
                <Button onClick={() => onTaskAction(currentTask, "待执行")}>重开</Button>
              )}
            </Space>
          </>
        )}
      </Drawer>

      {/* 新建 / 编辑计划 */}
      <Modal
        title={editing === "new" ? "新建计划" : "编辑计划"}
        open={!!editing}
        destroyOnHidden
        onCancel={() => setEditing(null)}
        onOk={async () => {
          const v = await form.validateFields();
          try {
            await savePlan({ ...(editing !== "new" && editing ? { id: editing.id } : {}), ...v });
            message.success("计划已保存");
            setEditing(null);
            setCurrentPlan(null);
            await load();
          } catch (e) {
            message.error(e instanceof Error ? e.message : "保存失败");
          }
        }}
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item name="workContent" label="计划内容" rules={[{ required: true, message: "请输入计划内容" }]}>
            <Input placeholder="如:会议中心电梯月度巡检" />
          </Form.Item>
          <Form.Item name="project" label="项目">
            <Input placeholder="如:会议中心" />
          </Form.Item>
          <Form.Item name="category" label="类型 / 点位">
            <Input placeholder="如:有机房电梯" />
          </Form.Item>
          <Form.Item name="ownerName" label="负责人">
            <Input />
          </Form.Item>
          <Form.Item name="cycleText" label="周期">
            <Input placeholder="如:每日 09:00 / 每周一" />
          </Form.Item>
          <Form.Item name="planEnd" label="截止日期">
            <Input placeholder="YYYY-MM-DD" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
