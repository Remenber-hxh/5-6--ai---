import { DeleteOutlined, PlusOutlined } from "@ant-design/icons";
import {
  Alert,
  Button,
  Card,
  Input,
  Modal,
  Select,
  Skeleton,
  Space,
  Table,
  Tag,
  message,
} from "antd";
import { useCallback, useEffect, useState } from "react";

import {
  ProjectEntry,
  ReportTemplateDTO,
  TemplateFieldDTO,
  createReportTemplate,
  deleteReportTemplate,
  getReportTemplate,
  listProjects,
  listReportTemplates,
  saveReportTemplate,
} from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 巡检模板编辑器。
 *
 * 【这一页改的是所有人填什么】改一个字段,所有巡检员的表单跟着变,
 * AI 提取什么跟着变,记录怎么存也跟着变。所以界面的责任不是"让人能改",
 * 而是【让人看得见哪些改不得】——
 *
 * 有记录之后,字段标识不能改、字段不能删:记录里的值是按标识存的,
 * 改了或删了,历史记录里那一项就永远读不出来,而且不报错。
 * 后端会拒,但等到点保存才被拒的话,人已经白填一轮了 —— 所以这里
 * 拿 recordCount 提前把那些输入框锁死,并说清为什么。
 */

const KIND_OPTIONS = [
  { value: "text", label: "文本" },
  { value: "number", label: "数值" },
  { value: "choice", label: "单选" },
];

const SOURCE_OPTIONS = [
  { value: "ai", label: "AI 可填" },
  { value: "manual", label: "人工填" },
];

function emptyTemplate(): ReportTemplateDTO {
  return {
    id: "", // 后端生成
    name: "",
    project: "",
    assetType: "",
    maxImages: 20,
    minImages: 5,
    // 【新模板默认带上设备编号】没有它记录挂不到任何设备,而后端会拒绝保存。
    // 与其让人保存时才撞上,不如一开始就在那儿。
    fields: [
      { code: "asset_no", label: "设备编号", kind: "text", required: true, source: "manual", manualOnly: true },
    ],
  };
}

