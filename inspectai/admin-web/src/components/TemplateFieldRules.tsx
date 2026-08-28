import { SaveOutlined } from "@ant-design/icons";
import { Alert, Button, Card, InputNumber, Space, Switch, Table, Tabs, Tag, Tooltip, Typography, message } from "antd";
import { useEffect, useMemo, useState } from "react";

import { ReportTemplateDTO, listReportTemplates, saveTemplateFields } from "../api/mgmt";

/**
 * 巡检模板 · 提交规则(字段必填 + 每单最少照片数)。
 *
 * 模板本身（字段、类型、选项、AI 提示词）写死在后端代码里。这一屏只配一件事：
 * 每个字段**必填还是选填**——这是业务规则，不该每次调整都排一次上线。
 *
 * 【asset_no 锁定，不给开关】它是台账认归属的字段。放开成选填之后，提交时不填，
 * 这条记录就挂不到任何设备上——台账里既看不到这次巡检，也不知道少了谁，
 * 而且要过很久对账时才发现。后端也会拒绝，这里一并锁住是为了不让人白点一次。
 */
export default function TemplateFieldRules() {
  const [templates, setTemplates] = useState<ReportTemplateDTO[]>([]);
  const [current, setCurrent] = useState<string>("");
  const [draft, setDraft] = useState<Record<string, boolean>>({});
  const [minImages, setMinImages] = useState<number>(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  async function load() {
    setLoading(true);
    try {
      const list = await listReportTemplates();
      setTemplates(list);
      if (list.length && !current) setCurrent(list[0].id);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const tpl = useMemo(() => templates.find((t) => t.id === current), [templates, current]);

  // 切模板时把草稿重置成服务端的当前值 —— 不重置的话，在 A 模板上改的开关
  // 会残留到 B 模板上，然后一保存就把 B 改错了。
  useEffect(() => {
    if (!tpl) return;
    const next: Record<string, boolean> = {};
    for (const f of tpl.fields) next[f.code] = f.required;
    setDraft(next);
    setMinImages(tpl.minImages ?? 0);
  }, [tpl]);

  const dirty = useMemo(() => {
    if (!tpl) return false;
    if (minImages !== (tpl.minImages ?? 0)) return true;
    return tpl.fields.some((f) => draft[f.code] !== undefined && draft[f.code] !== f.required);
  }, [tpl, draft, minImages]);

  const requiredCount = useMemo(
    () => (tpl ? tpl.fields.filter((f) => draft[f.code] ?? f.required).length : 0),
    [tpl, draft],
  );

  async function submit() {
    if (!tpl) return;
    setSaving(true);
    try {
      // 只提交真实存在的字段；锁定字段不提交（后端也会拒）
      const payload: Record<string, boolean> = {};
      for (const f of tpl.fields) {
        if (f.code === "asset_no") continue;
        payload[f.code] = draft[f.code] ?? f.required;
      }
      await saveTemplateFields(tpl.id, payload, minImages);
      message.success("已保存，巡检端立即生效");
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <Card
      title="巡检模板 · 提交规则"
      loading={loading}
      extra={
        <Button
          type="primary"
          icon={<SaveOutlined />}
          disabled={!dirty}
          loading={saving}
          onClick={submit}
        >
          保存
        </Button>
      }
    >
      <Space direction="vertical" size={14} style={{ width: "100%" }}>
        {/* 【用 Tabs 不用 Segmented】Segmented 不换行也不滚动:十个模板名
            在 1400px 上就已经顶到边,窄一点直接被裁掉,后面几个模板
            连点都点不到 —— 而且模板还要能自定义,以后只会更多。
            Tabs 自带溢出滚动和"更多"下拉,加多少个都不会丢。 */}
        {templates.length > 0 && (
          <Tabs
            size="small"
            activeKey={current}
            onChange={setCurrent}
            items={templates.map((t) => ({ key: t.id, label: t.name }))}
            style={{ marginBottom: -6 }}
          />
        )}

        {tpl && (
          <>
            <Alert
              type="info"
              showIcon
              message={`${tpl.name} · 共 ${tpl.fields.length} 个字段，其中 ${requiredCount} 个必填`}
              description={
                tpl.project || tpl.assetType
                  ? `适用：${[tpl.project, tpl.assetType].filter(Boolean).join(" · ")}`
                  : undefined
              }
            />
            {/* 【照片数量和字段必填放在一起】它们是同一件事:这一单要交什么才算数。
                分到两个地方配,改的时候会漏掉一半。 */}
            <Space>
              <span style={{ color: "#666" }}>每单最少照片</span>
              <InputNumber
                min={0}
                max={tpl.maxImages || 20}
                value={minImages}
                onChange={(v) => setMinImages(v ?? 0)}
                style={{ width: 110 }}
                addonAfter="张"
              />
              <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                0 = 不限；上限 {tpl.maxImages || 20} 张
              </Typography.Text>
            </Space>
            <Table<ReportTemplateDTO["fields"][number]>
              rowKey="code"
              size="middle"
              // 放不下就横向滚,不要压缩列宽 —— 压缩的结果是中文逐字换行
              scroll={{ x: "max-content" }}
              pagination={false}
              dataSource={tpl.fields}
              columns={[
                { title: "字段", dataIndex: "label" },
                {
                  title: "类型",
                  width: 100,
                  render: (_, f) => (
                    <Tag>{f.kind === "number" ? "数值" : f.kind === "choice" ? "选项" : "文本"}</Tag>
                  ),
                },
                {
                  title: "来源",
                  width: 110,
                  render: (_, f) =>
                    f.manualOnly ? (
                      <Tooltip title="人工填写，AI 不代填也不覆盖">
                        <Tag color="orange">人工</Tag>
                      </Tooltip>
                    ) : f.source === "ai" ? (
                      <Tag color="blue">AI 识别</Tag>
                    ) : (
                      <Tag>人工</Tag>
                    ),
                },
                {
                  title: "必填",
                  width: 120,
                  render: (_, f) => {
                    // 台账认归属的字段不给动 —— 后端也拒。这里锁住是为了
                    // 不让人点完才被打回来，界面能说清的事不该留给报错说。
                    if (f.code === "asset_no") {
                      return (
                        <Tooltip title="设备编号决定这条记录挂到哪台设备，必须填写，不可改为选填">
                          <Tag color="red">锁定必填</Tag>
                        </Tooltip>
                      );
                    }
                    const on = draft[f.code] ?? f.required;
                    return (
                      <Switch
                        checked={on}
                        checkedChildren="必填"
                        unCheckedChildren="选填"
                        onChange={(v) => setDraft((d) => ({ ...d, [f.code]: v }))}
                      />
                    );
                  },
                },
              ]}
            />
          </>
        )}
      </Space>
    </Card>
  );
}
