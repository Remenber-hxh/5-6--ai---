import anime from "animejs";
import { useEffect, useRef, useState } from "react";

// Anime.js 数字滚动:值变化时从上一个值滚到新值
export default function CountUp({ value }: { value: number }) {
  const [display, setDisplay] = useState(0);
  const prev = useRef(0);

  useEffect(() => {
    const obj = { v: prev.current };
    const a = anime({
      targets: obj,
      v: value,
      round: 1,
      duration: 900,
      easing: "easeOutCubic",
      update: () => setDisplay(obj.v),
    });
    prev.current = value;
    return () => a.pause();
  }, [value]);

  return <>{display}</>;
}
