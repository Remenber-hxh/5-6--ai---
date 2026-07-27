import { Toast } from "antd-mobile";
import { useEffect, useRef } from "react";

import { useAuth } from "@/store/auth";
import { useTray } from "@/store/tray";
import TrayStrip from "@/components/TrayStrip";

// 拍照台:拍多张 → 攒进托盘(IndexedDB)→ 联网后自动上传补 AI 识别。
// 弱网现场只管拍,照片与拍摄时间先落地,不被信号拖住巡检节奏。
export default function CapturePage() {
  const user = useAuth((s) => s.user);
  const { shots, online, busy, refresh, addFiles } = useTray();
  const inputRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function onPick(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files || []);
    e.target.value = ""; // 允许同一文件重选
    if (!files.length) return;
    try {
      await addFiles(files, user?.id || "");
      Toast.show({ content: `已存入托盘 ${files.length} 张`, position: "bottom" });
    } catch {
      Toast.show({ content: "存入失败,请重试" });
    }
  }

  return (
    <div className="capture-screen">
      {/* 顶部:身份 + 网络状态 */}
      <div className="capture-top">
        <span className="brand-badge">
          <span className="brand-word">JADEAST</span>
          <span className="brand-divider" />
          <span className="brand-cn">智巡</span>
        </span>
        <span className={online ? "net-pill net-on" : "net-pill net-off"}>
          {online ? "在线" : "离线暂存中"}
        </span>
      </div>

      {/* 中部:取景提示 + 快门 */}
      <div className="capture-main">
        <h1 className="capture-title">对准巡检设备拍照</h1>
        <p className="capture-sub">
          {online ? "AI 会自动识别场景,调出对应的日报模板" : "没有网络也能拍,照片先存本地,联网自动上传"}
        </p>

        <div className="viewfinder" aria-hidden="true">
          <span className="vf-corner vf-tl" />
          <span className="vf-corner vf-tr" />
          <span className="vf-corner vf-bl" />
          <span className="vf-corner vf-br" />
        </div>

        <label className="shutter-wrap">
          <input
            ref={inputRef}
            className="shutter-input"
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={onPick}
            aria-label="拍照"
          />
          <span className={busy ? "shutter shutter-busy" : "shutter"} />
        </label>
        <div className="shutter-label">{busy ? "处理中…" : "拍照"}</div>

        <div className="capture-who">
          {user?.displayName || user?.username} · {user?.departmentName || "默认部门"}
        </div>
      </div>

      {/* 底部:托盘 */}
      <TrayStrip shots={shots} />
    </div>
  );
}
