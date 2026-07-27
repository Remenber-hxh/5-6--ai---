import { Navigate, Route, Routes } from "react-router-dom";

import { useAuth } from "@/store/auth";
import CapturePage from "@/pages/CapturePage";
import ClassifyPage from "@/pages/ClassifyPage";
import LoginPage from "@/pages/LoginPage";
import MePage from "@/pages/MePage";
import RecordPage from "@/pages/RecordPage";
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
        <Route path="/me" element={guard(<MePage />)} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </div>
  );
}
