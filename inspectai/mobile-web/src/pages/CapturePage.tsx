import { useAuth } from "@/store/auth";

// 拍照台(占位)。Phase 1 后续步骤在此填入:
//   - 拍照托盘(拍多张 → 攒起来)
//   - 离线队列(IndexedDB:没网先存,来网自动传)
//   - 联网补 AI 识别 → 确认字段 → 幂等提交
export default function CapturePage() {
  const user = useAuth((s) => s.user);
  return (
    <div className="center-screen">
      <span className="brand-badge">
        <span className="brand-word">JADEAST</span>
        <span className="brand-divider" />
        <span className="brand-cn">智巡</span>
      </span>

      <h1 className="screen-title" style={{ textAlign: "center" }}>
        对准巡检设备拍照
      </h1>
      <p className="screen-sub" style={{ textAlign: "center" }}>
        AI 会自动识别场景,调出对应的日报模板
      </p>

      <div style={{ color: "var(--ink-3)", fontSize: 12, marginTop: 8 }}>
        {user?.displayName || user?.username} · {user?.departmentName || "默认部门"}
      </div>

      <div style={{ color: "var(--ink-3)", fontSize: 12, marginTop: 28 }}>
        (拍照托盘 / 离线队列 / 水印将在此填入)
      </div>
    </div>
  );
}
