import { ReloadOutlined } from "@ant-design/icons";
import { Button, Card, Col, Row, Skeleton, Tag, message } from "antd";
import { useEffect, useState } from "react";

import { AIHealth, getAIHealth, getHealth } from "../api/mgmt";
import { fmtTime } from "../lib/status";

interface Health {
  status?: string;
  storeKind?: string;
  aiServiceUrl?: string;
  wework?: boolean;
  weworkBot?: boolean;
  time?: string;
  service?: string;
}

// 服务状态卡:名称 + 地址/说明 + 状态 tag(与计划页状态卡同语言:左色角标)
function SvcCard({
  name,
  desc,
  ok,
  okText,
  badText,
}: {
  name: string;
  desc: string;
  ok: boolean;
  okText: string;
  badText: string;
}) {
  const color = ok ? "#12a968" : "#f5a524";
  return (
    <div
      style={{
        position: "relative",
        background: "#fff",
        borderRadius: 10,
        padding: "16px 18px",
        boxShadow: "0 1px 2px rgba(15, 35, 55, 0.04)",
        overflow: "hidden",
      }}
    >
      <span
        style={{
          position: "absolute",
          left: 0,
          top: 16,
          width: 4,
          height: 18,
          background: color,
          borderRadius: "0 2px 2px 0",
        }}
      />
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <b style={{ fontSize: 14.5 }}>{name}</b>
        <Tag color={ok ? "green" : "orange"} style={{ margin: 0 }}>
          {ok ? okText : badText}
        </Tag>
      </div>
      <div
        style={{
          color: "#8aa0b0",
          fontSize: 12,
          marginTop: 6,
          overflow: "hidden",
          textOverflow: "ellipsis",
          whiteSpace: "nowrap",
        }}
        title={desc}
      >
        {desc || "—"}
      </div>
    </div>
  );
}

// 系统管理:服务健康矩阵 + 运行详情(含时区哨兵);配置写入仍在服务端管理
export default function System() {
  const [health, setHealth] = useState<Health | null>(null);
  const [ai, setAi] = useState<AIHealth | null>(null);
  const [loading, setLoading] = useState(true);

  async function load() {
    setLoading(true);
    try {
      setHealth((await getHealth()) as Health);
    } catch (e) {
      message.error(e instanceof Error ? e.message : "获取服务状态失败");
      setHealth(null);
    } finally {
      setLoading(false);
    }
    // 【单独一条,失败不影响整页】AI 探活要打一次外部服务,
    // 它超时的时候不该把服务状态页一起拖黑。
    try {
      setAi(await getAIHealth());
    } catch {
      setAi(null);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  if (loading && !health) {
    return (
      <Card title="系统管理">
        <Skeleton active paragraph={{ rows: 6 }} />
      </Card>
    );
  }

  const ok = health?.status === "ok";
  const aiUrl = health?.aiServiceUrl || "";
  const store = health?.storeKind || "";
  // 时区哨兵:服务器时间不是东八区时提醒(影响"本周/今日"报表口径)
  const tzOk = (health?.time || "").includes("+08:00");

  return (
    <div>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(4, 1fr)",
          gap: 14,
          marginBottom: 16,
        }}
      >
        <SvcCard
          name="Go API 服务"
          desc={location.origin}
          ok={ok}
          okText="运行中"
          badText="异常"
        />
        {/* 【这张卡以前恒绿】它只判断 aiServiceUrl 非空就显示「已配置」,
            跟能不能用无关 —— 账户欠费时管理问答每一句都是兜底文案,
            而这里照样绿着。一个永远显示健康的指示灯比没有更糟。 */}
        <SvcCard
          name="AI 视觉 / 问答服务"
          desc={ai ? ai.reason || aiUrl : aiUrl}
          ok={Boolean(ai?.reachable && ai?.vision && ai?.chat)}
          okText="正常"
          badText={!ai ? "状态未知" : !ai.reachable ? "连不上" : "部分不可用"}
        />
        <SvcCard
          name="数据库"
          desc={store === "mysql" ? "MySQL · 结构化持久存储" : store || "—"}
          ok={store === "mysql"}
          okText={store === "mysql" ? "MySQL" : store || "未知"}
          badText={store || "内存模式"}
        />
        <SvcCard
          name="企业微信通知"
          desc={health?.wework ? "异常与待办自动推送" : "在服务端配置企业 ID / 应用密钥后启用"}
          ok={Boolean(health?.wework)}
          okText="已配置"
          badText="未配置"
        />
      </div>

      <Row gutter={16}>
        <Col span={14}>
          <Card
            size="small"
            title="运行详情"
            extra={
              <Button size="small" icon={<ReloadOutlined />} onClick={load} loading={loading}>
                重新检测
              </Button>
            }
          >
            <div style={rowStyle}>
              <span style={lblStyle}>服务标识</span>
              <b>{health?.service || "—"}</b>
            </div>
            <div style={rowStyle}>
              <span style={lblStyle}>服务器时间</span>
              <b>{health?.time ? fmtTime(health.time, true) : "—"}</b>
              {health?.time &&
                (tzOk ? (
                  <Tag color="green" style={{ marginLeft: 8 }}>
                    东八区
                  </Tag>
                ) : (
                  <Tag color="orange" style={{ marginLeft: 8 }} title="非东八区会影响今日/本周报表的时间口径">
                    时区非东八区
                  </Tag>
                ))}
            </div>
            <div style={rowStyle}>
              <span style={lblStyle}>群机器人</span>
              <b>{health?.weworkBot ? "已配置" : "未配置"}</b>
            </div>
            <div style={{ ...rowStyle, borderBottom: "none" }}>
              <span style={lblStyle}>前端版本</span>
              <b>admin-web(新版)· 与旧版后台并行,共用同一后端与数据库</b>
            </div>
          </Card>
        </Col>
        <Col span={10}>
          <Card size="small" title="说明">
            <p style={{ color: "#5b6b78", margin: 0, lineHeight: 1.8, fontSize: 13.5 }}>
              系统配置(企业微信应用、密钥、通知规则)在服务端管理,此页仅提供运行状态查看;发现服务异常时先「重新检测」,仍异常再联系管理员重启对应服务。
            </p>
          </Card>
        </Col>
      </Row>
    </div>
  );
}

const rowStyle: React.CSSProperties = {
  display: "flex",
  alignItems: "center",
  padding: "9px 0",
  borderBottom: "1px solid #f0f2f5",
  fontSize: 13.5,
};

const lblStyle: React.CSSProperties = { width: 90, flex: "none", color: "#8aa0b0" };
