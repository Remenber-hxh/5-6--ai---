package main

import "testing"

// ===== 猜出来的小数点必须让人看一眼 =====
//
// 2026-09-22 实测到的原始数据:模型在理由里写了"按固定格式切分",
// confidence 给 0.95,于是 NeedsReview(<0.85) 判成 false —— 这一行不提示复核。
// 而那块表的屏上根本没有小数点,位置完全是按表型推的。

func TestAssumedDecimalIsForcedToReview(t *testing.T) {
	// 线上原样:z1_reading
	f := &FieldValue{
		Code: "z1_reading", Value: "60197.924", AIValue: "60197.924",
		Confidence: 0.95, NeedsReview: false,
		Reason: "图3 EP 6019/7924k 按固定格式切分;放大复核一致",
	}
	guardAssumedDecimal(f)

	if !f.NeedsReview {
		t.Error("小数点是猜的,却没标成待复核 —— 人不会去核它")
	}
	if f.Confidence > assumedDecimalConfidence {
		t.Errorf("置信度没压下来:%v", f.Confidence)
	}
	if f.Value != "60197.924" {
		t.Errorf("读数本身不该被改动:%q", f.Value)
	}
	// 理由要说人话,"按固定格式切分"现场看不懂是什么意思
	if !contains(f.Reason, "请核对小数点") {
		t.Errorf("没告诉人要做什么:%q", f.Reason)
	}
}

// 屏上真看得见小数点的,不该被拖进复核 —— 那样每一行都待复核,提示就失效了。
func TestVisibleDecimalIsLeftAlone(t *testing.T) {
	f := &FieldValue{
		Code: "z3_reading", Value: "84363.520", Confidence: 0.95,
		Reason: "屏幕小数点清晰可见,直接读取",
	}
	guardAssumedDecimal(f)
	if f.NeedsReview || f.Confidence != 0.95 {
		t.Errorf("看得见小数点的被误伤了:%+v", f)
	}
}

// 整数读数(机械水表)没有小数位可猜,不该被这道闸碰到。
func TestIntegerReadingNotAffected(t *testing.T) {
	f := &FieldValue{
		Code: "living_water_reading", Value: "1992", Confidence: 0.9,
		Reason: "按固定格式读取黑色字轮",
	}
	guardAssumedDecimal(f)
	if f.NeedsReview || f.Confidence != 0.9 {
		t.Errorf("整数读数被误伤:%+v", f)
	}
}

// 【只压不抬】模型自己就不确信时保持原样,别替它把话说满。
func TestGuardNeverRaisesConfidence(t *testing.T) {
	f := &FieldValue{
		Code: "z4_reading", Value: "20184.018", Confidence: 0.3,
		Reason: "图6 EP 2018/4018 按固定格式假设",
	}
	guardAssumedDecimal(f)
	if f.Confidence != 0.3 {
		t.Errorf("置信度被抬上去了:%v", f.Confidence)
	}
	if !f.NeedsReview {
		t.Error("仍然要标待复核")
	}
}

// 线上出现过的几种说法都要认得出来 —— 提示词改了措辞不能让这道闸失效。
func TestMarkerCoverage(t *testing.T) {
	for _, reason := range []string{
		"图3 EP 6019/7924k 按固定格式切分;放大复核一致",
		"图6 EP 2018/4018 按固定格式假设;放大复核一致",
		"屏幕反光看不清小数点,按表型补",
		"未见小数点,按本场景格式推断",
	} {
		if !decimalWasAssumed("60197.924", reason) {
			t.Errorf("没认出这是猜的:%q", reason)
		}
	}
	for _, reason := range []string{
		"屏幕小数点清晰可见",
		"放大后逐位复核一致",
		"",
	} {
		if decimalWasAssumed("60197.924", reason) {
			t.Errorf("误判成猜的:%q", reason)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
