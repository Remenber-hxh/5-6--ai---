import { Card, Col, Descriptions, Row, Tag } from "antd";
import { useEffect, useState } from "react";

import { getHealth } from "../api/mgmt";

// 系统管理:服务健康状态一览(配置修改仍在旧版/服务端,避免误操作)
export default function System() {
  const [health, setHealth] = useState<Record<string, unknown> | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    getHealth()
      .then(setHealth)
      .catch((e) => setErr(e instanceof Error ? e.message : "获取失败"));
  }, []);

  const ok = health?.status === "ok";

  return (
    <Row gutter={16}>
      <Col span={12}>
        <Card title="服务状态">
          {err && <Tag color="red">{err}</Tag>}
          {health && (
            <Descriptions column={1} size="small">
              <Descriptions.Item label="后端服务">
                {ok ? <Tag color="green">正常</Tag> : <Tag color="red">异常</Tag>}
              </Descriptions.Item>
              <Descriptions.Item label="存储引擎">{String(health.storeKind || "—")}</Descriptions.Item>
              <Descriptions.Item label="AI 服务地址">{String(health.aiServiceUrl || "—")}</Descriptions.Item>
              <Descriptions.Item label="企业微信通知">
                {health.wework ? <Tag color="green">已配置</Tag> : <Tag>未配置</Tag>}
              </Descriptions.Item>
              <Descriptions.Item label="服务器时间">{String(health.time || "—")}</Descriptions.Item>
            </Descriptions>
          )}
        </Card>
      </Col>
      <Col span={12}>
        <Card title="说明">
          <p style={{ color: "#5b6b78", margin: 0, lineHeight: 1.8 }}>
            系统配置(企业微信应用、密钥、通知规则)在服务端管理;此页仅提供运行状态查看。前端为新版
            admin-web,与旧版后台并行运行,共用同一 Go 后端与数据库。
          </p>
        </Card>
      </Col>
    </Row>
  );
}
