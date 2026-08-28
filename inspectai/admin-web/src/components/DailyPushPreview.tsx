import { Alert, Button, Modal, Space, Switch, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useState } from "react";

import { DailyPushDigest, previewDailyPush } from "../api/mgmt";

/**
 * 每日未巡提醒 · 预览。
 *
 * 【为什么只到预览就停】口径不准的自动推送比不推送更糟:群里天天收到错的
 * 数字,很快就没人看了,而且很难挽回。先让这条消息在页面上跑准,
 * 再让它自己发出去 —— 和当初做今日看板是同一个顺序。
 *
 * 【给逐字原文,不给"大概长这样"】要确认的正是那些字会不会出现在
 * 领导的群里。所以下面那块是原样展示,不做任何美化渲染。
 */
export default function DailyPushPreview({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const [digest, setDigest] = useState<DailyPushDigest | null>(null);
  const [loading, setLoading] = useState(false);
  // 今天全都巡完了要不要发。默认不发 —— 空洞的推送是让人取关最快的方式;
  // 但"今天全部完成"对管理者确实是汇报,所以留个开关。
  const [silent, setSilent] = useState(true);

  const load = useCallback(async (s: boolean) => {
    setLoading(true);
    try {
      setDigest(await previewDailyPush(s));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "预览失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load(silent);
  }, [open, silent, load]);

  return (
    <Modal
      title="每日提醒 · 预览"
      open={open}
      width={640}
      onCancel={onClose}
      footer={<Button onClick={onClose}>关闭</Button>}
    >
      <Space direction="vertical" size={14} style={{ width: "100%" }}>
        <Alert
          type="info"
          showIcon
          message="这里只算不发"
          description="定时发送还没接上。先确认这条消息的口径和措辞对不对——发错的数字比不发更难挽回。"
        />

        <Space size={16} wrap>
          <Space size={8}>
            <span style={{ color: "#666" }}>全部完成时也发</span>
            <Switch checked={!silent} onChange={(v) => setSilent(!v)} />
          </Space>
          <Button size="small" loading={loading} onClick={() => void load(silent)}>
            重新计算
          </Button>
        </Space>

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
              <Alert type="success" showIcon message="按当前设置,今天这个点会发出下面这条" />
            ) : (
              <Alert type="warning" showIcon message={`今天不会发 —— ${digest.skipReason || "无内容"}`} />
            )}

            {/* 【原样展示,不渲染 markdown】企微那边怎么显示是它的事;
                这里要确认的是"哪些字会被发出去",美化了反而看不清。 */}
            <div>
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                发出去的原文
              </Typography.Text>
              <pre
                style={{
                  marginTop: 6,
                  padding: "12px 14px",
                  background: "#fafbfc",
                  border: "1px solid #f0f0f0",
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
      </Space>
    </Modal>
  );
}
