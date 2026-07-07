import { Button, Card, Col, Input, Modal, Row, Select, Space, Table, message } from "antd";
import { useEffect, useState } from "react";

import {
  PromptField,
  PromptTemplate,
  getPromptTemplate,
  listPromptTemplates,
  renderPromptTemplate,
  savePromptTemplate,
} from "../api/mgmt";

// 提示词模板中心:字段表可编辑(业务人员直接改 AI 判定规则),保存即时生效,可预览渲染后的完整 Prompt
export default function Prompts() {
  const [templates, setTemplates] = useState<PromptTemplate[]>([]);
  const [modes, setModes] = useState<{ value: string; label: string }[]>([]);
  const [current, setCurrent] = useState<PromptTemplate | null>(null);
  const [preview, setPreview] = useState("");
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    listPromptTemplates().then((d) => {
      setTemplates(d.templates || []);
      setModes(d.modes || []);
      if (d.templates?.length) void select(d.templates[0].id);
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  async function select(id: string) {
    const t = await getPromptTemplate(id);
    setCurrent(t);
  }

  function patchField(idx: number, key: keyof PromptField, value: string) {
    if (!current) return;
    const fields = current.fields.map((f, i) => (i === idx ? { ...f, [key]: value } : f));
    setCurrent({ ...current, fields });
  }

  async function save() {
    if (!current) return;
    setSaving(true);
    try {
      await savePromptTemplate(current);
      message.success("已保存,识别立即使用新规则");
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function doPreview() {
    if (!current) return;
    setPreview(await renderPromptTemplate(current.id));
  }

  return (
    <Card
      title="提示词模板"
      extra={
        <Space>
          <Select
            style={{ width: 220 }}
            value={current?.id}
            options={templates.map((t) => ({ value: t.id, label: t.name || t.id }))}
            onChange={(v) => void select(v)}
          />
          <Button onClick={doPreview}>预览完整 Prompt</Button>
          <Button type="primary" loading={saving} onClick={save}>
            保存
          </Button>
        </Space>
      }
    >
      {current && (
        <>
          <Row gutter={12} style={{ marginBottom: 12 }}>
            <Col span={8}>
              <div style={lbl}>模板名称</div>
              <Input value={current.name} onChange={(e) => setCurrent({ ...current, name: e.target.value })} />
            </Col>
            <Col span={10}>
              <div style={lbl}>场景描述</div>
              <Input value={current.scene} onChange={(e) => setCurrent({ ...current, scene: e.target.value })} />
            </Col>
            <Col span={6}>
              <div style={lbl}>必拍照片要求</div>
              <Input
                value={current.expectedPhotos}
                onChange={(e) => setCurrent({ ...current, expectedPhotos: e.target.value })}
              />
            </Col>
          </Row>
          <Table<PromptField>
            rowKey="code"
            size="small"
            dataSource={current.fields}
            pagination={false}
            scroll={{ x: 1100 }}
            columns={[
              { title: "字段", dataIndex: "code", width: 150, fixed: "left", render: (v, f) => `${f.label}\n(${v})` },
              {
                title: "判定模式",
                width: 150,
                render: (_, f, i) => (
                  <Select
                    size="small"
                    style={{ width: 140 }}
                    value={f.mode}
                    options={modes}
                    onChange={(v) => patchField(i, "mode", v)}
                  />
                ),
              },
              {
                title: "判「是」看什么",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    autoSize
                    value={f.yesWhen}
                    onChange={(e) => patchField(i, "yesWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "判「否」看什么",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    autoSize
                    value={f.noWhen}
                    onChange={(e) => patchField(i, "noWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "何时不返回(留人工)",
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    autoSize
                    value={f.skipWhen}
                    onChange={(e) => patchField(i, "skipWhen", e.target.value)}
                  />
                ),
              },
              {
                title: "备注",
                width: 180,
                render: (_, f, i) => (
                  <Input.TextArea
                    size="small"
                    autoSize
                    value={f.note}
                    onChange={(e) => patchField(i, "note", e.target.value)}
                  />
                ),
              },
            ]}
          />
        </>
      )}
      <Modal
        title="渲染后的完整 Prompt"
        open={!!preview}
        width={760}
        footer={null}
        onCancel={() => setPreview("")}
      >
        <pre style={{ whiteSpace: "pre-wrap", fontSize: 12.5, maxHeight: "60vh", overflow: "auto" }}>{preview}</pre>
      </Modal>
    </Card>
  );
}

const lbl: React.CSSProperties = { fontSize: 12, color: "#8aa0b0", marginBottom: 4 };
