import { Avatar, Loading, NoticeBar, PullRefresh, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import BeianLine from "@/components/BeianLine";
import { IconAlbum } from "@/components/icons";
import PendingPanel from "@/components/PendingPanel";
import { useLongPress } from "@/lib/useLongPress";
import { useAuth } from "@/store/auth";
import { clearRetakeTarget, getRetakeTarget } from "@/store/retake";
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
  // 待办计数归底部 TabBar 管(它跨页常驻,数据也该在那一层拉)。
  // 这里只留下拉刷新的动作 —— 首页下拉时顺带把服务器状态刷一次。
  const refresh = useCallback(async () => {
    await listOfflineShots().catch(() => []);
  }, []);

  useEffect(() => {
    void init();
  }, [init]);

  // 相册入口:长按快门从右侧滑出。不做成常驻按钮 —— 首页只留"拍照"一件事。
  // 复检上下文。用 state 兜一层而不是每次渲染读 localStorage ——
  // 点"取消复检"后横幅要立刻消失,直接读存储不会触发重渲染。
  const [retake, setRetake] = useState(getRetakeTarget);
  const [albumOpen, setAlbumOpen] = useState(false);
  // 光波是一次性动画。用递增的 key 而不是布尔值:布尔值在动画没播完时
  // 再次长按无法重放(类名没变化,浏览器不会重启动画),key 变了才会重挂。
  const [ripple, setRipple] = useState(0);
  const longPress = useLongPress(() => {
    setAlbumOpen(true);
    setRipple((n) => n + 1);
  });
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
      {/* 顶栏只剩产品名 + 头像。原来那圈胶囊描边和在线绿点都去掉了 ——
          在线状态离线时由下方整条 NoticeBar 表达,不需要常驻一个小点占着。
          品牌徽章也删了:顶栏已经有「智巡」,中间再放一次是重复。 */}
      <div className="capture-top">
        <span className="capture-wordmark">智巡</span>
        {/* 扫码放顶栏右侧、头像左边。
            【不放快门旁边】首页的中心动作只有"拍照"一件事,快门周围加东西
            会让人误触;而扫码是"开始之前"的动作,和身份钮同属工具区。 */}
        <button
          className="scan-entry"
          onClick={() => nav("/scan")}
          aria-label="扫码识别设备"
        >
          <svg viewBox="0 0 24 24" width="20" height="20" aria-hidden="true">
            <g fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round">
              <path d="M4 8V5.5A1.5 1.5 0 0 1 5.5 4H8" />
              <path d="M16 4h2.5A1.5 1.5 0 0 1 20 5.5V8" />
              <path d="M20 16v2.5a1.5 1.5 0 0 1-1.5 1.5H16" />
              <path d="M8 20H5.5A1.5 1.5 0 0 1 4 18.5V16" />
              <path d="M4 12h16" />
            </g>
          </svg>
        </button>
        <button
          className="user-window"
          onClick={() => nav("/me")}
          aria-label="我的"
        >
          <Avatar
            name={user?.displayName || user?.username || "巡"}
            src={user?.avatar}
          />
        </button>
      </div>

      {/* 锁定设备的横幅。没有它的话,巡检员根本不知道自己正处在"锁定某台设备"的
          状态里 —— 而这个状态会强制模板和设备编号,拍出来的记录全挂到那台上。
          必须能一眼看见、一键取消。

          【两种来源要分开说】复检是"这台有问题,去复查销账";扫码只是
          "已经确认是哪台了",拍出来算一次正常巡检。文案说错会让巡检员
          以为自己在做复查。 */}
      {retake && (
        <NoticeBar
          leftContent={
            <span className="nb-ic is-recheck">
              {retake.mode === "scan" ? "扫码" : "复检"}
            </span>
          }
          rightContent={
            <button
              className="tb-exit"
              onClick={() => {
                clearRetakeTarget();
                setRetake(null);
                Toast.show({
                  // 扫码只是"锁定了哪台设备",取消它不该说成"取消复检"
                  content: retake.mode === "scan" ? "已取消锁定" : "已取消复检",
                });
              }}
            >
              取消
            </button>
          }
        >
          {retake.mode === "scan"
            ? `已锁定:${retake.assetName}(直接拍照即可,记录会挂到这台上)`
            : `复检中:${retake.assetName}(编号已带入,重拍后自动更新这台设备)`}
        </NoticeBar>
      )}


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
        <NoticeBar
          tone="warn"
          leftContent={<span className="nb-ic">离线</span>}
        >
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
            {/* 取景框删干净了。点快门打开的是【系统相机】,对准动作发生在那里 ——
            这个框从来没框住过任何东西,却一直有扫描线在动,暗示"正在识别"。
            没有实景的情况下它就是个假框子。

            这里放一张静图(电梯井道线稿,public/home-hero.svg)。曾经试过「今日概览」
            三个数字 —— 和快门抢视觉
            重量、也不好看,已撤掉。图片缺失时自己隐藏,不留碎图占位。 */}
            <img
              className="home-hero"
              src="home-hero.svg"
              alt=""
              onError={(e) => {
                e.currentTarget.style.display = "none";
              }}
            />
          </div>
        </PullRefresh>
      </div>

      {/* 快门栏:钉在屏幕底部(滚动区之外),拇指自然落点。
        相机 app 的快门都在底部 —— 单手举着手机时中间那个位置够不着。
        保存中在钮心放弧形 loading,而不是只把按钮调暗:
        调暗只说明"不能点",不说明"正在干活"。
        长按是隐藏手势,下方文字必须写出来,否则相册入口等于消失。 */}
      <div className="shutter-bar">
        <div className="shutter-dock">
          {/* 光波是独立元素并带 key —— key 变化让它重新挂载,动画才会重放。
            不能把 key 放在 .shutter-dock 上:那会把快门和相册一起重挂,
            相册的展开过渡直接失效、file input 也会被重建。 */}
          {ripple > 0 && (
            <span className="shutter-ripple" key={ripple} aria-hidden />
          )}
          <label
            className={
              (saving ? "shutter is-busy" : "shutter") +
              (holding ? " is-holding" : "")
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
            className={
              albumOpen
                ? "album-fab upload-wrap is-open"
                : "album-fab upload-wrap"
            }
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
          {saving ? "保存中…" : albumOpen ? "点右侧图标选相册" : "拍照识别"}
        </span>
        {/* 登录后根域名首页 = 这一屏,备案号必须在这里也可见(工信部要求)。
          放在快门栏里而不是滚动区:滚动区现在只到 616px,备案号会飘在半空。 */}
        <BeianLine />
      </div>

      <PendingPanel />
    </div>
  );
}
