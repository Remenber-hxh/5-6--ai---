// API 客户端:与 Go 后端(同源 /api)对齐 —— token 走 X-InspectAI-Token 头
const TOKEN_KEY = "inspectai_admin_token";
const USER_KEY = "inspectai_admin_user";

export interface CurrentUser {
  id: string;
  username: string;
  displayName: string;
  roleCode: string; // admin / manager / supervisor / inspector
  roleName?: string;
  /** active / disabled / local("本地免鉴权"下的占位身份,不是真会话) */
  status?: string;
  perms?: string[]; // 登录时下发的能力键列表(权限矩阵)
  /**
   * 数据范围导致看不到数据时的说明。空 = 一切正常,不要显示任何东西。
   *
   * 【为什么要有】只给空列表是最糟的失败:用户以为数据丢了,反复刷新、
   * 报"系统坏了",而管理员那边一切正常。这句话让他知道该去找谁。
   */
  dataScopeNotice?: string;
}

// 能力检查:admin 全通过,其余看登录时下发的列表
export function hasPerm(user: CurrentUser | null, key: string): boolean {
  if (!user) return false;
  if (user.roleCode === "admin") return true;
  return (user.perms || []).includes(key);
}

export function getToken(): string {
  try {
    return localStorage.getItem(TOKEN_KEY) || "";
  } catch {
    return "";
  }
}

export function setToken(token: string) {
  try {
    if (token) localStorage.setItem(TOKEN_KEY, token);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {
    /* ignore */
  }
}

export function getStoredUser(): CurrentUser | null {
  try {
    const raw = localStorage.getItem(USER_KEY);
    return raw ? (JSON.parse(raw) as CurrentUser) : null;
  } catch {
    return null;
  }
}

export function setStoredUser(user: CurrentUser | null) {
  try {
    if (user) localStorage.setItem(USER_KEY, JSON.stringify(user));
    else localStorage.removeItem(USER_KEY);
  } catch {
    /* ignore */
  }
}

/** 会话失效时的出口。由 auth store 注册,避免 client 反向依赖 store。 */
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export async function api<T = any>(path: string, opts: RequestInit = {}): Promise<T> {
  const headers = new Headers(opts.headers || {});
  const token = getToken();
  if (token) headers.set("X-InspectAI-Token", token);
  if (opts.body && typeof opts.body === "string" && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }
  const res = await fetch(path, { ...opts, headers, credentials: "include" });
  let data: any = {};
  try {
    data = await res.json();
  } catch {
    /* 空响应 */
  }
  // 【登录接口除外】密码输错后端也是 401,当成"会话过期"会把上一个用户的
  // 本地信息一并抹掉,还会多跳一次路由。
  if (res.status === 401 && path !== "/api/auth/login") {
    setToken("");
    setStoredUser(null);
    // 【光改 hash 不够】store 里的 loggedIn 还是 true,路由守卫会把 #/login
    // 再弹回去 —— 表现是点什么都没反应。必须让 store 也知道。
    onUnauthorized?.();
    if (!location.hash.includes("login")) location.hash = "#/login";
    throw new ApiError(401, data.message || "登录已过期,请重新登录");
  }
  if (!res.ok) {
    throw new ApiError(res.status, data.message || data.error || `请求失败 (${res.status})`);
  }
  return data as T;
}

/**
 * 校验本地 token 是否还有效,并把服务端的最新身份取回来。
 * 返回 null 表示"不是真会话"(本地免鉴权模式),此时不要覆盖本地用户。
 */
export async function fetchMe(): Promise<CurrentUser | null> {
  const body = await api<{ user: CurrentUser; perms?: string[]; dataScopeNotice?: string }>(
    "/api/auth/me",
  );
  const user = body.user;
  if (!user || user.status === "local") return null;
  return { ...user, perms: body.perms, dataScopeNotice: body.dataScopeNotice };
}

export interface LoginResult {
  user: CurrentUser;
  mustChangePassword?: boolean; // 仍在使用默认密码,前端须强提示修改
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const data = await api<{ token?: string; user?: CurrentUser; perms?: string[]; mustChangePassword?: boolean }>(
    "/api/auth/login",
    {
      method: "POST",
      body: JSON.stringify({ username, password }),
    },
  );
  if (data.token) setToken(data.token);
  const user = (data.user || null) as CurrentUser | null;
  if (!user) throw new ApiError(500, "登录响应缺少用户信息");
  user.perms = data.perms || [];
  setStoredUser(user);
  return { user, mustChangePassword: data.mustChangePassword };
}

export function logout() {
  setToken("");
  setStoredUser(null);
}
