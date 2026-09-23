import { Picker } from "@/ui";
import { useEffect, useRef, useState } from "react";

import ReadingCrop from "@/components/ReadingCrop";
import type { FieldValue } from "@/api/inspection";

// ===== 抄表:一张照片 = 一行 =====
//
// 【为什么要按拍照顺序排】现场是一块表一块表挨个拍的。原来的确认页是
// 固定六行 Z1~Z4 + 两个水表,照片挤在顶上一条 —— 人要核对,得凭记忆
// 把六张照片和六行对起来:点开大图、翻页、退回来、再点下一个。
// 线上实测三千多个字段里人改过 AI 的值 0 次:不是 AI 准,是这个动作太贵。
//
// 【顺手解决了表号对不上】照片上根本没有 Z1~Z4 的字,AI 只能按上传顺序猜,
// 中间夹一张读不出的就整体错位一格。现在人是【看着照片选设备】的:
// 这块表贴着标、看得见是 Z1,就选 Z1,读数自动落到 Z1 那一栏。AI 不用猜了。

export interface MeterPhotoRowProps {
  /** 第几张(从 1 开始),给人一个和照片条对得上的锚 */
  index: number;
  photoUrl: string;
  /** 这张照片当前归哪一格读数;没人认领就是 null */
  field: FieldValue | null;
  /** 全部候选设备 */
  options: string[];
  /** 现在选不了的,以及为什么(一台设备只能归一行) */
  disabledAssets?: Record<string, string>;
  /** 当前这行选中的设备名 */
  assetName: string;
  onPickAsset: (assetName: string) => Promise<void> | void;
  /** 放掉这一行选的设备,好让它在别的行里重新可选 */
  onClearAsset?: () => Promise<void> | void;
  onChangeValue: (v: string) => Promise<void> | void;
  onOpenPhoto: () => void;
}

export default function MeterPhotoRow({
  index,
  photoUrl,
  field,
  options,
  disabledAssets,
  assetName,
  onPickAsset,
  onClearAsset,
  onChangeValue,
  onOpenPhoto,
}: MeterPhotoRowProps) {
  const [value, setValue] = useState(field?.value || "");
  const [busy, setBusy] = useState(false);
  // 停留时长交给外层统计,这里只管别在提交中途被覆盖
  const committed = useRef(field?.value || "");

  useEffect(() => {
    setValue(field?.value || "");
    committed.current = field?.value || "";
  }, [field?.value]);

  async function commit() {
    if (!field || value === committed.current) return;
    committed.current = value;
    await onChangeValue(value);
  }

  const unread = Boolean(field && !field.value && field.reason);
  // 【有读数但存疑,也要把话说出来】原来这一行只在"没读到数"时才显示理由,
  // 而读数合理性检查(reading_sanity.go)的场景恰恰是【有数、但这个数不对劲】:
  // "比上一次的 116748.24 还小""是上一次的 14 倍,小数点丢了"。
  // 那是唯一能当场发现归属错位/小数点错的线索,却一个字都没出现在这一屏 ——
  // 人在这里点提交,不会先跑去台账看。
  const doubt = Boolean(field && field.value && field.needsReview && field.reason);
  const needsReview = Boolean(field?.needsReview);

  return (
    <div
      className={`mpr ${needsReview ? "mpr-warn" : ""} ${assetName ? "" : "mpr-todo"}`}
    >
      <div className="mpr-head">
        {/* 【照片就是这一行的左栏,固定大小】原来照片单独挂在行下面,还缩进一截:
            六行连着核时,有读数区特写的和没有的两种图大小不一样,位置也对不齐,
            看着像每一行都在挪位置。放进行首、写死一个尺寸,一列对齐到底。
            原来左边还占着一个「第 N 张」,一屏就少看一行。 */}
        <span className="mpr-thumb">
          {field?.bbox?.length === 4 ? (
            <ReadingCrop url={photoUrl} bbox={field.bbox} onOpen={onOpenPhoto} />
          ) : (
            <button className="mpr-photo" onClick={onOpenPhoto} aria-label={`看第 ${index} 张大图`}>
              <img src={photoUrl} alt="" loading="lazy" />
            </button>
          )}
        </span>
        {/* 【设备选择器就是这一行的身份】选完读数落到哪一栏由它决定。
            没选之前读数框是禁用的 —— 不知道是哪台表,填了也不知道记到哪。 */}
        <Picker
          options={options}
          disabledOptions={disabledAssets}
          value={assetName}
          placeholder="选一台设备"
          onChange={(v) => {
            if (busy) return;
            setBusy(true);
            void Promise.resolve(onPickAsset(v)).finally(() => setBusy(false));
          }}
          onClear={
            onClearAsset
              ? () => {
                  if (busy) return;
                  setBusy(true);
                  void Promise.resolve(onClearAsset()).finally(() => setBusy(false));
                }
              : undefined
          }
        />
        <input
          className="mpr-input"
          type="number"
          inputMode="decimal"
          value={value}
          disabled={!assetName || busy}
          placeholder={assetName ? "读数" : "先选设备"}
          onChange={(e) => setValue(e.target.value)}
          onBlur={() => void commit()}
        />
      </div>

      {/* 【读不出来要说出来,不能只留一个空格子】AI 试过、放弃了,
          理由写在这儿,人才知道该自己看照片填,而不是以为系统没跑。 */}
      {(unread || doubt) && (
        <div className={doubt ? "mpr-note is-doubt" : "mpr-note"}>
          {field?.reason}
        </div>
      )}

    </div>
  );
}

/** 这张照片被哪一格读数认领了 */
export function fieldOfPhoto(fields: FieldValue[], imageId: string): FieldValue | null {
  return fields.find((f) => f.sourceImageId === imageId) || null;
}

