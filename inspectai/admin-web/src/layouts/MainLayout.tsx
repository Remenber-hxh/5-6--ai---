import {
  ApartmentOutlined,
  AuditOutlined,
  CalendarOutlined,
  DatabaseOutlined,
  FileSearchOutlined,
  FileTextOutlined,
  FormOutlined,
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  ReloadOutlined,
  RobotOutlined,
  SettingOutlined,
  TeamOutlined,
  UserOutlined,
} from "@ant-design/icons";
import { Avatar, Badge, Button, Dropdown, Layout, Menu, Select, Tooltip } from "antd";
import { useEffect, useState } from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";

import { listAssets, listChangeRequests } from "../api/mgmt";
import { isMgmtRole, useAuth } from "../store/auth";
import { useUi } from "../store/ui";

const { Sider, Header, Content } = Layout;

// 导航顺序与旧版一致(记录在台账前)
const NAV = [
  { key: "/", icon: <RobotOutlined />, label: "首页" },
  { key: "/plan", icon: <CalendarOutlined />, label: "巡检计划" },
  { key: "/record", icon: <FileSearchOutlined />, label: "巡检记录" },
  { key: "/ledger", icon: <DatabaseOutlined />, label: "资产台账" },
  { key: "/approval", icon: <AuditOutlined />, label: "审批中心" },
  { key: "/data", icon: <ApartmentOutlined />, label: "数据看板" },
  { key: "/profile", icon: <UserOutlined />, label: "个人首页" },
  { key: "/users", icon: <TeamOutlined />, label: "用户与权限" },
  { key: "/logs", icon: <FileTextOutlined />, label: "操作日志" },
  { key: "/system", icon: <SettingOutlined />, label: "系统管理" },
  { key: "/prompts", icon: <FormOutlined />, label: "提示词模板", mgmtOnly: true },
];

const COLLAPSE_KEY = "inspectai_sider_collapsed";

export default function MainLayout() {
  const nav = useNavigate();
  const loc = useLocation();
  const { user, loggedIn, logout } = useAuth();
  const { project, setProject } = useUi();
  const [pending, setPending] = useState(0);
  const [projects, setProjects] = useState<string[]>([]);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === "1");

  useEffect(() => {
    if (!loggedIn) return;
    listChangeRequests()
      .then((rs) => setPending(rs.filter((r) => r.status === "pending").length))
      .catch(() => void 0);
  }, [loggedIn, loc.pathname]);

  useEffect(() => {
    if (!loggedIn) return;
    listAssets()
      .then((as) => setProjects(Array.from(new Set(as.map((a) => a.project).filter(Boolean))) as string[]))
      .catch(() => void 0);
  }, [loggedIn]);

  if (!loggedIn) return <Navigate to="/login" replace />;

  const items = NAV.filter((n) => !n.mgmtOnly || isMgmtRole(user)).map(
    ({ key, icon, label }) => ({ key, icon, label }),
  );
  const pageTitle = NAV.find((n) => n.key === loc.pathname)?.label || "";

  function toggleCollapsed() {
    const next = !collapsed;
    setCollapsed(next);
    localStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
  }

  return (
    <Layout style={{ minHeight: "100vh" }}>
      <Sider
        theme="dark"
        width={208}
        collapsedWidth={64}
        collapsible
        collapsed={collapsed}
        trigger={null}
        style={{ background: "#0b1626" }}
      >
        <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              padding: collapsed ? "16px 0 12px" : "16px 18px 12px",
              justifyContent: collapsed ? "center" : "flex-start",
            }}
          >
            <img src="logo.svg" alt="智巡" style={{ width: 30, height: 30, flex: "none" }} />
            {!collapsed && (
              <span
                style={{
                  color: "#eef6f4",
                  fontWeight: 800,
                  letterSpacing: "0.06em",
                  fontSize: 15,
                  whiteSpace: "nowrap",
                }}
              >
                JADEAST <span style={{ color: "#3ee6b4", fontWeight: 500 }}>智巡</span>
              </span>
            )}
          </div>
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[loc.pathname]}
            items={items}
            onClick={({ key }) => nav(key)}
            style={{ flex: 1, background: "transparent" }}
          />
          {/* 用户块固定在侧边栏底部(与旧版一致) */}
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
            placement="topLeft"
          >
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 10,
                padding: collapsed ? "14px 0" : "14px 16px",
                justifyContent: collapsed ? "center" : "flex-start",
                cursor: "pointer",
                borderTop: "1px solid rgba(255,255,255,0.08)",
              }}
            >
              <Avatar size={30} style={{ background: "rgba(62,230,180,0.18)", color: "#3ee6b4", flex: "none" }}>
                {(user?.displayName || "?").slice(0, 1)}
              </Avatar>
              {!collapsed && (
                <span style={{ display: "flex", flexDirection: "column", lineHeight: 1.3, minWidth: 0 }}>
                  <b style={{ color: "#eef6f4", fontSize: 13 }}>{user?.displayName || user?.username}</b>
                  <small style={{ color: "#8aa3ad", fontSize: 11 }}>{user?.roleName || user?.roleCode}</small>
                </span>
              )}
            </div>
          </Dropdown>
        </div>
      </Sider>
      <Layout>
        <Header
          style={{
            background: "#fff",
            padding: "0 16px 0 8px",
            display: "flex",
            alignItems: "center",
            gap: 10,
            borderBottom: "1px solid #eef1f5",
          }}
        >
          <Button
            type="text"
            aria-label="收起/展开侧边栏"
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={toggleCollapsed}
          />
          <b style={{ fontSize: 15 }}>{pageTitle}</b>
          <Select
            allowClear
            placeholder="全部项目"
            style={{ width: 160, marginLeft: 12, marginRight: "auto" }}
            value={project || undefined}
            options={projects.map((p) => ({ value: p, label: p }))}
            onChange={(v) => setProject(v || "")}
          />
          <Tooltip title="刷新">
            <Button type="text" icon={<ReloadOutlined />} onClick={() => window.location.reload()} />
          </Tooltip>
          <Tooltip title="系统配置">
            <Button type="text" icon={<SettingOutlined />} onClick={() => nav("/system")} />
          </Tooltip>
          <Badge count={pending} size="small" offset={[-4, 2]}>
            <Button onClick={() => nav("/approval")}>待审批</Button>
          </Badge>
        </Header>
        <Content style={{ padding: 20, background: "#f5f7fa" }}>
          <Outlet />
        </Content>
      </Layout>
    </Layout>
  );
}
