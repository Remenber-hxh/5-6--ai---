import { ThunderboltOutlined } from "@ant-design/icons";
import { Button, Input, Modal, Table, Tag, message } from "antd";
import { useState } from "react";

import { TemplateFieldDTO, createReportTemplate, draftTemplateFields } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 一段需求描述 → 一份【新模板】。
 *
 * 【它是新增,不是修改】生成的检查项直接建成一个新模板入库,当场出现在
 * 模板下拉里。早先的做法是把生成结果覆盖到当前选中的模板上 —— 那等于
 * "想加一种巡检"的动作,做出来的是"把已有的那种改掉",而被改掉的那份
 * 判定规则已经在给现场的照片打分了。
 *
 * 【为什么产出的是字段表,不是一整段提示词】
 * "字段表 → 标准提示词"的渲染早就有(总则/字段映射/输出/置信度 那套格式)。
 * 让 AI 直接写整段提示词的话,要再解析回字段表才能细调 —— 而自然语言解析
 * 回结构化数据是有损的,解析不出来的部分会静默丢掉。
 * 反过来先出字段表、再渲染成提示词,两边无损,而且预览里看到的提示词
 * 和模型将来收到的一字不差。
 *
 * 【生成完不直接入库】先摆出来让人过一遍再决定采不采用。
 * 直接建的话,一次没写清楚的需求会在模板列表里留下一份垃圾模板 ——
 * 而模板一旦有记录就删不掉了。
 */

const MODE_LABEL: Record<string, string> = {
  visual: "看外观",
  visual_lenient: "看外观(宽松)",
  read_text: "读文字",
  objective_date: "查日期",
  functional_test: "现场测试照",
  sensory: "靠听/闻",
  system: "系统填",
  summary: "汇总",
};

export default function PromptDraft({
  onCreated,
}: {
  /** 新模板已经入库,把它的 id 交给调用方去选中 */
  onCreated: (templateId: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [requirement, setRequirement] = useState("");
  const [busy, setBusy] = useState(false);
  const [saving, setSaving] = useState(false);
  const [draft, setDraft] = useState<TemplateFieldDTO[] | null>(null);

  async function generate() {
    if (!name.trim()) {
      message.warning("先给这个模板起个名字");
      return;
    }
    if (!requirement.trim()) {
      message.warning("先写一段要检查什么");
      return;
    }
    setBusy(true);
    try {
      const d = await draftTemplateFields({ requirement, templateName: name });
      setDraft(d.fields || []);
      setOpen(false); // 结果弹窗接管,两层弹窗叠着看不清
    } catch (e) {
      // 后端的理由已经是人话(没配密钥 / 需求太含糊 / 账户欠费),
      // 换成"生成失败"人不知道该改什么。
      message.error(e instanceof Error ? e.message : "生成失败");
    } finally {
      setBusy(false);
    }
  }

  // 采用 = 建库。
  //
  // 【项目和设备类型这里不填】设备类型要求全局唯一,填错会和别的模板撞车,
  // 而撞车的后果(后台建的设备认错模板)要到很久以后才暴露。留空是安全的,
  // 建完到「巡检模板」页配 —— 那一页有唯一性校验和现成的项目下拉。
  async function adopt() {
    if (!draft) return;
    setSaving(true);
    try {
      const d = await createReportTemplate({
        id: "", // 后端生成,不让人手填一个永久且改不了的键
        name: name.trim(),
        project: "",
        assetType: "",
        maxImages: 20,
        minImages: 5,
        fields: draft,
      });
      const id = d.template?.id || "";
      message.success("已建好,可以逐条改判定依据了");
      setDraft(null);
      setName("");
      setRequirement("");
      if (id) onCreated(id);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "建立失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Button icon={<ThunderboltOutlined />} onClick={() => setOpen(true)}>
        AI 新建模板
      </Button>

      <Modal
        open={open}
        title="新建模板"
        okText="生成"
        cancelText="取消"
        confirmLoading={busy}
        onCancel={() => setOpen(false)}
        onOk={generate}
        width={620}
      >
        <Input
          value={name}
          onChange={(e) => setName(e.target.value)}
          placeholder="模板名称,如 电梯机房巡检"
          style={{ marginBottom: 10 }}
        />
        <Input.TextArea
          value={requirement}
          onChange={(e) => setRequirement(e.target.value)}
          autoSize={{ minRows: 4, maxRows: 10 }}
          placeholder="要检查什么,用大白话说。例:看机房门有没有关好、警示标识齐不齐、地面有没有堆杂物、灭火器有没有过期"
        />
      </Modal>

      <Modal
        open={!!draft}
        title={`生成了 ${draft?.length || 0} 个检查项`}
        width={860}
        onCancel={() => setDraft(null)}
        okText="建立模板"
        cancelText="重来"
        confirmLoading={saving}
        onOk={adopt}
      >
        {/* 【先看再入库】这一步决定所有巡检员将来填什么、AI 判什么,
            扫一眼就点确定的话,一条判错的检查项会跟着每一次巡检。 */}
        <Table<TemplateFieldDTO>
          rowKey="code"
          size="small"
          pagination={false}
          scroll={{ x: 760, y: 420 }}
          dataSource={draft || []}
          columns={[
            {
              title: "检查项",
              width: 190,
              fixed: "left",
              render: (_, f) => (
                <div>
                  <div>{f.label}</div>
                  <div style={{ color: C.textFaint, fontSize: 12 }}>{f.code}</div>
                </div>
              ),
            },
            {
              title: "怎么判",
              width: 120,
              render: (_, f) => {
                const m = f.judgeMode || "";
                // 【靠听/闻的要标出来】照片判不了这类,系统会留给人工 ——
                // 不标的话人会以为 AI 也能判,现场就不会去听、去闻。
                const soft = m === "sensory";
                return (
                  <Tag color={soft ? "orange" : undefined}>{MODE_LABEL[m] || m || "—"}</Tag>
                );
              },
            },
            {
              title: "判「是」看什么",
              render: (_, f) => (
                <span style={{ fontSize: 12.5, color: C.textSub }}>{f.yesWhen || "—"}</span>
              ),
            },
            {
              title: "判「否」看什么",
              render: (_, f) => (
                <span style={{ fontSize: 12.5, color: C.textSub }}>{f.noWhen || "—"}</span>
              ),
            },
          ]}
        />
      </Modal>
    </>
  );
}
