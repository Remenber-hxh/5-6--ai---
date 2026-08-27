import { AutoComplete, Button, Card, Checkbox, DatePicker, Empty, Form, Input, InputNumber, Modal, Popconfirm, Segmented, Select, Skeleton, Space, Steps, Table, Tag, message } from "antd";
import dayjs, { Dayjs } from "dayjs";
import { AnimatePresence, motion, useReducedMotion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  AssetEntry,
  EngineeringPlan,
  EngineeringTask,
  PLAN_TYPES,
  ProjectEntry,
  ProjectScopeDTO,
  UserEntry,
  WEEKDAY_OPTIONS,
  deletePlan,
  deleteTask,
  dispatchPlan,
  listAssets,
  listPlans,
  listProjects,
  listTasks,
  listUsersWithScope,
  savePlan,
  setTaskStatus,
} from "../api/mgmt";
import OwnerBinding from "../components/OwnerBinding";
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

const DATE_FMT = "YYYY-MM-DD";
type PlanRange = [Dayjs | null, Dayjs | null] | undefined;

// 库里存的是日期字符串,RangePicker 要 dayjs 对象。
//
// 【解析不出来当没填,不要硬转】历史数据里有手打进来的 "2026年3月"、"3/1" 这种,
// dayjs 会给出 Invalid Date —— RangePicker 拿到它渲染成空白,而且这条计划
// 从此再也保存不了(校验永远不过),现象是"这一条编辑不了",查不到原因。
function toRange(start?: string, end?: string): PlanRange {
  const parse = (v?: string) => {
    if (!v) return null;
    const d = dayjs(v);
    return d.isValid() ? d : null;
  };
  const s = parse(start);
  const e = parse(end);
  return s || e ? [s, e] : undefined;
}

