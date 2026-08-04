import { create } from "zustand";

import {
  CurrentUser,
  getStoredUser,
  getToken,
  login as apiLogin,
  logout as apiLogout,
  setStoredUser,
  setToken,
  setUnauthorizedHandler,
} from "@/api/client";
import { unblockAll } from "@/lib/uploadQueue";
import { Toast } from "@/ui";

interface AuthState {
  user: CurrentUser | null;
  loggedIn: boolean;
  login: (
    username: string,
    password: string,
  ) => Promise<{ mustChangePassword?: boolean }>;
  logout: () => void;
  /** 后端返回 401 时由 client 触发,清本地登录态 */
  sessionExpired: () => void;
  /** 局部更新当前用户(改头像等),同时写回 localStorage 让刷新后不回退 */
  patchUser: (patch: Partial<CurrentUser>) => void;
}

export const useAuth = create<AuthState>((set, get) => ({
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
  /**
   * 会话在后端已经失效(401)。和主动退出的区别:不再调 /logout ——
   * 那个请求也会 401,徒增一次弱网下的等待。
   */
  sessionExpired() {
    if (!get().loggedIn && !get().user) return; // 已经清过就别重复触发路由跳转
    setToken("");
    setStoredUser(null);
    set({ user: null, loggedIn: false });
    Toast.show({ content: "登录已过期,请重新登录" });
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

// 把 401 的出口接到 client 上。
//
// 【为什么用注册而不是直接 import】client 是最底层,让它去 import store 会形成
// client → store → client 的环:打包时的求值顺序不可控,很容易踩到
// "模块还没初始化完就被用了"的空指针。反过来由 store 注册回调,依赖方向单一。
setUnauthorizedHandler(() => useAuth.getState().sessionExpired());

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
