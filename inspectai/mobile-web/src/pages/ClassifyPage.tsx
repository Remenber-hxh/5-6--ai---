import { Button, Toast } from "@/ui";
import { useCallback, useEffect, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";

import FlowHeader from "@/components/FlowHeader";
import LoadingScene from "@/components/LoadingScene";
import { clearRetakeTarget, getRetakeTarget } from "@/store/retake";
import { ApiError } from "@/api/client";
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
  // 走"已知设备"那条快路时,等待屏要说实话(没有 AI 在跑)
  const [skipping, setSkipping] = useState(false);
  const [templates, setTemplates] = useState<TemplateDTO[]>([]);
  const [busy, setBusy] = useState(false);
  // 残留的复检/扫码上下文已被判定无效并清掉 —— 本次渲染起不再采用它
  const [retakeGone, setRetakeGone] = useState(false);

  const run = useCallback(async () => {
    if (!shotIds.length) {
      nav("/review", { replace: true });
      return;
    }
    // 【已知模板就别再让 AI 猜一次】扫码和复检都已经确定了是哪台设备、用哪个模板。
    //
    // 原来这里【无条件】跑一次场景分类,拿到结果之后在 go() 里被 retake.templateId
    // 覆盖掉 —— 等于花了一次 AI 调用、让人在"识别中"那屏干等十几秒,然后把结果扔了。
    //
    // 直接建记录跳过去:少一次调用、少一屏等待、少一个能猜错的环节。
    const known = getRetakeTarget();
    if (known?.templateId) {
      setSkipping(true);
      try {
        const rec = await createRecordFromShots(
          known.templateId,
          shotIds,
          known.pointId || undefined,
        );
        nav(`/record/${rec.id}`, { replace: true });
        return;
      } catch (err) {
        // 建记录失败(多半是网络)不该把人卡死在这一屏 —— 退回正常的分类流程,
        // 照片还在服务器上,手动选模板照样走得下去。
        setSkipping(false);

        // 【上下文本身坏了就必须扔掉,不能只是退回】
        //
        // 这里原来静默吞掉一切错误,埋了一个很难查的坑:上下文里的 templateId
        // 在服务端根本不存在时——
        //   1. 这一步拿它建记录,服务器回「未找到日报模板」,被这个 catch 吞掉
        //   2. 屏幕翻到"AI 正在识别场景",人以为一切正常
        //   3. AI 正确识别出模板,界面照实显示"识别为 X,置信度 96%"
        //   4. 点「下一步」,go() 里又用这条坏上下文把正确结果覆盖回去
        //   5. 同一个错误这次才弹出来 —— 而界面上明明写着识别成功
        // 也就是说界面显示的模板和实际发出去的不是同一个。
        //
        // 坏 templateId 有两个来源,现象一样:
        //   - 残留:上下文只在"提交成功"或"手动取消"时清,中途放弃就一直留着
        //     (已另加 12 小时过期,见 store/retake)
        //   - 扫码:二维码里的 templateId 只是资产 ID 中间那一段字符串,
        //     从没和真实模板表校验过,模板下线或改名就会失配
        //
        // 4xx = 服务器明确说这个上下文不成立(模板/点位/任务不存在),
        // 留着它只会让后面每一步继续用错的 id。5xx 和网络错误则保留,
        // 那些是临时故障,清掉会让正在做的复检白做。
        //
        // 【401 除外】会话过期不是这条上下文的错。清了的话,人重新登录回来
        // 会发现复检目标莫名其妙没了,而他什么都没做错。
        const bad =
          err instanceof ApiError &&
          err.status >= 400 &&
          err.status < 500 &&
          err.status !== 401;
        if (bad) {
          clearRetakeTarget();
          setRetakeGone(true);
          // 【必须说一声】人是带着"我在复检这台设备"的预期进来的,
          // 默默取消锁定的话,这条记录会当成一次普通巡检落库,
          // 而他以为原来那条异常已经销账了。
          Toast.show({
            content:
              known.mode === "scan"
                ? "扫码锁定的设备已失效,本次按普通巡检继续"
                : "复检目标已失效,本次按普通巡检继续",
          });
        }
      }
    }
    try {
      const [res, tpls] = await Promise.all([
        classifyOfflineShots(shotIds),
        listTemplates(),
      ]);
      setTemplates(tpls);
      setResult(res.classify);
    } catch {
      setTemplates(await listTemplates().catch(() => []));
      // 识别失败不阻断:转手动选模板,照片仍在服务器上不会丢
      setResult({
        templateId: "unknown",
        templateName: "无法识别",
        needsManualPick: true,
      });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    void run();
  }, [run]);

  // 复检:目标设备用哪个模板是【已知】的,不该再让 AI 猜一次 ——
  // 猜错了这条记录就落到别的模板上,原来那条异常照样挂着,而且多出一台"新设备"。
  //
  // 【上面判定为无效的,这里必须一起失效】只清 localStorage 不够:
  // retake 是渲染期读的,本次渲染里它还是旧值,go() 照样会拿它覆盖
  // AI 的正确结果 —— 那正是这个 bug 的最后一步。
  const retake = retakeGone ? null : getRetakeTarget();

  async function go(templateId: string) {
    if (retake?.templateId) templateId = retake.templateId;
    if (busy || !templateId || templateId === "unknown") return;
    setBusy(true);
    try {
      // 点位也带上:同一台设备的记录要落在同一个点位下
      const rec = await createRecordFromShots(
        templateId,
        shotIds,
        retake?.pointId || undefined,
      );
      nav(`/record/${rec.id}`, { replace: true });
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "创建失败" });
      setBusy(false);
    }
  }

  // 识别中:整屏场景,与旧版一致
  if (skipping) return <LoadingScene kind="known" />;
  if (!result) return <LoadingScene kind="classify" />;

  const uncertain = result.needsManualPick || result.templateId === "unknown";

  return (
    <div className="flow-screen">
      <FlowHeader
        title="确认场景"
        onBack={() => nav("/review")}
        step="classify"
      />

      <div className="scroll-area flow-body">
        <p className="flow-caption">已选 {shotIds.length} 张照片</p>
        <div className={uncertain ? "classify-result warn" : "classify-result"}>
          <div className="cr-label">
            {uncertain ? "AI 不太确定" : "AI 识别为"}
          </div>
          <div className="cr-name">
            {uncertain ? "请手动选择模板" : result.templateName}
          </div>
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
              <button
                className="template-row"
                key={t.id}
                onClick={() => void go(t.id)}
              >
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
          <Button
            block
            className="btn-primary"
            loading={busy}
            onClick={() => void go(result.templateId)}
          >
            下一步 · 填写日报
          </Button>
        </div>
      )}
    </div>
  );
}
