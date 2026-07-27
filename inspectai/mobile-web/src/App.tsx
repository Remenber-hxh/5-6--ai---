import { Navigate, Route, Routes } from "react-router-dom";

import { useAuth } from "@/store/auth";
import CapturePage from "@/pages/CapturePage";
import LoginPage from "@/pages/LoginPage";
import MePage from "@/pages/MePage";

// Phase 1 应用外壳:登录 + 按角色落地 + 几个占位页。
// 拍照托盘 / 离线队列 / 水印在后续步骤填入 CapturePage。
export default function App() {
  const loggedIn = useAuth((s) => s.loggedIn);

  return (
    <div className="app-shell">
      <Routes>
        <Route path="/login" element={loggedIn ? <Navigate to="/" replace /> : <LoginPage />} />
        <Route path="/" element={loggedIn ? <CapturePage /> : <Navigate to="/login" replace />} />
        <Route path="/me" element={loggedIn ? <MePage /> : <Navigate to="/login" replace />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Routes>
    </div>
  );
}
