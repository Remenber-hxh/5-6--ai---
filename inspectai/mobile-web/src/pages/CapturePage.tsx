import { Avatar, Badge, Loading, NoticeBar, PullRefresh, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import BeianLine from "@/components/BeianLine";
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

  // 次要入口(旧版 .hero-secondary 的文字链接排)。
  // 「从相册上传」是 label 包 file input,不能做成 button —— 单列出来。
  const linkBtn = (label: string, count: number, to: string) => (
    <button className="link-btn" onClick={() => nav(to)}>
      {label}
      {/* Badge 是红点本体,靠 .link-btn 的 position:relative 定位 */}
      <Badge count={count} className="lk-badge" />
    </button>
  );
  const links = [
    { key: "tasks", node: linkBtn("我的任务", openTasks, "/tasks") },
    {
      key: "album",
      node: (
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
      ),
    },
    { key: "shots", node: linkBtn("待处理照片", pendingShots, "/review") },
    { key: "ledger", node: linkBtn("设备健康", 0, "/ledger") },
    // 审批只对管理角色开
    ...(canReview
      ? [{ key: "approvals", node: linkBtn("待审批", pendingApprovals, "/approvals") }]
      : []),
  ];

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
      {/* 顶栏照旧版:左边只有产品名,右边一个带在线圆点的身份钮。
          品牌徽章不在这里 —— 它居中在下面的大标题上方。 */}
      <div className="capture-top">
        <span className="capture-wordmark">智巡</span>
        {/* 在线状态收进身份钮的小圆点,不再单独一个 pill 抢位置;
            真离线时下方会出现整条 NoticeBar,不靠这个小点表达。 */}
        <button
          className={online ? "user-window is-online" : "user-window is-offline"}
          onClick={() => nav("/me")}
        >
          {/* 文字头像:顶栏右端原本只有一段文字会显得飘,给它一个"这是个人"的锚点 */}
          <Avatar name={user?.displayName || user?.username || "巡"} />
          <span className="uw-name">{user?.displayName || user?.username || "巡检员"}</span>
          <span className="uw-dot" />
        </button>
      </div>

      {/* 当前任务:从「我的任务」进来后常驻,提交时带上任务自动销账。
          任务名可能很长,交给 NoticeBar 的 marquee 处理,不再自己截断。 */}
      {activeTask && (
        <NoticeBar
          leftContent={<span className="tb-dot" />}
          rightContent={
            <button
              className="tb-exit"
              onClick={() => {
                clearActiveTask();
                nav(0);
              }}
            >
              退出
            </button>
          }
        >
          正在执行:{activeTask.title || activeTask.workContent || "巡检任务"}
        </NoticeBar>
      )}

      {/* 离线是这个产品的核心场景,不该只靠右上角一个小 pill 表达。
          在线时这里不占位;离线时才出现,并说清影响(照片不会丢)。
          原先底部那行常驻小字就是它的前身 —— 常驻但在线时纯属废话。 */}
      {!online && (
        <NoticeBar tone="warn" leftContent={<span className="nb-ic">离线</span>}>
          没有网络也能拍。照片先存这台手机,联网后自动上传并补 AI 识别。
        </NoticeBar>
      )}

      {/* 下拉刷新待办计数。原先只在进页面时拉一次,想看最新数得退出去再进来 ——
          手机上下拉刷新是肌肉记忆,缺了它这屏是"死"的。
          结构注意:.arco-pull-refresh 自己是滚动容器(height:100%; overflow-y:auto),
          必须由外层 .capture-scroll 给出确定高度;内容的 flex 排版留在 .capture-main。
          曾经图省事把 capture-main 的类直接扣在 PullRefresh 上 —— 它带
          display:flex + align-items:center,把组件内部三层结构压成了 228px 宽。 */}
      <div className="capture-scroll">
        <PullRefresh onRefresh={loadBadges}>
          <div className="capture-main">
        <div className="cap-hero">
          {/* 品牌徽章居中在大标题上方 —— 旧版的层次:徽章 → 标题 → 副标题 →
              取景框 → 快门 → 次要链接。之前把徽章挤到左上角,层次就散了。 */}
          <span className="brand-badge">
            <span className="brand-word">JADEAST</span>
            <span className="brand-divider" />
            <span className="brand-cn">智巡</span>
          </span>
          <h1 className="capture-title">对准巡检设备拍照</h1>
          {/* 副标题恒定讲正常流程。离线这个例外由上方的 NoticeBar 承担 ——
              两处都改文案会说同一件事两遍。 */}
          <p className="capture-sub">AI 会自动识别场景,调出对应的日报模板</p>
        </div>

        {/* 取景框是【画面区】,不是按钮 —— 里面有网格和虚线对准圈,提示"把设备放中间"。
            曾经把快门塞进框里当成一个大按钮,两个概念糊在一起,观感和语义都塌了。 */}
        <div className="viewfinder" aria-hidden>
          <span className="vf-corner vf-tl" />
          <span className="vf-corner vf-tr" />
          <span className="vf-corner vf-bl" />
          <span className="vf-corner vf-br" />
          <span className="vf-scan" />
        </div>

        {/* 快门:框下方独立大圆钮,带发光外环。80px 触控目标,戴手套也够。
            保存中在钮心放一个弧形 loading,而不是只把按钮调暗 ——
            调暗只说明"不能点",不说明"正在干活"。 */}
        <label className={saving ? "shutter is-busy" : "shutter"}>
          <input
            className="vf-input"
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={onPick}
            aria-label="拍照"
          />
          {saving && (
            <span className="shutter-loading">
              <Loading size={26} color="#0a8a63" />
            </span>
          )}
        </label>
        <span className="shutter-label">{saving ? "保存中…" : "拍照识别"}</span>

        {/* 次要入口:旧版是一排轻量文字链接,不和主操作抢视觉重量。
            之前做成四个实心磁贴 —— 查询类的「设备健康」被抬到和拍照同级,权重给错了。
            计数仍用 Badge,只是缩成链接右上角的小红点。 */}
        {/* 旧版用「·」做分隔符,但它只有 3 个入口从不换行。这里最多 5 个,
            换行后第二行会以一个孤立的「·」开头 —— 改用 gap 排,不再插分隔符。 */}
        <div className="hero-secondary">
          {links.map((l) => (
            <span className="hs-item" key={l.key}>
              {l.node}
            </span>
          ))}
        </div>

            {/* 登录后根域名首页 = 这一屏,备案号必须在这里也可见(旧版同样两处都放) */}
            <BeianLine />
          </div>
        </PullRefresh>
      </div>

      <PendingPanel />
    </div>
  );
}
