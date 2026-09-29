import { Checkbox, Segmented, Space } from "antd";

import { WEEKDAY_OPTIONS } from "../api/mgmt";

/**
 * 执行日:每天 / 按星期 / 法定工作日。
 *
 * 【计划和推送群共用这一个】后端只有一套判断(runsOnDay)和一套校验,
 * 前端也只有这一个选择器 —— 各画一份的话,迟早一边能选的另一边存不进去,
 * 或者同一个值在两处显示成两种说法。
 *
 * 值就是后端的 weekdays 字段:"" = 每天,"1,2,3" = 按星期,"workday" = 法定工作日。
 */

export const DAY_RULE_WORKDAY = "workday";

type Mode = "every" | "weekly" | "workday";

function modeOf(v: string): Mode {
  const s = (v || "").trim();
  if (!s) return "every";
  if (s === DAY_RULE_WORKDAY) return "workday";
  return "weekly";
}

/** 给人看的一句话:每天 / 法定工作日 / 周一三五 */
export function dayRuleText(v?: string | null): string {
  const m = modeOf(v || "");
  if (m === "every") return "每天";
  if (m === "workday") return "法定工作日";
  const picked = (v || "").split(",").map((x) => x.trim()).filter(Boolean);
  if (picked.length === 7) return "每天";
  return (
    "周" +
    [...picked]
      .sort()
      .map((d) => WEEKDAY_OPTIONS.find((w) => w.value === d)?.label || d)
      .join("")
  );
}

export default function DayRulePicker({
  value,
  onChange,
  disabled,
  size,
}: {
  value?: string;
  onChange?: (v: string) => void;
  disabled?: boolean;
  size?: "small" | "middle";
}) {
  const v = value || "";
  const mode = modeOf(v);
  const picked = new Set(mode === "weekly" ? v.split(",").map((x) => x.trim()).filter(Boolean) : []);

  return (
    <Space size={10} wrap>
      <Segmented
        size={size}
        disabled={disabled}
        value={mode}
        onChange={(m) => {
          if (m === "every") onChange?.("");
          else if (m === "workday") onChange?.(DAY_RULE_WORKDAY);
          // 切到「按星期」从周一到周五起步 —— 从一个都没勾开始的话,
          // 它存下来等于"每天",和刚点的那个选项对不上
          else onChange?.("1,2,3,4,5");
        }}
        options={[
          { value: "every", label: "每天" },
          { value: "weekly", label: "按星期" },
          { value: "workday", label: "法定工作日" },
        ]}
      />
      {mode === "weekly" && (
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
                  onChange?.([...next].sort().join(","));
                }}
              >
                {d.label}
              </Checkbox>
            );
          })}
        </Space>
      )}
    </Space>
  );
}
