import { Navigate, Route, Routes } from "react-router-dom";

import { useAuth } from "@/store/auth";
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
      <Routes>
        <Route path="/login" element={loggedIn ? <Navigate to="/" replace /> : <LoginPage />} />
        <Route path="/" element={guard(<CapturePage />)} />
        <Route path="/review" element={guard(<ReviewPage />)} />
        <Route path="/classify" element={guard(<ClassifyPage />)} />
        <Route path="/record/:id" element={guard(<RecordPage />)} />
        <Route path="/preview/:id" element={guard(<PreviewPage />)} />
        <Route path="/tasks" element={guard(<TasksPage />)} />
        <Route path="/ledger" element={guard(<LedgerPage />)} />
        <Route path="/asset/:id" element={guard(<AssetDetailPage />)} />
        <Route path="/me" element={guard(<MePage />)} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </div>
  );
}
