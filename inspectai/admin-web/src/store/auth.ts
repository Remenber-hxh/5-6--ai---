import { create } from "zustand";

import {
  CurrentUser,
  getStoredUser,
  getToken,
  login as apiLogin,
  logout as apiLogout,
} from "../api/client";

interface AuthState {
  user: CurrentUser | null;
  loggedIn: boolean;
  login: (username: string, password: string) => Promise<CurrentUser>;
  logout: () => void;
}

export const useAuth = create<AuthState>((set) => ({
  user: getStoredUser(),
  loggedIn: Boolean(getToken()),
  async login(username, password) {
    const user = await apiLogin(username, password);
    set({ user, loggedIn: true });
    return user;
  },
  logout() {
    apiLogout();
    set({ user: null, loggedIn: false });
  },
}));

// 管理角色(与旧版 render() 的门控口径一致)
export function isMgmtRole(user: CurrentUser | null): boolean {
  return ["admin", "manager", "supervisor"].includes(user?.roleCode || "");
}
