import ArcoInput from "@arco-design/mobile-react/esm/input";
import "@arco-design/mobile-react/esm/input/style/css";
import type { KeyboardEvent } from "react";

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
  disabled?: boolean;
  className?: string;
  onEnterPress?: () => void;
}

export function Input({
  value,
  onChange,
  type = "text",
  autoComplete,
  onEnterPress,
  ...rest
}: InputProps) {
  return (
    <ArcoInput
      value={value}
      type={type}
      // 关键:用 onInput 而非 onChange,否则打字时不更新
      onInput={(_e, v: string) => onChange?.(v)}
      onPressEnter={onEnterPress ? (_e: KeyboardEvent<HTMLInputElement>) => onEnterPress() : undefined}
      nativeProps={autoComplete ? { autoComplete } : undefined}
      border="none"
      {...rest}
    />
  );
}
