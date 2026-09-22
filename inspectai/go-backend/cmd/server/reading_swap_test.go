package main

import "testing"

// ===== 两格读数对调 =====
//
// 2026-09-22 线上卡死过:两个水表的格子里装着电表的读数,而"已经归了别行的
// 设备"当时在下拉里全被禁掉 —— 要腾出水表得先改那两格,可一台都选不了。
// 格子和设备一样多的时候,禁用永远解不开错位,只有对调能。

func twoFields() (FieldValue, FieldValue) {
	a := FieldValue{
		Code: "z1_reading", Label: "Z1能耗表读数", AssetName: "Z1",
		Value: "60197.924", AIValue: "60197.9", Confidence: 0.91,
		SourceImageID: "img_a", Bbox: []float64{1, 2, 3, 4}, Source: "ai", Version: 3,
	}
	b := FieldValue{
		Code: "fire_water_reading", Label: "消防水表读数", AssetName: "消防水表",
		Value: "1662", AIValue: "1662", Confidence: 0.4, NeedsReview: true,
		Reason: "反光", SourceImageID: "img_b", Bbox: []float64{5, 6, 7, 8},
		Source: "ai", Version: 1,
	}
	return a, b
}

// 【换的是"这一次抄到的东西",不换"这一格是谁"】
// AssetName 跟着换的话,日报上的固定栏位就跟着人选的设备跑了 ——
// 会出现没有 Z1 那一栏、却有两栏叫消防水表。
func TestSwapMovesPayloadNotIdentity(t *testing.T) {
	a, b := twoFields()
	swapFieldPayload(&a, &b)

	if a.AssetName != "Z1" || b.AssetName != "消防水表" {
		t.Errorf("设备归属被一起换走了:a=%q b=%q", a.AssetName, b.AssetName)
	}
	if a.Code != "z1_reading" || b.Code != "fire_water_reading" {
		t.Errorf("字段标识被换了:a=%q b=%q", a.Code, b.Code)
	}
	if a.Value != "1662" || b.Value != "60197.924" {
		t.Errorf("读数没换过来:a=%q b=%q", a.Value, b.Value)
	}
}

// 【整组一起换,少一样都不行】只换读数不换照片的话,
// 人看着 A 的照片核 B 的数,而两边都不报错。
func TestSwapCarriesTheWholeGroup(t *testing.T) {
	a, b := twoFields()
	a0, b0 := a, b
	swapFieldPayload(&a, &b)

	for name, got := range map[string][2]any{
		"照片":   {a.SourceImageID, b0.SourceImageID},
		"AI原值": {a.AIValue, b0.AIValue},
		"置信度":  {a.Confidence, b0.Confidence},
		"待复核":  {a.NeedsReview, b0.NeedsReview},
		"理由":   {a.Reason, b0.Reason},
	} {
		if got[0] != got[1] {
			t.Errorf("%s 没跟着换:得到 %v,应为 %v", name, got[0], got[1])
		}
	}
	if len(a.Bbox) != 4 || a.Bbox[0] != 5 {
		t.Errorf("读数区框没跟着换:%v", a.Bbox)
	}
	if len(b.Bbox) != 4 || b.Bbox[0] != 1 {
		t.Errorf("另一边的框也要换过来:%v", b.Bbox)
	}
	_ = a0
}

// 换这一下是人做的判断,不是 AI 填的;版本号两边都要涨,
// 否则前端拿着旧版本再改会被乐观锁挡下来,表现成"改不了"。
func TestSwapMarksHumanEditedAndBumpsBothVersions(t *testing.T) {
	a, b := twoFields()
	av, bv := a.Version, b.Version
	swapFieldPayload(&a, &b)

	if a.Source != "human-edited" || b.Source != "human-edited" {
		t.Errorf("来源没标成人工:a=%q b=%q", a.Source, b.Source)
	}
	if a.Version != av+1 || b.Version != bv+1 {
		t.Errorf("版本号没各自 +1:a %d→%d, b %d→%d", av, a.Version, bv, b.Version)
	}
}

// 【和空格对调也要能用】目标格空着时对调等价于搬家,不能丢东西。
func TestSwapWithEmptySlotKeepsEverything(t *testing.T) {
	a, _ := twoFields()
	empty := FieldValue{Code: "z5_reading", Label: "Z5读数", AssetName: "Z5"}
	swapFieldPayload(&a, &empty)

	if empty.Value != "60197.924" || empty.SourceImageID != "img_a" {
		t.Errorf("东西没搬到空格上:%+v", empty)
	}
	if a.Value != "" || a.SourceImageID != "" {
		t.Errorf("原来那格没腾空:%+v", a)
	}
	if empty.AssetName != "Z5" || a.AssetName != "Z1" {
		t.Errorf("设备归属被动了:a=%q empty=%q", a.AssetName, empty.AssetName)
	}
}

// 【换两次回到原样】现场就是这么用的:换错了再点一次换回来。
// 不成立的话,人就不敢用这个动作了。
func TestSwapTwiceIsIdentity(t *testing.T) {
	a, b := twoFields()
	a0, b0 := a, b
	swapFieldPayload(&a, &b)
	swapFieldPayload(&a, &b)

	if a.Value != a0.Value || a.SourceImageID != a0.SourceImageID ||
		b.Value != b0.Value || b.SourceImageID != b0.SourceImageID {
		t.Errorf("换两次没回到原样\na=%+v\nb=%+v", a, b)
	}
}
