import { useEffect, useState } from "react";

// ===== 识别中:整屏场景(复刻旧版 sceneLoading)=====
// 旧版不是在表单里挂一条常驻的"识别中"提示,而是一个专属整屏:
// 顶部进度条 + 轨道动画 + 分步文案 + 能力标签。识别完成才切到表单。

/** 分步文案与时间点,取自旧版 LOADING_SCRIPTS */
const SCRIPTS = {
  classify: [
    { t: 0, msg: "AI 正在识别场景…", step: -1 },
    { t: 4, msg: "正在匹配日报模板…", step: -1 },
  ],
  analyze: [
    { t: 0, msg: "正在逐张查看照片…", step: 0 },
    { t: 7, msg: "正在核对模板判定规则…", step: 1 },
    { t: 16, msg: "正在生成巡检结论…", step: 2 },
    { t: 30, msg: "即将完成,正在整理字段…", step: 2 },
  ],
};

const STEPS = {
  classify: ["识别巡检场景", "提取设备状态 / 表计读数", "匹配日报模板"],
  analyze: ["逐张查看照片", "核对模板判定规则", "生成巡检结论"],
};

const TAGS = ["设备状态", "仪表读数", "异常风险", "日报模板"];

export default function LoadingScene({
  kind,
}: {
  kind: "classify" | "analyze";
}) {
  const [elapsed, setElapsed] = useState(0);

  useEffect(() => {
    const started = Date.now();
    const timer = setInterval(
      () => setElapsed((Date.now() - started) / 1000),
      500,
    );
    return () => clearInterval(timer);
  }, [kind]);

  const script = SCRIPTS[kind];
  let cur = script[0];
  for (const it of script) if (elapsed >= it.t) cur = it;

  // 进度条按时间推进但不到头,留给真实完成事件收尾(旧版同样做法)
  const pct = Math.min(92, 8 + elapsed * 3);
  const steps = STEPS[kind];

  return (
    <div className="loading-screen">
      <div className="progress">
        <div className="progress-bar" style={{ width: `${pct}%` }} />
      </div>

      <div className="loading-body">
        <div className="loading-box">
          <div className="loading-orbit" aria-hidden="true">
            <span className="orbit-ring" />
            <span className="orbit-ring orbit-ring-2" />
            <span className="orbit-core" />
          </div>

          <p className="loading-msg">{cur.msg}</p>

          <ol className="loading-steps">
            {steps.map((t, i) => {
              const state =
                cur.step < 0
                  ? i === 0
                    ? "active"
                    : ""
                  : i < cur.step
                    ? "done"
                    : i === cur.step
                      ? "active"
                      : "";
              return (
                <li className={`step ${state}`} key={t}>
                  <span className="step-dot" />
                  <span className="step-text">{t}</span>
                </li>
              );
            })}
          </ol>

          <div className="loading-tags" aria-hidden="true">
            {TAGS.map((t) => (
              <span key={t}>{t}</span>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
