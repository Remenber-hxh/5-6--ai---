package main

import (
	"fmt"
	"strings"
)

// ===== 这台设备为什么是「待复核」 =====
//
// 【缺的是什么】台账和详情页只说状态,不说为什么。现场看到「待复核」却
// 完全不知道要复核什么 —— 读数看着挺正常,照片也拍了,人只能猜。
// 2026-09-22 现场原话:"这个待复核的原因到底是什么,我看不出来"。
//
// 触发「待复核」的其实有四种,而页面只在其中两种时给了线索:
//   1. 读数为空            → 摘要里说了
//   2. 理由含异常关键词     → 摘要里追加了 "AI 提示:…"
//   3. NeedsReview(置信低) → 【什么都不说】
//   4. 总结失败 / 高优先级建议 → 【什么都不说】
// 后两种恰恰是最常见的,于是"待复核"在现场就成了一个没有下文的标签。
//
// 【为什么不落库】原因完全由"那条记录 + 那个字段"决定,和状态同源。
// 存一列就多一份会过期的副本:记录改了、AI 重跑了,列里还是旧话。
// 这里每次显示时现算 —— enrichAssetForDisplay 本来就已经把记录取出来了。

// assetStatusReason 一句话说清为什么要人来看一眼。返回空 = 没什么要说的。
//
// field 是这台设备在那条记录里对应的那一格(可能为 nil:老记录、或者
// 这台设备是按名字挂上去的)。
func assetStatusReason(a *AssetEntry, rec *Record, field *FieldValue) string {
	if a == nil {
		return ""
	}
	// 【只在"要人跟进"的状态下给理由】正常设备挂一句解释是噪音。
	switch firstNonEmpty(a.StatusLevel, statusLevel(a.LastStatus)) {
	case "warning", "danger", "repair":
	default:
		return ""
	}

	// 顺序就是判定顺序(见 readingAssetStatus / hasAbnormalSignal),
	// 先命中的那条才是真正让它变成这个状态的原因。
	if field != nil {
		if strings.TrimSpace(field.Value) == "" {
			return "这一格没有取到读数,需要人工补填"
		}
		if r := strings.TrimSpace(field.Reason); r != "" && containsAnomalyKeyword(r) {
			return "AI 提示:" + r
		}
		if field.NeedsReview {
			// 【把置信度说出来】"AI 不太确定"这种话没法行动;
			// 给个数字,人才知道是"扫一眼确认"还是"必须对着表重核"。
			if field.Confidence > 0 {
				return fmt.Sprintf("AI 识别把握不大(%.0f%%),这个数要人对着表核一遍",
					field.Confidence*100)
			}
			return "AI 标了需要人工复核,这个数要对着表核一遍"
		}
	}

	if rec != nil {
		if strings.TrimSpace(rec.AISummaryError) != "" {
			return "AI 这次没能生成总结,状态没法自动判定,请人工看一眼"
		}
		for _, r := range rec.AIRecommendations {
			text := strings.TrimSpace(r.Text)
			if text == "" || !strings.EqualFold(r.Priority, "high") {
				continue
			}
			// assetName 为空时 hasAbnormalSignal 会认所有高优先级建议,这里照同一口径
			if a.AssetName == "" || strings.Contains(text, a.AssetName) {
				return "AI 重点提示:" + truncate(text, 60)
			}
		}
	}

	// 【兜底也要说点什么】走到这儿说明状态是别处定的(比如人在后台直接改的、
	// 或者派了复检任务)。空着会让人以为系统忘了给理由。
	if a.LastStatus == "待维修" {
		return "已标记为待维修,等维修完成后更新状态"
	}
	return "这台设备被标成需要跟进,原因没有随记录带过来 —— 请查看巡检记录"
}

// fieldForAsset 这台设备在这条记录里对应的那一格。
//
// 【先按 assetName 找,再按字段名回落】抄表记录里每一格都写明了 assetName;
// 老记录没有那一位,只能按"字段名去掉读数二字 == 设备名"回落 ——
// 和 defaultAssetForField 同一个口径,两处分叉就会出现"详情页说没读数、
// 记录里明明有"。
func fieldForAsset(rec *Record, assetName string) *FieldValue {
	if rec == nil || strings.TrimSpace(assetName) == "" {
		return nil
	}
	want := strings.TrimSpace(assetName)
	for i := range rec.Fields {
		if strings.TrimSpace(rec.Fields[i].AssetName) == want {
			return &rec.Fields[i]
		}
	}
	key := normalizeAssetKey(want)
	for i := range rec.Fields {
		if normalizeAssetKey(rec.Fields[i].Label) == key {
			return &rec.Fields[i]
		}
	}
	return nil
}
