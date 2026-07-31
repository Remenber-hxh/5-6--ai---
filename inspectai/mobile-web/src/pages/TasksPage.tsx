import { Skeleton, Toast } from "@/ui";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import {
  EngineeringTaskDTO,
  listEngineeringTasks,
  startEngineeringTask,
} from "@/api/inspection";
import { setActiveTask } from "@/store/activeTask";
import { useAuth } from "@/store/auth";

/**
 * 只有【已下发】的任务才该出现在巡检员手机上(旧版 myOpenTasks 的白名单)。
 *
 * 「待执行 / 待下发」= 管理员还没下发,巡检员不该看到;「已取消 / 已完成」同理。
 * 之前这里写的是黑名单 status !== "已完成",于是未下发和已取消的任务全漏了出来,
 * 而概览数字只统三种状态 —— 卡片数和数字对不上。改回白名单。
 */
const VISIBLE_STATUS = ["进行中", "待整改", "逾期"];

/** 排序:逾期最先,再进行中,再待整改(旧版 TASK_STATUS_ORDER 的语义) */
const ORDER: Record<string, number> = { 逾期: 0, 进行中: 1, 待整改: 2 };

/** 任务状态 → 全站统一标签的语义档位(.tag-*) */
function statusTone(s: string): string {
  if (s === "逾期") return "danger";
  if (s === "进行中") return "ok";
  return "brand";
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
  const user = useAuth((s) => s.user);
  const me = user?.displayName || user?.username || "";
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

  const dispatched = tasks.filter((t) => VISIBLE_STATUS.includes(t.status));
  // 优先只看派给自己的;一条都没派到就退回显示全部(旧版同样的兜底,
  // 免得任务没填负责人时巡检员看到空列表)
  const mine = dispatched.filter((t) => !t.assigneeName || t.assigneeName === me);
  const open = mine.length ? mine : dispatched;

  const sorted = [...open].sort(
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
          <div className="empty-state">
            <span className="es-badge">✓</span>
            <span className="es-title">任务清零</span>
            <span className="es-hint">暂无待办巡检任务,新任务下发后会出现在这里</span>
          </div>
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
                    <span className={`tag tag-${statusTone(t.status)}`}>{t.status || "待执行"}</span>
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
    </div>
  );
}
