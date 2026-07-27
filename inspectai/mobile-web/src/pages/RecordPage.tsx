import { Button, Dialog, Input, Toast } from "antd-mobile";
import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import {
  FieldValue,
  RecordDTO,
  enableManual,
  getRecord,
  patchField,
  startAnalysis,
  submitRecord,
} from "@/api/inspection";

/** AI 识别是异步任务,轮询到出结果或超时 */
const POLL_MS = 2000;
const POLL_MAX = 30; // 最长约 60 秒

/** AI 状态药丸:四种态,口径与旧版 pillFor/pillTextFor 完全一致 */
function pillOf(f: FieldValue): { cls: string; text: string } | null {
  if (f.source === "human-confirmed") return { cls: "edited", text: "已确认" };
  if (f.source === "human-edited") return { cls: "edited", text: "已修改" };
  if (f.source === "ai" && f.confidence) {
    return {
      cls: f.needsReview ? "review" : "confirmed",
      text: `AI ${Math.round(f.confidence * 100)}%`,
    };
  }
  if (f.source === "ai" && String(f.value || "").trim()) {
    return { cls: f.needsReview ? "review" : "confirmed", text: "AI 识别" };
  }
  if (f.source === "manual" && !f.value) return { cls: "empty", text: "待填" };
  return null;
}

// 字段行:旧版 iOS 设置列表样式 —— 标签左、值右、状态药丸
function FieldRow({
  field,
  recordId,
  onSaved,
}: {
  field: FieldValue;
  recordId: string;
  /** 只回传被更新的那一个字段(后端就返回这个),由上层合并进记录 */
  onSaved: (updated: FieldValue) => void;
}) {
  const [value, setValue] = useState(field.value);
  const [saving, setSaving] = useState(false);
  // 停留时长:后端据此写字段确认留痕,用来识别"秒确认"的惰性操作
  const enteredAt = useRef(Date.now());

  useEffect(() => {
    setValue(field.value);
  }, [field.value]);

  async function save(action: "confirm" | "correct") {
    if (saving) return;
    setSaving(true);
    try {
      const updated = await patchField(recordId, field.code, value, field.version, {
        action,
        durationMs: Date.now() - enteredAt.current,
      });
      onSaved(updated);
      enteredAt.current = Date.now();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    } finally {
      setSaving(false);
    }
  }

  const changed = value !== field.value;
  const pill = pillOf(field);
  const needsReview = field.source === "ai" && field.needsReview;

  return (
    <>
      <div className={needsReview ? "fld fld-warn" : "fld"}>
        <span className="fld-label">
          {field.label}
          {field.required && <em className="fld-req">*</em>}
        </span>

        <span className="fld-value">
          {field.options?.length ? (
            <span className="opt-row">
              {field.options.map((o) => (
                <button key={o} className={value === o ? "opt on" : "opt"} onClick={() => setValue(o)}>
                  {o}
                </button>
              ))}
            </span>
          ) : (
            <Input
              value={value}
              onChange={setValue}
              placeholder={field.required ? "必填" : "选填"}
              type={field.kind === "number" ? "number" : "text"}
              onBlur={() => {
                if (changed) void save("correct");
              }}
            />
          )}
          {changed ? (
            <button className="fld-btn" disabled={saving} onClick={() => void save("correct")}>
              保存
            </button>
          ) : pill ? (
            <span
              className={`ai-pill ${pill.cls}`}
              role="button"
              onClick={() => void save("confirm")}
              title="点击确认无误"
            >
              {pill.text}
            </span>
          ) : (
            <button className="fld-btn" disabled={saving} onClick={() => void save("confirm")}>
              确认
            </button>
          )}
        </span>
      </div>
      {field.source === "ai" && field.aiValue && field.aiValue !== value && (
        <div className="fld-ai">AI 原值:{field.aiValue}</div>
      )}
      {field.reason && <div className="fld-reason">{field.reason}</div>}
    </>
  );
}

