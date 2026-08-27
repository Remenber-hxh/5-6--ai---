import { Alert, Button, Checkbox, Empty, Modal, Select, Space, Table, Tag, Typography, message } from "antd";
import { useCallback, useEffect, useMemo, useState } from "react";

import {
  OwnerBindingGroup,
  OwnerBindingReport,
  applyOwnerBindings,
  getOwnerBindingReport,
} from "../api/mgmt";

/**
 * 把存量计划的负责人(手打的名字)对应到账号。
 *
 * 【为什么要有这一屏,而不是迁移里跑一遍自动全绑】
 * 按名字模糊匹配去猜谁是谁,重名、改过名、写法不一致的会被静默绑错。
 * 而"绑错人"的表现是每日提醒发给了不该发的人 —— 不报错,得等有人抱怨
 * 才发现,那时已经错了很多天。一次人工确认换掉这个风险,很划算。
 *
 * 【所以这一屏的职责是"让人看清楚再点头"】不是"一键搞定"。
 * 三堆分开摆,各自需要人做的判断不一样:
 *   唯一命中  只需确认"是这个人吗",默认勾上
 *   命中多个  必须选一个,不选就不动
 *   谁都不是  这里做不了什么,得先去建账号 —— 只列出来,不给操作
 */
export default function OwnerBinding({ open, onClose }: { open: boolean; onClose: (changed: boolean) => void }) {
  const [report, setReport] = useState<OwnerBindingReport | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  // 唯一命中里勾了哪些名字
  const [picked, setPicked] = useState<Record<string, boolean>>({});
  // 重名的选了哪个账号:ownerName → userId
  const [chosen, setChosen] = useState<Record<string, string>>({});
  const [changed, setChanged] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const r = await getOwnerBindingReport();
      setReport(r);
      // 唯一命中默认勾上 —— 但【整组都绑不了的不勾】,勾了也只会整批被拒
      const next: Record<string, boolean> = {};
      for (const g of r.matched) {
        next[g.ownerName] = (g.blockedPlanIds?.length || 0) < g.planCount;
      }
      setPicked(next);
      setChosen({});
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (open) void load();
  }, [open, load]);

  // 要提交的 planId → userId。
  //
  // 【被拦住的计划要摘掉,不能整组提交】后端是全量校验、一条不合格整批拒。
  // 不摘的话用户点一次提交,得到的是"什么都没绑",而界面上明明勾了。
  const bindings = useMemo(() => {
    if (!report) return [];
    const out: { planId: string; userId: string }[] = [];
    for (const g of report.matched) {
      if (!picked[g.ownerName]) continue;
      const uid = g.candidates?.[0]?.userId;
      if (!uid) continue;
      const blocked = new Set(g.blockedPlanIds || []);
      for (const pid of g.planIds) if (!blocked.has(pid)) out.push({ planId: pid, userId: uid });
    }
    for (const g of report.ambiguous) {
      const uid = chosen[g.ownerName];
      if (!uid) continue;
      for (const pid of g.planIds) out.push({ planId: pid, userId: uid });
    }
    return out;
  }, [report, picked, chosen]);

  async function submit() {
    if (!bindings.length) return;
    setSaving(true);
    try {
      const res = await applyOwnerBindings(bindings);
      message.success(`已绑定 ${res.applied} 条计划`);
      setChanged(true);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "绑定失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Modal
      title="负责人绑定"
      open={open}
      width={860}
      onCancel={() => onClose(changed)}
      footer={
        <Space>
          <Typography.Text type="secondary">
            {bindings.length ? `将绑定 ${bindings.length} 条计划` : "还没有选中任何一条"}
          </Typography.Text>
          <Button onClick={() => onClose(changed)}>关闭</Button>
          <Button type="primary" disabled={!bindings.length} loading={saving} onClick={submit}>
            应用绑定
          </Button>
        </Space>
      }
    >
      <Space direction="vertical" size={14} style={{ width: "100%" }}>
        <Typography.Paragraph type="secondary" style={{ marginBottom: 0 }}>
          老计划的负责人是手打的名字,只有绑到账号才能按人过滤和发提醒。
          这里只列出对应关系,绑不绑由你决定 —— 系统不会替你猜。
        </Typography.Paragraph>

        {report && (
          <Space size={6} wrap>
            <Tag>共 {report.totalPlans} 条计划</Tag>
            <Tag color="green">已绑 {report.alreadyBound}</Tag>
            {report.noOwner > 0 && <Tag>没写负责人 {report.noOwner}</Tag>}
          </Space>
        )}

        {/* ── 唯一命中 ── */}
        <div>
          <Typography.Text strong>唯一命中</Typography.Text>
          <Table<OwnerBindingGroup>
            rowKey="ownerName"
            size="small"
            loading={loading}
            style={{ marginTop: 8 }}
            pagination={false}
            scroll={{ x: "max-content" }}
            locale={{ emptyText: <Empty description="没有可以自动对上的名字" image={Empty.PRESENTED_IMAGE_SIMPLE} /> }}
            dataSource={report?.matched || []}
            columns={[
              {
                title: "绑定",
                width: 60,
                render: (_, g) => {
                  const allBlocked = (g.blockedPlanIds?.length || 0) >= g.planCount;
                  return (
                    <Checkbox
                      checked={!!picked[g.ownerName]}
                      disabled={allBlocked}
                      onChange={(e) => setPicked((p) => ({ ...p, [g.ownerName]: e.target.checked }))}
                    />
                  );
                },
              },
              { title: "计划里写的名字", dataIndex: "ownerName", width: 150 },
              {
                title: "对应账号",
                width: 200,
                render: (_, g) => {
                  const c = g.candidates?.[0];
                  if (!c) return "—";
                  return (
                    <span>
                      {c.displayName}
                      <Typography.Text type="secondary" style={{ marginLeft: 6, fontSize: 12 }}>
                        {c.username}
                        {c.departmentName ? ` · ${c.departmentName}` : ""}
                      </Typography.Text>
                    </span>
                  );
                },
              },
              {
                title: "计划数",
                width: 130,
                render: (_, g) => {
                  const blocked = g.blockedPlanIds?.length || 0;
                  if (!blocked) return `${g.planCount} 条`;
                  return (
                    <span>
                      {g.planCount - blocked} 条
                      <Tag color="orange" style={{ marginLeft: 6 }}>
                        {blocked} 条绑不了
                      </Tag>
                    </span>
                  );
                },
              },
              {
                title: "说明",
                render: (_, g) =>
                  g.blockedNote ? (
                    <Typography.Text type="warning" style={{ fontSize: 12 }}>
                      {g.blockedNote} —— 要绑先去「用户与权限」把项目分给他
                    </Typography.Text>
                  ) : (
                    <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                      —
                    </Typography.Text>
                  ),
              },
            ]}
          />
        </div>

        {/* ── 重名:必须人选 ── */}
        {(report?.ambiguous.length || 0) > 0 && (
          <div>
            <Typography.Text strong>重名,需要你指定</Typography.Text>
            <Alert
              type="warning"
              showIcon
              style={{ margin: "8px 0" }}
              message="这些名字对应多个账号。不选就不动 —— 系统不会替你挑。"
            />
            <Table<OwnerBindingGroup>
              rowKey="ownerName"
              size="small"
              pagination={false}
              scroll={{ x: "max-content" }}
              dataSource={report?.ambiguous || []}
              columns={[
                { title: "计划里写的名字", dataIndex: "ownerName", width: 150 },
                { title: "计划数", width: 80, render: (_, g) => `${g.planCount} 条` },
                {
                  title: "选一个账号",
                  render: (_, g) => (
                    <Select
                      allowClear
                      style={{ minWidth: 260 }}
                      placeholder="不选 = 这些计划保持未绑定"
                      value={chosen[g.ownerName] || undefined}
                      onChange={(v) => setChosen((c) => ({ ...c, [g.ownerName]: v || "" }))}
                      options={(g.candidates || []).map((c) => ({
                        value: c.userId,
                        label: `${c.displayName}(${c.username})${c.departmentName ? " · " + c.departmentName : ""}`,
                      }))}
                    />
                  ),
                },
              ]}
            />
          </div>
        )}

        {/* ── 谁都不是:这里做不了什么,只是让人知道 ── */}
        {(report?.unmatched.length || 0) > 0 && (
          <div>
            <Typography.Text strong>没有对应账号</Typography.Text>
            <Typography.Paragraph type="secondary" style={{ fontSize: 12, margin: "4px 0 8px" }}>
              外委班组、离职的人、或者名字写错了。这些计划保持只有名字 ——
              能看能改,只是收不到每日提醒。
            </Typography.Paragraph>
            <Space size={[6, 6]} wrap>
              {(report?.unmatched || []).map((g) => (
                <Tag key={g.ownerName}>
                  {g.ownerName} · {g.planCount} 条
                </Tag>
              ))}
            </Space>
          </div>
        )}
      </Space>
    </Modal>
  );
}
