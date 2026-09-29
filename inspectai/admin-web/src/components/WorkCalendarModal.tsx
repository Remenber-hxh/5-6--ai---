import { LeftOutlined, RightOutlined } from "@ant-design/icons";
import { Alert, Button, Input, Modal, Popconfirm, Space, Tooltip, message } from "antd";
import dayjs from "dayjs";
import { useCallback, useEffect, useMemo, useState } from "react";

import { WorkCalendarDay, getWorkCalendar, parseHolidayNotice, saveWorkCalendar } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 工作日历:法定节假日 + 调休上班日。
 *
 * 【点一下就存】一年只有二三十个特殊日子,改一天存一天,不设"保存"按钮 ——
 * 改了没点保存就关掉,是这类页面最常见的"我明明改了"。
 * 点日子循环切换:平常 → 休 → 班 → 平常。
 *
 * 【整份通知一次录入】国务院每年公布一次,粘贴进来先认给人看,确认后覆盖那一年。
 */

const OFF_BG = "#eaf1ff";
const ON_BG = "#fff4e0";
const ON_FG = "#a8660a";
const WEEK_HEAD = ["一", "二", "三", "四", "五", "六", "日"];

type Kind = "off" | "on";

function cnDate(d: string) {
  const t = dayjs(d);
  return `${t.month() + 1}月${t.date()}日`;
}

/** 按节日名归成几行:"国庆节 10月1日–10月8日 放假 · 9月28日、10月11日 上班" */
function summarize(days: WorkCalendarDay[]) {
  const groups = new Map<string, { off: string[]; on: string[] }>();
  for (const d of [...days].sort((a, b) => a.date.localeCompare(b.date))) {
    const key = d.name || "";
    if (!groups.has(key)) groups.set(key, { off: [], on: [] });
    groups.get(key)![d.kind].push(d.date);
  }
  const ranges = (dates: string[]) => {
    const out: string[] = [];
    let start = "";
    let prev = "";
    for (const d of dates) {
      if (prev && dayjs(prev).add(1, "day").format("YYYY-MM-DD") === d) {
        prev = d;
        continue;
      }
      if (start) out.push(start === prev ? cnDate(start) : `${cnDate(start)}–${cnDate(prev)}`);
      start = prev = d;
    }
    if (start) out.push(start === prev ? cnDate(start) : `${cnDate(start)}–${cnDate(prev)}`);
    return out.join("、");
  };
  return [...groups.entries()].map(([name, g]) => ({
    name: name || "单独设置的日子",
    off: ranges(g.off),
    on: g.on.map(cnDate).join("、"),
    first: g.off[0] || g.on[0],
  }))
    .sort((a, b) => a.first.localeCompare(b.first));
}

function Summary({ days }: { days: WorkCalendarDay[] }) {
  const rows = summarize(days);
  if (!rows.length) return null;
  return (
    <div style={{ display: "grid", gap: 6, fontSize: 13 }}>
      {rows.map((r) => (
        <div key={r.name} style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
          <span style={{ minWidth: 110, fontWeight: 600, color: C.text }}>{r.name}</span>
          <span style={{ color: C.textSub }}>
            {r.off && <>{r.off} 放假</>}
            {r.off && r.on && " · "}
            {r.on && <>{r.on} 上班</>}
          </span>
        </div>
      ))}
    </div>
  );
}

