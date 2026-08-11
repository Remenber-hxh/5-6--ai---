import ArcoInput from "@arco-design/mobile-react/esm/input";
import "@arco-design/mobile-react/esm/input/style/css";
import type { KeyboardEvent, ReactNode } from "react";

// ===== Input 适配层 =====
//
// 差异(容易静默踩中):
//   antd-mobile:onChange(value)      —— 逐字触发,直接给值
//   Arco:       onChange(e, value)   —— 【失焦时】才触发
//               onInput(e, value)    —— 逐字触发
//
// 若把 onChange 原样映射过去,输入框会变成"打字时不更新",表现为登录框没反应。
// 这里对外保留 antd-mobile 的 onChange(value) 语义,内部接 Arco 的 onInput。

export interface InputProps {
  value?: string;
  /** 逐字触发,直接给值(对齐 antd-mobile 语义) */
  onChange?: (value: string) => void;
  placeholder?: string;
  type?: string;
  clearable?: boolean;
  autoComplete?: string;
  /** 手机输入法干预。注册码这种全大写的字段要 "characters",
      账号这种区分大小写的要 "none" —— 默认行为会把首字母自动大写。 */
  autoCapitalize?: "none" | "sentences" | "words" | "characters";
  autoCorrect?: "on" | "off";
  spellCheck?: boolean;
  disabled?: boolean;
  className?: string;
  onEnterPress?: () => void;
  /** 框内右侧内容(密码小眼睛等) */
  suffix?: ReactNode;
}

export function Input({
  value,
  onChange,
  type = "text",
  autoComplete,
  autoCapitalize,
  autoCorrect,
  spellCheck,
  onEnterPress,
  ...rest
}: InputProps) {
  // 【这些属性必须走 nativeProps】Arco 不认这几个名字,直接摊在组件上
  // 会被它当未知属性丢掉 —— 不报错,只是输入法照旧自作主张。
  const native: Record<string, unknown> = {};
  if (autoComplete) native.autoComplete = autoComplete;
  if (autoCapitalize) native.autoCapitalize = autoCapitalize;
  if (autoCorrect) native.autoCorrect = autoCorrect;
  if (spellCheck !== undefined) native.spellCheck = spellCheck;
  return (
    <ArcoInput
      value={value}
      type={type}
      // 关键:用 onInput 而非 onChange,否则打字时不更新
      onInput={(_e, v: string) => onChange?.(v)}
      onPressEnter={
        onEnterPress
          ? (_e: KeyboardEvent<HTMLInputElement>) => onEnterPress()
          : undefined
      }
      nativeProps={Object.keys(native).length ? native : undefined}
      border="none"
      {...rest}
    />
  );
}
