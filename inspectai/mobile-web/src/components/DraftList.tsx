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
 *
 * 【删掉就是删掉了】不承诺"照片能拿回来"、也不提"可以重新来一次"。
 * 早先的做法是把照片退回「待处理」并在文案里说明 —— 结果那些照片重新堆在
 * 待处理列表里,人以为没删干净、又去删一遍,而他点删除时本来的意思就是
 * "这一趟不要了"。多说一句反而让人不确定到底删没删。
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
      // 【只说后果,不做承诺】组件要求必须有 content。
      // 这里唯一该说的是"删了就没了" —— 那是人按下去之前需要知道的;
      // "照片能拿回来""可以重新来一次"都是给自己找麻烦的承诺。
      content: "删除后不可恢复。",
      confirmText: "删除",
      cancelText: "取消",
    });
    if (!ok) return;
    setBusy(d.id);
    try {
      await deleteDraftRecord(d.id);
      Toast.show({ content: "已删除" });
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
