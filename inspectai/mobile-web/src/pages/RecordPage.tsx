import { Button, Toast } from "@/ui";
import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import LoadingScene from "@/components/LoadingScene";
import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import {
  FieldValue,
  RecordDTO,
  enableManual,
  getRecord,
  patchField,
  startAnalysis,
} from "@/api/inspection";

const POLL_MS = 2000;
const POLL_MAX = 40; // 最长约 80 秒

/** AI 状态药丸,口径与旧版 pillFor / pillTextFor 一致 */
function pillOf(f: FieldValue): { cls: string; text: string } | null {
  if (f.source === "human-confirmed") return { cls: "edited", text: "已确认" };
  if (f.source === "human-edited") return { cls: "edited", text: "已修改" };
  if (f.source === "ai" && f.confidence) {
    return { cls: f.needsReview ? "review" : "confirmed", text: `AI ${Math.round(f.confidence * 100)}%` };
  }
  if (f.source === "ai" && String(f.value || "").trim()) {
    return { cls: f.needsReview ? "review" : "confirmed", text: "AI 识别" };
  }
  return null;
}

/** 长文本字段:说明 / 备注 / 记录 —— 旧版用整块 textarea */
function isLongText(f: FieldValue): boolean {
  return f.kind === "text" && /说明|备注|记录/.test(f.label);
}

// 字段行:旧版语义 —— 标签左、药丸+控件右,改动即存,不放确认按钮。
// 逐字段挂"确认"按钮会让页面充斥重复动作;旧版靠自动保存 + 一键确认解决。
function FieldRow({
  field,
  recordId,
  onSaved,
}: {
  field: FieldValue;
  recordId: string;
  onSaved: (updated: FieldValue) => void;
}) {
  const [value, setValue] = useState(field.value);
  // 停留时长:后端据此写字段确认留痕,用来识别"秒确认"的惰性操作
  const enteredAt = useRef(Date.now());

  useEffect(() => {
    setValue(field.value);
  }, [field.value]);

  async function commit(next: string) {
    if (next === field.value) return;
    try {
      const updated = await patchField(recordId, field.code, next, field.version, {
        action: "correct",
        durationMs: Date.now() - enteredAt.current,
      });
      onSaved(updated);
      enteredAt.current = Date.now();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    }
  }

  const pill = pillOf(field);
  const pillEl = pill ? <span className={`ai-pill ${pill.cls}`}>{pill.text}</span> : null;

  if (isLongText(field)) {
    return (
      <div className="fld fld-block">
        <div className="fld-label">
          {field.label}
          {field.required && <em className="fld-req">*</em>}
          {pillEl}
        </div>
        <textarea
          className="fld-textarea"
          value={value}
          placeholder="可选填写"
          onChange={(e) => setValue(e.target.value)}
          onBlur={() => void commit(value)}
        />
      </div>
    );
  }

  return (
    <div className={field.needsReview ? "fld fld-warn" : "fld"}>
      <div className="fld-label">
        {field.label}
        {field.required && <em className="fld-req">*</em>}
      </div>
      <div className="fld-value">
        {pillEl}
        {field.options?.length ? (
          <select
            className="fld-select"
            value={value}
            onChange={(e) => {
              setValue(e.target.value);
              void commit(e.target.value); // 选择类改完即存,不等失焦
            }}
          >
            <option value="">请选择</option>
            {field.options.map((o) => (
              <option key={o} value={o}>
                {o}
              </option>
            ))}
          </select>
        ) : (
          <input
            className="fld-input"
            type={field.kind === "number" ? "number" : "text"}
            inputMode={field.kind === "number" ? "decimal" : undefined}
            value={value}
            placeholder={field.kind === "number" ? "请输入数值" : "请输入"}
            onChange={(e) => setValue(e.target.value)}
            onBlur={() => void commit(value)}
          />
        )}
      </div>
    </div>
  );
}

