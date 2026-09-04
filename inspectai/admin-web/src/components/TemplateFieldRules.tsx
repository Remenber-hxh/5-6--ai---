import { SaveOutlined } from "@ant-design/icons";
import { Alert, Button, Card, InputNumber, Select, Space, Switch, Table, Tag, Tooltip, Typography, message } from "antd";
import { useEffect, useMemo, useRef, useState } from "react";

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
export default function TemplateFieldRules({
  templateId,
  onTemplateChange,
}: {
  /** 三个页签共用的"当前模板"。传了就用它,没传/对不上就退回第一个。 */
  templateId?: string;
  onTemplateChange?: (id: string) => void;
} = {}) {
  const [templates, setTemplates] = useState<ReportTemplateDTO[]>([]);
  const [current, setCurrent] = useState<string>("");
  const [draft, setDraft] = useState<Record<string, boolean>>({});
  const [minImages, setMinImages] = useState<number>(0);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  // dirty 是在下面算出来的,而"跟随换模板"的 effect 写在它前面
  // (读起来更顺)。用 ref 取当前值,避免为了顺序把逻辑拆散。
  const dirtyRef = useRef(false);

  async function load() {
    setLoading(true);
    try {
      const list = await listReportTemplates();
      setTemplates(list);
      if (list.length && !current) {
        const want = list.find((t) => t.id === templateId) || list[0];
        setCurrent(want.id);
        if (want.id !== templateId) onTemplateChange?.(want.id);
      }
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

  // 【跟随另外两个页签换模板】理由同 TemplateEditor:页签切走后组件还在,
  // 不跟的话回来看到的是上一个模板的必填开关。改了一半(dirty)就不跟,
  // 未保存的改动优先。
  useEffect(() => {
    if (!templateId || templateId === current) return;
    if (!templates.some((t) => t.id === templateId)) return;
    if (dirtyRef.current) return;
    setCurrent(templateId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [templateId, current, templates]);

  const dirty = useMemo(() => {
    if (!tpl) return false;
    if (minImages !== (tpl.minImages ?? 0)) return true;
    return tpl.fields.some((f) => draft[f.code] !== undefined && draft[f.code] !== f.required);
  }, [tpl, draft, minImages]);
  dirtyRef.current = dirty;

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
        {/* 【改成下拉,和另外两个页签一致】原来是一排 Tab。它自己没问题
            (会溢出滚动),但这一页外面已经套了一层 Tab(提示词/巡检模板/
            提交规则)—— 两排 Tab 叠着,人分不清哪一排是页签、哪一排是模板,
            点错一排就跳到别的页去了。另外两页选模板用的都是下拉,统一。 */}
        {templates.length > 0 && (
          <Space>
            <span style={{ color: "#666" }}>模板</span>
            <Select
              style={{ width: 260 }}
              value={current}
              onChange={(id) => {
                setCurrent(id);
                onTemplateChange?.(id);
              }}
              options={templates.map((t) => ({ value: t.id, label: t.name }))}
            />
          </Space>
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
            {/* 【限宽】这张表只有四列,铺满 1400px 的话字段名在最左、
                必填开关在最右,中间隔着一大片空白,一行要横着扫完整屏 —— 
                很容易看错行,把 A 字段的开关当成 B 字段的。 */}
            <div style={{ maxWidth: 860 }}>
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
            </div>
          </>
        )}
      </Space>
    </Card>
  );
}
