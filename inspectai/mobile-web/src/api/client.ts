// API 客户端 —— 与 Go 后端(同源 /api)对齐,token 走 X-InspectAI-Token 头。
// 与 admin-web 同契约,但用独立 storage 键,便于同机同时开两端调试。

const TOKEN_KEY = "inspectai_mobile_token";
const USER_KEY = "inspectai_mobile_user";

export interface CurrentUser {
  id: string;
  tenantId?: string;
  username: string;
  displayName: string;
  roleCode: string; // admin / manager / supervisor / inspector / 自定义
  roleName?: string;
  departmentName?: string;
  /** storage 根下的相对路径,用 avatarURL() 转成可访问地址;空则回落文字头像 */
  avatar?: string;
  isPlatformAdmin?: boolean;
  perms?: string[];
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
  code: string;
  status: number;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.code = code;
    this.status = status;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  headers.set("Content-Type", "application/json");
  const token = getToken();
  if (token) headers.set("X-InspectAI-Token", token);

  const res = await fetch(path, { ...init, headers });
  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) {
    throw new ApiError(res.status, body.error || "error", body.message || "请求失败");
  }
  return body as T;
}

export interface LoginResult {
  user: CurrentUser;
  mustChangePassword?: boolean;
}

export async function login(username: string, password: string): Promise<LoginResult> {
  const body = await api<{ token: string; user: CurrentUser; perms?: string[]; mustChangePassword?: boolean }>(
    "/api/auth/login",
    { method: "POST", body: JSON.stringify({ username, password }) },
  );
  setToken(body.token);
  const user: CurrentUser = { ...body.user, perms: body.perms };
  setStoredUser(user);
  return { user, mustChangePassword: body.mustChangePassword };
}

export function logout() {
  const token = getToken();
  if (token) void api("/api/auth/logout", { method: "POST" }).catch(() => void 0);
  setToken("");
  setStoredUser(null);
}
