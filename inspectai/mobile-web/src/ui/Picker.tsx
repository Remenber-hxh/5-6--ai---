import { useEffect, useRef, useState } from "react";

// ===== 下拉选择 =====
//
// 点一下,在字段行下方展开一个整宽的选项列表 —— 经典下拉的形态:
// 选项左对齐、逐行排列、超过若干项可滚动、当前值高亮。
//
// 走过的四版,记下来免得再兜圈:
//   1. 原生 <select>   系统控件,灰底高亮/白框/字号全不可控,和界面两套语言
//   2. 分段按钮        少一次点击,但视觉过重,被否
//   3. Picker 底部面板  滚轮 + 确定/取消,两项选择用它太隆重
//   4. Popover.Menu    就地弹小气泡,但默认深色、宽度按内容,不是"下拉框"的样子
//
// 最后自己写:Arco 没有"整宽下拉列表"这个形态(它的 dropdown 是给筛选栏用的,
// 撑满屏幕宽)。这里要的是贴着字段行的一小块列表,自己写反而更短更可控。

export interface PickerProps {
  options: string[];
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}

export function Picker({
  options,
  value,
  onChange,
  placeholder = "请选择",
}: PickerProps) {
  const [open, setOpen] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);

  // 点外面关掉。用 pointerdown 而不是 click:click 会和触发器自己的
  // onClick 打架(先关再开,看起来像没反应)。
  useEffect(() => {
    if (!open) return;
    const onDown = (e: PointerEvent) => {
      if (!boxRef.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("pointerdown", onDown);
    return () => document.removeEventListener("pointerdown", onDown);
  }, [open]);

  return (
    <div className="dd" ref={boxRef}>
      <button
        type="button"
        className={value ? "dd-trigger" : "dd-trigger is-empty"}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="dd-text">{value || placeholder}</span>
        <span className={open ? "dd-arrow is-open" : "dd-arrow"} aria-hidden />
      </button>

      {open && (
        <ul className="dd-list" role="listbox">
          {options.map((o) => (
            <li key={o}>
              <button
                type="button"
                role="option"
                aria-selected={o === value}
                className={o === value ? "dd-item is-on" : "dd-item"}
                onClick={() => {
                  onChange(o);
                  setOpen(false);
                }}
              >
                {o}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
