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
import Profile from "./pages/Profile";
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
        components: {
          // 侧边导航:品牌深海军蓝,选中品牌绿,悬停青绿微光(与 Agent 页深色同族)
          Layout: { siderBg: "#0b1626", triggerBg: "#0b1626" },
          Menu: {
            darkItemBg: "transparent",
            darkSubMenuItemBg: "transparent",
            darkItemColor: "rgba(214, 228, 240, 0.72)",
            darkItemHoverBg: "rgba(62, 230, 180, 0.08)",
            darkItemHoverColor: "#eef6f4",
            darkItemSelectedBg: "#12a968",
            darkItemSelectedColor: "#04241b",
            itemBorderRadius: 8,
            itemMarginInline: 10,
          },
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
            <Route path="/profile" element={<Profile />} />
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
