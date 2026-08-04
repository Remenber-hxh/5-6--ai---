import ArcoDatePicker from "@arco-design/mobile-react/esm/date-picker";
import "@arco-design/mobile-react/esm/date-picker/style/css";
import { useState } from "react";

// ===== 日期时间字段 =====
//
// 「检查时间」这类字段原来是纯文本输入框,格式全靠人自觉 —— 手机小键盘上
// 敲 "2026-08-04 10:00" 十几个字符,错一个后端就解析不出来,而且巡检员
// 是戴着手套在机房里操作的。
//
// 用滚轮选择器:选出来的一定是合法时间,而且比敲字快得多。
//
// 【为什么统一成 "YYYY-MM-DD HH:mm"】
// 后端和旧版都用这个格式(initialFieldValues 里 time.Format("2006-01-02 15:04")),
// 这里必须一模一样 —— 字段值是要进日报和台账的,格式一变对不上历史数据。
//
// 扫过样式:date-picker 没有 .ios/.android 分支,不受平台判定影响。

/** 把字段里的 "2026-08-04 10:00" 解析成时间戳;解析不出就用当前时间 */
function parseTs(value: string): number {
  const m = value.trim().match(/^(\d{4})-(\d{2})-(\d{2})[ T](\d{2}):(\d{2})/);
  if (!m) return Date.now();
  const [, y, mo, d, h, mi] = m;
  return new Date(+y, +mo - 1, +d, +h, +mi).getTime();
}

function fmt(ts: number): string {
  const d = new Date(ts);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}`;
}

export interface DateFieldProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

export function DateField({
  value,
  onChange,
  placeholder = "请选择时间",
}: DateFieldProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        className={value ? "dd-trigger" : "dd-trigger is-empty"}
        onClick={() => setOpen(true)}
      >
        <span className="dd-text">{value || placeholder}</span>
        <span className="dd-arrow" aria-hidden />
      </button>
      <ArcoDatePicker
        visible={open}
        title="选择时间"
        mode="datetime"
        currentTs={parseTs(value)}
        // 巡检时间不可能是未来 —— 允许选未来只会产生没法解释的记录
        maxTs={Date.now()}
        onOk={(ts) => {
          onChange(fmt(typeof ts === "number" ? ts : ts[0]));
          setOpen(false);
        }}
        onDismiss={() => setOpen(false)}
        onHide={() => setOpen(false)}
      />
    </>
  );
}
