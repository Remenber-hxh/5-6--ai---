package main

import (
	"strings"
	"testing"
	"time"
)

// ===== 异常提醒要说出是哪一项 =====
//
// 2026-10-08 线上:AI 总结服务没回来,六块表全成了"待复核",
// 群里收到"Z1、Z2、Z3、等 6 项 · 待复核" —— 真正存疑的生活水表(2119,
// 比上一次大 20 倍)哪儿都没写,点进记录也找不到。

func oct8EnergyRecord() *Record {
	rec := &Record{
		ID: "rec_1008", TenantID: defaultTenantID, Project: "紫菡雅集", TemplateID: "zihan_energy",
		PointName: "能耗抄表点位", Inspector: "苑文涛", Submitted: true,
		AISummaryError: "AI 总结降级：fallback-call-failed",
	}
	add := func(code, label, asset, value string) *FieldValue {
		rec.Fields = append(rec.Fields, FieldValue{Code: code, Label: label, AssetName: asset, Value: value, Source: "ai", Confidence: 0.92})
		return &rec.Fields[len(rec.Fields)-1]
	}
	add("z1_reading", "Z1 能耗表读数", "Z1", "206171.68")
	add("z2_reading", "Z2 能耗表读数", "Z2", "116969.15")
	add("z3_reading", "Z3 能耗表读数", "Z3", "86025.608")
	add("z4_reading", "Z4 能耗表读数", "Z4", "61873.044")
	add("living_water_reading", "生活水表读数", "生活水表", "2119")
	add("fire_water_reading", "消防水表读数", "消防水表", "108")
	w, _ := fieldByCode(rec.Fields, "living_water_reading")
	w.NeedsReview = true
	w.Reason = "图2黑色字轮H002115;" + sanityNoteMark + "是上一次 107 的 20 倍,量级对不上 —— 最常见的原因是小数点丢了"
	return rec
}

// AI 总结失败不能把每台设备都拖成"待复核"。
func TestSummaryFailureDoesNotFlagEveryMeter(t *testing.T) {
	rec := oct8EnergyRecord()
	var flagged []string
	for _, a := range buildZihanEnergyAssets(rec, time.Now()) {
		if a.LastStatus != "正常" {
			flagged = append(flagged, a.AssetName)
		}
	}
	if len(flagged) != 1 || flagged[0] != "生活水表" {
		t.Fatalf("只有生活水表存疑,却标了:%v", flagged)
	}
}

// 推送卡片:只点存疑的那台,并写出哪一格、什么读数、为什么。
func TestAlertCardNamesTheProblemItem(t *testing.T) {
	rec := oct8EnergyRecord()
	card := inspectionAlertCard(rec, buildZihanEnergyAssets(rec, time.Now()), "https://x/v2/#/record?focus=rec_1008")
	for _, want := range []string{"**生活水表**", "问题", "生活水表读数 2119", "是上一次 107 的 20 倍"} {
		if !strings.Contains(card, want) {
			t.Errorf("卡片里缺「%s」:\n%s", want, card)
		}
	}
	if strings.Contains(card, "等 6") || strings.Contains(card, "Z1") {
		t.Errorf("没问题的表不该出现在卡片里:\n%s", card)
	}
}

// 读数格挂的不是格子名上那台表时,问题一栏要写出实际的表。
func TestAlertLineShowsTheActualMeter(t *testing.T) {
	rec := &Record{Fields: []FieldValue{{
		Code: "living_water_reading", Label: "生活水表读数", AssetName: "消防水表", Value: "107",
		NeedsReview: true, Reason: sanityNoteMark + "比消防水表 上一次的 104 还小",
	}}}
	lines := attentionFieldLines(rec, 3)
	if len(lines) != 1 || !strings.HasPrefix(lines[0], "生活水表读数(消防水表) 107") {
		t.Fatalf("没写出实际挂的表:%v", lines)
	}
}

func TestAttentionAssetLineWording(t *testing.T) {
	var as []*AssetEntry
	for _, n := range []string{"Z1", "Z2", "Z3", "Z4", "生活水表", "消防水表"} {
		as = append(as, &AssetEntry{AssetName: n})
	}
	if got := attentionAssetLine(as); got != "Z1、Z2、Z3 等 6 台" {
		t.Errorf("得到 %q", got)
	}
}

// 后台详情和推送读同一份:记录出站时带上"需要注意"的那几项。
func TestOutboundRecordCarriesAttentionItems(t *testing.T) {
	out := sanitizeRecordForCurrentTemplate(oct8EnergyRecord())
	if len(out.AttentionItems) != 1 {
		t.Fatalf("应只有生活水表一项,得到 %+v", out.AttentionItems)
	}
	it := out.AttentionItems[0]
	if it.Label != "生活水表读数" || it.Value != "2119" || it.Kind != "待复核" ||
		it.Reason != "是上一次 107 的 20 倍,量级对不上" {
		t.Fatalf("内容不对:%+v", it)
	}
}
