import { Button, Input, Picker, Popup, Tag, Textarea, Toast } from "@/ui";
import { useEffect, useState } from "react";

import {
  createChangeRequest,
  getRecord,
  uploadDraftPhotos,
} from "@/api/inspection";

import type { AssetDTO, AssetSnapshotDTO, FieldValue } from "@/api/inspection";
import { fieldIsBad } from "@/lib/fieldStatus";

// ===== 申请修改 =====
//
// 复刻旧版 openChangeRequestSheet(app.js:1930)。新版一直只有【审批端】——
// 主管能批能驳,可巡检员根本发不出申请,这个闭环是断的:现场发现上一次填错
// 了(或 AI 认错了),除了找主管口头说,没有任何在系统里留痕的办法。
//
// 【为什么默认目标是"巡检记录"而不是"资产"】
// 申请修改的本质是纠正这一次巡检填错/认错的字段。改记录会连带把台账重算,
// 留下的是"哪次巡检的哪个字段被谁改成什么、谁批的";而直接改资产台账只是
// 主管改了个状态,没有巡检依据。所以记录排在前面、默认选中,资产降级放最后。
// 旧版同一口径。
//
// 【为什么理由做成可点的片】
// 戴着手套在机房里打字很难受。四个常见理由点一下就填进去,想补充再手改 ——
// 能点就别让人打。旧版 renderReasonChips 的做法。

const REASONS = ["AI 识别有误", "现场已整改", "补拍补录", "误判,实际正常"];

// 资产台账能改的三样。【必须和后端 applyChangeRequestSQL 的 asset 分支对齐】——
// 那里只认 assetName / lastStatus / lastSummary 三个键,发别的键会"提交成功、
// 审批时报 asset patch 为空"。选项取自后台台账编辑表单,两端保持一致。
const ASSET_STATUS = ["正常", "异常", "待复核", "待维修"];


export interface ChangeRequestSheetProps {
  visible: boolean;
  onClose: () => void;
  asset: AssetDTO;
  /** 该设备的巡检历史,用来选"改哪一次" */
  history: AssetSnapshotDTO[];
  /** 提交成功后回调(外层刷新详情) */
  onSubmitted?: () => void;
}

