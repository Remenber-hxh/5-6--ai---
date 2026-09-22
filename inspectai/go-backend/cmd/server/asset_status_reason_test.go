package main

import (
	"strings"
	"testing"
)

// ===== 「待复核」必须说得出原因 =====
//
// 2026-09-22 现场:详情页只有一个「待复核」标签,读数看着正常、照片也拍了,
// 人完全不知道要核什么。触发待复核有四种,而页面只在其中两种时给了线索。

func warnAsset(name string) *AssetEntry {
	return &AssetEntry{AssetName: name, LastStatus: "待复核", StatusLevel: "warning"}
}

// 【以前完全不说的那一种之一】AI 置信度低 → NeedsReview。
func TestReasonTellsLowConfidence(t *testing.T) {
	f := &FieldValue{Code: "fire_water_reading", Label: "消防水表读数",
		AssetName: "消防水表", Value: "60197.924", NeedsReview: true, Confidence: 0.55}
	got := assetStatusReason(warnAsset("消防水表"), &Record{}, f)
	if got == "" {
		t.Fatal("置信度低是最常见的待复核原因,却一个字都不说")
	}
	if !strings.Contains(got, "55%") {
		t.Errorf("没把置信度说出来,人不知道是扫一眼还是必须重核:%q", got)
	}
}

// 【以前完全不说的那一种之二】AI 总结失败。
func TestReasonTellsSummaryFailure(t *testing.T) {
	rec := &Record{AISummaryError: "上游超时"}
	f := &FieldValue{Value: "1992"} // 字段本身没毛病
	got := assetStatusReason(warnAsset("生活水表"), rec, f)
	if !strings.Contains(got, "总结") {
		t.Errorf("总结失败导致的待复核没说清:%q", got)
	}
}

// 读数为空:要说的是"去补填",不是"去核对"。
func TestReasonTellsMissingReading(t *testing.T) {
	f := &FieldValue{Code: "z1_reading", Value: ""}
	got := assetStatusReason(warnAsset("Z1"), &Record{}, f)
	if !strings.Contains(got, "补填") {
		t.Errorf("没读到数时该让人去补填:%q", got)
	}
}

// 理由里带异常关键词:原样把 AI 的话给出来,那是最具体的线索。
func TestReasonPassesThroughAnomalyText(t *testing.T) {
	f := &FieldValue{Value: "100", Reason: "屏幕反光,数字模糊"}
	got := assetStatusReason(warnAsset("Z3"), &Record{}, f)
	if !strings.Contains(got, "模糊") {
		t.Errorf("AI 说的具体理由没带出来:%q", got)
	}
}

// 高优先级建议里点了这台设备的名字。
func TestReasonUsesHighPriorityRecommendation(t *testing.T) {
	rec := &Record{AIRecommendations: []Recommendation{
		{Priority: "high", Text: "Z4 读数较上次倒退,建议现场核实"},
		{Priority: "low", Text: "无关紧要的一条"},
	}}
	got := assetStatusReason(warnAsset("Z4"), rec, &FieldValue{Value: "1"})
	if !strings.Contains(got, "倒退") {
		t.Errorf("没把高优先级建议带出来:%q", got)
	}
	if strings.Contains(got, "无关紧要") {
		t.Errorf("低优先级的那条不该出现:%q", got)
	}
}

// 【正常设备不挂解释】每台都写一句"一切正常"是噪音。
func TestNormalAssetHasNoReason(t *testing.T) {
	a := &AssetEntry{AssetName: "Z1", LastStatus: "正常", StatusLevel: "normal"}
	f := &FieldValue{Value: "100", NeedsReview: true, Confidence: 0.3}
	if got := assetStatusReason(a, &Record{}, f); got != "" {
		t.Errorf("正常设备挂了一句解释:%q", got)
	}
}

// 【兜底也要说点什么】状态是别处定的(后台手改、派了复检任务)时,
// 空着会让人以为系统忘了给理由。
func TestReasonNeverEmptyWhenFollowupNeeded(t *testing.T) {
	if got := assetStatusReason(warnAsset("Z1"), nil, nil); got == "" {
		t.Error("需要跟进却一句解释都没有 —— 和改之前一样")
	}
}

// fieldForAsset:先按 assetName 认,老记录没有那一位时按字段名回落。
func TestFieldForAssetMatchesByNameThenLabel(t *testing.T) {
	rec := &Record{Fields: []FieldValue{
		{Code: "z1_reading", Label: "Z1能耗表读数", AssetName: "Z1", Value: "1"},
		{Code: "living_water_reading", Label: "生活水表读数", Value: "2"}, // 老记录:没有 assetName
	}}
	if f := fieldForAsset(rec, "Z1"); f == nil || f.Code != "z1_reading" {
		t.Errorf("按 assetName 没认出来:%+v", f)
	}
	if f := fieldForAsset(rec, "生活水表"); f == nil || f.Code != "living_water_reading" {
		t.Errorf("老记录按字段名回落失败:%+v", f)
	}
	if f := fieldForAsset(rec, "不存在的表"); f != nil {
		t.Errorf("不该硬凑一个:%+v", f)
	}
}