// 名字归一化。和后端的 ownerNameKey 保持一致 —— 两边算出不同的结果时,
// 表现是"表单说对上了,保存却没绑",而这种错没有任何报错。
// Excel 粘出来的名字中间带空格是常事,中文输入法敲的还是全角空格(U+3000)。
function ownerNameKey(s: string): string {
  return (s || "").replace(/[\s　]/g, "").toLowerCase();
}

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
  // 项目和人员用于表单的下拉。【项目必须是选的不是打的】——
  // 权限、数据范围、看板都按项目名精确匹配,手打多一个空格就成了
  // 一个谁都看不见的孤儿项目,而且不报错。
  const [projectList, setProjectList] = useState<ProjectEntry[]>([]);
  const [users, setUsers] = useState<UserEntry[]>([]);
  // 每个人能看到哪些项目。由后端算好 —— 前端拿 dataScope 自己推等于把规则
  // 复制一份,两边迟早说不一样的话。
  const [scopes, setScopes] = useState<Record<string, ProjectScopeDTO>>({});
  const [loading, setLoading] = useState(false);
  const [bucket, setBucket] = useState<"" | Bucket>("");
  const [proj, setProj] = useState("");
  const [kw, setKw] = useState("");
  const [selPlanId, setSelPlanId] = useState("");
  const [selTaskId, setSelTaskId] = useState("");
  const [editing, setEditing] = useState<EngineeringPlan | null | "new">(null);
  const [bindingOpen, setBindingOpen] = useState(false);
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
  // 表单里当前选的项目。负责人候选要跟着它变 —— 用 useWatch 而不是在
  // onChange 里手动同步:后者漏一条路径(回填、重置)就会和表单说不一样的话。
  const formProject = Form.useWatch<string | undefined>("project", form) || "";
  // 系统里关了动效的人(前庭功能敏感 / 远程桌面)照样要能用,只是不动
  const reduce = useReducedMotion();

  async function load() {
    setLoading(true);
    try {
      // 台账用于每日计划的设备多选。【取不到不该拖垮整页】——
      // 计划页的主体是计划和任务,设备清单只在建每日计划时用得上。
      const [ps, ts, as, prj, us] = await Promise.all([
        listPlans(),
        listTasks(),
        listAssets().catch(() => [] as AssetEntry[]),
        listProjects().catch(() => [] as ProjectEntry[]),
        listUsersWithScope().catch(() => ({ users: [] as UserEntry[], scopes: {} })),
      ]);
      setPlans(ps);
      setTasks(ts);
      setAssets(as);
      setProjectList(prj);
      setUsers(us.users);
      setScopes(us.scopes);
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

  // ===== 表单下拉的候选项 =====

  // 项目:只给在册且未停用的。
  //
  // 【正在编辑的那条要单独并进来】它的项目可能已经停用、或是早年手打进来的,
  // 不在候选里的话 Select 会显示成空白 —— 用户不改这一项、只改负责人,
  // 一保存项目就没了,而且界面上看不出发生过什么。
  const projectOptions = useMemo(() => {
    const opts = projectList
      .filter((p) => !p.disabled)
      .map((p) => ({ value: p.name, label: p.name }));
    const cur = editing && editing !== "new" ? editing.project : "";
    if (cur && !opts.some((o) => o.value === cur)) {
      opts.unshift({ value: cur, label: `${cur}(已停用 / 不在项目册)` });
    }
    return opts;
  }, [projectList, editing]);

  // 负责人:人员表做候选,但不锁死 —— 外委班组的人不一定有账号。
  //
  // 【userId 一起带着】选中候选时要把账号 ID 也存进计划,「我的计划」和
  // 点名提醒按 ID 过滤才准。只存名字的话,重名和改过名的就分不清了。
  //
  // 【状态值是 "disabled" 不是「停用」】界面上写中文,库里存英文。
  // 写成中文的话这个过滤【永远不成立】,停用的人照样出现在候选里 —— 踩过。
  //
  // 【按项目筛掉看不到的人】派给一个数据范围里没有这个项目的人,等于没派:
  // 他打开什么都没有,而派的人以为派出去了 —— 两边都不知道对方在等什么,
  // 要到"提醒该来没来"那天才暴露。后端也会拦(owner_cannot_see_project),
  // 这里筛掉是为了不让人填完一整张表才被打回来。
  const ownerOptions = useMemo(
    () =>
      users
        .filter((u) => u.status !== "disabled")
        .filter((u) => {
          if (!formProject) return true; // 还没选项目,先都列出来
          const sc = scopes[u.id];
          if (!sc) return true; // 后端没给范围信息就不筛 —— 宁可让后端拦,也别静默少人
          if (sc.blocked) return false;
          if (sc.seesAll) return true;
          return (sc.projects || []).includes(formProject);
        })
        .map((u) => {
          const name = u.displayName || u.username || "";
          return {
            value: name,
            userId: u.id,
            label: u.departmentName ? `${name} · ${u.departmentName}` : name,
          };
        })
        .filter((o) => o.value),
    [users, scopes, formProject],
  );

  // ===== 负责人名字 → 账号的解析 =====
  //
  // 【为什么必须有这一步】编辑一条老计划时,负责人栏里显示着一个正确的名字,
  // 但 owner_id 是空的。不解析的话:用户改完保存 → 还是「未绑账号」,
  // 而表单上完全看不出还差一步 —— 他只会反复编辑、反复失败。
  //
  // 【和迁移里拒绝"自动全绑"不矛盾】那里是一次几十条、没人在看;
  // 这里是一条计划、一个名字、人正盯着屏幕,而且下面会明确写出对应到了谁。
  // 唯一命中才自动填,重名一律不猜。
  const ownerName = Form.useWatch<string | undefined>("ownerName", form) || "";
  const ownerId = Form.useWatch<string | undefined>("ownerId", form) || "";

  const ownerResolve = useMemo(() => {
    const key = ownerNameKey(ownerName);
    if (!key) return { eligible: [], anyMatch: false };
    const eligible = ownerOptions.filter((o) => ownerNameKey(o.value) === key);
    const anyMatch = users.some(
      (u) =>
        u.status !== "disabled" &&
        (ownerNameKey(u.displayName || "") === key || ownerNameKey(u.username || "") === key),
    );
    return { eligible, anyMatch };
  }, [ownerName, ownerOptions, users]);

  // 唯一命中就填上。填的是隐藏字段,所以下面的 extra 必须把结果说出来 ——
  // 否则就成了"系统偷偷替我决定了负责人是谁"。
  useEffect(() => {
    if (!editing || ownerId) return;
    if (ownerResolve.eligible.length === 1) {
      form.setFieldsValue({ ownerId: ownerResolve.eligible[0].userId });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [ownerResolve, ownerId, editing]);

  const ownerHint = useMemo(() => {
    if (!ownerName) return "选人员表里的人才能收到每日提醒;手填的名字只做记录";
    if (ownerId) {
      const hit = ownerOptions.find((o) => o.userId === ownerId);
      return `已对应账号:${hit?.label || ownerId} —— 保存后按人过滤和提醒都按它算`;
    }
    if (ownerResolve.eligible.length > 1) {
      return `「${ownerName}」对应 ${ownerResolve.eligible.length} 个账号,请从下拉里选一个`;
    }
    if (ownerResolve.anyMatch) {
      return `「${ownerName}」的账号看不到「${formProject}」—— 派给他也看不到,请换人或先分配项目`;
    }
    return `人员表里没有「${ownerName}」—— 仍可保存(外委人员就是这种),只是收不到每日提醒`;
  }, [ownerName, ownerId, ownerOptions, ownerResolve, formProject]);

  // 被项目范围筛掉了几个人。要说出来 —— 不说的话候选列表凭空变短,
  // 用户只会觉得"怎么找不到老张了",而不知道是范围没配。
  const hiddenOwnerCount = useMemo(() => {
    if (!formProject) return 0;
    const active = users.filter((u) => u.status !== "disabled").length;
    return Math.max(0, active - ownerOptions.length);
  }, [users, ownerOptions, formProject]);

  // 【改了项目就要重新检查负责人】先选人再改项目的话,那个人可能看不到
  // 新项目了。不清掉的话表单看着完全正常,一提交才被后端打回来 ——
  // 而那时用户已经填完整张表,还得自己猜是哪一项不对。
  useEffect(() => {
    if (!editing || !formProject) return;
    const curId = form.getFieldValue("ownerId");
    if (!curId) return;
    if (ownerOptions.some((o) => o.userId === curId)) return;
    form.setFieldsValue({ ownerId: "", ownerName: "" });
    message.warning(`原负责人看不到「${formProject}」,已清空 —— 请重新选`);
    // ownerOptions 是按 formProject 算出来的,依赖它就够了
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [formProject, ownerOptions]);

  // 正在编辑的这条,有没有存着控件读不出来的日期。
  const unparsedDates = useMemo(() => {
    if (!editing || editing === "new") return [];
    return [editing.planStart, editing.planEnd].filter(
      (v): v is string => Boolean(v) && !dayjs(v).isValid(),
    );
  }, [editing]);

  // 要巡的设备:只列当前项目的。
  //
  // 【原来是全量,这是个真漏洞】一条「会议中心」的每日计划里混进紫菡雅集的
  // 设备,那些设备名就会出现在会议中心巡检员的今日待巡清单里 ——
  // 数据是按计划的项目授权的,设备却来自另一个项目,权限在这里被绕过去了。
  //
  // 没选项目时给空列表并禁用,不是给全量:让人先选项目,比让他选完
  // 一堆设备再发现选错了项目要好。
  const assetOptions = useMemo(() => {
    if (!formProject) return [];
    return assets
      .filter((a) => (a.project || "") === formProject)
      .map((a) => ({
        value: a.id,
        label: `${a.assetName || a.assetKey}${a.assetType ? " · " + a.assetType : ""}`,
      }));
  }, [assets, formProject]);

  // 改项目后,已选的设备可能不属于新项目了。和负责人同一个道理:
  // 不清掉的话表单看着正常,提交后这条计划就横跨两个项目,而且不报错。
  useEffect(() => {
    if (!editing || !formProject) return;
    const cur: string[] = form.getFieldValue("assetIds") || [];
    if (!cur.length) return;
    const ok = cur.filter((id) => assetOptions.some((o) => o.value === id));
    if (ok.length === cur.length) return;
    form.setFieldsValue({ assetIds: ok });
    message.warning(`已移除 ${cur.length - ok.length} 台不属于「${formProject}」的设备`);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [formProject, assetOptions]);

  // 类型 / 点位:没有主数据表,拿历史填过的值做候选,避免同一类点位写出五种叫法。
  const categoryOptions = useMemo(
    () =>
      Array.from(new Set(plans.map((p) => p.category).filter(Boolean)))
        .sort()
        .map((c) => ({ value: c as string })),
    [plans],
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

  // 【删除和取消不是一回事,文案要说清】取消是"这活不做了",记录还在;
  // 删除是这条从来没存在过。后端会拦住有巡检记录 / 已派发任务的,
  // 拦回来的话把原话直接给用户看 —— 那句话里已经写了该怎么办。
  async function onDelete(kind: "plan" | "task", id: string) {
    try {
      if (kind === "plan") {
        await deletePlan(id);
        setSelPlanId("");
      } else {
        await deleteTask(id);
        setSelTaskId("");
      }
      message.success("已删除");
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "删除失败");
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
                  // 【顺序不能反】resetFields 会把表单退回 initialValue(adhoc)。
                  // 原来写成先 set 后 reset —— 于是在「月度计划」里点新建,
                  // 类型永远显示「临时计划」,人得每次手动改回来。
                  form.resetFields();
                  // 【类型跟随当前视图】在「周计划」里点新建,类型就该是周计划。
                  // 改漏了这条计划会跑到别的视图去,而且不报错 ——
                  // 人只会觉得"我刚建的计划不见了"。
                  form.setFieldsValue({ planType: view === "today" ? "adhoc" : view });
                }}
              >
                新建计划
              </Button>
              {/* 【放在这里而不是单开一页】它是一次性的整理动作,不是日常功能。
                  单开一页会让人以为这是要经常来的地方。 */}
              <Button type="text" onClick={() => setBindingOpen(true)}>
                负责人绑定
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
                {/* 【删除放在最后、用文字按钮】它和上面那些不是同一类操作:
                    那些推进流程,这条抹掉一条数据。做成同样显眼的按钮
                    会让人在赶时间时误点。 */}
                <Popconfirm
                  title="删除这个任务?"
                  description="删除后无法恢复。只想停掉它的话请用「取消任务」——那样会留痕。"
                  okText="删除"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => onDelete("task", selTask.id)}
                >
                  <Button block type="text" danger>
                    删除任务
                  </Button>
                </Popconfirm>
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
                {/* 【标出有没有绑账号】绑了才收得到每日提醒。不标的话这两种
                    情况在界面上长得一模一样,而差别要到提醒该来没来那天才暴露。 */}
                <FieldRow label="责任人">
                  {selPlan.ownerName || "—"}
                  {selPlan.ownerName && !selPlan.ownerId && (
                    <Tag color="orange" style={{ marginLeft: 8, fontWeight: 400 }}>
                      未绑账号
                    </Tag>
                  )}
                </FieldRow>
                <FieldRow label="说明">{selPlan.cycleText || "—"}</FieldRow>
                <FieldRow label="计划节点">
                  {[selPlan.planStart, selPlan.planEnd].filter(Boolean).join(" 至 ") || "—"}
                </FieldRow>
                <FieldRow label="预算">
                  {selPlan.budgetAmount ? `${Number(selPlan.budgetAmount).toLocaleString()} 元` : "—"}
                </FieldRow>
                {/* 【备注以前是只写不读的】表单里能填,但任何地方都不显示 ——
                    填过的人以为记下来了,实际上再也看不到。有才显示,没有不占行。 */}
                {selPlan.remark && <FieldRow label="备注">{selPlan.remark}</FieldRow>}
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
                    // 【先 reset 再回填】上一次打开留在表单里的值不清掉的话,
                    // 会串到这条计划上 —— 比如上次编辑的设备清单原样跟过来。
                    form.resetFields();
                    form.setFieldsValue({
                      workContent: selPlan.workContent,
                      project: selPlan.project,
                      category: selPlan.category,
                      ownerName: selPlan.ownerName,
                      ownerId: selPlan.ownerId || "",
                      cycleText: selPlan.cycleText,
                      remark: selPlan.remark,
                      budgetAmount: selPlan.budgetAmount || undefined,
                      planRange: toRange(selPlan.planStart, selPlan.planEnd),
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
                {/* 计划也给删除:库里已经攒了一批名字叫「1」的测试行,
                    没有删除键的话它们会一直堵在列表里 —— 而"看着乱"
                    最后会变成"没人看"。已派发过任务的后端会拦住。 */}
                <Popconfirm
                  title="删除这条计划?"
                  description="删除后无法恢复。已派发过任务的计划删不掉——需要先处理那些任务。"
                  okText="删除"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => onDelete("plan", selPlan.id)}
                >
                  <Button block type="text" danger>
                    删除计划
                  </Button>
                </Popconfirm>
              </Space>
            </Card>
                ) : null}
              </motion.div>
            )}
          </AnimatePresence>
        </div>
      </div>

      <OwnerBinding
        open={bindingOpen}
        onClose={(changed) => {
          setBindingOpen(false);
          if (changed) void load(); // 绑过就刷新,否则详情面板还挂着「未绑账号」
        }}
      />

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
            const { weekdayList, planRange, ...rest } = v;
            const [start, end] = (planRange as PlanRange) || [];
            // 【编辑时以原记录打底】后端保存是整行覆盖(upsert 把每一列都写成
            // 传来的值),表单没管到的列会被写成空。原来只传了表单里那几项,
            // 于是改一次负责人,就把序号、业务类型、风险等级、已派发任务的关联
            // 一并清空,状态还被打回「待执行」——「派发执行任务」按钮重新出现,
            // 同一条计划能派发第二次。整条链路一个错都不报。
            const base = editing !== "new" && editing ? editing : null;
            // 【没动过日期就原样带回去,不要用控件的空值覆盖】
            // toRange 对解析不了的历史值(手打的 "2026年3月"、"3/1")返回空,
            // 于是 RangePicker 显示成空 —— 如果照着控件写回去,用户只是改了个
            // 负责人,原来的日期就被抹掉了,而且界面上看不出发生过什么。
            // 用 isFieldTouched 区分"没碰过"和"主动清空":后者仍然要生效。
            const rangeTouched = form.isFieldTouched("planRange");
            const dateOut = (d: Dayjs | null | undefined, old?: string) =>
              d ? d.format(DATE_FMT) : rangeTouched ? "" : old || "";
            const payload = {
              ...(base || {}),
              ...rest,
              weekdays: Array.isArray(weekdayList)
                ? [...weekdayList].sort().join(",")
                : undefined,
              planStart: dateOut(start, base?.planStart),
              planEnd: dateOut(end, base?.planEnd),
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
          {/* 【项目只能选,不能打】权限、数据范围、今日看板都按项目名精确匹配。
              手打的话多一个空格就匹配不上,这条计划对被限定项目的人直接不可见,
              而且两边都不报错 —— 建的人以为发出去了,该看的人一直没看到。
              留空也一样:后端会补成「默认项目」,而项目册里没有这个项目,
              结果还是只有看得到全部数据的人才见得着。 */}
          <Form.Item
            name="project"
            label="项目"
            rules={[{ required: true, message: "必须选项目 —— 留空会归到「默认项目」,被限定项目的人看不到" }]}
          >
            <Select
              showSearch
              optionFilterProp="label"
              placeholder="从项目册里选"
              options={projectOptions}
              notFoundContent="项目册里还没有项目,先去「项目管理」登记"
            />
          </Form.Item>
          {/* 【这两个用 AutoComplete 不用 Select】点位叫法和外委班组的人名
              都没有主数据表,锁死会让人填不进去;但给候选能防住
              同一个东西写出五种叫法(「有机房电梯」/「有机房 电梯」/「电梯-有机房」)。 */}
          <Form.Item name="category" label="类型 / 点位">
            <AutoComplete
              allowClear
              options={categoryOptions}
              placeholder="如:有机房电梯"
              filterOption={(input, option) => String(option?.value ?? "").includes(input)}
            />
          </Form.Item>
          <Form.Item
            name="ownerName"
            label="负责人"
            extra={
              hiddenOwnerCount > 0
                ? `${ownerHint}(已隐藏 ${hiddenOwnerCount} 人:数据范围里没有「${formProject}」)`
                : ownerHint
            }
          >
            <AutoComplete
              allowClear
              options={ownerOptions}
              placeholder="从人员里选,外委人员可直接填"
              filterOption={(input, option) => String(option?.label ?? "").includes(input)}
              // 【名字一变就重算绑定】手打成一个对不上账号的名字时必须把
              // ownerId 清掉 —— 留着旧 ID 的话,这条计划显示着新名字、
              // 提醒却还发给旧账号那个人,而界面上完全看不出来。
              onChange={(v) => {
                const hit = ownerOptions.find((o) => o.value === v);
                form.setFieldValue("ownerId", hit?.userId || "");
              }}
            />
          </Form.Item>
          {/* 绑定的账号 ID。不给人看也不给人改 —— 它是「负责人」那一栏选出来的
              结果,单独摆出来只会让人以为这是两件要分别填的事。 */}
          <Form.Item name="ownerId" hidden>
            <Input />
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
                      placeholder={formProject ? "从台账里选" : "先选上面的项目"}
                      disabled={!formProject}
                      optionFilterProp="label"
                      options={assetOptions}
                    />
                  </Form.Item>
                </>
              ) : (
                // 预算只对工程类计划有意义,每日巡检不该问这个。
                // 详情面板一直在显示「预算」,但表单以前根本没有这一项 ——
                // 也就是说那个数字只能靠导入,后台永远填不进去。
                <Form.Item name="budgetAmount" label="预算">
                  {/* 【显式给泛型】不写的话 TS 会从 min={0} 把值类型推成字面量 0,
                      parser 返回任何别的数字都编译不过。 */}
                  <InputNumber<number>
                    min={0}
                    precision={2}
                    style={{ width: "100%" }}
                    addonAfter="元"
                    placeholder="可不填"
                    formatter={(v) => (v == null ? "" : `${v}`.replace(/\B(?=(\d{3})+(?!\d))/g, ","))}
                    parser={(v) => (v ? Number(v.replace(/,/g, "")) : 0)}
                  />
                </Form.Item>
              )
            }
          </Form.Item>
          {/* 【日期用选的】原来是手打 "YYYY-MM-DD" 的文本框:打错格式不报错,
              存进去之后排序、筛选、看板全部按字符串比,一条 "2026/3/1" 会永远排在最前面。
              而且起始日期以前根本没有入口 —— 列表的「计划节点」列只能显示单边。 */}
          {/* 【原值解析不了就说出来】历史数据里有手打的 "2026年3月"、"3/1",
              控件显示不了它们,只能是空白 —— 不提示的话用户会以为这条计划
              本来就没填日期,而它其实是有值的,只是选了新日期就会被替换掉。 */}
          <Form.Item
            name="planRange"
            label="计划区间"
            extra={
              unparsedDates.length
                ? `原值「${unparsedDates.join(" / ")}」不是标准日期,控件显示不出来。不动它就保留原样,选了新日期才会替换。`
                : "留空 = 长期有效"
            }
          >
            <DatePicker.RangePicker
              style={{ width: "100%" }}
              format={DATE_FMT}
              allowEmpty={[true, true]}
              placeholder={["开始日期", "截止日期"]}
            />
          </Form.Item>
          {/* 【原来叫「周期」】有了计划类型和执行日之后它不参与任何逻辑了,
              还叫周期会和上面的类型打架 —— 人不知道该信哪个。改成纯说明。 */}
          <Form.Item name="cycleText" label="说明">
            <Input placeholder="补充说明,不参与排期" />
          </Form.Item>
          <Form.Item name="remark" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
