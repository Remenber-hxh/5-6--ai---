import { animate, createScope, createTimeline, stagger, svg, utils } from "animejs";
import { memo, useEffect, useRef } from "react";

// 巡检机芯 — 致敬 animejs.com 的"精密仪器"语言,换成智巡语义:
// 外圈四段状态弧(绿=正常/蓝=进行/橙=待办/红=异常)+ 刻度环反向缓转
// 内芯扫描线 + 数据点链沿波形轨道行进 + 雷达扫掠 + 呼吸核
// 全部动画由 anime.js v4 驱动:createScope 管生命周期与 reduced-motion,
// createDrawable 画线入场,createMotionPath(offset) 做点链相位,.speed 实时变速
const CX = 300;
const CY = 300;

function arcPath(r: number, startDeg: number, endDeg: number): string {
  const a0 = ((startDeg - 90) * Math.PI) / 180;
  const a1 = ((endDeg - 90) * Math.PI) / 180;
  const x0 = CX + r * Math.cos(a0);
  const y0 = CY + r * Math.sin(a0);
  const x1 = CX + r * Math.cos(a1);
  const y1 = CY + r * Math.sin(a1);
  const large = endDeg - startDeg > 180 ? 1 : 0;
  return `M ${x0.toFixed(2)} ${y0.toFixed(2)} A ${r} ${r} 0 ${large} 1 ${x1.toFixed(2)} ${y1.toFixed(2)}`;
}

// 四段状态弧:与全站状态语义同色(正常/进行中/待执行/异常)
const ARCS = [
  { from: 8, to: 76, color: "#3ee6b4" },
  { from: 98, to: 166, color: "#246bfe" },
  { from: 188, to: 256, color: "#f5a524" },
  { from: 278, to: 346, color: "#ef4b3f" },
];

const CORE_R = 168;
const DOTS = 12;
const WAVE = "M 150 300 Q 212 224, 300 300 T 450 300";
const ORIGIN = { transformOrigin: "300px 300px" } as const;

