import { DownloadOutlined, UploadOutlined } from "@ant-design/icons";
import { Button, Card, Col, Descriptions, Empty, Input, Popconfirm, Row, Select, Skeleton, Space, Tag, message } from "antd";
import { motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AssetEntry, EngineeringTask, listAssets, listRecords, listTasks, markAssetNormal, uploadAssetCover } from "../api/mgmt";
import { exportCsv } from "../lib/csv";
import { useUi } from "../store/ui";
import { InspectionRecord, fmtTime, mediaUrl, recordBusinessStatus, statusTagColor } from "../lib/status";

const levelTag = (a: AssetEntry) => {
  const s = a.lastStatus || "";
  if (s === "异常" || a.statusLevel === "danger") return <Tag color="red">异常</Tag>;
  if (s === "待复核" || a.statusLevel === "warning") return <Tag color="orange">待复核</Tag>;
  if (s === "待维修" || a.statusLevel === "repair") return <Tag color="gold">维修中</Tag>;
  return <Tag color="green">正常</Tag>;
};

// 资产台账:卡片网格(带设备照片预览,与旧版一致)+ 详情抽屉(巡检轨迹)
export default function Ledger() {
  const nav = useNavigate();
  const [assets, setAssets] = useState<AssetEntry[]>([]);
  const [records, setRecords] = useState<InspectionRecord[]>([]);
  const [kw, setKw] = useState("");
  const [type, setType] = useState("");
  const [current, setCurrent] = useState<AssetEntry | null>(null);
  const [coverUploading, setCoverUploading] = useState(false);
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const { project } = useUi();
  const [loading, setLoading] = useState(true);
  const [params] = useSearchParams();

  async function reload() {
    const as = await listAssets();
    setAssets(as);
    if (current) setCurrent(as.find((x) => x.id === current.id) || null);
  }
  async function changeCover(file?: File | null) {
    if (!current || !file) return;
    setCoverUploading(true);
    try {
      const updated = await uploadAssetCover(current.id, file);
      setAssets((items) => items.map((item) => (item.id === updated.id ? updated : item)));
      setCurrent(updated);
      message.success("标准图已更新");
    } catch (e) {
      message.error(e instanceof Error ? e.message : "上传失败");
    } finally {
      setCoverUploading(false);
    }
  }

  useEffect(() => {
    Promise.all([listAssets(), listRecords(), listTasks()]).then(([as, rs, ts]) => {
      setAssets(as);
      setRecords(rs);
      setTasks(ts);
      const focus = params.get("focus");
      if (focus) {
        const hit = as.find((a) => a.id === focus);
        if (hit) setCurrent(hit);
      }
      setLoading(false);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const types = useMemo(
    () => Array.from(new Set(assets.map((a) => a.assetType).filter(Boolean))) as string[],
    [assets],
  );

  // 资产 → 最近一张现场照片(取该点位最新带图记录)
  const photoOf = useMemo(() => {
    const map: Record<string, string> = {};
    const sorted = [...records].sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""));
    for (const a of assets) {
      const preferred = a.coverImage?.path || a.coverImage?.url || a.coverImagePath || a.lastPhotoPath;
      if (preferred) {
        map[a.id] = mediaUrl(preferred);
        continue;
      }
      const rec = sorted.find((r) => (r.pointId === a.pointId || r.id === a.lastRecordId) && r.images?.length);
      const p = rec?.images?.[0];
      if (p) map[a.id] = mediaUrl(p.path || p.url);
    }
    return map;
  }, [assets, records]);

  const rows = useMemo(
    () =>
      assets.filter(
        (a) =>
          (!project || a.project === project) &&
          (!type || a.assetType === type) &&
          (!kw ||
            (a.assetName || "").includes(kw) ||
            (a.assetKey || "").includes(kw) ||
            (a.project || "").includes(kw)),
      ),
    [assets, kw, type, project],
  );

  const trail = useMemo(() => {
    if (!current) return [];
    return records
      .filter((r) => r.pointId === current.pointId || r.id === current.lastRecordId)
      .sort((a, b) => (b.createdAt || "").localeCompare(a.createdAt || ""))
      .slice(0, 5);
  }, [current, records]);

  const currentPhoto = current ? photoOf[current.id] : "";
  const coverInputId = current ? `asset-cover-${current.id}` : "asset-cover";
  // 默认选中首个资产(右侧面板不留白,与计划/记录页一致)
  // eslint-disable-next-line react-hooks/rules-of-hooks
  useEffect(() => {
    if (!current || !rows.some((a) => a.id === current.id)) setCurrent(rows[0] || null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows]);

  return (
    <div style={{ display: "grid", gridTemplateColumns: "1fr 396px", gap: 16, alignItems: "start" }}>
    <Card
      size="small"
      title={`资产台账(${rows.length} 台)`}
      extra={
        <Space>
          <Select
            allowClear
            placeholder="类型"
            style={{ width: 150 }}
            options={types.map((t) => ({ value: t, label: t }))}
            onChange={(v) => setType(v || "")}
          />
          <Input.Search allowClear placeholder="搜设备名 / 编号 / 项目" style={{ width: 240 }} onSearch={setKw} />
          <Button
            icon={<DownloadOutlined />}
            onClick={() =>
              exportCsv(
                `智巡-资产台账-${new Date().toISOString().slice(0, 10)}`,
                ["设备", "编号", "类型", "项目", "点位", "状态", "最近巡检"],
                rows.map((a) => [
                  a.assetName || "",
                  a.assetKey || a.id,
                  a.assetType || "",
                  a.project || "",
                  a.pointName || "",
                  a.lastStatus || "正常",
                  fmtTime(a.lastInspectedAt, true),
                ]),
              )
            }
          >
            导出
          </Button>
        </Space>
      }
    >
      {loading && assets.length === 0 ? (
        <Skeleton active paragraph={{ rows: 8 }} />
      ) : rows.length === 0 ? (
        <Empty description={kw || type ? "没有匹配的资产" : "暂无资产"}>
          {(kw || type) && (
            <Button
              onClick={() => {
                setKw("");
                setType("");
              }}
            >
              清除筛选
            </Button>
          )}
        </Empty>
      ) : (
        <Row gutter={[16, 16]}>
          {rows.map((a, i) => (
            <Col key={a.id} xs={24} sm={12} xl={8}>
              <motion.div
                initial={{ opacity: 0, y: 10 }}
                animate={{ opacity: 1, y: 0 }}
                transition={{ delay: Math.min(i * 0.04, 0.4), duration: 0.3 }}
              >
                <Card
                  hoverable
                  size="small"
                  onClick={() => setCurrent(a)}
                  cover={
                    photoOf[a.id] ? (
                      <img
                        src={photoOf[a.id]}
                        alt=""
                        style={{ height: 130, objectFit: "cover" }}
                        loading="lazy"
                      />
                    ) : (
                      <div
                        style={{
                          height: 130,
                          display: "grid",
                          placeItems: "center",
                          background: "#f0f3f7",
                          color: "#9db0be",
                          fontSize: 12,
                        }}
                      >
                        暂无现场照片
                      </div>
                    )
                  }
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <b style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                      {a.assetName}
                    </b>
                    {levelTag(a)}
                  </div>
                  <div style={{ color: "#8aa0b0", fontSize: 12, marginTop: 4 }}>
                    {a.assetType || "—"} · {a.project || "—"}
                  </div>
                  <div style={{ color: "#8aa0b0", fontSize: 12 }}>最近巡检 {fmtTime(a.lastInspectedAt)}</div>
                </Card>
              </motion.div>
            </Col>
          ))}
        </Row>
      )}
    </Card>
      <div style={{ position: "sticky", top: 0, maxHeight: "calc(100vh - 104px)", overflowY: "auto" }}>
        {current ? (
          <Card
            size="small"
            title={
              <Space>
                <span style={{ borderLeft: "3px solid #12a968", paddingLeft: 8 }}>资产详情</span>
                {levelTag(current)}
              </Space>
            }
          >
            <h3 style={{ margin: "4px 0 10px", fontSize: 17 }}>{current.assetName}</h3>
            <div style={{ marginBottom: 12 }}>
              {currentPhoto ? (
                <img
                  src={currentPhoto}
                  alt=""
                  style={{ width: "100%", height: 176, objectFit: "cover", borderRadius: 10, display: "block" }}
                />
              ) : (
                <div
                  style={{
                    height: 176,
                    display: "grid",
                    placeItems: "center",
                    background: "linear-gradient(135deg, #f2f6f8, #e8eef3)",
                    borderRadius: 10,
                    color: "#8aa0b0",
                    fontSize: 13,
                  }}
                >
                  暂无标准图
                </div>
              )}
              <input
                id={coverInputId}
                type="file"
                accept="image/png,image/jpeg,image/webp"
                style={{ display: "none" }}
                onChange={(event) => {
                  void changeCover(event.currentTarget.files?.[0]);
                  event.currentTarget.value = "";
                }}
              />
              <Button
                block
                icon={<UploadOutlined />}
                loading={coverUploading}
                style={{ marginTop: 8 }}
                onClick={() => document.getElementById(coverInputId)?.click()}
              >
                {currentPhoto ? "\u66f4\u6362\u6807\u51c6\u56fe" : "\u4e0a\u4f20\u6807\u51c6\u56fe"}
              </Button>
            </div>
            <Descriptions column={1} size="small">
              <Descriptions.Item label="编号">{current.assetKey || current.id}</Descriptions.Item>
              <Descriptions.Item label="类型">{current.assetType || "—"}</Descriptions.Item>
              <Descriptions.Item label="项目">{current.project || "—"}</Descriptions.Item>
              <Descriptions.Item label="点位">{current.pointName || "—"}</Descriptions.Item>
              <Descriptions.Item label="最近巡检">{fmtTime(current.lastInspectedAt, true)}</Descriptions.Item>
            </Descriptions>
            {(() => {
              const follow = tasks.filter((t) => t.assetId === current.id && t.status === "待整改");
              return follow.length ? (
                <div
                  onClick={() => nav("/plan")}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 8,
                    padding: "8px 12px",
                    margin: "12px 0",
                    borderRadius: 8,
                    background: "rgba(224,57,43,0.07)",
                    cursor: "pointer",
                    fontSize: 13,
                  }}
                >
                  <Tag color="red" style={{ margin: 0 }}>待整改</Tag>
                  <span style={{ flex: 1, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {follow[0].title}
                  </span>
                  <span style={{ color: "#d4380d" }}>查看任务 →</span>
                </div>
              ) : null;
            })()}
            {(current.lastStatus === "异常" || current.lastStatus === "待复核" || current.statusLevel === "danger" || current.statusLevel === "warning") && (
              <Popconfirm
                title="确认复核后标记该资产为正常?关联的待整改任务将自动销账。"
                onConfirm={async () => {
                  try {
                    await markAssetNormal(current);
                    message.success("已标记正常,关联任务自动销账");
                    await reload();
                  } catch (e) {
                    message.error(e instanceof Error ? e.message : "操作失败");
                  }
                }}
              >
                <Button type="primary" size="large" block style={{ margin: "12px 0" }}>标记正常</Button>
              </Popconfirm>
            )}
            <div style={{ margin: "16px 0 8px", fontWeight: 600 }}>巡检轨迹(近 {trail.length} 条)</div>
            {trail.length === 0 ? (
              <div style={{ color: "#8aa0b0", fontSize: 13 }}>暂无巡检记录</div>
            ) : (
              trail.map((r) => {
                const s = recordBusinessStatus(r);
                return (
                  <div
                    key={r.id}
                    onClick={() => nav(`/record?focus=${encodeURIComponent(r.id)}`)}
                    style={{
                      display: "flex",
                      alignItems: "center",
                      gap: 10,
                      padding: "8px 0",
                      borderBottom: "1px solid #f0f2f5",
                      cursor: "pointer",
                      fontSize: 13,
                    }}
                  >
                    <span style={{ color: "#8aa0b0", flex: "none" }}>{fmtTime(r.createdAt)}</span>
                    <Tag color={statusTagColor(s)} style={{ margin: 0 }}>
                      {s}
                    </Tag>
                    <span
                      style={{
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                        whiteSpace: "nowrap",
                        color: "#5b6b78",
                      }}
                    >
                      {(r.aiSummary || r.report || r.recordNo || "").slice(0, 30)}
                    </span>
                  </div>
                );
              })
            )}
          </Card>
        ) : (
          <Card size="small">
            <Empty description="点击左侧资产查看详情" />
          </Card>
        )}
      </div>
    </div>
  );
}
