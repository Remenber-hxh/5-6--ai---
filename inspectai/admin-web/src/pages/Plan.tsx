import { Button, Card, Checkbox, Empty, Form, Input, Modal, Popconfirm, Segmented, Select, Skeleton, Space, Steps, Table, Tag, message } from "antd";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  AssetEntry,
  EngineeringPlan,
  EngineeringTask,
  PLAN_TYPES,
  WEEKDAY_OPTIONS,
  dispatchPlan,
  listAssets,
  listPlans,
  listTasks,
  savePlan,
  setTaskStatus,
} from "../api/mgmt";
import TodayInspection from "../components/TodayInspection";
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
  { key: "pending", label: "待执行", sub: "未开始", color: "#f5a524" },
  { key: "processing", label: "进行中", sub: "现场处理", color: "#246bfe" },
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
  return <Tag color="orange">需跟进</Tag>;
};

// 面板开合的时长与缓动。
//
// 【标准缓动:快出慢入】起步快、收尾慢,人眼看着像"被放到位"而不是"匀速滑过去"。
// 220ms 是有意的:再短就成了闪一下,再长(>300ms)每点一行都要等它,反而拖沓。
const EASE = "220ms cubic-bezier(0.22, 0.61, 0.36, 1)";
const PANEL_MOTION = { duration: 0.22, ease: [0.22, 0.61, 0.36, 1] as const };

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
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [bucket, setBucket] = useState<"" | Bucket>("");
  const [proj, setProj] = useState("");
  const [kw, setKw] = useState("");
  const [selPlanId, setSelPlanId] = useState("");
  const [selTaskId, setSelTaskId] = useState("");
  const [editing, setEditing] = useState<EngineeringPlan | null | "new">(null);
  const { project } = useUi();
  const [params, setParams] = useSearchParams();
  // 顶层视图。默认「今日执行」—— 打开这一页最想知道的就是今天还差什么,
  // 而不是"年度计划有几条"。
  //
  // 写进地址栏:刷新留在原处,也能把某一个视图的链接直接发人
  // (和用户页、提示词页同一套做法)。
  const view = params.get("view") || "today";
  const setView = (v: string) => {
    const next = new URLSearchParams(params);
    if (v === "today") next.delete("view");
    else next.set("view", v);
    setParams(next, { replace: true });
  };
  // 计划表按当前视图筛类型 —— 视图本身就是类型,不用再来一个筛选器
  const planType = view === "today" ? "" : view;
  const [form] = Form.useForm();

  const focusTask = params.get("task") || "";
  // 系统里关了动效的人(前庭功能敏感 / 远程桌面)照样要能用,只是不动
  const reduce = useReducedMotion();

  async function load() {
    setLoading(true);
    try {
      // 台账用于每日计划的设备多选。【取不到不该拖垮整页】——
      // 计划页的主体是计划和任务,设备清单只在建每日计划时用得上。
      const [ps, ts, as] = await Promise.all([
        listPlans(),
        listTasks(),
        listAssets().catch(() => [] as AssetEntry[]),
      ]);
      setPlans(ps);
      setTasks(ts);
      setAssets(as);
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


  const projects = useMemo(
    () => Array.from(new Set(realPlans.map((p) => p.project).filter(Boolean))) as string[],
    [realPlans],
  );

  const rows = useMemo(
    () =>
      realPlans.filter(
        (p) =>
          (!bucket || planStatusBucket(p.status) === bucket) &&
          (!proj || p.project === proj) &&
          // 【空 planType 当临时】存量计划没有这个字段,后端读出来会补成 adhoc,
          // 但前端也兜一层 —— 万一有别的入口写进来没走后端那条路
          (!planType || (p.planType || "adhoc") === planType) &&
          (!kw || (p.workContent || "").includes(kw) || (p.category || "").includes(kw)),
      ),
    [realPlans, bucket, proj, planType, kw],
  );

  // Agent 派发后深链 /plan?task=xxx:切「全部」并选中该复查任务(右侧详情面板直出)
  useEffect(() => {
    if (focusTask && tasks.some((t) => t.id === focusTask)) {
      setBucket("");
      setSelTaskId(focusTask);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tasks, focusTask]);

  // 默认选中首行(旧版行为:右侧面板不留白)。
  //
  // 【今日执行视图不做这件事】那一屏根本没有计划表,自动选中会让右侧凭空
  // 冒出一条「计划详情」—— 而它指向的计划在当前页面上一处都看不到,
  // 用户不知道它从哪来、也不知道自己在改什么。
  useEffect(() => {
    if (view === "today") {
      setSelPlanId("");
      return;
    }
    if (!selTaskId && (!selPlanId || !rows.some((r) => r.id === selPlanId))) {
      setSelPlanId(rows[0]?.id || "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, view]);

  // 【今日执行只认任务】那一屏有复查任务表,点它是合理的;
  // 计划则一律不显示 —— 页面上没有它的入口。
  const selPlan = view === "today" ? null : rows.find((p) => p.id === selPlanId) || null;
  const selTask = tasks.find((t) => t.id === selTaskId) || null;
  const hasPanel = Boolean(selTask || selPlan);

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

  // 首次加载:骨架屏(结构先行,不闪转圈)
  if (loading && plans.length === 0) {
    return (
      <div style={{ display: "grid", gridTemplateColumns: "minmax(0, 1fr) 396px", gap: 16, alignItems: "start" }}>
        <div>
          <Card size="small" style={{ marginBottom: 16 }}>
            <Skeleton active paragraph={{ rows: 1 }} />
          </Card>
          <Card size="small">
            <Skeleton active paragraph={{ rows: 8 }} />
          </Card>
        </div>
        <Card size="small">
          <Skeleton active paragraph={{ rows: 6 }} />
        </Card>
      </div>
    );
  }

  const hasFilter = Boolean(bucket || proj || planType || kw);
  const clearFilters = () => {
    setBucket("");
    setProj("");
    setKw("");
  };

  return (
    <>
      {/* 【右侧面板按需占位】没选中任何行时它是空的,却一直占着 396px。
          1366 宽的笔记本上主区只剩 900px,七列的表格会被挤到逐字换行
          (今天在用户页刚踩过)。选中了才让出空间。

          【收起是宽度归零,不是撤掉这一列】写成单列的话,正在退场的面板
          会瞬间掉到表格下面再淡出 —— 看着像"弹了一下"。留着这一列、
          把它压到 0 并由父级 overflow 裁掉,退场就是干净地被抹掉。 */}
      <div
        style={{
          display: "grid",
          // 【左列必须 minmax(0, 1fr)】写成 1fr 的话,内容列会按里面最宽的
          // 元素(七列表格)撑开,把右侧 396px 面板挤出视口 —— 截图里
          // 「派发执行任务」按钮右半边就是这么没的。
          gridTemplateColumns: hasPanel ? "minmax(0, 1fr) 396px" : "minmax(0, 1fr) 0px",
          gap: hasPanel ? 16 : 0,
          transition: reduce ? undefined : `grid-template-columns ${EASE}, gap ${EASE}`,
          alignItems: "start",
        }}
      >
        <div>
          {/* ===== 顶层六个视图 =====

              【这不是筛选,是切换视图】筛选是"在同一张表里缩小范围",而
              「今日执行」和「年度计划」根本不是同一种东西:一个回答"现在去干什么",
              一个回答"长期怎么排的"。放同一屏,人要不停切换视角 ——
              那正是客户说"混乱"的来源。每个视图只回答一个问题。 */}
          <Segmented
            value={view}
            onChange={(v) => setView(v as string)}
            style={{ marginBottom: 16 }}
            options={[
              { value: "today", label: "今日执行" },
              ...PLAN_TYPES.map((t) => ({ value: t.value, label: t.label })),
            ]}
          />

          {view === "today" && (
            <Card size="small" style={{ marginBottom: 16 }}>
              <TodayInspection />
            </Card>
          )}
          {/* 【待跟进归到执行视图】异常复查是"今天要处理的事",
              和长期计划台账不是一回事 —— 混在一起人要不停切换视角。 */}
          {view === "today" && showRecheck && (
            <Card title="复查任务" size="small" style={{ marginBottom: 16 }}>
              <Table<EngineeringTask>
                rowKey="id"
                size="middle"
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

          {/* 【这张表装的是计划,不是任务】原来标题写「巡检任务」,而
              dataSource 是 plans —— 同一页里另有一张真的任务表(复查任务)。
              用户点开一行,右侧详情展示的是计划的字段,却被告知这是任务。
              这是命名 bug,不是审美问题:人分不清自己在改什么。 */}
          {view !== "today" && (
          <Card title="巡检计划" size="small">
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
                  // 【类型跟随当前视图】在「周计划」里点新建,类型就该是周计划。
                  // 不带的话每次都要手动改回来,改漏了这条计划会跑到别的视图去,
                  // 而且不报错 —— 人只会觉得"我刚建的计划不见了"。
                  form.setFieldsValue({ planType: view === "today" ? "adhoc" : view });
                  form.resetFields();
                }}
              >
                新建计划
              </Button>
            </Space>
            <Table<EngineeringPlan>
              rowKey="id"
              size="middle"
              loading={loading}
              locale={{
                emptyText: (
                  <Empty
                    description={
                      hasFilter
                        ? "没有匹配的计划"
                        : `还没有${PLAN_TYPES.find((t) => t.value === view)?.label || "计划"}`
                    }
                  >
                    {hasFilter ? (
                      <Button onClick={clearFilters}>清除筛选</Button>
                    ) : (
                      <Button
                        type="primary"
                        onClick={() => {
                          setEditing("new");
                          form.resetFields();
                          form.setFieldsValue({ planType: view });
                        }}
                      >
                        新建{PLAN_TYPES.find((t) => t.value === view)?.label}
                      </Button>
                    )}
                  </Empty>
                ),
              }}
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
              // 【列要跟着视图变】每日计划的关键信息是"哪几天执行、管几台设备";
              // 而「周期」和「计划节点」对它没意义 —— 现在那两列全是横杠,
              // 占着宽度不说事。年度/月度正相反,起止日期才是重点。
              columns={[
                { title: "计划名称", dataIndex: "workContent" },
                { title: "项目", dataIndex: "project", width: 100 },
                ...(view === "daily"
                  ? [
                      {
                        title: "执行日",
                        width: 150,
                        render: (_: unknown, p: EngineeringPlan) => {
                          const wd = (p.weekdays || "").split(",").filter(Boolean);
                          // 空 = 每天。说"每天"而不是留空 —— 留空会被当成"没配好"
                          if (!wd.length) return <Tag color="blue">每天</Tag>;
                          if (wd.length === 7) return <Tag color="blue">每天</Tag>;
                          return (
                            <span>
                              {wd
                                .map((d) => WEEKDAY_OPTIONS.find((w) => w.value === d)?.label || d)
                                .join(" ")}
                            </span>
                          );
                        },
                      },
                      {
                        title: "设备",
                        width: 90,
                        render: (_: unknown, p: EngineeringPlan) => {
                          const n = p.assetIds?.length || 0;
                          // 0 台要标出来 —— 那条计划的完成率永远算不出来
                          return n > 0 ? `${n} 台` : <Tag color="orange">未指定</Tag>;
                        },
                      },
                    ]
                  : [
                      { title: "类型 / 点位", dataIndex: "category", width: 120, render: (v: string) => v || "—" },
                      {
                        title: "计划节点",
                        width: 160,
                        ellipsis: true,
                        render: (_: unknown, p: EngineeringPlan) =>
                          [p.planStart, p.planEnd].filter(Boolean).join(" 至 ") || p.planEnd || "—",
                      },
                    ]),
                { title: "责任人", dataIndex: "ownerName", width: 90 },
                { title: "状态", width: 88, render: (_, p) => bucketTag(p.status) },
              ]}
            />
          </Card>
          )}
        </div>

        {/* 右侧详情面板 —— 点了才有。
            【不放"点击左侧查看详情"的空态】那句话既没有信息也不需要人做什么:
            表格行本来就有 hover 和手型,点一下就出来了。一个常驻的空卡片
            只是在页面底部多一块灰,让人以为下面还有内容。 */}
        <div style={{ position: "sticky", top: 0, overflow: "hidden" }}>
          <AnimatePresence mode="wait" initial={false}>
            {hasPanel && (
              <motion.div
                // 【key 只分"任务/计划"两种,不用 id】用 id 的话,在计划表里
                // 换一行会整块重放一次进出动画 —— 内容明明只换了几个字,
                // 却闪一下,看着像页面刷新了。切换的是同一类详情就直接换内容。
                key={selTask ? "task" : "plan"}
                initial={reduce ? false : { opacity: 0, x: 20 }}
                animate={{ opacity: 1, x: 0 }}
                exit={reduce ? { opacity: 0 } : { opacity: 0, x: 20 }}
                transition={reduce ? { duration: 0 } : PANEL_MOTION}
                // 【宽度写死】外层那一列正在从 396 收到 0,不定宽的话面板会
                // 跟着被压扁,文字在退场过程中重排 —— 那正是"卡顿感"的来源。
                style={{ width: 396 }}
              >
                {selTask ? (
            <Card
              size="small"
              title={
                <Space>
                  <span style={{ borderLeft: "3px solid #12a968", paddingLeft: 8 }}>任务详情</span>
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
              <Space
                direction="vertical"
                style={{ width: "100%", borderTop: "1px solid #f0f2f5", paddingTop: 14 }}
              >
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
                  <span style={{ borderLeft: "3px solid #12a968", paddingLeft: 8 }}>计划详情</span>
                  {bucketTag(selPlan.status)}
                </Space>
              }
            >
              <h3 style={{ margin: "4px 0 10px", fontSize: 17 }}>{selPlan.workContent || "—"}</h3>
              <div style={{ borderTop: "1px solid #f0f2f5" }}>
                <FieldRow label="项目">{selPlan.project || "—"}</FieldRow>
                <FieldRow label="类别">{selPlan.category || "—"}</FieldRow>
                <FieldRow label="责任人">{selPlan.ownerName || "—"}</FieldRow>
                <FieldRow label="说明">{selPlan.cycleText || "—"}</FieldRow>
                <FieldRow label="计划节点">
                  {[selPlan.planStart, selPlan.planEnd].filter(Boolean).join(" 至 ") || "—"}
                </FieldRow>
                <FieldRow label="预算">
                  {selPlan.budgetAmount ? `${Number(selPlan.budgetAmount).toLocaleString()} 元` : "—"}
                </FieldRow>
              </div>
              <Space
                direction="vertical"
                style={{ width: "100%", marginTop: 12, borderTop: "1px solid #f0f2f5", paddingTop: 14 }}
              >
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
                      // 【这三项必须回填】不回填的话:一编辑保存,类型退回默认、
                      // 执行日和设备清单全被清空 —— 而且没有任何提示,
                      // 表现是"我只改了个负责人,第二天提醒就不来了"。
                      planType: selPlan.planType || "adhoc",
                      weekdayList: (selPlan.weekdays || "")
                        .split(",")
                        .map((x) => x.trim())
                        .filter(Boolean),
                      assetIds: selPlan.assetIds || [],
                    });
                  }}
                >
                  编辑计划
                </Button>
              </Space>
            </Card>
                ) : null}
              </motion.div>
            )}
          </AnimatePresence>
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
            // Checkbox.Group 给的是数组,后端要 "1,2,3" 的串。
            // 【必须排序】不排的话勾选顺序会被原样存下来("3,1,2"),
            // 虽然判定不受影响,但下次打开看到的顺序是乱的,像坏了。
            const { weekdayList, ...rest } = v;
            const payload = {
              ...(editing !== "new" && editing ? { id: editing.id } : {}),
              ...rest,
              weekdays: Array.isArray(weekdayList)
                ? [...weekdayList].sort().join(",")
                : undefined,
            };
            await savePlan(payload);
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
          {/* 【类型放在最前面】它决定下面还要填什么 —— 选了「每日巡检」才需要
              执行日和设备清单。放在后面的话人会先填一堆再发现要重来。 */}
          <Form.Item
            name="planType"
            label="计划类型"
            initialValue="adhoc"
            rules={[{ required: true, message: "请选择计划类型" }]}
          >
            <Select options={PLAN_TYPES.map((t) => ({ value: t.value, label: t.label }))} />
          </Form.Item>
          <Form.Item noStyle shouldUpdate={(a, b) => a.planType !== b.planType}>
            {({ getFieldValue }) =>
              getFieldValue("planType") === "daily" ? (
                <>
                  <Form.Item
                    name="weekdayList"
                    label="执行日"
                    extra="不选 = 每天执行"
                  >
                    <Checkbox.Group
                      options={WEEKDAY_OPTIONS.map((w) => ({ value: w.value, label: w.label }))}
                    />
                  </Form.Item>
                  {/* 【每日计划必须指定设备】完成情况是按设备自动判定的
                      (这些设备今天有没有巡检记录)。没有清单就永远算不出完成率,
                      而看板上它只会显示成一条空计划。后端也会拒绝。 */}
                  <Form.Item
                    name="assetIds"
                    label="要巡的设备"
                    extra="完成情况按这些设备自动判定 —— 巡检员正常拍照提交即可,不用另外打勾"
                    rules={[{ required: true, message: "每日巡检计划必须指定设备" }]}
                  >
                    <Select
                      mode="multiple"
                      allowClear
                      placeholder="从台账里选"
                      optionFilterProp="label"
                      options={assets.map((a) => ({
                        value: a.id,
                        label: `${a.assetName || a.assetKey}${a.project ? " · " + a.project : ""}`,
                      }))}
                    />
                  </Form.Item>
                </>
              ) : null
            }
          </Form.Item>
          {/* 【原来叫「周期」】有了计划类型和执行日之后它不参与任何逻辑了,
              还叫周期会和上面的类型打架 —— 人不知道该信哪个。改成纯说明。 */}
          <Form.Item name="cycleText" label="说明">
            <Input placeholder="补充说明,不参与排期" />
          </Form.Item>
          <Form.Item name="planEnd" label="截止日期">
            <Input placeholder="YYYY-MM-DD" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
