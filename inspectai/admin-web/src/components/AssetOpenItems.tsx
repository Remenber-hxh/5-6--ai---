import { Alert, Space, Tag, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { AssetOpenItems as OpenItems, getAssetOpenItems } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 一台设备身上还没了结的事。
 *
 * 【这是设备档案页最要紧的一块,所以放在最上面】打开这一页的人要的从来是
 * 同一件事:这台设备现在要不要我管。而"有没有没销账的问题"就是答案本身 ——
 * 排在设备信息、趋势、轨迹之后的话,人得往下翻才知道要不要管。
 *
 * 【没有未了结的事就整块不显示】"一切正常"这句话不需要占一块地方 ——
 * 页面上没有警示本身就是"正常"。
 */
export default function AssetOpenItems({
  assetId,
  bare,
}: {
  assetId: string;
  /**
   * bare:去掉 Alert 的外框和图标,只出内容。
   *
   * 【为什么需要这个变体】档案页顶上曾经有两块红色:一块是这里的
   * Alert「1 项未了结,其中 1 项已逾期」,六十像素外还有一句
   * 「需要处理 · 1 项整改已逾期」。同一件事用两种视觉语言说两遍,
   * 结果是两块都变弱 —— 人分不清哪块才是要点。
   * 合成一块之后,结论那行做标题,任务行做明细,红色只出现一次。
   */
  bare?: boolean;
}) {
  const nav = useNavigate();
  const [data, setData] = useState<OpenItems | null>(null);

  const load = useCallback(async (id: string) => {
    try {
      setData(await getAssetOpenItems(id));
    } catch {
      setData(null); // 拿不到就不显示 —— 这一块是警示,拿不到时不该编一个"正常"出来
    }
  }, []);

  useEffect(() => {
    if (assetId) void load(assetId);
  }, [assetId, load]);

  if (!data) return null;
  const { tasks, abnormalWithoutTask } = data;
  if (tasks.length === 0 && !abnormalWithoutTask) return null;

  // 【异常但无人接手,单独用更重的语气】它比"有几条在办任务"危险得多:
  // 有任务至少说明有人知道、有人在管;这一种是出了问题而流程根本没启动。
  if (abnormalWithoutTask) {
    const msg = `最近一次巡检判为「${data.lastStatus || "异常"}」,但没有任何在办任务`;
    const how = "也就是说问题被发现了,却没人接手。请在计划页派发一条复查任务,或到台账把它标记为正常。";
    // bare 模式下也要保留这一档的重量 —— 它比"有几条在办任务"危险得多:
    // 有任务至少说明有人在管,这一种是流程根本没启动。
    return bare ? (
      <div style={{ fontSize: 13.5, lineHeight: 1.7 }}>
        <div style={{ color: C.danger, fontWeight: 600 }}>{msg}</div>
        <div style={{ color: C.textSub }}>{how}</div>
      </div>
    ) : (
      <Alert type="error" showIcon message={msg} description={how} />
    );
  }

  const overdue = tasks.filter((t) => t.overdue).length;

  const rows = (
    <Space direction="vertical" size={6} style={{ width: "100%", marginTop: bare ? 0 : 4 }}>
      {tasks.map((t) => (
            <div
              key={t.id}
              className="hl-row"
              // 点过去看那条任务 —— 光知道"有一项没做完"不够,
              // 人下一步要做的是去处理它
              onClick={() => nav(`/plan?task=${encodeURIComponent(t.id)}`)}
              style={{
                display: "flex",
                alignItems: "baseline",
                gap: 10,
                padding: "5px 6px",
                borderRadius: 6,
                cursor: "pointer",
                fontSize: 13.5,
                flexWrap: "wrap",
              }}
            >
              <span style={{ fontWeight: 500 }}>{t.title}</span>
              <Tag color={t.status === "待整改" ? "red" : "blue"}>{t.status || "进行中"}</Tag>
              {t.assigneeName && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {t.assigneeName}
                </Typography.Text>
              )}
              {t.dueAt && (
                <span
                  style={{
                    marginLeft: "auto",
                    fontSize: 12,
                    color: t.overdue ? C.danger : C.textFaint,
                    fontVariantNumeric: "tabular-nums",
                  }}
                >
                  {t.overdue ? "已逾期 · " : "截止 "}
                  {t.dueAt}
                </span>
              )}
            </div>
          ))}
    </Space>
  );

  if (bare) return rows;

  return (
    <Alert
      type={overdue > 0 ? "error" : "warning"}
      showIcon
      message={
        overdue > 0
          ? `${tasks.length} 项未了结,其中 ${overdue} 项已逾期`
          : `${tasks.length} 项未了结`
      }
      description={rows}
    />
  );
}
