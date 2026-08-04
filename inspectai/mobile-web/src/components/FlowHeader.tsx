import type { ReactNode } from "react";

import { NavBar, Progress } from "@/ui";

// ===== 流程页统一头部 =====
//
// 旧版把顶栏 + 进度条集中在 setScene() 里按屏配置(TITLES / PROGRESS 两张表),
// 所以每屏的标题、返回去向、进度百分比是一处可查的。新版重构成多路由后,
// 这套东西散掉了 —— 8 个页面各写一个 .flow-head,没有返回键也没有进度。
// 这里把它收回一处,对齐旧版的做法。
//
// 步骤百分比沿用旧版 PROGRESS 表的语义(照片 → 场景 → 字段 → 提交 递进),
// 只是把旧版的 camera→loading 换成新版的「选照片」一屏。
const STEP_PERCENT: Record<string, number> = {
  review: 15, // 选择本次巡检的照片
  classify: 30, // 确认巡检场景
  record: 60, // 填/确认日报字段
  preview: 85, // 确认并提交
};

export interface FlowHeaderProps {
  title: ReactNode;
  /** 返回去向。不传即不显示返回键(拍照台) */
  onBack?: () => void;
  /** 右侧操作(旧版 #topAction) */
  action?: { text: string; onClick: () => void };
  /**
   * 处于巡检主流程的哪一步。传了才显示进度条 ——
   * 旧版同样只在流程屏显示,任务/台账/档案/审批屏不显示。
   */
  step?: keyof typeof STEP_PERCENT;
  /** 深色科技风(拍照台 / 识别中) */
  dark?: boolean;
}

export default function FlowHeader({
  title,
  onBack,
  action,
  step,
  dark,
}: FlowHeaderProps) {
  return (
    <>
      <NavBar title={title} onBack={onBack} action={action} dark={dark} />
      {step ? <Progress percent={STEP_PERCENT[step]} /> : null}
    </>
  );
}
