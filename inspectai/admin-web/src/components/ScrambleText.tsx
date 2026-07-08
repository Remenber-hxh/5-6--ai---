import { createTimer } from "animejs";
import { useEffect, useRef } from "react";

// 次世代解码效果:挂载时用 anime.js createTimer 的 progress 驱动"乱码 → 真字"逐字揭示。
// 用 anime 的计时引擎(非自制 rAF),确定性、可读性优先(真实文字始终作为 children)。
const GLYPHS = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789#%&/<>*";

export default function ScrambleText({
  text,
  className,
  duration = 1100,
  delay = 0,
}: {
  text: string;
  className?: string;
  duration?: number;
  delay?: number;
}) {
  const ref = useRef<HTMLSpanElement>(null);

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (window.matchMedia("(prefers-reduced-motion: reduce)").matches) {
      el.textContent = text;
      return;
    }
    const keep = (c: string) => c === " " || c === "·" || c === "·";
    const timer = createTimer({
      duration,
      delay,
      onUpdate: (self) => {
        const revealed = Math.floor(self.progress * text.length);
        let out = "";
        for (let i = 0; i < text.length; i++) {
          const ch = text[i];
          out += i < revealed || keep(ch) ? ch : GLYPHS[(Math.random() * GLYPHS.length) | 0];
        }
        el.textContent = out;
      },
      onComplete: () => {
        el.textContent = text;
      },
    });
    return () => {
      timer.pause();
      el.textContent = text;
    };
  }, [text, duration, delay]);

  return (
    <span ref={ref} className={className}>
      {text}
    </span>
  );
}
