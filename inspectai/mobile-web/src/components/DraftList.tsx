import { Button, Dialog, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";

import { DraftBrief, deleteDraftRecord, listDrafts } from "@/api/inspection";

/**
 * 没提交完的记录。
 *
 * 【为什么必须有这一块】提交被打断(退出微信、信号断、后台被杀)之后,
 * 那条记录留在库里没提交,而手机上一个入口都没有 —— 首页、任务、
 * 待处理、台账都不列它。
 *
 * 更要命的是照片跟着一起消失:建记录时照片已经从「待处理」里认领走了,
 * 所以人既回不到那条记录,也拿不回照片重做一次 —— 现场白跑一趟。
 *
 * 给两条出路,都由本人决定:接着提交,或者直接删掉。
 * 【删掉不销毁照片】后端会把当初认领的照片放回「待处理」—— 现场拍的东西
 * 不能因为删一条草稿就没了。
 */
export default function DraftList() {
  const nav = useNavigate();
  const [drafts, setDrafts] = useState<DraftBrief[]>([]);
  const [busy, setBusy] = useState("");

  const load = useCallback(async () => {
    try {
      setDrafts(await listDrafts());
    } catch {
      setDrafts([]); // 拿不到就不显示,不在这一屏上摆一个报错
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  if (!drafts.length) return null;

  async function remove(d: DraftBrief) {
    const ok = await Dialog.confirm({
      title: "删除这条没提交的记录?",
      // 【说清照片的去向】不说的话没人敢点 —— 谁也不想把现场拍的东西删没了。
      content: "记录会删掉,当初用的照片会放回「待处理」,可以重新来一次。",
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    setBusy(d.id);
    try {
      await deleteDraftRecord(d.id);
      Toast.show({ content: "已删除,照片已放回待处理" });
      await load();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "删除失败" });
    } finally {
      setBusy("");
    }
  }

  return (
    <div className="draft-list">
      <div className="draft-list-title">
        没提交完的记录 {drafts.length} 条
      </div>
      {drafts.map((d) => (
        <div className="draft-row" key={d.id}>
          <div className="draft-main">
            <div className="draft-name">
              {d.assetNo ? `${d.assetNo} · ` : ""}
              {d.templateName || "巡检记录"}
            </div>
            <div className="draft-meta">
              {d.createdAt}
              {d.imageCount > 0 && ` · ${d.imageCount} 张照片`}
              {/* 【填了几项要说出来】只给时间戳的话,人分不清"刚建的空壳"
                  和"就差点提交" —— 而这两种该做的事完全相反。 */}
              {d.fieldsTotal > 0 && ` · 已填 ${d.fieldsFilled}/${d.fieldsTotal} 项`}
            </div>
          </div>
          <div className="draft-actions">
            <Button
              block={false}
              className="draft-btn draft-btn-go"
              onClick={() => nav(`/record/${encodeURIComponent(d.id)}`)}
            >
              接着填
            </Button>
            <Button
              block={false}
              type="ghost"
              className="draft-btn draft-btn-del"
              loading={busy === d.id}
              onClick={() => void remove(d)}
            >
              删除
            </Button>
          </div>
        </div>
      ))}
    </div>
  );
}
