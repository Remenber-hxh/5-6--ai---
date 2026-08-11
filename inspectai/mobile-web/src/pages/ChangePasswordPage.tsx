import { Button, Input, Toast } from "@/ui";
import { useState } from "react";
import { useNavigate } from "react-router-dom";

import { ApiError, changeMyPassword } from "@/api/client";
import FlowHeader from "@/components/FlowHeader";
import { useAuth } from "@/store/auth";

/**
 * 改自己的密码。
 *
 * 【后端改完会踢掉所有会话,包括当前这个】这是有意的:改密码的动机常常就是
 * "密码可能泄露了",只留当前会话有效、别的设备照旧登着,等于没解决问题。
 * 所以这里成功后要主动清本地登录态回登录页,而不是等下一个请求 401 ——
 * 后者的表现是"改完密码就坏了"。
 */
export default function ChangePasswordPage() {
  const nav = useNavigate();
  const sessionExpired = useAuth((s) => s.sessionExpired);
  const [oldPwd, setOldPwd] = useState("");
  const [newPwd, setNewPwd] = useState("");
  const [confirmPwd, setConfirmPwd] = useState("");
  const [showPwd, setShowPwd] = useState(false);
  const [loading, setLoading] = useState(false);

  async function submit() {
    if (!oldPwd) {
      Toast.show({ content: "请输入当前密码" });
      return;
    }
    // 下限跟后端一致(6 位)。前端先拦一道,免得填完三个框才被打回来。
    if (newPwd.length < 6) {
      Toast.show({ content: "新密码至少 6 位" });
      return;
    }
    if (newPwd === oldPwd) {
      Toast.show({ content: "新密码不能和当前密码相同" });
      return;
    }
    // 【必须有确认框】改密码没有"撤销":打错一个字符,人就被自己锁在外面了,
    // 只能去找管理员重置。多一个框换掉这个风险很划算。
    if (newPwd !== confirmPwd) {
      Toast.show({ content: "两次输入的新密码不一致" });
      return;
    }
    setLoading(true);
    try {
      await changeMyPassword(oldPwd, newPwd);
      Toast.show({ content: "密码已修改,请用新密码重新登录" });
      sessionExpired(); // 清本地登录态(后端那边所有会话已经作废)
      nav("/login", { replace: true });
    } catch (err) {
      Toast.show({
        content: err instanceof ApiError ? err.message : "修改失败,请重试",
      });
    } finally {
      setLoading(false);
    }
  }

  const eye = (
    <button
      type="button"
      className="pwd-eye"
      aria-label={showPwd ? "隐藏密码" : "显示密码"}
      onClick={() => setShowPwd((v) => !v)}
    >
      {showPwd ? "隐藏" : "显示"}
    </button>
  );

  return (
    <div className="flow-screen me-screen">
      <FlowHeader title="修改密码" onBack={() => nav(-1)} />

      <div className="scroll-area flow-body">
        <div className="pwd-form">
          <div className="field">
            <span className="field-label">当前密码</span>
            <Input
              type={showPwd ? "text" : "password"}
              placeholder="请输入当前密码"
              value={oldPwd}
              onChange={setOldPwd}
              autoComplete="current-password"
              suffix={eye}
            />
          </div>
          <div className="field">
            <span className="field-label">新密码</span>
            <Input
              type={showPwd ? "text" : "password"}
              placeholder="至少 6 位"
              value={newPwd}
              onChange={setNewPwd}
              autoComplete="new-password"
            />
          </div>
          <div className="field">
            <span className="field-label">确认新密码</span>
            <Input
              type={showPwd ? "text" : "password"}
              placeholder="再输入一次"
              value={confirmPwd}
              onChange={setConfirmPwd}
              autoComplete="new-password"
              onEnterPress={submit}
            />
          </div>
          <p className="screen-hint">
            改完需要重新登录。其他设备上的登录也会一并退出。
          </p>
        </div>
      </div>

      <div className="flow-foot">
        <Button
          block
          className="btn-primary"
          loading={loading}
          onClick={submit}
        >
          确认修改
        </Button>
      </div>
    </div>
  );
}
