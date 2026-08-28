import { CheckCircleFilled, ReloadOutlined, RightOutlined } from "@ant-design/icons";
import { Alert, Button, Empty, Space, Tag, Tooltip, Typography, message } from "antd";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import { TodayBoard, getTodayBoard } from "../api/mgmt";
import { C } from "../styles/tokens";

/**
 * 今日执行。
 *
 * 【这一屏只回答一个问题:今天还差什么】
 *
 * 上一版把圆环进度、每条计划的进度标签、每台设备的绿/灰标签全铺出来，
 * 一屏里"有多少/做了没"被表达了五遍——客户看到的不是信息丰富，
 * 是到处都是数字，不知道该看哪个。
 *
 * 所以砍到只剩三样：
 *   1. 一个大数字：还差几台（不是圆环、不是百分比——人要的是"还剩几件事"）
 *   2. 未完成的清单（唯一需要行动的信息）
 *   3. 已完成折叠成一行，点开才展开
 *
 * 【已完成的不该占面积】20 台设备铺一片绿标签，而读者要的只是那几个灰的。
 */

const WEEKDAY_CN = ["", "周一", "周二", "周三", "周四", "周五", "周六", "周日"];

/**
 * action:放在「刷新」旁边的额外操作。
 *
 * 【为什么不放在外层 Card 的 extra 里】那张卡没有标题,头部就是一条空白横条,
 * 右端一个浅灰文字按钮 —— 在这套极简配色里几乎等于隐形,用户找不到。
 * 而这一行(大数字 · 日期 · 刷新)本来就是眼睛落点。
 */
export default function TodayInspection({ action }: { action?: React.ReactNode }) {
  const nav = useNavigate();
  const [board, setBoard] = useState<TodayBoard | null>(null);
  const [loading, setLoading] = useState(true);
  const [showDone, setShowDone] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      setBoard(await getTodayBoard());
    } catch (e) {
      message.error(e instanceof Error ? e.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  // 把所有计划的设备摊平成一张清单。
  //
  // 【按设备而不是按计划组织】客户关心的是"哪几台还没巡",不是
  // "第二条计划完成了 3/5"。按计划分组会让人在几个方框之间来回找灰色标签。
  const { todo, done } = useMemo(() => {
    const todo: { key: string; name: string; project?: string; owner?: string; missing?: boolean }[] = [];
    const done: { key: string; name: string; at?: string }[] = [];
    const seen = new Set<string>();
    for (const p of board?.plans ?? []) {
      for (const a of p.assets) {
        if (seen.has(a.assetId)) continue; // 两条计划点同一台设备,只列一次
        seen.add(a.assetId);
        if (a.done) done.push({ key: a.assetId, name: a.assetName, at: a.doneAt });
        else
          todo.push({
            key: a.assetId,
            name: a.assetName,
            project: a.project || p.project,
            owner: p.ownerName,
            missing: a.missing,
          });
      }
    }
    return { todo, done };
  }, [board]);

  const noAssetPlans = (board?.plans ?? []).filter((p) => p.noAssets);

  if (loading) return <div style={{ padding: 24, color: C.textFaint }}>加载中…</div>;

  if (!board || board.plans.length === 0) {
    return (
      <Empty
        image={Empty.PRESENTED_IMAGE_SIMPLE}
        description="今天没有排定的每日巡检计划"
        style={{ padding: "32px 0" }}
      />
    );
  }

  return (
    <Space direction="vertical" size={18} style={{ width: "100%" }}>
      {/* ── 一个数字。不是圆环、不是百分比 ── */}
      <Space align="baseline" size={12} wrap>
        {todo.length > 0 ? (
          <>
            <span style={{ fontSize: 40, fontWeight: 800, color: C.warn, lineHeight: 1 }}>
              {todo.length}
            </span>
            <span style={{ fontSize: 16, color: C.text }}>台还没巡</span>
          </>
        ) : (
          <Space align="center">
            <CheckCircleFilled style={{ color: C.ok, fontSize: 26 }} />
            <span style={{ fontSize: 20, fontWeight: 700, color: C.ok }}>今天的巡检已全部完成</span>
          </Space>
        )}
        <Typography.Text type="secondary" style={{ fontSize: 13 }}>
          {board.date} · {WEEKDAY_CN[board.weekday]} · 共 {board.total} 台
        </Typography.Text>
        <Button size="small" icon={<ReloadOutlined />} onClick={() => void load()}>
          刷新
        </Button>
        {action}
      </Space>

      {/* ── 待巡清单:唯一需要行动的信息 ── */}
      {todo.length > 0 && (
        <div style={{ border: `1px solid ${C.line}`, borderRadius: 8, overflow: "hidden" }}>
          {todo.map((a, i) => (
            <div
              key={a.key}
              // 【点一行跳台账】只看到"K01 没巡"是不够的 —— 现场调度前想知道
              // 这台上次什么状态、有没有遗留异常。台账详情正好有这些。
              //
              // 已经从台账删掉的不给点:跳过去是个 404,比不能点更让人困惑。
              onClick={a.missing ? undefined : () => nav(`/ledger?focus=${encodeURIComponent(a.key)}`)}
              className={a.missing ? undefined : "today-row"}
              style={{
                display: "flex",
                alignItems: "center",
                gap: 12,
                padding: "11px 14px",
                borderTop: i ? `1px solid ${C.line}` : "none",
                cursor: a.missing ? "default" : "pointer",
              }}
            >
              <span style={{ width: 6, height: 6, borderRadius: 3, background: C.warn, flex: "0 0 6px" }} />
              <span style={{ fontWeight: 600, minWidth: 130 }}>{a.name}</span>
              {a.project && <Tag>{a.project}</Tag>}
              {a.owner && (
                <Typography.Text type="secondary" style={{ fontSize: 12 }}>
                  {a.owner}
                </Typography.Text>
              )}
              {a.missing && (
                <Tooltip title="这台设备已不在台账里(计划录入后被删),请编辑计划移除它">
                  <Tag color="red">台账已删除</Tag>
                </Tooltip>
              )}
            </div>
          ))}
        </div>
      )}

      {/* ── 已完成:折叠成一行 ── */}
      {done.length > 0 && (
        <div>
          <Button type="text" size="small" onClick={() => setShowDone((v) => !v)} style={{ paddingLeft: 0 }}>
            <RightOutlined rotate={showDone ? 90 : 0} style={{ fontSize: 11, marginRight: 6 }} />
            已完成 {done.length} 台
          </Button>
          {showDone && (
            <Space size={[6, 6]} wrap style={{ marginTop: 8 }}>
              {done.map((a) => (
                <Tooltip key={a.key} title={a.at ? `已巡 ${a.at.slice(0, 16).replace("T", " ")}` : ""}>
                  <Tag color="green" style={{ opacity: 0.75 }}>
                    {a.name}
                  </Tag>
                </Tooltip>
              ))}
            </Space>
          )}
        </div>
      )}

      {/* 没指定设备的计划:显式说出来。静默当 0/0 会让它看起来像"已完成" */}
      {noAssetPlans.length > 0 && (
        <Alert
          type="warning"
          showIcon
          message={`有 ${noAssetPlans.length} 条每日计划没有指定要巡的设备,完成情况无法统计`}
          description={noAssetPlans.map((p) => p.title).join("、") + " —— 请编辑计划补上设备清单"}
        />
      )}
    </Space>
  );
}
