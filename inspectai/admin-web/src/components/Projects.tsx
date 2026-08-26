import { PlusOutlined } from "@ant-design/icons";
import { Button, Card, Form, Input, Modal, Popconfirm, Space, Table, Tag, Tooltip, Typography, message } from "antd";
import { useEffect, useState } from "react";

import { ProjectEntry, createProject, deleteProject, listProjects, updateProject } from "../api/mgmt";

/**
 * 项目管理。
 *
 * 项目是"谁能看到哪些数据"的划分依据:把人分到项目里,再把他的数据范围
 * 设成「本项目」,他就只看得到这些现场的东西。
 *
 * 【为什么没有"改名"】后端的设备台账、巡检记录、修改申请之间是靠中文项目名
 * 互相关联的。改了名,台账就认不出这个项目了。所以只提供登记和停用。
 *
 * 【设备数为 0 要当回事】说明这个项目名和台账里的对不上 —— 多半是手工新建时
 * 名字写得和现场数据不一致。列表把这一列摆出来就是为了让人当场发现。
 */
export default function Projects() {
  const [list, setList] = useState<ProjectEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [saving, setSaving] = useState(false);
  const [form] = Form.useForm();

  async function load() {
    setLoading(true);
    try {
      setList(await listProjects());
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function submit() {
    const v = await form.validateFields();
    setSaving(true);
    try {
      await createProject({ name: v.name.trim(), note: v.note });
      message.success("项目已创建");
      setCreating(false);
      form.resetFields();
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "创建失败");
    } finally {
      setSaving(false);
    }
  }

  async function toggle(p: ProjectEntry) {
    try {
      await updateProject(p.id, { note: p.note, disabled: !p.disabled });
      message.success(`已${p.disabled ? "启用" : "停用"}「${p.name}」`);
      await load();
    } catch (e) {
      message.error(e instanceof Error ? e.message : "操作失败");
    }
  }

  async function remove(p: ProjectEntry) {
    try {
      await deleteProject(p.id);
      message.success(`已删除「${p.name}」`);
      await load();
    } catch (e) {
      // 原样显示后端的话:被拒时那句里有"请改用停用"
      message.error(e instanceof Error ? e.message : "删除失败");
    }
  }

  return (
    <Card
      title="项目"
      style={{ marginTop: 16 }}
      extra={
        <Button icon={<PlusOutlined />} onClick={() => setCreating(true)}>
          新建项目
        </Button>
      }
    >
      <Typography.Paragraph type="secondary" style={{ marginTop: -8 }}>
        项目决定一个人能看到哪些现场的数据。在「用户」里把人分到项目，再把他的数据范围设成本项目即可。
      </Typography.Paragraph>
      <Table<ProjectEntry>
        rowKey="id"
        size="middle"
        loading={loading}
        dataSource={list}
        // 放不下就横向滚,不要压缩列宽 —— 压缩的结果是中文逐字换行
        scroll={{ x: "max-content" }}
        pagination={false}
        columns={[
          { title: "项目名称", dataIndex: "name" },
          { title: "编号", width: 100, render: (_, p) => p.code || "—" },
          {
            title: "设备",
            width: 90,
            render: (_, p) =>
              p.assetCount > 0 ? (
                `${p.assetCount} 台`
              ) : (
                <Tag color="orange">0 台</Tag>
              ),
          },
          { title: "成员", width: 90, render: (_, p) => `${p.memberCount} 人` },
          { title: "备注", render: (_, p) => p.note || "—" },
          {
            title: "状态",
            width: 90,
            render: (_, p) => (p.disabled ? <Tag>停用</Tag> : <Tag color="green">启用</Tag>),
          },
          {
            title: "操作",
            width: 150,
            render: (_, p) => (
              <Space size={4} split={<span style={{ color: "#e8e8e8" }}>|</span>}>
                <Popconfirm title={`确认${p.disabled ? "启用" : "停用"}「${p.name}」?`} onConfirm={() => toggle(p)}>
                  <a>{p.disabled ? "启用" : "停用"}</a>
                </Popconfirm>
                {/* 【有设备就不给删】后端会拒绝,这里直接置灰 ——
                    让人点一次才知道被拒,不如一眼看出来。 */}
                {p.assetCount > 0 ? (
                  <Tooltip title="项目下还有设备台账，请改用「停用」">
                    <span style={{ color: "#bbb" }}>删除</span>
                  </Tooltip>
                ) : (
                  <Popconfirm
                    title={`删除「${p.name}」?`}
                    description="项目将被永久移除，成员归属一并清除。"
                    okText="删除"
                    okButtonProps={{ danger: true }}
                    onConfirm={() => remove(p)}
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
        title="新建项目"
        open={creating}
        onOk={submit}
        confirmLoading={saving}
        onCancel={() => setCreating(false)}
        destroyOnHidden
      >
        <Form form={form} layout="vertical" requiredMark={false}>
          <Form.Item
            name="name"
            label="项目名称"
            extra="必须和设备台账里的项目名完全一致，否则设备挂不上来。创建后不能改名。"
            rules={[{ required: true, message: "请输入项目名称" }]}
          >
            <Input placeholder="例如：会议中心" />
          </Form.Item>
          <Form.Item name="note" label="备注">
            <Input.TextArea rows={2} maxLength={120} showCount />
          </Form.Item>
        </Form>
      </Modal>
    </Card>
  );
}
