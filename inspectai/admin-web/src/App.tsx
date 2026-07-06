import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";

import MainLayout from "./layouts/MainLayout";
import AgentHome from "./pages/AgentHome";
import Approvals from "./pages/Approvals";
import DataBoard from "./pages/DataBoard";
import Ledger from "./pages/Ledger";
import Login from "./pages/Login";
import Logs from "./pages/Logs";
import Plan from "./pages/Plan";
import Prompts from "./pages/Prompts";
import Records from "./pages/Records";
import System from "./pages/System";
import Users from "./pages/Users";

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
            <Route path="/plan" element={<Plan />} />
            <Route path="/ledger" element={<Ledger />} />
            <Route path="/record" element={<Records />} />
            <Route path="/approval" element={<Approvals />} />
            <Route path="/data" element={<DataBoard />} />
            <Route path="/users" element={<Users />} />
            <Route path="/logs" element={<Logs />} />
            <Route path="/system" element={<System />} />
            <Route path="/prompts" element={<Prompts />} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </HashRouter>
    </ConfigProvider>
  );
}
