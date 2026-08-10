import { create } from "zustand";

import {
  CurrentUser,
  fetchMe,
  getStoredUser,
  getToken,
  login as apiLogin,
  logout as apiLogout,
  setStoredUser,
  setToken,
  setUnauthorizedHandler,
} from "../api/client";

interface AuthState {
  user: CurrentUser | null;
  loggedIn: boolean;
  login: (username: string, password: string) => Promise<{ user: CurrentUser; mustChangePassword?: boolean }>;
  logout: () => void;
  /** 后端返回 401 时由 client 触发,清本地登录态 */
  sessionExpired: () => void;
  /** 开机校验:问后端这个 token 还认不认,顺带刷新身份与权限 */
  revalidate: () => Promise<void>;
}

export const useAuth = create<AuthState>((set) => ({
  user: getStoredUser(),
  loggedIn: Boolean(getToken()),
  async login(username, password) {
    const res = await apiLogin(username, password);
    set({ user: res.user, loggedIn: true });
    return res;
  },
  logout() {
    apiLogout();
    set({ user: null, loggedIn: false });
  },
  sessionExpired() {
    setToken("");
    setStoredUser(null);
    set({ user: null, loggedIn: false });
  },
  async revalidate() {
    if (!getToken()) return;
    try {
      const me = await fetchMe();
      // 服务端认这个会话:顺手刷新身份与权限。管理员改了某人的角色后,
      // 原来要等他退出重登才生效。
      if (me) {
        setStoredUser(me);
        set({ user: me, loggedIn: true });
      }
    } catch {
      // 401 已由 client 的全局出口清干净;其余错误(后端没起来)不动登录态。
    }
  },
}));

// 把 401 的出口接到 client 上。由 store 注册而不是 client 直接 import store,
// 避免 client → store → client 的循环依赖。
setUnauthorizedHandler(() => useAuth.getState().sessionExpired());

// 管理角色(与旧版 render() 的门控口径一致)
export function isMgmtRole(user: CurrentUser | null): boolean {
  return ["admin", "manager", "supervisor"].includes(user?.roleCode || "");
}