export default function WorkCalendarModal({ open, onClose }: { open: boolean; onClose: () => void }) {
  const [year, setYear] = useState(() => dayjs().year());
  const [days, setDays] = useState<WorkCalendarDay[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [pasteOpen, setPasteOpen] = useState(false);
  const [text, setText] = useState("");
  const [parsed, setParsed] = useState<{ year: number; days: WorkCalendarDay[]; warnings: string[] } | null>(null);

  const load = useCallback(async (y: number) => {
    setLoading(true);
    try {
      setDays((await getWorkCalendar(y)).days || []);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load(year);
  }, [open, year, load]);

  const byDate = useMemo(() => new Map(days.map((d) => [d.date, d])), [days]);

  async function toggle(date: string) {
    if (saving) return;
    const cur = byDate.get(date);
    const next: Kind | "" = !cur ? "off" : cur.kind === "off" ? "on" : "";
    // 【挨着的日子是同一个节日就沿用名字】国庆多放一天,点一下就归到国庆里,
    // 不会在下面的清单里单独冒出一行
    let name = "";
    if (next) {
      for (const n of [-1, 1]) {
        const near = byDate.get(dayjs(date).add(n, "day").format("YYYY-MM-DD"));
        if (near && near.kind === next && near.name) {
          name = near.name;
          break;
        }
      }
    }
    const before = days;
    setDays(next ? [...days.filter((d) => d.date !== date), { date, kind: next, name }] : days.filter((d) => d.date !== date));
    setSaving(true);
    try {
      await saveWorkCalendar({ days: [{ date, kind: next, name }] });
    } catch (e) {
      setDays(before); // 没存进去就退回原样,别让界面停在一个库里没有的状态
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function runParse() {
    if (!text.trim()) return;
    try {
      const r = await parseHolidayNotice(year, text);
      setParsed({ year: r.year, days: r.days || [], warnings: r.warnings || [] });
    } catch (e) {
      message.error(e instanceof Error ? e.message : "识别失败");
    }
  }

  async function applyParsed() {
    if (!parsed) return;
    setSaving(true);
    try {
      await saveWorkCalendar({ replaceYear: parsed.year, days: parsed.days });
      message.success(`${parsed.year} 年的放假安排已录入`);
      setPasteOpen(false);
      setText("");
      setParsed(null);
      if (parsed.year !== year) setYear(parsed.year);
      else await load(year);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  const today = dayjs().format("YYYY-MM-DD");

  return (
    <Modal title="工作日历" open={open} width={940} onCancel={onClose} footer={<Button onClick={onClose}>关闭</Button>}>
      <div style={{ display: "grid", gap: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 12, flexWrap: "wrap" }}>
          <Space size={4}>
            <Button type="text" size="small" icon={<LeftOutlined />} aria-label="上一年" onClick={() => setYear(year - 1)} />
            <span style={{ fontSize: 16, fontWeight: 700, color: C.text, minWidth: 64, textAlign: "center" }}>{year} 年</span>
            <Button type="text" size="small" icon={<RightOutlined />} aria-label="下一年" onClick={() => setYear(year + 1)} />
          </Space>
          <Space size={14} style={{ fontSize: 12, color: C.textSub }}>
            <span><Mark kind="off" /> 放假</span>
            <span><Mark kind="on" /> 调休上班</span>
          </Space>
          <Button size="small" style={{ marginLeft: "auto" }} onClick={() => setPasteOpen((v) => !v)}>
            {pasteOpen ? "收起" : "粘贴放假通知"}
          </Button>
        </div>

        {pasteOpen && (
          <div style={{ display: "grid", gap: 10 }}>
            <Input.TextArea
              rows={6}
              value={text}
              onChange={(e) => {
                setText(e.target.value);
                setParsed(null);
              }}
              placeholder="国务院放假通知正文"
            />
            <Space>
              <Button size="small" disabled={!text.trim()} onClick={() => void runParse()}>
                识别
              </Button>
            </Space>
            {parsed && (
              <div style={{ display: "grid", gap: 10, padding: "12px 14px", border: `1px solid ${C.line}`, borderRadius: 8 }}>
                {parsed.warnings.length > 0 && (
                  <Alert
                    type="warning"
                    showIcon
                    message="这些地方没认出来,需要在下面的日历上手动补"
                    description={
                      <ul style={{ margin: 0, paddingLeft: 18 }}>
                        {parsed.warnings.map((w) => (
                          <li key={w}>{w}</li>
                        ))}
                      </ul>
                    }
                  />
                )}
                <Summary days={parsed.days} />
                {parsed.days.length > 0 && (
                  <Space>
                    <Popconfirm
                      title={`覆盖 ${parsed.year} 年的日历?`}
                      description={`${parsed.year} 年原来录的日子会被这份替换`}
                      okText="覆盖"
                      onConfirm={() => void applyParsed()}
                    >
                      <Button type="primary" size="small" loading={saving}>
                        用这份覆盖 {parsed.year} 年
                      </Button>
                    </Popconfirm>
                  </Space>
                )}
              </div>
            )}
          </div>
        )}

        {!loading && days.length === 0 && (
          <div style={{ color: C.textSub, fontSize: 13 }}>{year} 年还没录入,按周一到周五算</div>
        )}

        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(196px, 1fr))",
            gap: "18px 22px",
            opacity: loading ? 0.5 : 1,
          }}
        >
          {Array.from({ length: 12 }, (_, m) => (
            <Month key={m} year={year} month={m} byDate={byDate} today={today} onPick={(d) => void toggle(d)} />
          ))}
        </div>

        {days.length > 0 && <Summary days={days} />}
      </div>
    </Modal>
  );
}

function Mark({ kind }: { kind: Kind }) {
  return (
    <span
      style={{
        display: "inline-grid",
        placeItems: "center",
        width: 18,
        height: 18,
        borderRadius: 4,
        fontSize: 11,
        fontWeight: 700,
        background: kind === "off" ? OFF_BG : ON_BG,
        color: kind === "off" ? C.progress : ON_FG,
        verticalAlign: "-4px",
      }}
    >
      {kind === "off" ? "休" : "班"}
    </span>
  );
}

function Month({
  year,
  month,
  byDate,
  today,
  onPick,
}: {
  year: number;
  month: number;
  byDate: Map<string, WorkCalendarDay>;
  today: string;
  onPick: (date: string) => void;
}) {
  const first = dayjs(new Date(year, month, 1));
  const lead = (first.day() + 6) % 7; // 周一在最前
  const n = first.daysInMonth();
  const cells: (string | null)[] = [
    ...Array.from({ length: lead }, () => null),
    ...Array.from({ length: n }, (_, i) => first.add(i, "day").format("YYYY-MM-DD")),
  ];
  return (
    <div>
      <div style={{ fontWeight: 600, color: C.text, fontSize: 13, marginBottom: 6 }}>{month + 1} 月</div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(7, 1fr)", gap: 2, fontVariantNumeric: "tabular-nums" }}>
        {WEEK_HEAD.map((w, i) => (
          <div key={w} style={{ textAlign: "center", fontSize: 11, color: i >= 5 ? C.textFaint : C.textSub, paddingBottom: 2 }}>
            {w}
          </div>
        ))}
        {cells.map((d, i) => {
          if (!d) return <div key={`b${i}`} />;
          const rec = byDate.get(d);
          const weekend = (dayjs(d).day() + 6) % 7 >= 5;
          const bg = rec?.kind === "off" ? OFF_BG : rec?.kind === "on" ? ON_BG : "transparent";
          const fg = rec?.kind === "off" ? C.progress : rec?.kind === "on" ? ON_FG : weekend ? C.textFaint : C.text;
          const tip = rec ? `${rec.name ? rec.name + " · " : ""}${rec.kind === "off" ? "放假" : "调休上班"}` : "";
          const cell = (
            <button
              key={d}
              type="button"
              className="wc-day"
              onClick={() => onPick(d)}
              aria-label={`${cnDate(d)}${tip ? " " + tip : ""}`}
              style={{
                position: "relative",
                height: 26,
                border: d === today ? `1px solid ${C.text}` : "1px solid transparent",
                borderRadius: 4,
                background: bg,
                color: fg,
                fontSize: 12,
                fontWeight: rec ? 600 : 400,
                cursor: "pointer",
                padding: 0,
              }}
            >
              {dayjs(d).date()}
              {rec && (
                <span style={{ position: "absolute", top: 0, right: 2, fontSize: 8, lineHeight: 1.2 }}>
                  {rec.kind === "off" ? "休" : "班"}
                </span>
              )}
            </button>
          );
          // 【始终套着 Tooltip,没内容时不弹】有标记才套的话,点一下外层就换了,
          // 按钮被重新挂载 —— 用键盘切换的人焦点丢掉,得从头 Tab 过来
          return (
            <Tooltip key={d} title={tip || undefined} mouseEnterDelay={0.3}>
              {cell}
            </Tooltip>
          );
        })}
      </div>
    </div>
  );
}
