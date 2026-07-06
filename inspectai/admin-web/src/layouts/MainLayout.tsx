import {
  ApartmentOutlined,
  AuditOutlined,
  CalendarOutlined,
  DatabaseOutlined,
  FileSearchOutlined,
  FileTextOutlined,
  FormOutlined,
  LogoutOutlined,
  RobotOutlined,
  SettingOutlined,
  TeamOutlined,
} from "@ant-design/icons";
import { Avatar, Dropdown, Layout, Menu } from "antd";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";

import { isMgmtRole, useAuth } from "../store/auth";

const { Sider, Header, Content } = Layout;

const NAV = [
  { key: "/", icon: <RobotOutlined />, label: "Agent 工作台" },
  { key: "/plan", icon: <CalendarOutlined />, label: "巡检计划" },
  { key: "/ledger", icon: <DatabaseOutlined />, label: "资产台账" },
  { key: "/record", icon: <FileSearchOutlined />, label: "巡检记录" },
  { key: "/approval", icon: <AuditOutlined />, label: "审批中心" },
  { key: "/data", icon: <ApartmentOutlined />, label: "数据看板" },
  { key: "/users", icon: <TeamOutlined />, label: "用户与权限" },
  { key: "/logs", icon: <FileTextOutlined />, label: "操作日志" },
  { key: "/system", icon: <SettingOutlined />, label: "系统管理" },
  { key: "/prompts", icon: <FormOutlined />, label: "提示词模板", mgmtOnly: true },
];

export default function MainLayout() {
  const nav = useNavigate();
  const loc = useLocation();
  const { user, loggedIn, logout } = useAuth();

  if (!loggedIn) return <Navigate to="/login" replace />;

  const items = NAV.filter((n) => !n.mgmtOnly || isMgmtRole(user)).map(
    ({ key, icon, label }) => ({ key, icon, label }),
  );

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider theme="dark" width={208}>
        <div
          style={{
            color: "#eef6f4",
            fontWeight: 800,
            letterSpacing: "0.08em",
            padding: "18px 20px 14px",
            fontSize: 15,
          }}
        >
          JADEAST <span style={{ color: "#3ee6b4" }}>智巡</span>
        </div>
        <Menu
          theme="dark"
          mode="inline"
          selectedKeys={[loc.pathname]}
          items={items}
          onClick={({ key }) => nav(key)}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            background: "#fff",
            padding: "0 20px",
            display: "flex",
            justifyContent: "flex-end",
            alignItems: "center",
            borderBottom: "1px solid #eef1f5",
          }}
        >
          <Dropdown
            menu={{
              items: [
                {
                  key: "logout",
                  icon: <LogoutOutlined />,
                  label: "退出登录",
                  onClick: () => {
                    logout();
                    nav("/login", { replace: true });
                  },
                },
              ],
            }}
          >
            <span style={{ cursor: "pointer", display: "inline-flex", alignItems: "center", gap: 8 }}>
              <Avatar size={28} style={{ background: "#0f2333" }}>
                {(user?.displayName || "?").slice(0, 1)}
              </Avatar>
              <span>{user?.displayName || user?.username}</span>
            </span>
          </Dropdown>
        </Header>
        <Content style={{ padding: 20, background: "#f5f7fa" }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
