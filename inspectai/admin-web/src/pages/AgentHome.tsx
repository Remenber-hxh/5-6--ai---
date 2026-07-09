import { DownloadOutlined, HistoryOutlined, PlusOutlined, SendOutlined } from "@ant-design/icons";
import { Button, DatePicker, Drawer, Empty, Input, List, Select, Tag, message as antdMsg } from "antd";
import dayjs from "dayjs";
import { motion } from "motion/react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  ActionProposal,
  AssetEntry,
  ChatSource,
  EngineeringTask,
  UserEntry,
  act,
  chat,
  dailyReport,
  extractActionProposal,
  listAssets,
  listChangeRequests,
  listRecords,
  listTasks,
  listUsers,
  weeklyReport,
} from "../api/mgmt";
import CountUp from "../components/CountUp";
import DotField from "../components/DotField";
import { ChatSession, listSessions, removeSession, saveSession } from "../lib/history";
import { ensureLive2d, live2dHide, live2dSay, live2dShow } from "../lib/live2d";
import { InspectionRecord, fmtTime, recordBusinessStatus, statusTagColor } from "../lib/status";
import { buildDailyHtml, buildWeeklyHtml, exportWordDoc } from "../lib/wordExport";
import { useAuth } from "../store/auth";
import "./agent.css";

interface Msg {
  id: string;
  role: "user" | "ai";
  text: string;
  ts?: number;
  sources?: ChatSource[];
  proposal?: ActionProposal | null;
  proposalDone?: string; // 派发结果文案
  proposalTaskId?: string; // 派发后生成的复查任务 id(用于「查看任务详情」跳转)
  proposalDismissed?: boolean;
  report?: any; // 周报/日报数据(结构化渲染)
  reportKind?: "weekly" | "daily";
  navJump?: { path: string; label: string } | null; // 「前往 X」跳转 chip
}

