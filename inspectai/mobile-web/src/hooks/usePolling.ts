import { useEffect, useRef } from "react";

// ===== 轮询 hook =====
//
// 项目里有两处轮询等后端异步任务:
//   填报页  等 AI 把字段识别出来(实测 12~23 秒)
//   预览页  等 AI 把日报总结生成出来
// 原来各写了一套,逻辑还不一样 —— 而其中一套刚出过事:
// 页面被卸载后轮询中断,后端算完了页面也不知道(见 2026-08-03 的修复)。
//
// 【为什么不是 setInterval】
// 定时器不管上一次有没有回来就发下一次。弱网下一次请求要十几秒,
// setInterval 会把请求越堆越多,把本来就窄的带宽占死。
// 这里是"上一次结束 → 等一个间隔 → 再发下一次",串行,永远只有一个在飞。
//
// 【为什么要有次数上限】
// 后端任务可能永远不结束(挂了、被杀了)。没有上限的轮询会一直烧流量和电,
// 而巡检员的手机整天在外面。到顶就停,页面自己给个"可手动填写"的出路。

export interface PollingOptions<T> {
  /** 为 false 时不轮询 */
  enabled: boolean;
  /** 每次拿到的结果 */
  onTick: (value: T) => void;
  /** 返回 true 表示"等到了",停止轮询 */
  done: (value: T) => boolean;
  intervalMs?: number;
  /** 最多轮询多少次;到顶调 onGiveUp */
  maxTicks?: number;
  onGiveUp?: () => void;
}

export function usePolling<T>(
  fetcher: () => Promise<T>,
  options: PollingOptions<T>,
) {
  const { enabled, intervalMs = 2000, maxTicks = 40 } = options;

  // 这三个每次渲染都是新函数,不能进依赖数组(否则每渲染一次就重启一轮轮询)
  const ref = useRef(options);
  ref.current = options;
  const fetcherRef = useRef(fetcher);
  fetcherRef.current = fetcher;

  useEffect(() => {
    if (!enabled) return;
    let stopped = false;

    void (async () => {
      for (let i = 0; i < maxTicks; i++) {
        await new Promise((r) => setTimeout(r, intervalMs));
        if (stopped) return;
        let value: T;
        try {
          value = await fetcherRef.current();
        } catch {
          // 单次失败不终止整轮:弱网下丢一两次很正常,下一轮可能就通了。
          // 真正的终点是 done() 或次数用尽。
          continue;
        }
        if (stopped) return;
        ref.current.onTick(value);
        if (ref.current.done(value)) return;
      }
      if (!stopped) ref.current.onGiveUp?.();
    })();

    return () => {
      stopped = true;
    };
  }, [enabled, intervalMs, maxTicks]);
}
