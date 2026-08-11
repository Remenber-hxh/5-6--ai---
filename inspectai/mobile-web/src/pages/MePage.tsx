import { Avatar, Cell, CellGroup, Dialog, Loading, Toast } from "@/ui";
import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import { uploadMyAvatar } from "@/api/inspection";
import BeianLine from "@/components/BeianLine";
import FlowHeader from "@/components/FlowHeader";
import { IconLedger, IconLock, IconPhotos, IconTasks } from "@/components/icons";
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
      Toast.show({
        content: err instanceof Error ? err.message : "头像上传失败",
      });
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
          <button
            className="me-avatar"
            onClick={() => fileRef.current?.click()}
            disabled={busy}
          >
            <Avatar name={name} src={user?.avatar} size="large" />
            {/* 上传中盖一层。原来这里常驻一个「换」字角标(顺带兼作转圈位),
                角标去掉后仍然需要一个"正在传"的反馈 —— 弱网下压缩加上传要好几秒,
                没有任何反应用户会以为没点上,然后反复点。 */}
            {busy && (
              <span className="me-avatar-busy">
                <Loading size={16} color="#fff" />
              </span>
            )}
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
            {user?.roleName || user?.roleCode} ·{" "}
            {user?.departmentName || "默认部门"}
          </p>
        </div>

        {/* 管理角色登录后落在这一屏,给它该有的入口 —— 否则这页是个死胡同 */}
        {/* 图标用手写细线 SVG,不用 emoji —— 🖼(U+1F5BC)在很多系统上没有字形,
            这里一度渲染成豆腐块;而且 emoji 放在设置列表里也不像正经产品。 */}
        <CellGroup header="巡检">
          <Cell
            label="我的任务"
            icon={
              <span className="me-ic">
                <IconTasks />
              </span>
            }
            onClick={() => nav("/tasks")}
          />
          <Cell
            label="待处理照片"
            icon={
              <span className="me-ic">
                <IconPhotos />
              </span>
            }
            onClick={() => nav("/review")}
          />
          <Cell
            label="设备健康"
            icon={
              <span className="me-ic">
                <IconLedger />
              </span>
            }
            onClick={() => nav("/ledger")}
          />
        </CellGroup>

        {/* 这一组原来列的是账号/角色/部门三行只读信息,而角色和部门在上面的
            身份区已经写着了 —— 一屏之内说两遍同一句话。换成能【做事】的入口。 */}
        <CellGroup header="账号">
          <Cell
            label="修改密码"
            icon={
              <span className="me-ic">
                <IconLock />
              </span>
            }
            onClick={() => nav("/me/password")}
          />
        </CellGroup>

        <BeianLine />
      </div>

      {/* 退出登录钉在底部,不放进滚动区。
          原来它跟在「部门」那行后面 —— 内容 938px、容器 788px,只多出 150px,
          于是这个按钮【整个在折叠之下】,而视口底部看到的是一行普通 Cell,
          页面看起来已经结束了,没有任何"下面还有东西"的提示。用户报"退出登录坏了",
          实际是根本没意识到要往下滚。
          注:自动化点击测不出这类问题 —— Playwright 点击前会自动滚到元素,
          真人手指不会。这次是靠命中测试(elementFromPoint)才发现的。 */}
      <div className="flow-foot">
        <button className="me-logout" onClick={() => void onLogout()}>
          退出登录
        </button>
      </div>
    </div>
  );
}
