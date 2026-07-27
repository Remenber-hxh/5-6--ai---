import { Toast } from "antd-mobile";
import { useEffect } from "react";

import PendingPanel from "@/components/PendingPanel";
import { useAuth } from "@/store/auth";
import { usePending } from "@/store/pending";

// 拍照台:拍多张 → 存进离线仓库 → 联网后自动上传补 AI 识别。
// 弱网现场只管拍,照片与拍摄时间先落地,不被信号拖住巡检节奏。
export default function CapturePage() {
  const user = useAuth((s) => s.user);
  const { online, saving, init, addFiles } = usePending();

  useEffect(() => {
    void init();
  }, [init]);

  async function onPick(e: React.ChangeEvent<HTMLInputElement>) {
    const files = Array.from(e.target.files || []);
    e.target.value = ""; // 允许同一文件重选
    if (!files.length) return;
    const { added, error } = await addFiles(files, user?.id || "");
    if (error) {
      Toast.show({ content: error, duration: 3000 });
    } else {
      Toast.show({ content: `已保存 ${added} 张`, position: "bottom" });
    }
  }

  return (
    <div className="capture-screen">
      <div className="capture-top">
        <span className="brand-badge">
          <span className="brand-word">JADEAST</span>
          <span className="brand-divider" />
          <span className="brand-cn">智巡</span>
        </span>
        <span className={online ? "net-pill net-on" : "net-pill net-off"}>
          {online ? "在线" : "离线 · 照片先存本机"}
        </span>
      </div>

      <div className="capture-main">
        <h1 className="capture-title">对准巡检设备拍照</h1>
        <p className="capture-sub">
          {online ? "AI 会自动识别场景,调出对应的日报模板" : "没有网络也能拍,联网后自动上传识别"}
        </p>

        <div className="viewfinder" aria-hidden="true">
          <span className="vf-corner vf-tl" />
          <span className="vf-corner vf-tr" />
          <span className="vf-corner vf-bl" />
          <span className="vf-corner vf-br" />
          <span className="vf-scan" />
        </div>

        <label className="shutter-wrap">
          <input
            className="shutter-input"
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={onPick}
            aria-label="拍照"
          />
          <span className={saving ? "shutter shutter-busy" : "shutter"} />
        </label>
        <div className="shutter-label">{saving ? "保存中…" : "拍照"}</div>

        <div className="capture-who">
          {user?.displayName || user?.username} · {user?.departmentName || "默认部门"}
        </div>
      </div>

      <PendingPanel />
    </div>
  );
}
