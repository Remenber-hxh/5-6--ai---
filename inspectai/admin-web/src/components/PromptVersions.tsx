import { Button, Drawer, Empty, Modal, Skeleton, Space, Tag, Typography, message } from "antd";
import { useEffect, useState } from "react";

import {
  PromptVersion,
  getPromptVersion,
  listPromptVersions,
  restorePromptVersion,
} from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 提示词历史版本。
 *
 * 【为什么编辑能力必须配这个】改提示词是全系统最容易"改坏了还看不出来"的
 * 操作:不报错、界面照常、下一张照片开始悄悄误判,而且对所有人立刻生效。
 * 没有回滚的话,唯一的补救是凭记忆把原文敲回去 —— 而人恰恰记不住自己
 * 删掉的那一句是什么。
 *
 * 【先看再回滚,不给一键】列表里只有时间和备注,光凭这两样认不出哪一版
 * 是对的。所以回滚前必须能把那一版的正文整个看一遍。
 */

function fmtTime(ts: string): string {
  const d = new Date(ts);
  if (!ts || Number.isNaN(d.getTime())) return ts || "";
  const p = (n: number) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export default function PromptVersions({
  templateId,
  open,
  onClose,
  onRestored,
}: {
  templateId: string;
  open: boolean;
  onClose: () => void;
  onRestored: () => void;
}) {
  const [versions, setVersions] = useState<PromptVersion[]>([]);
  const [loading, setLoading] = useState(false);
  const [viewing, setViewing] = useState<{ v: PromptVersion; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!open || !templateId) return;
    setLoading(true);
    listPromptVersions(templateId)
      .then(setVersions)
      .catch(() => setVersions([]))
      .finally(() => setLoading(false));
  }, [open, templateId]);

  async function view(v: PromptVersion) {
    try {
      const d = await getPromptVersion(templateId, v.id);
      setViewing({ v, text: d.prompt || "" });
    } catch (e) {
      message.error(e instanceof Error ? e.message : "读取失败");
    }
  }

  function confirmRestore(v: PromptVersion) {
    Modal.confirm({
      title: `回滚到 ${fmtTime(v.createdAt)} 那一版?`,
      // 【说清生效范围】提示词一改是全系统所有项目立刻生效,
      // 不说的话人会以为只影响自己这一台设备。
      content: "回滚后,所有项目的下一次识别立即使用这一版规则。当前内容会作为一条新版本保留。",
      okText: "回滚",
      cancelText: "取消",
      onOk: async () => {
        setBusy(true);
        try {
          await restorePromptVersion(templateId, v.id);
          message.success("已回滚");
          setViewing(null);
          onRestored();
          onClose();
        } catch (e) {
          message.error(e instanceof Error ? e.message : "回滚失败");
          throw e;
        } finally {
          setBusy(false);
        }
      },
    });
  }

  return (
    <>
      <Drawer title="历史版本" open={open} onClose={onClose} width={400}>
        {loading ? (
          <Skeleton active paragraph={{ rows: 6 }} />
        ) : versions.length === 0 ? (
          <Empty
            image={Empty.PRESENTED_IMAGE_SIMPLE}
            description="还没有保存过。第一次保存时会自动留下改动前的样子。"
          />
        ) : (
          <div style={{ display: "grid", gap: 1, background: C.line }}>
            {versions.map((v, i) => (
              <div key={v.id} style={{ background: "#fff", padding: "12px 2px" }}>
                <div style={{ display: "flex", alignItems: "baseline", gap: 8 }}>
                  <span style={{ fontSize: 13.5, color: C.text, fontVariantNumeric: "tabular-nums" }}>
                    {fmtTime(v.createdAt)}
                  </span>
                  {i === 0 && <Tag color="green">当前</Tag>}
                  <span style={{ marginLeft: "auto", fontSize: 12.5, color: C.textFaint }}>
                    {v.author}
                  </span>
                </div>
                {v.note && (
                  <div style={{ fontSize: 13, color: C.textSub, marginTop: 4 }}>{v.note}</div>
                )}
                <Space size={4} style={{ marginTop: 6 }}>
                  <Button type="text" size="small" onClick={() => void view(v)}>
                    查看
                  </Button>
                  {/* 【当前那一条不给回滚】回滚到自己是空操作,却会多写一条
                      历史 —— 点了没反应,人只会再点一次。 */}
                  {i > 0 && (
                    <Button type="text" size="small" disabled={busy} onClick={() => confirmRestore(v)}>
                      回滚到这一版
                    </Button>
                  )}
                </Space>
              </div>
            ))}
          </div>
        )}
      </Drawer>

      <Modal
        title={viewing ? `${fmtTime(viewing.v.createdAt)} 的内容` : ""}
        open={!!viewing}
        width={760}
        onCancel={() => setViewing(null)}
        footer={
          viewing && versions[0]?.id !== viewing.v.id ? (
            <Button type="primary" onClick={() => viewing && confirmRestore(viewing.v)}>
              回滚到这一版
            </Button>
          ) : null
        }
      >
        <Typography.Paragraph>
          <pre
            style={{
              whiteSpace: "pre-wrap",
              fontSize: 12.5,
              maxHeight: "60vh",
              overflow: "auto",
              margin: 0,
            }}
          >
            {viewing?.text}
          </pre>
        </Typography.Paragraph>
      </Modal>
    </>
  );
}
