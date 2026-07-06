import { DownloadOutlined, HistoryOutlined, PlusOutlined, SendOutlined } from "@ant-design/icons";
import { Button, Drawer, Empty, Input, List, message as antdMsg } from "antd";
import { motion } from "motion/react";
import { useEffect, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  ActionProposal,
  AssetEntry,
  ChatSource,
  act,
  chat,
  dailyReport,
  extractActionProposal,
  listAssets,
  weeklyReport,
} from "../api/mgmt";
import { ChatSession, listSessions, saveSession } from "../lib/history";
import { buildDailyHtml, buildWeeklyHtml, exportWordDoc } from "../lib/wordExport";
import "./agent.css";

interface Msg {
  id: string;
  role: "user" | "ai";
  text: string;
  sources?: ChatSource[];
  proposal?: ActionProposal | null;
  proposalDone?: string; // 派发结果文案
  proposalDismissed?: boolean;
  report?: any; // 周报/日报数据(结构化渲染)
  reportKind?: "weekly" | "daily";
}

// 预设场景(与旧版一致,演示动线的五个入口)
const PRESETS = [
  "查看本周巡检计划",
  "目前有哪些待审批工单需要处理？",
  "最近 30 天有哪些设备需要重点关注？",
  "分析本月异常趋势",
  "生成本周巡检周报",
];

const WEEKLY_RE = /周报|周度报告|本周报告/;
const DAILY_RE = /日报|今日(汇报|情况|报告|总结|管理)/;

let seq = 0;
const mid = () => `m_${Date.now()}_${++seq}`;

