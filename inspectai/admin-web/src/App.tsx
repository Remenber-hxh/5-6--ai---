import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";

import MainLayout from "./layouts/MainLayout";
import AgentHome from "./pages/AgentHome";
import Approvals from "./pages/Approvals";
import Ledger from "./pages/Ledger";
import Login from "./pages/Login";
import Records from "./pages/Records";

// 范围铁律:新版只做四个核心页(Agent 首页/台账/记录/审批),其余用旧版 admin-frontend
export default function App() {
  return (
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: "#12a968",
          borderRadius: 8,
        },
      }}
    >
      <HashRouter>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route element={<MainLayout />}>
            <Route path="/" element={<AgentHome />} />
            <Route path="/ledger" element={<Ledger />} />
            <Route path="/record" element={<Records />} />
            <Route path="/approval" element={<Approvals />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </HashRouter>
    </ConfigProvider>
  );
}
