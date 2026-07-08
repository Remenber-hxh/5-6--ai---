import { animate, createScope, createTimeline, stagger, svg, utils } from "animejs";
import { memo, useEffect, useRef } from "react";

// 巡检机芯 v2 — animejs.com 精密仪器语言的智巡版:
// 质感层(机身渐变盘/凹槽环/高光扇区/双层细刻度)+ 四段状态弧 dual-stroke bloom
// 内芯:菱形剪影扫描线 + 三条数据点链(不同相位/速度)+ 雷达扫掠
// 主角是中心的「Agent 内核」反应堆:辉光核 + 呼吸 + 虚线环 + 电子轨道
// 全部由 anime.js v4 驱动:createScope / createDrawable / createMotionPath(offset) / .speed
const CX = 300;
const CY = 300;
const CORE_R = 168;

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

// 三条数据点链:主波 / 上斜波 / 下缓波(全部落在内芯圆内)
const CHAINS = [
  { id: "a", d: "M 150 300 Q 212 224, 300 300 T 450 300", dots: 10, dur: 9000, r: 3 },
  { id: "b", d: "M 190 232 Q 260 194, 330 226 T 428 212", dots: 7, dur: 6500, r: 2.4 },
  { id: "c", d: "M 210 374 Q 268 398, 322 378 T 414 368", dots: 5, dur: 12000, r: 2 },
];

// 菱形剪影扫描线(宽度阶梯量化,致敬原版的像素菱形芯)
function scanLines() {
  const out: { y: number; half: number; op: number }[] = [];
  for (let i = 0; i < 48; i++) {
    const y = CY - CORE_R + 4 + i * 7;
    const dy = Math.abs(y - CY);
    const circHalf = Math.sqrt(Math.max(CORE_R * CORE_R - dy * dy, 0));
    const diamond = (1 - dy / CORE_R) * CORE_R * 1.06;
    const stepped = Math.max(Math.ceil(diamond / 21) * 21 - 5, 0);
    const half = Math.min(circHalf - 4, stepped);
    if (half < 6) continue;
    out.push({ y, half, op: dy < 45 ? 0.34 : dy < 100 ? 0.24 : 0.15 });
  }
  return out;
}
const SCAN_LINES = scanLines();
const ORIGIN = { transformOrigin: "300px 300px" } as const;

