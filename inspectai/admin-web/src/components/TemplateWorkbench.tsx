import { EyeOutlined, PlusOutlined, SaveOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  Card,
  Collapse,
  Drawer,
  Input,
  InputNumber,
  Select,
  Space,
  Spin,
  Typography,
  message,
} from "antd";
import { useCallback, useEffect, useRef, useState } from "react";

import {
  ReportTemplateDTO,
  TemplateFieldDTO,
  getReportTemplate,
  listReportTemplates,
  renderPromptTemplate,
  saveReportTemplate,
} from "../api/mgmt";
import FieldCard from "./FieldCard";

// ===== 巡检模板工作台:一个检查项在一处配完 =====
//
// 【替代的是什么】原来同一页三个页签 ——「提示词」「巡检模板」「提交规则」——
// 各编同一份模板的一个侧面。配一个「机房卫生」要走三处:叫什么在模板页、
// 必不必填在提交规则页、AI 怎么判在提示词页。
//
// 那个拆分不是设计出来的,是接口形状逼出来的:三个写入口各写字段表的一部分,
// 互相错开以免冲掉。所以还得配一套跨页同步机制("一页存盘另外两页要重读"),
// 而那套机制本身就是拆错了的证据。
//
// 后端合并之后(一个 PUT 写完整份),这里跟着合:一个列表、点开一行、
// 这一行的全部设置都在里面、一次存盘。跨页同步整个删掉。

const { Text } = Typography;

export interface TemplateWorkbenchProps {
  templateId?: string;
  onTemplateChange?: (id: string) => void;
}

