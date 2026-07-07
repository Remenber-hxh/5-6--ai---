import { ConfigProvider, Button, Card, Empty, Form, Input, Modal, Popconfirm, Select, Space, Steps, Table, Tag, message } from "antd";
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

const BUCKETS: { key: Bucket; label: string; color: string }[] = [
  { key: "pending", label: "待执行", color: "#f5a524" },
  { key: "processing", label: "进行中", color: "#246bfe" },
  { key: "overdue", label: "需跟进", color: "#ef4b3f" },
  { key: "done", label: "已完成", color: "#12a968" },
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
  return <Tag color="orange">需跟进</Tag>;
};

// 详情面板字段行(旧版 label/value 样式)
function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", padding: "7px 0", fontSize: 13.5 }}>
      <span style={{ width: 74, flex: "none", color: "#8aa0b0" }}>{label}</span>
      <b style={{ color: "#1c2b3a", fontWeight: 600 }}>{children}</b>
    </div>
  );
}

// 巡检计划:完全依照旧版样式——连排状态卡 / 复查任务 / 工具栏筛选 / 右侧常驻详情面板
export default function Plan() {
  const [plans, setPlans] = useState<EngineeringPlan[]>([]);
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [bucket, setBucket] = useState<"" | Bucket>("");
  const [freq, setFreq] = useState("");
  const [proj, setProj] = useState("");
  const [kw, setKw] = useState("");
  const [selPlanId, setSelPlanId] = useState("");
  const [selTaskId, setSelTaskId] = useState("");
  const [editing, setEditing] = useState<EngineeringPlan | null | "new">(null);
  const { project } = useUi();
  const [form] = Form.useForm();

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
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const realPlans = useMemo(
    () => plans.filter((p) => p.source !== "seed" && (!project || p.project === project)),
    [plans, project],
  );

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
    c.overdue += recheckTasks.length; // 需跟进并入复查任务
    return c;
  }, [realPlans, recheckTasks]);

  const total = Math.max(counts.pending + counts.processing + counts.overdue + counts.done, 1);

  const projects = useMemo(
    () => Array.from(new Set(realPlans.map((p) => p.project).filter(Boolean))) as string[],
    [realPlans],
  );
  const freqs = useMemo(
    () => Array.from(new Set(realPlans.map((p) => p.cycleText).filter(Boolean))) as string[],
    [realPlans],
  );

  const rows = useMemo(
    () =>
      realPlans.filter(
        (p) =>
          (!bucket || planStatusBucket(p.status) === bucket) &&
          (!proj || p.project === proj) &&
          (!freq || p.cycleText === freq) &&
          (!kw || (p.workContent || "").includes(kw) || (p.category || "").includes(kw)),
      ),
    [realPlans, bucket, proj, freq, kw],
  );

  // 默认选中首行(旧版行为:右侧面板不留白)
  useEffect(() => {
    if (!selTaskId && (!selPlanId || !rows.some((r) => r.id === selPlanId))) {
      setSelPlanId(rows[0]?.id || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows]);

  const selPlan = rows.find((p) => p.id === selPlanId) || null;
  const selTask = tasks.find((t) => t.id === selTaskId) || null;

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

  // 任务步骤:待执行 → 进行中 → 已完成
  const taskStep = (s?: string) => (s === "已完成" ? 2 : s === "进行中" || s === "待整改" ? 1 : 0);

  return (
    // 旧版此页主色为蓝(操作按钮/链接),页内局部覆盖主题
    <ConfigProvider theme={{ token: { colorPrimary: "#246bfe" } }}>
      <div style={{ display: "grid", gridTemplateColumns: "1fr 372px", gap: 16, alignItems: "start" }}>
        <div>
          {/* 状态卡:白底连排,角标色块 + 大数字 + 底部占比条(旧版样式) */}
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(4, 1fr)",
              background: "#fff",
              borderRadius: 10,
              overflow: "hidden",
              marginBottom: 16,
              boxShadow: "0 1px 2px rgba(15, 35, 55, 0.04)",
            }}
          >
            {BUCKETS.map((b, i) => {
              const n = counts[b.key];
              const active = bucket === b.key;
              return (
                <div
                  key={b.key}
                  onClick={() => setBucket(active ? "" : b.key)}
                  className="plan-bucket-cell"
                  style={{
                    position: "relative",
                    padding: "18px 18px 16px",
                    cursor: "pointer",
                    borderLeft: i ? "1px solid #eef1f5" : "none",
                    background: active ? `${b.color}0d` : "#fff",
                    outline: active ? `1px solid ${b.color}55` : "none",
                    outlineOffset: -1,
                  }}
                >
                  {/* 左上角色块角标 */}
                  <span
                    style={{
                      position: "absolute",
                      left: 0,
                      top: 14,
                      width: 4,
                      height: 18,
                      background: b.color,
                      borderRadius: "0 2px 2px 0",
                    }}
                  />
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span style={{ color: "#334759", fontSize: 14, fontWeight: 600 }}>{b.label}</span>
                    <b style={{ fontSize: 30, fontWeight: 800, color: "#101d2c" }}>{n}</b>
                  </div>
                  {/* 底部占比条 */}
                  <div
                    style={{
                      position: "absolute",
                      left: 0,
                      bottom: 0,
                      height: 4,
                      width: `${Math.max(Math.round((n / total) * 100), n > 0 ? 8 : 0)}%`,
                      background: b.color,
                    }}
                  />
                </div>
              );
            })}
          </div>

          {/* 复查任务 */}
          {showRecheck && (
            <Card title="复查任务" size="small" style={{ marginBottom: 16 }}>
              <Table<EngineeringTask>
                rowKey="id"
                size="small"
                dataSource={recheckTasks}
                pagination={false}
                rowClassName={(t) => (t.id === selTaskId ? "row-selected" : "")}
                onRow={(t) => ({
                  onClick: () => {
                    setSelTaskId(t.id);
                  },
                  style: { cursor: "pointer" },
                })}
                columns={[
                  { title: "设备 / 任务", render: (_, t) => <b>{t.title || "异常复查"}</b> },
                  { title: "项目", dataIndex: "project", width: 130 },
                  { title: "责任人", dataIndex: "assigneeName", width: 110 },
                  { title: "截止", dataIndex: "dueAt", width: 120, render: (v) => <b>{v || "—"}</b> },
                  { title: "状态", width: 100, render: (_, t) => taskTag(t.status) },
                ]}
              />
            </Card>
          )}

          {/* 巡检任务:工具栏(项目/频次/状态/搜索/新建) + 七列表 */}
          <Card title="巡检任务" size="small">
            <Space style={{ marginBottom: 14 }} wrap>
              <Select
                allowClear
                placeholder="全部项目"
                style={{ width: 130 }}
                options={projects.map((p) => ({ value: p, label: p }))}
                onChange={(v) => setProj(v || "")}
              />
              <Select
                allowClear
                placeholder="全部频次"
                style={{ width: 150 }}
                options={freqs.map((f) => ({ value: f, label: f }))}
                onChange={(v) => setFreq(v || "")}
              />
              <Select
                allowClear
                placeholder="全部状态"
                style={{ width: 120 }}
                value={bucket || undefined}
                options={BUCKETS.map((b) => ({ value: b.key, label: b.label }))}
                onChange={(v) => setBucket((v as Bucket) || "")}
              />
              <Input.Search
                allowClear
                placeholder="搜索计划 / 点位"
                style={{ width: 180 }}
                onSearch={setKw}
              />
              <Button
                type="primary"
                onClick={() => {
                  setEditing("new");
                  form.resetFields();
                }}
              >
                新建计划
              </Button>
            </Space>
            <Table<EngineeringPlan>
              rowKey="id"
              size="small"
              loading={loading}
              dataSource={rows}
              pagination={{ pageSize: 12, showTotal: (t) => `共 ${t} 条` }}
              rowClassName={(p) => (p.id === selPlanId && !selTaskId ? "row-selected" : "")}
              onRow={(p) => ({
                onClick: () => {
                  setSelPlanId(p.id);
                  setSelTaskId("");
                },
                style: { cursor: "pointer" },
              })}
              columns={[
                { title: "计划名称", dataIndex: "workContent" },
                { title: "项目", dataIndex: "project", width: 100 },
                { title: "类型 / 点位", dataIndex: "category", width: 120, render: (v) => v || "—" },
                { title: "周期", dataIndex: "cycleText", width: 130, ellipsis: true, render: (v) => v || "—" },
                { title: "责任人", dataIndex: "ownerName", width: 90 },
                {
                  title: "计划节点",
                  width: 160,
                  ellipsis: true,
                  render: (_, p) => [p.planStart, p.planEnd].filter(Boolean).join(" 至 ") || p.planEnd || "—",
                },
                { title: "状态", width: 88, render: (_, p) => bucketTag(p.status) },
              ]}
            />
          </Card>
        </div>

        {/* 右侧常驻详情面板(旧版 aside) */}
        <div style={{ position: "sticky", top: 0 }}>
          {selTask ? (
            <Card
              size="small"
              title={
                <Space>
                  <span style={{ borderLeft: "3px solid #246bfe", paddingLeft: 8 }}>任务详情</span>
                  {taskTag(selTask.status)}
                </Space>
              }
            >
              <h3 style={{ margin: "4px 0 10px", fontSize: 17 }}>{selTask.title || "异常复查"}</h3>
              <div style={{ borderTop: "1px solid #f0f2f5" }}>
                <FieldRow label="项目">{selTask.project || "—"}</FieldRow>
                <FieldRow label="点位">{(selTask as { category?: string }).category || "—"}</FieldRow>
                <FieldRow label="责任人">{selTask.assigneeName || "—"}</FieldRow>
                <FieldRow label="频次">{selTask.taskType || "异常复查"}</FieldRow>
                <FieldRow label="截止">{selTask.dueAt || "—"}</FieldRow>
              </div>
              {(selTask as { workContent?: string }).workContent && (
                <div style={{ margin: "6px 0 2px" }}>
                  <div style={{ color: "#8aa0b0", fontSize: 13 }}>说明</div>
                  <div style={{ fontSize: 13.5, marginTop: 2 }}>
                    {(selTask as { workContent?: string }).workContent}
                  </div>
                </div>
              )}
              <Steps
                size="small"
                current={taskStep(selTask.status)}
                items={[{ title: "待执行" }, { title: "进行中" }, { title: "已完成" }]}
                style={{ margin: "18px 0 10px" }}
                progressDot={(_dot, { index, status }) => (
                  <span
                    style={{
                      display: "inline-block",
                      width: index === taskStep(selTask.status) ? 12 : 9,
                      height: index === taskStep(selTask.status) ? 12 : 9,
                      borderRadius: "50%",
                      background:
                        status === "finish" ? "#12a968" : status === "process" ? "#246bfe" : "#d9dee4",
                      boxShadow: status === "process" ? "0 0 0 3px rgba(36, 107, 254, 0.18)" : undefined,
                    }}
                  />
                )}
              />
              <div style={{ color: "#5b6b78", fontSize: 13, marginBottom: 14 }}>
                {selTask.status === "已完成"
                  ? "任务已完成,结果已入库"
                  : selTask.status === "待执行" || !selTask.status
                    ? "尚未下发,巡检员移动端不可见"
                    : "已下发,巡检员可在移动端执行"}
              </div>
              <Space direction="vertical" style={{ width: "100%" }}>
                {(selTask.status === "待执行" || !selTask.status) && (
                  <Button type="primary" size="large" block onClick={() => onTaskAction(selTask, "进行中")}>
                    下发到移动端
                  </Button>
                )}
                {(selTask.status === "进行中" || selTask.status === "待整改") && (
                  <Button type="primary" size="large" block onClick={() => onTaskAction(selTask, "已完成")}>
                    标记完成
                  </Button>
                )}
                {selTask.status === "已完成" ? (
                  <Button size="large" block onClick={() => onTaskAction(selTask, "待执行")}>
                    重开任务
                  </Button>
                ) : (
                  <Popconfirm title="确认取消该任务?" onConfirm={() => onTaskAction(selTask, "已取消")}>
                    <Button size="large" block>取消任务</Button>
                  </Popconfirm>
                )}
                <Button block type="text" onClick={() => setSelTaskId("")}>
                  返回计划详情
                </Button>
              </Space>
            </Card>
          ) : selPlan ? (
            <Card
              size="small"
              title={
                <Space>
                  <span style={{ borderLeft: "3px solid #246bfe", paddingLeft: 8 }}>计划详情</span>
                  {bucketTag(selPlan.status)}
                </Space>
              }
            >
              <h3 style={{ margin: "4px 0 10px", fontSize: 17 }}>{selPlan.workContent || "—"}</h3>
              <div style={{ borderTop: "1px solid #f0f2f5" }}>
                <FieldRow label="项目">{selPlan.project || "—"}</FieldRow>
                <FieldRow label="类别">{selPlan.category || "—"}</FieldRow>
                <FieldRow label="责任人">{selPlan.ownerName || "—"}</FieldRow>
                <FieldRow label="周期">{selPlan.cycleText || "—"}</FieldRow>
                <FieldRow label="计划节点">
                  {[selPlan.planStart, selPlan.planEnd].filter(Boolean).join(" 至 ") || "—"}
                </FieldRow>
                <FieldRow label="预算">
                  {selPlan.budgetAmount ? `${Number(selPlan.budgetAmount).toLocaleString()} 元` : "—"}
                </FieldRow>
              </div>
              <Space direction="vertical" style={{ width: "100%", marginTop: 12 }}>
                {planStatusBucket(selPlan.status) === "pending" && (
                  <Popconfirm title="派发执行任务并下发移动端?" onConfirm={() => onDispatch(selPlan)}>
                    <Button type="primary" size="large" block>
                      派发执行任务
                    </Button>
                  </Popconfirm>
                )}
                {planTaskOf(selPlan) && (
                  <Button block onClick={() => setSelTaskId(planTaskOf(selPlan)!.id)}>
                    查看任务进度
                  </Button>
                )}
                <Button
                  block
                  onClick={() => {
                    setEditing(selPlan);
                    form.setFieldsValue({
                      workContent: selPlan.workContent,
                      project: selPlan.project,
                      category: selPlan.category,
                      ownerName: selPlan.ownerName,
                      cycleText: selPlan.cycleText,
                      planEnd: selPlan.planEnd,
                    });
                  }}
                >
                  编辑计划
                </Button>
              </Space>
            </Card>
          ) : (
            <Card size="small">
              <Empty description="点击左侧计划或任务查看详情" />
            </Card>
          )}
        </div>
      </div>

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
    </ConfigProvider>
  );
}
