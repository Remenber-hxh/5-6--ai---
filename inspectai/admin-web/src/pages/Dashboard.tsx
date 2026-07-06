import { Alert, Card, Col, Row, Statistic } from "antd";
import { motion } from "motion/react";
import { useEffect, useState } from "react";

import { api } from "../api/client";

interface Overview {
  assetTotal?: number;
  recordRecent?: number;
  abnormalRecent?: number;
  pendingReviews?: number;
  pendingApprovals?: number;
}

// 骨架页:验证 API 链路 + Motion 入场;Agent 首页整体迁移在下一步
export default function Dashboard() {
  const [ov, setOv] = useState<Overview | null>(null);
  const [err, setErr] = useState("");

  useEffect(() => {
    api<{ overview?: Overview }>("/api/management-ai/snapshot?range=30d")
      .then((d) => setOv(d.overview || null))
      .catch((e) => setErr(e instanceof Error ? e.message : "加载失败"));
  }, []);

  const cards = [
    { title: "资产总数", value: ov?.assetTotal },
    { title: "近 30 天巡检", value: ov?.recordRecent },
    { title: "近 30 天异常", value: ov?.abnormalRecent },
    { title: "待复核 / 待审批", value: (ov?.pendingReviews ?? 0) + (ov?.pendingApprovals ?? 0) },
  ];

  return (
    <div>
      {err && <Alert type="warning" message={err} style={{ marginBottom: 16 }} />}
      <Row gutter={16}>
        {cards.map((c, i) => (
          <Col span={6} key={c.title}>
            <motion.div
              initial={{ opacity: 0, y: 12 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.35, delay: i * 0.08, ease: "easeOut" }}
            >
              <Card>
                <Statistic title={c.title} value={c.value ?? "—"} />
              </Card>
            </motion.div>
          </Col>
        ))}
      </Row>
      <Card style={{ marginTop: 16 }}>
        <p style={{ margin: 0, color: "#5b6b78" }}>
          admin-web 脚手架已通:登录 → 权限路由 → 真实 API。Agent 首页与各业务页按优先级逐页迁移,旧版
          admin-frontend 保持可用。
        </p>
      </Card>
    </div>
  );
}
