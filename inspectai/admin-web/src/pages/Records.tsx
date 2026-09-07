import { DownloadOutlined } from "@ant-design/icons";
import { Button, Card, Empty, Image, Input, Select, Skeleton, Space, Table, Tag, message } from "antd";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  ConfirmLog,
  downloadRecordsCsv,
  listConfirmLogs,
  listRecords,
  listReportTemplates,
} from "../api/mgmt";
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
  const [total, setTotal] = useState(0);
  const [templateNames, setTemplateNames] = useState<string[]>([]);
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

  // 深链带来的目标记录。【只认一次】消费掉就清空 —— 不清的话,之后每次
  // 翻页都会被拽回目标那一页,人根本翻不动。
  const [pending, setPending] = useState<{ focus: string; focusNo: string } | null>(() => {
    const focus = params.get("focus") || "";
    const focusNo = params.get("focusNo") || "";
    return focus || focusNo ? { focus, focusNo } : null;
  });

  // 模板下拉的选项。
  //
  // 【不能再从当前这批记录里推】服务端分页之后"这批"只有 15 条,
  // 下拉里就只剩这 15 条用到的模板 —— 人会以为别的模板没有记录。
  useEffect(() => {
    listReportTemplates()
      .then((list) => setTemplateNames(list.map((t) => t.name).filter(Boolean)))
      .catch(() => setTemplateNames([]));
  }, []);

  // 取当前这一页。筛选变了就回到第 1 页(留在第 7 页多半是空的)。
  useEffect(() => {
    let alive = true;
    setLoading(true);
    listRecords({
      limit: PAGE_SIZE,
      // 有待定的深链目标时不传 offset,让后端决定翻到哪一页
      offset: pending ? undefined : (page - 1) * PAGE_SIZE,
      project,
      template: tpl,
      status,
      keyword: kw,
      focus: pending?.focus,
      focusNo: pending?.focusNo,
    })
      .then((d) => {
        if (!alive) return;
        setRecords(d.records);
        setTotal(d.total);
        if (!pending) return;

        if (d.focusIndex >= 0) {
          // 【这里会多发一次请求,是有意接受的】setPage 之后 effect 会
          // 再拉一次同一页,拿到的数据一模一样,不闪也不错。
          // 想省掉它就得记住"当前这批对应哪个查询"再跳过 —— 那个判断
          // 一旦写错就是"页面该刷新却不刷新",比多发一次请求糟得多。
          // 而且它只在从深链进来时发生一次。
          setPage(Math.floor(d.offset / PAGE_SIZE) + 1);
          const hit = d.records.find(
            (r) => r.id === pending.focus || (pending.focusNo && r.recordNo === pending.focusNo),
          );
          if (hit) {
            setSelId(hit.id);
            setFlashId(hit.id);
          }
          setPending(null);
          return;
        }
        // 找不到 = 被筛选挡住了。
        //
        // 【不能就这么算了】列表会照常显示第一页,右侧详情显示的是完全
        // 另一次巡检,而人以为那就是他点进来的那一条 —— 没有任何提示。
        if (status || tpl || kw || project) {
          setStatus("");
          setKw("");
          setTpl("");
          setProject("");
          // 悄悄改掉用户的筛选是不礼貌的,至少要说一声为什么
          message.info("已清除筛选,以显示你要看的那条记录");
          return; // 筛选清空会重新触发这个 effect,pending 留着下一轮再用
        }
        // 筛选本来就是空的还找不到:这条记录真的不在(被删了 / 没权限看)
        message.warning("没有找到你要看的那条记录,它可能已被删除或不在你的可见范围内");
        setPending(null);
      })
      .catch(() => {
        if (alive) setRecords([]);
      })
      .finally(() => {
        if (alive) setLoading(false);
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, project, tpl, status, kw, pending]);

  // 筛选一变就回第 1 页 —— 留在第 7 页的话,筛完多半是一片空白,
  // 而人看到的是"没有结果",不是"你还停在第 7 页"。
  //
  // 【和筛选写在同一个事件里,不用 effect】用 effect 的话是两次渲染:
  // 先带着旧页码请求一次(第 3 页 + 新筛选,多半是空的),再回到第 1 页
  // 请求第二次 —— 白跑一趟,中间还会闪一下错的内容。
  // 写在一起 React 会合批,只发一次请求。
  const changeFilter = (apply: () => void) => {
    apply();
    setPage(1);
  };

  // 项目是全局状态(侧栏切的),不经过上面那个函数,只能靠 effect 兜。
  // page 已经是 1 时 setPage(1) 不会触发重渲染,所以不会多发请求。
  useEffect(() => {
    setPage(1);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [project]);

  // 服务端已经筛过了,这里不再二次过滤 —— 再筛一次的话,后端给的 total
  // 和页面上实际显示的行数会对不上。
  const rows = records;

  // 首行自动选中(右侧面板不留白,与计划页一致)
  useEffect(() => {
    if (pending) return; // 深链定位还没落定,别抢走选中
    if (!selId || !rows.some((r) => r.id === selId)) setSelId(rows[0]?.id || "");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows]);

  const current = rows.find((r) => r.id === selId) || null;

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

  const [exporting, setExporting] = useState(false);

  // 导出交给后端。
  //
  // 【为什么不再用页面手里那批数据】它有条数上限,导出的文件跟着少 ——
  // 而文件里没有任何地方写着"这只是最近 N 条"。页面上还有分页能看出来,
  // 文件发出去之后只会被当成全量:贴进汇报、发给甲方、拿去对账。
  //
  // 筛选条件原样带给后端,导出的范围就等于你在页面上筛出来的那一批。
  async function doExport() {
    setExporting(true);
    try {
      await downloadRecordsCsv({ project, template: tpl, status, keyword: kw });
    } catch (e) {
      message.error(e instanceof Error ? e.message : "导出失败");
    } finally {
      setExporting(false);
    }
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
            onChange={(v) => changeFilter(() => setStatus(v || ""))}
          />
          <Select
            allowClear
            showSearch
            placeholder="按模板筛选"
            style={{ width: 160 }}
            options={templateNames.map((t) => ({ value: t, label: t }))}
            onChange={(v) => changeFilter(() => setTpl(v || ""))}
          />
          <Input.Search allowClear placeholder="搜点位 / 编号 / 巡检员" style={{ width: 200 }} onSearch={(v) => changeFilter(() => setKw(v))} />
          <Button icon={<DownloadOutlined />} loading={exporting} onClick={doExport}>
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
            // 【总数由后端给】原来这里数的是前端手里那个数组的长度 ——
            // 于是"共 100 条"看上去就是"这个系统只存了 100 条巡检记录"。
            total,
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
