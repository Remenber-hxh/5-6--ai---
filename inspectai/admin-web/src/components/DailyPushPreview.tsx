import { Alert, Button, Checkbox, Modal, Space, Switch, TimePicker, message } from "antd";
import dayjs from "dayjs";
import { useCallback, useEffect, useState } from "react";

import {
  DailyPushBot,
  DailyPushBotOverride,
  DailyPushConfig,
  DailyPushDigest,
  getDailyPushConfig,
  previewDailyPush,
  saveDailyPushConfig,
} from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 每日未巡提醒 · 预览 + 设置。
 *
 * 【预览和开关放在一起】它们回答的是同一件事的两半:
 * "会发什么"和"什么时候发"。分成两个页面的话,人改完时间不会回去看文案,
 * 而文案里那些数字正是这条提醒唯一的价值。
 *
 * 【逐字原文,不渲染 markdown】要确认的正是哪些字会出现在领导的群里。
 */

const WEEKDAYS = [
  { v: "1", label: "一" },
  { v: "2", label: "二" },
  { v: "3", label: "三" },
  { v: "4", label: "四" },
  { v: "5", label: "五" },
  { v: "6", label: "六" },
  { v: "7", label: "日" },
];

export default function DailyPushPreview({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [digest, setDigest] = useState<DailyPushDigest | null>(null);
  const [cfg, setCfg] = useState<DailyPushConfig | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      // 设置读不到不该让预览也打不开 —— 预览是这个弹窗更常用的那一半
      const c = await getDailyPushConfig().catch(() => null);
      setCfg(c);
      setDigest(await previewDailyPush(c?.silentWhenDone ?? true));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  // 改了设置立刻重算预览 —— "全部完成时也发"这个开关会直接改变文案
  async function patch(next: Partial<DailyPushConfig>) {
    if (!cfg) return;
    const merged = { ...cfg, ...next };
    setCfg(merged);
    setSaving(true);
    try {
      await saveDailyPushConfig({
        enabled: merged.enabled,
        time: merged.time,
        weekdays: merged.weekdays,
        silentWhenDone: merged.silentWhenDone,
      });
      setDigest(await previewDailyPush(merged.silentWhenDone));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
      await load(); // 失败就回到服务端的真实状态,别让界面停在一个没存进去的值上
    } finally {
      setSaving(false);
    }
  }

  /**
   * 改某一个群的单独设置。
   *
   * 【必须把这个群现有的覆盖一起发回去】后端是整份替换 ——
   * 只发一个 time 的话,这个群原来设的"暂停"会被一起清掉,
   * 而页面上不会有任何提示,下次到点它就又开始发了。
   */
  async function patchBot(bot: DailyPushBot, next: Partial<DailyPushBotOverride>) {
    if (!cfg) return;
    setSaving(true);
    try {
      await saveDailyPushConfig({
        enabled: cfg.enabled,
        time: cfg.time,
        weekdays: cfg.weekdays,
        silentWhenDone: cfg.silentWhenDone,
        bots: [{ index: bot.index, ...bot.override, ...next }],
      });
      // 【重新拉一遍,不在本地拼】effective 是后端合并出来的,
      // 前端自己算一份就等于把合并规则写两遍,迟早分叉。
      setCfg(await getDailyPushConfig());
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
      await load();
    } finally {
      setSaving(false);
    }
  }

  const picked = new Set((cfg?.weekdays || "").split(",").filter(Boolean));

  return (
    <Modal
      title="每日未巡提醒"
      open={open}
      width={660}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
    >
      <Space direction="vertical" size={16} style={{ width: "100%" }}>
        {/* 【没配 webhook 要第一时间说】否则用户打开开关、等到第二天、
            然后来问"为什么没发" —— 而原因和计划、设备都无关。 */}
        {cfg && !cfg.botReady && (
          <Alert
            type="warning"
            showIcon
            message="企业微信群机器人未配置,提醒发不出去"
            description="需要在服务器上设置 WEWORK_BOT_WEBHOOK。设置可以先存,等配好了自动生效。"
          />
        )}

        {cfg && (
          <div style={{ display: "grid", gap: 12 }}>
            <Space size={12} wrap>
              <Switch
                checked={cfg.enabled}
                loading={saving}
                onChange={(v) => void patch({ enabled: v })}
              />
              <span style={{ fontWeight: 600, color: C.text }}>
                {cfg.enabled ? "已开启自动推送" : "未开启,只能在这里预览"}
              </span>
            </Space>

            <Space size={12} wrap>
              <span style={{ color: C.textSub, fontSize: 13 }}>每天</span>
              <TimePicker
                format="HH:mm"
                allowClear={false}
                value={dayjs(cfg.time, "HH:mm")}
                onChange={(v) => v && void patch({ time: v.format("HH:mm") })}
              />
              <span style={{ color: C.textSub, fontSize: 13 }}>推送</span>
            </Space>

            <Space size={10} wrap>
              <span style={{ color: C.textSub, fontSize: 13 }}>执行日</span>
              {WEEKDAYS.map((d) => (
                <Checkbox
                  key={d.v}
                  checked={picked.size === 0 || picked.has(d.v)}
                  onChange={(e) => {
                    // 空 = 每天。所以从"空"开始取消某一天,要先当成全选再去掉。
                    const base = picked.size === 0 ? WEEKDAYS.map((x) => x.v) : [...picked];
                    const next = e.target.checked
                      ? [...new Set([...base, d.v])]
                      : base.filter((x) => x !== d.v);
                    void patch({ weekdays: next.sort().join(",") });
                  }}
                >
                  {d.label}
                </Checkbox>
              ))}
            </Space>

            <Space size={12}>
              <Switch
                size="small"
                checked={!cfg.silentWhenDone}
                onChange={(v) => void patch({ silentWhenDone: !v })}
              />
              <span style={{ color: C.textSub, fontSize: 13 }}>
                全部巡完时也发一条
              </span>
            </Space>
          </div>
        )}

        {/* 【只有配了两个群以上才出现这一块】一个群的时候,"分项目设置"
            是一个永远只有一行、而且那一行还只能写"跟随全局"的空壳。 */}
        {cfg && (cfg.bots?.length ?? 0) > 1 && (
          <div style={{ display: "grid", gap: 10 }}>
            <div style={{ fontWeight: 600, color: C.text, fontSize: 13 }}>
              分项目设置
              <span style={{ fontWeight: 400, color: C.textSub, marginLeft: 8 }}>
                不单独设就按上面那套发
              </span>
            </div>
            {cfg.bots!.map((b) => (
              <BotConfigRow
                key={b.index}
                bot={b}
                saving={saving}
                onChange={(next) => void patchBot(b, next)}
              />
            ))}
          </div>
        )}

        {digest && (
          <>
            {digest.wouldSend ? (
              <Alert
                type="success"
                showIcon
                message={
                  cfg?.enabled
                    ? `今天 ${cfg.time} 会发出下面这条`
                    : "开启后,今天这个点会发出下面这条"
                }
              />
            ) : (
              <Alert type="info" showIcon message={`今天不会发 —— ${digest.skipReason || "无内容"}`} />
            )}

            <pre
                style={{
                  marginTop: 0,
                  padding: "12px 14px",
                  background: "#fafbfc",
                  border: `1px solid ${C.line}`,
                  borderRadius: 8,
                  fontSize: 13,
                  lineHeight: 1.7,
                  whiteSpace: "pre-wrap",
                  wordBreak: "break-word",
                }}
              >
                {digest.text || "(空)"}
            </pre>
          </>
        )}

        <Button size="small" loading={loading} onClick={() => void load()}>
          重新计算
        </Button>
      </Space>
    </Modal>
  );
}

/**
 * 一个群一行。
 *
 * 【收着的时候要把实际几点发写出来】只显示"跟随全局"的话,人得抬头去看
 * 上面那块才知道这个群几点发;而这一行存在的理由恰恰是"这个群到底怎么发"。
 */
function BotConfigRow({
  bot,
  saving,
  onChange,
}: {
  bot: DailyPushBot;
  saving: boolean;
  onChange: (next: Partial<DailyPushBotOverride>) => void;
}) {
  const eff = bot.effective;
  const picked = new Set((eff.weekdays || "").split(",").filter(Boolean));
  const bad = bot.unknownProjects ?? [];
  // 【显示的是库里查到的项目名】配置里那串字符串只在对不上的时候才出现,
  // 而且是作为错误出现 —— 把它当标题显示的话,配错了看起来和配对了一样。
  const label = bot.projects.length
    ? bot.projects.join("、")
    : bad.length
      ? "配置的项目不存在"
      : "全部项目";

  return (
    <div
      style={{
        border: `1px solid ${C.line}`,
        borderRadius: 8,
        padding: "10px 12px",
        display: "grid",
        gap: 8,
      }}
    >
      <Space size={10} wrap style={{ justifyContent: "space-between", width: "100%" }}>
        <Space size={8}>
          <span style={{ fontWeight: 600, color: C.text }}>{label}</span>
          {/* 这个群的 webhook 没配好,单独设置存了也发不出去 —— 就地说明白,
              不然人会以为是时间设错了,反复改时间。 */}
          {!bot.ready && (
            <span style={{ color: C.textSub, fontSize: 12 }}>(地址未配置,发不出去)</span>
          )}
          {/* 【配错项目名是个安静的故障】这个群照常"按时发送成功",
              只是内容里一台设备都没有。不喊出来的话,人会去查计划和设备。 */}
          {bad.length > 0 && (
            <span style={{ color: C.danger, fontSize: 12 }}>
              服务器上配的「{bad.join("、")}」库里没有,这个群收不到任何设备
            </span>
          )}
        </Space>
        <Space size={8}>
          <Switch
            size="small"
            checked={!bot.follows}
            disabled={saving}
            onChange={(v) =>
              // 打开时用这个群【现在实际用的】那套当起点 —— 从全局的值开始改,
              // 比从一个空表单开始少一次"它现在到底几点发"的来回。
              onChange(
                v
                  ? { time: eff.time, weekdays: eff.weekdays }
                  : { enabled: null, time: null, weekdays: null, silentWhenDone: null },
              )
            }
          />
          <span style={{ color: C.textSub, fontSize: 13 }}>单独设置</span>
        </Space>
      </Space>

      {bot.follows ? (
        <div style={{ color: C.textSub, fontSize: 13 }}>
          跟随全局:{eff.time} · {picked.size === 0 ? "每天" : `周${[...picked].sort().map((v) => WEEKDAYS.find((d) => d.v === v)?.label).join("")}`}
        </div>
      ) : (
        <div style={{ display: "grid", gap: 8 }}>
          <Space size={10} wrap>
            <TimePicker
              size="small"
              format="HH:mm"
              allowClear={false}
              disabled={saving}
              value={dayjs(eff.time, "HH:mm")}
              onChange={(v) => v && onChange({ time: v.format("HH:mm") })}
            />
            {WEEKDAYS.map((d) => (
              <Checkbox
                key={d.v}
                disabled={saving}
                checked={picked.size === 0 || picked.has(d.v)}
                onChange={(e) => {
                  const base = picked.size === 0 ? WEEKDAYS.map((x) => x.v) : [...picked];
                  const next = e.target.checked
                    ? [...new Set([...base, d.v])]
                    : base.filter((x) => x !== d.v);
                  onChange({ weekdays: next.sort().join(",") });
                }}
              >
                {d.label}
              </Checkbox>
            ))}
          </Space>
          <Space size={10}>
            <Switch
              size="small"
              disabled={saving}
              checked={bot.override.enabled === false}
              onChange={(v) => onChange({ enabled: v ? false : null })}
            />
            {/* 【只能停自己,不能反过来打开】上面那个总开关关着的时候,
                这里开着也不发 —— 所以文案写"暂停",不写"启用"。 */}
            <span style={{ color: C.textSub, fontSize: 13 }}>这个群暂停推送</span>
          </Space>
        </div>
      )}
    </div>
  );
}
