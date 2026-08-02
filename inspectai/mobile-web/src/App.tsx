import { Navigate, Route, Routes } from "react-router-dom";

import AppTabs from "@/components/AppTabs";
import PageTransition from "@/components/PageTransition";
import { useAuth } from "@/store/auth";
import ApprovalsPage from "@/pages/ApprovalsPage";
import AssetDetailPage from "@/pages/AssetDetailPage";
import CapturePage from "@/pages/CapturePage";
import ClassifyPage from "@/pages/ClassifyPage";
import LedgerPage from "@/pages/LedgerPage";
import LoginPage from "@/pages/LoginPage";
import MePage from "@/pages/MePage";
import PreviewPage from "@/pages/PreviewPage";
import RecordPage from "@/pages/RecordPage";
import TasksPage from "@/pages/TasksPage";
import ReviewPage from "@/pages/ReviewPage";

// 巡检主流程:拍照 → (联网自动上传) → 选照片识别 → 填日报 → 提交
export default function App() {
  const loggedIn = useAuth((s) => s.loggedIn);
  const guard = (el: JSX.Element) => (loggedIn ? el : <Navigate to="/login" replace />);

  return (
    <div className="app-shell">
      {/* 转场包在 Routes 外层:它按当前路径判断层级(平级淡入 / 下钻推入 / 返回退出)。
          底栏在外面,不参与转场 —— 它是常驻的,跟着页面一起淡会显得整个 app 在闪。 */}
      <PageTransition>
      <Routes>
        <Route path="/login" element={loggedIn ? <Navigate to="/" replace /> : <LoginPage />} />
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
