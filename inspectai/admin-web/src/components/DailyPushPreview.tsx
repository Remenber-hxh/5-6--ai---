import { Alert, Button, Checkbox, Modal, Space, Switch, Tag, TimePicker, Typography, message } from "antd";
import dayjs from "dayjs";
import { useCallback, useEffect, useState } from "react";

import {
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
              {cfg.timezone && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  按 {cfg.timezone} 计算
                </Typography.Text>
              )}
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
                全部巡完时也发一条(默认不发 —— 空洞的推送会让人不再看它)
              </span>
            </Space>
          </div>
        )}

        {digest && (
          <>
            <Space size={6} wrap>
              <Tag>{digest.date}</Tag>
              <Tag>共 {digest.total} 台</Tag>
              <Tag color="green">已巡 {digest.done}</Tag>
              {digest.pending > 0 && <Tag color="orange">待巡 {digest.pending}</Tag>}
              {digest.missing > 0 && <Tag color="red">台账已删 {digest.missing}</Tag>}
            </Space>

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

            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                发出去的原文
              </Typography.Text>
              <pre
                style={{
                  marginTop: 6,
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
            </div>
          </>
        )}

        <Button size="small" loading={loading} onClick={() => void load()}>
          重新计算
        </Button>
      </Space>
    </Modal>
  );
}
