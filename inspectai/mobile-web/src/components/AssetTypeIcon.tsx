// ===== 设备类型图标 =====
//
// 台账里 35 台设备有 22 台没有封面照片(63%)。原来空的时候画一个细线小方框
// 占位,混在真实照片中间像"图挂了",整个列表显得脏。
//
// 换成按设备类型出图标:巡检员本来就是靠"这是台电梯还是水泵"来认行的,
// 类型图标比一个空框有信息量。有照片时仍然优先显示照片 —— 照片更具体。
//
// 图标沿用 icons.tsx 的画法:手写细线 SVG、stroke 用 currentColor。
// 只画 6 个形,类型再多也归到这 6 类里 —— 图标是用来快速分辨的,
// 每种类型都画一个独有形状反而谁也记不住。

const IC = {
  width: 22,
  height: 22,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.6,
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

/** 电梯 / 扶梯:轿厢加上下箭头 */
function Elevator() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="4.5" y="3.5" width="15" height="17" rx="2" />
      <path d="M12 3.5v17" />
      <path d="M8.2 12.4 9.6 10.6 11 12.4" />
      <path d="M13 11.6 14.4 13.4 15.8 11.6" />
    </svg>
  );
}

/** 水泵 / 消防泵:泵体加出水管 */
function Pump() {
  return (
    <svg {...IC} aria-hidden>
      <circle cx="10.5" cy="13.5" r="5.5" />
      <path d="M10.5 13.5V8h6.5v3" />
      <path d="M17 8h3.5" />
      <path d="M4 19.5h13" />
    </svg>
  );
}

/** 配电 / UPS / 变电:闪电 */
function Power() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="4.5" y="3.5" width="15" height="17" rx="2" />
      <path d="M12.8 7.5 9.5 12.6h3.4L11.2 16.8" />
    </svg>
  );
}

/** 水表 / 电表:表盘加指针 */
function Meter() {
  return (
    <svg {...IC} aria-hidden>
      <circle cx="12" cy="12" r="8" />
      <path d="M12 12l3.2-2.4" />
      <path d="M12 4v1.8M20 12h-1.8M12 20v-1.8M4 12h1.8" />
    </svg>
  );
}

/** 热水 / 锅炉:罐体加热浪 */
function Boiler() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="6" y="7.5" width="12" height="13" rx="2.5" />
      <path d="M9.5 4.5c0 1.2 1 1.2 1 2.4M13.5 4.5c0 1.2 1 1.2 1 2.4" />
      <path d="M9.5 13.5h5" />
    </svg>
  );
}

/** 兜底:通用设备箱 */
function Generic() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="3.5" y="6" width="17" height="12.5" rx="2" />
      <path d="M8 6V4.5h8V6" />
      <path d="M3.5 11.5h17" />
    </svg>
  );
}

// 按关键字归类,不按完整类型名精确匹配 —— 后台可以随手加新类型,
// 精确匹配会让新类型直接掉到兜底图标上。
const RULES: Array<[RegExp, () => JSX.Element]> = [
  [/电梯|扶梯|升降/, Elevator],
  [/泵/, Pump],
  [/变电|配电|强电|UPS|电井/i, Power],
  [/水表|电表|能耗|计量/, Meter],
  [/热水|锅炉|换热/, Boiler],
];

export default function AssetTypeIcon({ type }: { type?: string }) {
  const t = (type || "").trim();
  const hit = RULES.find(([re]) => re.test(t));
  const Icon = hit ? hit[1] : Generic;
  return <Icon />;
}
