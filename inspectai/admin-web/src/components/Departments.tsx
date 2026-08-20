import { PlusOutlined } from "@ant-design/icons";
import { Button, Card, Form, Input, Modal, Popconfirm, Space, Table, Tag, Typography, message } from "antd";
import { useEffect, useState } from "react";

import {
  Department,
  UserEntry,
  createDepartment,
  deleteDepartment,
  listDepartments,
  updateDepartment,
} from "../api/mgmt";

/**
 * 部门管理。
 *
 * 【在此之前这个功能等于不存在】departments 表只有一条种子「默认部门」，
 * 后端没有任何写接口，所以新建用户时的部门下拉永远只有一项。
 *
 * 【人数一列是有用的，不是装饰】部门下还有人时后端会拒绝删除，
 * 把人数直接摆出来，管理员点删除之前就知道会不会被拦。
 */
export default function Departments({ users, onChanged }: { users: UserEntry[]; onChanged?: () => void }) {
  const [list, setList] = useState<Department[]>([]);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<Department | "new" | null>(null);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  async function load() {
    setLoading(true);
    try {
      setList(await listDepartments());
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  function countUsers(id: string) {
    return users.filter((u) => u.departmentId === id).length;
  }

  function openEdit(d: Department | "new") {
    setEditing(d);
    if (d === "new") form.resetFields();
    else form.setFieldsValue({ name: d.name });
  }

  async function submit() {
    const v = await form.validateFields();
    setSaving(true);
    try {
      if (editing === "new") await createDepartment({ name: v.name.trim() });
      else if (editing) await updateDepartment(editing.id, v.name.trim());
      message.success("已保存");
      setEditing(null);
      await load();
      onChanged?.();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSaving(false);
    }
  }

  async function remove(d: Department) {
    try {
      await deleteDepartment(d.id);
      message.success(`已删除「${d.name}」`);
      await load();
      onChanged?.();
    } catch (e) {
      // 【原样显示后端的话】那句话里带着下一步动作（"请先把用户调到其他部门"），
      // 自己编一句"删除失败"等于把它丢掉，管理员只能反复点。
      message.error(e instanceof Error ? e.message : "删除失败");
    }
  }

  return (
    <Card
      title="部门"
      style={{ marginTop: 16 }}
      extra={
        <Button icon={<PlusOutlined />} onClick={() => openEdit("new")}>
          新建部门
        </Button>
      }
    >
      <Typography.Paragraph type="secondary" style={{ marginTop: -8 }}>
        新建用户时可以选择部门。部门下还有人时不能删除，请先把人调走。
      </Typography.Paragraph>
      <Table<Department>
        rowKey="id"
        size="middle"
        loading={loading}
        dataSource={list}
        pagination={false}
        columns={[
          { title: "部门名称", dataIndex: "name" },
          {
            title: "人数",
            width: 110,
            render: (_, d) => {
              const n = countUsers(d.id);
              return n > 0 ? `${n} 人` : <span style={{ color: "#999" }}>暂无</span>;
            },
          },
          {
            title: "操作",
            width: 140,
            render: (_, d) => {
              // 默认部门是新建用户的兜底归属，删了之后建出来的人没有部门。
              // 后端也挡着，这里一并置灰是为了让人不用点一次才知道。
              const isDefault = d.id === "dept_default";
              const n = countUsers(d.id);
              return (
                <Space>
                  <a onClick={() => openEdit(d)}>重命名</a>
                  {isDefault ? (
                    <Tag>默认</Tag>
                  ) : n > 0 ? (
                    <span style={{ color: "#bbb" }}>删除</span>
                  ) : (
                    <Popconfirm title={`确认删除「${d.name}」?`} onConfirm={() => remove(d)}>
                      <a style={{ color: "#d4380d" }}>删除</a>
                    </Popconfirm>
                  )}
                </Space>
              );
            },
          },
        ]}
      />
      <Modal
        title={editing === "new" ? "新建部门" : "重命名部门"}
        open={!!editing}
        onOk={submit}
        confirmLoading={saving}
        onCancel={() => setEditing(null)}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item name="name" label="部门名称" rules={[{ required: true, message: "请输入部门名称" }]}>
            <Input placeholder="例如：运维部" maxLength={40} />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
