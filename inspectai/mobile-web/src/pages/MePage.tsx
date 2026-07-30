import { Button } from "@/ui";
import { useNavigate } from "react-router-dom";

import { useAuth } from "@/store/auth";

// 个人页(占位):展示当前账号身份 + 退出。视觉沿用旧版玻璃卡片。
export default function MePage() {
  const nav = useNavigate();
  const user = useAuth((s) => s.user);
  const logout = useAuth((s) => s.logout);

  return (
    <div className="center-screen">
      <div className="glass-card">
        <span className="brand-badge">
          <span className="brand-word">JADEAST</span>
          <span className="brand-divider" />
          <span className="brand-cn">智巡</span>
        </span>

        <h1 className="screen-title">{user?.displayName || user?.username}</h1>
        <p className="screen-sub">
          {user?.roleName || user?.roleCode} · {user?.departmentName || "默认部门"}
        </p>

        <Button
          block
          className="btn-ghost"
          onClick={() => {
            logout();
            nav("/login", { replace: true });
          }}
        >
          退出登录
        </Button>
      </div>
    </div>
  );
}
