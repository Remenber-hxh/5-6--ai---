import { Toast } from "antd-mobile";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import PendingPanel from "@/components/PendingPanel";
import { useAuth } from "@/store/auth";
import { usePending } from "@/store/pending";
import { clearActiveTask, getActiveTask } from "@/store/activeTask";
import { listEngineeringTasks, listOfflineShots, listPendingChangeRequests } from "@/api/inspection";

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
        <h1 className="capture-title">对准巡检设备拍照</h1>
        <p className="capture-sub">
          {online ? "AI 会自动识别场景,调出对应的日报模板" : "没有网络也能拍,联网后自动上传识别"}
        </p>

        <div className="viewfinder" aria-hidden="true">
          <span className="vf-corner vf-tl" />
          <span className="vf-corner vf-tr" />
          <span className="vf-corner vf-bl" />
          <span className="vf-corner vf-br" />
          <span className="vf-scan" />
        </div>

        <label className="shutter-wrap">
          <input
            className="shutter-input"
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={onPick}
            aria-label="拍照"
          />
          <span className={saving ? "shutter shutter-busy" : "shutter"} />
        </label>
        <div className="shutter-label">{saving ? "保存中…" : "拍照"}</div>

        <div className="capture-who">
          {user?.displayName || user?.username} · {user?.departmentName || "默认部门"}
        </div>

        {/* 旧版 hero-secondary:我的任务 · 从相册上传 · 手动选择模板 */}
        <div className="hero-secondary">
          <button className="link-btn" onClick={() => nav("/tasks")}>
            我的任务
            {openTasks > 0 && <span className="badge-dot">{openTasks > 99 ? "99+" : openTasks}</span>}
          </button>
          <span className="link-sep">·</span>
          <label className="link-btn upload-wrap">
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
          <span className="link-sep">·</span>
          <button className="link-btn" onClick={() => nav("/ledger")}>
            设备健康
          </button>
          {canReview && (
            <>
              <span className="link-sep">·</span>
              <button className="link-btn" onClick={() => nav("/approvals")}>
                待审批
                {pendingApprovals > 0 && (
                  <span className="badge-dot">{pendingApprovals > 99 ? "99+" : pendingApprovals}</span>
                )}
              </button>
            </>
          )}
          <span className="link-sep">·</span>
          <button className="link-btn" onClick={() => nav("/review")}>
            待处理照片
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
