import { Button, Dialog, Toast } from "@/ui";
import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import { RecordDTO, getRecord, submitRecord } from "@/api/inspection";
import { clearActiveTask, getActiveTask } from "@/store/activeTask";

/** AI 总结未就绪时轮询等待:提交后后端才异步生成总结 */
const POLL_MS = 2000;
const POLL_MAX = 15;

const PRIORITY_ORDER: Record<string, number> = { high: 0, medium: 1, low: 2 };
const PRIORITY_TEXT: Record<string, string> = { high: "高", medium: "中", low: "低" };

// 日报预览(旧版 scenePreview):字段汇总 + AI 总结 + AI 行动建议 → 提交
export default function PreviewPage() {
  const { id = "" } = useParams();
  const nav = useNavigate();
  const [rec, setRec] = useState<RecordDTO | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [waitingSummary, setWaitingSummary] = useState(false);

  useEffect(() => {
    let stop = false;
    void (async () => {
      try {
        const fresh = await getRecord(id);
        if (!stop) setRec(fresh);
      } catch {
        Toast.show({ content: "记录加载失败" });
      }
    })();
    return () => {
      stop = true;
    };
  }, [id]);

  async function onSubmit() {
    if (!rec || submitting) return;
    const missing = rec.fields.filter((f) => f.required && !f.value.trim());
    if (missing.length) {
      Toast.show({ content: `必填字段未完成:${missing[0].label}` });
      return;
    }
    // 后端会拒绝 retake_required 状态的提交。与其让用户点了没反应,
    // 不如提前说清楚并给出路 —— 出路在字段页(重拍 / 转人工)。
    if (rec.recognitionStatus === "retake_required" && !rec.manualRequired) {
      const go = await Dialog.confirm({
        content: rec.retakeReason
          ? `这条记录还需处理:${rec.retakeReason}
先去补拍或转人工填写,才能提交。`
          : "这条记录识别不稳,需先补拍或转人工填写后才能提交。",
        confirmText: "去处理",
        cancelText: "取消",
      });
      if (go) nav(`/record/${rec.id}`);
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
      const submitted = await submitRecord(rec.id);
      setRec(submitted);
      // 关联的工程任务由后端自动销账,本地清掉当前任务标记
      if (getActiveTask()) clearActiveTask();
      Toast.show({ content: "已提交", position: "bottom" });

      // AI 总结是提交后异步生成的,轮询等它出来再展示
      if (!submitted.aiSummary) {
        setWaitingSummary(true);
        for (let i = 0; i < POLL_MAX; i++) {
          await new Promise((r) => setTimeout(r, POLL_MS));
          const fresh = await getRecord(rec.id);
          setRec(fresh);
          if (fresh.aiSummary || fresh.aiSummaryError) break;
        }
        setWaitingSummary(false);
      }
    } catch (err) {
      // 失败原因必须让用户看见 —— 后端会因"待重拍/必填缺失"等明确拒绝,
      // 吞掉错误会让人以为按钮坏了。
      Toast.show({
        content: err instanceof Error ? err.message : "提交失败",
        duration: 3500,
      });
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

  const recos = [...(rec.aiRecommendations || [])].sort(
    (a, b) => (PRIORITY_ORDER[a.priority] ?? 3) - (PRIORITY_ORDER[b.priority] ?? 3),
  );

  return (
    <div className="flow-screen">
      <FlowHeader
        title={rec.submitted ? "日报已提交" : "提交日报"}
        onBack={() => nav(`/record/${rec.id}`)}
        action={{ text: "设备健康", onClick: () => nav("/ledger") }}
        step="preview"
      />

      <div className="scroll-area flow-body">
        <p className="flow-caption">
          {rec.templateName} · {rec.project} · {rec.pointName}
        </p>
        {/* 字段汇总:只读,要改回上一步 */}
        <div className="pv-title">日报字段</div>
        <div className="pv-card">
          {rec.fields.map((f) => (
            <div className="pv-row" key={f.code}>
              <span className="pv-k">{f.label}</span>
              <span className="pv-v">{f.value || "-"}</span>
            </div>
          ))}
        </div>

        {rec.aiSummary && (
          <div className="pv-card ai-card">
            <div className="ai-head">
              <span className="ai-mark">✦</span>
              <span className="ai-title">AI 总结</span>
            </div>
            <div className="ai-body">{rec.aiSummary}</div>
            {rec.aiSummaryTags && rec.aiSummaryTags.length > 0 && (
              <div className="ai-tags">
                {rec.aiSummaryTags.map((t) => (
                  <span className="tag tag-brand" key={t}>
                    {t}
                  </span>
                ))}
              </div>
            )}
          </div>
        )}

        {waitingSummary && !rec.aiSummary && (
          <div className="analyzing">
            <span className="spinner" />
            <span>AI 正在生成日报总结…</span>
          </div>
        )}

        {recos.length > 0 && (
          <div className="pv-card ai-card">
            <div className="ai-head">
              <span className="ai-mark amber">✦</span>
              <span className="ai-title">AI 行动建议</span>
            </div>
            {recos.map((r, i) => (
              <div className="reco" key={i}>
                <span className={`reco-pri ${r.priority || "low"}`}>
                  {PRIORITY_TEXT[r.priority] || "·"}
                </span>
                <span className="reco-main">
                  <span className="reco-text">{r.text}</span>
                  <span className="reco-meta">
                    {r.category || "建议"} · 依据:{r.basis || "基于本次字段"}
                  </span>
                </span>
              </div>
            ))}
          </div>
        )}

        {rec.aiSummaryError && (
          <div className="pending-alert">
            <span className="pa-msg">AI 总结生成异常:{rec.aiSummaryError}(已使用本地兜底文本)</span>
          </div>
        )}
      </div>

      <div className="flow-foot foot-row">
        {rec.submitted ? (
          <Button block className="btn-primary" onClick={() => nav("/ledger")}>
            查看设备健康
          </Button>
        ) : (
          /* 「返回修改」已由顶栏返回键承担,底部只留主操作 */
          <Button block className="btn-primary" loading={submitting} onClick={() => void onSubmit()}>
            提交日报
          </Button>
        )}
      </div>
    </div>
  );
}
