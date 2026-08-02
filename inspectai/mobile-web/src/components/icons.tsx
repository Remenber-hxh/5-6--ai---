// ===== 细线图标 =====
//
// 手写 SVG,不引图标库:整个 app 只用到 5 个图标,为它拉一个几十 KB 的
// 图标包不划算,而且图标库的线宽/圆角风格未必对得上这套工业科技风。
//
// 【不要用 emoji 代替】个人页一度用 📋🖼📈 —— 除了在设置列表里显得业余,
// 🖼(U+1F5BC)在很多系统上根本没有字形,直接渲染成豆腐块 □。
//
// stroke 用 currentColor:颜色跟随父级,深色屏浅色屏同一套图标都能用。

const IC = {
  width: 22,
  height: 22,
  viewBox: "0 0 24 24",
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.7,
  strokeLinecap: "round",
  strokeLinejoin: "round",
} as const;

/** 我的任务:带勾的剪贴板 */
export function IconTasks() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="5.5" y="5" width="13" height="15.5" rx="2.2" />
      <path d="M9 5.2V3.9a.9.9 0 0 1 .9-.9h4.2a.9.9 0 0 1 .9.9v1.3" />
      <path d="m9.2 13.6 1.9 1.9 3.6-3.8" />
    </svg>
  );
}

/** 设备健康:折线图 */
export function IconLedger() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="4" y="4" width="16" height="16" rx="3" />
      <path d="M7.5 13h2.3l1.5-4 2.3 6.5 1.5-3.7h1.5" />
    </svg>
  );
}

/** 待审批:徽章 + 绶带 */
export function IconApproval() {
  return (
    <svg {...IC} aria-hidden>
      <circle cx="12" cy="9.5" r="5.2" />
      <path d="m9.9 9.5 1.5 1.5 3-3.1" />
      <path d="m9.2 14.6-1.4 4.4 4.2-2 4.2 2-1.4-4.4" />
    </svg>
  );
}

/** 待处理照片:叠放的两张图 */
export function IconPhotos() {
  return (
    <svg {...IC} aria-hidden>
      <rect x="7.5" y="4" width="12.5" height="12.5" rx="2.2" />
      <path d="M4 9.5v8A2.5 2.5 0 0 0 6.5 20H16" />
    </svg>
  );
}

/** 退出:门 + 箭头 */
export function IconLogout() {
  return (
    <svg {...IC} aria-hidden>
      <path d="M14.5 4.5h3A1.5 1.5 0 0 1 19 6v12a1.5 1.5 0 0 1-1.5 1.5h-3" />
      <path d="M10 15.5 13.5 12 10 8.5" />
      <path d="M13.5 12H4.5" />
    </svg>
  );
}
