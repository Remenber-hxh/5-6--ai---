import { Button, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import LoadingScene from "@/components/LoadingScene";
import {
  ClassifyResult,
  TemplateDTO,
  classifyOfflineShots,
  createRecordFromShots,
  listTemplates,
} from "@/api/inspection";

// 场景确认:独立一屏(旧版 sceneClassify)。
// 旧版语义 —— AI 有把握时【只显示识别结果,不列模板】;
// 只有 AI 不确定才列出模板供手动选,且点整行即进入下一步。
export default function ClassifyPage() {
  const nav = useNavigate();
  const [params] = useSearchParams();
  // 走 URL 参数,刷新页面不丢选择
  const shotIds = (params.get("shots") || "").split(",").filter(Boolean);

  const [result, setResult] = useState<ClassifyResult | null>(null);
  const [templates, setTemplates] = useState<TemplateDTO[]>([]);
  const [busy, setBusy] = useState(false);

  const run = useCallback(async () => {
    if (!shotIds.length) {
      nav("/review", { replace: true });
      return;
    }
    try {
      const [res, tpls] = await Promise.all([classifyOfflineShots(shotIds), listTemplates()]);
      setTemplates(tpls);
      setResult(res.classify);
    } catch {
      setTemplates(await listTemplates().catch(() => []));
      // 识别失败不阻断:转手动选模板,照片仍在服务器上不会丢
      setResult({ templateId: "unknown", templateName: "无法识别", needsManualPick: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void run();
  }, [run]);

  async function go(templateId: string) {
    if (busy || !templateId || templateId === "unknown") return;
    setBusy(true);
    try {
      const rec = await createRecordFromShots(templateId, shotIds);
      nav(`/record/${rec.id}`, { replace: true });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "创建失败" });
      setBusy(false);
    }
  }

  // 识别中:整屏场景,与旧版一致
  if (!result) return <LoadingScene kind="classify" />;

  const uncertain = result.needsManualPick || result.templateId === "unknown";

  return (
    <div className="flow-screen">
      <FlowHeader title="确认场景" onBack={() => nav("/review")} step="classify" />

      <div className="scroll-area flow-body">
        <p className="flow-caption">已选 {shotIds.length} 张照片</p>
        <div className={uncertain ? "classify-result warn" : "classify-result"}>
          <div className="cr-label">{uncertain ? "AI 不太确定" : "AI 识别为"}</div>
          <div className="cr-name">{uncertain ? "请手动选择模板" : result.templateName}</div>
          <div className="cr-conf">
            {uncertain
              ? result.error || "未识别到典型设备"
              : `置信度 ${Math.round((result.confidence || 0) * 100)}%`}
          </div>
        </div>

        {/* 只有 AI 不确定时才列模板;有把握时这里是空的(旧版做法) */}
        {uncertain && (
          <div className="template-list">
            {templates.map((t) => (
              <button className="template-row" key={t.id} onClick={() => void go(t.id)}>
                <span className="tr-icon">{t.name.charAt(0)}</span>
                <span className="tr-meta">
                  <span className="tr-name">{t.name}</span>
                  <span className="tr-sub">{t.project}</span>
                </span>
                <span className="tr-arrow">›</span>
              </button>
            ))}
          </div>
        )}
      </div>

      {!uncertain && (
        <div className="flow-foot">
          <Button block className="btn-primary" loading={busy} onClick={() => void go(result.templateId)}>
            下一步 · 填写日报
          </Button>
        </div>
      )}
    </div>
  );
}
