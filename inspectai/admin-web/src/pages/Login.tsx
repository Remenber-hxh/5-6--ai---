import { LockOutlined, UserOutlined } from "@ant-design/icons";
import { Button, Form, Input, message } from "antd";
import { motion } from "motion/react";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { useAuth } from "../store/auth";

// 深色科技风登录页:延续旧版气质(深蓝黑底 + 青绿光),卡片只保留必要字段
export default function Login() {
  const nav = useNavigate();
  const login = useAuth((s) => s.login);
  const [loading, setLoading] = useState(false);

  async function onFinish(values: { username: string; password: string }) {
    setLoading(true);
    try {
      await login(values.username.trim(), values.password);
      nav("/", { replace: true });
    } catch (e) {
      message.error(e instanceof Error ? e.message : "登录失败");
    } finally {
      setLoading(false);
    }
  }

  return (
    <div style={styles.page}>
      <div style={styles.horizon} />
      <motion.div
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.45, ease: "easeOut" }}
        style={styles.card}
      >
        <div style={styles.brand}>
          JADEAST <span style={{ color: "#3ee6b4" }}>| 智巡</span>
        </div>
        <h1 style={styles.title}>管理后台登录</h1>
        <Form layout="vertical" onFinish={onFinish} requiredMark={false}>
          <Form.Item name="username" rules={[{ required: true, message: "请输入账号" }]}>
            <Input size="large" prefix={<UserOutlined />} placeholder="账号" autoFocus />
          </Form.Item>
          <Form.Item name="password" rules={[{ required: true, message: "请输入密码" }]}>
            <Input.Password size="large" prefix={<LockOutlined />} placeholder="密码" />
          </Form.Item>
          <Button type="primary" size="large" htmlType="submit" block loading={loading}>
            登录
          </Button>
        </Form>
        <div style={styles.foot}>忘记密码?联系系统管理员重置</div>
      </motion.div>
    </div>
  );
}

const styles: Record<string, React.CSSProperties> = {
  page: {
    minHeight: "100vh",
    display: "grid",
    placeItems: "center",
    background: "radial-gradient(120% 80% at 50% 120%, #0a2433 0%, #071521 38%, #050b12 70%)",
    position: "relative",
    overflow: "hidden",
  },
  horizon: {
    position: "absolute",
    left: "50%",
    bottom: "-72vw",
    transform: "translateX(-50%)",
    width: "160vw",
    height: "80vw",
    borderRadius: "50%",
    background: "#050b12",
    borderTop: "2px solid rgba(170, 255, 230, 0.85)",
    boxShadow: "0 -10px 80px 6px rgba(62, 230, 180, 0.35)",
    pointerEvents: "none",
  },
  card: {
    position: "relative",
    zIndex: 1,
    width: 380,
    padding: "36px 34px 26px",
    borderRadius: 14,
    background: "rgba(10, 20, 28, 0.82)",
    border: "1px solid rgba(62, 230, 180, 0.22)",
    backdropFilter: "blur(14px)",
    boxShadow: "0 18px 60px rgba(0, 0, 0, 0.45)",
  },
  brand: {
    fontSize: 13,
    letterSpacing: "0.18em",
    color: "#eef6f4",
    fontWeight: 700,
    marginBottom: 18,
  },
  title: { color: "#eef6f4", fontSize: 24, margin: "0 0 22px", fontWeight: 700 },
  foot: { marginTop: 16, textAlign: "center", fontSize: 12, color: "#7e94a0" },
};
