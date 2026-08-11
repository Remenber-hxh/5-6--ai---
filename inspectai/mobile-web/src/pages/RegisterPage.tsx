import { Button, Input, Toast } from "@/ui";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { ApiError } from "@/api/client";
import BeianLine from "@/components/BeianLine";
import { landingForRole, useAuth } from "@/store/auth";

/**
 * 自助注册。
 *
 * 凭注册码准入,不是开放注册 —— 后端 /api/assets 对任何【已登录】用户开放,
 * 谁注册成功谁就能看到客户的全部设备台账和健康状态。码由管理员在后台生成
 * 后发给班组,新人自己注册完当场就能干活,不用等审批。
 *
 * 角色和部门都写在码上,这里不让用户选 —— 让新人自己挑角色等于没有门槛。
 */
export default function RegisterPage() {
  const nav = useNavigate();
  const doRegister = useAuth((s) => s.register);
  const [code, setCode] = useState("");
  const [username, setUsername] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [password, setPassword] = useState("");
  const [showPwd, setShowPwd] = useState(false);
  const [loading, setLoading] = useState(false);

  async function submit() {
    if (!code.trim()) {
      Toast.show({ content: "请填写注册码" });
      return;
    }
    if (!username.trim() || !displayName.trim()) {
      Toast.show({ content: "请填写账号和姓名" });
      return;
    }
    // 长度下限和后端保持一致(6 位)。前端先拦一道,免得填完一整屏
    // 才被后端打回来重来。
    if (password.length < 6) {
      Toast.show({ content: "密码至少 6 位" });
      return;
    }
    setLoading(true);
    try {
      await doRegister({
        username: username.trim(),
        displayName: displayName.trim(),
        password,
        code: code.trim(),
      });
      Toast.show({ content: "注册成功", position: "bottom" });
      const role = useAuth.getState().user?.roleCode || "inspector";
      nav(landingForRole(role), { replace: true });
    } catch (err) {
      Toast.show({
        content: err instanceof ApiError ? err.message : "注册失败,请重试",
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

        <h1 className="screen-title">注册账号</h1>
        <p className="screen-sub">用主管发给你的注册码开通账号</p>

        <div className="field">
          <span className="field-label">注册码</span>
          <Input
            placeholder="例如 ABCD-2345"
            value={code}
            onChange={setCode}
            clearable
            /* 码里只有大写字母和数字。关掉自动大写以外的输入法干预,
               省得手机把它改成别的东西 */
            autoCapitalize="characters"
            autoCorrect="off"
            spellCheck={false}
          />
        </div>
        <div className="field">
          <span className="field-label">账号</span>
          <Input
            placeholder="登录用,例如 inspector02"
            value={username}
            onChange={setUsername}
            clearable
            autoComplete="username"
            autoCapitalize="none"
            autoCorrect="off"
          />
        </div>
        <div className="field">
          <span className="field-label">姓名</span>
          <Input
            placeholder="真实姓名,巡检记录会署这个名"
            value={displayName}
            onChange={setDisplayName}
            clearable
          />
        </div>
        <div className="field">
          <span className="field-label">密码</span>
          <Input
            type={showPwd ? "text" : "password"}
            placeholder="至少 6 位"
            value={password}
            onChange={setPassword}
            autoComplete="new-password"
            onEnterPress={submit}
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

        <Button block className="btn-primary" loading={loading} onClick={submit}>
          注册并进入
        </Button>

        <p className="screen-hint">
          已有账号?
          <button
            type="button"
            className="link-btn"
            onClick={() => nav("/login", { replace: true })}
          >
            去登录
          </button>
        </p>
      </div>

      <BeianLine />
    </div>
  );
}
