import { Button, Card, Col, Empty, List, Row, Tag } from "antd";
import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { EngineeringTask, OperationLog, listOperationLogs, listTasks } from "../api/mgmt";
import { actionColor, actionLabel, targetLabel } from "../lib/oplog";
import { fmtTime } from "../lib/status";
import { useAuth } from "../store/auth";

// 角色 → 权限范围说明(与旧版口径一致)
const ROLE_SCOPE: Record<string, string[]> = {
  admin: ["全部页面与配置", "用户与权限管理", "审批与派发", "提示词模板编辑"],
  manager: ["巡检计划与派发", "审批中心", "数据看板", "提示词模板编辑"],
  supervisor: ["巡检计划与派发", "审批中心", "数据看板", "提示词模板编辑"],
  inspector: ["移动端巡检填报", "我的任务", "个人记录查看"],
};

const taskTagColor = (s?: string) =>
  s === "待整改" ? "red" : s === "进行中" ? "blue" : s === "待执行" ? "orange" : "default";

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
  const initial = (user?.displayName || user?.username || "?").slice(0, 1);

  return (
    <div>
      {/* 身份卡:侧栏同款深底头像 + 姓名/角色 + 权限范围 */}
      <Card size="small" style={{ marginBottom: 16 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 18, padding: "6px 4px" }}>
          <div
            style={{
              width: 56,
              height: 56,
              borderRadius: "50%",
              background: "#0b1626",
              color: "#3ee6b4",
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontSize: 22,
              fontWeight: 700,
              flex: "none",
              boxShadow: "inset 0 0 0 2px rgba(62, 230, 180, 0.35)",
            }}
          >
            {initial}
          </div>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
              <b style={{ fontSize: 18 }}>{user?.displayName || "—"}</b>
              <Tag color="geekblue" style={{ margin: 0 }}>
                {user?.roleName || user?.roleCode || "—"}
              </Tag>
            </div>
            <div style={{ color: "#8aa0b0", fontSize: 13, marginTop: 4 }}>
              账号 {user?.username || "—"}
            </div>
          </div>
          <div style={{ textAlign: "right" }}>
            <div style={{ color: "#8aa0b0", fontSize: 12, marginBottom: 6 }}>权限范围</div>
            <div style={{ display: "flex", gap: 6, flexWrap: "wrap", justifyContent: "flex-end" }}>
              {scope.map((s) => (
                <Tag key={s} style={{ margin: 0 }}>
                  {s}
                </Tag>
              ))}
            </div>
          </div>
        </div>
      </Card>

      <Row gutter={16}>
        <Col span={12}>
          <Card size="small" title={`我的待办(${tasks.length})`}>
            {tasks.length === 0 ? (
              <Empty description="暂无待办任务" image={Empty.PRESENTED_IMAGE_SIMPLE}>
                <Button onClick={() => nav("/plan")}>前往巡检计划</Button>
              </Empty>
            ) : (
              <List
                size="small"
                dataSource={tasks.slice(0, 8)}
                renderItem={(t) => (
                  <List.Item
                    style={{ cursor: "pointer" }}
                    onClick={() => nav("/plan")}
                    extra={<Tag color={taskTagColor(t.status)}>{t.status}</Tag>}
                  >
                    <List.Item.Meta
                      title={t.title}
                      description={t.dueAt ? `截止 ${t.dueAt}` : undefined}
                    />
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
        <Col span={12}>
          <Card size="small" title="最近操作" extra={<a onClick={() => nav("/logs")}>全部</a>}>
            {logs.length === 0 ? (
              <Empty description="暂无操作记录" image={Empty.PRESENTED_IMAGE_SIMPLE} />
            ) : (
              <List
                size="small"
                dataSource={logs}
                renderItem={(l) => (
                  <List.Item style={{ padding: "8px 0" }}>
                    <span style={{ color: "#8aa0b0", marginRight: 10, fontSize: 12.5, flex: "none" }}>
                      {fmtTime(l.createdAt)}
                    </span>
                    <Tag color={actionColor(l.action)} style={{ marginRight: 8 }}>
                      {actionLabel(l.action)}
                    </Tag>
                    <span style={{ color: "#5b6b78", fontSize: 13 }}>{targetLabel(l.targetType)}</span>
                  </List.Item>
                )}
              />
            )}
          </Card>
        </Col>
      </Row>
    </div>
  );
}
