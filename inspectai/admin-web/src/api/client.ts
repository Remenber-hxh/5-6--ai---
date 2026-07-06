// API 客户端:与 Go 后端(同源 /api)对齐 —— token 走 X-InspectAI-Token 头
const TOKEN_KEY = "inspectai_admin_token";
const USER_KEY = "inspectai_admin_user";

export interface CurrentUser {
  id: string;
  username: string;
  displayName: string;
  roleCode: string; // admin / manager / supervisor / inspector
  roleName?: string;
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
  if (res.status === 401) {
    setToken("");
    setStoredUser(null);
    if (!location.hash.includes("login")) location.hash = "#/login";
    throw new ApiError(401, data.message || "登录已过期,请重新登录");
  }
  if (!res.ok) {
    throw new ApiError(res.status, data.message || data.error || `请求失败 (${res.status})`);
  }
  return data as T;
}

export async function login(username: string, password: string): Promise<CurrentUser> {
  const data = await api<{ token?: string; user?: CurrentUser }>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  if (data.token) setToken(data.token);
  const user = (data.user || null) as CurrentUser | null;
  if (!user) throw new ApiError(500, "登录响应缺少用户信息");
  setStoredUser(user);
  return user;
}

export function logout() {
  setToken("");
  setStoredUser(null);
}
