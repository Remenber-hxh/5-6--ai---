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
      <motion.div
        initial={{ opacity: 0, y: 16 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.45, ease: "easeOut" }}
        style={styles.card}
      >
        <div style={{ ...styles.brand, display: "flex", alignItems: "center", gap: 8 }}>
          <img src="logo.svg" alt="" style={{ width: 24, height: 24 }} />
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
    // 与旧版同配方:辉光 + 暗化叠加 + 科技巡检背景图
    background:
      "radial-gradient(circle at 50% 44%, rgba(34, 160, 255, 0.10), transparent 30%)," +
      "linear-gradient(180deg, rgba(1, 10, 26, 0.15), rgba(1, 10, 26, 0.45))," +
      "url('login-bg.png') center / cover no-repeat, #020814",
    position: "relative",
    overflow: "hidden",
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
