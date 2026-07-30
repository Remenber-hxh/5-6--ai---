import { Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import PendingPanel from "@/components/PendingPanel";
import { useAuth } from "@/store/auth";
import { usePending } from "@/store/pending";
import { clearActiveTask, getActiveTask } from "@/store/activeTask";
import { listEngineeringTasks, listOfflineShots, listPendingChangeRequests } from "@/api/inspection";

// 工作台磁贴图标:细线风(1.7px 描边,currentColor),与深色玻璃卡同语系。
// 不引图标库 —— 5 个图标手写 SVG 比拉一个依赖轻得多。
const IC = {
  width: 22,
  height: 22,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.7,
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

function IconTasks() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="5.5" y="5" width="13" height="15.5" rx="2.2" />
      <path d="M9 5.2V3.9a.9.9 0 0 1 .9-.9h4.2a.9.9 0 0 1 .9.9v1.3" />
      <path d="m9.2 13.6 1.9 1.9 3.6-3.8" />
    </svg>
  );
}
function IconLedger() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="3" />
      <path d="M7.5 13h2.3l1.5-4 2.3 6.5 1.5-3.7h1.5" />
    </svg>
  );
}
function IconApproval() {
  return (
    <svg {...IC} aria-hidden>
      <circle cx="12" cy="9.5" r="5.2" />
      <path d="m9.9 9.5 1.5 1.5 3-3.1" />
      <path d="m9.2 14.6-1.4 4.4 4.2-2 4.2 2-1.4-4.4" />
    </svg>
  );
}
function IconPhotos() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="7.5" y="4" width="12.5" height="12.5" rx="2.2" />
      <path d="M4 9.5v8A2.5 2.5 0 0 0 6.5 20H16" />
    </svg>
  );
}
function IconAlbum() {
  return (
    <svg {...IC} width={18} height={18} aria-hidden>
      <rect x="4" y="5" width="16" height="14" rx="2.2" />
      <circle cx="9" cy="10" r="1.4" />
      <path d="m6.5 16.5 4-4 3 3 3.2-3.2 2.8 2.7" />
    </svg>
  );
}

