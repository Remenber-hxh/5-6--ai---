import { Card, Col, Descriptions, List, Row, Tag } from "antd";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { EngineeringTask, OperationLog, listOperationLogs, listTasks } from "../api/mgmt";
import { useAuth } from "../store/auth";
import { fmtTime } from "../lib/status";

// 角色 → 权限范围说明(与旧版口径一致)
const ROLE_SCOPE: Record<string, string[]> = {
  admin: ["全部页面与配置", "用户与权限管理", "审批与派发", "提示词模板编辑"],
  manager: ["巡检计划与派发", "审批中心", "数据看板", "提示词模板编辑"],
  supervisor: ["巡检计划与派发", "审批中心", "数据看板", "提示词模板编辑"],
  inspector: ["移动端巡检填报", "我的任务", "个人记录查看"],
};

export default function Profile() {
  const nav = useNavigate();
  const { user } = useAuth();
  const [tasks, setTasks] = useState<EngineeringTask[]>([]);
  const [logs, setLogs] = useState<OperationLog[]>([]);

  useEffect(() => {
    listTasks()
      .then((ts) =>
        setTasks(
          ts.filter(
            (t) =>
              (!t.assigneeName || t.assigneeName === user?.displayName) &&
              ["待执行", "进行中", "待整改"].includes(t.status || ""),
          ),
        ),
      )
      .catch(() => void 0);
    listOperationLogs()
      .then((ls) => setLogs(ls.filter((l) => l.actorName === user?.displayName).slice(0, 10)))
      .catch(() => void 0);
  }, [user]);

  const scope = ROLE_SCOPE[user?.roleCode || ""] || ROLE_SCOPE.inspector;

  return (
    <Row gutter={16}>
      <Col span={10}>
        <Card title="我的资料" style={{ marginBottom: 16 }}>
          <Descriptions column={1} size="small">
            <Descriptions.Item label="姓名">{user?.displayName || "—"}</Descriptions.Item>
            <Descriptions.Item label="账号">{user?.username || "—"}</Descriptions.Item>
            <Descriptions.Item label="角色">
              <Tag color="geekblue">{user?.roleName || user?.roleCode}</Tag>
            </Descriptions.Item>
          </Descriptions>
        </Card>
        <Card title="权限范围">
          <List
            size="small"
            dataSource={scope}
            renderItem={(s) => <List.Item style={{ padding: "6px 0" }}>· {s}</List.Item>}
          />
        </Card>
      </Col>
      <Col span={14}>
        <Card title={`我的待办(${tasks.length})`} style={{ marginBottom: 16 }}>
          {tasks.length === 0 ? (
            <div style={{ color: "#8aa0b0", fontSize: 13 }}>暂无待办任务</div>
          ) : (
            <List
              size="small"
              dataSource={tasks.slice(0, 8)}
              renderItem={(t) => (
                <List.Item
                  style={{ cursor: "pointer" }}
                  onClick={() => nav("/plan")}
                  extra={
                    <Tag color={t.status === "待整改" ? "red" : t.status === "进行中" ? "blue" : "default"}>
                      {t.status}
                    </Tag>
                  }
                >
                  <List.Item.Meta title={t.title} description={t.dueAt ? `截止 ${t.dueAt}` : undefined} />
                </List.Item>
              )}
            />
          )}
        </Card>
        <Card title="最近操作" extra={<a onClick={() => nav("/logs")}>全部</a>}>
          {logs.length === 0 ? (
            <div style={{ color: "#8aa0b0", fontSize: 13 }}>暂无操作记录</div>
          ) : (
            <List
              size="small"
              dataSource={logs}
              renderItem={(l) => (
                <List.Item style={{ padding: "6px 0" }}>
                  <span style={{ color: "#8aa0b0", marginRight: 10 }}>{fmtTime(l.createdAt)}</span>
                  {l.action} · {l.targetType}
                </List.Item>
              )}
            />
          )}
        </Card>
      </Col>
    </Row>
  );
}
