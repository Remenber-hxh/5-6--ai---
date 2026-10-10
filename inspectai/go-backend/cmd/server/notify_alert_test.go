package main

import (
	"net/http"
	"net/http/httptest"
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

// 10/08 那条记录在确认页上真实发生的十步(field_confirm_logs 原样):
// AI 把两块水表的读数放反了 —— 生活水表格是 1017102、消防水表格是 2115。
// 消防水表格当场被量级检查拦下:"是上一次 107 的 20 倍"。巡检员改值、清设备、
// 对调、重新挂设备、再改值,最后两格都对了 —— 线上那版对调时把这句理由一起搬到了
// 生活水表格,又没重查,于是生活水表顶着别的表的"上一次 107"一直待复核。
func TestOct8WaterMeterFixLeavesNothingStale(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "生活水表", "消防水表")
	for name, h := range map[string]struct {
		code string
		v    float64
	}{"生活水表": {"living_water_reading", 2047}, "消防水表": {"fire_water_reading", 107}} {
		v := h.v
		if err := s.store.WriteAssetSnapshots(nil, []*FieldObservation{{
			AssetID: "紫菡雅集::zihan_energy::" + name, RecordID: "rec_0930", FieldKey: h.code,
			ValueNumber: &v, CreatedAt: time.Date(2026, 9, 30, 8, 38, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
	}

	rec := zihanMeterRecord(t, "rec_1008w", map[string]string{
		"living_water_reading": "1017102", "fire_water_reading": "2115",
	})
	recField(t, rec, "living_water_reading").AssetName = "生活水表"
	recField(t, rec, "fire_water_reading").AssetName = "消防水表"
	flagImplausibleReadings(s.store, rec)
	if !strings.Contains(recField(t, rec, "fire_water_reading").Reason, "107") {
		t.Fatalf("前提不成立:消防水表格的 2115 应先被拦下,reason=%q", recField(t, rec, "fire_water_reading").Reason)
	}
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}

	steps := []struct{ code, body string }{
		{"living_water_reading", `{"value":"1017102"}`},                           // 13:51:51 confirm
		{"fire_water_reading", `{"value":"2115"}`},                                // 13:51:51 confirm
		{"living_water_reading", `{"value":"104"}`},                               // 13:52:03 correct
		{"living_water_reading", `{"assetName":""}`},                              // 13:52:04 清设备
		{"fire_water_reading", `{"assetName":""}`},                                // 13:52:08 清设备
		{"swap", `{"aCode":"living_water_reading","bCode":"fire_water_reading"}`}, // 13:52:10
		{"fire_water_reading", `{"assetName":"消防水表"}`},                            // 13:52:10
		{"living_water_reading", `{"assetName":"生活水表"}`},                          // 13:52:12
		{"living_water_reading", `{"value":"2119"}`},                              // 13:52:21 correct
	}
	for i, st := range steps {
		var w *httptest.ResponseRecorder
		if st.code == "swap" {
			w = postSwap(t, s, tok, rec.ID, st.body)
		} else {
			w = patchField(t, s, tok, rec.ID, st.code, st.body)
		}
		if w.Code != http.StatusOK {
			t.Fatalf("第 %d 步 %s %s 失败:%d %s", i+1, st.code, st.body, w.Code, w.Body.String())
		}
	}

	got, err := s.store.GetRecord(defaultTenantID, rec.ID)
	if err != nil || got == nil {
		t.Fatalf("读回记录失败:%v", err)
	}
	living := recField(t, got, "living_water_reading")
	fire := recField(t, got, "fire_water_reading")
	if living.Value != "2119" || living.AssetName != "生活水表" || fire.Value != "104" || fire.AssetName != "消防水表" {
		t.Fatalf("回放结果不对:生活=%s(%s) 消防=%s(%s)", living.Value, living.AssetName, fire.Value, fire.AssetName)
	}
	if strings.Contains(living.Reason, "107") || strings.Contains(living.Reason, sanityNoteMark) {
		t.Errorf("生活水表格还挂着别的表的理由:%q", living.Reason)
	}
	if items := sanitizeRecordForCurrentTemplate(got).AttentionItems; len(items) != 0 {
		t.Errorf("两格都已改对,不该再有需要注意的项:%+v", items)
	}
}

// 消防水表 AI 每次都读成 01017102,被拦下;巡检员改成 104 之后不该再报。
// 原样确认 AI 那个数(不改值)则要照样报 —— 那个数确实不对。
func TestCorrectingFlaggedReadingClearsTheAlert(t *testing.T) {
	setup := func(t *testing.T) (*Server, string, *Record) {
		s, tok, _ := newSwapAPIServer(t)
		seedDefaultNamedMeters(t, s, "消防水表")
		v := 104.0
		if err := s.store.WriteAssetSnapshots(nil, []*FieldObservation{{
			AssetID: "紫菡雅集::zihan_energy::消防水表", RecordID: "rec_1008", FieldKey: "fire_water_reading",
			ValueNumber: &v, CreatedAt: time.Date(2026, 10, 8, 13, 51, 0, 0, time.UTC),
		}}); err != nil {
			t.Fatal(err)
		}
		rec := zihanMeterRecord(t, "rec_1009", map[string]string{"fire_water_reading": "01017102"})
		recField(t, rec, "fire_water_reading").AssetName = "消防水表"
		flagImplausibleReadings(s.store, rec)
		if !strings.Contains(recField(t, rec, "fire_water_reading").Reason, sanityNoteMark) {
			t.Fatal("前提不成立:01017102 应先被拦下")
		}
		if err := s.store.CreateRecord(rec); err != nil {
			t.Fatal(err)
		}
		return s, tok, rec
	}
	items := func(t *testing.T, s *Server, id string) []RecordAttention {
		got, err := s.store.GetRecord(defaultTenantID, id)
		if err != nil || got == nil {
			t.Fatal(err)
		}
		return sanitizeRecordForCurrentTemplate(got).AttentionItems
	}

	t.Run("改成正确读数", func(t *testing.T) {
		s, tok, rec := setup(t)
		if w := patchField(t, s, tok, rec.ID, "fire_water_reading", `{"value":"104"}`); w.Code != http.StatusOK {
			t.Fatalf("改值失败:%d %s", w.Code, w.Body.String())
		}
		if it := items(t, s, rec.ID); len(it) != 0 {
			t.Errorf("人已经改对了,不该再报:%+v", it)
		}
	})
	t.Run("空着直接确认", func(t *testing.T) {
		s, tok, rec := setup(t)
		if f := recField(t, rec, "fire_water_reading"); f.Value != "" {
			t.Fatalf("前提不成立:01017102 应已被清空,得到 %q", f.Value)
		}
		if w := patchField(t, s, tok, rec.ID, "fire_water_reading", `{"value":""}`); w.Code != http.StatusOK {
			t.Fatalf("确认失败:%d %s", w.Code, w.Body.String())
		}
		if it := items(t, s, rec.ID); len(it) != 1 {
			t.Errorf("读数还空着,应该照样报:%+v", it)
		}
	})
}

// AI 把两块水表放反:两格都因量级不对被清空。人一对调,各自按新的那块表再比 ——
// 对得上的放回来(生活水表 2115),对不上的继续空着(消防水表格里的 1017102)。
func TestSwapRestoresClearedReadingThatFitsTheNewMeter(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "生活水表", "消防水表")
	for name, h := range map[string]struct {
		code string
		v    float64
	}{"生活水表": {"living_water_reading", 2047}, "消防水表": {"fire_water_reading", 104}} {
		v := h.v
		_ = s.store.WriteAssetSnapshots(nil, []*FieldObservation{{
			AssetID: "紫菡雅集::zihan_energy::" + name, RecordID: "rec_prev", FieldKey: h.code,
			ValueNumber: &v, CreatedAt: time.Date(2026, 9, 30, 8, 38, 0, 0, time.UTC),
		}})
	}
	rec := zihanMeterRecord(t, "rec_swap_back", map[string]string{
		"living_water_reading": "1017102", "fire_water_reading": "2115",
	})
	recField(t, rec, "living_water_reading").AssetName = "生活水表"
	recField(t, rec, "fire_water_reading").AssetName = "消防水表"
	flagImplausibleReadings(s.store, rec)
	if recField(t, rec, "living_water_reading").Value != "" || recField(t, rec, "fire_water_reading").Value != "" {
		t.Fatal("前提不成立:两格都应先被清空")
	}
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	if w := postSwap(t, s, tok, rec.ID, `{"aCode":"living_water_reading","bCode":"fire_water_reading"}`); w.Code != http.StatusOK {
		t.Fatalf("对调失败:%d %s", w.Code, w.Body.String())
	}
	got, _ := s.store.GetRecord(defaultTenantID, rec.ID)
	living, fire := recField(t, got, "living_water_reading"), recField(t, got, "fire_water_reading")
	if living.Value != "2115" || strings.Contains(living.Reason, sanityNoteMark) {
		t.Errorf("2115 和生活水表对得上,应放回来:value=%q reason=%q", living.Value, living.Reason)
	}
	if fire.Value != "" || !strings.Contains(fire.Reason, "AI 读作 1017102") {
		t.Errorf("1017102 和消防水表对不上,应继续空着:value=%q reason=%q", fire.Value, fire.Reason)
	}
}

// 一张表巡六块表:AI 建议里点了 Z1、Z2 的名字,不能把它们拖成待复核 ——
// 它们那一格自己没有任何问题。
func TestRecommendationDoesNotFlagMetersInSharedForm(t *testing.T) {
	rec := oct8EnergyRecord()
	w, _ := fieldByCode(rec.Fields, "living_water_reading")
	w.NeedsReview, w.Reason = false, ""
	rec.AIRecommendations = []Recommendation{{Priority: "high", Text: "Z1、Z2 读数需核对小数点"}}
	for _, a := range buildZihanEnergyAssets(rec, time.Now()) {
		if a.LastStatus != "正常" {
			t.Errorf("%s 被整条记录的建议拖成了 %s", a.AssetName, a.LastStatus)
		}
	}
	// 一条记录一台设备的,高优先级建议照旧算
	single := &Record{TemplateID: "fire_pump", AIRecommendations: rec.AIRecommendations}
	if !hasAbnormalSignal(&FieldValue{Value: "正常"}, single, "") {
		t.Error("单台设备的记录,高优先级建议应该照旧算异常信号")
	}
}

// 提醒卡片的"建议"只挑点到出问题那台表的;一条都没点到就用通用的那句。
func TestAlertAdviceOnlyAboutTheFlaggedMeter(t *testing.T) {
	rec := oct8EnergyRecord()
	assets := buildZihanEnergyAssets(rec, time.Now()) // 只有生活水表存疑
	rec.AIRecommendations = []Recommendation{
		{Priority: "high", Text: "Z3 配电柜柜门未关好"},
		{Priority: "low", Text: "生活水表读数偏大,请对照照片核对"},
	}
	card := inspectionAlertCard(rec, assets, "https://x")
	if !strings.Contains(card, "生活水表读数偏大") || strings.Contains(card, "Z3 配电柜") {
		t.Errorf("建议应只说生活水表:\n%s", card)
	}
	rec.AIRecommendations = rec.AIRecommendations[:1]
	if card := inspectionAlertCard(rec, assets, "https://x"); !strings.Contains(card, "请主管查看后台记录") {
		t.Errorf("没有点到生活水表的建议时应用通用的那句:\n%s", card)
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

// 2026-10-09:Z2 被手输成 1169788.40(多输了一位,上一次的 10 倍)。趋势图标了"偏离平时",
// 群里却没提醒 —— 识别后的检查不管人填的数。提交时要再比一遍:值不动,设备进待复核。
func TestHandTypedReadingIsFlaggedOnSubmit(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)
	const z2 = "紫菡雅集::zihan_energy::Z2"
	if err := srv.store.CreateAsset(&AssetEntry{ID: z2, TenantID: defaultTenantID, Project: "紫菡雅集",
		TemplateID: "zihan_energy", AssetType: "电表", AssetKey: "Z2", AssetName: "Z2", LastStatus: "正常"}); err != nil {
		t.Fatal(err)
	}
	for i, v := range []float64{116895.19, 116969.15} {
		v := v
		_ = srv.store.WriteAssetSnapshots(nil, []*FieldObservation{{AssetID: z2, RecordID: "hist_" + itoaSafe(i),
			FieldKey: "z2_reading", ValueNumber: &v, CreatedAt: time.Date(2026, 9, 30+8*i, 9, 0, 0, 0, time.UTC)}})
	}
	if err := srv.store.CreateRecord(&Record{
		ID: "rec_1009", TenantID: defaultTenantID, Project: "紫菡雅集", TemplateID: "zihan_energy",
		Inspector: "巡检员", InspectorUserID: "user_i", RecognitionStatus: "recognized",
		CreatedAt: time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC), Images: fixtureImages(5),
		Fields: []FieldValue{{Code: "z2_reading", Label: "Z2 能耗表读数", AssetName: "Z2",
			Value: "1169788.40", AIValue: "116978.84", Source: "human-edited", Confidence: 0.97}},
	}); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/inspection/records/rec_1009/submit", strings.NewReader(""))
	req.Header.Set("X-InspectAI-Token", tokens["inspector"])
	req.Header.Set("Idempotency-Key", "k_1009")
	w := httptest.NewRecorder()
	srv.router(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("提交失败(手填的数不该挡住提交):%d %s", w.Code, w.Body.String())
	}

	got, _ := srv.store.GetRecord(defaultTenantID, "rec_1009")
	f := recField(t, got, "z2_reading")
	if f.Value != "1169788.40" {
		t.Errorf("人填的数不该被改:%q", f.Value)
	}
	if !strings.Contains(f.Reason, "手填 1169788.40") || !strings.Contains(f.Reason, "10 倍") {
		t.Errorf("理由里要写明是手填的、差了多少:%q", f.Reason)
	}
	if a, _ := srv.store.GetAsset(defaultTenantID, z2); a == nil || a.LastStatus != "待复核" {
		t.Errorf("Z2 应进待复核(群里才会提醒),得到 %+v", a)
	}
}
