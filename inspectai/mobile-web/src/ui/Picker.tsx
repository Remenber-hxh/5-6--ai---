import ArcoPicker from "@arco-design/mobile-react/esm/picker";
import "@arco-design/mobile-react/esm/picker/style/css";
import { useState } from "react";

// ===== 选择器 =====
//
// 交互上就是下拉,但弹出的是【底部选择面板】而不是系统控件 ——
// 原生 <select> 弹的是浏览器/系统自己的下拉框,灰底高亮、白框、字号不受控,
// 和界面完全两套语言,这也是它难看的根源。Picker 由设计系统渲染,可控。
//
// 触发器自己写成一行文本 + 箭头,和字段行的右对齐排版一致;
// 面板交给组件,滚轮、确定/取消、遮罩、动画都不用自己维护。

export interface PickerProps {
  options: string[];
  value: string;
  onChange: (value: string) => void;
  /** 未选时的占位 */
  placeholder?: string;
  title?: string;
}

export function Picker({ options, value, onChange, placeholder = "请选择", title }: PickerProps) {
  const [open, setOpen] = useState(false);

  return (
    <>
      <button
        type="button"
        className={value ? "pk-trigger" : "pk-trigger is-empty"}
        onClick={() => setOpen(true)}
      >
        <span className="pk-text">{value || placeholder}</span>
        <span className="pk-arrow" aria-hidden />
      </button>
      <ArcoPicker
        visible={open}
        title={title}
        cascade={false}
        data={[options.map((o) => ({ label: o, value: o }))]}
        // 未选过时默认落在第一项上,避免打开就是空白
        value={[value || options[0]]}
        okText="确定"
        dismissText="取消"
        onOk={(v) => {
          onChange(String(v?.[0] ?? ""));
          setOpen(false);
        }}
        onDismiss={() => setOpen(false)}
        onHide={() => setOpen(false)}
      />
    </>
  );
}
