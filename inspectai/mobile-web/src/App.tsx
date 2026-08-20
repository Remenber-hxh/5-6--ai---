import { useEffect } from "react";
import { Navigate, Route, Routes, useLocation } from "react-router-dom";

import AppTabs from "@/components/AppTabs";
import PageTransition from "@/components/PageTransition";
import { useAuth } from "@/store/auth";
import ApprovalsPage from "@/pages/ApprovalsPage";
import AssetDetailPage from "@/pages/AssetDetailPage";
import CapturePage from "@/pages/CapturePage";
import ClassifyPage from "@/pages/ClassifyPage";
import LedgerPage from "@/pages/LedgerPage";
import ChangePasswordPage from "@/pages/ChangePasswordPage";
import LoginPage from "@/pages/LoginPage";
import RegisterPage from "@/pages/RegisterPage";
import MePage from "@/pages/MePage";
import PreviewPage from "@/pages/PreviewPage";
import RecordPage from "@/pages/RecordPage";
import TasksPage from "@/pages/TasksPage";
import ReviewPage from "@/pages/ReviewPage";

// 巡检主流程:拍照 → (联网自动上传) → 选照片识别 → 填日报 → 提交
export default function App() {
  const loggedIn = useAuth((s) => s.loggedIn);
  const revalidate = useAuth((s) => s.revalidate);
  const notice = useAuth((s) => s.user?.dataScopeNotice);

  // 开机问一次后端"这个 token 还认不认"。
  //
  // 【为什么必须问】登录态只看本地存没存过 token,失效了本地照样认:
  // 进来是完整的 App 外壳,然后每个页面各自弹"加载失败" —— 巡检员的感受是
  // "登着录但什么都打不开",而不是"该重新登录了"。真失效时这里会经由
  // client 的 401 出口直接清干净并回登录页。
  // 离线时【不】判失效,见 store 里的注释。
  useEffect(() => {
    void revalidate();
  }, [revalidate]);

  const guard = (el: JSX.Element) =>
    loggedIn ? el : <Navigate to="/login" replace />;
  const location = useLocation();

  return (
    <div className="app-shell">
      {/* 【数据范围提示】同 admin-web:空页面是最糟的失败方式。
          放在 PageTransition 外面 —— 它是账号级状态,不该跟着翻页闪。 */}
      {loggedIn && notice && <div className="scope-notice">{notice}</div>}
      {/* 转场包在 Routes 外层:它按当前路径判断层级(平级淡入 / 下钻推入 / 返回退出)。
          底栏在外面,不参与转场 —— 它是常驻的,跟着页面一起淡会显得整个 app 在闪。 */}
      <PageTransition>
        {/* 【location 必须显式传】不传的话 Routes 从 context 取当前路径,
          于是【正在退出的那一层】也会跟着渲染成新页面 —— 结果是:
            · 新页面挂载两次(相隔约 130ms,正好是退出动画时长),
              每个页面的数据请求都发两遍
            · 转场看着也不对:旧页面根本没参与,是新页面自己滑出去再滑进来
          显式传 location 后,退出中的那一层保持上一次渲染时的路径,
          新页面只挂载一次。
          2026-08-03:填报页"识别完了却不填表"就是这么来的 —— 第一次挂载
          发起识别,130ms 后被卸载,轮询随之中断;第二次挂载看到状态已是
          processing,直接跳过(见 RecordPage 的注释)。 */}
        <Routes location={location}>
          <Route
            path="/login"
            element={loggedIn ? <Navigate to="/" replace /> : <LoginPage />}
          />
          {/* 注册和登录一样:已登录就别再进来了 */}
          <Route
            path="/register"
            element={loggedIn ? <Navigate to="/" replace /> : <RegisterPage />}
          />
          <Route path="/" element={guard(<CapturePage />)} />
          <Route path="/review" element={guard(<ReviewPage />)} />
          <Route path="/classify" element={guard(<ClassifyPage />)} />
          <Route path="/record/:id" element={guard(<RecordPage />)} />
          <Route path="/preview/:id" element={guard(<PreviewPage />)} />
          <Route path="/tasks" element={guard(<TasksPage />)} />
          <Route path="/ledger" element={guard(<LedgerPage />)} />
          <Route path="/approvals" element={guard(<ApprovalsPage />)} />
          <Route path="/asset/:id" element={guard(<AssetDetailPage />)} />
          <Route path="/me" element={guard(<MePage />)} />
          <Route path="/me/password" element={guard(<ChangePasswordPage />)} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </PageTransition>

      {/* 底部常驻导航。放在 Routes 之外、作为 .app-shell 的最后一个 flex 子元素,
          天然贴底 —— 不用 position:fixed,也就不用为它另算占位高度。
          它自己决定在哪些路由显示(只在 5 个顶层目的地)。 */}
      {loggedIn && <AppTabs />}
    </div>
  );
}
