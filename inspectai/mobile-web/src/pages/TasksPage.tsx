import { Skeleton, Toast } from "@/ui";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import EmptyState from "@/components/EmptyState";
import FlowHeader from "@/components/FlowHeader";
import SectionHeader from "@/components/SectionHeader";
import StatusTag from "@/components/StatusTag";
import {
  getTodayBoard,
  listEngineeringTasks,
  startEngineeringTask,
} from "@/api/inspection";
import { useResource } from "@/hooks/useResource";

import type { EngineeringTaskDTO, TodayAssetDTO } from "@/api/inspection";
import { setActiveTask } from "@/store/activeTask";
import { setRetakeTarget } from "@/store/retake";

// 「该显示哪些任务」的规则在后端(?scope=open-mine,见 open_tasks.go):
// 在办三态 + 主管看全部 / 巡检员看自己的和未指派的。
//
// 【下面这个白名单不是"再筛一遍",是渲染不变量】
// 这一页的每张卡片都带一个「开始巡检」按钮。已完成/已取消的任务配这个按钮
// 是句病句 —— 用户点了会发生什么谁也说不清。所以这一页【只渲染能开始巡检
// 的任务】,这条不依赖后端。
//
// 2026-08-04 就栽在这上面:我先把客户端的筛选去掉、后端还没重启,于是
// 老接口发来的全量任务(含已完成)全渲染出来了,每张都挂着「开始巡检」。
// 前后端版本错位是常态(SPA 还会被浏览器缓存住),页面不能假设服务端一定
// 筛好了。注意这里【不】按负责人筛 —— 那个才是当初角标对不上的来源。
const CAN_START = ["进行中", "待整改", "逾期"];

/** 排序:逾期最先,再进行中,再待整改(旧版 TASK_STATUS_ORDER 的语义) */
const ORDER: Record<string, number> = { 逾期: 0, 进行中: 1, 待整改: 2 };

