import { ThunderboltOutlined } from "@ant-design/icons";
import { Button, Input, Modal, Table, Tag, message } from "antd";
import { useState } from "react";

import { TemplateFieldDTO, draftTemplateFields } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 一段需求描述 → 一份检查项字段表。
 *
 * 【为什么产出的是字段表,不是一整段提示词】
 * "字段表 → 标准提示词"的渲染早就有(总则/字段映射/输出/置信度 那套格式)。
 * 让 AI 直接写整段提示词的话,要再解析回字段表才能细调 —— 而自然语言解析
 * 回结构化数据是有损的,解析不出来的部分会静默丢掉。
 * 反过来先出字段表、再渲染成提示词,两边无损,而且预览里看到的提示词
 * 和模型将来收到的一字不差。
 *
 * 【生成完不直接落库】先摆出来让人过一遍再决定采不采用。
 * 直接覆盖的话,一次没写清楚的需求会把已经调好的字段表冲掉。
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
  templateName,
  assetType,
  onAdopt,
}: {
  templateName?: string;
  assetType?: string;
  /** 采用之后由调用方决定怎么合并进当前模板 */
  onAdopt: (fields: TemplateFieldDTO[]) => void;
}) {
  const [open, setOpen] = useState(false);
  const [requirement, setRequirement] = useState("");
  const [busy, setBusy] = useState(false);
  const [draft, setDraft] = useState<TemplateFieldDTO[] | null>(null);
  const [model, setModel] = useState("");

  async function generate() {
    if (!requirement.trim()) {
      message.warning("先写一段要检查什么");
      return;
    }
    setBusy(true);
    try {
      const d = await draftTemplateFields({ requirement, templateName, assetType });
      setDraft(d.fields || []);
      setModel(d.model || "");
      setOpen(false); // 结果弹窗接管,两层弹窗叠着看不清
    } catch (e) {
      // 后端的理由已经是人话(没配密钥 / 需求太含糊 / 账户欠费),
      // 换成"生成失败"人不知道该改什么。
      message.error(e instanceof Error ? e.message : "生成失败");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      {/* 【入口是一个按钮,不是常驻的输入框】生成是偶尔做一次的事
          (建模板时用一次),而改判定依据是天天做的。把偶尔用的东西
          常驻在页面顶上,天天做的那件事就被推下去一屏。 */}
      <Button icon={<ThunderboltOutlined />} onClick={() => setOpen(true)}>
        AI 生成检查项
      </Button>

      <Modal
        open={open}
        title="描述要检查什么"
        okText="生成"
        cancelText="取消"
        confirmLoading={busy}
        onCancel={() => setOpen(false)}
        onOk={generate}
        width={620}
      >
        <Input.TextArea
          value={requirement}
          onChange={(e) => setRequirement(e.target.value)}
          autoSize={{ minRows: 4, maxRows: 10 }}
          placeholder="用大白话说就行。例:电梯机房巡检,看机房门有没有关好、警示标识齐不齐、地面有没有堆杂物、照明空调正不正常、灭火器有没有过期、设备有没有异响异味"
        />
        <div style={{ marginTop: 8, fontSize: 12.5, color: C.textFaint }}>
          生成之后会先摆出来给你过一遍,确认了才替换当前字段表
        </div>
      </Modal>

      <Modal
        open={!!draft}
        title={`生成了 ${draft?.length || 0} 个检查项`}
        width={860}
        onCancel={() => setDraft(null)}
        okText="采用这份"
        cancelText="重来"
        onOk={() => {
          if (draft) onAdopt(draft);
          setDraft(null);
          setRequirement("");
        }}
      >
        {/* 【先看再采用】这一步决定所有巡检员将来填什么、AI 判什么,
            扫一眼就点确定的话,一条判错的检查项会跟着每一次巡检。 */}
        <div style={{ fontSize: 12.5, color: C.textFaint, marginBottom: 10 }}>
          采用后会替换当前模板的字段表,保存前还能逐条改
          {model && ` · 由 ${model} 生成`}
        </div>
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