// 拍照台:拍多张 → 存进离线仓库 → 联网后自动上传补 AI 识别。
// 弱网现场只管拍,照片与拍摄时间先落地,不被信号拖住巡检节奏。
export default function CapturePage() {
  const nav = useNavigate();
  const user = useAuth((s) => s.user);
  const { online, saving, init, addFiles, uploadedTick } = usePending();
  const activeTask = getActiveTask();
  // 待办角标:让巡检员一眼看到"还有事没做完",不用点进去才知道
  const [pendingShots, setPendingShots] = useState(0);
  const [openTasks, setOpenTasks] = useState(0);
  const [pendingApprovals, setPendingApprovals] = useState(0);
  // 审批是管理角色的活,巡检员不显示这个入口
  const canReview = ["admin", "manager", "supervisor"].includes(user?.roleCode || "");

  const loadBadges = useCallback(async () => {
    // 角标是辅助信息,拉取失败静默降级为不显示,不打扰主流程
    const [shots, tasks, crs] = await Promise.all([
      listOfflineShots().catch(() => []),
      listEngineeringTasks().catch(() => []),
      canReview ? listPendingChangeRequests().catch(() => []) : Promise.resolve([]),
    ]);
    setPendingShots(shots.length);
    setOpenTasks(tasks.filter((t) => t.status !== "已完成").length);
    setPendingApprovals(crs.length);
  }, [canReview]);

  // uploadedTick 变化 = 刚有照片传上服务器,红点要立刻跟上,
  // 否则用户拍完照看红点没动,会以为没生效。
  useEffect(() => {
    void loadBadges();
    // 从其他页返回或切回前台时刷新,否则处理完照片红点还挂着
    const onVisible = () => {
      if (document.visibilityState === "visible") void loadBadges();
    };
    document.addEventListener("visibilitychange", onVisible);
    window.addEventListener("focus", onVisible);
    return () => {
      document.removeEventListener("visibilitychange", onVisible);
      window.removeEventListener("focus", onVisible);
    };
  }, [loadBadges, uploadedTick]);

  useEffect(() => {
    void init();
  }, [init]);

  async function onPick(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files || []);
    e.target.value = ""; // 允许同一文件重选
    if (!files.length) return;
    const { added, error } = await addFiles(files, user?.id || "");
    if (error) {
      Toast.show({ content: error, duration: 3000 });
    } else {
      Toast.show({ content: `已保存 ${added} 张`, position: "bottom" });
    }
  }

  return (
    <div className="capture-screen">
      <div className="capture-top">
        <span className="brand-badge">
          <span className="brand-word">JADEAST</span>
          <span className="brand-divider" />
          <span className="brand-cn">智巡</span>
        </span>
        <span className="top-right">
          <span className={online ? "net-pill net-on" : "net-pill net-off"}>
            {online ? "在线" : "离线暂存"}
          </span>
          {/* 用户入口:点进去可看身份、退出登录(旧版右上角 user-window) */}
          <button className="user-window" onClick={() => nav("/me")}>
            {user?.displayName || user?.username || "巡检员"}
          </button>
        </span>
      </div>

      {/* 当前任务横幅:从「我的任务」进来后常驻,提交时带上任务自动销账 */}
      {activeTask && (
        <div className="task-banner">
          <span className="tb-dot" />
          <span className="tb-text">
            正在执行:{activeTask.title || activeTask.workContent || "巡检任务"}
          </span>
          <button
            className="tb-exit"
            onClick={() => {
              clearActiveTask();
              nav(0);
            }}
          >
            退出
          </button>
        </div>
      )}

      <div className="capture-main">
        <div className="cap-hero">
          {/* 身份只留这一处:部门 · 角色(原先顶栏人名 + 按钮下人名部门重复) */}
          <p className="cap-eyebrow">
            {user?.departmentName || "默认部门"} · {user?.roleName || "巡检员"}
          </p>
          <h1 className="capture-title">对准巡检设备拍照</h1>
          <p className="capture-sub">
            {online ? "AI 会自动识别场景,调出对应的日报模板" : "没有网络也能拍,联网后自动上传识别"}
          </p>
        </div>

        {/* 整个取景框就是快门:弱网/手套/颠簸现场,不用瞄准一个小圆钮 */}
        <label className="vf-btn">
          <input
            className="vf-input"
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={onPick}
            aria-label="拍照"
          />
          <span className="vf-corner vf-tl" />
          <span className="vf-corner vf-tr" />
          <span className="vf-corner vf-bl" />
          <span className="vf-corner vf-br" />
          <span className="vf-scan" />
          <span className={saving ? "vf-ring busy" : "vf-ring"} />
          <span className="vf-text">{saving ? "保存中…" : "拍照"}</span>
        </label>

        <label className="cap-album upload-wrap">
          <IconAlbum />
          从相册上传
          <input
            className="upload-input"
            type="file"
            accept="image/*"
            multiple
            onChange={onPick}
            aria-label="从相册上传"
          />
        </label>

        {/* 工作台磁贴:取代旧版"文字链接+圆点"导航,角标即待办数 */}
        <p className="cap-nav-label">工作台</p>
        <div className="cap-nav">
          <button className="nav-tile" onClick={() => nav("/tasks")}>
            <IconTasks />
            <span className="nt-label">我的任务</span>
            {openTasks > 0 && <span className="badge-dot">{openTasks > 99 ? "99+" : openTasks}</span>}
          </button>
          <button className="nav-tile" onClick={() => nav("/ledger")}>
            <IconLedger />
            <span className="nt-label">设备健康</span>
          </button>
          {canReview && (
            <button className="nav-tile" onClick={() => nav("/approvals")}>
              <IconApproval />
              <span className="nt-label">待审批</span>
              {pendingApprovals > 0 && (
                <span className="badge-dot">{pendingApprovals > 99 ? "99+" : pendingApprovals}</span>
              )}
            </button>
          )}
          <button className="nav-tile" onClick={() => nav("/review")}>
            <IconPhotos />
            <span className="nt-label">待处理照片</span>
            {pendingShots > 0 && (
              <span className="badge-dot">{pendingShots > 99 ? "99+" : pendingShots}</span>
            )}
          </button>
        </div>
      </div>

      <PendingPanel />
    </div>
  );
}