// 填写日报:AI 预填 → 逐项确认/修正 → 提交
export default function RecordPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [rec, setRec] = useState<RecordDTO | null>(null);
  const [analyzing, setAnalyzing] = useState(false);
  const [submitting, setSubmitting] = useState(false);
  const [viewing, setViewing] = useState<PhotoMeta | null>(null);
  // 已发起识别的记录 ID,防 StrictMode 双跑重复建任务
  const kickedRef = useRef("");

  // 载入记录;若尚未识别则触发 AI 字段识别并轮询结果。
  //
  // 依赖只能是 id。曾经把 rec 也放进依赖,而循环里又 setRec ——
  // 每轮拿到新数据就触发 effect 重跑、清理函数置 cancelled,
  // 循环提前 return 导致 setAnalyzing(false) 永不执行:
  // 转圈永远停不下来、轮询也断了,用户以为"识别特别慢"。
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
        // StrictMode 下 effect 会跑两次,用 ref 防重复发起识别任务
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
          if (fresh.recognitionStatus !== "processing" && fresh.recognitionStatus !== "not_started") {
            break;
          }
        }
      } catch {
        Toast.show({ content: "AI 识别未成功,可手动填写" });
      } finally {
        // 无条件复位:组件还在就一定要把转圈停掉
        setAnalyzing(false);
      }
    })();
    return () => {
      stop = true;
    };
  }, [id]);

  // 后端 PATCH 字段只返回该字段,按 code 合并进记录(与旧版 Object.assign 同语义)
  function mergeField(updated: FieldValue) {
    setRec((cur) =>
      cur
        ? { ...cur, fields: cur.fields.map((f) => (f.code === updated.code ? { ...f, ...updated } : f)) }
        : cur,
    );
  }

  async function onSubmit() {
    if (!rec) return;
    const missing = rec.fields.filter((f) => f.required && !f.value.trim());
    if (missing.length) {
      Toast.show({ content: `还有 ${missing.length} 个必填项没填` });
      return;
    }
    const ok = await Dialog.confirm({
      content: "提交后记录将进入台账,确认提交?",
      confirmText: "提交",
      cancelText: "再看看",
    });
    if (!ok) return;

    setSubmitting(true);
    try {
      await submitRecord(rec.id);
      Toast.show({ content: "已提交", position: "bottom" });
      nav("/", { replace: true });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "提交失败" });
    } finally {
      setSubmitting(false);
    }
  }

  if (!rec) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  const done = rec.fields.filter((f) => f.value.trim()).length;

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">{rec.templateName}</h1>
        <p className="flow-sub">
          {rec.project} · {rec.pointName} · {done}/{rec.fields.length} 项已填
        </p>
      </div>

      <div className="scroll-area flow-body">
        {analyzing && (
          <div className="analyzing">
            <span className="spinner" />
            <span>AI 正在识别字段…</span>
          </div>
        )}
        {/* 识别不稳:照旧版给两条出路(补拍 / 转人工),不能只报错不给路走 */}
        {rec.retakeReason && (
          <div className="retake-box">
            <div className="retake-why">{rec.retakeReason}</div>
            <div className="retake-tries">已尝试 {rec.captureAttempts ?? 0} / 3 次</div>
            <div className="retake-acts">
              <button className="fld-btn" onClick={() => nav("/")}>
                去补拍
              </button>
              <button
                className="fld-btn primary"
                onClick={async () => {
                  try {
                    const fresh = await enableManual(rec.id);
                    setRec(fresh);
                    Toast.show({ content: "已转人工填写,请逐项填写后提交" });
                  } catch {
                    Toast.show({ content: "切换失败,请重试" });
                  }
                }}
              >
                转人工填写
              </button>
            </div>
          </div>
        )}
        {rec.manualRequired && !rec.retakeReason && (
          <div className="analyzing" style={{ color: "#ffc46b", background: "rgba(255,196,107,0.08)", borderColor: "rgba(255,196,107,0.24)" }}>
            <span>AI 未能稳定识别,请逐项手动填写</span>
          </div>
        )}

        <div className="fld-group-title">日报字段</div>
        <div className="fld-group">
          {rec.fields.map((f) => (
            <FieldRow key={f.code} field={f} recordId={rec.id} onSaved={mergeField} />
          ))}
        </div>

        {rec.images.length > 0 && (
          <div className="rec-images">
            <div className="rec-images-head">本次照片 {rec.images.length} 张</div>
            <div className="pending-strip">
              {rec.images.map((img) => {
                const url = `/storage/uploads/${rec.id}/${img.id}_${img.fileName}`;
                return (
                  <button
                    className="shot-card"
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
                    <img className="shot-img" src={url} alt="" />
                  </button>
                );
              })}
            </div>
          </div>
        )}
      </div>

      {viewing && <PhotoViewer meta={viewing} onClose={() => setViewing(null)} />}

      <div className="flow-foot">
        <Button block className="btn-primary" loading={submitting} onClick={() => void onSubmit()}>
          提交日报
        </Button>
      </div>
    </div>
  );
}