function fmtDue(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getMonth() + 1}/${d.getDate()} 截止`;
}

// 我的任务:工程巡检任务闭环入口(旧版 sceneTasks)
export default function TasksPage() {
  const nav = useNavigate();
  // 点概览数字筛列表。再点一次取消 —— 和台账那四个数字同一个交互,
  // 不另设"清除筛选"按钮:数字本身就是开关,少一个控件少一分学习成本。
  const [pick, setPick] = useState<string | null>(null);
  // 竞态防护/卸载保护/错误提示都在 useResource 里(见 hooks/useResource.ts)
  const { data, loading } = useResource(
    (signal) => listEngineeringTasks(signal),
    [],
    {
      errorText: "任务加载失败",
    },
  );
  const tasks = loading ? null : (data ?? []);

  // 今日待巡。【拉不到不该拖垮整页】—— 这一页原本的主体是工程任务,
  // 今日待巡是加上去的一段。errorText: null:它自己有空态,不弹提示。
  const { data: board } = useResource((signal) => getTodayBoard(signal), [], {
    errorText: null,
  });

  // 【按设备去重】两条每日计划可能都点了同一台("每日例检"和"重点关注"),
  // 不去重的话同一台会在清单里出现两次,巡检员会以为要拍两遍。
  const todo = useMemo(() => {
    const out: TodayAssetDTO[] = [];
    const seen = new Set<string>();
    for (const p of board?.plans ?? []) {
      for (const a of p.assets ?? []) {
        if (a.done || seen.has(a.assetId)) continue;
        seen.add(a.assetId);
        out.push(a);
      }
    }
    return out;
  }, [board]);

  // 点一台就去巡它。复用扫码那套锁定机制 —— 扫码和"从清单里点"
  // 对用户是两个入口,对系统是同一件事:已经确认了是哪台设备。
  //
  // 【mode 必须是 scan 不是 recheck】写成 recheck 的话,首页会挂一条
  // 写着"复检"的横幅、提交后弹"复检已提交",而数据上这就是一次正常巡检 ——
  // 巡检员会以为自己刚做的是复查,主管看记录也会被这句话误导。
  function startAsset(a: TodayAssetDTO) {
    if (a.missing) return;
    setRetakeTarget({
      mode: "scan",
      assetId: a.assetId,
      templateId: a.templateId || "",
      pointId: a.pointId || "",
      assetNo: a.assetKey || "",
      assetName: a.assetName,
    });
    Toast.show({ content: `对准「${a.assetName}」拍照即可` });
    nav("/");
  }

  async function start(task: EngineeringTaskDTO) {
    setActiveTask(task); // 记住当前任务,拍照提交时带上,提交后自动销账
    if (task.status !== "进行中") {
      await startEngineeringTask(task.id);
    }
    Toast.show({ content: "已开始,请拍照巡检", position: "bottom" });
    nav("/");
  }

  // 骨架列表代替空屏转圈:先出结构,数据到了填进去,视觉不跳变
  if (!tasks) {
    return (
      <div className="flow-screen">
        <FlowHeader title="我的任务" onBack={() => nav("/")} />
        <div className="scroll-area flow-body">
          <Skeleton rows={4} className="sk-list sk-card" />
        </div>
      </div>
    );
  }

  const sorted = tasks
    .filter((t) => CAN_START.includes(t.status))
    .sort(
      (a, b) =>
        (ORDER[a.status] ?? 3) - (ORDER[b.status] ?? 3) ||
        String(a.dueAt || "9999").localeCompare(String(b.dueAt || "9999")),
    );
  // 概览数字必须从【正在渲染的这份列表】算出来,否则数字和卡片数会对不上
  // 概览数字必须从【正在渲染的这份列表】算出来,否则数字和卡片数会对不上
  const counts: Record<string, number> = {};
  for (const st of CAN_START)
    counts[st] = sorted.filter((t) => t.status === st).length;

  // 点中某一档时列表只留那一档;数字本身不跟着变 —— 它们是"总共有多少",
  // 跟着变的话点一下别的档就看不到了,等于把筛选入口自己关掉。
  const shown = pick ? sorted.filter((t) => t.status === pick) : sorted;

  return (
    <div className="flow-screen">
      <FlowHeader title="我的任务" onBack={() => nav("/")} />

      <div className="scroll-area flow-body">
        {/* ===== 今日待巡 =====

            【和下面的复查任务分成两段,不合并成一个列表】两者性质不同:
            这里的一台设备是「今天还没有巡检快照」——一个算出来的状态,
            没有 ID、没有状态流转,拍照提交就自动销账;
            下面的复查任务是一条真实记录,要人操作状态。
            硬塞进同一个列表,就得给今日待巡编一个假的 ID 和假的状态机,
            那正是后台那一页当初被说"混乱"的来源。 */}
        {board && (
          <>
            <div className="tasks-sec-head">
              <SectionHeader
                title="今日待巡"
                count={todo.length}
                tone={todo.length ? "risk" : "ok"}
              />
            </div>
            {/* 【空的时候也要说话】原来 total=0 就整段不渲染 —— 于是
                "今天没排计划"、"计划里没填设备"、"接口挂了"三种情况
                在屏幕上长得一模一样:什么都没有。排查时无从下手,
                用户也只会觉得"这功能没做"。 */}
            {board.total === 0 ? (
              <p className="today-clear">
                {(board.plans ?? []).some((p) => p.noAssets)
                  ? "今天有每日计划,但计划里没写要巡哪些设备 —— 请联系管理员补上"
                  : "今天没有排给你的巡检计划"}
              </p>
            ) : todo.length === 0 ? (
              <p className="today-clear">今天的 {board.total} 台已全部巡完</p>
            ) : (
              <div className="today-list">
                {todo.map((a) => (
                  <button
                    key={a.assetId}
                    className="today-item"
                    // 台账里已经删掉的点不了 —— 跳过去是个死路,
                    // 比不能点更让人困惑。但仍要显示,否则这台会永远
                    // 算作未完成而没人知道为什么。
                    disabled={a.missing}
                    onClick={() => startAsset(a)}
                  >
                    <span className="ti-dot" aria-hidden />
                    <span className="ti-main">
                      <b>{a.assetName}</b>
                      <span className="ti-sub">
                        {a.missing ? "台账里已删除,请联系管理员" : a.project || ""}
                      </span>
                    </span>
                    {!a.missing && <span className="ti-go">去巡检</span>}
                  </button>
                ))}
              </div>
            )}
          </>
        )}

        <div className="tasks-sec-head">
          <SectionHeader title="复查任务" count={sorted.length} />
        </div>
        {sorted.length === 0 ? (
          <EmptyState
            icon="✓"
            title="任务清零"
            hint="暂无待办巡检任务,新任务下发后会出现在这里"
          />
        ) : (
          <>
            <div className="task-summary">
              {CAN_START.map((st) => {
                const n = counts[st];
                const on = pick === st;
                return (
                  <button
                    key={st}
                    className={[
                      "ts-item",
                      st === "逾期" && n ? "warn" : "",
                      on ? "on" : "",
                    ]
                      .filter(Boolean)
                      .join(" ")}
                    // 数字是 0 的那一档点了只会得到空列表,直接禁掉
                    disabled={n === 0}
                    onClick={() => setPick(on ? null : st)}
                  >
                    <b>{n}</b>
                    <span>{st}</span>
                  </button>
                );
              })}
            </div>

            {shown.map((t) => {
              const title = t.title || t.workContent || "巡检任务";
              const showDesc = t.workContent && !title.includes(t.workContent);
              return (
                <article className="task-card" key={t.id}>
                  <div className="task-card-head">
                    <StatusTag text={t.status || "待执行"} />
                    {t.dueAt && (
                      <span className="task-due">{fmtDue(t.dueAt)}</span>
                    )}
                  </div>
                  <div className="task-title">{title}</div>
                  {t.assetName && (
                    <div className="task-asset">{t.assetName}</div>
                  )}
                  {showDesc && <p className="task-desc">{t.workContent}</p>}
                  <button className="task-start" onClick={() => void start(t)}>
                    开始巡检
                  </button>
                </article>
              );
            })}
          </>
        )}
      </div>
    </div>
  );
}
