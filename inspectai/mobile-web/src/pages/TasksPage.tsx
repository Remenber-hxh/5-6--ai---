import { Skeleton, Toast } from "@/ui";
import { useNavigate } from "react-router-dom";

import EmptyState from "@/components/EmptyState";
import FlowHeader from "@/components/FlowHeader";
import StatusTag from "@/components/StatusTag";
import { listEngineeringTasks, startEngineeringTask } from "@/api/inspection";
import { useResource } from "@/hooks/useResource";

import type { EngineeringTaskDTO } from "@/api/inspection";
import { setActiveTask } from "@/store/activeTask";

// 「该显示哪些任务」的规则已经收到后端(?scope=open-mine,见 open_tasks.go):
// 在办三态 + 主管看全部 / 巡检员看自己的和未指派的。
// 这里不再筛第二遍 —— 客户端各筛各的,正是底栏角标和页面数字对不上的来源。

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
  // 竞态防护/卸载保护/错误提示都在 useResource 里(见 hooks/useResource.ts)
  const { data, loading } = useResource(
    (signal) => listEngineeringTasks(signal),
    [],
    {
      errorText: "任务加载失败",
    },
  );
  const tasks = loading ? null : (data ?? []);

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

  const sorted = [...tasks].sort(
    (a, b) =>
      (ORDER[a.status] ?? 3) - (ORDER[b.status] ?? 3) ||
      String(a.dueAt || "9999").localeCompare(String(b.dueAt || "9999")),
  );
  // 概览数字必须从【正在渲染的这份列表】算出来,否则数字和卡片数会对不上
  const counts = {
    doing: sorted.filter((t) => t.status === "进行中").length,
    fixing: sorted.filter((t) => t.status === "待整改").length,
    overdue: sorted.filter((t) => t.status === "逾期").length,
  };

  return (
    <div className="flow-screen">
      <FlowHeader title="我的任务" onBack={() => nav("/")} />

      <div className="scroll-area flow-body">
        <p className="flow-caption">待办的工程巡检任务</p>
        {sorted.length === 0 ? (
          <EmptyState
            icon="✓"
            title="任务清零"
            hint="暂无待办巡检任务,新任务下发后会出现在这里"
          />
        ) : (
          <>
            <div className="task-summary">
              <div className="ts-item">
                <b>{counts.doing}</b>
                <span>进行中</span>
              </div>
              <div className="ts-item">
                <b>{counts.fixing}</b>
                <span>待整改</span>
              </div>
              <div className={counts.overdue ? "ts-item warn" : "ts-item"}>
                <b>{counts.overdue}</b>
                <span>逾期</span>
              </div>
            </div>

            {sorted.map((t) => {
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
