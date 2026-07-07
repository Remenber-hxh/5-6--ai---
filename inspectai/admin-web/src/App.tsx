import { ConfigProvider, Spin } from "antd";
import zhCN from "antd/locale/zh_CN";
import { Suspense, lazy } from "react";
import { HashRouter, Navigate, Route, Routes } from "react-router-dom";

import MainLayout from "./layouts/MainLayout";
import Login from "./pages/Login";

// 路由级按需加载:重页面(看板 ECharts / Agent Live2D)不进首包
const AgentHome = lazy(() => import("./pages/AgentHome"));
const Approvals = lazy(() => import("./pages/Approvals"));
const DataBoard = lazy(() => import("./pages/DataBoard"));
const Ledger = lazy(() => import("./pages/Ledger"));
const Logs = lazy(() => import("./pages/Logs"));
const Plan = lazy(() => import("./pages/Plan"));
const Profile = lazy(() => import("./pages/Profile"));
const Prompts = lazy(() => import("./pages/Prompts"));
const Records = lazy(() => import("./pages/Records"));
const System = lazy(() => import("./pages/System"));
const Users = lazy(() => import("./pages/Users"));

const fallback = (
  <div style={{ display: "grid", placeItems: "center", minHeight: "40vh" }}>
    <Spin />
  </div>
);

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
            <Route
              path="/"
              element={<Suspense fallback={fallback}><AgentHome /></Suspense>}
            />
            <Route path="/plan" element={<Suspense fallback={fallback}><Plan /></Suspense>} />
            <Route path="/ledger" element={<Suspense fallback={fallback}><Ledger /></Suspense>} />
            <Route path="/record" element={<Suspense fallback={fallback}><Records /></Suspense>} />
            <Route path="/approval" element={<Suspense fallback={fallback}><Approvals /></Suspense>} />
            <Route path="/data" element={<Suspense fallback={fallback}><DataBoard /></Suspense>} />
            <Route path="/profile" element={<Suspense fallback={fallback}><Profile /></Suspense>} />
            <Route path="/users" element={<Suspense fallback={fallback}><Users /></Suspense>} />
            <Route path="/logs" element={<Suspense fallback={fallback}><Logs /></Suspense>} />
            <Route path="/system" element={<Suspense fallback={fallback}><System /></Suspense>} />
            <Route path="/prompts" element={<Suspense fallback={fallback}><Prompts /></Suspense>} />
            <Route path="*" element={<Navigate to="/" replace />} />
          </Route>
        </Routes>
      </HashRouter>
    </ConfigProvider>
  );
}
