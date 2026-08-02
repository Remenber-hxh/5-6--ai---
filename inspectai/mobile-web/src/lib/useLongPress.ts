import { useCallback, useEffect, useRef, useState } from "react";

// ===== 长按手势 =====
//
// 用在快门上:长按调出相册入口。有两个必须处理的细节,少一个都会出怪事。
//
// 【1】长按后那一次 click 必须拦掉。
//   快门是 <label> 包 file input —— 长按松手时 label 照样触发 click,
//   于是"长按想选相册"变成"打开了相机"。这里在 click 阶段 preventDefault,
//   阻止 label 激活它关联的 input。
//   为什么保留 label 结构而不改成 button + input.click():
//   label 包 input 是最稳的写法,企微 webview 里程序化 click 有风险,
//   相机是主操作,不拿它冒险。
//
// 【2】手指移动要取消。
//   按住后滑动是"想滚动页面",不是长按。超过阈值就取消,否则用户一滑
//   就弹出相册,很烦。

const HOLD_MS = 400;
const MOVE_TOLERANCE = 10;

export function useLongPress(onLongPress: () => void) {
  const timer = useRef<number | null>(null);
  const start = useRef<{ x: number; y: number } | null>(null);
  // 标记这一轮按压是否已触发长按 —— click 阶段据此决定拦不拦
  const fired = useRef(false);
  const [holding, setHolding] = useState(false);

  const clear = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
    start.current = null;
    setHolding(false);
  }, []);

  useEffect(() => clear, [clear]);

  return {
    /** 按住中,给按钮做视觉反馈用 */
    holding,
    handlers: {
      onPointerDown: (e: React.PointerEvent) => {
        fired.current = false;
        start.current = { x: e.clientX, y: e.clientY };
        setHolding(true);
        timer.current = window.setTimeout(() => {
          fired.current = true;
          setHolding(false);
          onLongPress();
        }, HOLD_MS);
      },
      onPointerMove: (e: React.PointerEvent) => {
        const s = start.current;
        if (!s) return;
        if (Math.abs(e.clientX - s.x) > MOVE_TOLERANCE || Math.abs(e.clientY - s.y) > MOVE_TOLERANCE) {
          clear();
        }
      },
      onPointerUp: clear,
      onPointerCancel: clear,
      onPointerLeave: clear,
      onClick: (e: React.MouseEvent) => {
        // 长按已触发 → 拦掉这次 click,不让 label 打开相机
        if (fired.current) {
          e.preventDefault();
          fired.current = false;
        }
      },
    },
  };
}
