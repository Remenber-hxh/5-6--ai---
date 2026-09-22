import { Button, DateField, Image, Picker, Toast } from "@/ui";
import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";

import CenterLoading from "@/components/CenterLoading";
import FlowHeader from "@/components/FlowHeader";
import LoadingScene from "@/components/LoadingScene";
import PhotoViewer, { PhotoMeta } from "@/components/PhotoViewer";
import MeterPhotoRow, { fieldOfPhoto } from "@/components/MeterPhotoRow";
import ReadingCrop from "@/components/ReadingCrop";
import {
  FieldValue,
  RecordDTO,
  enableManual,
  getRecord,
  listTemplates,
  moveReading,
  patchField,
  patchFieldAsset,
  patchFieldAssetSource,
  patchFieldSource,
  swapReadings,
  startAnalysis,
} from "@/api/inspection";
import { usePolling } from "@/hooks/usePolling";
import { useResource } from "@/hooks/useResource";
import { getRetakeTarget } from "@/store/retake";

const POLL_MS = 2000;
const POLL_MAX = 40; // 最长约 80 秒

/** AI 状态药丸,口径与旧版 pillFor / pillTextFor 一致 */
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
  cropUrl,
  onOpenPhoto,
}: {
  field: FieldValue;
  recordId: string;
  onSaved: (updated: FieldValue) => void;
  /** 这个读数来源照片的地址;没有来源就不传 */
  cropUrl?: string;
  onOpenPhoto?: () => void;
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
      const updated = await patchField(
        recordId,
        field.code,
        next,
        field.version,
        {
          action: "correct",
          durationMs: Date.now() - enteredAt.current,
        },
      );
      onSaved(updated);
      enteredAt.current = Date.now();
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    }
  }

  // 只改设备归属,不动读数 —— 走的是另一个接口(不带 value),
  // 否则后端会把没提交的读数当成"改成空"。
  async function commitAsset(next: string) {
    if (next === (field.assetName || "")) return;
    try {
      onSaved(await patchFieldAsset(recordId, field.code, next, field.version));
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    }
  }

  const pill = pillOf(field);
  const pillEl = pill ? (
    <span className={`ai-pill ${pill.cls}`}>{pill.text}</span>
  ) : null;

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

  const hasAssetPicker = Boolean(field.assetOptions?.length);
  const rowCls = [
    "fld",
    field.needsReview ? "fld-warn" : "",
    hasAssetPicker ? "fld-meter" : "",
  ]
    .filter(Boolean)
    .join(" ");

  return (
    <div className={rowCls}>
      <div className="fld-label">
        <span className="fld-label-text">
          {field.label}
          {field.required && <em className="fld-req">*</em>}
        </span>
        {/* 【这个读数是哪台表的】抄表一条记录抄六台,照片上没有 Z1~Z4 标识,
            AI 只能按上传顺序猜 —— 中间夹一张读不出的就整体错位一格,
            读数一个不差、全填错了格子,而且哪儿都不报错。

            放在标签下面单起一行,不跟读数挤在一起:它说明的是"这一格是谁的",
            和读数不是一回事;并排摆着人会以为要填两个数。 */}
        {hasAssetPicker ? (
          <span className="fld-asset">
            <Picker
              options={field.assetOptions ?? []}
              value={field.assetName || ""}
              placeholder="选设备"
              onChange={(v) => void commitAsset(v)}
            />
          </span>
        ) : null}
      </div>
      <div className="fld-value">
        {/* 【读数区特写摆在输入框左边】核对从"记着六张照片的顺序、点开、翻页、
            退回来"变成扫一眼。放右边会被数字和药丸挤掉,放左边紧贴标签,
            视线是「这一格 → 这张图 → 这个数」一条线。 */}
        {cropUrl && field.bbox?.length === 4 ? (
          <ReadingCrop
            url={cropUrl}
            bbox={field.bbox}
            onOpen={onOpenPhoto}
          />
        ) : null}
        {pillEl}
        {/* 选择类统一用 Picker:交互是下拉,但弹的是【底部选择面板】而不是
            系统控件。原生 <select> 的下拉样式完全不受控(灰底高亮、白框、
            字号各异),那是它难看的根源。
            (中间试过分段按钮,虽然少一次点击,但视觉上被否了。) */}
        {field.code === "inspection_time" ? (
          /* 日期时间用滚轮选,不让人在小键盘上敲 16 个字符 ——
             敲错一个后端就解析不出来,而巡检员是戴着手套操作的。 */
          <DateField
            value={value}
            onChange={(v) => {
              setValue(v);
              void commit(v);
            }}
          />
        ) : field.options?.length ? (
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
  // 这类巡检最少要几张照片。0 = 不限。
  //
  // 【为什么填单页必须自己说一遍】拦截在后端的"提交"那一步,而在此之前
  // 选照片、分类、填一整张表全都放行 —— 巡检员填完十几个字段才被打回来,
  // 而那时他可能已经离开设备现场了,补拍要重新跑一趟。
  // 选照片那一屏只有扫码/复检流程能提前提示(那时模板已知),
  // 普通流程要等 AI 分完场景才知道模板 —— 也就是【到这一页才知道】。
  const [minImages, setMinImages] = useState(0);

  useEffect(() => {
    if (!rec?.templateId) return;
    listTemplates()
      .then((tpls) => setMinImages(tpls.find((t) => t.id === rec.templateId)?.minImages || 0))
      .catch(() => void 0); // 取不到就不提示,后端仍然会拦
  }, [rec?.templateId]);
  const [analyzing, setAnalyzing] = useState(false);
  const [confirming, setConfirming] = useState(false);
  // 看图存的是【第几张】而不是那一张的元数据:查看器现在能左右翻,
  // 得把整组照片交给它。-1 表示没打开。
  const [viewing, setViewing] = useState(-1);
  const kickedRef = useRef("");

  // 首次加载。竞态防护和卸载保护在 useResource 里。
  const { data: loaded } = useResource(
    (signal) => getRecord(id, signal),
    [id],
    {
      errorText: "记录加载失败",
    },
  );
  useEffect(() => {
    if (!loaded) return;
    const rt = getRetakeTarget();
    if (!rt?.assetNo) {
      setRec(loaded);
      return;
    }
    // 复检:把目标设备编号写进 asset_no。
    //
    // 【这一步才是决定归属的】前面强制模板和点位只保证"落在同一类、同一处",
    // 后端认资产身份靠的是 asset_no(见 assetIDFor)。不写的话,AI 从照片里
    // 认出来的编号可能和目标不一致 —— 复检出来的记录会挂到另一台设备上,
    // 原来那条异常照样挂着,而且台账里多一台。
    // source 标成 manual:这是人指定的,不是识别出来的,别让后续流程当成低置信。
    setRec({
      ...loaded,
      fields: loaded.fields.map((f) =>
        f.code === "asset_no"
          ? { ...f, value: rt.assetNo, source: "manual", confidence: 1 }
          : f,
      ),
    });
  }, [loaded]);

  // 【processing 也要接着等】原来是 `状态 !== "not_started" 就 return`,
  // 于是"识别已经在跑"被当成"没我的事",既不转圈也不轮询 —— 页面停在一堆空
  // 字段上,而后端十几秒后已经识别好了。用户看到的就是"识别完了没填表"。
  // 会撞上这个分支的场合不止一种:识别中刷新、从预览页返回、从任务列表二次
  // 进入同一条记录。(还有一个更隐蔽的:转场层曾让页面挂载两次,第二次必然
  // 撞上 —— 那个已在 App.tsx 修掉,但这里的判断本来就该这么写。)
  const running =
    rec?.recognitionStatus === "not_started" ||
    rec?.recognitionStatus === "processing";

  // 还没开始的才发起识别;已经在跑的直接进轮询,不重复触发 ——
  // 重复触发会多花一次模型调用,而且后端把 CaptureAttempts 加一,
  // 凑够三次就误判成"人工填写"。
  useEffect(() => {
    if (rec?.recognitionStatus !== "not_started" || kickedRef.current === id)
      return;
    kickedRef.current = id;
    void startAnalysis(id).catch(() =>
      Toast.show({ content: "AI 识别未成功,可手动填写" }),
    );
  }, [id, rec?.recognitionStatus]);

  useEffect(() => setAnalyzing(Boolean(running)), [running]);

  usePolling(() => getRecord(id), {
    enabled: Boolean(running),
    intervalMs: POLL_MS,
    maxTicks: POLL_MAX,
    onTick: setRec,
    done: (fresh) =>
      fresh.recognitionStatus !== "processing" &&
      fresh.recognitionStatus !== "not_started",
    onGiveUp: () => Toast.show({ content: "AI 识别未成功,可手动填写" }),
  });

  // 后端 PATCH 字段只返回该字段,按 code 合并(与旧版 Object.assign 同语义)
  function mergeField(updated: FieldValue) {
    setRec((cur) =>
      cur
        ? {
            ...cur,
            fields: cur.fields.map((f) =>
              f.code === updated.code ? { ...f, ...updated } : f,
            ),
          }
        : cur,
    );
  }

  // 整组照片的元数据 —— 缩略图和查看器用【同一份】,翻页时顺序才对得上
  // 还差几张。0 = 够了或不限。
  const shortOf = minImages > 0 ? Math.max(0, minImages - (rec?.images?.length || 0)) : 0;

  const photos: PhotoMeta[] = (rec?.images || []).map((img) => ({
    url: `/storage/uploads/${rec!.id}/${img.id}_${img.fileName}`,
    fileName: img.fileName,
    inspector: rec!.inspector,
    project: rec!.project,
    location: rec!.pointName,
  }));

  // 只统计置信度 <95% 的 AI 字段;≥95% 视为可信,不需人工逐项确认(旧版口径)
  const lowConf = (rec?.fields || []).filter(
    (f) =>
      f.source === "ai" &&
      String(f.value || "").trim() !== "" &&
      (f.confidence || 0) < 0.95,
  );

  // ===== 顶部语境:这份表是谁、在哪填的 =====
  const siteField = (rec?.fields || []).find((f) => f.code === "site") || null;
  const [siteDraft, setSiteDraft] = useState("");
  useEffect(() => setSiteDraft(siteField?.value || ""), [siteField?.value]);

  // 【可以直接传值】下拉选中时 setSiteDraft 还没生效,读 siteDraft 拿到的是旧值 ——
  // 表现是"选了巡塘书香,存进去的还是紫菡雅集"。
  async function commitSite(next: string = siteDraft) {
    if (!rec || !siteField || next === siteField.value) return;
    try {
      mergeField(
        await patchField(rec.id, siteField.code, next, siteField.version, {
          action: "correct",
        }),
      );
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
      setSiteDraft(siteField.value); // 存不上就退回原值,别让界面显示一个没存进去的数
    }
  }

  // ===== 抄表模式:一张照片一行 =====
  //
  // 【判据是"这一格配了设备类型"】不是写死模板 id。配了类型就说明
  // "这个读数属于某一台具体的设备",那才谈得上"选是哪台"。
  // 写死 id 的话,以后后台配出第二个抄表模板,它不会走这条路,而且不报错。
  const isMeterField = (f: FieldValue) => Boolean(f.assetOptions?.length);
  const meterMode = (rec?.fields || []).some(isMeterField) && (rec?.images.length || 0) > 0;

  /** 这次巡检能选的全部设备(按台账现算,后端已经填在每个读数字段上) */
  function allAssets(): string[] {
    const all = new Set<string>();
    for (const f of rec?.fields || []) for (const o of f.assetOptions || []) all.add(o);
    return [...all];
  }

  /**
   * 这一格是不是真的有人在用(绑了照片,或者已经有读数)。
   * 用来找"哪一格还空着",以及判断该走搬家还是对调。
   */
  function slotInUse(f: FieldValue): boolean {
    return Boolean(f.sourceImageId) || String(f.value || "").trim() !== "";
  }

  /**
   * 哪些设备现在选不了 —— 一台设备只能归一行,已经归了别行的变灰。
   *
   * 【判据是"另一行正用着它",不看抄到数没有】只要那一格绑了照片(界面上就是
   * 实实在在的另一行),或者已经有读数,这台设备就算名下有主。
   * 只按"有读数"算的话,一台已经归了第 1 行、只是还没抄到数的表,
   * 在别的行里还能再选一次 —— 同一台设备时而灰时而不灰,人无从预期。
   *
   * 【代价,写在这儿免得以后忘】六张照片都认领完之后,所有在用的设备都是灰的,
   * 想把两行的归属换过来就得先把其中一行改成一台还没用过的设备腾位置。
   * 台账里的表比格子多时总有余量;一样多时这一页就调不动了。
   *
   * 【为什么是变灰而不是从列表里去掉】去掉的话人只会觉得"怎么没有 Z3",
   * 不知道它在哪、也不知道该怎么办;摆在那儿变灰,至少说明"它在,只是轮不到"。
   */
  function takenAssets(self: FieldValue | null): Record<string, string> {
    const out: Record<string, string> = {};
    if (!rec) return out;
    for (const f of rec.fields) {
      if (f.code === self?.code || !f.assetName || !slotInUse(f)) continue;
      out[f.assetName] = "另一张照片已选";
    }
    return out;
  }

  /**
   * 放掉这一行选的设备 —— 让它在别的行里重新变成可选。
   *
   * 【为什么需要这个动作】已经归了别行的设备在下拉里是灰的。六张照片都认领完
   * 之后,在用的设备就全是灰的,想把两行的归属换过来就没有入口了。
   * 先在一行点「清除」,那台表立刻在别的行里可选 —— 这是解开错位的那把钥匙。
   *
   * 【只放设备,不动读数和照片】这一行还是这张照片、还是这个数,只是暂时
   * 说不清它是哪台表。清掉读数的话,人为了换个归属得把抄到的数重抄一遍。
   */
  async function clearAssetForField(f: FieldValue) {
    if (!rec) return;
    try {
      await patchFieldAsset(rec.id, f.code, "", f.version);
      setRec(await getRecord(rec.id));
    } catch (err) {
      Toast.show({
        content: err instanceof Error ? err.message : "清除失败,请重试",
        duration: 3000,
      });
    }
  }

  /**
   * 给某张照片指定设备。
   *
   * 【三种情况】
   *   照片还没人认领 → 把它和读数一起写到那台设备的格子上
   *   已经认领了、选的还是同一台 → 什么都不做
   *   已经认领了、改选另一台 → 走 move:读数、照片、置信度整组搬过去
   *
   * 【那台设备的格子已经占着东西 → 两行对调,不是把它顶掉】
   *
   * 已经抄到数的设备在下拉里是灰的(见 takenAssets),所以能走到这里的
   * 是"那一格绑了照片、但还没抄到数"——这时仍然不能直接顶掉它:
   * 那张照片会失去归属,而界面上没有任何地方说过。整组对调才不丢东西。
   */
  //
  // 【找格子不能只靠"哪一格默认绑的是这台"】原来就是这么找的,而默认设备是
  // 按"格子名去掉读数二字 == 设备名"算出来的 —— 台账里的表叫「Z1」而不是
  // 「Z1能耗表」时,没有一格默认绑着它,于是点什么都提示"没有对应的格子",
  // 只有名字恰好一字不差的「生活水表」能选。线上就是这样。
  //
  // 现在按这个顺序找:
  //   1. 已经有一格绑着这台 → 就是它
  //   2. 这张照片已经在某一格上,而这台表是那格能选的 → 就在原格上换设备
  //   3. 找一格同类型、还空着的(没绑设备、也没读数)
  async function pickAssetForPhoto(imageId: string, assetName: string) {
    if (!rec) return;
    const cur = fieldOfPhoto(rec.fields, imageId);
    let target = rec.fields.find((f) => f.assetName === assetName);
    // 这一格是"现在归这台表的那一格",还是"随便找的一个空位"。
    // 两者后面要走的路不一样:前者可能要和人对调,后者直接占用就行。
    let targetWasFree = false;

    try {
      if (!target && cur && (cur.assetOptions || []).includes(assetName)) {
        await patchFieldAsset(rec.id, cur.code, assetName, cur.version);
        setRec(await getRecord(rec.id));
        return;
      }
      if (!target) {
        // 【空位 = 没设备、没读数;照片不算数】
        //
        // 照片本来也在判据里,结果把「清除」堵死了:清除只放掉设备,那一格还挂着
        // 原来那张照片,于是它被算成占用 —— 人明明刚腾出一格,再选却弹
        // "这类设备的 4 个位置都已经用了"。2026-09-22 实测到的。
        //
        // 照片不该挡路:一格没设备时,它那一行本来就显示「选一台设备」;
        // 把这一格改派给另一张照片,只是换了哪一张照片没着落,界面上看着一样。
        // 【但有读数的绝不能碰】那是人抄下来的数,顶掉就没了。
        const free = rec.fields.filter(
          (f) =>
            (f.assetOptions || []).includes(assetName) &&
            !f.assetName &&
            String(f.value || "").trim() === "",
        );
        // 真正空着的(连照片都没有)优先 —— 不动别人那张图。
        target = free.find((f) => !f.sourceImageId) || free[0];
        targetWasFree = Boolean(target);
      }
      if (!target) {
        // 【说清是哪种满了】同类型的格子数是模板定的(比如只有 4 个电表位),
        // 台账里的表比格子多时,多出来的那台这张表记不下 —— 不说的话人会以为是系统坏了。
        const sameType = rec.fields.filter((f) => (f.assetOptions || []).includes(assetName)).length;
        Toast.show({
          content: `这类设备的 ${sameType} 个位置都已经用了 —— 先把别的行换掉,或者这张表记不下这台`,
          duration: 3000,
        });
        return;
      }
      if (cur && cur.code === target.code) return;

      // 【随便找来的空位直接占用,不走对调】那一格没设备、没读数,
      // 只是可能还挂着别人那张照片。对调的意义是"两台表换个位置",
      // 而这里根本没有另一台表 —— 走对调只会把一格空的和一格满的绕一圈。
      //
      // 目标格是"现在归这台表的那一格"、而且占着东西时,才是要对调的那种:
      // AI 把归属排错了,人要把两行换过来。
      if (!targetWasFree && slotInUse(target)) {
        if (cur) {
          // 两边都有东西:一次请求整组对调。
          // 【不能用两次 move 凑】move 遇到"目标格有读数"会直接拒绝。
          setRec(await swapReadings(rec.id, cur.code, target.code));
        } else {
          // 这张照片还没有格子,没法跟目标格互换 —— 先把目标格里那一组
          // 挪到一个空格寄存,腾出来再把这张照片放进去。
          const free = rec.fields.find(
            (f) =>
              f.code !== target!.code &&
              (f.assetOptions || []).includes(target!.assetName || assetName) &&
              !slotInUse(f),
          );
          if (!free) {
            Toast.show({
              content: `「${assetName}」那一行有读数,而且没有空位可以腾 —— 先把它换到别的表上`,
              duration: 3000,
            });
            return;
          }
          await moveReading(rec.id, target.code, free.code);
          const after = await getRecord(rec.id);
          const t = after.fields.find((f) => f.code === target!.code);
          setRec(
            t && t.assetName === assetName
              ? await patchFieldSource(rec.id, target.code, imageId, t?.version ?? 0)
              : await patchFieldAssetSource(
                  rec.id, target.code, assetName, imageId, t?.version ?? 0,
                ),
          );
        }
      } else if (!cur) {
        // 照片还没人认领,目标格也空着:照片和设备一起挂上去(读数由人接着填)
        setRec(
          target.assetName === assetName
            ? await patchFieldSource(rec.id, target.code, imageId, target.version)
            : await patchFieldAssetSource(rec.id, target.code, assetName, imageId, target.version),
        );
      } else {
        // 照片在别的格上、目标格空着:读数、照片整组搬过去,再标上是哪台表
        let next = await moveReading(rec.id, cur.code, target.code);
        const moved = next.fields.find((f) => f.code === target!.code);
        if (moved && moved.assetName !== assetName) {
          await patchFieldAsset(rec.id, moved.code, assetName, moved.version);
          next = await getRecord(rec.id);
        }
        setRec(next);
      }
    } catch (err) {
      Toast.show({
        content: err instanceof Error ? err.message : "改不了,请重试",
        duration: 3000,
      });
    }
  }

  async function saveFieldValue(f: FieldValue, v: string) {
    if (!rec) return;
    try {
      mergeField(await patchField(rec.id, f.code, v, f.version, { action: "correct" }));
    } catch (err) {
      Toast.show({ content: err instanceof Error ? err.message : "保存失败" });
    }
  }

  async function confirmAll() {
    if (!rec || confirming) return;
    setConfirming(true);
    try {
      for (const f of lowConf) {
        const updated = await patchField(rec.id, f.code, f.value, f.version, {
          action: "confirm",
        });
        mergeField(updated);
      }
      Toast.show({
        content: `已确认 ${lowConf.length} 项`,
        position: "bottom",
      });
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
    return <CenterLoading />;
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
            <div className="retake-tries">
              已尝试 {rec.captureAttempts ?? 0} / 3 次
            </div>
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
            <div className="fld-group-title">
              巡检照片({rec.images.length} 张)
              {/* 【差几张就写在标题上】不够的时候这一行是红的,
                  比等到点提交才被打回来早了整整一张表。 */}
              {shortOf > 0 && (
                <span style={{ color: "var(--ios-red)", marginLeft: 8, fontWeight: 400 }}>
                  还差 {shortOf} 张
                </span>
              )}
            </div>
            <div className="photo-strip">
              {photos.map((p, i) => (
                <button
                  className="photo-thumb"
                  key={p.url}
                  onClick={() => setViewing(i)}
                >
                  {/* 用组件库的 Image:自带加载占位、失败兜底和自动重试。
                      手写 <img> 在现场信号差时会白一片或直接裂图。 */}
                  <Image src={p.url} radius={12} />
                </button>
              ))}
            </div>
          </>
        )}

        {/* 【照片在前,巡检人/地点跟在后面】这两条回答的是"这份表是谁、在哪填的",
            是语境,不是要逐项核对的检查项 —— 所以既不能混在读数中间(往下核表时
            每次都要跳过它们),也不该占掉首屏最上面那块。

            现场打开这一页,第一眼要确认的是"我拍的这几张对不对、够不够";
            照片先亮出来,人和地点紧跟着做落款,再往下才是一行一张的核对。

            巡检地点仍然是个可改的字段(系统按项目预填,人能改);
            巡检人不是字段,是记录本身带的,所以只读。 */}
        <div className="rec-who">
          {/* 【按后台设的类型显示,不写死成文本框】原来这里固定是一个输入框,
              后台把巡检地点改成"选一个"、设了选项和必填,手机上一样都不生效 ——
              改模板的人以为没保存上,反复改。 */}
          {siteField && (
            <div className="rec-who-row">
              <span className="rec-who-k">
                巡检地点
                {siteField.required && <i className="fld-req"> *</i>}
              </span>
              {siteField.kind === "choice" && (siteField.options || []).length > 0 ? (
                <Picker
                  options={siteField.options || []}
                  value={siteDraft}
                  placeholder="请选择"
                  onChange={(v) => {
                    setSiteDraft(v);
                    void commitSite(v);
                  }}
                />
              ) : (
                <input
                  className="rec-who-v"
                  value={siteDraft}
                  placeholder="请输入"
                  onChange={(e) => setSiteDraft(e.target.value)}
                  onBlur={() => void commitSite()}
                />
              )}
            </div>
          )}
          <div className="rec-who-row">
            <span className="rec-who-k">巡检人</span>
            <span className="rec-who-v is-ro">{rec.inspector || "—"}</span>
          </div>
        </div>

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

        {/* ===== 抄表:一张照片一行,照片摆在行下面 =====
            只给【读数配了设备类型】的模板走这条(现在是紫菡能耗)。
            电梯巡检那种 17 个字段配 5 张照片,一张照片对不上一行,硬套会很怪。 */}
        {meterMode && (
          <>
            <div className="fld-group-title">按拍照顺序核对</div>
            {rec.images.map((img, i) => {
              const f = fieldOfPhoto(rec.fields, img.id);
              return (
                <MeterPhotoRow
                  key={img.id}
                  index={i + 1}
                  photoUrl={photos[i]?.url || ""}
                  field={f}
                  options={allAssets()}
                  disabledAssets={takenAssets(f)}
                  assetName={f?.assetName || ""}
                  onPickAsset={(name) => pickAssetForPhoto(img.id, name)}
                  onClearAsset={f ? () => clearAssetForField(f) : undefined}
                  onChangeValue={(v) => (f ? saveFieldValue(f, v) : Promise.resolve())}
                  onOpenPhoto={() => setViewing(i)}
                />
              );
            })}
            <div className="fld-group-title">其余项</div>
          </>
        )}

        {/* 「日报字段」标题删了:整页只有这一组字段,标题不起区分作用 */}
        <div className="fld-group">
          {/* 巡检地点已经提到最上面了,这里不再出现第二遍 ——
              同一个字段在一屏里出现两处,改了一处另一处不动,人会以为没存上。 */}
          {rec.fields
            .filter((f) => f.code !== "site")
            .filter((f) => !meterMode || !isMeterField(f))
            .map((f) => {
            // 读数来自哪张照片 —— 按 id 找,不按下标:照片能补拍、能删,
            // 下标会在删掉一张之后指向另一张图,而界面上看不出指错了。
            const srcIdx = f.sourceImageId
              ? rec.images.findIndex((img) => img.id === f.sourceImageId)
              : -1;
            return (
              <FieldRow
                key={f.code}
                field={f}
                recordId={rec.id}
                onSaved={mergeField}
                cropUrl={srcIdx >= 0 ? photos[srcIdx]?.url : undefined}
                onOpenPhoto={srcIdx >= 0 ? () => setViewing(srcIdx) : undefined}
              />
            );
          })}
        </div>
      </div>

      <PhotoViewer
        photos={photos}
        index={viewing}
        onClose={() => setViewing(-1)}
      />

      <div className="flow-foot">
        {/* 【差张数时把话说在按钮上方,而不是点完弹一下】
            弹窗一闪就没了,而这是个要走回设备旁边补拍的动作 ——
            提示必须一直在,直到他真的补够。 */}
        {shortOf > 0 && (
          <div className="foot-warn">
            这类巡检至少要 {minImages} 张照片,还差 {shortOf} 张 —— 请回拍照页补拍
          </div>
        )}
        <Button block className="btn-primary" onClick={toPreview}>
          保存并预览日报
        </Button>
      </div>
    </div>
  );
}
