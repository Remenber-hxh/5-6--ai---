import { Button, Card, DatePicker, Form, Input, InputNumber, Modal, Typography, message } from "antd";
import dayjs from "dayjs";
import { useState } from "react";

import { AssetEntry, saveAssetProfile } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 设备静态档案:厂家、型号、投运日期、维保周期。
 *
 * 【为什么这几项值得单开一块】趋势只有相对值,没有绝对判断 ——
 * 供水压力 0.55 MPa 是低还是正常?看曲线只知道"比平时低 8%",
 * 而"是不是已经低到该报修"要对着设计值和设备年限才说得出来。
 *
 * 【维保周期是唯一一个直接参与判断的】其余几项是给人看的背景,
 * 而它能算出"距上次维保已超期 242 天" —— 那句话不需要人懂设备也能行动。
 */

const DATE_FMT = "YYYY-MM-DD";

/**
 * 投运至今多久,一句人话。
 *
 * 【不要出现「已 0 年」】那读起来像系统算错了,而不是"今年刚投运"。
 * 不满一年就说月份;当月投运就直说,一个数字都不给 —— 没有信息量的
 * 数字比不给更糟,它会让人怀疑旁边那些数字准不准。
 */
function ageText(commissionedAt?: string): string {
  if (!commissionedAt) return "";
  const d = dayjs(commissionedAt, DATE_FMT);
  if (!d.isValid()) return "";
  const y = dayjs().diff(d, "year");
  if (y > 0) return `已 ${y} 年`;
  const m = dayjs().diff(d, "month");
  if (m > 0) return `已 ${m} 个月`;
  // 未来日期(填错了)也走这里 —— 不编一个负数出来
  return dayjs().isBefore(d) ? "" : "本月投运";
}

