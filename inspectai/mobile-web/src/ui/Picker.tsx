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
  /**
   * 这几项现在选不了,以及为什么。键是选项,值是一句话理由。
   *
   * 【为什么是"变灰 + 说明",不是直接从列表里去掉】抄表一条记录抄六台表,
   * 一台只能归一行。去掉的话人只会觉得"怎么没有 Z3",不知道它在哪、
   * 也不知道该怎么办。摆在那儿写明「第 3 张已选」,他才知道要先去
   * 把第 3 张那行清掉 —— 少一次困惑,而不是少一个选项。
   */
  disabledOptions?: Record<string, string>;
}

export function Picker({
  options,
  value,
  onChange,
  placeholder = "请选择",
  disabledOptions,
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
          {options.map((o) => {
            // 当前选中的这一项永远可选 —— 否则人打开自己这一行的选择器,
            // 看到的是自己已经选的那台变灰了,像是出了故障。
            const why = o === value ? "" : disabledOptions?.[o] || "";
            return (
              <li key={o}>
                <button
                  type="button"
                  role="option"
                  aria-selected={o === value}
                  aria-disabled={Boolean(why)}
                  className={
                    why ? "dd-item is-off" : o === value ? "dd-item is-on" : "dd-item"
                  }
                  onClick={() => {
                    if (why) return; // 理由就写在这一行上,点了不动才对
                    onChange(o);
                    setOpen(false);
                  }}
                >
                  <span>{o}</span>
                  {why && <em className="dd-why">{why}</em>}
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
