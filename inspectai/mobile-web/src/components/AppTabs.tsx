import { useLocation, useNavigate } from "react-router-dom";

import { getBadgeCounts } from "@/api/inspection";
import { useResource } from "@/hooks/useResource";
import { usePending } from "@/store/pending";
import { TabBar } from "@/ui";

import { IconPhotos, IconShoot, IconTasks, IconUser } from "./icons";

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
// 台账不在底栏:它是【查询类】功能,不是巡检员的日常动线
// (拍照 → 处理照片 → 销任务)。入口保留在「我的 → 设备健康」。
// 四项比五项也更宽松,每个 tab 的点击区更大 —— 现场戴手套操作有意义。
const TAB_ROUTES = ["/", "/review", "/tasks", "/me"] as const;

// 哪些顶层页是深色科技风。目前只有拍照台 —— 它是取景语境。
// 其余(待处理/任务/台账/我的)都是浅色 iOS 风。底栏据此跟随,
// 否则在浅色页底部就是一条焊上去的黑带。
const DARK_ROUTES = new Set<string>(["/"]);

export default function AppTabs() {
  const nav = useNavigate();
  const { pathname } = useLocation();
  // 拍完照上传成功会推进这个计数,角标要跟着动,否则用户拍完看角标没变
  const uploadedTick = usePending((s) => s.uploadedTick);

  const visible = (TAB_ROUTES as readonly string[]).includes(pathname);

  // 【为什么不再拉两个完整列表】
  // 角标只要两个数字,原来却把 offline-shots(82KB)和 engineering/tasks(38KB)
  // 整个拉下来取 length,而这个 effect 依赖 pathname —— 每切一次标签重来一遍。
  // 实测连切 5 次标签下行 2MB。后端现在直接给数,几十字节。
  //
  // errorText: null —— 角标是辅助信息,拉不到就不显示,不该弹提示打扰主流程。
  const { data } = useResource(
    (signal) => getBadgeCounts(signal),
    [pathname, uploadedTick],
    {
      enabled: visible,
      errorText: null,
    },
  );
  const shots = data?.shots ?? 0;
  const tasks = data?.tasks ?? 0;

  if (!visible) return null;

  return (
    <TabBar
      activeKey={pathname}
      dark={DARK_ROUTES.has(pathname)}
      onChange={(key) => nav(key)}
      items={[
        { key: "/", title: "巡检", icon: <IconShoot /> },
        { key: "/review", title: "待处理", icon: <IconPhotos />, badge: shots },
        { key: "/tasks", title: "任务", icon: <IconTasks />, badge: tasks },
        { key: "/me", title: "我的", icon: <IconUser /> },
      ]}
    />
  );
}
