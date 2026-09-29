import { Checkbox, Space, Switch, Tooltip } from "antd";

import { WEEKDAY_OPTIONS } from "../api/mgmt";

/**
 * 执行日:勾哪几天 + 要不要跳过法定节假日。
 *
 * 【计划和推送群共用这一个】后端只有一套判断(runsOnDay)和一套校验,
 * 前端也只有这一个选择器 —— 各画一份的话,迟早一边能选的另一边存不进去,
 * 或者同一个值在两处显示成两种说法。
 *
 * 【没有单独的「每天」选项】七天全勾就是每天,再摆一个「每天」是同一件事两个入口。
 * 【跳过节假日和星期可以组合】周一到周五 + 跳过 = 法定工作日;
 * 周一到周六 + 跳过(六天班)、七天 + 跳过(平时天天推、放假才停)也都要能表达。
 * 打开后:放假不执行,调休上班那天照常执行(不管勾没勾那个星期)。
 */

export interface DayRule {
  /** "1,2,3,4,5"(1=周一 … 7=周日)。空 = 每天 */
  weekdays: string;
  followCalendar: boolean;
}

const ALL = WEEKDAY_OPTIONS.map((w) => w.value as string);
const WORKWEEK = "1,2,3,4,5";

/** 第一版把「法定工作日」存成 weekdays="workday",读出来换算成 周一到周五 + 跳过 */
export function toDayRule(weekdays?: string | null, followCalendar?: boolean | null): DayRule {
  const w = (weekdays || "").trim();
  if (w === "workday") return { weekdays: WORKWEEK, followCalendar: true };
  return { weekdays: w, followCalendar: !!followCalendar };
}

function pickedOf(weekdays: string): string[] {
  const list = weekdays.split(",").map((x) => x.trim()).filter(Boolean);
  return list.length ? list : ALL; // 空 = 每天
}

/** 给人看的一句话:每天 / 周一至周五 / 周一三五 / 法定工作日 / 每天 · 跳过节假日 */
export function dayRuleText(weekdays?: string | null, followCalendar?: boolean | null): string {
  const r = toDayRule(weekdays, followCalendar);
  const picked = [...pickedOf(r.weekdays)].sort();
  const key = picked.join(",");
  if (r.followCalendar && key === WORKWEEK) return "法定工作日";
  const base =
    picked.length === 7
      ? "每天"
      : key === WORKWEEK
        ? "周一至周五"
        : "周" + picked.map((d) => WEEKDAY_OPTIONS.find((w) => w.value === d)?.label || d).join("");
  return r.followCalendar ? `${base} · 跳过节假日` : base;
}

export default function DayRulePicker({
  value,
  onChange,
  disabled,
  size,
}: {
  value?: DayRule;
  onChange?: (v: DayRule) => void;
  disabled?: boolean;
  size?: "small" | "middle";
}) {
  const rule = toDayRule(value?.weekdays, value?.followCalendar);
  const picked = new Set(pickedOf(rule.weekdays));

  return (
    <Space size={14} wrap>
      <Space size={6} wrap>
        {WEEKDAY_OPTIONS.map((d) => {
          const on = picked.has(d.value);
          return (
            <Checkbox
              key={d.value}
              checked={on}
              // 【最后一个勾不让去掉】一个都不勾存下来就是空串 = 每天,
              // 人点的是"去掉周五",得到的却是"天天都执行"
              disabled={disabled || (on && picked.size === 1)}
              onChange={(e) => {
                const next = new Set(picked);
                if (e.target.checked) next.add(d.value);
                else next.delete(d.value);
                // 七天全勾存成空串 —— 和存量的"每天"是同一个写法,不出现两种
                const list = [...next].sort();
                onChange?.({ ...rule, weekdays: list.length === 7 ? "" : list.join(",") });
              }}
            >
              {d.label}
            </Checkbox>
          );
        })}
      </Space>
      <Space size={8}>
        <Switch
          size="small"
          disabled={disabled}
          checked={rule.followCalendar}
          onChange={(v) => onChange?.({ ...rule, followCalendar: v })}
        />
        <Tooltip title="按工作日历:放假不执行,调休上班那天照常执行">
          <span style={{ fontSize: size === "small" ? 13 : 14, cursor: "help" }}>跳过法定节假日</span>
        </Tooltip>
      </Space>
    </Space>
  );
}
