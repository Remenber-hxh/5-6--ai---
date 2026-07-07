import { PlusOutlined } from "@ant-design/icons";
import { Button, Card, Form, Input, Modal, Popconfirm, Select, Space, Table, Tag, message } from "antd";
import { useEffect, useState } from "react";

import {
  UserEntry,
  createUser,
  listRoles,
  listUsers,
  resetUserPassword,
  setUserStatus,
  updateUser,
} from "../api/mgmt";
import { fmtTime } from "../lib/status";

const roleTag = (code?: string, name?: string) => {
  const color =
    code === "admin" ? "geekblue" : code === "manager" || code === "supervisor" ? "cyan" : "default";
  return <Tag color={color}>{name || code || "—"}</Tag>;
};

const FALLBACK_ROLES = [
  { code: "admin", name: "管理员" },
  { code: "manager", name: "经理" },
  { code: "supervisor", name: "主管" },
  { code: "inspector", name: "巡检员" },
];

export default function Users() {
  const [users, setUsers] = useState<UserEntry[]>([]);
  const [roles, setRoles] = useState(FALLBACK_ROLES);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<UserEntry | null | "new">(null);
  const [pwdUser, setPwdUser] = useState<UserEntry | null>(null);
  const [form] = Form.useForm();
  const [pwdForm] = Form.useForm();

  async function load() {
    setLoading(true);
    try {
      setUsers(await listUsers());
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
    listRoles()
      .then((rs) => rs.length && setRoles(rs))
      .catch(() => void 0);
  }, []);

  function openEdit(u: UserEntry | "new") {
    setEditing(u);
    if (u === "new") form.resetFields();
    else form.setFieldsValue({ username: u.username, displayName: u.displayName, roleCode: u.roleCode });
  }

  async function submit() {
    const v = await form.validateFields();
    try {
      if (editing === "new") {
        await createUser(v);
        message.success("用户已创建");
      } else if (editing) {
        await updateUser(editing.id, { displayName: v.displayName, roleCode: v.roleCode });
        message.success("用户已更新");
      }
      setEditing(null);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    }
  }

  async function submitPwd() {
    const v = await pwdForm.validateFields();
    if (!pwdUser) return;
    try {
      await resetUserPassword(pwdUser.id, v.password);
      message.success(`已为 ${pwdUser.username} 重置密码`);
      setPwdUser(null);
      pwdForm.resetFields();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "重置失败");
    }
  }

  async function toggle(u: UserEntry) {
    const next = u.status === "active" || !u.status ? "disabled" : "active";
    try {
      await setUserStatus(u.id, next);
      message.success(`已${next === "disabled" ? "停用" : "启用"} ${u.username}`);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  return (
    <Card
      title="用户与权限"
      extra={
        <Button type="primary" icon={<PlusOutlined />} onClick={() => openEdit("new")}>
          新建用户
        </Button>
      }
    >
      <Table<UserEntry>
        rowKey="id"
        size="middle"
        loading={loading}
        dataSource={users}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 人` }}
        columns={[
          { title: "姓名", dataIndex: "displayName" },
          { title: "账号", dataIndex: "username", width: 150 },
          { title: "角色", width: 130, render: (_, u) => roleTag(u.roleCode, u.roleName) },
          {
            title: "状态",
            width: 90,
            render: (_, u) =>
              u.status === "disabled" ? <Tag>停用</Tag> : <Tag color="green">启用</Tag>,
          },
          { title: "创建时间", width: 150, render: (_, u) => fmtTime(u.createdAt, true) },
          {
            title: "操作",
            width: 220,
            render: (_, u) => (
              <Space>
                <a onClick={() => openEdit(u)}>编辑</a>
                <a onClick={() => setPwdUser(u)}>重置密码</a>
                <Popconfirm
                  title={`确认${u.status === "disabled" ? "启用" : "停用"}「${u.displayName}」?`}
                  onConfirm={() => toggle(u)}
                >
                  <a style={{ color: u.status === "disabled" ? undefined : "#d4380d" }}>
                    {u.status === "disabled" ? "启用" : "停用"}
                  </a>
                </Popconfirm>
              </Space>
            ),
          },
        ]}
      />
      <Modal
        title={editing === "new" ? "新建用户" : "编辑用户"}
        open={!!editing}
        onOk={submit}
        onCancel={() => setEditing(null)}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item name="username" label="账号" rules={[{ required: true, message: "请输入账号" }]}>
            <Input disabled={editing !== "new"} autoComplete="off" />
          </Form.Item>
          <Form.Item name="displayName" label="姓名" rules={[{ required: true, message: "请输入姓名" }]}>
            <Input />
          </Form.Item>
          <Form.Item name="roleCode" label="角色" rules={[{ required: true, message: "请选择角色" }]}>
            <Select options={roles.map((r) => ({ value: r.code, label: r.name }))} />
          </Form.Item>
          {editing === "new" && (
            <Form.Item
              name="password"
              label="初始密码"
              rules={[{ required: true, min: 6, message: "至少 6 位" }]}
            >
              <Input.Password autoComplete="new-password" />
            </Form.Item>
          )}
        </Form>
      </Modal>
      <Modal
        title={`重置密码 · ${pwdUser?.displayName || ""}`}
        open={!!pwdUser}
        onOk={submitPwd}
        onCancel={() => setPwdUser(null)}
        destroyOnHidden
      >
        <Form form={pwdForm} layout="vertical" requiredMark={false}>
          <Form.Item name="password" label="新密码" rules={[{ required: true, min: 6, message: "至少 6 位" }]}>
            <Input.Password autoComplete="new-password" />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