export default function TemplateEditor({
  templateId,
  onTemplateChange,
}: {
  /** 三个页签共用的"当前模板"。传了就用它,没传/对不上就退回第一个。 */
  templateId?: string;
  onTemplateChange?: (id: string) => void;
} = {}) {
  const [list, setList] = useState<ReportTemplateDTO[]>([]);
  const [current, setCurrent] = useState<ReportTemplateDTO | null>(null);
  const [recordCount, setRecordCount] = useState(0);
  // 载入时就存在的字段标识 = 历史记录里可能有数据的那些。
  // 【必须按标识记,不能按下标】按下标的话,刚新加的字段(排在后面)
  // 也会被算进"已有数据",人加完就删不掉了。
  const [lockedCodes, setLockedCodes] = useState<Set<string>>(new Set());
  const [assetByBuilder, setAssetByBuilder] = useState(false);
  const [creating, setCreating] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [loading, setLoading] = useState(true);
  const [projects, setProjects] = useState<ProjectEntry[]>([]);

  const reloadList = useCallback(async () => {
    const tpls = await listReportTemplates().catch(() => []);
    setList(tpls);
    return tpls;
  }, []);

  const select = useCallback(async (id: string) => {
    try {
      const d = await getReportTemplate(id);
      setCurrent(d.template);
      setRecordCount(d.recordCount || 0);
      setLockedCodes(
        (d.recordCount || 0) > 0
          ? new Set((d.template.fields || []).map((f) => f.code))
          : new Set(),
      );
      setAssetByBuilder(!!d.assetByBuilder);
      setCreating(false);
      setDirty(false);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "读取失败");
    }
  }, []);

  useEffect(() => {
    void (async () => {
      // 项目拉不到不该拖垮整页 —— 它只是个下拉的候选
      listProjects().then(setProjects).catch(() => setProjects([]));
      const tpls = await reloadList();
      const want = tpls.find((t) => t.id === templateId) || tpls[0];
      if (want) {
        // 地址栏里的 id 这一页没有(比如刚被删掉),退回第一个 ——
        // 但要把地址栏也纠正过来,否则另外两页还指着那个不存在的 id。
        if (want.id !== templateId) onTemplateChange?.(want.id);
        await select(want.id);
      }
      setLoading(false);
    })();
    // 只在挂载时定位一次。把 templateId 放进依赖的话,选完模板 → 通知父级 →
    // 参数变 → 这个 effect 重跑 → 把编辑中的内容重新拉一遍,改到一半会被冲掉。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [reloadList, select]);

  // 【跟随另外两个页签换模板】页签切走之后这个组件还留在内存里,
  // 只在挂载时读一次 templateId 的话:在提示词页换了模板,回到这一页
  // 看到的还是上一个 —— 而标题栏的模板名会显示成新的那个,人会照着
  // 一个错的表编辑。
  //
  // 【改了一半就不跟】未保存的改动比"两页同步"重要得多。这时候留在原地,
  // 标题栏的「未保存」标签还在,保存或放弃之后自然会跟上。
  useEffect(() => {
    if (!templateId || creating || dirty) return;
    if (current?.id === templateId) return;
    if (!list.some((t) => t.id === templateId)) return;
    void select(templateId);
  }, [templateId, creating, dirty, current?.id, list, select]);

  // 有记录 = 字段标识锁死。改了它历史记录里那一项就查不出来了。
  const codeLocked = recordCount > 0 && !creating;

  function patch(next: ReportTemplateDTO) {
    setCurrent(next);
    setDirty(true);
  }

  function patchField(idx: number, key: keyof TemplateFieldDTO, value: unknown) {
    if (!current) return;
    patch({
      ...current,
      fields: current.fields.map((f, i) => (i === idx ? { ...f, [key]: value } : f)),
    });
  }

  function addField() {
    if (!current) return;
    patch({
      ...current,
      fields: [
        ...current.fields,
        { code: "", label: "", kind: "text", required: false, source: "manual" },
      ],
    });
  }

  function removeField(idx: number) {
    if (!current) return;
    patch({ ...current, fields: current.fields.filter((_, i) => i !== idx) });
  }

  function switchTemplate(id: string) {
    onTemplateChange?.(id); // 让另外两个页签跟着切到同一份模板
    if (!dirty) return void select(id);
    Modal.confirm({
      title: "当前模板有未保存的修改",
      content: "切换后这些修改会丢失,确认切换?",
      okText: "放弃修改并切换",
      cancelText: "留在本页",
      onOk: () => void select(id),
    });
  }

  function startCreate() {
    const go = () => {
      setCurrent(emptyTemplate());
      setRecordCount(0);
      setLockedCodes(new Set());
      setAssetByBuilder(false);
      setCreating(true);
      setDirty(false);
    };
    if (!dirty) return go();
    Modal.confirm({
      title: "当前模板有未保存的修改",
      content: "新建后这些修改会丢失,确认新建?",
      okText: "放弃修改并新建",
      cancelText: "留在本页",
      onOk: go,
    });
  }

  async function save() {
    if (!current) return;
    setSaving(true);
    try {
      const saved = creating
        ? await createReportTemplate(current)
        : await saveReportTemplate(current);
      message.success(creating ? "已创建" : "已保存,移动端下次打开即生效");
      await reloadList();
      await select(saved.template?.id || current.id);
    } catch (e) {
      // 后端的拒绝理由都写成了人话,直接给出来 ——
      // 换成"保存失败"会让人完全不知道该改哪里。
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  function removeTemplate() {
    if (!current || creating) return;
    Modal.confirm({
      title: `删除模板「${current.name}」?`,
      content: "只有从没被巡检记录用过的模板才能删除。",
      okText: "删除",
      okButtonProps: { danger: true },
      cancelText: "取消",
      onOk: async () => {
        try {
          await deleteReportTemplate(current.id);
          message.success("已删除");
          const tpls = await reloadList();
          if (tpls.length) await select(tpls[0].id);
          else setCurrent(null);
        } catch (e) {
          message.error(e instanceof Error ? e.message : "删除失败");
          throw e;
        }
      },
    });
  }

  if (loading) {
    return (
      <Card size="small">
        <Skeleton active paragraph={{ rows: 8 }} />
      </Card>
    );
  }

  return (
    <Card
      size="small"
      title={
        <Space>
          巡检模板
          {dirty && <Tag color="orange">未保存</Tag>}
          {codeLocked && <Tag>已有 {recordCount} 条记录</Tag>}
        </Space>
      }
      extra={
        <Space>
          <Select
            style={{ width: 220 }}
            value={creating ? undefined : current?.id}
            placeholder="选择模板"
            options={list.map((t) => ({ value: t.id, label: t.name || t.id }))}
            onChange={switchTemplate}
          />
          <Button type="text" icon={<PlusOutlined />} onClick={startCreate}>
            新建
          </Button>
          <Button type="text" danger disabled={creating || !current} onClick={removeTemplate}>
            删除
          </Button>
          <Button type="primary" loading={saving} disabled={!dirty} onClick={save}>
            保存
          </Button>
        </Space>
      }
    >
      {!current ? null : (
        <>
          {/* 【这一页只管"有哪些字段、字段长什么样"】必填和照片张数是
              「提交规则」页的,放两处的话同一份数据有两个写入口,
              迟早互相冲掉而且不报错。后端也按这条规则拒收。 */}
          <div style={{ fontSize: 12.5, color: C.textFaint, marginBottom: 12 }}>
            必填与照片张数在「提交规则」里设置
          </div>
          {/* 【把"改不得"说在前面】不说的话人会改半天才被拒,
              而且不知道是自己填错了还是系统坏了。 */}
          {codeLocked && (
            <Alert
              type="info"
              showIcon
              style={{ marginBottom: 14 }}
              message="这个模板已经在用了,字段标识不能再改、也不能删"
              description="历史记录里的内容是按字段标识存的,改了标识或删了字段,那些记录里对应的内容就再也读不出来。改中文名称、加新字段、调顺序都不受影响。"
            />
          )}
          {assetByBuilder && (
            <Alert
              type="warning"
              showIcon
              style={{ marginBottom: 14 }}
              message="这个模板按读数拆成多台设备,不使用「设备编号」字段"
              description="它的资产归属由后台代码特判(一次巡检生成多台表计)。改动字段前请确认读数字段的标识没有变。"
            />
          )}

          <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit,minmax(150px,1fr))", gap: 12, marginBottom: 16 }}>
            {/* 【标识不给人填】它是永久的:一旦有记录就再也改不了。
                让人手填一个永久且不可改的键,等于把一次手滑变成永久债务。
                新建时后端生成,这里只在已有模板上显示出来备查。 */}
            {!creating && (
              <Labeled label="模板标识">
                <Input value={current.id} disabled />
              </Labeled>
            )}
            <Labeled label="模板名称">
              <Input value={current.name} onChange={(e) => patch({ ...current, name: e.target.value })} />
            </Labeled>
            <Labeled label="所属项目">
              {/* 【选而不是打】项目名是权限、数据范围、看板的关联键,
                  手打多一个空格就成了一个谁都看不见的孤儿项目,而且不报错。 */}
              <Select
                style={{ width: "100%" }}
                placeholder="选择项目"
                options={projects.map((p) => ({ value: p.name, label: p.name }))}
                value={current.project || undefined}
                onChange={(v) => patch({ ...current, project: v })}
              />
            </Labeled>
            <Labeled label="设备类型">
              {/* 【这里是"定义"类型,不是"引用"】所以是自由输入 + 唯一性校验,
                  而不是下拉 —— 下拉里选一个已有的反而会和别的模板撞车。
                  后台建资产时靠它反推模板,撞车的话第二个模板永远反推不到。 */}
              <Input
                value={current.assetType || ""}
                placeholder="如 有机房电梯;不能和别的模板重复"
                onChange={(e) => patch({ ...current, assetType: e.target.value })}
              />
            </Labeled>
          </div>

          <Table<TemplateFieldDTO>
            rowKey={(_, i) => String(i)}
            size="small"
            pagination={false}
            scroll={{ x: 900 }}
            dataSource={current.fields}
            columns={[
              {
                title: "字段标识",
                width: 170,
                fixed: "left",
                render: (_, f, i) => (
                  <Input
                    size="small"
                    value={f.code}
                    // 【已有记录的字段锁死】新加的那些还能改 —— 它们还没有数据
                    disabled={codeLocked && lockedCodes.has(f.code)}
                    onChange={(e) => patchField(i, "code", e.target.value.trim())}
                  />
                ),
              },
              {
                title: "名称",
                render: (_, f, i) => (
                  <Input size="small" value={f.label} onChange={(e) => patchField(i, "label", e.target.value)} />
                ),
              },
              {
                title: "类型",
                width: 110,
                render: (_, f, i) => (
                  <Select
                    size="small"
                    style={{ width: 100 }}
                    value={f.kind}
                    options={KIND_OPTIONS}
                    onChange={(v) => patchField(i, "kind", v)}
                  />
                ),
              },
              {
                title: "选项",
                width: 200,
                render: (_, f, i) =>
                  f.kind === "choice" ? (
                    <Select
                      size="small"
                      mode="tags"
                      style={{ width: "100%" }}
                      placeholder="回车添加,至少两个"
                      value={f.options || []}
                      onChange={(v) => patchField(i, "options", v)}
                    />
                  ) : (
                    <span style={{ color: C.textFaint, fontSize: 12 }}>—</span>
                  ),
              },
              {
                title: "来源",
                width: 110,
                render: (_, f, i) => (
                  <Select
                    size="small"
                    style={{ width: 100 }}
                    value={f.source || "manual"}
                    options={SOURCE_OPTIONS}
                    onChange={(v) => patchField(i, "source", v)}
                  />
                ),
              },
              {
                title: "",
                width: 50,
                render: (_, f, i) =>
                  // 已有记录的字段不给删 —— 删了历史记录里那一项就查不出来
                  codeLocked && lockedCodes.has(f.code) ? (
                    <span style={{ color: C.muted, fontSize: 12 }}>锁定</span>
                  ) : (
                    <Button
                      type="text"
                      size="small"
                      danger
                      icon={<DeleteOutlined />}
                      onClick={() => removeField(i)}
                    />
                  ),
              },
            ]}
          />
          <Button type="dashed" block icon={<PlusOutlined />} style={{ marginTop: 10 }} onClick={addField}>
            加一个字段
          </Button>
        </>
      )}
    </Card>
  );
}

function Labeled({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div>
      <div style={{ fontSize: 12, color: C.textFaint, marginBottom: 4 }}>{label}</div>
      {children}
    </div>
  );
}
