import { Button, Toast } from "antd-mobile";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";

import {
  ClassifyResult,
  OfflineShotDTO,
  TemplateDTO,
  classifyOfflineShots,
  createRecordFromShots,
  listOfflineShots,
  listTemplates,
} from "@/api/inspection";

/** 拍摄与上传的时间差:离线越久差越大。公开展示,不隐藏 */
function offlineGap(shot: OfflineShotDTO): string {
  const cap = new Date(shot.capturedAt).getTime();
  const rec = new Date(shot.receivedAt).getTime();
  if (!cap || !rec || rec <= cap) return "";
  const mins = Math.round((rec - cap) / 60000);
  if (mins < 1) return "";
  if (mins < 60) return `离线 ${mins} 分钟`;
  return `离线 ${Math.round(mins / 60)} 小时`;
}

function fmtTime(iso: string): string {
  const d = new Date(iso);
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
}

// 已上传照片 → 选中 → AI 识别场景 → 确认模板 → 成单
export default function ReviewPage() {
  const nav = useNavigate();
  const [shots, setShots] = useState<OfflineShotDTO[]>([]);
  const [picked, setPicked] = useState<Set<string>>(new Set());
  const [templates, setTemplates] = useState<TemplateDTO[]>([]);
  const [classify, setClassify] = useState<ClassifyResult | null>(null);
  const [chosenTpl, setChosenTpl] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const [list, tpls] = await Promise.all([listOfflineShots(), listTemplates()]);
      setShots(list);
      setTemplates(tpls);
      // 默认全选:绝大多数情况就是把刚传的这批一起成单
      setPicked(new Set(list.map((s) => s.id)));
    } catch {
      Toast.show({ content: "加载失败,请下拉重试" });
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const pickedIds = useMemo(() => [...picked], [picked]);

  function toggle(id: string) {
    setPicked((cur) => {
      const next = new Set(cur);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    setClassify(null); // 选择变了,之前的识别结果作废
  }

  async function onClassify() {
    if (!pickedIds.length) {
      Toast.show({ content: "请先选择照片" });
      return;
    }
    setBusy(true);
    try {
      const res = await classifyOfflineShots(pickedIds);
      setClassify(res.classify);
      setChosenTpl(res.classify.needsManualPick ? "" : res.classify.templateId);
    } catch {
      Toast.show({ content: "识别失败,可手动选择模板" });
      setClassify({ templateId: "unknown", templateName: "无法识别", needsManualPick: true });
    } finally {
      setBusy(false);
    }
  }

  async function onCreate() {
    const tplID = chosenTpl || classify?.templateId;
    if (!tplID || tplID === "unknown") {
      Toast.show({ content: "请先选择日报模板" });
      return;
    }
    setBusy(true);
    try {
      const rec = await createRecordFromShots(tplID, pickedIds);
      nav(`/record/${rec.id}`, { replace: true });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "创建失败" });
    } finally {
      setBusy(false);
    }
  }

  if (loading) {
    return (
      <div className="center-screen">
        <span className="spinner" />
      </div>
    );
  }

  if (!shots.length) {
    return (
      <div className="center-screen">
        <h1 className="screen-title" style={{ textAlign: "center" }}>
          没有待处理的照片
        </h1>
        <p className="screen-sub" style={{ textAlign: "center" }}>
          先去拍照,联网后照片会自动上传到这里
        </p>
        <Button block className="btn-ghost" onClick={() => nav("/")}>
          去拍照
        </Button>
      </div>
    );
  }

  return (
    <div className="flow-screen">
      <div className="flow-head">
        <h1 className="flow-title">选择本次巡检的照片</h1>
        <p className="flow-sub">
          已上传 {shots.length} 张 · 选中 {picked.size} 张
        </p>
      </div>

      <div className="scroll-area flow-body">
        <div className="shot-grid">
          {shots.map((s) => {
            const gap = offlineGap(s);
            return (
              <button
                key={s.id}
                className={picked.has(s.id) ? "grid-cell picked" : "grid-cell"}
                onClick={() => toggle(s.id)}
              >
                <img src={`/api/inspection/offline-shots/${s.id}/image`} alt="" loading="lazy" />
                <span className="cell-check">{picked.has(s.id) ? "✓" : ""}</span>
                <span className="cell-meta">
                  {fmtTime(s.capturedAt)}
                  {gap && <em className="cell-gap">{gap}</em>}
                </span>
              </button>
            );
          })}
        </div>

        {classify && (
          <div className="classify-box">
            <div className="classify-head">
              {classify.needsManualPick ? "AI 未能确定场景,请手动选择" : "AI 识别为"}
            </div>
            {!classify.needsManualPick && (
              <div className="classify-hit">
                {classify.templateName}
                {typeof classify.confidence === "number" && (
                  <span className="classify-conf">
                    置信度 {Math.round(classify.confidence * 100)}%
                  </span>
                )}
              </div>
            )}
            <div className="tpl-list">
              {templates.map((t) => (
                <button
                  key={t.id}
                  className={chosenTpl === t.id ? "tpl-item on" : "tpl-item"}
                  onClick={() => setChosenTpl(t.id)}
                >
                  {t.name}
                </button>
              ))}
            </div>
          </div>
        )}
      </div>

      <div className="flow-foot">
        {!classify ? (
          <Button block className="btn-primary" loading={busy} onClick={() => void onClassify()}>
            AI 识别场景
          </Button>
        ) : (
          <Button block className="btn-primary" loading={busy} onClick={() => void onCreate()}>
            下一步 · 填写日报
          </Button>
        )}
      </div>
    </div>
  );
}
