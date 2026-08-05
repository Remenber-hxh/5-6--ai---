import { Button, Input, Picker, Popup, Tag, Textarea, Toast } from "@/ui";
import { useEffect, useState } from "react";

import {
  createChangeRequest,
  getRecord,
  uploadDraftPhotos,
} from "@/api/inspection";

import type { AssetDTO, AssetSnapshotDTO, FieldValue } from "@/api/inspection";

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

/** 判断字段是不是"待复核"。与台账/AI 总结同一套是否语义(旧版 crfFieldIsBad) */
function fieldIsBad(f: FieldValue): boolean {
  const v = String(f.value || "").trim();
  if (!v) return false;
  if (["异常", "缺失", "破损", "故障"].includes(v)) return true;
  // "是/否"的好坏取决于问法:"…正常吗"答否是坏,"…有异响吗"答是才是坏。
  // 这里按标签里是否含"异常/异响/异味/漏/破损"这类词判断。
  const occurrence = /异响|异味|漏|破损|故障|缺失|超期|过期/.test(f.label);
  if (v === "否") return !occurrence;
  if (v === "是" || v === "有") return occurrence;
  return false;
}

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

  async function submit() {
    const [kind, id] = splitTarget(target);
    if (!id) return;
    if (!reason.trim()) {
      Toast.show({ content: "请填写修改理由" });
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

        {fields === null ? (
          splitTarget(target)[0] === "record" ? (
            <div className="cr-hint">正在取这次巡检的字段…</div>
          ) : (
            <div className="cr-hint">
              直接改台账状态不留巡检依据,建议优先选一次巡检记录来改。
            </div>
          )
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
          提交申请{changed.length ? ` · 改了 ${changed.length} 项` : ""}
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
