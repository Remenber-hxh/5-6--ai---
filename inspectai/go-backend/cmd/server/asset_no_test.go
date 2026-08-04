package main

import "testing"

// 设备编号必须是人填的，AI 不能碰。
//
// 这条是数据完整性，不是体验：asset_no 是资产台账的主键(buildAsset 拿它当
// 资产名)。AI 认错一个字符，两台设备就在台账里并成一台，或者凭空多出一台。
// 原来两个电梯模板还给它写死了默认值 "HYZX-WJ-DT01" —— 识别失败的记录全挂
// 到同一个编号下，而且被标成"已确认"，看起来像人核对过的。
func TestAssetNoIsNeverFilledByAI(t *testing.T) {
	// 所有模板的 asset_no 都必须是 ManualOnly，且不带默认值
	for _, tpl := range reportTemplates() {
		for _, f := range tpl.Fields {
			if f.Code != "asset_no" {
				continue
			}
			if !f.ManualOnly {
				t.Errorf("模板 %s 的 asset_no 不是 ManualOnly —— AI 会写它", tpl.Name)
			}
			if f.Source != "manual" {
				t.Errorf("模板 %s 的 asset_no source=%q，应为 manual", tpl.Name, f.Source)
			}
			if f.Default != "" {
				t.Errorf("模板 %s 的 asset_no 带默认值 %q —— 识别失败时所有记录会并成同一台设备",
					tpl.Name, f.Default)
			}
			if !f.Required {
				t.Errorf("模板 %s 的 asset_no 不是必填 —— 空编号会让记录落不到具体设备上", tpl.Name)
			}
		}
	}

	// applyRecognizedFields 必须跳过它，哪怕 AI 明确返回了值
	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到电梯模板")
	}
	rec := &Record{TemplateID: tpl.ID, Fields: initialFieldValues(tpl, "巡检员")}
	applyRecognizedFields(rec, []RecognizedField{
		{Code: "asset_no", Value: "AI瞎猜的编号", Confidence: 0.99},
		{Code: "inspection_time", Value: "2026-08-04 10:00", Confidence: 0.9},
	})
	for _, f := range rec.Fields {
		if f.Code == "asset_no" {
			if f.Value != "" {
				t.Fatalf("AI 把设备编号写成了 %q —— 必须留空等人选", f.Value)
			}
			if f.Source == "ai" {
				t.Fatalf("设备编号的 source 被改成了 ai")
			}
		}
		// 对照:非 ManualOnly 的字段该被正常填上，证明不是整个函数没生效
		if f.Code == "inspection_time" && f.Value != "2026-08-04 10:00" {
			t.Fatalf("普通字段没有被 AI 填上(value=%q)，测试本身可能失效了", f.Value)
		}
	}
}
