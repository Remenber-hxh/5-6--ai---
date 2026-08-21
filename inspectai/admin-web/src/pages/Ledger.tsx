import { DeleteOutlined, DownloadOutlined, EditOutlined, MoreOutlined, PlusOutlined, QrcodeOutlined, UploadOutlined } from "@ant-design/icons";
import { AutoComplete, Button, Card, Col, Descriptions, Dropdown, Empty, Form, Input, Modal, Popconfirm, Row, Select, Skeleton, Space, Tag, message } from "antd";
import { motion } from "motion/react";
import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import { AssetEntry, AssetSnapshotEntry, EngineeringTask, createAsset, deleteAsset, listAssetSnapshots, listAssets, listTasks, markAssetNormal, updateAsset, uploadAssetCover } from "../api/mgmt";
import AssetQRSheet from "../components/AssetQRSheet";
import { exportCsv } from "../lib/csv";
import { useUi } from "../store/ui";
import { fmtTime, mediaUrl, statusTagColor } from "../lib/status";

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
  const [kw, setKw] = useState("");
  const [type, setType] = useState("");
  const [current, setCurrent] = useState<AssetEntry | null>(null);
  const [coverUploading, setCoverUploading] = useState(false);
  const [editing, setEditing] = useState(false);
  const [editForm] = Form.useForm();
  const [creating, setCreating] = useState(false);
  const [createForm] = Form.useForm();
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const { project } = useUi();
  const [loading, setLoading] = useState(true);
  const [qrOpen, setQrOpen] = useState(false);
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
    // 【不再拉全量巡检记录】这个页面以前每次打开都要下载整份记录列表
    // (每条带 fields_json / images_json,实测 654 KB),而它只被用来做两处
    // 关联 —— 而那两处的关联键还是错的(按 pointId 而非 assetId)。
    // 轨迹改成按资产单查,封面直接用资产自己的字段,这份下载就完全不需要了。
    Promise.all([listAssets(), listTasks()]).then(([as, ts]) => {
      setAssets(as);
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
  const projects = useMemo(
    () => Array.from(new Set(assets.map((a) => a.project).filter(Boolean))) as string[],
    [assets],
  );

  // 资产 → 封面照。全部取自资产【自己】的字段,不去记录里找。
  //
  // 【原来还有一段兜底:按 pointId 到全量记录里找最新带图的一条】pointId 是
  // 巡检点位/模板,同类设备共用 —— 那段兜底会把别的电梯的照片贴到这台头上
  // (HT-3 一度显示的是一张空调温控器)。而且它本来就多余:下面这行的最后
  // 一个回退已经是 a.lastPhotoPath,后端在 enrichAssetForDisplay 里填好了。
  const photoOf = useMemo(() => {
    const map: Record<string, string> = {};
    for (const a of assets) {
      const p = a.coverImage?.path || a.coverImage?.url || a.coverImagePath || a.lastPhotoPath;
      if (p) map[a.id] = mediaUrl(p);
    }
    return map;
  }, [assets]);

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

  // 巡检轨迹:按【资产】查后端,不在前端筛全量记录。
  //
  // 【原来是 r.pointId === current.pointId】pointId 是巡检点位/模板
  // (比如"无机房电梯"),整栋楼的无机房电梯共用同一个 —— 筛出来的是
  // "所有同类设备的记录",不是这一台的。线上表现:KT-3 的轨迹里列着
  // FT-11、FT-12 的巡检时间,而 KT-3 自己那条 10:58 反而不在里面。
  // 不报错、不空白,看起来一切正常,所以一直没人发现。
  //
  // asset_snapshots 才是按 assetId 记的,后端 /api/assets/{id}/records 查的就是它。
  const [trail, setTrail] = useState<AssetSnapshotEntry[]>([]);
  useEffect(() => {
    if (!current) {
      setTrail([]);
      return;
    }
    let alive = true; // 快速切换资产时,后回来的旧请求不能覆盖新结果
    listAssetSnapshots(current.id, 5)
      .then((d) => alive && setTrail(d.records))
      .catch(() => alive && setTrail([]));
    return () => {
      alive = false;
    };
  }, [current]);

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
          <Button type="primary" icon={<PlusOutlined />} onClick={() => { createForm.resetFields(); setCreating(true); }}>
            新增资产
          </Button>
          {/* 打印范围跟随当前筛选(rows 而不是 assets)——
              想只印某个项目,先在上面筛好再点这里 */}
          <Button icon={<QrcodeOutlined />} onClick={() => setQrOpen(true)}>
            二维码
          </Button>
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
            extra={
              <Dropdown
                trigger={["click"]}
                menu={{
                  items: [
                    {
                      key: "cover",
                      icon: <UploadOutlined />,
                      label: currentPhoto ? "更换标准图" : "上传标准图",
                    },
                    {
                      key: "edit",
                      icon: <EditOutlined />,
                      label: "编辑资产",
                    },
                    { type: "divider" },
                    {
                      key: "delete",
                      icon: <DeleteOutlined />,
                      label: "删除资产",
                      danger: true,
                    },
                  ],
                  onClick: ({ key }) => {
                    if (key === "cover") document.getElementById(coverInputId)?.click();
                    else if (key === "edit") {
                      editForm.setFieldsValue({
                        assetName: current.assetName,
                        lastStatus: current.lastStatus || "正常",
                        lastSummary: current.lastSummary || "",
                      });
                      setEditing(true);
                    } else if (key === "delete") {
                      Modal.confirm({
                        title: "删除该资产?",
                        content: "台账、快照与趋势数据将删除;历史巡检记录保留。存在未完成整改任务时无法删除。",
                        okText: "删除",
                        okButtonProps: { danger: true },
                        onOk: async () => {
                          try {
                            await deleteAsset(current.id);
                            message.success("资产已删除");
                            setCurrent(null);
                            await reload();
                          } catch (e) {
                            message.error(e instanceof Error ? e.message : "删除失败");
                          }
                        },
                      });
                    }
                  },
                }}
              >
                <Button
                  type="text"
                  size="small"
                  icon={<MoreOutlined />}
                  loading={coverUploading}
                  style={{ color: "#98a8b3" }}
                />
              </Dropdown>
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
            <Modal
              title="编辑资产"
              open={editing}
              onCancel={() => setEditing(false)}
              onOk={() => editForm.submit()}
              destroyOnClose
            >
              <Form
                form={editForm}
                layout="vertical"
                onFinish={async (v) => {
                  try {
                    await updateAsset(current.id, v);
                    message.success("已保存");
                    setEditing(false);
                    await reload();
                  } catch (e) {
                    message.error(e instanceof Error ? e.message : "保存失败");
                  }
                }}
              >
                <Form.Item name="assetName" label="资产名称" rules={[{ required: true, message: "请输入名称" }]}>
                  <Input maxLength={64} />
                </Form.Item>
                <Form.Item name="lastStatus" label="状态">
                  <Select options={["正常", "异常", "待复核", "待维修"].map((s) => ({ value: s, label: s }))} />
                </Form.Item>
                <Form.Item name="lastSummary" label="摘要">
                  <Input.TextArea rows={3} maxLength={200} />
                </Form.Item>
              </Form>
            </Modal>
            <div style={{ margin: "16px 0 8px", fontWeight: 600 }}>巡检轨迹(近 {trail.length} 条)</div>
            {trail.length === 0 ? (
              <div style={{ color: "#8aa0b0", fontSize: 13 }}>暂无巡检记录</div>
            ) : (
              trail.map((r) => {
                // 【点进去要用 recordId,不是快照自己的 id】快照是"这台设备在
                // 某次巡检里的状态",记录才是那次巡检本身。传错了详情页什么都查不到。
                const s = r.status || "已完成";
                return (
                  <div
                    key={r.id}
                    onClick={() => nav(`/record?focus=${encodeURIComponent(r.recordId)}`)}
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
                      {/* 不用 slice 截断:外层已经 overflow:hidden + textOverflow:ellipsis,
                          按宽度省略比按字数截更准,也不会把 emoji 劈成半个 */}
                      {r.summary || "—"}
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
      <AssetQRSheet assets={rows} open={qrOpen} onClose={() => setQrOpen(false)} />
      <Modal
        title="新增资产"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={() => createForm.submit()}
        destroyOnClose
      >
        <Form
          form={createForm}
          layout="vertical"
          requiredMark={false}
          onFinish={async (v) => {
            try {
              const created = await createAsset(v);
              message.success("资产已建档(未巡检)");
              setCreating(false);
              await reload();
              setCurrent(created);
            } catch (e) {
              message.error(e instanceof Error ? e.message : "创建失败");
            }
          }}
        >
          <Form.Item name="project" label="项目" rules={[{ required: true, message: "请输入项目" }]}>
            <AutoComplete
              options={projects.map((p) => ({ value: p }))}
              placeholder="选择或输入项目名"
              filterOption={(input, opt) => String(opt?.value || "").includes(input)}
            />
          </Form.Item>
          <Form.Item name="assetKey" label="设备编号" rules={[{ required: true, message: "请输入编号" }]}>
            <Input placeholder="如 K08 / XFBF-2" maxLength={64} />
          </Form.Item>
          <Form.Item name="assetName" label="设备名称" rules={[{ required: true, message: "请输入名称" }]}>
            <Input maxLength={64} />
          </Form.Item>
          <Form.Item name="assetType" label="设备类型">
            <AutoComplete
              options={types.map((t) => ({ value: t }))}
              placeholder="如 有机房电梯 / 消防泵房"
              filterOption={(input, opt) => String(opt?.value || "").includes(input)}
            />
          </Form.Item>
          <Form.Item name="summary" label="备注">
            <Input.TextArea rows={2} maxLength={200} placeholder="选填" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