export default function AssetProfileCard({
  asset,
  onSaved,
}: {
  asset: AssetEntry;
  onSaved: (next: AssetEntry) => void;
}) {
  const [editing, setEditing] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  const age = ageText(asset.commissionedAt);
  const rows: { label: string; value: React.ReactNode }[] = [];
  if (asset.manufacturer) rows.push({ label: "厂家", value: asset.manufacturer });
  if (asset.model) rows.push({ label: "型号", value: asset.model });
  if (asset.commissionedAt) {
    rows.push({
      label: "投运",
      // 【年限比日期有用】"2011-06-01"要人自己减一次;"已投运 15 年"直接就是判断依据。
      value: (
        <span style={{ fontVariantNumeric: "tabular-nums" }}>
          {asset.commissionedAt}
          {age && <span style={{ color: C.textFaint, marginLeft: 6 }}>{age}</span>}
        </span>
      ),
    });
  }
  if (asset.lastMaintainedAt) {
    rows.push({
      label: "上次维保",
      value: <span style={{ fontVariantNumeric: "tabular-nums" }}>{asset.lastMaintainedAt}</span>,
    });
  }
  if (asset.maintenanceCycleDays) {
    rows.push({
      label: "维保周期",
      value: (
        <span style={{ fontVariantNumeric: "tabular-nums" }}>{asset.maintenanceCycleDays} 天</span>
      ),
    });
  }
  if (asset.assetNote) rows.push({ label: "备注", value: asset.assetNote });

  function open() {
    form.setFieldsValue({
      manufacturer: asset.manufacturer || "",
      model: asset.model || "",
      commissionedAt: asset.commissionedAt ? dayjs(asset.commissionedAt, DATE_FMT) : null,
      lastMaintainedAt: asset.lastMaintainedAt ? dayjs(asset.lastMaintainedAt, DATE_FMT) : null,
      maintenanceCycleDays: asset.maintenanceCycleDays || 0,
      assetNote: asset.assetNote || "",
    });
    setEditing(true);
  }

  async function submit() {
    const v = await form.validateFields();
    setSaving(true);
    try {
      const next = await saveAssetProfile(asset.id, {
        manufacturer: v.manufacturer || "",
        model: v.model || "",
        // 日期控件给 dayjs 对象,后端要 YYYY-MM-DD 字符串;清空则传空串
        commissionedAt: v.commissionedAt ? v.commissionedAt.format(DATE_FMT) : "",
        lastMaintainedAt: v.lastMaintainedAt ? v.lastMaintainedAt.format(DATE_FMT) : "",
        maintenanceCycleDays: v.maintenanceCycleDays ?? 0,
        assetNote: v.assetNote || "",
      });
      message.success("已保存");
      setEditing(false);
      onSaved(next);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  return (
    <>
      <Card
        size="small"
        title="设备档案"
        extra={
          <Button type="text" size="small" onClick={open}>
            {rows.length ? "编辑" : "补充资料"}
          </Button>
        }
      >
        {rows.length === 0 ? (
          // 【这一处的空态要留着】和"没有趋势"不同:那是系统算不出来,
          // 这是【等着人去填】—— 说出来才有人会去补,而这批资料正是
          // 要向甲方索要的东西,不提示的话没人记得这里缺一块。
          <Typography.Text type="secondary" style={{ fontSize: 13 }}>
            还没有厂家、投运日期、维保周期等资料。填上之后,读数才有绝对参照,
            维保超期也能自动提醒。
          </Typography.Text>
        ) : (
          // 【标签列右对齐、正文左对齐】两列各自贴着中缝,眼睛只需要
          // 沿一条线往下扫;标签左对齐的话,长短不一的标签会把值列
          // 推得参差,五行看起来像五个不同的东西。
          // 【按可用宽度自动分列】这张卡在档案页的右栏里只有 300px 宽(一列),
          // 窄屏落回单栏时却有整页宽 —— 还按一列排的话,五行短字段又变成
          // "九成是空白",正是要改掉的那个毛病。auto-fit 让同一份代码
          // 在两种宽度下都排得满。
          <div
            style={{
              display: "grid",
              gap: "9px 24px",
              gridTemplateColumns: "repeat(auto-fit, minmax(210px, 1fr))",
            }}
          >
            {rows.map((r) => (
              <div
                key={r.label}
                style={{
                  display: "grid",
                  gridTemplateColumns: "auto 1fr",
                  gap: 12,
                  fontSize: 13.5,
                  alignItems: "baseline",
                }}
              >
                <span style={{ color: C.textFaint, textAlign: "right", minWidth: 56 }}>
                  {r.label}
                </span>
                <span style={{ color: C.text, minWidth: 0, wordBreak: "break-word" }}>
                  {r.value}
                </span>
              </div>
            ))}
          </div>
        )}
      </Card>

      <Modal
        title="设备档案"
        open={editing}
        destroyOnHidden
        confirmLoading={saving}
        onCancel={() => setEditing(false)}
        onOk={submit}
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item name="manufacturer" label="厂家">
            <Input placeholder="如:通力电梯" />
          </Form.Item>
          <Form.Item name="model" label="型号">
            <Input />
          </Form.Item>
          <Form.Item name="commissionedAt" label="投运日期">
            <DatePicker style={{ width: "100%" }} format={DATE_FMT} />
          </Form.Item>
          <Form.Item name="lastMaintainedAt" label="上次维保">
            <DatePicker style={{ width: "100%" }} format={DATE_FMT} />
          </Form.Item>
          {/* 【说清 0 的含义】不说的话,人会把 0 理解成"不用维保",
              而系统的行为是"不判断" —— 两者在界面上看不出区别。 */}
          <Form.Item
            name="maintenanceCycleDays"
            label="维保周期"
            extra="填了才会在超期时提醒;0 = 不设定,不提醒"
          >
            <InputNumber min={0} max={3650} style={{ width: "100%" }} addonAfter="天" />
          </Form.Item>
          <Form.Item name="assetNote" label="备注">
            <Input.TextArea rows={2} />
          </Form.Item>
        </Form>
      </Modal>
    </>
  );
}
