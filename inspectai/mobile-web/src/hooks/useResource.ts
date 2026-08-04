import { useCallback, useEffect, useRef, useState } from "react";

import { Toast } from "@/ui";

// ===== 取数 hook =====
//
// 体检时数出来:11 个页面写了 11 遍 useEffect + try/catch + Toast,
// 而其中只有 2 个做了竞态防护。剩下 9 个是这样的:
//
//   useEffect(() => {
//     void (async () => {
//       try { setData(await load()); } catch { Toast.show({content: "加载失败"}); }
//     })();
//   }, []);
//
// 少的那层保护会在两种情况下咬人:
//   1. 慢响应盖掉新状态 —— 切了筛选条件,旧请求后到,把新结果覆盖了
//   2. 组件卸载后 setState —— 用户点得快,页面已经走了,响应才回来
// 这两件事在办公室 wifi 下几乎撞不上,在机房弱网下天天发生。
//
// 【为什么用序号而不是 AbortController 判定"谁说了算"】
// abort 只能取消请求,不能保证"已经在飞行途中、马上要 resolve 的那个"不写状态。
// 序号是最后一道闸:只有最新一次发起的结果被采纳。两者都要 ——
// signal 省流量,序号保正确。

export interface ResourceState<T> {
  data: T | null;
  loading: boolean;
  /** 失败原因;成功后清空 */
  error: string | null;
  /** 手动重新加载(下拉刷新、操作完刷新列表) */
  reload: () => void;
}

export interface ResourceOptions {
  /** 失败时的提示文案;传 null 表示不弹提示(由调用方自己处理) */
  errorText?: string | null;
  /** 为 false 时不发起请求(等依赖就绪) */
  enabled?: boolean;
}

export function useResource<T>(
  load: (signal: AbortSignal) => Promise<T>,
  deps: readonly unknown[],
  options: ResourceOptions = {},
): ResourceState<T> {
  const { errorText = "加载失败", enabled = true } = options;

  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(enabled);
  const [error, setError] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  // 只认最新一次发起的结果
  const runIdRef = useRef(0);
  // load 每次渲染都是新函数,放进依赖会无限重启;用 ref 取最新的那个
  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    if (!enabled) {
      setLoading(false);
      return;
    }
    const myRun = ++runIdRef.current;
    const ctl = new AbortController();
    setLoading(true);

    void (async () => {
      try {
        const value = await loadRef.current(ctl.signal);
        if (myRun !== runIdRef.current) return; // 已被更新的一次取代
        setData(value);
        setError(null);
      } catch (err) {
        if (myRun !== runIdRef.current) return;
        if (ctl.signal.aborted) return; // 自己取消的,不算错误
        const msg = err instanceof Error ? err.message : "加载失败";
        setError(msg);
        // errorText 传 null = 调用方自己处理(比如角标那种静默降级)
        if (errorText !== null) Toast.show({ content: errorText });
      } finally {
        if (myRun === runIdRef.current) setLoading(false);
      }
    })();

    return () => {
      // 卸载/依赖变化:让飞行中的请求作废,并中止它省掉后续流量
      runIdRef.current++;
      ctl.abort();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, enabled, nonce]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);

  return { data, loading, error, reload };
}
