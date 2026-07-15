import { PlusOutlined, SaveOutlined } from "@ant-design/icons";
import { Button, Card, Checkbox, Form, Input, Modal, Popconfirm, Select, Skeleton, Space, Table, Tag, Tooltip, message } from "antd";
import { useEffect, useState } from "react";

import {
  Department,
  PermDef,
  UserEntry,
  createUser,
  getPermissions,
  listDepartments,
  listRoles,
  listUsers,
  resetUserPassword,
  savePermissions,
  setUserStatus,
  updateUser,
} from "../api/mgmt";
import { useAuth } from "../store/auth";
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

// 权限矩阵可配置的角色列(admin 恒全通过,只展示不可改)
const MATRIX_ROLES = [
  { code: "manager", name: "经理" },
  { code: "supervisor", name: "主管" },
  { code: "inspector", name: "巡检员" },
];

export default function Users() {
  const { user: me } = useAuth();
  const [users, setUsers] = useState<UserEntry[]>([]);
  const [roles, setRoles] = useState(FALLBACK_ROLES);
  const [depts, setDepts] = useState<Department[]>([]);
  const [permCatalog, setPermCatalog] = useState<PermDef[]>([]);
  const [matrix, setMatrix] = useState<Record<string, string[]>>({});
  const [savingPerms, setSavingPerms] = useState(false);
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
    listDepartments().then(setDepts).catch(() => void 0);
    if (me?.roleCode === "admin") {
      getPermissions()
        .then((d) => {
          setPermCatalog(d.catalog || []);
          setMatrix(d.matrix || {});
        })
        .catch(() => void 0);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function togglePerm(permKey: string, roleCode: string, checked: boolean) {
    setMatrix((m) => {
      const cur = new Set(m[permKey] || []);
      if (checked) cur.add(roleCode);
      else cur.delete(roleCode);
      return { ...m, [permKey]: Array.from(cur) };
    });
  }

  async function submitPerms() {
    setSavingPerms(true);
    try {
      const res = await savePermissions(matrix);
      setMatrix(res.matrix || matrix);
      message.success("权限矩阵已保存,接口立即生效;菜单可见性在下次登录后更新");
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    } finally {
      setSavingPerms(false);
    }
  }

  function openEdit(u: UserEntry | "new") {
    setEditing(u);
    if (u === "new") form.resetFields();
    else
      form.setFieldsValue({
        username: u.username,
        displayName: u.displayName,
        roleCode: u.roleCode,
        departmentId: u.departmentId || undefined,
      });
  }

  async function submit() {
    const v = await form.validateFields();
    try {
      if (editing === "new") {
        await createUser(v);
        message.success("用户已创建");
      } else if (editing) {
        await updateUser(editing.id, { displayName: v.displayName, roleCode: v.roleCode, departmentId: v.departmentId });
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

  if (loading && users.length === 0) {
    return (
      <Card title="用户与权限">
        <Skeleton active paragraph={{ rows: 6 }} />
      </Card>
    );
  }

  return (
    <>
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
          { title: "部门", width: 130, render: (_, u) => u.departmentName || "—" },
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
          <Form.Item name="departmentId" label="部门">
            <Select
              allowClear
              placeholder="默认部门"
              options={depts.map((d) => ({ value: d.id, label: d.name }))}
            />
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
    {me?.roleCode === "admin" && permCatalog.length > 0 && (
      <Card
        title="角色权限矩阵"
        style={{ marginTop: 16 }}
        extra={
          <Button type="primary" icon={<SaveOutlined />} loading={savingPerms} onClick={submitPerms}>
            保存
          </Button>
        }
      >
        <Table<PermDef>
          rowKey="key"
          size="middle"
          pagination={false}
          dataSource={permCatalog}
          columns={[
            {
              title: "能力",
              width: 220,
              render: (_, p) => (
                <Tooltip title={p.desc}>
                  <span>
                    {p.label}
                    {p.locked && <Tag style={{ marginLeft: 8 }}>固定</Tag>}
                  </span>
                </Tooltip>
              ),
            },
            {
              title: "管理员",
              width: 90,
              align: "center" as const,
              render: () => <Checkbox checked disabled />,
            },
            ...MATRIX_ROLES.map((role) => ({
              title: role.name,
              width: 90,
              align: "center" as const,
              render: (_: unknown, p: PermDef) => (
                <Checkbox
                  disabled={p.locked}
                  checked={(matrix[p.key] || []).includes(role.code)}
                  onChange={(e) => togglePerm(p.key, role.code, e.target.checked)}
                />
              ),
            })),
            {
              title: "说明",
              render: (_, p) => <span style={{ color: "#8aa0b0", fontSize: 12.5 }}>{p.desc}</span>,
            },
          ]}
        />
      </Card>
    )}
    </>
  );
}
