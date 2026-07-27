import { create } from "zustand";

import {
  CurrentUser,
  getStoredUser,
  getToken,
  login as apiLogin,
  logout as apiLogout,
} from "@/api/client";

interface AuthState {
  user: CurrentUser | null;
  loggedIn: boolean;
  login: (username: string, password: string) => Promise<{ mustChangePassword?: boolean }>;
  logout: () => void;
}

export const useAuth = create<AuthState>((set) => ({
  user: getStoredUser(),
  loggedIn: Boolean(getToken()),
  async login(username, password) {
    const res = await apiLogin(username, password);
    set({ user: res.user, loggedIn: true });
    return { mustChangePassword: res.mustChangePassword };
  },
  logout() {
    apiLogout();
    set({ user: null, loggedIn: false });
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
