import { Space, Typography } from "antd";
import { useCallback, useEffect, useState } from "react";

import { AssetVerdict as Verdict, getAssetVerdict } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 一句话结论 + 依据。
 *
 * 【为什么要有这一层】页面上摆着一堆事实 —— 状态、最近巡检时间、
 * 未了结任务、读数曲线。全是真的,但没有一个是判断。人得自己把它们
 * 在脑子里合起来,而这正是他打开这一页想让系统替他做的事。
 *
 * 【依据必须摆出来】只给一个"健康分 72",人既无法认同也无法反驳,
 * 最后只会忽略它。写清"已 41 天没巡""水箱水位较平时低 33%",
 * 这个判断才站得住,人也才有机会说"这条不算,我知道原因"。
 */

const TONE: Record<Verdict["level"], { dot: string; text: string }> = {
  act: { dot: C.danger, text: C.danger },
  schedule: { dot: C.warn, text: C.warn },
  ok: { dot: C.ok, text: C.textSub },
};

export default function AssetVerdict({ assetId }: { assetId: string }) {
  const [v, setV] = useState<Verdict | null>(null);

  const load = useCallback(async (id: string) => {
    try {
      setV(await getAssetVerdict(id));
    } catch {
      setV(null); // 拿不到就不显示 —— 结论拿不到时,不该编一个"正常"出来
    }
  }, []);

  useEffect(() => {
    if (assetId) void load(assetId);
  }, [assetId, load]);

  if (!v) return null;

  return (
    <Space size={8} align="baseline" wrap>
      <span
        style={{
          width: 8,
          height: 8,
          borderRadius: 4,
          background: TONE[v.level].dot,
          display: "inline-block",
        }}
      />
      <span style={{ fontWeight: 600, color: TONE[v.level].text }}>{v.headline}</span>
      {/* 【依据跟在同一行】另起一段的话,人会先读结论然后停下来 ——
          而结论本身没有信息量,信息量全在后面这几条。 */}
      {v.reasons.length > 0 && (
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {v.reasons.join(" · ")}
        </Typography.Text>
      )}
    </Space>
  );
}