export default function RecordPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [rec, setRec] = useState<RecordDTO | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [viewing, setViewing] = useState<PhotoMeta | null>(null);
  const kickedRef = useRef("");

  // 依赖只能是 id:循环里会 setRec,若把 rec 放进依赖会自噬 ——
  // effect 反复重启、清理函数置位导致 setAnalyzing(false) 永不执行,转圈停不下来。
  useEffect(() => {
    let stop = false;
    void (async () => {
      let cur: RecordDTO;
      try {
        cur = await getRecord(id);
      } catch {
        Toast.show({ content: "记录加载失败" });
        return;
      }
      if (stop) return;
      setRec(cur);
      if (cur.recognitionStatus !== "not_started") return;

      setAnalyzing(true);
      try {
        if (kickedRef.current !== id) {
          kickedRef.current = id;
          await startAnalysis(id);
        }
        for (let i = 0; i < POLL_MAX && !stop; i++) {
          await new Promise((r) => setTimeout(r, POLL_MS));
          if (stop) return;
          const fresh = await getRecord(id);
          if (stop) return;
          setRec(fresh);
          if (fresh.recognitionStatus !== "processing" && fresh.recognitionStatus !== "not_started") break;
        }
      } catch {
        Toast.show({ content: "AI 识别未成功,可手动填写" });
      } finally {
        setAnalyzing(false);
      }
    })();
    return () => {
      stop = true;
    };
  }, [id]);

  // 后端 PATCH 字段只返回该字段,按 code 合并(与旧版 Object.assign 同语义)
  function mergeField(updated: FieldValue) {
    setRec((cur) =>
      cur
        ? { ...cur, fields: cur.fields.map((f) => (f.code === updated.code ? { ...f, ...updated } : f)) }
        : cur,
    );
  }

  // 只统计置信度 <95% 的 AI 字段;≥95% 视为可信,不需人工逐项确认(旧版口径)
  const lowConf = (rec?.fields || []).filter(
    (f) => f.source === "ai" && String(f.value || "").trim() !== "" && (f.confidence || 0) < 0.95,
  );

  async function confirmAll() {
    if (!rec || confirming) return;
    setConfirming(true);
    try {
      for (const f of lowConf) {
        const updated = await patchField(rec.id, f.code, f.value, f.version, { action: "confirm" });
        mergeField(updated);
      }
      Toast.show({ content: `已确认 ${lowConf.length} 项`, position: "bottom" });
    } catch {
      Toast.show({ content: "部分字段确认失败,请重试" });
    } finally {
      setConfirming(false);
    }
  }

  // 旧版流程:字段确认 →「保存并预览日报」→ 预览页看 AI 总结/建议 → 提交。
  // 提交前先让人看一眼总结,是巡检闭环的一环,不该跳过。
  function toPreview() {
    if (!rec) return;
    const missing = rec.fields.filter((f) => f.required && !f.value.trim());
    if (missing.length) {
      Toast.show({ content: `必填字段未完成:${missing[0].label}` });
      return;
    }
    nav(`/preview/${rec.id}`);
  }

  // 识别中 = 整屏专属场景(旧版做法),不在表单里挂常驻提示条
  if (analyzing) return <LoadingScene kind="analyze" />;

  if (!rec) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }


  return (
    <div className="flow-screen">
      {/* 标题用旧版的固定文案(TITLES.form)。模板名不进标题 —— 顶栏 17px
          放不下「电梯巡检(有机房)」这种长名,会被截断 */}
      <FlowHeader
        title="确认日报字段"
        onBack={() => nav("/")}
        action={{ text: "设备健康", onClick: () => nav("/ledger") }}
        step="record"
      />

      <div className="scroll-area flow-body">
        {/* 顶部那行「模板 · 项目 · 点位 · N/M 项已填」删了:
            四段信息挤成一行小字,读起来费劲又占地方。
            模板名在顶栏标题里已有语境;填写进度靠字段本身的填/未填状态
            和顶部进度条就看得出来。 */}
        {/* 识别不稳:给出路,不能只报错(旧版:重拍 / 转人工) */}
        {rec.retakeReason && (
          <div className="retake-box">
            <div className="retake-why">{rec.retakeReason}</div>
            <div className="retake-tries">已尝试 {rec.captureAttempts ?? 0} / 3 次</div>
            <div className="retake-acts">
              <button className="fld-btn" onClick={() => nav("/")}>
                去补拍
              </button>
              <button
                className="fld-btn"
                onClick={async () => {
                  try {
                    setRec(await enableManual(rec.id));
                    Toast.show({ content: "已转人工填写" });
                  } catch {
                    Toast.show({ content: "切换失败" });
                  }
                }}
              >
                转人工填写
              </button>
            </div>
          </div>
        )}

        {rec.images.length > 0 && (
          <>
            <div className="fld-group-title">巡检照片({rec.images.length} 张)</div>
            <div className="photo-strip">
              {rec.images.map((img) => {
                const url = `/storage/uploads/${rec.id}/${img.id}_${img.fileName}`;
                return (
                  <button
                    className="photo-thumb"
                    key={img.id}
                    onClick={() =>
                      setViewing({
                        url,
                        fileName: img.fileName,
                        inspector: rec.inspector,
                        project: rec.project,
                        location: rec.pointName,
                      })
                    }
                  >
                    <img src={url} alt="" loading="lazy" />
                  </button>
                );
              })}
            </div>
          </>
        )}

        {/* 一键确认:只针对置信偏低的项,替代逐字段按钮(旧版做法) */}
        {lowConf.length > 0 && (
          <div className="confirm-all">
            <div className="ca-msg">
              <b>{lowConf.length}</b> 项识别置信偏低,请核对
            </div>
            <button onClick={() => void confirmAll()} disabled={confirming}>
              {confirming ? "确认中…" : `一键确认 (${lowConf.length})`}
            </button>
          </div>
        )}

        <div className="fld-group-title">日报字段</div>
        <div className="fld-group">
          {rec.fields.map((f) => (
            <FieldRow key={f.code} field={f} recordId={rec.id} onSaved={mergeField} />
          ))}
        </div>
      </div>

      {viewing && <PhotoViewer meta={viewing} onClose={() => setViewing(null)} />}

      <div className="flow-foot">
        <Button block className="btn-primary" onClick={toPreview}>
          保存并预览日报
        </Button>
      </div>
    </div>
  );
}
