import ArcoProgress from "@arco-design/mobile-react/esm/progress";
import "@arco-design/mobile-react/esm/progress/style/css";

// ===== Progress 适配层 =====
//
// 只用来做顶栏下方那条 3px 细进度条(旧版 #progressBar)。
//
// 注意不要用 Arco 的 `mode="nav"`:那会给它 position:fixed + 全宽,
// 按 window 定位。本项目是 .flow-screen 内部滚动,细条应该跟着 NavBar
// 当 flex 子元素排,所以走默认的 base 模式。

export interface ProgressProps {
  /** 0–100 */
  percent: number;
}

export function Progress({ percent }: ProgressProps) {
  return (
    <ArcoProgress
      className="flow-progress"
      percentage={percent}
      showPercent={false}
      trackStroke={3}
      progressStroke={3}
      // 旧版是 0.4s cubic-bezier(0.4,0,0.2,1) 的宽度过渡
      duration={400}
      mountedTransition
    />
  );
}
