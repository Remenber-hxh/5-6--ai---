import { EngineeringTaskDTO } from "@/api/inspection";

// 当前正在执行的工程任务。
// 存 localStorage:巡检员从任务页点"开始巡检"后要切到拍照台、可能中途切后台,
// 关键是提交记录时要带上 engineeringTaskId,后端据此自动销账闭环。

const KEY = "inspectai_mobile_active_task";

export function setActiveTask(task: EngineeringTaskDTO | null) {
  try {
    if (task) localStorage.setItem(KEY, JSON.stringify(task));
    else localStorage.removeItem(KEY);
  } catch {
    /* 隐私模式下写不进就算了,不影响主流程 */
  }
}

export function getActiveTask(): EngineeringTaskDTO | null {
  try {
    const raw = localStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as EngineeringTaskDTO) : null;
  } catch {
    return null;
  }
}

export function clearActiveTask() {
  setActiveTask(null);
}
