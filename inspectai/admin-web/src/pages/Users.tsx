import { Card, Table, Tag } from "antd";
import { useEffect, useState } from "react";

import { UserEntry, listUsers } from "../api/mgmt";
import { fmtTime } from "../lib/status";

const roleTag = (code?: string, name?: string) => {
  const color =
    code === "admin" ? "geekblue" : code === "manager" || code === "supervisor" ? "cyan" : "default";
  return <Tag color={color}>{name || code || "—"}</Tag>;
};

// 用户与权限:只读视图(建账/改角色仍在旧版后台操作,避免双端写冲突)
export default function Users() {
  const [users, setUsers] = useState<UserEntry[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    listUsers()
      .then(setUsers)
      .finally(() => setLoading(false));
  }, []);

  return (
    <Card title="用户与权限">
      <Table<UserEntry>
        rowKey="id"
        loading={loading}
        dataSource={users}
        pagination={{ pageSize: 20, showTotal: (t) => `共 ${t} 人` }}
        columns={[
          { title: "姓名", dataIndex: "displayName" },
          { title: "账号", dataIndex: "username", width: 150 },
          { title: "角色", width: 130, render: (_, u) => roleTag(u.roleCode, u.roleName) },
          {
            title: "状态",
            width: 100,
            render: (_, u) =>
              u.status === "disabled" ? <Tag>停用</Tag> : <Tag color="green">启用</Tag>,
          },
          { title: "创建时间", width: 160, render: (_, u) => fmtTime(u.createdAt, true) },
        ]}
      />
    </Card>
  );
}
