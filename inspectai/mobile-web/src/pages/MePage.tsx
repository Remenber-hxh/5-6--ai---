import { Avatar, Cell, CellGroup, Dialog, Loading, Toast } from "@/ui";
import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { uploadMyAvatar } from "@/api/inspection";
import BeianLine from "@/components/BeianLine";
import FlowHeader from "@/components/FlowHeader";
import { prepareAvatar } from "@/lib/avatar";
import { useAuth } from "@/store/auth";

/**
 * 个人空间。
 *
 * 之前这里只有 39 行:一张居中玻璃卡 + 姓名 + 退出按钮 —— 不像 app,
 * 像个登录后的落地页。而管理角色登录后正好落在这一屏。
 *
 * 现在按手机端"我的"页的通行形态重做:头部身份区(可换头像)+ 分组设置列表。
 */
export default function MePage() {
  const nav = useNavigate();
  const user = useAuth((s) => s.user);
  const patchUser = useAuth((s) => s.patchUser);
  const logout = useAuth((s) => s.logout);
  const fileRef = useRef<HTMLInputElement>(null);
  const [busy, setBusy] = useState(false);

  const name = user?.displayName || user?.username || "巡检员";

  async function onPickAvatar(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = ""; // 允许重选同一张
    if (!file || busy) return;
    setBusy(true);
    try {
      // 相册随手一张就是 3–8MB,先压到 256px 方形再传 ——
      // 弱网现场直接传原图要等半天,后端 2MB 上限也会拒
      const blob = await prepareAvatar(file);
      const path = await uploadMyAvatar(blob);
      patchUser({ avatar: path });
      Toast.show({ content: "头像已更新", position: "bottom" });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "头像上传失败" });
    } finally {
      setBusy(false);
    }
  }

  async function onLogout() {
    // 退出会清掉本机登录态,待上传的照片要等重新登录才能continue —— 值得确认一次
    const ok = await Dialog.confirm({
      title: "退出登录",
      content: "退出后需要重新登录才能继续巡检。本机未上传的照片不会丢失。",
      confirmText: "退出",
    });
    if (!ok) return;
    logout();
    nav("/login", { replace: true });
  }

  return (
    <div className="flow-screen me-screen">
      <FlowHeader title="我的" onBack={() => nav("/")} />

      <div className="scroll-area flow-body">
        {/* 身份区:点头像即换 */}
        <div className="me-hero">
          <button className="me-avatar" onClick={() => fileRef.current?.click()} disabled={busy}>
            <Avatar name={name} src={user?.avatar} size="large" />
            <span className="me-avatar-edit">{busy ? <Loading size={16} color="#fff" /> : "换"}</span>
          </button>
          {/* 【别用 .upload-input】那个类是给 <label> 包裹用的(absolute inset:0 铺满父级)。
              这里是独立元素、靠 ref.click() 触发,没有定位父级 —— 用了会铺成一张
              覆盖全屏的透明层,把页面上所有按钮的点击全部接走(踩过)。 */}
          <input
            ref={fileRef}
            hidden
            type="file"
            accept="image/*"
            onChange={onPickAvatar}
            aria-label="更换头像"
          />
          <h1 className="me-name">{name}</h1>
          <p className="me-sub">
            {user?.roleName || user?.roleCode} · {user?.departmentName || "默认部门"}
          </p>
        </div>

        {/* 管理角色登录后落在这一屏,给它该有的入口 —— 否则这页是个死胡同 */}
        <CellGroup header="巡检">
          <Cell label="我的任务" icon={<span className="me-ic">📋</span>} onClick={() => nav("/tasks")} />
          <Cell label="待处理照片" icon={<span className="me-ic">🖼</span>} onClick={() => nav("/review")} />
          <Cell label="设备健康" icon={<span className="me-ic">📈</span>} onClick={() => nav("/ledger")} />
        </CellGroup>

        <CellGroup header="账号">
          <Cell label="账号" text={user?.username} />
          <Cell label="角色" text={user?.roleName || user?.roleCode} />
          <Cell label="部门" text={user?.departmentName || "默认部门"} />
        </CellGroup>

        <button className="me-logout" onClick={() => void onLogout()}>
          退出登录
        </button>

        <BeianLine />
      </div>
    </div>
  );
}
