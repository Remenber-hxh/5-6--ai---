package main

import "strings"

// ===== 一条记录里需要人看一眼的那几项 =====
//
// 2026-10-08:群里收到"智巡异常提醒 · 待复核",点进巡检记录 ——
// 详情里只有一段总结、六张照片和一张字段表,表上每一行长得一样,
// 看不出是哪一项让它待复核。真正存疑的是生活水表那一格(2119,比上一次大 20 倍),
// 理由明明写在字段的 reason 里,只是哪个界面都没把它摆出来。
//
// 【判据只有一份,推送和后台详情共用】在记录出站的收口处算好
// (sanitizeRecordForCurrentTemplate,和 BusinessStatus 一起),
// 推送卡片的"问题"一行、后台详情的"需要注意"都读这一份 ——
// 两边各判一遍的话,迟早出现"群里说有问题、点进去说一切正常"。

// RecordAttention 需要注意的一项。
type RecordAttention struct {
	Code  string `json:"code"`
	Label string `json:"label"`
	// Kind 异常(值里就写着异常/故障…)/ 待复核(读数存疑、AI 没把握)—— 和业务状态同一套词
	Kind string `json:"kind"`
	// Asset 读数格实际挂的那台设备 —— 只在和格子名对不上时给("生活水表读数"格里记的是消防水表)
	Asset  string `json:"asset,omitempty"`
	Value  string `json:"value,omitempty"`
	Reason string `json:"reason,omitempty"` // 给人看的那一句,已经从长理由里摘出来
}

// recordAttentionItems 判据和设备状态同一套:hasAbnormalSignal 的字段那一半,
// 加上值里出现异常词(record_status.go 的 abnormalValueRe)。
func recordAttentionItems(rec *Record) []RecordAttention {
	if rec == nil {
		return nil
	}
	var out []RecordAttention
	for i := range rec.Fields {
		f := &rec.Fields[i]
		v := strings.TrimSpace(f.Value)
		if !(v != "" && abnormalValueRe.MatchString(v)) && !hasAbnormalSignal(f, nil, "") {
			continue
		}
		item := RecordAttention{
			Code: f.Code, Label: firstNonEmpty(f.Label, f.Code), Value: v, Kind: "待复核",
			Reason: shortAnomalyReason(f.Reason),
		}
		if v != "" && abnormalValueRe.MatchString(v) {
			item.Kind = "异常"
		}
		if name := strings.TrimSpace(f.AssetName); name != "" &&
			!strings.HasPrefix(normalizeAssetKey(item.Label), normalizeAssetKey(name)) {
			item.Asset = name
		}
		if item.Reason == "" && f.NeedsReview {
			item.Reason = "AI 没把握,需要对照照片确认"
		}
		out = append(out, item)
	}
	return out
}

// attentionFieldLines 推送卡片"问题"一行用的短句:"生活水表读数(消防水表) 107:比上一次…"。
func attentionFieldLines(rec *Record, max int) []string {
	var out []string
	for _, it := range recordAttentionItems(rec) {
		line := it.Label
		if it.Asset != "" {
			line += "(" + it.Asset + ")"
		}
		if it.Value != "" {
			line += " " + it.Value
		}
		if it.Reason != "" {
			line += ":" + it.Reason
		}
		out = append(out, truncateRunes(line, 60))
		if len(out) >= max {
			break
		}
	}
	return out
}

// shortAnomalyReason 理由里最该让人看到的那一句。
//
// 有【读数存疑】就取它后面那段、到破折号为止("是上一次 107 的 20 倍,量级对不上");
// 否则只在理由里有异常词时给出(截短),正常的识别说明("放大复核一致")不往外带。
func shortAnomalyReason(reason string) string {
	r := strings.TrimSpace(reason)
	if i := strings.Index(r, sanityNoteMark); i >= 0 {
		r = r[i+len(sanityNoteMark):]
		if j := strings.Index(r, "——"); j > 0 {
			r = r[:j]
		}
		return strings.TrimRight(strings.TrimSpace(r), ",，;；")
	}
	if containsAnomalyKeyword(r) {
		return truncateRunes(r, 30)
	}
	return ""
}
