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
  /** active / disabled / local("本地免鉴权"下的占位身份,不是真会话) */
  status?: string;
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

/**
 * 请求超时。机房里信号时有时无,不设超时的 fetch 会一直挂着 ——
 * 页面转圈不停、轮询卡死在某一次上,用户只能杀进程重开。
 * 30 秒:AI 识别那条链路本来就慢(实测 12~23 秒),不能设太短。
 */
const TIMEOUT_MS = 30_000;

/** 会话失效时的出口。由 auth store 注册,避免 client 反向依赖 store。 */
let onUnauthorized: (() => void) | null = null;
export function setUnauthorizedHandler(fn: () => void) {
  onUnauthorized = fn;
}

export interface ApiOptions extends RequestInit {
  /** 覆盖默认超时(毫秒);上传大图这类可以放宽 */
  timeoutMs?: number;
}

export async function api<T>(path: string, init: ApiOptions = {}): Promise<T> {
  const { timeoutMs = TIMEOUT_MS, ...rest } = init;
  const headers = new Headers(rest.headers);
  // 【不能无条件设 JSON】FormData 要浏览器自己写 multipart 的 boundary,
  // 手动设 Content-Type 会把 boundary 冲掉,后端解不出文件。
  // 原来因为这条,头像上传和离线照片上传只能各自绕开 client 裸写 fetch,
  // 于是 token 头、错误处理各写了一遍。
  const isForm = rest.body instanceof FormData;
  if (!isForm && !headers.has("Content-Type"))
    headers.set("Content-Type", "application/json");
  const token = getToken();
  if (token) headers.set("X-InspectAI-Token", token);

  // 外部传了 signal 就尊重它,同时叠加超时 —— 两者任一触发都中止
  const ctl = new AbortController();
  const timer = setTimeout(() => ctl.abort(), timeoutMs);
  if (rest.signal)
    rest.signal.addEventListener("abort", () => ctl.abort(), { once: true });

  let res: Response;
  try {
    res = await fetch(path, { ...rest, headers, signal: ctl.signal });
  } catch (err) {
    // AbortError 的原生文案是英文的 "The user aborted a request",
    // 直接弹给巡检员没有意义,换成能指导下一步的话。
    if (err instanceof DOMException && err.name === "AbortError") {
      throw new ApiError(0, "timeout", "网络超时,请检查信号后重试");
    }
    throw new ApiError(0, "network", "网络不可用,请稍后重试");
  } finally {
    clearTimeout(timer);
  }

  const text = await res.text();
  let body: Record<string, unknown> = {};
  if (text) {
    try {
      body = JSON.parse(text) as Record<string, unknown>;
    } catch {
      // 网关 502/超时页返回的是 HTML。原来这里会抛 SyntaxError,
      // 页面 catch 到后显示的提示和真实原因毫无关系,查起来很费劲。
      throw new ApiError(
        res.status,
        "bad_response",
        res.ok ? "服务返回了无法解析的内容" : `服务异常(${res.status})`,
      );
    }
  }
  if (!res.ok) {
    // 【登录接口除外】密码输错后端也返回 401,当成"会话过期"去清登录态
    // 会把上一个用户的本地信息一起抹掉,还可能触发一次多余的跳转。
    if (res.status === 401 && path !== "/api/auth/login") {
      // 会话过期。原来每个页面各自报"加载失败",而本地还存着用户信息,
      // 看起来像"登着录但什么都打不开"。统一清掉并回登录页。
      onUnauthorized?.();
    }
    throw new ApiError(
      res.status,
      String(body.error || "error"),
      String(body.message || "请求失败"),
    );
  }
  return body as T;
}

export interface LoginResult {
  user: CurrentUser;
  mustChangePassword?: boolean;
}

export async function login(
  username: string,
  password: string,
): Promise<LoginResult> {
  const body = await api<{
    token: string;
    user: CurrentUser;
    perms?: string[];
    mustChangePassword?: boolean;
  }>("/api/auth/login", {
    method: "POST",
    body: JSON.stringify({ username, password }),
  });
  setToken(body.token);
  const user: CurrentUser = { ...body.user, perms: body.perms };
  setStoredUser(user);
  return { user, mustChangePassword: body.mustChangePassword };
}

/**
 * 校验本地 token 是否还有效,并顺带把服务端的最新身份取回来。
 *
 * 【为什么需要】登录态原本只看 localStorage 里有没有 token 这个字符串。
 * token 早就失效了(会话过期、账号被停用、服务端换了库)本地也照样认,
 * 于是进来看到的是完整的 App 外壳,然后每个页面各自弹"加载失败" ——
 * 巡检员的感受是"登着录但什么都打不开",而不是"该重新登录了"。
 *
 * 返回 null 表示"没法确认"(离线、后端没起来):此时【不能】判定为失效,
 * 机房里本来就常没信号,把人踢回登录页反而毁掉离线可用性。
 */
export async function fetchMe(): Promise<CurrentUser | null> {
  const body = await api<{ user: CurrentUser; perms?: string[] }>(
    "/api/auth/me",
  );
  const user = body.user;
  if (!user) return null;
  // 后端在"本地免鉴权"模式下会回一个 status=local 的占位身份。
  // 那不是真会话,拿它覆盖本地用户会把显示名和角色改错。
  if (user.status === "local") return null;
  return { ...user, perms: body.perms };
}

export function logout() {
  const token = getToken();
  if (token)
    void api("/api/auth/logout", { method: "POST" }).catch(() => void 0);
  setToken("");
  setStoredUser(null);
}

/**
 * 自助注册。凭注册码准入 —— 后端 /api/assets 对任何已登录用户开放,
 * 没有码的话谁都能看到客户的设备台账。
 *
 * 成功后后端直接下发会话,不用再手动登一次。
 */
export async function register(input: {
  username: string;
  displayName: string;
  password: string;
  code: string;
}): Promise<LoginResult> {
  const body = await api<{
    token: string;
    user: CurrentUser;
    perms?: string[];
  }>("/api/auth/register", {
    method: "POST",
    body: JSON.stringify(input),
  });
  setToken(body.token);
  const user: CurrentUser = { ...body.user, perms: body.perms };
  setStoredUser(user);
  return { user };
}

/**
 * 改自己的密码。
 *
 * 【后端改完会踢掉所有会话,包括当前这个】所以这里成功后必须把本地登录态
 * 一起清掉、回登录页。不清的话页面看着还登着,下一个请求才 401 ——
 * 用户会以为"改完密码就坏了"。
 */
export async function changeMyPassword(
  oldPassword: string,
  newPassword: string,
): Promise<void> {
  await api("/api/auth/me/password", {
    method: "POST",
    body: JSON.stringify({ oldPassword, newPassword }),
  });
}