function AgentMachineInner({ busy, dim }: { busy: boolean; dim: boolean }) {
  const rootRef = useRef<HTMLDivElement>(null);
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

      // 入场:状态弧画线 → 机身/刻度错峰淡入 → 内核弹出
      if (reduce) {
        utils.set(".agm-fade", { opacity: 1 });
        utils.set(".agm-reactor", { scale: 1, opacity: 1 });
      } else {
        utils.set(".agm-reactor", { scale: 0, opacity: 0 });
        createTimeline({ defaults: { ease: "outCubic" } })
          .add(svg.createDrawable(".agm-arc"), { draw: ["0 0", "0 1"], duration: 900, delay: stagger(130) })
          .add(".agm-fade", { opacity: [0, 1], duration: 700, delay: stagger(55) }, "-=650")
          .add(".agm-reactor", { scale: [0, 1], opacity: [0, 1], duration: 900, ease: "outElastic(1, .7)" }, "-=350");
      }

      const spin = (sel: string, duration: number, dir: 1 | -1) =>
        list.push(
          animate(sel, { rotate: 360 * dir, duration: reduce ? 0 : duration, ease: "linear", loop: !reduce }),
        );
      spin(".agm-rotate-slow", 80000, 1); // 状态弧 + 细白弧
      spin(".agm-ticks-fine", 90000, 1); // 细刻度
      spin(".agm-ticks-bold", 120000, -1); // 粗刻度反向
      spin(".agm-wedges", 65000, -1); // 高光扇区
      spin(".agm-sweep", 5200, 1); // 雷达扫掠
      spin(".agm-core-ring", 16000, 1); // 内核虚线环
      spin(".agm-orbit", 3800, -1); // 内核电子轨道

      list.push(
        // 内芯示波器:每根横线 scaleX 伸缩,从中心向两端错峰 → 波形实时 morph(致敬原版)
        animate(".agm-scanline", {
          scaleX: [{ to: 0.42 }, { to: 1.2 }, { to: 0.78 }],
          duration: reduce ? 0 : 2800,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
          delay: reduce ? 0 : stagger(36, { from: "center" }),
        }),
        animate(".agm-scan", {
          translateY: [-5, 5],
          duration: reduce ? 0 : 7000,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
        }),
        animate(".agm-pulse", {
          r: [9, 12],
          opacity: [1, 0.55],
          duration: reduce ? 0 : 1600,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
        }),
        animate(".agm-core-halo", {
          opacity: [0.5, 0.9],
          duration: reduce ? 0 : 1600,
          ease: "inOutSine",
          alternate: true,
          loop: !reduce,
        }),
      );

      // 数据点链:createMotionPath 的 offset 做相位,三链不同速度
      for (const c of CHAINS) {
        const path = rootRef.current!.querySelector<SVGPathElement>(`.agm-wave-${c.id}`);
        if (!path) continue;
        const dots = rootRef.current!.querySelectorAll<SVGCircleElement>(`.agm-dot-${c.id}`);
        dots.forEach((dot, i) => {
          utils.set(dot, { opacity: 0.3 + 0.7 * (1 - i / dots.length) });
          list.push(
            animate(dot, {
              ...svg.createMotionPath(path, i / dots.length),
              duration: reduce ? 0 : c.dur,
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

  // busy → 整机平滑提速 3x(实时改 .speed / playbackRate)
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
          {/* 机身:左上受光的金属渐变盘 */}
          <radialGradient id="agm-body" cx="38%" cy="30%" r="75%">
            <stop offset="0%" stopColor="#152741" />
            <stop offset="55%" stopColor="#0c1a2e" />
            <stop offset="100%" stopColor="#081221" />
          </radialGradient>
          <radialGradient id="agm-core-glow" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#3ee6b4" stopOpacity="0.10" />
            <stop offset="70%" stopColor="#3ee6b4" stopOpacity="0.03" />
            <stop offset="100%" stopColor="#3ee6b4" stopOpacity="0" />
          </radialGradient>
          <radialGradient id="agm-halo" cx="50%" cy="50%" r="50%">
            <stop offset="0%" stopColor="#3ee6b4" stopOpacity="0.55" />
            <stop offset="45%" stopColor="#3ee6b4" stopOpacity="0.18" />
            <stop offset="100%" stopColor="#3ee6b4" stopOpacity="0" />
          </radialGradient>
          <linearGradient id="agm-sweep-grad" x1="0" y1="0" x2="1" y2="0">
            <stop offset="0%" stopColor="#3ee6b4" stopOpacity="0.30" />
            <stop offset="100%" stopColor="#3ee6b4" stopOpacity="0" />
          </linearGradient>
        </defs>

        {/* ===== 机身质感层 ===== */}
        <circle className="agm-fade" cx={CX} cy={CY} r={284} fill="none" stroke="rgba(159,220,204,0.08)" strokeWidth={1} />
        <circle className="agm-fade" cx={CX} cy={CY} r={204} fill="url(#agm-body)" />
        <circle className="agm-fade" cx={CX} cy={CY} r={204} fill="none" stroke="#223850" strokeWidth={1.5} />
        <circle className="agm-fade" cx={CX} cy={CY} r={195} fill="none" stroke="rgba(0,0,0,0.5)" strokeWidth={7} />
        <circle className="agm-fade" cx={CX} cy={CY} r={187} fill="none" stroke="rgba(130,170,200,0.09)" strokeWidth={1} />

        {/* 高光扇区(缓转,景深来源) */}
        <g className="agm-wedges agm-fade" style={ORIGIN}>
          <path d={arcPath(196, 300, 344)} stroke="rgba(190,220,240,0.10)" strokeWidth={14} strokeLinecap="round" fill="none" />
          <path d={arcPath(196, 118, 140)} stroke="rgba(190,220,240,0.05)" strokeWidth={14} strokeLinecap="round" fill="none" />
        </g>

        {/* ===== 双层细密刻度 ===== */}
        <g className="agm-ticks-fine agm-fade" style={ORIGIN}>
          <circle cx={CX} cy={CY} r={250} fill="none" stroke="rgba(159,220,204,0.22)" strokeWidth={8} strokeDasharray="1.1 5.445" />
        </g>
        <g className="agm-ticks-bold agm-fade" style={ORIGIN}>
          <circle cx={CX} cy={CY} r={232} fill="none" stroke="rgba(159,220,204,0.13)" strokeWidth={13} strokeDasharray="1.6 13.584" />
        </g>

        {/* ===== 状态弧(dual-stroke bloom)+ 细白弧组 ===== */}
        <g className="agm-rotate-slow" style={{ ...ORIGIN, filter: "drop-shadow(0 0 7px rgba(62,230,180,0.3))" }}>
          {ARCS.map((a) => (
            <path
              key={`bloom-${a.color}`}
              className="agm-fade"
              d={arcPath(272, a.from, a.to)}
              stroke={a.color}
              strokeWidth={11}
              strokeOpacity={0.22}
              strokeLinecap="round"
              fill="none"
            />
          ))}
          {ARCS.map((a) => (
            <path
              key={a.color}
              className="agm-arc"
              d={arcPath(272, a.from, a.to)}
              stroke={a.color}
              strokeWidth={4.5}
              strokeLinecap="round"
              fill="none"
            />
          ))}
          <path className="agm-fade" d={arcPath(262, 318, 356)} stroke="rgba(255,255,255,0.30)" strokeWidth={1.4} fill="none" />
          <path className="agm-fade" d={arcPath(257, 322, 352)} stroke="rgba(255,255,255,0.16)" strokeWidth={1.2} fill="none" />
          <path className="agm-fade" d={arcPath(252, 326, 348)} stroke="rgba(255,255,255,0.10)" strokeWidth={1} fill="none" />
        </g>

        {/* ===== 内芯:菱形扫描线 + 三条点链 + 扫掠 ===== */}
        <g clipPath="url(#agm-core-clip)">
          <circle className="agm-fade" cx={CX} cy={CY} r={CORE_R} fill="url(#agm-core-glow)" />
          <g className="agm-scan agm-fade">
            {SCAN_LINES.map((l) => (
              <rect
                key={l.y}
                className="agm-scanline"
                x={CX - l.half}
                y={l.y - 1.4}
                width={l.half * 2}
                height={2.8}
                rx={1.4}
                fill={`rgba(62,230,180,${l.op})`}
              />
            ))}
          </g>
          <g className="agm-sweep agm-fade" style={ORIGIN}>
            <path d={`M ${CX} ${CY} L ${CX + CORE_R} ${CY - 58} A ${CORE_R} ${CORE_R} 0 0 1 ${CX + CORE_R} ${CY + 58} Z`} fill="url(#agm-sweep-grad)" />
          </g>
          {CHAINS.map((c) => (
            <g key={c.id} className="agm-fade">
              <path className={`agm-wave-${c.id}`} d={c.d} fill="none" stroke="rgba(62,230,180,0.10)" strokeWidth={1.2} />
              {Array.from({ length: c.dots }, (_, i) => (
                <circle key={i} className={`agm-dot-${c.id}`} r={i === 0 ? c.r + 1 : c.r} fill="#3ee6b4" cx={0} cy={0} />
              ))}
            </g>
          ))}
        </g>
        <circle className="agm-fade" cx={CX} cy={CY} r={CORE_R + 7} fill="none" stroke="rgba(62,230,180,0.25)" strokeWidth={1.5} />

        {/* ===== Agent 内核反应堆(主角) ===== */}
        <g className="agm-reactor" style={ORIGIN}>
          <circle className="agm-core-halo" cx={CX} cy={CY} r={52} fill="url(#agm-halo)" />
          <circle cx={CX} cy={CY} r={34} fill="none" stroke="rgba(62,230,180,0.28)" strokeWidth={1} />
          <g className="agm-core-ring" style={ORIGIN}>
            <circle cx={CX} cy={CY} r={23} fill="none" stroke="rgba(126,255,210,0.65)" strokeWidth={1.6} strokeDasharray="7 5" />
          </g>
          <g className="agm-orbit" style={ORIGIN}>
            <circle cx={CX + 41} cy={CY} r={2.6} fill="#b6ffe6" />
            <circle cx={CX - 41} cy={CY} r={1.7} fill="#7effd2" opacity={0.6} />
          </g>
          <circle className="agm-pulse" cx={CX} cy={CY} r={9} fill="#3ee6b4" style={{ filter: "drop-shadow(0 0 8px rgba(62,230,180,0.9))" }} />
          {[0, 90, 180, 270].map((deg) => {
            const rad = ((deg - 90) * Math.PI) / 180;
            return (
              <line
                key={deg}
                x1={CX + 44 * Math.cos(rad)}
                y1={CY + 44 * Math.sin(rad)}
                x2={CX + 50 * Math.cos(rad)}
                y2={CY + 50 * Math.sin(rad)}
                stroke="rgba(126,255,210,0.5)"
                strokeWidth={1.6}
              />
            );
          })}
        </g>
      </svg>
    </div>
  );
}

const AgentMachine = memo(AgentMachineInner);
export default AgentMachine;
