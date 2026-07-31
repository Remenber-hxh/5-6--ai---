import { create } from "zustand";

import {
  CurrentUser,
  getStoredUser,
  getToken,
  login as apiLogin,
  logout as apiLogout,
  setStoredUser,
} from "@/api/client";
import { unblockAll } from "@/lib/uploadQueue";

interface AuthState {
  user: CurrentUser | null;
  loggedIn: boolean;
  login: (username: string, password: string) => Promise<{ mustChangePassword?: boolean }>;
  logout: () => void;
  /** 局部更新当前用户(改头像等),同时写回 localStorage 让刷新后不回退 */
  patchUser: (patch: Partial<CurrentUser>) => void;
}

export const useAuth = create<AuthState>((set) => ({
  user: getStoredUser(),
  loggedIn: Boolean(getToken()),
  async login(username, password) {
    const res = await apiLogin(username, password);
    set({ user: res.user, loggedIn: true });
    // 之前因登录失效被拦下的照片,现在可以重新上传了 ——
    // 用户重新登录后还要逐张点重试是荒谬的。
    void unblockAll(true).catch(() => void 0);
    return { mustChangePassword: res.mustChangePassword };
  },
  logout() {
    apiLogout();
    set({ user: null, loggedIn: false });
  },
  patchUser(patch) {
    set((s) => {
      if (!s.user) return s;
      const next = { ...s.user, ...patch };
      // 本项目的登录态是从 localStorage 恢复的,只改内存的话刷新就回退了
      setStoredUser(next);
      return { user: next };
    });
  },
}));

// 登录后按角色自动落地(领导要的"进去直接到该用的界面",身份由账号定,不手选)。
export function landingForRole(roleCode: string): string {
  switch (roleCode) {
    case "supervisor":
      return "/approvals"; // 复核 → 审批
    case "manager":
    case "admin":
      return "/me"; // 管理角色移动端先落个人页(后台在 admin-web)
    default:
      return "/"; // 巡检员 → 拍照台
  }
}
