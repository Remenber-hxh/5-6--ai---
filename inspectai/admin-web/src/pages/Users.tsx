import { PlusOutlined, SaveOutlined } from "@ant-design/icons";
import { Button, Card, Checkbox, Col, Form, Input, Menu, Modal, Popconfirm, Row, Select, Skeleton, Space, Table, Tag, Tooltip, message } from "antd";
import { useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";

import {
  Department,
  PermDef,
  RoleEntry,
  UserEntry,
  createRole,
  createUser,
  deleteRole,
  getPermissions,
  listDepartments,
  listRoles,
  listUsers,
  resetUserPassword,
  savePermissions,
  setUserStatus,
  updateRole,
  updateUser,
  deleteUser,
  listProjects,
  listUserProjects,
  setUserProjects,
  type ProjectEntry,
} from "../api/mgmt";
import Departments from "../components/Departments";
import Projects from "../components/Projects";
import RegistrationCodes from "../components/RegistrationCodes";
import { useAuth } from "../store/auth";
import { fmtTime } from "../lib/status";

const roleTag = (code?: string, name?: string) => {
  const color =
    code === "admin" ? "geekblue" : code === "manager" || code === "supervisor" ? "cyan" : "default";
  return <Tag color={color}>{name || code || "—"}</Tag>;
};

const FALLBACK_ROLES: RoleEntry[] = [
  { id: "role_admin", code: "admin", name: "管理员", builtin: true },
  { id: "role_manager", code: "manager", name: "经理", builtin: true },
  { id: "role_supervisor", code: "supervisor", name: "主管", builtin: true },
  { id: "role_inspector", code: "inspector", name: "巡检员", builtin: true },
];

export default function Users() {
  const { user: me } = useAuth();
  const [users, setUsers] = useState<UserEntry[]>([]);
  const [roles, setRoles] = useState<RoleEntry[]>(FALLBACK_ROLES);
  const [roleEditing, setRoleEditing] = useState<RoleEntry | null | "new">(null);
  const [roleForm] = Form.useForm();
  const [depts, setDepts] = useState<Department[]>([]);
  const [permCatalog, setPermCatalog] = useState<PermDef[]>([]);
  const [matrix, setMatrix] = useState<Record<string, string[]>>({});
  const [savingPerms, setSavingPerms] = useState(false);
  const [loading, setLoading] = useState(true);
  const [editing, setEditing] = useState<UserEntry | null | "new">(null);
  const [pwdUser, setPwdUser] = useState<UserEntry | null>(null);
  const [form] = Form.useForm();
  const [pwdForm] = Form.useForm();
  const [projects, setProjects] = useState<ProjectEntry[]>([]);

  // ===== 页内左侧导航 =====
  //
  // 这一页原来是六张卡片从上到下堆一屏:用户、角色、权限矩阵、部门、项目、注册码。
  // 想找注册码要一路滚到最底,而且滚下去之后完全不知道自己在哪一块。
  //
  // 拆成六个菜单项挂到侧栏又会让侧栏更长 —— 那才是真正该保持精简的地方。
  // 所以用页内导航:侧栏条目数不变,这一页内部却分得清清楚楚(GitHub Settings 就是这样)。
  const isAdmin = me?.roleCode === "admin";
  const paneNav = useMemo(
    () =>
      [
        { key: "users", label: "用户" },
        // 【非管理员看不到的块,菜单项也不能留】留着的话点进去是一片空白,
        // 看起来像页面坏了 —— 而不是"你没这个权限"。
        ...(isAdmin
          ? [
              { key: "roles", label: "角色管理" },
              { key: "matrix", label: "权限矩阵" },
              { key: "dept", label: "部门" },
              { key: "project", label: "项目" },
              { key: "invite", label: "注册码" },
            ]
          : []),
      ],
    [isAdmin],
  );

  // 标签状态写进地址栏:刷新留在原处,也能把「注册码」的链接直接发给同事。
  // 用 replace 不用 push —— 切来切去不该往浏览器历史里堆。
  const [params, setParams] = useSearchParams();
  const raw = params.get("tab") || "users";
  // 【地址栏里的值可能是他看不了的那一块】比如经理点开别人发来的 ?tab=matrix。
  // 不兜底的话右边一片空白,左边也没有对应的选中项。
  const tab = paneNav.some((n) => n.key === raw) ? raw : "users";
  const setTab = (k: string) => {
    const next = new URLSearchParams(params);
    if (k === "users") next.delete("tab");
    else next.set("tab", k);
    setParams(next, { replace: true });
  };

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
    listProjects().then(setProjects).catch(() => void 0);
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

  async function reloadRoles() {
    try {
      const rs = await listRoles();
      if (rs.length) setRoles(rs);
    } catch { /* 保持现值 */ }
  }

  async function submitRole() {
    const v = await roleForm.validateFields();
    try {
      if (roleEditing === "new") {
        await createRole(v);
        message.success("角色已创建,可在下方矩阵为其勾选能力");
      } else if (roleEditing) {
        await updateRole(roleEditing.id, v);
        message.success("角色已更新");
      }
      setRoleEditing(null);
      await reloadRoles();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "保存失败");
    }
  }

  async function removeRole(role: RoleEntry) {
    try {
      await deleteRole(role.id);
      message.success(`已删除角色「${role.name}」`);
      await reloadRoles();
      const d = await getPermissions();
      setPermCatalog(d.catalog || []);
      setMatrix(d.matrix || {});
    } catch (e) {
      message.error(e instanceof Error ? e.message : "删除失败(仍有用户使用该角色?)");
    }
  }

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
    if (u === "new") {
      form.resetFields();
      return;
    }
    form.setFieldsValue({
      username: u.username,
      displayName: u.displayName,
      roleCode: u.roleCode,
      departmentId: u.departmentId || undefined,
      dataScope: u.dataScope || "",
      projectIds: [],
    });
    // 归属单独取:用户列表接口不返回它(每行都带一串项目 id,列表会变重)。
    // 先置空再异步填,避免上一次打开的选择残留在这一次的弹窗里。
    listUserProjects(u.id)
      .then((ids) => form.setFieldValue("projectIds", ids))
      .catch(() => void 0);
  }

  async function submit() {
    const v = await form.validateFields();
    try {
      if (editing === "new") {
        const created = await createUser(v);
        if (v.projectIds?.length && created?.user?.id) {
          await setUserProjects(created.user.id, v.projectIds);
        }
        message.success("用户已创建");
      } else if (editing) {
        await updateUser(editing.id, {
          displayName: v.displayName,
          roleCode: v.roleCode,
          departmentId: v.departmentId,
          // 恒传字符串:后端靠"有没有传"判断要不要改,漏传就改不回「按角色」
          dataScope: v.dataScope ?? "",
        });
        await setUserProjects(editing.id, v.projectIds || []);
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

  async function remove(u: UserEntry) {
    try {
      await deleteUser(u.id);
      message.success(`已删除 ${u.displayName}`);
      await load();
    } catch (e) {
      // 【原样显示后端的话】被拒绝时那句话里带着原因和替代做法
      //（"已提交过巡检记录…请改用停用"），换成"删除失败"就把它丢了。
      message.error(e instanceof Error ? e.message : "删除失败");
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
    // 【必须 wrap={false}】antd 的 Row 默认允许换行:右边内容一宽(表格列多、
    // 权限矩阵横向能到十几列),整个内容区就被挤到左侧菜单【下面】去,
    // 看起来像布局塌了。左右分栏这种场景永远不该换行。
    <Row gutter={16} wrap={false}>
      <Col flex="168px" style={{ flex: "0 0 168px" }}>
        {/* 【跟着滚】用户表能有几十行,滚下去之后菜单跟着划走,想切到别的块
            得先滚回顶部。sticky 让它一直贴在视口上沿。 */}
        <div className="pane-nav">
          <Menu
            mode="inline"
            selectedKeys={[tab]}
            items={paneNav}
            onClick={({ key }) => setTab(key)}
          />
        </div>
      </Col>
      {/* minWidth: 0 让宽表格在这一列内部横向滚动,而不是把整列撑开 */}
      <Col flex="auto" style={{ minWidth: 0, overflowX: "auto" }}>
      {tab === "users" && (
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
        // 放不下就横向滚,不要压缩列宽 —— 压缩的结果是中文逐字换行
        scroll={{ x: "max-content" }}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 人` }}
        columns={[
          // 【必须给宽度】不给的话这一列会被压到最窄,"临时工程主管"变成
          // 一列竖排的单字。列宽在窄屏下是靠横向滚动解决的,不是靠压缩。
          { title: "姓名", dataIndex: "displayName", width: 120 },
          { title: "账号", dataIndex: "username", width: 150 },
          { title: "角色", width: 130, render: (_, u) => roleTag(u.roleCode, u.roleName) },
          { title: "部门", width: 130, render: (_, u) => u.departmentName || "—" },
          {
            title: "数据范围",
            width: 110,
            render: (_, u) =>
              u.dataScope === "all" ? (
                <Tag color="blue">全部数据</Tag>
              ) : u.dataScope === "self" ? (
                <Tag>仅本人</Tag>
              ) : (
                <span style={{ color: "#999" }}>按角色</span>
              ),
          },
          {
            title: "状态",
            width: 90,
            render: (_, u) =>
              u.status === "disabled" ? <Tag>停用</Tag> : <Tag color="green">启用</Tag>,
          },
          { title: "创建时间", width: 150, render: (_, u) => fmtTime(u.createdAt, true) },
          {
            title: "操作",
            width: 260,
            render: (_, u) => (
              <Space size={4} split={<span style={{ color: "#e8e8e8" }}>|</span>}>
                <a onClick={() => openEdit(u)}>编辑</a>
                <a onClick={() => setPwdUser(u)}>重置密码</a>
                <Popconfirm
                  title={`确认${u.status === "disabled" ? "启用" : "停用"}「${u.displayName}」?`}
                  onConfirm={() => toggle(u)}
                >
                  <a>{u.status === "disabled" ? "启用" : "停用"}</a>
                </Popconfirm>
                {/* 【停用是常规做法,删除是例外】所以删除放在最后、只有它是红的。
                    自己的账号不给删 —— 后端也挡着,这里置灰是为了不让人白点一次。 */}
                {u.id === me?.id ? (
                  <Tooltip title="不能删除自己的账号">
                    <span style={{ color: "#bbb" }}>删除</span>
                  </Tooltip>
                ) : (
                  <Popconfirm
                    title={`删除「${u.displayName}」?`}
                    description="账号将被永久移除。若只是不再使用，请用「停用」。"
                    okText="删除"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => remove(u)}
                  >
                    <a style={{ color: "#d4380d" }}>删除</a>
                  </Popconfirm>
                )}
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
          <Form.Item
            name="dataScope"
            label="数据范围"
            initialValue=""
            extra="决定这个人能看到多少条数据，和他能做什么（角色）无关。"
          >
            <Select
              options={[
                { value: "", label: "按角色（默认）" },
                { value: "all", label: "全部数据" },
                { value: "project", label: "本项目（含组内其他人）" },
                { value: "project_self", label: "本项目台账 + 仅本人记录" },
                { value: "self", label: "仅本人提交的" },
              ]}
            />
          </Form.Item>
          <Form.Item
            name="projectIds"
            label="所属项目"
            extra="不选 = 不受项目限制。"
            dependencies={["dataScope"]}
            rules={[
              ({ getFieldValue }) => ({
                validator(_, value) {
                  const scope = getFieldValue("dataScope");
                  // 选了「本项目」却一个项目都不选，后端会判定成"什么都看不到"
                  // （不能放行，否则限制形同虚设）。在这里拦住，别让管理员
                  // 保存完才发现这个人打开是空白页。
                  if ((scope === "project" || scope === "project_self") && !value?.length) {
                    return Promise.reject(new Error("数据范围选了本项目，必须至少指定一个项目"));
                  }
                  return Promise.resolve();
                },
              }),
            ]}
          >
            <Select
              mode="multiple"
              allowClear
              placeholder="不限"
              options={projects
                .filter((p) => !p.disabled)
                .map((p) => ({ value: p.id, label: p.name }))}
            />
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
      )}
      {tab === "roles" && me?.roleCode === "admin" && (
      <Card
        title="角色管理"
        style={{ marginTop: 16 }}
        extra={
          <Button icon={<PlusOutlined />} onClick={() => { roleForm.resetFields(); setRoleEditing("new"); }}>
            新增角色
          </Button>
        }
      >
        <Table<RoleEntry>
          rowKey="id"
          size="middle"
          pagination={false}
          dataSource={roles}
          columns={[
            { title: "角色", width: 180, render: (_, ro) => roleTag(ro.code, ro.name) },
            {
              title: "类型",
              width: 100,
              render: (_, ro) => (ro.builtin ? <Tag>内置</Tag> : <Tag color="blue">自定义</Tag>),
            },
            { title: "说明", render: (_, ro) => <span style={{ color: "#8aa0b0" }}>{ro.description || "—"}</span> },
            {
              title: "操作",
              width: 160,
              render: (_, ro) => (
                <Space>
                  {ro.code !== "admin" && (
                    <a onClick={() => { roleForm.setFieldsValue({ name: ro.name, description: ro.description }); setRoleEditing(ro); }}>
                      重命名
                    </a>
                  )}
                  {!ro.builtin && (
                    <Popconfirm
                      title={`删除角色「${ro.name}」?`}
                      description="仍有用户使用时无法删除;其矩阵勾选将一并清除。"
                      okText="删除"
                      okButtonProps={{ danger: true }}
                      onConfirm={() => removeRole(ro)}
                    >
                      <a style={{ color: "#d4380d" }}>删除</a>
                    </Popconfirm>
                  )}
                  {ro.code === "admin" && <span style={{ color: "#b9c6cf" }}>固定</span>}
                </Space>
              ),
            },
          ]}
        />
        <Modal
          title={roleEditing === "new" ? "新增角色" : "重命名角色"}
          open={!!roleEditing}
          onOk={submitRole}
          onCancel={() => setRoleEditing(null)}
          destroyOnHidden
        >
          <Form form={roleForm} layout="vertical" requiredMark={false}>
            <Form.Item name="name" label="角色名称" rules={[{ required: true, message: "请输入角色名称" }]}>
              <Input maxLength={20} placeholder="如 工程主管 / 保洁组长" />
            </Form.Item>
            <Form.Item name="description" label="说明">
              <Input maxLength={60} placeholder="选填" />
            </Form.Item>
          </Form>
        </Modal>
      </Card>
      )}
      {tab === "matrix" && me?.roleCode === "admin" && permCatalog.length > 0 && (
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
            ...roles.filter((r) => r.code !== "admin").map((role) => ({
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

      {tab === "dept" && <Departments users={users} onChanged={load} />}
      {tab === "project" && <Projects />}
      {tab === "invite" && <RegistrationCodes roles={roles} />}
        </Col>
      </Row>
    );
}
