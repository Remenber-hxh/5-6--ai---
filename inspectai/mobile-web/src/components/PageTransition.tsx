import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";
import { CSSTransition, SwitchTransition } from "react-transition-group";

import type { ReactNode } from "react";

// ===== 页面转场 =====
//
// 转场不是装饰,它要传递【层级关系】—— 这是"像不像 app"里很实的一环:
//   平级切换(底部 tab 之间)→ 淡入淡出。它们没有上下级,滑动会误导。
//   下钻(台账 → 设备详情、待处理 → 填日报)→ 从右侧推入,"我进去了一层"
//   返回 → 向右退出,"我出来了"
//
// 只做这三种,不做更多 —— 用户是戴手套、赶时间的巡检员,
// 炫技动效只会让他们多等。总时长压在 200ms 内。
//
// 【只用 transform / opacity】这两个属性走合成器,不触发重排重绘;
// 动 width/height/top 会让整页每帧重新布局,在低端安卓上直接掉帧。

/** 底部 tab 的五个顶层目的地,互为平级 */
const TOP_LEVEL = new Set(["/", "/review", "/tasks", "/ledger", "/me"]);

function depth(path: string): number {
  return TOP_LEVEL.has(path) ? 0 : 1;
}

export default function PageTransition({ children }: { children: ReactNode }) {
  const location = useLocation();
  const prev = useRef(location.pathname);
  const nodeRef = useRef<HTMLDivElement>(null);

  const from = depth(prev.current);
  const to = depth(location.pathname);
  // 同级 = 淡入淡出;进入更深一层 = 推入;退回浅层 = 退出
  const kind = from === to ? "fade" : to > from ? "push" : "pop";

  // 【必须在 effect 里更新,不能在 render 里】render 期间改 ref 是 React 的
  // 反模式:StrictMode 会双次 render,第一次就把 prev 改成新路径,
  // 第二次算出 from === to,于是 push / pop 永远不触发,全都退化成 fade。
  // 这个 bug 只在开发环境的 StrictMode 下暴露 —— 但错的是写法,不是 StrictMode。
  useEffect(() => {
    prev.current = location.pathname;
  }, [location.pathname]);

  return (
    <SwitchTransition mode="out-in">
      <CSSTransition
        key={location.pathname}
        nodeRef={nodeRef}
        classNames={`pg-${kind}`}
        timeout={{ enter: 180, exit: 120 }}
        // 页面是 .app-shell 的 flex 子元素,转场时不能把它从流里摘出去,
        // 否则底栏会在两页之间跳一下
        unmountOnExit
      >
        <div className="pg-layer" ref={nodeRef}>
          {children}
        </div>
      </CSSTransition>
    </SwitchTransition>
  );
}
