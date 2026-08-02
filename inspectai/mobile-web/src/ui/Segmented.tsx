import ArcoRadio from "@arco-design/mobile-react/esm/radio";
import "@arco-design/mobile-react/esm/radio/style/css";

// ===== 分段选择 =====
//
// 替掉原生 <select>。原生下拉在移动端弹的是系统控件,样式完全不可控,
// 灰底高亮 + 白框,和界面两套语言。
//
// 【为什么不用 Picker / ActionSheet】那两个是给"选项多、要滚"准备的。
// 实测这套模板的选择字段【72 个里全部是 2 项(是/否)】,没有一个超过 2 项。
// 两个选项还要弹层,等于把一次点击变成两次(开弹层 + 选) ——
// 巡检员一页填十几项,这个差别是实打实的。分段按钮直接点,一次到位。
//
// 用 Radio.Group 而不是自己写按钮组:换来键盘可达、aria 语义、
// 单选互斥这些不用自己维护的东西。视觉上通过 CSS 做成分段条。

export interface SegmentedProps {
  options: string[];
  value: string;
  onChange: (value: string) => void;
  className?: string;
}

export function Segmented({ options, value, onChange, className }: SegmentedProps) {
  return (
    <ArcoRadio.Group
      className={className ? `seg ${className}` : "seg"}
      value={value}
      onChange={(v) => onChange(String(v ?? ""))}
      options={options.map((o) => ({ value: o, children: o }))}
    />
  );
}