// 预设场景(问句/图标/色调与旧版 AGENT_ACTS 一致;page = 回答后附「前往 X」)
const PRESETS: { q: string; label: string; page?: string; tone: string; svg: string }[] = [
  { q: "查看本周巡检计划", label: "查看本周巡检计划", page: "/plan", tone: "#7fb0ff", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="4.5" width="18" height="17" rx="2"/><path d="M16 2.5v4M8 2.5v4M3 9.5h18"/></svg>' },
  { q: "目前有哪些待审批工单需要处理？", label: "查询待审批工单", page: "/approval", tone: "#c4a7ff", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M9 13h6M9 17h4"/></svg>' },
  { q: "最近 30 天有哪些设备需要重点关注？", label: "定位异常设备", page: "/data", tone: "#3ee6b4", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><circle cx="12" cy="12" r="8"/><circle cx="12" cy="12" r="3"/><path d="M12 1v3M12 20v3M1 12h3M20 12h3"/></svg>' },
  { q: "分析本月异常趋势", label: "分析本月异常趋势", page: "/data", tone: "#ffc46b", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M3 3v18h18"/><path d="M7 14l4-4 3 3 5-6"/></svg>' },
  { q: "生成本周巡检周报", label: "生成本周周报", tone: "#7fb0ff", svg: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><path d="M14 2v6h6M8 13h8M8 17h5M8 9h2"/></svg>' },
];

const WEEKLY_RE = /周报|周度报告|本周报告/;
const DAILY_RE = /日报|今日(汇报|情况|报告|总结|管理)/;

// @页面 显式提及 → 跳转 chip(与旧版 navIntent 口径一致:只认 @,不按关键词猜)
const NAV_PAGES = [
  { path: "/approval", label: "审批中心" },
  { path: "/ledger", label: "资产台账" },
  { path: "/data", label: "数据看板" },
  { path: "/record", label: "巡检记录" },
  { path: "/plan", label: "巡检计划" },
  { path: "/users", label: "用户与权限" },
  { path: "/logs", label: "操作日志" },
  { path: "/system", label: "系统管理" },
];

function navIntent(text: string): { path: string; label: string } | null {
  const m = String(text || "").match(/@([一-龥A-Za-z]{2,8})/);
  if (!m) return null;
  const term = m[1];
  for (const n of NAV_PAGES) {
    if (n.label === term || n.label.includes(term) || term.includes(n.label)) return n;
  }
  return null;
}

function presetJump(q: string): { path: string; label: string } | null {
  const hit = PRESETS.find((p) => p.q === q && p.page);
  if (!hit) return null;
  const nav = NAV_PAGES.find((n) => n.path === hit.page);
  return nav || null;
}

let seq = 0;
const mid = () => `m_${Date.now()}_${++seq}`;

function greeting(): string {
  const h = new Date().getHours();
  if (h < 6) return "凌晨好";
  if (h < 12) return "上午好";
  if (h < 14) return "中午好";
  if (h < 18) return "下午好";
  return "晚上好";
}

export default function AgentHome() {
  const nav = useNavigate();
  const { user } = useAuth();
  const displayName = user?.displayName || user?.username || "访客";
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const [pendingCrs, setPendingCrs] = useState(0);
  const [users, setUsers] = useState<UserEntry[]>([]);
  const [histOpen, setHistOpen] = useState(false);
  const [histTick, setHistTick] = useState(0);
  const [sessionId, setSessionId] = useState(() => mid());
  const bodyRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<any>(null);

  const [petOn, setPetOn] = useState(() => localStorage.getItem("inspectai_live2d") !== "off");

  useEffect(() => {
    listAssets().then(setAssets).catch(() => void 0);
    // 加载记录:依据展开卡里「近期巡检历史列表」+ 今日快照用
    listRecords().then(setRecords).catch(() => void 0);
    // 今日快照:待整改任务 / 待审批;派发卡:责任人下拉
    listTasks().then(setTasks).catch(() => void 0);
    listChangeRequests()
      .then((rows) => setPendingCrs(rows.filter((r) => r.status === "pending").length))
      .catch(() => void 0);
    listUsers().then(setUsers).catch(() => void 0);
  }, []);

  // 今日快照:全部同源真实数据(资产/记录/任务/审批),点击直达对应页
  const snap = useMemo(() => {
    const todayKey = dayjs().format("YYYY-MM-DD");
    return {
      approvals: pendingCrs,
      abnormalAssets: assets.filter((a) => a.lastStatus === "异常" || a.lastStatus === "待复核").length,
      todayRecords: records.filter((r) => String(r.createdAt || "").slice(0, 10) === todayKey).length,
      rectifyTasks: tasks.filter((t) => t.status === "待整改").length,
    };
  }, [assets, records, tasks, pendingCrs]);

  // 看板娘:开关持久化;关闭时连引擎/模型都不加载(省 1.3MB 包 + 2.7MB 模型)
  useEffect(() => {
    if (!petOn) {
      live2dHide();
      return;
    }
    let alive = true;
    void ensureLive2d().then((ok) => {
      if (ok && alive) live2dShow();
    });
    return () => {
      alive = false;
      live2dHide();
    };
  }, [petOn]);

  function togglePet() {
    const next = !petOn;
    setPetOn(next);
    localStorage.setItem("inspectai_live2d", next ? "on" : "off");
  }

  useEffect(() => {
    if (busy) live2dSay("正在分析,请稍候…", 6000, 4);
  }, [busy]);

  useEffect(() => {
    bodyRef.current?.scrollTo({ top: bodyRef.current.scrollHeight, behavior: "smooth" });
    // 会话随消息自动落盘(7 天保留)
    if (msgs.length) {
      const firstUser = msgs.find((m) => m.role === "user");
      saveSession({
        id: sessionId,
        ts: Date.now(),
        title: (firstUser?.text || "对话").slice(0, 30),
        msgs,
      });
    }
  }, [msgs, busy, sessionId]);

  function newChat() {
    setMsgs([]);
    setSessionId(mid());
  }

  function restore(s: ChatSession) {
    setMsgs(s.msgs as Msg[]);
    setSessionId(s.id);
    setHistOpen(false);
  }

  async function send(question: string) {
    const q = question.trim();
    if (!q || busy) return;
    setInput("");
    setMsgs((m) => [...m, { id: mid(), role: "user", text: q, ts: Date.now() }]);
    setBusy(true);
    try {
      if (WEEKLY_RE.test(q)) {
        const d = await weeklyReport();
        setMsgs((m) => [
          ...m,
          { id: mid(), role: "ai", text: d.summary || "本周周报已生成。", report: d, reportKind: "weekly", ts: Date.now() },
        ]);
      } else if (DAILY_RE.test(q)) {
        const d = await dailyReport();
        setMsgs((m) => [
          ...m,
          { id: mid(), role: "ai", text: d.summary || "今日日报已生成。", report: d, reportKind: "daily", ts: Date.now() },
        ]);
      } else {
        const res = await chat(q, msgsToHistory(msgs));
        const { text, proposal } = extractActionProposal(res.reply || "AI 没有给出回复。");
        setMsgs((m) => [
          ...m,
          {
            id: mid(),
            role: "ai",
            text,
            proposal,
            sources: res.sources || [],
            navJump: navIntent(q) || presetJump(q),
            ts: Date.now(),
          },
        ]);
      }
    } catch (e) {
      setMsgs((m) => [
        ...m,
        { id: mid(), role: "ai", text: e instanceof Error ? e.message : "请求失败,请稍后再试", ts: Date.now() },
      ]);
    } finally {
      setBusy(false);
      // 回答落地后重新聚焦输入框,连续提问不用再点
      setTimeout(() => inputRef.current?.focus?.(), 50);
    }
  }

  async function dispatchProposal(msg: Msg, extra: Record<string, string> = {}) {
    const p = msg.proposal;
    if (!p) return;
    const asset = assets.find(
      (a) => a.assetKey === p.asset || a.assetName === p.asset || a.id === p.asset,
    );
    if (!asset) {
      antdMsg.warning(`未找到设备「${p.asset}」,无法派发`);
      return;
    }
    const params: Record<string, string> = {};
    if (p.assignee) params.assignee = p.assignee;
    if (p.dueAt) params.dueAt = p.dueAt;
    for (const [k, v] of Object.entries(extra)) if (v) params[k] = v;
    try {
      const res = await act(p.type, asset.id, params);
      const detail = res.task
        ? `(责任人 ${res.task.assigneeName || "—"} · 截止 ${res.task.dueAt || "—"})`
        : "";
      setMsgs((m) =>
        m.map((x) =>
          x.id === msg.id
            ? { ...x, proposalDone: (res.message || "复查任务已派发") + detail, proposalTaskId: res.task?.id || "" }
            : x,
        ),
      );
    } catch (e) {
      antdMsg.error(e instanceof Error ? e.message : "派发失败");
    }
  }

  return (
    <div style={st.page} className={msgs.length ? "agent-chatting" : ""}>
      <DotField active={msgs.length > 0} busy={busy} />
      {msgs.length === 0 && (
        <motion.div
          className="agent-hero"
          initial={{ opacity: 0, y: 18 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ duration: 0.5 }}
        >
          <h1 className="agent-hero-title">
            <span className="brand">
              <span className="greet">{greeting()},</span>
              <span className="name">{displayName}</span>
            </span>
          </h1>
          {/* 今日快照:同源真实数据,点击直达对应页 */}
          <motion.div
            className="agent-snap"
            initial={{ opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ delay: 0.25, duration: 0.4 }}
          >
            {[
              { label: "待审批", value: snap.approvals, path: "/approval", warn: snap.approvals > 0 },
              { label: "异常设备", value: snap.abnormalAssets, path: "/ledger", warn: snap.abnormalAssets > 0 },
              { label: "今日巡检", value: snap.todayRecords, path: "/record", warn: false },
              { label: "待整改", value: snap.rectifyTasks, path: "/plan", warn: snap.rectifyTasks > 0 },
            ].map((x) => (
              <button key={x.label} className="agent-snap-chip" onClick={() => nav(x.path)}>
                <i className={x.warn ? "warn" : ""} />
                {x.label}
                <b className={x.warn ? "warn" : ""}>
                  <CountUp value={x.value} />
                </b>
              </button>
            ))}
          </motion.div>
        </motion.div>
      )}
      <div ref={bodyRef} className="agent-body" style={st.body}>
        {msgs.map((m) => (
          <MsgView
            key={m.id}
            m={m}
            assets={assets}
            records={records}
            users={users}
            onJump={(path) => nav(path)}
            onDispatch={dispatchProposal}
            onDismiss={(x) =>
              setMsgs((list) => list.map((y) => (y.id === x.id ? { ...y, proposalDismissed: true } : y)))
            }
          />
        ))}
        {busy && (
          <div className="agent-typing" aria-label="智巡 Agent 思考中">
            <span className="radar" />
            <span className="agent-typing-label">思考中</span>
            <i />
            <i />
            <i />
          </div>
        )}
      </div>
      {msgs.length === 0 && (
        <motion.div
          style={st.presetsRow}
          initial={{ opacity: 0, y: 10 }}
          animate={{ opacity: 1, y: 0 }}
          transition={{ delay: 0.15 }}
        >
          {PRESETS.map((p, i) => (
            <motion.button
              key={p.q}
              initial={{ opacity: 0, y: 10 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ delay: 0.2 + i * 0.06 }}
              style={st.preset}
              onClick={() => send(p.q)}
            >
              <span
                className="preset-ico"
                style={{ width: 17, height: 17, display: "inline-flex", flex: "none" }}
                dangerouslySetInnerHTML={{ __html: p.svg }}
              />
              {p.label}
            </motion.button>
          ))}
        </motion.div>
      )}
      <div style={st.composer} className="agent-composer">
        <Input
          ref={inputRef}
          className="agent-input"
          size="large"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onPressEnter={() => send(input)}
          placeholder="请输入您的问题,或直接 @相关页面,如:@巡检记录"
          variant="borderless"
          disabled={busy}
        />
        <Button
          type="primary"
          size="large"
          icon={<SendOutlined />}
          onClick={() => send(input)}
          loading={busy}
        />
      </div>
      <div style={st.foot}>
        {/* 免责声明绝对定位水平居中;操作链接靠右,两者互不挤占 */}
        <span style={st.footDisclaimer}>AI 生成的内容仅供参考,请以实际数据为准</span>
        <span style={{ display: "flex", gap: 14, marginLeft: "auto" }}>
          <a style={st.footLink} onClick={togglePet}>
            {petOn ? "隐藏看板娘" : "显示看板娘"}
          </a>
          <a style={st.footLink} onClick={() => setHistOpen(true)}>
            <HistoryOutlined /> 历史对话
          </a>
          <a style={st.footLink} onClick={newChat}>
            <PlusOutlined /> 清空对话
          </a>
        </span>
      </div>
      <Drawer
        title="历史对话(保留 7 天)"
        open={histOpen}
        width={360}
        onClose={() => setHistOpen(false)}
      >
        {listSessions().length === 0 ? (
          <Empty description="暂无历史对话" />
        ) : (
          <List
            key={histTick}
            dataSource={listSessions()}
            renderItem={(s) => (
              <List.Item
                style={{ cursor: "pointer" }}
                onClick={() => restore(s)}
                actions={[
                  <a
                    key="del"
                    style={{ color: "#d4380d" }}
                    onClick={(e) => {
                      e.stopPropagation();
                      removeSession(s.id);
                      setHistTick((t) => t + 1);
                    }}
                  >
                    删除
                  </a>,
                ]}
              >
                <List.Item.Meta
                  title={s.title}
                  description={new Date(s.ts).toLocaleString("zh-CN", {
                    month: "2-digit",
                    day: "2-digit",
                    hour: "2-digit",
                    minute: "2-digit",
                  })}
                />
              </List.Item>
            )}
          />
        )}
      </Drawer>
    </div>
  );
}

function msgsToHistory(msgs: Msg[]) {
  return msgs.slice(-6).map((m) => ({ role: m.role === "user" ? "user" : "ai", text: m.text }));
}

// AI 回复只允许一处 **加粗**(提示词约束),轻量渲染即可,不引 markdown 库
function renderBold(line: string) {
  const parts = line.split(/\*\*(.+?)\*\*/g);
  return parts.map((p, i) =>
    i % 2 === 1 ? (
      <b key={i} style={{ color: "#eef6f4" }}>
        {p}
      </b>
    ) : (
      p
    ),
  );
}

// 依据展开卡:资产→近期巡检历史列表;记录→状态+总结;规范→说明。尾部融合跳转按钮(复刻旧版 srcAssetHTML/srcRecordHTML)
function SourceDetail({
  src,
  assets,
  records,
  onJump,
}: {
  src: ChatSource;
  assets: AssetEntry[];
  records: InspectionRecord[];
  onJump: (path: string) => void;
}) {
  if (src.type === "asset") {
    const a = assets.find((x) => x.id === src.assetId);
    const hist = a
      ? records
          .filter((r) => r.pointId === a.pointId)
          .sort((x, y) => String(y.createdAt || "").localeCompare(String(x.createdAt || "")))
          .slice(0, 5)
      : [];
    return (
      <div style={st.srcDetail}>
        <b>{src.title || "异常史"}</b>
        <p style={st.srcSub}>
          {a?.assetName || src.summary || ""}
          {a ? ` · 近期巡检 ${hist.length} 条` : ""}
        </p>
        {hist.map((r) => {
          const s = recordBusinessStatus(r);
          return (
            <div key={r.id} style={st.srcHistRow}>
              <span style={{ color: "#8aa3ad", flex: "none" }}>{fmtTime(r.createdAt)}</span>
              <Tag color={statusTagColor(s)} style={{ margin: 0 }}>
                {s}
              </Tag>
              <span style={st.srcHistSum}>{(r.aiSummary || r.report || "-").slice(0, 26)}</span>
            </div>
          );
        })}
        {!hist.length && <p style={st.srcMuted}>暂无历史记录</p>}
        {src.assetId && (
          <button className="agent-cta" onClick={() => onJump(`/ledger?focus=${encodeURIComponent(src.assetId!)}`)}>
            打开资产台账 →
          </button>
        )}
      </div>
    );
  }
  if (src.type === "record") {
    const r = records.find((x) => x.id === src.recordId);
    const s = r ? recordBusinessStatus(r) : "";
    return (
      <div style={st.srcDetail}>
        <b>
          {src.title || "巡检记录"}
          {s && (
            <Tag color={statusTagColor(s)} style={{ marginLeft: 8 }}>
              {s}
            </Tag>
          )}
        </b>
        {r && <p style={st.srcSub}>{fmtTime(r.createdAt)}</p>}
        <p style={{ margin: "6px 0 0", whiteSpace: "pre-wrap" }}>
          {r ? r.aiSummary || r.report || "暂无总结" : src.detail || src.summary || "记录未在当前已加载数据中"}
        </p>
        {src.recordId && (
          <button className="agent-cta" onClick={() => onJump(`/record?focus=${encodeURIComponent(src.recordId!)}`)}>
            打开完整记录 →
          </button>
        )}
      </div>
    );
  }
  return (
    <div style={st.srcDetail}>
      <b>{src.title}</b>
      {(src.detail || src.summary) && (
        <p style={{ margin: "6px 0 0", whiteSpace: "pre-wrap" }}>{src.detail || src.summary}</p>
      )}
    </div>
  );
}

function MsgView({
  m,
  assets,
  records,
  users,
  onJump,
  onDispatch,
  onDismiss,
}: {
  m: Msg;
  assets: AssetEntry[];
  records: InspectionRecord[];
  users: UserEntry[];
  onJump: (path: string) => void;
  onDispatch: (m: Msg, extra: Record<string, string>) => void;
  onDismiss: (m: Msg) => void;
}) {
  const [expanded, setExpanded] = useState<ChatSource | null>(null);
  const [assignee, setAssignee] = useState("");
  const [dueAt, setDueAt] = useState("");
  const user = m.role === "user";

  // 回复里的设备编号 / 记录号自动变链接(命中真实数据才加,避免死链)
  function linkify(text: string, keyBase: string) {
    const keys = assets
      .map((a) => a.assetKey || "")
      .filter((k) => k.length >= 3)
      .sort((a, b) => b.length - a.length);
    const pattern = [...keys.map((k) => k.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")), "ZX-[A-Z0-9-]{6,}"].join("|");
    if (!pattern) return [text];
    const re = new RegExp(`(${pattern})`, "g");
    return text.split(re).map((part, i) => {
      if (i % 2 === 0) return part;
      const asset = assets.find((a) => a.assetKey === part);
      const path = asset
        ? `/ledger?focus=${encodeURIComponent(asset.id)}`
        : `/record?focusNo=${encodeURIComponent(part)}`;
      return (
        <a key={`${keyBase}_${i}`} onClick={() => onJump(path)} style={{ color: "#5cf0c4", cursor: "pointer" }}>
          {part}
        </a>
      );
    });
  }
  return (
    <motion.div
      initial={{ opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.3 }}
      style={{ display: "flex", flexDirection: "column", alignItems: user ? "flex-end" : "flex-start" }}
    >
      <div style={user ? st.userBubble : st.aiBubble}>
        {m.text.split("\n").map((line, i) => (
          <p key={i} style={{ margin: i ? "8px 0 0" : 0 }}>
            {renderBold(line).map((node, j) =>
              typeof node === "string" && !user ? <span key={j}>{linkify(node, `${i}_${j}`)}</span> : node,
            )}
          </p>
        ))}
        {m.report &&
          (m.reportKind === "daily" ? (
            <DailyReportView d={m.report} onJump={onJump} />
          ) : (
            <WeeklyReportView d={m.report} onJump={onJump} />
          ))}
      </div>
      {m.ts && (
        <div style={{ display: "flex", gap: 10, marginTop: 4, fontSize: 11, color: "rgba(138, 163, 173, 0.6)" }}>
          <span>
            {new Date(m.ts).toLocaleTimeString("zh-CN", { hour: "2-digit", minute: "2-digit" })}
          </span>
          {!user && (
            <a
              style={{ color: "rgba(138, 163, 173, 0.75)", cursor: "pointer" }}
              onClick={() => {
                navigator.clipboard?.writeText(m.text);
                antdMsg.success("已复制回答");
              }}
            >
              复制
            </a>
          )}
        </div>
      )}
      {m.report && (
        <Button
          size="small"
          ghost
          icon={<DownloadOutlined />}
          style={{ marginTop: 8 }}
          onClick={() =>
            m.reportKind === "daily"
              ? exportWordDoc(
                  `智巡日报-${new Date().toISOString().slice(0, 10)}`,
                  "智巡 · 巡检日报",
                  buildDailyHtml(m.report),
                )
              : exportWordDoc(
                  `智巡周报-${new Date().toISOString().slice(0, 10)}`,
                  "智巡 · 本周巡检周报",
                  buildWeeklyHtml(m.report),
                )
          }
        >
          导出 Word
        </Button>
      )}
      {m.proposal && !m.proposalDone && !m.proposalDismissed && (
        <div style={st.proposal}>
          <div style={{ marginBottom: 6 }}>
            <span style={st.proposalTag}>建议动作</span> 派复检任务 · 目标设备{" "}
            <b>{m.proposal.asset}</b>
          </div>
          {m.proposal.reason && (
            <div style={{ color: "#9fb2ad", fontSize: 12, marginBottom: 8 }}>{m.proposal.reason}</div>
          )}
          <div style={{ display: "flex", gap: 8, marginBottom: 10 }}>
            <Select
              size="small"
              showSearch
              allowClear
              placeholder={m.proposal.assignee || "责任人(默认上次巡检人)"}
              value={assignee || undefined}
              onChange={(v) => setAssignee(v || "")}
              options={users
                .filter((u) => u.status !== "disabled" && u.displayName)
                .map((u) => ({ value: u.displayName!, label: `${u.displayName}${u.roleName ? ` · ${u.roleName}` : ""}` }))}
              style={{ width: 200 }}
              optionFilterProp="label"
            />
            <DatePicker
              size="small"
              placeholder={m.proposal.dueAt || "截止日期"}
              value={dueAt ? dayjs(dueAt) : null}
              onChange={(d) => setDueAt(d ? d.format("YYYY-MM-DD") : "")}
              disabledDate={(d) => d.isBefore(dayjs().startOf("day"))}
              style={{ width: 150 }}
            />
          </div>
          <div style={{ display: "flex", gap: 8 }}>
            <Button type="primary" size="small" onClick={() => onDispatch(m, { assignee, dueAt })}>
              确认派发
            </Button>
            <Button size="small" ghost onClick={() => onDismiss(m)}>
              忽略
            </Button>
          </div>
        </div>
      )}
      {m.proposalDone && (
        <div style={st.proposalDone}>
          <span>✓ {m.proposalDone}</span>
          {m.proposalTaskId && (
            <button
              className="agent-cta"
              onClick={() => onJump(`/plan?task=${encodeURIComponent(m.proposalTaskId!)}`)}
            >
              查看任务详情 →
            </button>
          )}
        </div>
      )}
      {/* 有未忽略的建议动作卡时不出「前往 X」chip(动作卡本身就是下一步,派发后也不再堆看板跳转) */}
      {m.navJump && !(m.proposal && !m.proposalDismissed) && (
        <button className="agent-cta" onClick={() => onJump(m.navJump!.path)}>
          前往{m.navJump.label} →
        </button>
      )}
      {!!m.sources?.length && (
        <div style={st.srcRow}>
          <span style={{ color: "rgba(220,233,247,0.45)", fontSize: 12 }}>依据</span>
          {m.sources.map((s, i) => (
            <button
              key={i}
              style={st.srcChip}
              title={s.summary || ""}
              onClick={() => {
                // 官方资料直接开外链;其余(记录/资产/规范)一律在答案下方原地展开,再点收起
                if (s.type === "official" && s.url) {
                  window.open(s.url, "_blank", "noopener");
                  return;
                }
                setExpanded(expanded === s ? null : s);
              }}
            >
              {s.title}
            </button>
          ))}
        </div>
      )}
      {expanded && <SourceDetail src={expanded} assets={assets} records={records} onJump={onJump} />}
    </motion.div>
  );
}

// 日报:结论 + 执行 + 异常清单的紧凑渲染(数据同 /report?type=daily)
// ===== 报告共享:板块标题 / 环比 / 记录·资产跳转链接(对话内报告用) =====
function ReportSection({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div style={{ marginTop: 12 }}>
      <div style={st.repH2}>{title}</div>
      <div style={{ overflowX: "auto" }}>{children}</div>
    </div>
  );
}
function repDelta(a?: number, b?: number) {
  const v = (a || 0) - (b || 0);
  if (v > 0) return <span style={{ color: "#ff8d7a" }}>▲ {v}</span>;
  if (v < 0) return <span style={{ color: "#3ee6b4" }}>▼ {-v}</span>;
  return <span style={{ color: "#8aa3ad" }}>持平</span>;
}
const repPct = (x?: number, digits = 1) => ((x || 0) * 100).toFixed(digits) + "%";
function RecLink({ id, no, onJump }: { id?: string; no?: string; onJump: (p: string) => void }) {
  if (!id) return <>{no || "—"}</>;
  return (
    <a style={st.repLink} onClick={() => onJump(`/record?focus=${encodeURIComponent(id)}`)}>
      {no || "查看"}
    </a>
  );
}
function AstLink({ id, name, onJump }: { id?: string; name?: string; onJump: (p: string) => void }) {
  if (!id) return <>{name || "—"}</>;
  return (
    <a style={st.repLink} onClick={() => onJump(`/ledger?focus=${encodeURIComponent(id)}`)}>
      {name || "—"}
    </a>
  );
}

// 日报:对话内全板块渲染(与旧版 buildDailyReportHtml 对齐;结论文字在气泡上方)
function DailyReportView({ d, onJump }: { d: any; onJump: (p: string) => void }) {
  const c = d.conclusion || {},
    ex = d.execution || {},
    as = d.assetStatus || {},
    rq = d.reviewQuality || {},
    cmp = d.compare || {},
    ns = d.nextStep || {};
  const riskL = (l: string) => (l === "danger" ? "高" : l === "warning" ? "中" : "低");
  const ab: any[] = d.abnormalList || [];
  const nm = d.normalSummary || {},
    items: any[] = nm.items || [];
  const rep: any[] = cmp.repeatedIssues || [];
  const focus = (ns.focusAssets || []).join("、");
  return (
    <div style={{ marginTop: 4, fontSize: 12.5 }}>
      <div>
        <span style={{ color: c.hasAbnormal ? "#ff8d7a" : "#3ee6b4", fontWeight: 600 }}>
          {c.hasAbnormal ? "今日有异常" : "今日无异常"}
        </span>
        {" · 异常 "}<b>{c.abnormalCount ?? 0}</b>
        {" · 待处理 "}<b>{c.pendingCount ?? 0}</b>
        {" · 今日闭环 "}<b>{c.closedCount ?? 0}</b>
      </div>
      <ReportSection title="巡检执行(任务)">
        <table style={st.table}>
          <thead><tr>{["计划", "已完成", "进行中", "未开始", "逾期", "完成率"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
          <tbody><tr>
            <td style={st.td}>{ex.plan ?? 0}</td><td style={st.td}>{ex.done ?? 0}</td><td style={st.td}>{ex.processing ?? 0}</td>
            <td style={st.td}>{ex.notStarted ?? 0}</td><td style={st.td}>{ex.overdue ?? 0}</td><td style={st.td}>{repPct(ex.completeRate, 0)}</td>
          </tr></tbody>
        </table>
      </ReportSection>
      <ReportSection title="资产 / 记录状态(今日)">
        <table style={st.table}>
          <thead><tr>{["今日巡检", "正常", "异常", "待复核", "需补图", "人工填写"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
          <tbody><tr>
            <td style={st.td}>{as.inspected ?? 0}</td><td style={st.td}>{as.normal ?? 0}</td><td style={st.td}>{as.abnormal ?? 0}</td>
            <td style={st.td}>{as.pendingReview ?? 0}</td><td style={st.td}>{as.needRetake ?? 0}</td><td style={st.td}>{as.manualFill ?? 0}</td>
          </tr></tbody>
        </table>
      </ReportSection>
      <ReportSection title="人工复核质量">
        <table style={st.table}><tbody>
          <tr><td style={st.td}>AI 识别成功</td><td style={st.td}>{rq.aiSuccess ?? 0}</td><td style={st.td}>人工修正字段</td><td style={st.td}>{rq.manualEdits ?? 0}</td></tr>
          <tr><td style={st.td}>低置信字段</td><td style={st.td}>{rq.lowConf ?? 0}</td><td style={st.td}>补图次数</td><td style={st.td}>{rq.retakes ?? 0}</td></tr>
          <tr><td style={st.td}>未看图确认</td><td style={st.td}>{rq.noPhotoConfirm ?? 0}</td><td style={st.td}>需主管复核</td><td style={st.td}>{rq.needSupervisor ?? 0}</td></tr>
        </tbody></table>
      </ReportSection>
      <ReportSection title="较昨日">
        <table style={st.table}><tbody><tr>
          <td style={st.td}>巡检数变化</td><td style={st.td}>{repDelta(cmp.recordDelta ?? 0, 0)}</td>
          <td style={st.td}>异常数变化</td><td style={st.td}>{repDelta(cmp.abnormalDelta ?? 0, 0)}</td>
        </tr></tbody></table>
        {rep.length > 0 && (
          <p style={st.repMuted}>重复异常:{rep.slice(0, 4).map((x) => (x.assetName || "") + (x.fieldLabel ? " · " + x.fieldLabel : "")).join(";")}</p>
        )}
      </ReportSection>
      <ReportSection title="下一步">
        <p style={st.repMuted}>待办转入 <b>{ns.carryOver ?? 0}</b> 项 · 待审批 <b>{ns.approvals ?? 0}</b> 项{focus ? ` · 重点盯:${focus}` : ""}</p>
      </ReportSection>
      <ReportSection title={`异常处理清单(${ab.length})`}>
        {ab.length ? (
          <table style={st.table}>
            <thead><tr>{["点位", "异常字段", "值", "风险", "责任人", "截止", "状态", "记录号"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
            <tbody>{ab.map((x, i) => (
              <tr key={i}>
                <td style={st.td}>{x.point}</td><td style={st.td}>{x.field}</td><td style={st.td}>{x.value}</td><td style={st.td}>{riskL(x.risk)}</td>
                <td style={st.td}>{x.assignee}</td><td style={st.td}>{x.dueAt || "—"}</td><td style={st.td}>{x.status}</td>
                <td style={st.td}><RecLink id={x.recordId} no={x.recordNo} onJump={onJump} /></td>
              </tr>
            ))}</tbody>
          </table>
        ) : <p style={st.repMuted}>今日无异常 / 待处理记录。</p>}
      </ReportSection>
      <ReportSection title={`正常记录摘要(${nm.count ?? 0})`}>
        {items.length ? (
          <table style={st.table}>
            <thead><tr>{["点位", "模板", "巡检人", "提交"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
            <tbody>{items.slice(0, 12).map((x: any, i: number) => (
              <tr key={i}><td style={st.td}>{x.point}</td><td style={st.td}>{x.template}</td><td style={st.td}>{x.inspector}</td><td style={st.td}>{x.submittedAt}</td></tr>
            ))}</tbody>
          </table>
        ) : <p style={st.repMuted}>今日暂无正常记录。</p>}
      </ReportSection>
    </div>
  );
}

// 周报:对话内全板块渲染(与旧版 buildWeeklyReportHtml 对齐;结论文字在气泡上方)
function WeeklyReportView({ d, onJump }: { d: any; onJump: (p: string) => void }) {
  const m = d.metrics || {};
  const risk: any[] = (d.topRisk || []).slice(0, 5);
  const ic: any[] = d.issueClosure || [];
  const qs = d.qualitySummary || {};
  const na: any[] = d.nextActions || [];
  const srcs = ((d.traceability || {}).sources || []).join("、");
  const riskLabel = (l: string) =>
    l === "danger" ? "高风险" : l === "warning" ? "需关注" : l === "repair" ? "维修中" : "正常";
  const mrow = (label: string, a?: number, b?: number) => (
    <tr key={label}>
      <td style={st.td}>{label}</td><td style={st.td}>{a ?? 0}</td><td style={st.td}>{b ?? 0}</td><td style={st.td}>{repDelta(a, b)}</td>
    </tr>
  );
  const qrow = (label: string, v?: number, j?: string) => (
    <tr key={label}><td style={st.td}>{label}</td><td style={st.td}>{v ?? 0}</td><td style={st.td}>{j}</td></tr>
  );
  return (
    <div style={{ marginTop: 4, fontSize: 12.5 }}>
      <ReportSection title={`二、核心指标(${(d.rangeStart || "").slice(0, 10)} ~ ${(d.rangeEnd || "").slice(0, 10)})`}>
        <table style={st.table}>
          <thead><tr>{["指标", "本周", "上周", "环比"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
          <tbody>
            {mrow("巡检记录数", m.recordRecent, m.recordPrev)}
            {mrow("巡检资产数", m.assetInspectedRecent, m.assetInspectedPrev)}
            {mrow("异常记录数", m.abnormalRecent, m.abnormalPrev)}
            {mrow("已闭环数", m.closedRecent, m.closedPrev)}
            <tr><td style={st.td}>待复核 / 待审批</td><td style={st.td} colSpan={3}>{m.pendingReviews ?? 0} 条 / {m.pendingApprovals ?? 0} 条</td></tr>
            <tr><td style={st.td}>需补图 / 未看图确认率</td><td style={st.td} colSpan={3}>{m.needRetake ?? 0} 条 / {repPct(m.lazyConfirmRate)}</td></tr>
          </tbody>
        </table>
      </ReportSection>
      {risk.length > 0 && (
        <ReportSection title="三、重点关注资产">
          <table style={st.table}>
            <thead><tr>{["资产", "风险", "主要问题", "AI 依据", "建议动作"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
            <tbody>{risk.map((a, i) => (
              <tr key={i}>
                <td style={st.td}><AstLink id={a.assetId} name={a.assetName} onJump={onJump} /></td>
                <td style={st.td}>{riskLabel(a.riskLevel)}</td><td style={st.td}>{a.mainIssue}</td>
                <td style={st.td}>{a.aiBasis}</td><td style={st.td}>{a.suggestedAction}</td>
              </tr>
            ))}</tbody>
          </table>
        </ReportSection>
      )}
      <ReportSection title={`四、异常闭环情况(${ic.length})`}>
        {ic.length ? (
          <table style={st.table}>
            <thead><tr>{["异常项", "发现", "来源记录", "状态", "责任人", "截止", "处理建议"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
            <tbody>{ic.map((x, i) => (
              <tr key={i}>
                <td style={st.td}>{x.issueName}{x.value ? "=" + x.value : ""}</td>
                <td style={st.td}>{x.foundAt}</td>
                <td style={st.td}><RecLink id={x.recordId} no={x.recordNo} onJump={onJump} /></td>
                <td style={st.td}>{x.status}</td><td style={st.td}>{x.assignee}</td>
                <td style={st.td}>{x.dueAt || "—"}</td><td style={st.td}>{x.suggestion}</td>
              </tr>
            ))}</tbody>
          </table>
        ) : <p style={st.repMuted}>本周无未闭环异常。</p>}
      </ReportSection>
      <ReportSection title="五、巡检质量与 AI 协同">
        <table style={st.table}>
          <thead><tr>{["维度", "本周", "风险判断"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
          <tbody>
            {qrow("AI 识别成功记录", qs.aiSuccess, "正常")}
            {qrow("人工修正字段", qs.manualEdits, "字段规则可优化")}
            {qrow("低置信度字段", qs.lowConfidenceFields, "需补参考图")}
            {qrow("补图次数", qs.retakes, "拍摄规范需加强")}
            {qrow("未看图确认", qs.noPhotoConfirm, (qs.noPhotoConfirm || 0) > 0 ? "需主管抽查" : "良好")}
            {qrow("重复异常字段", qs.repeatedFieldIssues, "应纳入重点规则")}
          </tbody>
        </table>
      </ReportSection>
      {na.length > 0 && (
        <ReportSection title="六、下周工作安排">
          <table style={st.table}>
            <thead><tr>{["工作项", "对象", "负责人", "时间", "触发依据"].map((x) => <th key={x} style={st.th}>{x}</th>)}</tr></thead>
            <tbody>{na.map((x, i) => (
              <tr key={i}><td style={st.td}>{x.workItem}</td><td style={st.td}>{x.target}</td><td style={st.td}>{x.assignee}</td><td style={st.td}>{x.time}</td><td style={st.td}>{x.trigger}</td></tr>
            ))}</tbody>
          </table>
        </ReportSection>
      )}
      <ReportSection title="七、数据溯源">
        <p style={st.repMuted}>本周报基于{srcs || "系统巡检数据"}自动生成;每条异常可回溯到记录编号、资产编号与图片证据,点击上方记录号/资产名即可跳转查看。</p>
      </ReportSection>
    </div>
  );
}

const st: Record<string, React.CSSProperties> = {
  page: {
    position: "relative",
    height: "calc(100vh - 64px)",
    display: "flex",
    flexDirection: "column",
    overflow: "hidden",
    background: "radial-gradient(120% 80% at 50% 120%, #0a2433 0%, #071521 38%, #050b12 70%)",
    margin: -20,
  },
  foot: {
    position: "relative",
    zIndex: 1,
    display: "flex",
    justifyContent: "space-between",
    alignItems: "center",
    width: "100%",
    maxWidth: 980,
    margin: "0 auto",
    padding: "0 4px 14px",
  },
  footLink: { color: "#8aa3ad", fontSize: 12, cursor: "pointer" },
  footDisclaimer: {
    position: "absolute",
    left: "50%",
    transform: "translateX(-50%)",
    color: "#7e94a0",
    fontSize: 12,
    whiteSpace: "nowrap",
    pointerEvents: "none",
  },
  jumpChip: {
    marginTop: 8,
    padding: "7px 14px",
    border: 0,
    borderRadius: 8,
    background: "linear-gradient(135deg, #3ee6b4, #18c597)",
    color: "#04241b",
    fontSize: 13,
    fontWeight: 600,
    cursor: "pointer",
  },
  body: {
    position: "relative",
    zIndex: 1,
    flex: 1,
    overflowY: "auto",
    padding: 24,
    width: "100%",
    maxWidth: 1000,
    margin: "0 auto",
    display: "flex",
    flexDirection: "column",
    // 短对话贴底:靠 .agent-body>:first-child{margin-top:auto}(见 agent.css)。
    // 不用 justify-content:flex-end——它在内容超高时会把顶部溢出且无法滚动(长周报滚不动的根因)。
    gap: 12,
  },
  presetsRow: {
    position: "relative",
    zIndex: 1,
    display: "flex",
    flexWrap: "wrap",
    justifyContent: "center",
    gap: 10,
    width: "100%",
    maxWidth: 980,
    margin: "0 auto 14px",
    padding: "0 4px",
  },
  preset: {
    display: "inline-flex",
    alignItems: "center",
    gap: 7,
    padding: "9px 16px",
    borderRadius: 10,
    border: "1px solid rgba(255,255,255,0.14)",
    background: "rgba(255,255,255,0.06)",
    color: "#dfeae8",
    cursor: "pointer",
    fontSize: 13,
  },
  userBubble: {
    background: "linear-gradient(135deg, #18c597, #10a37f)",
    color: "#04241b",
    padding: "10px 16px",
    borderRadius: 12,
    maxWidth: "78%",
    fontSize: 14,
  },
  aiBubble: {
    background: "linear-gradient(180deg, rgba(255,255,255,0.09), rgba(255,255,255,0.045))",
    border: "1px solid rgba(255,255,255,0.12)",
    backdropFilter: "blur(16px)",
    boxShadow: "0 10px 34px rgba(0,20,16,0.34)",
    color: "#eef5f3",
    padding: "14px 18px",
    borderRadius: 14,
    maxWidth: "88%",
    fontSize: 14,
    lineHeight: 1.75,
  },
  typing: { color: "#8aa3ad", fontSize: 13, paddingLeft: 4 },
  composer: {
    position: "relative",
    zIndex: 1,
    display: "flex",
    gap: 10,
    alignItems: "center",
    width: "100%",
    maxWidth: 980,
    margin: "0 auto 8px",
    padding: 8,
    borderRadius: 16,
    background: "rgba(255,255,255,0.07)",
    border: "1px solid rgba(255,255,255,0.14)",
    backdropFilter: "blur(14px)",
  },
  proposal: {
    marginTop: 8,
    padding: "12px 16px",
    borderRadius: 12,
    background: "linear-gradient(180deg, rgba(62,230,180,0.09), rgba(255,255,255,0.035))",
    border: "1px solid rgba(62,230,180,0.2)",
    borderLeft: "2px solid rgba(62,230,180,0.55)",
    boxShadow: "0 8px 24px rgba(0,20,16,0.28)",
    color: "#eef5f3",
    fontSize: 13,
    maxWidth: "88%",
  },
  proposalTag: {
    background: "rgba(62,230,180,0.15)",
    color: "#3ee6b4",
    borderRadius: 6,
    padding: "1px 8px",
    fontSize: 12,
    marginRight: 6,
  },
  proposalDone: {
    marginTop: 8,
    color: "#3ee6b4",
    fontSize: 13,
    display: "flex",
    flexDirection: "column",
    alignItems: "flex-start",
    gap: 2,
  },
  srcRow: { display: "flex", gap: 8, flexWrap: "wrap", alignItems: "center", marginTop: 9 },
  srcChip: {
    padding: "5px 10px",
    borderRadius: 8,
    border: "1px solid rgba(255,255,255,0.14)",
    background: "rgba(255,255,255,0.06)",
    color: "rgba(226,240,236,0.85)",
    cursor: "pointer",
    fontSize: 12,
    maxWidth: 240,
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  srcDetail: {
    marginTop: 8,
    padding: "10px 12px",
    borderRadius: 8,
    background: "rgba(255,255,255,0.06)",
    border: "1px solid rgba(255,255,255,0.13)",
    color: "rgba(226,240,236,0.88)",
    fontSize: 12.5,
    lineHeight: 1.65,
    maxWidth: "88%",
  },
  srcOpen: {
    display: "inline-block",
    marginTop: 10,
    padding: "6px 12px",
    border: "1px solid rgba(62,230,180,0.32)",
    borderRadius: 8,
    background: "rgba(62,230,180,0.08)",
    color: "#7effd2",
    fontSize: 12.5,
    fontWeight: 600,
    cursor: "pointer",
  },
  srcSub: { margin: "4px 0 8px", color: "rgba(220,233,247,0.5)", fontSize: 12 },
  srcHistRow: {
    display: "flex",
    alignItems: "center",
    gap: 8,
    padding: "5px 0",
    fontSize: 12.5,
    borderTop: "1px solid rgba(255,255,255,0.06)",
  },
  srcHistSum: {
    color: "rgba(226,240,236,0.82)",
    overflow: "hidden",
    textOverflow: "ellipsis",
    whiteSpace: "nowrap",
  },
  srcMuted: { margin: "6px 0 0", color: "rgba(220,233,247,0.4)", fontSize: 12 },
  repH2: {
    margin: "12px 0 6px",
    color: "#7effd2",
    fontSize: 13,
    fontWeight: 600,
    borderBottom: "1px solid rgba(126,255,210,0.2)",
    paddingBottom: 4,
  },
  repLink: { color: "#5cf0c4", cursor: "pointer" },
  repMuted: { margin: "6px 0 0", color: "rgba(220,233,247,0.62)", fontSize: 12, lineHeight: 1.65 },
  table: { width: "100%", borderCollapse: "collapse" as const },
  th: {
    border: "1px solid rgba(255,255,255,0.16)",
    padding: "5px 8px",
    textAlign: "left" as const,
    background: "rgba(255,255,255,0.1)",
  },
  td: { border: "1px solid rgba(255,255,255,0.16)", padding: "5px 8px" },
};
