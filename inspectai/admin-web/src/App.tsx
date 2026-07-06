import { ConfigProvider } from "antd";
import zhCN from "antd/locale/zh_CN";
import { HashRouter, Route, Routes } from "react-router-dom";

import MainLayout from "./layouts/MainLayout";
import Dashboard from "./pages/Dashboard";
import Login from "./pages/Login";
import Placeholder from "./pages/Placeholder";

// HashRouter:构建产物可直接被任意静态服务/nginx 托管,无需 history 回退配置
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
            <Route path="/" element={<Dashboard />} />
            <Route path="/plan" element={<Placeholder title="巡检计划" />} />
            <Route path="/record" element={<Placeholder title="巡检记录" />} />
            <Route path="/ledger" element={<Placeholder title="资产台账" />} />
            <Route path="/approval" element={<Placeholder title="审批中心" />} />
            <Route path="/data" element={<Placeholder title="数据看板" />} />
            <Route path="/users" element={<Placeholder title="用户与权限" />} />
            <Route path="/logs" element={<Placeholder title="操作日志" />} />
            <Route path="/system" element={<Placeholder title="系统管理" />} />
            <Route path="/prompts" element={<Placeholder title="提示词模板" />} />
            <Route path="*" element={<Placeholder title="页面不存在" />} />
          </Route>
        </Routes>
      </HashRouter>
    </ConfigProvider>
  );
}
