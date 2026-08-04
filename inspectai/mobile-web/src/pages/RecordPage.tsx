import { Button, Image, Picker, Toast } from "@/ui";
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
        {/* 选择类统一用 Picker:交互是下拉,但弹的是【底部选择面板】而不是
            系统控件。原生 <select> 的下拉样式完全不受控(灰底高亮、白框、
            字号各异),那是它难看的根源。
            (中间试过分段按钮,虽然少一次点击,但视觉上被否了。) */}
        {field.options?.length ? (
          <Picker
            options={field.options}
            value={value}
            onChange={(v) => {
              setValue(v);
              void commit(v); // 选择类改完即存,不等失焦
            }}
          />
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
  // 看图存的是【第几张】而不是那一张的元数据:查看器现在能左右翻,
  // 得把整组照片交给它。-1 表示没打开。
  const [viewing, setViewing] = useState(-1);
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

      // 【processing 也要接着等】原来这里是 `!== "not_started" 就 return`,
      // 于是"识别已经在跑"被当成"没我的事",既不转圈也不轮询 —— 页面就停在
      // 一堆空字段上,而后端十几秒后已经识别好了。用户看到的就是"识别完了
      // 没填表"。会撞上这个分支的场合不止一种:
      //   · 识别中刷新页面
      //   · 从预览页返回填报页
      //   · 从任务列表二次进入同一条记录
      // (还有一个更隐蔽的:转场层曾让页面挂载两次,第二次必然撞上 —— 那个
      //  已在 App.tsx 修掉,但这里的兜底得留着,上面三种是真实操作。)
      const running = cur.recognitionStatus === "not_started" || cur.recognitionStatus === "processing";
      if (!running) return;

      setAnalyzing(true);
      try {
        // 只有还没开始的才发起;已经在跑的直接进轮询,不重复触发(会多花一次
        // 模型调用,而且后端会把 CaptureAttempts 加一,凑够三次就误判成人工填写)
        if (cur.recognitionStatus === "not_started" && kickedRef.current !== id) {
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

  // 整组照片的元数据 —— 缩略图和查看器用【同一份】,翻页时顺序才对得上
  const photos: PhotoMeta[] = (rec?.images || []).map((img) => ({
    url: `/storage/uploads/${rec!.id}/${img.id}_${img.fileName}`,
    fileName: img.fileName,
    inspector: rec!.inspector,
    project: rec!.project,
    location: rec!.pointName,
  }));

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
              {photos.map((p, i) => (
                <button className="photo-thumb" key={p.url} onClick={() => setViewing(i)}>
                  {/* 用组件库的 Image:自带加载占位、失败兜底和自动重试。
                      手写 <img> 在现场信号差时会白一片或直接裂图。 */}
                  <Image src={p.url} radius={12} />
                </button>
              ))}
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

        {/* 「日报字段」标题删了:整页只有这一组字段,标题不起区分作用 */}
        <div className="fld-group">
          {rec.fields.map((f) => (
            <FieldRow key={f.code} field={f} recordId={rec.id} onSaved={mergeField} />
          ))}
        </div>
      </div>

      <PhotoViewer photos={photos} index={viewing} onClose={() => setViewing(-1)} />

      <div className="flow-foot">
        <Button block className="btn-primary" onClick={toPreview}>
          保存并预览日报
        </Button>
      </div>
    </div>
  );
}