export default function AgentHome() {
  const nav = useNavigate();
  const [msgs, setMsgs] = useState<Msg[]>([]);
  const [input, setInput] = useState("");
  const [busy, setBusy] = useState(false);
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [histOpen, setHistOpen] = useState(false);
  const [sessionId, setSessionId] = useState(() => mid());
  const bodyRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    listAssets().then(setAssets).catch(() => void 0);
  }, []);

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
    setMsgs((m) => [...m, { id: mid(), role: "user", text: q }]);
    setBusy(true);
    try {
      if (WEEKLY_RE.test(q)) {
        const d = await weeklyReport();
        setMsgs((m) => [
          ...m,
          { id: mid(), role: "ai", text: d.summary || "本周周报已生成。", report: d, reportKind: "weekly" },
        ]);
      } else if (DAILY_RE.test(q)) {
        const d = await dailyReport();
        setMsgs((m) => [
          ...m,
          { id: mid(), role: "ai", text: d.summary || "今日日报已生成。", report: d, reportKind: "daily" },
        ]);
      } else {
        const res = await chat(q, msgsToHistory(msgs));
        const { text, proposal } = extractActionProposal(res.reply || "AI 没有给出回复。");
        setMsgs((m) => [
          ...m,
          { id: mid(), role: "ai", text, proposal, sources: res.sources || [] },
        ]);
      }
    } catch (e) {
      setMsgs((m) => [
        ...m,
        { id: mid(), role: "ai", text: e instanceof Error ? e.message : "请求失败,请稍后再试" },
      ]);
    } finally {
      setBusy(false);
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
            ? { ...x, proposalDone: (res.message || "复查任务已派发") + detail }
            : x,
        ),
      );
    } catch (e) {
      antdMsg.error(e instanceof Error ? e.message : "派发失败");
    }
  }

  function onSourceClick(s: ChatSource) {
    if (s.type === "record" && s.recordId) nav(`/record?focus=${encodeURIComponent(s.recordId)}`);
    else if (s.type === "asset" && s.assetId) nav(`/ledger?focus=${encodeURIComponent(s.assetId)}`);
    else if (s.type === "official" && s.url) window.open(s.url, "_blank", "noopener");
  }

  return (
    <div style={st.page}>
      <div style={st.horizon} />
      <div style={st.topBar}>
        <Button size="small" ghost icon={<PlusOutlined />} onClick={newChat}>
          新对话
        </Button>
        <Button size="small" ghost icon={<HistoryOutlined />} onClick={() => setHistOpen(true)}>
          历史对话
        </Button>
      </div>
      <div ref={bodyRef} style={st.body}>
        {msgs.length === 0 && (
          <motion.div
            initial={{ opacity: 0, y: 14 }}
            animate={{ opacity: 1, y: 0 }}
            transition={{ duration: 0.5 }}
            style={st.hero}
          >
            <div style={st.hi}>
              你好,我是 <span style={{ color: "#3ee6b4" }}>智巡 Agent</span>
            </div>
            <div style={st.sub}>您身边的智能巡检助手,随时为您服务</div>
            <div style={st.presets}>
              {PRESETS.map((p, i) => (
                <motion.button
                  key={p}
                  initial={{ opacity: 0, y: 10 }}
                  animate={{ opacity: 1, y: 0 }}
                  transition={{ delay: 0.15 + i * 0.07 }}
                  style={st.preset}
                  onClick={() => send(p)}
                >
                  {p}
                </motion.button>
              ))}
            </div>
          </motion.div>
        )}
        {msgs.map((m) => (
          <MsgView
            key={m.id}
            m={m}
            onDispatch={dispatchProposal}
            onDismiss={(x) =>
              setMsgs((list) => list.map((y) => (y.id === x.id ? { ...y, proposalDismissed: true } : y)))
            }
            onSource={onSourceClick}
          />
        ))}
        {busy && (
          <div className="agent-typing" aria-label="智巡 Agent 思考中">
            <i />
            <i />
            <i />
          </div>
        )}
      </div>
      <div style={st.composer}>
        <Input
          size="large"
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onPressEnter={() => send(input)}
          placeholder="请输入您的问题,如:灭火器怎么维保"
          variant="borderless"
          style={{ color: "#e9f3f1" }}
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
            dataSource={listSessions()}
            renderItem={(s) => (
              <List.Item style={{ cursor: "pointer" }} onClick={() => restore(s)}>
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

function MsgView({
  m,
  onDispatch,
  onDismiss,
  onSource,
}: {
  m: Msg;
  onDispatch: (m: Msg, extra: Record<string, string>) => void;
  onDismiss: (m: Msg) => void;
  onSource: (s: ChatSource) => void;
}) {
  const [expanded, setExpanded] = useState<ChatSource | null>(null);
  const [assignee, setAssignee] = useState("");
  const [dueAt, setDueAt] = useState("");
  const user = m.role === "user";
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
            {renderBold(line)}
          </p>
        ))}
        {m.report &&
          (m.reportKind === "daily" ? <DailyReportView d={m.report} /> : <WeeklyReportView d={m.report} />)}
      </div>
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
            <Input
              size="small"
              placeholder={m.proposal.assignee || "责任人(默认上次巡检人)"}
              value={assignee}
              onChange={(e) => setAssignee(e.target.value)}
              style={{ width: 180 }}
            />
            <Input
              size="small"
              placeholder={m.proposal.dueAt || "截止 YYYY-MM-DD"}
              value={dueAt}
              onChange={(e) => setDueAt(e.target.value)}
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
      {m.proposalDone && <div style={st.proposalDone}>✓ {m.proposalDone}</div>}
      {!!m.sources?.length && (
        <div style={st.srcRow}>
          <span style={{ color: "rgba(220,233,247,0.45)", fontSize: 12 }}>依据</span>
          {m.sources.map((s, i) => (
            <button
              key={i}
              style={st.srcChip}
              title={s.summary || ""}
              onClick={() => (s.type === "standard" ? setExpanded(expanded === s ? null : s) : onSource(s))}
            >
              {s.title}
            </button>
          ))}
        </div>
      )}
      {expanded && (
        <div style={st.srcDetail}>
          <b>{expanded.title}</b>
          <p style={{ margin: "6px 0 0", whiteSpace: "pre-wrap" }}>{expanded.detail || "—"}</p>
        </div>
      )}
    </motion.div>
  );
}

