import { Toast } from "@/ui";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  EngineeringTaskDTO,
  listEngineeringTasks,
  startEngineeringTask,
} from "@/api/inspection";
import { setActiveTask } from "@/store/activeTask";

/** 排序:逾期最先、再待执行、再进行中(旧版 TASK_STATUS_ORDER) */
const ORDER: Record<string, number> = { 逾期: 0, 待执行: 1, 进行中: 2 };

function statusClass(s: string): string {
  if (s === "逾期") return "overdue";
  if (s === "进行中") return "doing";
  return "pending";
}

function fmtDue(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getMonth() + 1}/${d.getDate()} 截止`;
}

// 我的任务:工程巡检任务闭环入口(旧版 sceneTasks)
export default function TasksPage() {
  const nav = useNavigate();
  const [tasks, setTasks] = useState<EngineeringTaskDTO[] | null>(null);

  useEffect(() => {
    void (async () => {
      try {
        setTasks(await listEngineeringTasks());
      } catch {
        Toast.show({ content: "任务加载失败" });
        setTasks([]);
      }
    })();
  }, []);

  async function start(task: EngineeringTaskDTO) {
    setActiveTask(task); // 记住当前任务,拍照提交时带上,提交后自动销账
    if (task.status === "待执行" || task.status === "逾期") {
      await startEngineeringTask(task.id);
    }
    Toast.show({ content: "已开始,请拍照巡检", position: "bottom" });
    nav("/");
  }

  if (!tasks) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  const open = tasks.filter((t) => t.status !== "已完成");
  const counts = {
    pending: open.filter((t) => t.status === "待执行").length,
    doing: open.filter((t) => t.status === "进行中").length,
    overdue: open.filter((t) => t.status === "逾期").length,
  };
  const sorted = [...open].sort(
    (a, b) =>
      (ORDER[a.status] ?? 3) - (ORDER[b.status] ?? 3) ||
      String(a.dueAt || "9999").localeCompare(String(b.dueAt || "9999")),
  );

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">我的任务</h1>
        <p className="flow-sub">待办的工程巡检任务</p>
      </div>

      <div className="scroll-area flow-body">
        {sorted.length === 0 ? (
          <div className="task-empty">暂无待办巡检任务</div>
        ) : (
          <>
            <div className="task-summary">
              <div className="ts-item">
                <b>{counts.pending}</b>
                <span>待执行</span>
              </div>
              <div className="ts-item">
                <b>{counts.doing}</b>
                <span>进行中</span>
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
                    <span className={`task-status ${statusClass(t.status)}`}>{t.status || "待执行"}</span>
                    {t.dueAt && <span className="task-due">{fmtDue(t.dueAt)}</span>}
                  </div>
                  <div className="task-title">{title}</div>
                  {t.assetName && <div className="task-asset">{t.assetName}</div>}
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

      <div className="flow-foot">
        <button className="task-back" onClick={() => nav("/")}>
          返回拍照
        </button>
      </div>
    </div>
  );
}
