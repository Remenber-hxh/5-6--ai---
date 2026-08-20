import {
  LogoutOutlined,
  MenuFoldOutlined,
  MenuUnfoldOutlined,
  ReloadOutlined,
  SettingOutlined,
} from "@ant-design/icons";
import { Alert, Avatar, Badge, Button, Dropdown, Layout, Menu, Tooltip } from "antd";
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
          {/* 【数据范围提示】看不到数据时必须说清为什么。
              只给空列表,用户会以为数据丢了,反复刷新、报"系统坏了",
              而管理员那边一切正常 —— 这类问题最难定位,因为没人报对症状。
              挂在这里一处,覆盖所有页面。 */}
          {user?.dataScopeNotice && (
            <Alert type="warning" showIcon banner message={user.dataScopeNotice} style={{ marginBottom: 12 }} />
          )}
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
          {/* 【首页(/)不渲染】那是 AgentHome 的整屏布局,它自己页底已经有一份;
              两份都在的话这一份会掉到它的深色面板外面、露在白底上被截断。 */}
          {loc.pathname !== "/" && (
          <>
          {/* 网站备案号,常驻内容区底部。
              ICP 是工信部要求(链 beian.miit.gov.cn);公安联网备案要求编号放在
              底部并链到全国公安机关互联网站安全管理服务平台 —— 现在的域名是
              beian.mps.gov.cn,老的 www.beian.gov.cn 已经迁走,别再用旧链接。 */}
          <div style={{ textAlign: "center", padding: "12px 0 2px", color: "#9aa7b2", fontSize: 12 }}>
            <a
              href="https://beian.mps.gov.cn/#/query/webSearch?code=32020602003940"
              target="_blank"
              rel="noreferrer"
              style={{
                color: "inherit",
                textDecoration: "none",
                display: "inline-flex",
                alignItems: "center",
                gap: 3,
                whiteSpace: "nowrap",
              }}
            >
              {/* 警徽图标。文件缺失时自己藏起来,不要留碎图占位 ——
                  备案文字才是合规要求,图标是惯例。原图 36×40,宽高按比例写死。放 public/beian-gongan.png 即可。
                  【不带开头的斜杠】本页线上挂在 /v2/ 下,绝对路径会指到站点根。 */}
              <img
                src="beian-gongan.png"
                alt=""
                width={12}
                height={13}
                onError={(e) => {
                  e.currentTarget.style.display = "none";
                }}
              />
              苏公网安备32020602003940号
            </a>
            <span style={{ margin: "0 6px", opacity: 0.5 }} aria-hidden>
              ·
            </span>
            <a
              href="https://beian.miit.gov.cn/"
              target="_blank"
              rel="noopener"
              style={{ color: "inherit", textDecoration: "none" }}
            >
              苏ICP备2026048624号
            </a>
          </div>
          </>
          )}
        </Content>
      </Layout>
    </Layout>
  );
}
