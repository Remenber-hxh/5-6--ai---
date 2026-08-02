import { Avatar, Loading, NoticeBar, PullRefresh, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import BeianLine from "@/components/BeianLine";
import { IconAlbum } from "@/components/icons";
import PendingPanel from "@/components/PendingPanel";
import { useLongPress } from "@/lib/useLongPress";
import { useAuth } from "@/store/auth";
import { usePending } from "@/store/pending";
import { clearActiveTask, getActiveTask } from "@/store/activeTask";
import { listOfflineShots } from "@/api/inspection";

// 拍照台:拍多张 → 存进离线仓库 → 联网后自动上传补 AI 识别。
// 弱网现场只管拍,照片与拍摄时间先落地,不被信号拖住巡检节奏。
export default function CapturePage() {
  const nav = useNavigate();
  const user = useAuth((s) => s.user);
  const { online, saving, init, addFiles } = usePending();
  const activeTask = getActiveTask();
  // 待办角标现在归底部 TabBar 管(它跨页常驻,数据也该在那一层拉)。
  // 这里只留下拉刷新要用的动作 —— 首页下拉时顺带把服务器状态刷一次。
  const refresh = useCallback(async () => {
    await listOfflineShots().catch(() => []);
  }, []);

  useEffect(() => {
    void init();
  }, [init]);

  // 相册入口:长按快门从右侧滑出。不做成常驻按钮 —— 首页只留"拍照"一件事。
  const [albumOpen, setAlbumOpen] = useState(false);
  const longPress = useLongPress(() => setAlbumOpen(true));
  const holding = longPress.holding;

  // 弹出后 5 秒自动收起:一直挂着会挡视线,用户也会以为它坏了。
  // 5 秒够看清并点到 —— 再短来不及,再长就等于常驻按钮了。
  useEffect(() => {
    if (!albumOpen) return;
    const t = window.setTimeout(() => setAlbumOpen(false), 5000);
    return () => window.clearTimeout(t);
  }, [albumOpen]);

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
          <Avatar name={user?.displayName || user?.username || "巡"} src={user?.avatar} />
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
        <PullRefresh onRefresh={refresh}>
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
        {/* 快门 + 相册。长按快门,相册图标从它右侧滑出。
            相册入口不再是独立按钮 —— 首页只留"拍照"一件事,C 位更纯粹。
            但长按是隐藏手势,不知道的人永远找不到,所以下方文字明写出来。 */}
        <div className="shutter-dock">
          <label
            className={
              (saving ? "shutter is-busy" : "shutter") + (holding ? " is-holding" : "")
            }
            {...longPress.handlers}
          >
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

          {/* 相册:长按后滑出。是 label 包 input —— 用户点它是一次全新的
              用户手势,file picker 一定能打开;不依赖长按那一次手势的延续。 */}
          <label
            className={albumOpen ? "album-fab upload-wrap is-open" : "album-fab upload-wrap"}
            aria-hidden={!albumOpen}
          >
            <IconAlbum />
            <input
              className="upload-input"
              type="file"
              accept="image/*"
              multiple
              onChange={(e) => {
                setAlbumOpen(false);
                void onPick(e);
              }}
              aria-label="从相册上传"
              tabIndex={albumOpen ? 0 : -1}
            />
          </label>
        </div>
        <span className="shutter-label">
          {saving ? "保存中…" : albumOpen ? "点右侧图标选相册" : "拍照识别 · 长按选相册"}
        </span>

        {/* 登录后根域名首页 = 这一屏,备案号必须在这里也可见(旧版同样两处都放) */}
            <BeianLine />
          </div>
        </PullRefresh>
      </div>

      <PendingPanel />
    </div>
  );
}