export default function TemplateWorkbench({
  templateId,
  onTemplateChange,
}: TemplateWorkbenchProps) {
  const [list, setList] = useState<ReportTemplateDTO[]>([]);
  const [current, setCurrent] = useState<ReportTemplateDTO | null>(null);
  const [recordCount, setRecordCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [preview, setPreview] = useState<string | null>(null);
  const [previewing, setPreviewing] = useState(false);
  // 新加的字段还没有 code,用它给卡片一个稳定的 key ——
  // 用下标当 key 的话,删中间一行会让后面所有卡片的展开状态错位一格。
  const seq = useRef(0);
  const keys = useRef(new WeakMap<TemplateFieldDTO, string>());

  const keyOf = useCallback((f: TemplateFieldDTO) => {
    let k = keys.current.get(f);
    if (!k) {
      k = f.code || `new_${(seq.current += 1)}`;
      keys.current.set(f, k);
    }
    return k;
  }, []);

  const select = useCallback(async (id: string) => {
    setLoading(true);
    try {
      const d = await getReportTemplate(id);
      setCurrent(d.template);
      setRecordCount(d.recordCount);
      setDirty(false);
      setExpanded(null);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "读取失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void listReportTemplates().then(setList).catch(() => undefined);
  }, []);

  useEffect(() => {
    if (!templateId || current?.id === templateId) return;
    void select(templateId);
  }, [templateId, current?.id, select]);

  function patch(next: Partial<ReportTemplateDTO>) {
    if (!current) return;
    setCurrent({ ...current, ...next });
    setDirty(true);
  }

  function patchField(idx: number, p: Partial<TemplateFieldDTO>) {
    if (!current) return;
    const fields = current.fields.map((f, i) => {
      if (i !== idx) return f;
      const merged = { ...f, ...p };
      // key 跟着对象走,改内容不能让卡片折叠回去
      const k = keys.current.get(f);
      if (k) keys.current.set(merged, k);
      return merged;
    });
    patch({ fields });
  }

  function addField() {
    if (!current) return;
    const f: TemplateFieldDTO = {
      code: "",
      label: "",
      kind: "text",
      required: false,
      source: "ai",
    };
    patch({ fields: [...current.fields, f] });
    setExpanded(keyOf(f));
  }

  function removeField(idx: number) {
    if (!current) return;
    patch({ fields: current.fields.filter((_, i) => i !== idx) });
  }

  async function save() {
    if (!current || saving) return;
    // 【在前端先拦一道,但后端那道不能撤】这里拦是为了让人当场看见哪一行有问题,
    // 而不是存完收到一句笼统的 400。真正的防线仍在后端。
    const bad = current.fields.find((f) => !f.code.trim() || !f.label.trim());
    if (bad) {
      message.error("有检查项还没填名字或标识");
      return;
    }
    const badChoice = current.fields.find(
      (f) => f.kind === "choice" && (f.options?.length || 0) < 2,
    );
    if (badChoice) {
      message.error(`「${badChoice.label}」是选一个,至少要有两个选项`);
      return;
    }
    setSaving(true);
    try {
      await saveReportTemplate(current);
      message.success("已保存,现场下一次打开就是新的");
      setDirty(false);
      void listReportTemplates().then(setList).catch(() => undefined);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function showPreview() {
    if (!current) return;
    setPreviewing(true);
    try {
      setPreview(await renderPromptTemplate(current.id));
    } catch (e) {
      message.error(e instanceof Error ? e.message : "预览失败");
    } finally {
      setPreviewing(false);
    }
  }

  return (
    <Space direction="vertical" size={12} style={{ width: "100%" }}>
      <Space wrap>
        <Select
          value={current?.id}
          placeholder="选一个模板"
          style={{ width: 260 }}
          onChange={(id) => {
            onTemplateChange?.(id);
            void select(id);
          }}
          options={list.map((t) => ({ value: t.id, label: t.name || t.id }))}
        />
        {current && (
          <>
            <Button icon={<SaveOutlined />} type="primary" loading={saving} onClick={() => void save()} disabled={!dirty}>
              {dirty ? "保存" : "已保存"}
            </Button>
            <Button icon={<EyeOutlined />} loading={previewing} onClick={() => void showPreview()}>
              看 AI 会收到什么
            </Button>
          </>
        )}
      </Space>

      {loading && <Spin />}

      {current && !loading && (
        <>
          {recordCount > 0 && (
            <Alert
              type="info"
              showIcon
              message={`这个模板已有 ${recordCount} 条巡检记录`}
              description="字段标识锁住了,改了历史记录里那一项就查不出来。中文名、选项、必填、AI 判定都还能改。"
            />
          )}

          <Card size="small" title="这次巡检是去哪、该拍到什么">
            <Space direction="vertical" size={10} style={{ width: "100%" }}>
              <Space wrap>
                <Labeled label="模板名">
                  <Input value={current.name} style={{ width: 200 }} onChange={(e) => patch({ name: e.target.value })} />
                </Labeled>
                <Labeled label="项目">
                  <Input value={current.project || ""} style={{ width: 160 }} onChange={(e) => patch({ project: e.target.value })} />
                </Labeled>
                <Labeled label="最少几张照片">
                  <InputNumber min={0} value={current.minImages ?? 0} onChange={(v) => patch({ minImages: v ?? 0 })} />
                </Labeled>
                <Labeled label="最多几张">
                  <InputNumber min={1} value={current.maxImages ?? 20} onChange={(v) => patch({ maxImages: v ?? 20 })} />
                </Labeled>
              </Space>
              <Labeled label="一句话说明这次去哪、查什么">
                <Input
                  value={current.scene || ""}
                  placeholder="有机房电梯:查机房环境和设备 + 轿厢层站现场"
                  onChange={(e) => patch({ scene: e.target.value })}
                />
              </Labeled>
              <Labeled label="该拍到哪些照片(一行一条)">
                <Input.TextArea
                  rows={3}
                  value={(current.expectedPhotos || []).join("\n")}
                  placeholder={"机房门\n机房内部\n轿厢操作面板"}
                  onChange={(e) =>
                    patch({
                      expectedPhotos: e.target.value.split("\n").map((s) => s.trim()).filter(Boolean),
                    })
                  }
                />
              </Labeled>
              <Labeled label="照片上一眼能认出这个场景的东西">
                <Input
                  value={current.sceneFeatures || ""}
                  placeholder="蓝底 LCD 电表 / 机械字轮水表"
                  onChange={(e) => patch({ sceneFeatures: e.target.value })}
                />
                <Text type="secondary" style={{ fontSize: 12 }}>
                  现场拍完自动认场景靠它。留空 = 这个模板不参与自动匹配
                </Text>
              </Labeled>
              <Labeled label="整体还要交代 AI 什么(可留空)">
                <Input.TextArea
                  rows={3}
                  value={current.extraNotes || ""}
                  placeholder="落不进任何一个检查项的话写这里,比如「这几块表固定 5 位整数 + 3 位小数」"
                  onChange={(e) => patch({ extraNotes: e.target.value })}
                />
              </Labeled>
            </Space>
          </Card>

          <Card
            size="small"
            title={`检查项 · ${current.fields.length} 项`}
            extra={
              <Button size="small" icon={<PlusOutlined />} onClick={addField}>
                加一项
              </Button>
            }
          >
            {current.fields.length === 0 && <Text type="secondary">还没有检查项,点右上角加一项</Text>}
            {current.fields.map((f, i) => {
              const k = keyOf(f);
              return (
                <FieldCard
                  key={k}
                  field={f}
                  expanded={expanded === k}
                  codeLocked={recordCount > 0 && Boolean(f.code)}
                  onToggle={() => setExpanded(expanded === k ? null : k)}
                  onPatch={(p) => patchField(i, p)}
                  onRemove={() => removeField(i)}
                />
              );
            })}
          </Card>

          {/* 【整段文本收进「高级」】它是迁移期的出口,不是常规用法。
              摆在外面的话,人会以为这是和字段表平级的两种选择,
              一选过去字段表就整个不参与了 —— 而且看不出来。 */}
          <Collapse
            size="small"
            items={[
              {
                key: "adv",
                label: "高级 · 直接写整段提示词",
                children: (
                  <Space direction="vertical" style={{ width: "100%" }}>
                    <Alert
                      type="warning"
                      showIcon
                      message="选了整段文本,上面的检查项就不再参与生成提示词了"
                      description="只有字段表表达不了的场景才用它(比如抄表要先判表型再读数)。正文留空 = 用回内置那份。"
                    />
                    <Select
                      value={current.promptMode || "structured"}
                      style={{ width: 200 }}
                      onChange={(v) => patch({ promptMode: v })}
                      options={[
                        { value: "structured", label: "用上面的检查项生成" },
                        { value: "raw", label: "直接写整段正文" },
                      ]}
                    />
                    {current.promptMode === "raw" && (
                      <Input.TextArea
                        rows={10}
                        value={current.rawText || ""}
                        style={{ fontFamily: "monospace", fontSize: 12 }}
                        onChange={(e) => patch({ rawText: e.target.value })}
                      />
                    )}
                  </Space>
                ),
              },
            ]}
          />
        </>
      )}

      <Drawer
        title="AI 实际会收到这段话"
        width={720}
        open={preview !== null}
        onClose={() => setPreview(null)}
      >
        <pre style={{ whiteSpace: "pre-wrap", fontSize: 12, lineHeight: 1.7 }}>{preview}</pre>
      </Drawer>
    </Space>
  );
}

function Labeled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 12, color: "#888", marginBottom: 4 }}>{label}</div>
      {children}
    </div>
  );
}
