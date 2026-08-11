import { CopyOutlined, PlusOutlined } from "@ant-design/icons";
import {
  Button,
  Card,
  Form,
  Input,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Table,
  Tag,
  Tooltip,
  Typography,
  message,
} from "antd";
import { useEffect, useState } from "react";

import {
  RegistrationCodeEntry,
  RoleEntry,
  createRegistrationCode,
  listRegistrationCodes,
  setRegistrationCodeDisabled,
} from "../api/mgmt";
import { fmtTime } from "../lib/status";

/**
 * 注册码管理。
 *
 * 巡检员自助注册的唯一门槛 —— 后端 /api/assets 对任何已登录用户开放,
 * 一张码流出去,拿到的人就能看到客户的全部设备台账和健康状态。所以这一屏
 * 的重点不是"生成",而是让管理员随时看清【谁还能用、用了多少次】并能一键停用。
 */
export default function RegistrationCodes({ roles }: { roles: RoleEntry[] }) {
  const [codes, setCodes] = useState<RegistrationCodeEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  async function reload() {
    setLoading(true);
    try {
      setCodes(await listRegistrationCodes());
    } catch (e) {
      message.error(e instanceof Error ? e.message : "注册码加载失败");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void reload();
  }, []);

  async function submit() {
    const v = await form.validateFields();
    setSaving(true);
    try {
      const created = await createRegistrationCode({
        roleCode: v.roleCode,
        maxUses: v.maxUses ?? 0,
        expiresInDays: v.expiresInDays ?? 0,
        note: v.note,
      });
      setCreating(false);
      form.resetFields();
      await reload();
      // 生成完直接把码摆出来让人复制 —— 管理员的下一步一定是把它发给班组,
      // 让他去表格里找刚生成的那一行是多余的一步。
      Modal.success({
        title: "注册码已生成",
        content: (
          <div>
            <Typography.Title level={3} copyable style={{ margin: "12px 0" }}>
              {created.code}
            </Typography.Title>
            <div style={{ color: "#8aa0b0", fontSize: 12.5 }}>
              发给班组即可注册。可用次数
              {created.maxUses > 0 ? ` ${created.maxUses} 次` : "不限"}。
            </div>
          </div>
        ),
      });
    } catch (e) {
      message.error(e instanceof Error ? e.message : "生成失败");
    } finally {
      setSaving(false);
    }
  }

  async function toggle(rc: RegistrationCodeEntry) {
    try {
      await setRegistrationCodeDisabled(rc.id, !rc.disabled);
      await reload();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  // 管理员角色不给选:后端也会拒,但这里先藏起来,免得管理员选了再被打回来
  // 才知道不行 —— 界面能做到的事不该留给报错去说。
  const selectableRoles = roles.filter((r) => r.code !== "admin");

  return (
    <Card
      title="注册码"
      style={{ marginTop: 16 }}
      extra={
        <Button
          icon={<PlusOutlined />}
          onClick={() => {
            form.resetFields();
            setCreating(true);
          }}
        >
          生成注册码
        </Button>
      }
    >
      <div style={{ marginBottom: 12, color: "#8aa0b0", fontSize: 12.5 }}>
        班组成员用注册码在手机端自助开通账号，角色和部门由码决定。
        码外泄时点「停用」即刻失效。
      </div>

      <Table<RegistrationCodeEntry>
        rowKey="id"
        size="middle"
        loading={loading}
        dataSource={codes}
        pagination={{ pageSize: 10, showTotal: (t) => `共 ${t} 个` }}
        columns={[
          {
            title: "注册码",
            width: 160,
            render: (_, rc) => (
              <Typography.Text
                strong
                copyable={{ text: rc.code, icon: <CopyOutlined /> }}
                delete={rc.disabled}
              >
                {rc.code}
              </Typography.Text>
            ),
          },
          {
            title: "角色",
            width: 120,
            render: (_, rc) =>
              roles.find((r) => r.code === rc.roleCode)?.name || rc.roleCode,
          },
          {
            title: "已用 / 上限",
            width: 120,
            render: (_, rc) => (
              <span>
                {rc.usedCount} / {rc.maxUses > 0 ? rc.maxUses : "不限"}
              </span>
            ),
          },
          {
            title: "有效期",
            width: 150,
            render: (_, rc) =>
              rc.expiresAt ? fmtTime(rc.expiresAt, true) : "长期有效",
          },
          {
            title: "状态",
            width: 100,
            render: (_, rc) => {
              if (rc.disabled) return <Tag>已停用</Tag>;
              if (rc.maxUses > 0 && rc.usedCount >= rc.maxUses)
                return <Tag color="orange">已用完</Tag>;
              if (rc.expiresAt && new Date(rc.expiresAt) < new Date())
                return <Tag color="orange">已过期</Tag>;
              return <Tag color="green">可用</Tag>;
            },
          },
          {
            title: "备注",
            render: (_, rc) => (
              <Tooltip title={rc.note}>
                <span style={{ color: "#8aa0b0", fontSize: 12.5 }}>
                  {rc.note || "—"}
                </span>
              </Tooltip>
            ),
          },
          { title: "创建时间", width: 150, render: (_, rc) => fmtTime(rc.createdAt, true) },
          {
            title: "操作",
            width: 90,
            render: (_, rc) =>
              rc.disabled ? (
                <Button type="link" size="small" onClick={() => void toggle(rc)}>
                  启用
                </Button>
              ) : (
                <Popconfirm
                  title="停用这个注册码?"
                  description="停用后立即失效,已经注册的账号不受影响。"
                  okText="停用"
                  okButtonProps={{ danger: true }}
                  onConfirm={() => void toggle(rc)}
                >
                  <Button type="link" size="small" danger>
                    停用
                  </Button>
                </Popconfirm>
              ),
          },
        ]}
      />

      <Modal
        open={creating}
        title="生成注册码"
        okText="生成"
        confirmLoading={saving}
        onOk={() => void submit()}
        onCancel={() => setCreating(false)}
        destroyOnClose
      >
        <Form form={form} layout="vertical" style={{ marginTop: 16 }}>
          <Form.Item
            name="roleCode"
            label="注册出来的角色"
            initialValue="inspector"
            rules={[{ required: true, message: "请选择角色" }]}
            extra="管理员账号不能用注册码创建,请在上面的用户列表里手动新建"
          >
            <Select
              options={selectableRoles.map((r) => ({
                value: r.code,
                label: r.name,
              }))}
            />
          </Form.Item>
          <Form.Item
            name="maxUses"
            label="可用次数"
            initialValue={5}
            extra="0 表示不限次数。发给固定班组建议填人数,发给外包建议填 1"
          >
            <InputNumber min={0} max={999} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item
            name="expiresInDays"
            label="有效天数"
            initialValue={30}
            extra="0 表示长期有效。长期有效的码一旦外泄，只能靠手动停用"
          >
            <InputNumber min={0} max={365} style={{ width: "100%" }} />
          </Form.Item>
          <Form.Item name="note" label="备注">
            <Input placeholder="例如：会议中心 8 月新增班组" maxLength={60} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