function AgentMachineInner({ busy, dim }: { busy: boolean; dim: boolean }) {
  const rootRef = useRef<HTMLDivElement>(null);
  // 可变速的动画实例(busy 时整机提速)
  const spinning = useRef<{ speed: number }[]>([]);
  const speedProxy = useRef({ s: 1 });
  const speedTween = useRef<{ pause: () => void } | null>(null);

  useEffect(() => {
    if (!rootRef.current) return;
    const scope = createScope({
      root: rootRef.current,
      mediaQueries: { reduceMotion: "(prefers-reduced-motion)" },
    }).add((self) => {
      const reduce = Boolean(self?.matches.reduceMotion);
      const list: { speed: number }[] = [];

      // 入场:状态弧逐段画线 + 其余元素错峰淡入
      if (reduce) {
        utils.set([".agm-fade"], { opacity: 1 });
      } else {
        createTimeline({ defaults: { ease: "outCubic" } })
          .add(svg.createDrawable(".agm-arc"), { draw: ["0 0", "0 1"], duration: 900, delay: stagger(130) })
          .add(".agm-fade", { opacity: [0, 1], duration: 700, delay: stagger(90) }, "-=650");
      }

      // 持续运转(reduced-motion 下 duration 0 = 静止定格)
      const spin = (sel: string, duration: number, dir: 1 | -1) =>
        list.push(
          animate(sel, {
            rotate: 360 * dir,
            duration: reduce ? 0 : duration,
            ease: "linear",
            loop: !reduce,
          }),
        );
      spin(".agm-rotate-slow", 80000, 1);
      spin(".agm-rotate-rev", 110000, -1);
      spin(".agm-sweep", 5200, 1);

      list.push(
        animate(".agm-scan", {
          translateY: [-5, 5],
          duration: reduce ? 0 : 7000,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
        }),
        animate(".agm-pulse", {
          r: [5, 7.5],
          opacity: [0.9, 0.45],
          duration: reduce ? 0 : 1500,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
        }),
      );

      // 数据点链:createMotionPath 的 offset 参数给出相位,天然成链
      const wave = rootRef.current!.querySelector<SVGPathElement>(".agm-wave");
      if (wave) {
        const dots = rootRef.current!.querySelectorAll<SVGCircleElement>(".agm-dot");
        dots.forEach((dot, i) => {
          utils.set(dot, { opacity: 0.35 + 0.65 * (1 - i / DOTS) });
          list.push(
            animate(dot, {
              ...svg.createMotionPath(wave, i / DOTS),
              duration: reduce ? 0 : 9000,
              ease: "linear",
              loop: !reduce,
            }),
          );
        });
      }
      spinning.current = list;
    });
    return () => {
      spinning.current = [];
      scope.revert();
    };
  }, []);

  // busy → 整机平滑提速 3x(实时改 .speed,anime v4 playbackRate)
  useEffect(() => {
    speedTween.current?.pause();
    speedTween.current = animate(speedProxy.current, {
      s: busy ? 3 : 1,
      duration: 700,
      ease: "outQuad",
      onUpdate: () => {
        const s = speedProxy.current.s;
        for (const a of spinning.current) a.speed = s;
      },
    });
    return () => {
      speedTween.current?.pause();
    };
  }, [busy]);

  return (
    <div ref={rootRef} className={`agent-machine${busy ? " busy" : ""}${dim ? " dim" : ""}`} aria-hidden>
      <svg viewBox="0 0 600 600" width="100%" height="100%">
        <defs>
          <clipPath id="agm-core-clip">
            <circle cx={CX} cy={CY} r={CORE_R} />
          </clipPath>
          <radialGradient id="agm-center-fade" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#0b1626" stopOpacity="0.92" />
            <stop offset="55%" stopColor="#0b1626" stopOpacity="0.55" />
            <stop offset="100%" stopColor="#0b1626" stopOpacity="0" />
          </radialGradient>
          <linearGradient id="agm-sweep-grad" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="#3ee6b4" stopOpacity="0.28" />
            <stop offset="100%" stopColor="#3ee6b4" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* 外圈状态弧(缓转) */}
        <g className="agm-rotate-slow" style={{ ...ORIGIN, filter: "drop-shadow(0 0 6px rgba(62,230,180,0.25))" }}>
          {ARCS.map((a) => (
            <path
              key={a.color}
              className="agm-arc"
              d={arcPath(272, a.from, a.to)}
              stroke={a.color}
              strokeWidth={5}
              strokeLinecap="round"
              fill="none"
            />
          ))}
        </g>

        {/* 刻度环(反向) */}
        <g className="agm-rotate-rev agm-fade" style={ORIGIN}>
          <circle
            cx={CX}
            cy={CY}
            r={246}
            fill="none"
            stroke="rgba(159,220,204,0.30)"
            strokeWidth={11}
            strokeDasharray="1.5 11.36"
          />
        </g>

        {/* 静环两道 */}
        <circle className="agm-fade" cx={CX} cy={CY} r={212} fill="none" stroke="rgba(159,220,204,0.14)" strokeWidth={1} />
        <circle className="agm-fade" cx={CX} cy={CY} r={CORE_R + 8} fill="none" stroke="rgba(62,230,180,0.22)" strokeWidth={1.5} />

        {/* 内芯:扫描线(升降)+ 波形轨道 + 数据点链 + 雷达扫掠 */}
        <g clipPath="url(#agm-core-clip)">
          <g className="agm-scan agm-fade">
            {Array.from({ length: 40 }, (_, i) => {
              const y = CY - CORE_R - 10 + i * 9;
              const half = Math.sqrt(Math.max(CORE_R * CORE_R - (y - CY) * (y - CY), 100));
              return (
                <line
                  key={i}
                  x1={CX - half}
                  x2={CX + half}
                  y1={y}
                  y2={y}
                  stroke="rgba(62,230,180,0.11)"
                  strokeWidth={2.4}
                />
              );
            })}
          </g>
          <g className="agm-sweep agm-fade" style={ORIGIN}>
            <path d={`M ${CX} ${CY} L ${CX + CORE_R} ${CY - 58} A ${CORE_R} ${CORE_R} 0 0 1 ${CX + CORE_R} ${CY + 58} Z`} fill="url(#agm-sweep-grad)" />
          </g>
          <path className="agm-wave agm-fade" d={WAVE} fill="none" stroke="rgba(62,230,180,0.16)" strokeWidth={1.5} />
          <g className="agm-fade">
            {Array.from({ length: DOTS }, (_, i) => (
              <circle key={i} className="agm-dot" r={i === 0 ? 4 : 3} fill="#3ee6b4" cx={0} cy={0} />
            ))}
          </g>
        </g>

        {/* 中心减噪罩(保 hero 文字可读)+ 呼吸核 */}
        <circle className="agm-fade" cx={CX} cy={CY} r={CORE_R} fill="url(#agm-center-fade)" />
        <circle className="agm-pulse agm-fade" cx={CX} cy={CY} r={5} fill="#3ee6b4" />
      </svg>
    </div>
  );
}

const AgentMachine = memo(AgentMachineInner);
export default AgentMachine;