export default function ChangeRequestSheet({
  visible,
  onClose,
  asset,
  history,
  onSubmitted,
}: ChangeRequestSheetProps) {
  // 目标:record::<id> 或 asset::<id>。资产 id 自己就含 "::",所以取值时
  // 只能切第一个分隔符(旧版 splitTargetSel 栽过这个坑)。
  const targets = [
    ...history.map((h) => ({
      value: `record::${h.recordId}`,
      label: `巡检:${(h.createdAt || "").slice(0, 16).replace("T", " ")} · ${h.inspector || ""}`,
    })),
    { value: `asset::${asset.id}`, label: `资产台账:${asset.assetName}` },
  ];
  const [target, setTarget] = useState(targets[0]?.value || "");
  const [fields, setFields] = useState<FieldValue[] | null>(null);
  const [edits, setEdits] = useState<Record<string, string>>({});
  /** 选中"资产台账"时改的三项(和记录的字段编辑是两套,键名也不同) */
  const [assetEdits, setAssetEdits] = useState<Record<string, string>>({});
  const [reason, setReason] = useState("");
  const [photos, setPhotos] = useState<File[]>([]);
  const [busy, setBusy] = useState(false);

  // 打开时重置,免得上次填了一半的内容串到这次
  useEffect(() => {
    if (!visible) return;
    setTarget(targets[0]?.value || "");
    setReason("");
    setPhotos([]);
    setEdits({});
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible]);

  // 选中某次巡检 → 取那条记录的字段来编辑
  useEffect(() => {
    const [kind, id] = splitTarget(target);
    if (!visible || kind !== "record" || !id) {
      setFields(null);
      return;
    }
    let stop = false;
    setFields(null);
    void getRecord(id)
      .then((rec) => {
        if (!stop) setFields(rec.fields);
      })
      .catch(() => {
        if (!stop) Toast.show({ content: "这次巡检的字段取不到" });
      });
    return () => {
      stop = true;
    };
  }, [target, visible]);

  const valueOf = (f: FieldValue) => edits[f.code] ?? f.value ?? "";
  const changed = (fields || []).filter((f) => valueOf(f) !== (f.value || ""));

  // 资产那三项里真正被改动过的。原值取自当前台账,没动的不提交 ——
  // 审批的人一眼就该看出动了什么。
  const assetPatch = (): Record<string, unknown> => {
    const out: Record<string, unknown> = {};
    const base: Record<string, string> = {
      assetName: asset.assetName || "",
      lastStatus: asset.lastStatus || "",
      lastSummary: asset.lastSummary || "",
    };
    for (const k of Object.keys(base)) {
      const v = (assetEdits[k] ?? base[k]).trim();
      if (v && v !== base[k]) out[k] = v;
    }
    return out;
  };

  async function submit() {
    const [kind, id] = splitTarget(target);
    if (!id) return;
    if (!reason.trim()) {
      Toast.show({ content: "请填写修改理由" });
      return;
    }

    // 【两种目标的 patch 形状完全不同】
    // 记录:{ fields: [...] }(+ addImages)
    // 资产:{ assetName?, lastStatus?, lastSummary? }
    // 以前不分,资产也发 fields —— 后端创建时只校验"patch 非空",所以提交
    // 成功;等主管点通过才炸一句"asset patch 为空",而申请已经躺在待办里了。
    if (kind === "asset") {
      const ap = assetPatch();
      if (!Object.keys(ap).length) {
        Toast.show({ content: "没有改动任何一项" });
        return;
      }
      setBusy(true);
      try {
        await createChangeRequest({
          targetType: "asset",
          targetId: id,
          patch: ap,
          reason: reason.trim(),
        });
        Toast.show({ content: "已提交,等待主管审批", position: "bottom" });
        onClose();
        onSubmitted?.();
      } catch (err) {
        Toast.show({
          content: err instanceof Error ? err.message : "提交失败",
          duration: 3500,
        });
      } finally {
        setBusy(false);
      }
      return;
    }

    if (!changed.length && !photos.length) {
      Toast.show({ content: "没有任何修改" });
      return;
    }
    setBusy(true);
    try {
      const patch: Record<string, unknown> = {};
      // 只提交改动过的字段 —— 全量提交的话审批的人看不出动了什么,
      // 得自己逐项比对
      if (changed.length) {
        patch.fields = changed.map((f) => ({
          code: f.code,
          label: f.label,
          value: valueOf(f),
        }));
      }
      if (photos.length) {
        const up = await uploadDraftPhotos(photos);
        patch.addImages = { tmpDir: up.tmpDir, imageIds: up.imageIds };
      }
      await createChangeRequest({
        targetType: kind as "record" | "asset",
        targetId: id,
        patch,
        reason: reason.trim(),
      });
      Toast.show({ content: "已提交,等待主管审批", position: "bottom" });
      onClose();
      onSubmitted?.();
    } catch (err) {
      // 失败原因必须让人看见:后端会因"已有在途申请""无权修改"等明确拒绝,
      // 吞掉就变成"点了没反应"
      Toast.show({
        content: err instanceof Error ? err.message : "提交失败",
        duration: 3500,
      });
    } finally {
      setBusy(false);
    }
  }

  const isAsset = splitTarget(target)[0] === "asset";
  const bad = (fields || []).filter(fieldIsBad);
  const rest = (fields || []).filter((f) => !fieldIsBad(f));

  return (
    <Popup visible={visible} onClose={onClose} className="cr-sheet">
      <div className="cr-head">
        <span className="cr-title">申请修改</span>
        <button className="cr-close" onClick={onClose} aria-label="关闭">
          ×
        </button>
      </div>

      <div className="cr-body">
        <div className="cr-label">修改对象</div>
        <Picker
          options={targets.map((t) => t.label)}
          value={targets.find((t) => t.value === target)?.label || ""}
          onChange={(label) => {
            const hit = targets.find((t) => t.label === label);
            if (hit) setTarget(hit.value);
          }}
        />

        {isAsset ? (
          /* 资产台账:能改的就是后端认的这三项。
             以前这里只有一句"建议优先选巡检记录"的劝退提示,没有任何可改的
             东西 —— 选了资产的人只能靠补张照片才提交得出去,而后端的 asset
             分支根本不看照片,于是审批时必炸。 */
          <>
            <div className="cr-hint">
              改台账不留巡检依据。只有"这次巡检没问题、是台账状态记错了"才用它,
              否则请选一次巡检记录来改。
            </div>

            <div className="cr-sec">设备状态</div>
            <div className="cr-chips">
              {ASSET_STATUS.map((st) => {
                const cur = assetEdits.lastStatus ?? asset.lastStatus ?? "";
                return (
                  <Tag
                    key={st}
                    type={cur === st ? "primary" : "hollow"}
                    onClick={() =>
                      setAssetEdits((c) => ({ ...c, lastStatus: st }))
                    }
                  >
                    {st}
                  </Tag>
                );
              })}
            </div>

            <div className="cr-sec">设备名称</div>
            <Input
              value={assetEdits.assetName ?? asset.assetName ?? ""}
              onChange={(v) => setAssetEdits((c) => ({ ...c, assetName: v }))}
              placeholder="设备名称"
            />

            <div className="cr-sec">状态说明</div>
            <Textarea
              value={assetEdits.lastSummary ?? asset.lastSummary ?? ""}
              onChange={(v) => setAssetEdits((c) => ({ ...c, lastSummary: v }))}
              placeholder="台账上显示的那句说明"
              maxLength={200}
            />
          </>
        ) : fields === null ? (
          <div className="cr-hint">正在取这次巡检的字段…</div>
        ) : (
          <>
            {bad.length > 0 && (
              <>
                <div className="cr-sec warn">需复核 · {bad.length} 项</div>
                {bad.map((f) => (
                  <FieldEditor
                    key={f.code}
                    field={f}
                    value={valueOf(f)}
                    onChange={(v) => setEdits((c) => ({ ...c, [f.code]: v }))}
                  />
                ))}
              </>
            )}
            {rest.length > 0 && (
              <>
                <div className="cr-sec">其余字段</div>
                {rest.map((f) => (
                  <FieldEditor
                    key={f.code}
                    field={f}
                    value={valueOf(f)}
                    onChange={(v) => setEdits((c) => ({ ...c, [f.code]: v }))}
                  />
                ))}
              </>
            )}
          </>
        )}

        {/* 【资产目标下不显示补交照片】后端的 asset 分支完全不看 addImages ——
            放着只会让人以为传上去了。照片属于某一次巡检,不属于台账。 */}
        {!isAsset && (
          <>
        <div className="cr-label">补交照片</div>
        <label className="cr-photo">
          <input
            type="file"
            accept="image/*"
            capture="environment"
            multiple
            onChange={(e) => setPhotos(Array.from(e.target.files || []))}
          />
          <span>
            {photos.length ? `已选 ${photos.length} 张` : "拍照或从相册选"}
          </span>
        </label>
        <div className="cr-note">
          审批通过后才并入这次巡检 —— 没通过的照片不会进证据链
        </div>
          </>
        )}

        <div className="cr-label">修改理由</div>
        <div className="cr-chips">
          {REASONS.map((r) => (
            <Tag
              key={r}
              type={reason === r ? "primary" : "hollow"}
              onClick={() => setReason(r)}
            >
              {r}
            </Tag>
          ))}
        </div>
        <Textarea
          value={reason}
          onChange={setReason}
          placeholder="说明为什么要改,审批的人只看得到这一句"
          maxLength={120}
        />
      </div>

      <div className="cr-foot">
        <Button
          block
          className="btn-primary"
          loading={busy}
          onClick={() => void submit()}
        >
          提交申请
          {(isAsset ? Object.keys(assetPatch()).length : changed.length) > 0
            ? ` · 改了 ${isAsset ? Object.keys(assetPatch()).length : changed.length} 项`
            : ""}
        </Button>
      </div>
    </Popup>
  );
}

/** 资产 id 自己含 "::",只能切第一个分隔符 */
function splitTarget(sel: string): [string, string] {
  const i = (sel || "").indexOf("::");
  return i < 0 ? ["", ""] : [sel.slice(0, i), sel.slice(i + 2)];
}

/** 每个字段用与填报页同款的控件:有选项→下拉,其余→输入框 */
function FieldEditor({
  field,
  value,
  onChange,
}: {
  field: FieldValue;
  value: string;
  onChange: (v: string) => void;
}) {
  const dirty = value !== (field.value || "");
  return (
    <div className={dirty ? "cr-field dirty" : "cr-field"}>
      <span className="cr-field-label">{field.label}</span>
      {field.options?.length ? (
        <Picker options={field.options} value={value} onChange={onChange} />
      ) : (
        <Input value={value} onChange={onChange} placeholder="未填写" />
      )}
    </div>
  );
}
