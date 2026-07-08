import { animate, createScope, stagger } from "animejs";
import { memo, useEffect, useRef, useState } from "react";

// 点阵矩阵背景 — anime.js grid stagger 驱动:
// 满屏极淡规则小点,波纹从中心一圈圈扩散点亮品牌绿(致敬 anime.js 官方文档站的点阵语言)
// 语义:巡检设备点位矩阵 / 传感器阵列。克制、单色、留白。
const GAP = 40; // 点间距(越大越稀疏、越克制)

function DotFieldInner({ active, busy }: { active: boolean; busy: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  // 初始就用视口估算,避免首次测量拿到 0 尺寸导致不渲染
  const [grid, setGrid] = useState(() => ({
    cols: Math.max(Math.round((window.innerWidth - 64) / GAP), 6),
    rows: Math.max(Math.round((window.innerHeight - 120) / GAP), 4),
  }));
  const pulse = useRef<{ speed: number } | null>(null);
  const speedProxy = useRef({ s: 1 });
  const speedTween = useRef<{ pause: () => void } | null>(null);

  // 测量容器,按间距算网格行列(响应式)
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const { width, height } = el.getBoundingClientRect();
      if (!width || !height) return;
      setGrid({ cols: Math.max(Math.round(width / GAP), 6), rows: Math.max(Math.round(height / GAP), 4) });
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const total = grid.cols * grid.rows;

  // 波纹动画:从中心向外错峰点亮再回落,轮次之间留停顿(呼吸感,不刺眼)
  useEffect(() => {
    if (!total || !ref.current) return;
    const scope = createScope({
      root: ref.current,
      mediaQueries: { reduceMotion: "(prefers-reduced-motion)" },
    }).add((self) => {
      if (self?.matches.reduceMotion) return;
      const a = animate(".dotfield-dot", {
        opacity: [{ to: 0.42 }, { to: 0.05 }],
        scale: [{ to: 1.9 }, { to: 1 }],
        delay: stagger(70, { grid: [grid.cols, grid.rows], from: "center" }),
        loop: true,
        loopDelay: 850,
        ease: "inOutSine",
        duration: 900,
      });
      pulse.current = a;
    });
    return () => {
      pulse.current = null;
      scope.revert();
    };
  }, [total, grid.cols, grid.rows]);

  // busy 时波纹提速(AI 在思考)
  useEffect(() => {
    speedTween.current?.pause();
    speedTween.current = animate(speedProxy.current, {
      s: busy ? 2.4 : 1,
      duration: 600,
      ease: "outQuad",
      onUpdate: () => {
        if (pulse.current) pulse.current.speed = speedProxy.current.s;
      },
    });
    return () => {
      speedTween.current?.pause();
    };
  }, [busy]);

  return (
    <div
      ref={ref}
      className={`dotfield${active ? " dim" : ""}`}
      aria-hidden
      style={{
        gridTemplateColumns: grid.cols ? `repeat(${grid.cols}, 1fr)` : undefined,
        gridTemplateRows: grid.rows ? `repeat(${grid.rows}, 1fr)` : undefined,
      }}
    >
      {Array.from({ length: total }, (_, i) => (
        <span key={i} className="dotfield-dot" />
      ))}
    </div>
  );
}

const DotField = memo(DotFieldInner);
export default DotField;
