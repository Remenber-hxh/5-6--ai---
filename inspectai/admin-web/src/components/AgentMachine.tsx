import anime from "animejs";
import { memo, useEffect, useRef } from "react";

// 巡检机芯 — 致敬 animejs.com 的"精密仪器"语言,换成智巡的语义:
// 外圈四段状态弧(绿=正常/蓝=进行/橙=待办/红=异常)+ 刻度环反向缓转
// 内芯横向扫描线 + 数据点链沿波形轨道行进 + 雷达扫掠 + 呼吸核
// 纪律:只 transform/opacity;持续动画走 CSS;点链单 rAF 可平滑变速;busy 时提速发光
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

function AgentMachineInner({ busy, dim }: { busy: boolean; dim: boolean }) {
  const svgRef = useRef<SVGSVGElement>(null);
  const speedRef = useRef({ cur: 1, target: 1 });

  speedRef.current.target = busy ? 3 : 1;

  // 数据点链:单 rAF 沿波形轨道行进,速度朝目标值缓动(busy 平滑提速)
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    const path = svg.querySelector<SVGPathElement>(".agm-wave");
    const dots = Array.from(svg.querySelectorAll<SVGCircleElement>(".agm-dot"));
    if (!path || dots.length === 0) return;
    const len = path.getTotalLength();
    const reduced = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    if (reduced) {
      dots.forEach((d, i) => {
        const p = path.getPointAtLength((i / dots.length) * len);
        d.setAttribute("cx", String(p.x));
        d.setAttribute("cy", String(p.y));
      });
      return;
    }
    let raf = 0;
    let t = 0;
    let last = performance.now();
    const tick = (now: number) => {
      raf = requestAnimationFrame(tick);
      const dt = Math.min(now - last, 64) / 1000;
      last = now;
      if (document.hidden) return;
      const s = speedRef.current;
      s.cur += (s.target - s.cur) * Math.min(dt * 3, 1);
      t = (t + dt * 0.11 * s.cur) % 1;
      for (let i = 0; i < dots.length; i++) {
        const frac = (t + i / dots.length) % 1;
        const p = path.getPointAtLength(frac * len);
        dots[i].setAttribute("cx", String(p.x));
        dots[i].setAttribute("cy", String(p.y));
        // 链头亮、链尾淡(i=0 是头)
        dots[i].style.opacity = String(0.35 + 0.65 * (1 - i / dots.length));
      }
    };
    raf = requestAnimationFrame(tick);
    return () => cancelAnimationFrame(raf);
  }, []);

  // 机芯启动:状态弧逐段画入 + 刻度环/内芯淡入(anime.js 画线,一次性)
  useEffect(() => {
    const svg = svgRef.current;
    if (!svg) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) return;
    const arcs = svg.querySelectorAll(".agm-arc");
    anime.set(arcs, { strokeDashoffset: 100 });
    const tl = anime.timeline({ easing: "easeOutCubic" });
    tl.add({ targets: arcs, strokeDashoffset: [100, 0], duration: 900, delay: anime.stagger(130) });
    tl.add(
      { targets: svg.querySelectorAll(".agm-fade"), opacity: [0, 1], duration: 700, delay: anime.stagger(90) },
      "-=650",
    );
    return () => tl.pause();
  }, []);

  return (
    <div className={`agent-machine${busy ? " busy" : ""}${dim ? " dim" : ""}`} aria-hidden>
      <svg ref={svgRef} viewBox="0 0 600 600" width="100%" height="100%">
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

        {/* 外圈状态弧(缓转 80s) */}
        <g className="agm-rotate-slow" style={{ filter: "drop-shadow(0 0 6px rgba(62,230,180,0.25))" }}>
          {ARCS.map((a) => (
            <path
              key={a.color}
              className="agm-arc"
              d={arcPath(272, a.from, a.to)}
              stroke={a.color}
              strokeWidth={5}
              strokeLinecap="round"
              fill="none"
              pathLength={100}
              strokeDasharray={100}
            />
          ))}
        </g>

        {/* 刻度环(反向 110s) */}
        <g className="agm-rotate-rev agm-fade">
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

        {/* 内芯:横向扫描线(整组缓慢升降)+ 波形轨道 + 数据点链 + 雷达扫掠 */}
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
          <g className="agm-sweep agm-fade">
            <path d={`M ${CX} ${CY} L ${CX + CORE_R} ${CY - 58} A ${CORE_R} ${CORE_R} 0 0 1 ${CX + CORE_R} ${CY + 58} Z`} fill="url(#agm-sweep-grad)" />
          </g>
          <path className="agm-wave agm-fade" d={WAVE} fill="none" stroke="rgba(62,230,180,0.16)" strokeWidth={1.5} />
          <g className="agm-dots agm-fade">
            {Array.from({ length: DOTS }, (_, i) => (
              <circle key={i} className="agm-dot" r={i === 0 ? 4 : 3} fill="#3ee6b4" cx={CX} cy={CY} />
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
