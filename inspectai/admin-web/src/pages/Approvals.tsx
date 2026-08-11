import { Button, Card, Empty, Popconfirm, Segmented, Skeleton, Space, Table, Tag, message } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { ChangeRequest, listChangeRequests, reviewChangeRequest } from "../api/mgmt";
import { fmtTime } from "../lib/status";

const statusTag = (s?: string) =>
  s === "pending" ? (
    <Tag color="orange">待审批</Tag>
  ) : s === "approved" ? (
    <Tag color="green">已通过</Tag>
  ) : s === "rejected" ? (
    <Tag color="red">已驳回</Tag>
  ) : (
    <Tag>{s || "—"}</Tag>
  );

/**
 * 把 patch 摊成"字段 → 新值"的几行。
 *
 * 两种目标的形状完全不同:
 *   record: { fields: [{ code, label, value }], addImages?: {...} }
 *   asset:  { assetName?, lastStatus?, lastSummary? }
 * 审批的人要据此判断批不批,所以宁可显示原始键名,也不能什么都不显示。
 */
function patchRows(cr: ChangeRequest): { label: string; value: string }[] {
  const p = cr.patch || {};
  const out: { label: string; value: string }[] = [];
  const fields = p.fields;
  if (Array.isArray(fields)) {
    for (const f of fields as { label?: string; code?: string; value?: unknown }[]) {
      out.push({ label: f.label || f.code || "字段", value: String(f.value ?? "—") });
    }
  }
  const assetLabels: Record<string, string> = {
    assetName: "设备名称",
    lastStatus: "设备状态",
    lastSummary: "状态说明",
  };
  for (const [k, label] of Object.entries(assetLabels)) {
    if (p[k] !== undefined && p[k] !== null && p[k] !== "") {
      out.push({ label, value: String(p[k]) });
    }
  }
  const add = p.addImages as { imageIds?: unknown[] } | undefined;
  if (add?.imageIds?.length) {
    out.push({ label: "补交照片", value: `${add.imageIds.length} 张` });
  }
  return out;
}

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", padding: "7px 0", fontSize: 13.5 }}>
      <span style={{ width: 74, flex: "none", color: "#8aa0b0" }}>{label}</span>
      <b style={{ color: "#1c2b3a", fontWeight: 600, wordBreak: "break-all" }}>{children}</b>
    </div>
  );
}

