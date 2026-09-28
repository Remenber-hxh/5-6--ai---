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
   * 【为什么是"变灰",不是直接从列表里去掉】抄表一条记录抄六台表,
   * 一台只能归一行。去掉的话人只会觉得"怎么没有 Z3",不知道它在哪、
   * 也不知道该怎么办;摆在那儿变灰,至少说明"它存在,只是现在轮不到"。
   *
   * 【理由不显示在列表里】一行选项后面挂一句灰字,九个选项就是九条,
   * 列表被说明文字撑得比选项还满 —— 而人只是想点一台表。
   * 理由留在 aria-label 里,读屏能念到,眼睛不用为它多读一遍。
   */
  disabledOptions?: Record<string, string>;
  /**
   * 给了就在列表最上面多一项「清除」,把这一行的选择放掉。
   *
   * 【为什么需要它】一台设备只能归一行,已经归了别行的在这里是灰的。
   * 六张照片都认领完之后,在用的设备就全是灰的 —— 想把两行的归属换过来,
   * 得先有一行让出自己那台。没有这个出口的话,这一页就调不动了
   * (2026-09-22 线上两个水表填不了,就是卡在这儿)。
   *
   * 只在已经选了东西的时候显示:空着的行点"清除"没有任何意义。
   */
  onClear?: () => void;
  /** 整个选择器暂时点不开(比如这一行正在保存) */
  disabled?: boolean;
}

/** 列表最高 208px(约 5 项),和 CSS 的 max-height 一致 */
const LIST_MAX = 208;
const ITEM_H = 41;

/**
 * 往下展开放不放得下。
 *
 * 【为什么要算】确认页底部钉着「保存并预览日报」,最后两三行的下拉往下一展开,
 * 选项全压在按钮底下,点不着 —— 要先滚页面才露出来,而人不知道要滚。
 * 放不下、上面又放得下,就往上开。下边界取底部按钮条的上沿(没有就是屏幕底)。
 */
function shouldOpenUp(box: HTMLElement, rows: number): boolean {
  const r = box.getBoundingClientRect();
  const foot = document.querySelector(".flow-foot");
  const bottom = foot ? foot.getBoundingClientRect().top : window.innerHeight;
  const need = Math.min(LIST_MAX, rows * ITEM_H + 8) + 6;
  const below = bottom - r.bottom;
  const above = r.top - 56; // 顶栏
  return below < need && above > below;
}

export function Picker({
  options,
  value,
  onChange,
  placeholder = "请选择",
  disabledOptions,
  onClear,
  disabled,
}: PickerProps) {
  const [open, setOpen] = useState(false);
  const [up, setUp] = useState(false);
  const boxRef = useRef<HTMLDivElement>(null);

  function toggle() {
    if (disabled) return;
    if (open) {
      setOpen(false);
      return;
    }
    if (boxRef.current) {
      const rows = options.length + (onClear && value ? 1 : 0);
      setUp(shouldOpenUp(boxRef.current, rows));
    }
    setOpen(true);
  }

  // 存着的时候别让列表还开着
  useEffect(() => {
    if (disabled) setOpen(false);
  }, [disabled]);

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
        onClick={toggle}
        disabled={disabled}
        aria-expanded={open}
      >
        <span className="dd-text">{value || placeholder}</span>
        <span className={open ? "dd-arrow is-open" : "dd-arrow"} aria-hidden />
      </button>

      {open && (
        <ul className={up ? "dd-list is-up" : "dd-list"} role="listbox">
          {/* 【摆在最上面】它不是一个"选项",是一个动作:放掉这一行的选择,
              好让这台设备在别的行里重新变成可选。混在设备名中间会被当成
              一台叫"清除"的表。 */}
          {onClear && value && (
            <li>
              <button
                type="button"
                className="dd-item is-clear"
                onClick={() => {
                  onClear();
                  setOpen(false);
                }}
              >
                清除
              </button>
            </li>
          )}
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
                  aria-label={why ? `${o}(${why})` : undefined}
                  className={
                    why ? "dd-item is-off" : o === value ? "dd-item is-on" : "dd-item"
                  }
                  onClick={() => {
                    if (why) return; // 变灰的点了不动才对
                    onChange(o);
                    setOpen(false);
                  }}
                >
                  <span>{o}</span>
                </button>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
