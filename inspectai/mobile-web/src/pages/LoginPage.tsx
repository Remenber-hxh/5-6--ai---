import { Button, Input, Toast } from "@/ui";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { ApiError } from "@/api/client";
import BeianLine from "@/components/BeianLine";
import { landingForRole, useAuth } from "@/store/auth";

// 登录:一步走。身份(部门/角色/租户)由账号自动带出,不手选;登录后按角色自动落地。
// 视觉与旧版 frontend/ 的登录屏保持一致(品牌标牌 + 玻璃卡片 + 翡翠渐变按钮)。
export default function LoginPage() {
  const nav = useNavigate();
  const doLogin = useAuth((s) => s.login);
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [showPwd, setShowPwd] = useState(false);

  async function submit() {
    if (!username.trim() || !password) {
      Toast.show({ content: "请输入账号和密码" });
      return;
    }
    setLoading(true);
    try {
      await doLogin(username.trim(), password);
      const role = useAuth.getState().user?.roleCode || "inspector";
      nav(landingForRole(role), { replace: true });
    } catch (err) {
      Toast.show({
        content: err instanceof ApiError ? err.message : "登录失败,请重试",
      });
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="center-screen">
      <div className="glass-card">
        <span className="brand-badge">
          <span className="brand-word">JADEAST</span>
          <span className="brand-divider" />
          <span className="brand-cn">智巡</span>
        </span>

        <h1 className="screen-title">巡检员登录</h1>
        <p className="screen-sub">
          请使用主管分配的账号登录,记录会自动归到你名下
        </p>

        <div className="field">
          <span className="field-label">账号</span>
          <Input
            placeholder="例如 inspector01"
            value={username}
            onChange={setUsername}
            clearable
            autoComplete="username"
          />
        </div>
        <div className="field">
          <span className="field-label">密码</span>
          <Input
            type={showPwd ? "text" : "password"}
            placeholder="请输入密码"
            value={password}
            onChange={setPassword}
            autoComplete="current-password"
            onEnterPress={submit}
            /* 现场戴手套、强光下打错密码很常见,没有小眼睛只能反复清空重来 */
            suffix={
              <button
                type="button"
                className="pwd-eye"
                aria-label={showPwd ? "隐藏密码" : "显示密码"}
                onClick={() => setShowPwd((v) => !v)}
              >
                {showPwd ? "隐藏" : "显示"}
              </button>
            }
          />
        </div>

        <Button
          block
          className="btn-primary"
          loading={loading}
          onClick={submit}
        >
          登录
        </Button>

        <p className="screen-hint">忘记密码?联系主管或系统管理员重置</p>
      </div>

      <BeianLine />
    </div>
  );
}
