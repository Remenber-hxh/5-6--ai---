import { DownloadOutlined } from "@ant-design/icons";
import { Button, Card, Descriptions, Drawer, Image, Input, Select, Space, Table, Tag } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { ConfirmLog, listConfirmLogs, listRecords } from "../api/mgmt";
import { exportCsv } from "../lib/csv";
import { InspectionRecord, fmtTime, recordBusinessStatus, statusTagColor } from "../lib/status";

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
  const [kw, setKw] = useState("");
  const [current, setCurrent] = useState<InspectionRecord | null>(null);
  const [logs, setLogs] = useState<ConfirmLog[]>([]);
  const [params, setParams] = useSearchParams();

  useEffect(() => {
    listRecords().then((list) => {
      setRecords(list);
      const focus = params.get("focus");
      const focusNo = params.get("focusNo");
      if (focus || focusNo) {
        const hit = list.find((r) => r.id === focus || (focusNo && r.recordNo === focusNo));
        if (hit) setCurrent(hit);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 复核留痕随抽屉懒加载
  useEffect(() => {
    setLogs([]);
    if (current) listConfirmLogs(current.id).then(setLogs).catch(() => void 0);
  }, [current]);

  const rows = useMemo(
    () =>
      records.filter(
        (r) =>
          (!status || recordBusinessStatus(r) === status) &&
          (!kw ||
            (r.pointName || "").includes(kw) ||
            (r.recordNo || "").includes(kw) ||
            (r.inspector || "").includes(kw)),
      ),
    [records, status, kw],
  );

  function doExport() {
    exportCsv(
      `智巡-巡检记录-${new Date().toISOString().slice(0, 10)}`,
      ["记录编号", "巡检时间", "项目", "点位", "模板", "巡检人", "业务状态", "AI 总结"],
      rows.map((r) => [
        r.recordNo || r.id,
        fmtTime(r.createdAt, true),
        r.project || "",
        r.pointName || "",
        r.templateName || "",
        r.inspector || "",
        recordBusinessStatus(r),
        (r.aiSummary || r.report || "").slice(0, 120),
      ]),
    );
  }

  return (
    <Card
      title="巡检记录"
      extra={
        <Space>
          <Select
            allowClear
            placeholder="按状态筛选"
            style={{ width: 140 }}
            options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))}
            onChange={(v) => setStatus(v || "")}
          />
          <Input.Search allowClear placeholder="搜点位 / 编号 / 巡检员" style={{ width: 220 }} onSearch={setKw} />
          <Button icon={<DownloadOutlined />} onClick={doExport}>
            导出
          </Button>
        </Space>
      }
    >
      <Table<InspectionRecord>
        rowKey="id"
        dataSource={rows}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 条` }}
        onRow={(r) => ({ onClick: () => setCurrent(r), style: { cursor: "pointer" } })}
        columns={[
          { title: "时间", dataIndex: "createdAt", width: 150, render: (v) => fmtTime(v, true) },
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
          if (params.get("focus") || params.get("focusNo")) setParams({});
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
            {logs.length > 0 && (
              <>
                <div style={{ margin: "16px 0 8px", fontWeight: 600 }}>
                  复核留痕(共 {logs.length} 次字段确认)
                </div>
                <Table
                  size="small"
                  rowKey={(_, i) => String(i)}
                  dataSource={logs.slice(-8).reverse()}
                  pagination={false}
                  columns={[
                    { title: "字段", render: (_, l) => l.fieldLabel || l.fieldKey || "—" },
                    {
                      title: "动作",
                      width: 70,
                      render: (_, l) => {
                        const map: Record<string, [string, string]> = {
                          confirm: ["确认", "green"],
                          correct: ["修正", "orange"],
                          uncertain: ["标疑", "red"],
                        };
                        const [label, color] = map[l.action || ""] || [l.action || "—", "default"];
                        return <Tag color={color}>{label}</Tag>;
                      },
                    },
                    {
                      title: "看图",
                      width: 70,
                      render: (_, l) => (l.viewedPhoto ? "看图" : <span style={{ color: "#d4380d" }}>未看图</span>),
                    },
                    {
                      title: "置信度",
                      width: 80,
                      render: (_, l) => (l.aiConfidence ? `${Math.round(l.aiConfidence * 100)}%` : "—"),
                    },
                  ]}
                />
              </>
            )}
          </>
        )}
      </Drawer>
    </Card>
  );
}
