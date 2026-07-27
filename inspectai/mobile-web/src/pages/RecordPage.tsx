import { Button, Dialog, Input, Toast } from "antd-mobile";
import { useCallback, useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import {
  FieldValue,
  RecordDTO,
  getRecord,
  patchField,
  startAnalysis,
  submitRecord,
} from "@/api/inspection";

/** AI 识别是异步任务,轮询到出结果或超时 */
const POLL_MS = 2000;
const POLL_MAX = 30; // 最长约 60 秒

function FieldRow({
  field,
  recordId,
  onSaved,
}: {
  field: FieldValue;
  recordId: string;
  onSaved: (rec: RecordDTO) => void;
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
      const rec = await patchField(recordId, field.code, value, field.version, {
        action,
        durationMs: Date.now() - enteredAt.current,
      });
      onSaved(rec);
      enteredAt.current = Date.now();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    } finally {
      setSaving(false);
    }
  }

  const changed = value !== field.value;
  const isAI = field.source === "ai" && field.aiValue !== "";
  const lowConf = isAI && field.confidence > 0 && field.confidence < 0.8;

  return (
    <div className={lowConf ? "fld fld-warn" : "fld"}>
      <div className="fld-top">
        <span className="fld-label">
          {field.label}
          {field.required && <em className="fld-req">必填</em>}
        </span>
        {isAI && (
          <span className={lowConf ? "fld-conf low" : "fld-conf"}>
            AI {Math.round(field.confidence * 100)}%
          </span>
        )}
      </div>

      {field.options?.length ? (
        <div className="opt-row">
          {field.options.map((o) => (
            <button
              key={o}
              className={value === o ? "opt on" : "opt"}
              onClick={() => setValue(o)}
            >
              {o}
            </button>
          ))}
        </div>
      ) : (
        <Input
          value={value}
          onChange={setValue}
          placeholder={field.required ? "必填" : "选填"}
          type={field.kind === "number" ? "number" : "text"}
        />
      )}

      {isAI && field.aiValue !== value && (
        <div className="fld-ai">AI 原值:{field.aiValue || "(空)"}</div>
      )}
      {field.reason && <div className="fld-reason">{field.reason}</div>}

      <div className="fld-act">
        <button
          className="fld-btn primary"
          disabled={saving}
          onClick={() => void save(changed ? "correct" : "confirm")}
        >
          {changed ? "保存修改" : "确认无误"}
        </button>
      </div>
    </div>
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

  const load = useCallback(async () => {
    try {
      setRec(await getRecord(id));
    } catch {
      Toast.show({ content: "记录加载失败" });
    }
  }, [id]);

  useEffect(() => {
    void load();
  }, [load]);

  // 新建记录尚未识别 → 自动触发一次 AI 字段识别
  useEffect(() => {
    if (!rec || analyzing) return;
    if (rec.recognitionStatus !== "not_started") return;
    let cancelled = false;
    void (async () => {
      setAnalyzing(true);
      try {
        await startAnalysis(rec.id);
        for (let i = 0; i < POLL_MAX && !cancelled; i++) {
          await new Promise((r) => setTimeout(r, POLL_MS));
          const fresh = await getRecord(rec.id);
          if (cancelled) return;
          setRec(fresh);
          if (fresh.recognitionStatus !== "processing" && fresh.recognitionStatus !== "not_started") break;
        }
      } catch {
        Toast.show({ content: "AI 识别未成功,可手动填写" });
      } finally {
        if (!cancelled) setAnalyzing(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [rec, analyzing]);

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
        {rec.retakeReason && <div className="pending-alert">{rec.retakeReason}</div>}

        {rec.fields.map((f) => (
          <FieldRow key={f.code} field={f} recordId={rec.id} onSaved={setRec} />
        ))}

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
