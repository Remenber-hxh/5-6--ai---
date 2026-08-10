import {
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  ReloadOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { Avatar, Badge, Button, Dropdown, Layout, Menu, Tooltip } from "antd";
import { motion } from "motion/react";
import { useEffect, useState } from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router-dom";

import { hasPerm } from "../api/client";
import { listChangeRequests } from "../api/mgmt";
import { useAuth } from "../store/auth";

const { Sider, Header, Content } = Layout;

// 图标 = 旧版定制 SVG(public/nav);顺序与旧版一致(记录在台账前)
const ico = (name: string) => <img className="nav-ico" src={`nav/${name}.svg`} alt="" />;

// 菜单门控:perm = 权限矩阵能力键(后台可视化配置);adminOnly = 仅系统管理员(固定)。
// 与后端路由权限表(routes.go)同源对齐。
const NAV = [
  { key: "/", icon: ico("nav-home"), label: "首页" },
  { key: "/plan", icon: ico("nav-plan"), label: "巡检计划" },
  { key: "/record", icon: ico("nav-record"), label: "巡检记录" },
  { key: "/ledger", icon: ico("nav-asset"), label: "资产台账" },
  { key: "/approval", icon: ico("nav-approval"), label: "审批中心", perm: "approval_review" },
  { key: "/data", icon: ico("nav-data"), label: "数据看板" },
  { key: "/profile", icon: ico("nav-profile"), label: "个人首页" },
  { key: "/users", icon: ico("nav-users"), label: "用户与权限", adminOnly: true },
  { key: "/logs", icon: ico("nav-logs"), label: "操作日志", perm: "audit_view" },
  { key: "/system", icon: ico("nav-system"), label: "系统管理", adminOnly: true },
  { key: "/prompts", icon: ico("nav-prompt"), label: "提示词模板", perm: "prompt_manage" },
];

const COLLAPSE_KEY = "inspectai_sider_collapsed";

export default function MainLayout() {
  const nav = useNavigate();
  const loc = useLocation();
  const { user, loggedIn, logout } = useAuth();
  const revalidate = useAuth((s) => s.revalidate);

  // 进后台先问一次"这个 token 还认不认"。
  //
  // 【为什么必须问】登录态原本只看本地存没存过 token,失效了本地照样认:
  // 进来是完整的后台外壳,然后每个页面各自报"请求失败" ——
  // 看起来像"系统坏了",而不是"该重新登录了"。真失效时 client 的 401 出口
  // 会清干净登录态并回登录页。
  useEffect(() => {
    void revalidate();
  }, [revalidate]);
  const [pending, setPending] = useState(0);
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(COLLAPSE_KEY) === "1");

  useEffect(() => {
    if (!loggedIn) return;
    listChangeRequests()
      .then((rs) => setPending(rs.filter((r) => r.status === "pending").length))
      .catch(() => void 0);
  }, [loggedIn, loc.pathname]);

  if (!loggedIn) return <Navigate to="/login" replace />;

  const items = NAV.filter(
    (n) =>
      (!n.adminOnly || user?.roleCode === "admin") &&
      (!n.perm || hasPerm(user, n.perm)),
  ).map(({ key, icon, label }) => ({ key, icon, label }));

  function toggleCollapsed() {
    const next = !collapsed;
    setCollapsed(next);
    localStorage.setItem(COLLAPSE_KEY, next ? "1" : "0");
  }

  return (
    // 固定框架:侧栏/顶栏钉死,只有内容区滚动(长页面不再把头像框滚走)
    <Layout style={{ height: "100vh", overflow: "hidden" }}>
      <Sider
        theme="dark"
        width={208}
        collapsedWidth={64}
        collapsible
        collapsed={collapsed}
        trigger={null}
        style={{ background: "#0b1626", height: "100vh" }}
      >
        <div style={{ display: "flex", flexDirection: "column", height: "100%" }}>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              padding: collapsed ? "14px 0 10px" : "14px 14px 10px",
              justifyContent: collapsed ? "center" : "flex-start",
            }}
          >
            {collapsed ? (
              <img src="logo.svg" alt="智巡" style={{ width: 26, height: 26 }} />
            ) : (
              // 旧版设计好的品牌字标(深底变体:文字提亮,渐变图形不动)
              <img src="brand-logo-dark.svg" alt="JADEAST 智巡" style={{ width: 150, display: "block" }} />
            )}
          </div>
          <Menu
            theme="dark"
            mode="inline"
            selectedKeys={[loc.pathname]}
            items={items}
            onClick={({ key }) => nav(key)}
            style={{ flex: 1, background: "transparent", overflowY: "auto" }}
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
      <Layout style={{ height: "100vh", display: "flex", flexDirection: "column" }}>
        <Header
          style={{
            background: "#0b1626",
            padding: "0 16px 0 8px",
            display: "flex",
            alignItems: "center",
            gap: 10,
            borderBottom: "1px solid rgba(62, 230, 180, 0.35)",
            flex: "none",
          }}
        >
          <Button
            type="text"
            aria-label="收起/展开侧边栏"
            style={{ color: "#cfe0ea" }}
            icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
            onClick={toggleCollapsed}
          />
          <span style={{ marginRight: "auto" }} />
          <Tooltip title="刷新">
            <Button
              type="text"
              style={{ color: "#cfe0ea" }}
              icon={<ReloadOutlined />}
              onClick={() => window.location.reload()}
            />
          </Tooltip>
          <Tooltip title="系统配置">
            <Button
              type="text"
              style={{ color: "#cfe0ea" }}
              icon={<SettingOutlined />}
              onClick={() => nav("/system")}
            />
          </Tooltip>
          <Badge count={pending} size="small" offset={[-4, 2]}>
            <Button ghost style={{ borderColor: "rgba(62, 230, 180, 0.45)", color: "#3ee6b4" }} onClick={() => nav("/approval")}>
              待审批
            </Button>
          </Badge>
        </Header>
        <Content
          style={{
            padding: 20,
            background: "#f5f7fa",
            flex: 1,
            overflowY: "auto",
            display: "flex",
            flexDirection: "column",
          }}
        >
          {/* 页面切换轻过渡:淡入+8px 上浮,0.25s,不打扰操作 */}
          <motion.div
            key={loc.pathname}
            initial={{ opacity: 0, y: 8 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.25, ease: "easeOut" }}
            style={{ flex: 1 }}
          >
            <Outlet />
          </motion.div>
          {/* 网站 ICP 备案号(工信部要求),常驻内容区底部 */}
          <div style={{ textAlign: "center", padding: "12px 0 2px", color: "#9aa7b2", fontSize: 12 }}>
            <a
              href="https://beian.miit.gov.cn/"
              target="_blank"
              rel="noopener"
              style={{ color: "inherit", textDecoration: "none" }}
            >
              苏ICP备2026048624号
            </a>
          </div>
        </Content>
      </Layout>
    </Layout>
  );
}
