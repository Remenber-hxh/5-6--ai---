// 操作日志字典:动作/对象类型的中文与色彩(枚举值以 go-backend recordOperation 调用点为准)

export const ACTION_LABEL: Record<string, string> = {
  login: "登录",
  logout: "登出",
  ai_chat: "AI 问答",
  agent_recheck_create: "派发复检",
  agent_recheck_redispatch: "重新派发",
  "asset.patch": "更新资产",
  "change_request.create": "提交修改申请",
  "change_request.approve": "审批通过",
  "change_request.reject": "审批驳回",
  engineering_plan_save: "保存计划",
  engineering_task_create: "创建任务",
  engineering_task_status: "更新任务状态",
  engineering_task_auto_closed: "任务自动闭环",
  engineering_task_close_failed: "任务闭环失败",
  "user.create": "新建用户",
  "user.update": "更新用户",
  "user.reset_password": "重置密码",
  "user.set_status": "启用/停用用户",
  "wework.message.send": "企微应用通知",
  "wework.bot_message.send": "企微群通知",
};

// 语义色:绿=通过/闭环,红=驳回/失败,橙=改动,蓝=派发/创建,青=通知/AI
export const ACTION_COLOR: Record<string, string> = {
  login: "blue",
  logout: "default",
  ai_chat: "cyan",
  agent_recheck_create: "geekblue",
  agent_recheck_redispatch: "geekblue",
  "asset.patch": "orange",
  "change_request.create": "orange",
  "change_request.approve": "green",
  "change_request.reject": "red",
  engineering_plan_save: "orange",
  engineering_task_create: "geekblue",
  engineering_task_status: "orange",
  engineering_task_auto_closed: "green",
  engineering_task_close_failed: "red",
  "user.create": "geekblue",
  "user.update": "orange",
  "user.reset_password": "orange",
  "user.set_status": "orange",
  "wework.message.send": "cyan",
  "wework.bot_message.send": "cyan",
};

export const TARGET_LABEL: Record<string, string> = {
  user: "用户",
  ai: "AI 服务",
  record: "巡检记录",
  asset: "资产",
  engineering_task: "任务",
  engineering_plan: "计划",
  wework: "企微应用",
  wework_bot: "企微群机器人",
};

export const actionLabel = (a?: string) => ACTION_LABEL[a || ""] || a || "—";
export const actionColor = (a?: string) => ACTION_COLOR[a || ""] || "default";
export const targetLabel = (t?: string) => TARGET_LABEL[t || ""] || t || "—";

// detail 展平为「键=值」串(替代原始 JSON,可读)
export function fmtDetail(detail?: Record<string, unknown> | null): string {
  if (!detail || typeof detail !== "object") return "";
  return Object.entries(detail)
    .filter(([, v]) => v !== null && v !== undefined && v !== "")
    .map(([k, v]) => `${k}=${typeof v === "object" ? JSON.stringify(v) : String(v)}`)
    .join(" · ");
}
