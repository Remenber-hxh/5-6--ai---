import { useCallback, useEffect, useState } from "react";
import { useLocation, useNavigate } from "react-router-dom";

import { listEngineeringTasks, listOfflineShots } from "@/api/inspection";
import { usePending } from "@/store/pending";
import { TabBar } from "@/ui";

import { IconLedger, IconPhotos, IconShoot, IconTasks, IconUser } from "./icons";

// ===== 底部常驻导航 =====
//
// 这是"像不像一个 app"最要紧的一件事,比配色和辉光都重要:
// 在此之前每个页面都是孤岛 —— 想从台账去任务,必须先返回首页。
// 有了常驻 TabBar,当前位置永远清楚,任何地方一键可达。
//
// 【只在顶层目的地显示】流程页(选场景 → 填日报 → 提交)和下钻页
// (设备详情、待审批)不显示:
//   1. 流程页自己有底部主操作条(.flow-foot),两条底栏会打架
//   2. 巡检员填到一半误点 tab 就前功尽弃 —— 这类流程该是"进去就走完"
const TAB_ROUTES = ["/", "/review", "/tasks", "/ledger", "/me"] as const;

export default function AppTabs() {
  const nav = useNavigate();
  const { pathname } = useLocation();
  // 拍完照上传成功会推进这个计数,角标要跟着动,否则用户拍完看角标没变
  const uploadedTick = usePending((s) => s.uploadedTick);
  const [shots, setShots] = useState(0);
  const [tasks, setTasks] = useState(0);

  const visible = (TAB_ROUTES as readonly string[]).includes(pathname);

  const load = useCallback(async () => {
    // 角标是辅助信息,拉取失败静默降级为不显示,不打扰主流程
    const [s, t] = await Promise.all([
      listOfflineShots().catch(() => []),
      listEngineeringTasks().catch(() => []),
    ]);
    setShots(s.length);
    setTasks(t.filter((x) => ["进行中", "待整改", "逾期"].includes(x.status)).length);
  }, []);

  useEffect(() => {
    if (!visible) return;
    void load();
  }, [load, visible, pathname, uploadedTick]);

  if (!visible) return null;

  return (
    <TabBar
      activeKey={pathname}
      onChange={(key) => nav(key)}
      items={[
        { key: "/", title: "巡检", icon: <IconShoot /> },
        { key: "/review", title: "待处理", icon: <IconPhotos />, badge: shots },
        { key: "/tasks", title: "任务", icon: <IconTasks />, badge: tasks },
        { key: "/ledger", title: "台账", icon: <IconLedger /> },
        { key: "/me", title: "我的", icon: <IconUser /> },
      ]}
    />
  );
}
