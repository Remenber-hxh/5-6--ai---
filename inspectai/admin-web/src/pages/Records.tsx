import { DownloadOutlined } from "@ant-design/icons";
import { Button, Card, Empty, Image, Input, Select, Skeleton, Space, Table, Tag, message } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import { ConfirmLog, listConfirmLogs, listRecords } from "../api/mgmt";
import { exportCsv } from "../lib/csv";
import { InspectionRecord, fmtTime, mediaUrl, recordBusinessStatus, statusTagColor } from "../lib/status";
import { useUi } from "../store/ui";

const STATUS_OPTIONS = ["异常", "待复核", "需补图", "人工填写", "已完成", "正常"];

// 列表每页条数。定位逻辑要按它算目标在第几页,所以必须是常量 ——
// 两处各写一个数,改一处就会错位。
const PAGE_SIZE = 15;

function FieldRow({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ display: "flex", padding: "6px 0", fontSize: 13.5 }}>
      <span style={{ width: 62, flex: "none", color: "#8aa0b0" }}>{label}</span>
      <b style={{ color: "#1c2b3a", fontWeight: 600, wordBreak: "break-all" }}>{children}</b>
    </div>
  );
}

// 巡检记录:旧版双栏——左列表 + 右侧常驻记录详情面板(照片/字段/复核留痕)
export default function Records() {
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [status, setStatus] = useState<string>("");
  const [kw, setKw] = useState("");
  const [tpl, setTpl] = useState("");
  const { project, setProject } = useUi();
  const [selId, setSelId] = useState("");
  const [page, setPage] = useState(1);
  // 跳转定位后闪一下的那一行。只闪一次,之后回到普通选中态。
  const [flashId, setFlashId] = useState("");
  const [logs, setLogs] = useState<ConfirmLog[]>([]);
  const [loading, setLoading] = useState(true);
  const [params] = useSearchParams();

  useEffect(() => {
    listRecords()
      .then((list) => {
        setRecords(list);
        const focus = params.get("focus");
        const focusNo = params.get("focusNo");
        const hit = list.find((r) => r.id === focus || (focusNo && r.recordNo === focusNo));
        if (hit) {
          setSelId(hit.id);
          setFlashId(hit.id);
          // 【筛选可能把目标藏起来】项目/状态/关键词是跨页面留存的,从台账的
          // 巡检轨迹点进来时,它们未必匹配这一条。藏起来的后果不是"看不到",
          // 而是【看到错的那条】—— 下面"首行自动选中"那个兜底会把选中改成
          // 列表第一条,右侧详情显示的是完全另一次巡检,而且没有任何提示。
          const hidden =
            (!!status && recordBusinessStatus(hit) !== status) ||
            (!!tpl && hit.templateName !== tpl) ||
            (!!kw &&
              !(hit.pointName || "").includes(kw) &&
              !(hit.recordNo || "").includes(kw) &&
              !(hit.inspector || "").includes(kw)) ||
            (!!project && hit.project !== project);
          if (hidden) {
            setStatus("");
            setKw("");
            setTpl("");
            if (project && hit.project !== project) setProject("");
            // 悄悄改掉用户的筛选是不礼貌的,至少要说一声为什么
            message.info("已清除筛选，以显示你要看的那条记录");
          }
        }
      })
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const rows = useMemo(
    () =>
      records.filter(
        (r) =>
          (!project || r.project === project) &&
          (!tpl || r.templateName === tpl) &&
          (!status || recordBusinessStatus(r) === status) &&
          (!kw ||
            (r.pointName || "").includes(kw) ||
            (r.recordNo || "").includes(kw) ||
            (r.inspector || "").includes(kw)),
      ),
    [records, status, kw, tpl, project],
  );

  // 首行自动选中(右侧面板不留白,与计划页一致)
  useEffect(() => {
    if (!selId || !rows.some((r) => r.id === selId)) setSelId(rows[0]?.id || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows]);

  const current = rows.find((r) => r.id === selId) || null;

  // 让选中的那条【一定看得见】:算出它在第几页 → 翻过去 → 滚进视野。
  //
  // 【为什么必须做】从台账的巡检轨迹点进来带的是某条具体记录,而列表默认停在
  // 第 1 页。目标在第 3 页时,右侧详情面板是对的,但左边高亮的那一行在屏幕外 ——
  // 人对不上"到底是哪一条",等于没定位。
  useEffect(() => {
    if (!selId) return;
    const idx = rows.findIndex((r) => r.id === selId);
    if (idx < 0) return;
    const target = Math.floor(idx / PAGE_SIZE) + 1;
    setPage((p) => (p === target ? p : target));
  }, [rows, selId]);

  useEffect(() => {
    if (!selId) return;
    // 等 antd 把翻页后的行渲染出来再滚。直接同步查会拿到旧页的 DOM。
    const t = setTimeout(() => {
      document
        .querySelector(".row-selected")
        ?.scrollIntoView({ block: "center", behavior: "smooth" });
    }, 60);
    return () => clearTimeout(t);
  }, [selId, page]);

  // 闪完就清掉,免得之后手动点别的行时这一行还挂着强调样式
  useEffect(() => {
    if (!flashId) return;
    const t = setTimeout(() => setFlashId(""), 1800);
    return () => clearTimeout(t);
  }, [flashId]);

  // 复核留痕随选中记录懒加载
  useEffect(() => {
    setLogs([]);
    if (selId) listConfirmLogs(selId).then(setLogs).catch(() => void 0);
  }, [selId]);

  function doExport() {
    exportCsv(
      `智巡-巡检记录-${new Date().toISOString().slice(0, 10)}`,
      ["序号", "记录编号", "巡检时间", "所属项目", "巡检点位", "模板", "巡检人", "业务状态", "拍照次数", "字段明细", "AI 总结"],
      rows.map((r, i) => [
        i + 1,
        r.recordNo || r.id,
        fmtTime(r.createdAt, true),
        r.project || "",
        r.pointName || "",
        r.templateName || "",
        r.inspector || "",
        recordBusinessStatus(r),
        r.captureAttempts ?? "",
        (r.fields || []).map((f) => (f.label || f.code) + "=" + (f.value || f.aiValue || "")).join(";"),
        (r.aiSummary || r.report || "").slice(0, 200),
      ]),
    );
  }

  if (loading && records.length === 0) {
    return (
      <div style={{ display: "grid", gridTemplateColumns: "1fr 396px", gap: 16, alignItems: "start" }}>
        <Card title="巡检记录">
          <Skeleton active paragraph={{ rows: 8 }} />
        </Card>
        <Card size="small">
          <Skeleton active paragraph={{ rows: 6 }} />
        </Card>
      </div>
    );
  }

  const hasFilter = Boolean(status || tpl || kw);
  const curStatus = current ? recordBusinessStatus(current) : "";

  return (
    <div style={{ display: "grid", gridTemplateColumns: "1fr 396px", gap: 16, alignItems: "start" }}>
      <Card title="巡检记录" size="small">
        <Space style={{ marginBottom: 14 }} wrap>
          <Select
            allowClear
            placeholder="按状态筛选"
            style={{ width: 130 }}
            options={STATUS_OPTIONS.map((s) => ({ value: s, label: s }))}
            onChange={(v) => setStatus(v || "")}
          />
          <Select
            allowClear
            showSearch
            placeholder="按模板筛选"
            style={{ width: 160 }}
            options={Array.from(new Set(records.map((r) => r.templateName).filter(Boolean))).map((t) => ({
              value: t,
              label: t,
            }))}
            onChange={(v) => setTpl(v || "")}
          />
          <Input.Search allowClear placeholder="搜点位 / 编号 / 巡检员" style={{ width: 200 }} onSearch={setKw} />
          <Button icon={<DownloadOutlined />} onClick={doExport}>
            导出
          </Button>
        </Space>
        <Table<InspectionRecord>
          rowKey="id"
          size="middle"
          locale={{
            emptyText: (
              <Empty description={hasFilter ? "没有匹配的记录" : "暂无巡检记录"}>
                {hasFilter && (
                  <Button
                    onClick={() => {
                      setStatus("");
                      setTpl("");
                      setKw("");
                    }}
                  >
                    清除筛选
                  </Button>
                )}
              </Empty>
            ),
          }}
          dataSource={rows}
          pagination={{
            pageSize: PAGE_SIZE,
            current: page,
            onChange: setPage,
            showTotal: (t) => `共 ${t} 条`,
          }}
          rowClassName={(r) =>
            [r.id === selId ? "row-selected" : "", r.id === flashId ? "row-focus-flash" : ""]
              .filter(Boolean)
              .join(" ")
          }
          onRow={(r) => ({ onClick: () => setSelId(r.id), style: { cursor: "pointer" } })}
          columns={[
            { title: "时间", dataIndex: "createdAt", width: 140, render: (v) => fmtTime(v, true) },
            { title: "点位", dataIndex: "pointName", ellipsis: true },
            { title: "巡检员", dataIndex: "inspector", width: 90 },
            {
              title: "状态",
              width: 96,
              render: (_, r) => {
                const s = recordBusinessStatus(r);
                return <Tag color={statusTagColor(s)}>{s}</Tag>;
              },
            },
            { title: "编号", dataIndex: "recordNo", width: 210, ellipsis: true, render: (v, r) => v || r.id },
          ]}
        />
      </Card>

      {/* 右侧常驻记录详情面板 */}
      <div style={{ position: "sticky", top: 0, maxHeight: "calc(100vh - 104px)", overflowY: "auto" }}>
        {current ? (
          <Card
            size="small"
            title={
              <Space>
                <span style={{ borderLeft: "3px solid #12a968", paddingLeft: 8 }}>记录详情</span>
                <Tag color={statusTagColor(curStatus)}>{curStatus}</Tag>
              </Space>
            }
          >
            <div>
              <FieldRow label="编号">{current.recordNo || current.id}</FieldRow>
              <FieldRow label="时间">{fmtTime(current.createdAt, true)}</FieldRow>
              <FieldRow label="项目">{current.project || "—"}</FieldRow>
              <FieldRow label="点位">{current.pointName || "—"}</FieldRow>
              <FieldRow label="模板">{current.templateName || "—"}</FieldRow>
              <FieldRow label="巡检员">{current.inspector || "—"}</FieldRow>
            </div>
            <div style={{ margin: "8px 0 4px", borderTop: "1px solid #f0f2f5", paddingTop: 10 }}>
              <div style={{ color: "#8aa0b0", fontSize: 13, marginBottom: 4 }}>AI 总结</div>
              <div style={{ fontSize: 13.5, lineHeight: 1.7 }}>
                {current.aiSummary || current.report || "暂无总结"}
              </div>
            </div>
            {!!current.images?.length && (
              <Image.PreviewGroup>
                <div style={{ fontSize: 12, color: "#888", margin: "12px 0 4px" }}>
                  巡检照片 · {current.images.length} 张
                </div>
                <div style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 4 }}>
                  {/* 【不要再截断】这里原来是 images.slice(0, 6) —— 一次巡检拍 13 张,
                      后台只显示 6 张,而且它套在 PreviewGroup 里,点开大图也翻不到
                      第 7 张。照片是巡检的证据,复核的人必须能看全。
                      82px 的缩略图换行排,13 张也就三行。 */}
                  {current.images.map((img, i) => (
                    <Image
                      key={i}
                      width={82}
                      height={82}
                      style={{ objectFit: "cover", borderRadius: 6 }}
                      /* 列表位只有 82px,却下原图(单张可达数百 KB)。?w=240 让后端
                         出小图,点开大图时 PreviewGroup 用的仍是 preview 里的原图。 */
                      src={mediaUrl(img.path || img.url) + "?w=240"}
                      preview={{ src: mediaUrl(img.path || img.url) }}
                    />
                  ))}
                </div>
              </Image.PreviewGroup>
            )}
            <Table
              size="small"
              rowKey={(f) => f.code || f.label || ""}
              dataSource={current.fields || []}
              pagination={false}
              style={{ marginTop: 10 }}
              columns={[
                { title: "字段", render: (_, f) => f.label || f.code },
                { title: "值", render: (_, f) => f.value || f.aiValue || "—" },
                {
                  title: "置信度",
                  width: 78,
                  render: (_, f) => (f.confidence ? `${Math.round(f.confidence * 100)}%` : "—"),
                },
              ]}
            />
            {logs.length > 0 && (
              <>
                <div style={{ margin: "14px 0 8px", fontWeight: 700, fontSize: 13.5 }}>
                  复核留痕(共 {logs.length} 次字段确认)
                </div>
                <Table
                  size="small"
                  rowKey={(l) => `${l.createdAt}_${l.fieldKey}_${l.action}`}
                  dataSource={logs.slice(-8).reverse()}
                  pagination={false}
                  columns={[
                    { title: "字段", render: (_, l) => l.fieldLabel || l.fieldKey || "—" },
                    {
                      title: "动作",
                      width: 64,
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
                      width: 64,
                      render: (_, l) =>
                        l.viewedPhoto ? "看图" : <span style={{ color: "#d4380d" }}>未看图</span>,
                    },
                  ]}
                />
              </>
            )}
          </Card>
        ) : (
          <Card size="small">
            <Empty description="点击左侧记录查看详情" />
          </Card>
        )}
      </div>
    </div>
  );
}
