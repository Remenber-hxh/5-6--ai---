import { Card, Descriptions, Drawer, Image, Select, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { listRecords } from "../api/mgmt";
import { InspectionRecord, recordBusinessStatus, statusTagColor } from "../lib/status";

const STATUS_OPTIONS = ["异常", "待复核", "需补图", "人工填写", "已完成", "正常"];

// 图片地址口径与旧版 mediaUrl 一致:后端以 /storage/ 提供上传文件
function mediaUrl(path?: string): string {
  if (!path) return "";
  if (/^https?:\/\//i.test(path)) return path;
  const normalized = String(path).replace(/\\/g, "/");
  const idx = normalized.indexOf("/storage/");
  let p = idx >= 0 ? normalized.slice(idx + "/storage/".length) : normalized;
  p = p.replace(/^\/?storage\//, "").replace(/^\/+/, "");
  return `/storage/${encodeURI(p)}`;
}

export default function Records() {
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [status, setStatus] = useState<string>("");
  const [current, setCurrent] = useState<InspectionRecord | null>(null);
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    listRecords().then((list) => {
      setRecords(list);
      const focus = params.get("focus");
      if (focus) {
        const hit = list.find((r) => r.id === focus);
        if (hit) setCurrent(hit);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rows = useMemo(
    () => records.filter((r) => !status || recordBusinessStatus(r) === status),
    [records, status],
  );

  return (
    <Card
      title="巡检记录"
      extra={
        <Select
          allowClear
          placeholder="按状态筛选"
          style={{ width: 160 }}
          options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))}
          onChange={(v) => setStatus(v || "")}
        />
      }
    >
      <Table<InspectionRecord>
        rowKey="id"
        dataSource={rows}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        onRow={(r) => ({ onClick: () => setCurrent(r), style: { cursor: "pointer" } })}
        columns={[
          { title: "时间", dataIndex: "createdAt", width: 170, render: (v) => (v || "").slice(0, 16).replace("T", " ") },
          { title: "点位", dataIndex: "pointName" },
          { title: "模板", dataIndex: "templateName", width: 180 },
          { title: "巡检员", dataIndex: "inspector", width: 110 },
          {
            title: "状态",
            width: 100,
            render: (_, r) => {
              const s = recordBusinessStatus(r);
              return <Tag color={statusTagColor(s)}>{s}</Tag>;
            },
          },
          { title: "编号", dataIndex: "recordNo", width: 220, render: (v, r) => v || r.id },
        ]}
      />
      <Drawer
        title="记录详情"
        open={!!current}
        width={520}
        onClose={() => {
          setCurrent(null);
          if (params.get("focus")) setParams({});
        }}
      >
        {current && (
          <>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="编号">{current.recordNo || current.id}</Descriptions.Item>
              <Descriptions.Item label="点位">{current.pointName || "—"}</Descriptions.Item>
              <Descriptions.Item label="巡检员">{current.inspector || "—"}</Descriptions.Item>
              <Descriptions.Item label="AI 总结">
                {current.aiSummary || current.report || "暂无总结"}
              </Descriptions.Item>
            </Descriptions>
            {!!current.images?.length && (
              <Image.PreviewGroup>
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap", margin: "12px 0" }}>
                  {current.images.slice(0, 6).map((img, i) => (
                    <Image key={i} width={88} height={88} style={{ objectFit: "cover", borderRadius: 6 }} src={mediaUrl(img.path || img.url)} />
                  ))}
                </div>
              </Image.PreviewGroup>
            )}
            <Table
              size="small"
              rowKey={(f) => f.code || f.label || ""}
              dataSource={current.fields || []}
              pagination={false}
              columns={[
                { title: "字段", render: (_, f) => f.label || f.code },
                { title: "值", render: (_, f) => f.value || f.aiValue || "—" },
                {
                  title: "置信度",
                  width: 90,
                  render: (_, f) => (f.confidence ? `${Math.round(f.confidence * 100)}%` : "—"),
                },
              ]}
            />
          </>
        )}
      </Drawer>
    </Card>
  );
}