// 审批中心:旧版双栏——左列表 + 右侧常驻审批详情面板(字段对照/整宽操作钮)
export default function Approvals() {
  const [rows, setRows] = useState<ChangeRequest[]>([]);
  const [selId, setSelId] = useState("");
  const [loading, setLoading] = useState(false);
  const [filter, setFilter] = useState("待审批");
  const nav = useNavigate();
  const [params] = useSearchParams();
  const focus = params.get("focus") || "";

  async function load() {
    setLoading(true);
    try {
      setRows(await listChangeRequests());
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  // 通知深链 /v2/#/approval?focus=xxx:切到「全部」并选中该申请(可能已处理,不在待审批里)
  useEffect(() => {
    if (focus && rows.some((r) => r.id === focus)) {
      setFilter("全部");
      setSelId(focus);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, focus]);

  async function review(id: string, action: "approve" | "reject") {
    try {
      await reviewChangeRequest(id, action);
      message.success(action === "approve" ? "申请已通过" : "申请已驳回");
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  const shown = useMemo(() => {
    if (filter === "待审批") return rows.filter((r) => r.status === "pending");
    if (filter === "已处理") return rows.filter((r) => r.status !== "pending");
    return rows;
  }, [rows, filter]);

  // 首行自动选中(右侧面板不留白,与计划/记录页一致)
  useEffect(() => {
    if (!selId || !shown.some((r) => r.id === selId)) setSelId(shown[0]?.id || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shown]);

  const current = shown.find((r) => r.id === selId) || null;

  if (loading && rows.length === 0) {
    return (
      <div style={{ display: "grid", gridTemplateColumns: "1fr 396px", gap: 16, alignItems: "start" }}>
        <Card title="审批中心">
          <Skeleton active paragraph={{ rows: 8 }} />
        </Card>
        <Card size="small">
          <Skeleton active paragraph={{ rows: 6 }} />
        </Card>
      </div>
    );
  }

  return (
    <div style={{ display: "grid", gridTemplateColumns: "1fr 396px", gap: 16, alignItems: "start" }}>
      <Card
        title="审批中心"
        size="small"
        extra={<Segmented options={["待审批", "已处理", "全部"]} value={filter} onChange={(v) => setFilter(String(v))} />}
      >
        <Table<ChangeRequest>
          rowKey="id"
          size="middle"
          loading={loading}
          locale={{
            emptyText: (
              <Empty description={filter === "待审批" ? "当前没有待审批的申请" : "暂无记录"}>
                {filter === "待审批" && rows.length > 0 && (
                  <Button onClick={() => setFilter("全部")}>查看全部</Button>
                )}
              </Empty>
            ),
          }}
          dataSource={shown}
          pagination={{ pageSize: 15, showTotal: (t) => `共 ${t} 条` }}
          rowClassName={(r) => (r.id === selId ? "row-selected" : "")}
          onRow={(r) => ({ onClick: () => setSelId(r.id), style: { cursor: "pointer" } })}
          columns={[
            { title: "时间", dataIndex: "requestedAt", width: 140, render: (v) => fmtTime(v, true) },
            {
              title: "类型",
              width: 110,
              // targetType 是 "asset"/"record",直接摊给审批的人看没意义
              render: (_, cr) => (cr.targetType === "asset" ? "资产台账" : "巡检记录"),
            },
            { title: "设备", dataIndex: "targetName", ellipsis: true },
            { title: "申请人", dataIndex: "requestedBy", width: 100 },
            { title: "理由", dataIndex: "reason", ellipsis: true },
            { title: "状态", width: 92, render: (_, r) => statusTag(r.status) },
          ]}
        />
      </Card>

      {/* 右侧常驻审批详情面板 */}
      <div style={{ position: "sticky", top: 0, maxHeight: "calc(100vh - 104px)", overflowY: "auto" }}>
        {current ? (
          <Card
            size="small"
            title={
              <Space>
                <span style={{ borderLeft: "3px solid #12a968", paddingLeft: 8 }}>审批详情</span>
                {statusTag(current.status)}
              </Space>
            }
          >
            <h3 style={{ margin: "4px 0 10px", fontSize: 17 }}>
              {current.targetName || "修改申请"}
            </h3>
            <div style={{ borderTop: "1px solid #f0f2f5" }}>
              <FieldRow label="类型">
                {current.targetType === "asset" ? "资产台账" : "巡检记录"}
              </FieldRow>
              <FieldRow label="设备">{current.targetName || "—"}</FieldRow>
              <FieldRow label="申请人">{current.requestedBy || "—"}</FieldRow>
              <FieldRow label="时间">{fmtTime(current.requestedAt, true)}</FieldRow>
              {/* 【要改成什么,必须摊开给审批人看】以前这里读的是 fieldLabel /
                  oldValue / newValue —— 后端从来没有这三个字段,所以这一段
                  永远不显示:主管点"通过"时其实不知道自己批的是什么。
                  真实数据在 patch 里,两种目标形状不同,分开渲染。 */}
              {patchRows(current).map((row) => (
                <FieldRow key={row.label} label={row.label}>
                  {row.value}
                </FieldRow>
              ))}
            </div>
            <div style={{ margin: "6px 0 2px" }}>
              <div style={{ color: "#8aa0b0", fontSize: 13 }}>申请理由</div>
              <div style={{ fontSize: 13.5, marginTop: 2, lineHeight: 1.7 }}>{current.reason || "—"}</div>
            </div>
            {current.reviewNote && (
              <div style={{ margin: "10px 0 2px" }}>
                <div style={{ color: "#8aa0b0", fontSize: 13 }}>审批备注</div>
                <div style={{ fontSize: 13.5, marginTop: 2 }}>{current.reviewNote}</div>
              </div>
            )}
            <Space
              direction="vertical"
              style={{ width: "100%", marginTop: 12, borderTop: "1px solid #f0f2f5", paddingTop: 14 }}
            >
              {current.status === "pending" && (
                <>
                  <Popconfirm title="确认通过该申请?" onConfirm={() => review(current.id, "approve")}>
                    <Button type="primary" size="large" block>
                      通过申请
                    </Button>
                  </Popconfirm>
                  <Popconfirm title="确认驳回该申请?" onConfirm={() => review(current.id, "reject")}>
                    <Button danger size="large" block>
                      驳回申请
                    </Button>
                  </Popconfirm>
                </>
              )}
              {current.targetType === "record" && current.targetId && (
                <Button block onClick={() => nav("/record?focus=" + encodeURIComponent(current.targetId!))}>
                  查看原始记录 →
                </Button>
              )}
            </Space>
          </Card>
        ) : (
          <Card size="small">
            <Empty description="点击左侧申请查看详情" />
          </Card>
        )}
      </div>
    </div>
  );
}
