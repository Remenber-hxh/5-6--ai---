// 聊天历史:localStorage 持久化,保留 7 天(口径与旧版一致)
const KEY = "inspectai_agent_history_v1";
const TTL_MS = 7 * 24 * 3600 * 1000;

export interface ChatSession {
  id: string;
  ts: number; // 最后活跃时间
  title: string; // 首条用户提问
  msgs: unknown[]; // AgentHome 的 Msg 列表(原样存取)
}

function readAll(): ChatSession[] {
  try {
    const raw = localStorage.getItem(KEY);
    const list = raw ? (JSON.parse(raw) as ChatSession[]) : [];
    const fresh = list.filter((s) => Date.now() - s.ts < TTL_MS);
    if (fresh.length !== list.length) writeAll(fresh);
    return fresh;
  } catch {
    return [];
  }
}

function writeAll(list: ChatSession[]) {
  try {
    localStorage.setItem(KEY, JSON.stringify(list.slice(0, 50)));
  } catch {
    /* 存储满则放弃,不影响对话 */
  }
}

export function listSessions(): ChatSession[] {
  return readAll().sort((a, b) => b.ts - a.ts);
}

export function saveSession(s: ChatSession) {
  const list = readAll().filter((x) => x.id !== s.id);
  list.unshift(s);
  writeAll(list);
}

export function removeSession(id: string) {
  writeAll(readAll().filter((x) => x.id !== id));
}
