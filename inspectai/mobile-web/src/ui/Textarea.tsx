import ArcoTextarea from "@arco-design/mobile-react/esm/textarea";
import "@arco-design/mobile-react/esm/textarea/style/css";

// ===== 多行输入 =====
//
// 「修改理由」这类字段:一句话能说清,但也可能要写三行。用单行 input 会让
// 长理由被裁在框里看不见 —— 审批的人看不全理由就只能凭感觉批,那这一栏
// 就白填了。
//
// autosize:跟着内容长高,不预留一大块空白,也不出现内部滚动条。
// showStatistics + maxLength:写超了当场看得见,而不是提交时才被后端弹回来。

export interface TextareaProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  maxLength?: number;
  rows?: number;
  className?: string;
}

export function Textarea({
  value,
  onChange,
  placeholder,
  maxLength,
  rows = 2,
  className,
}: TextareaProps) {
  return (
    <ArcoTextarea
      className={className}
      value={value}
      onChange={(_e, v) => onChange(v)}
      placeholder={placeholder}
      maxLength={maxLength}
      rows={rows}
      autosize
      showStatistics={Boolean(maxLength)}
      border="none"
    />
  );
}