// 日报:结论 + 执行 + 异常清单的紧凑渲染(数据同 /report?type=daily)
function DailyReportView({ d }: { d: any }) {
  const c = d.conclusion || {};
  const ex = d.execution || {};
  const ab: any[] = d.abnormalList || [];
  return (
    <div style={{ marginTop: 10, fontSize: 12.5 }}>
      <div style={{ marginBottom: 8 }}>
        <span style={{ color: c.hasAbnormal ? "#ff8d7a" : "#3ee6b4", fontWeight: 600 }}>
          {c.hasAbnormal ? "今日有异常" : "今日无异常"}
        </span>
        {" · 异常 "}
        <b>{c.abnormalCount ?? 0}</b>
        {" · 待处理 "}
        <b>{c.pendingCount ?? 0}</b>
        {" · 今日闭环 "}
        <b>{c.closedCount ?? 0}</b>
        {" · 任务 "}
        <b>
          {ex.done ?? 0}/{ex.plan ?? 0}
        </b>
      </div>
      {ab.length > 0 && (
        <table style={st.table}>
          <tbody>
            {ab.slice(0, 6).map((x, i) => (
              <tr key={i}>
                <td style={st.td}>{x.point}</td>
                <td style={st.td}>{x.field}</td>
                <td style={st.td}>{x.status}</td>
                <td style={st.td}>{x.assignee}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </div>
  );
}

// 周报:七模块的紧凑渲染(数据同 /report?type=weekly,字段与旧版一致)
function WeeklyReportView({ d }: { d: any }) {
  const m = d.metrics || {};
  const row = (label: string, a?: number, b?: number) => (
    <tr key={label}>
      <td style={st.td}>{label}</td>
      <td style={st.td}>{a ?? 0}</td>
      <td style={st.td}>{b ?? 0}</td>
    </tr>
  );
  const closure: any[] = d.issueClosure || [];
  return (
    <div style={{ marginTop: 10, fontSize: 12.5 }}>
      <table style={st.table}>
        <thead>
          <tr>
            <th style={st.th}>指标</th>
            <th style={st.th}>本周</th>
            <th style={st.th}>上周</th>
          </tr>
        </thead>
        <tbody>
          {row("巡检记录数", m.recordRecent, m.recordPrev)}
          {row("异常记录数", m.abnormalRecent, m.abnormalPrev)}
          {row("已闭环数", m.closedRecent, m.closedPrev)}
        </tbody>
      </table>
      {closure.length > 0 && (
        <>
          <div style={{ margin: "10px 0 4px", color: "#7effd2", fontWeight: 600 }}>
            异常闭环({closure.length})
          </div>
          <table style={st.table}>
            <tbody>
              {closure.slice(0, 6).map((x, i) => (
                <tr key={i}>
                  <td style={st.td}>{x.issueName}</td>
                  <td style={st.td}>{x.status}</td>
                  <td style={st.td}>{x.assignee}</td>
                  <td style={st.td}>{x.recordNo}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </>
      )}
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
  topBar: {
    position: "relative",
    zIndex: 2,
    display: "flex",
    gap: 8,
    justifyContent: "flex-end",
    padding: "12px 16px 0",
  },
  horizon: {
    position: "absolute",
    left: "50%",
    top: "58%",
    transform: "translateX(-50%)",
    width: 2200,
    height: 2200,
    borderRadius: "50%",
    background: "#050b12",
    borderTop: "2px solid rgba(170,255,230,0.85)",
    boxShadow: "0 -10px 80px 6px rgba(62,230,180,0.4)",
    pointerEvents: "none",
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
    gap: 12,
  },
  hero: { textAlign: "center", marginTop: "14vh" },
  hi: { fontSize: 36, fontWeight: 800, color: "#eef6f4" },
  sub: { marginTop: 12, color: "#8aa3ad", fontSize: 14 },
  presets: { display: "flex", gap: 12, flexWrap: "wrap", justifyContent: "center", marginTop: 48 },
  preset: {
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
    background: "rgba(255,255,255,0.08)",
    border: "1px solid rgba(255,255,255,0.14)",
    backdropFilter: "blur(14px)",
    color: "#eef5f3",
    padding: "12px 16px",
    borderRadius: 12,
    maxWidth: "88%",
    fontSize: 14,
    lineHeight: 1.7,
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
    margin: "0 auto 18px",
    padding: 8,
    borderRadius: 16,
    background: "rgba(255,255,255,0.07)",
    border: "1px solid rgba(255,255,255,0.14)",
    backdropFilter: "blur(14px)",
  },
  proposal: {
    marginTop: 8,
    padding: "10px 14px",
    borderRadius: 10,
    background: "rgba(255,255,255,0.08)",
    border: "1px solid rgba(255,255,255,0.14)",
    borderLeft: "2px solid rgba(255,255,255,0.32)",
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
  proposalDone: { marginTop: 8, color: "#3ee6b4", fontSize: 13 },
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
  table: { width: "100%", borderCollapse: "collapse" as const },
  th: {
    border: "1px solid rgba(255,255,255,0.16)",
    padding: "5px 8px",
    textAlign: "left" as const,
    background: "rgba(255,255,255,0.1)",
  },
  td: { border: "1px solid rgba(255,255,255,0.16)", padding: "5px 8px" },
};
