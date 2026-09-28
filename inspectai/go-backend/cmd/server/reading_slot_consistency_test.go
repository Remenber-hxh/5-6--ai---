package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ===== 抄表:一台表、一格、一张照片、一个读数,四样必须对得上 =====
//
// 2026-09-27 线上反馈的两件事,根子是同一个:同一台表在不同的地方被
// 用不同的规则认出来。
//
//  1. 台账里「照片是这台表的,读数不是」—— 照片按"这台表被选在哪一格"取,
//     读数历史却按一张写死的对照表取(Z1 永远是第 1 格)。
//  2. 确认页下拉「莫名其妙变灰 / 该灰的能选」—— 默认名每次读记录都猜一遍,
//     会和人在别的格子上选过的名字撞在一起;改设备名的接口也不拦重复。

func seedDefaultNamedMeters(t *testing.T, s *Server, names ...string) {
	t.Helper()
	for _, n := range names {
		typ := "电表"
		if n == "生活水表" || n == "消防水表" {
			typ = "水表"
		}
		if err := s.store.CreateAsset(&AssetEntry{
			ID: "紫菡雅集::zihan_energy::" + n, TenantID: defaultTenantID, Project: "紫菡雅集",
			TemplateID: "zihan_energy", AssetType: typ, AssetKey: n, AssetName: n, LastStatus: "未巡检",
		}); err != nil {
			t.Fatalf("CreateAsset %s: %v", n, err)
		}
	}
}

// 【台账读数必须跟着格子走】Z1能耗表 被选在了第 3 格、Z3能耗表 在第 1 格,
// 那 Z1能耗表 这次的读数就是第 3 格的 300,不是第 1 格的 100。
func TestObservationFollowsTheSlotTheMeterWasPickedIn(t *testing.T) {
	srv, st := newLedgerTestServer(t)
	for _, a := range []*AssetEntry{
		{ID: "紫菡雅集::zihan_energy::z1_energy_meter", AssetKey: "z1_energy_meter", AssetName: "Z1能耗表", AssetType: "电表"},
		{ID: "紫菡雅集::zihan_energy::z3_energy_meter", AssetKey: "z3_energy_meter", AssetName: "Z3能耗表", AssetType: "电表"},
	} {
		a.TenantID, a.Project, a.TemplateID, a.LastStatus = defaultTenantID, "紫菡雅集", "zihan_energy", "未巡检"
		if err := st.CreateAsset(a); err != nil {
			t.Fatal(err)
		}
	}
	rec := meterRecord(map[string][2]string{
		"z1_reading": {"100", "Z3能耗表"},
		"z3_reading": {"300", "Z1能耗表"},
	})
	assets := srv.buildLedgerAssets(rec, time.Now())
	_, obs := buildRecordObservations(rec, assets, time.Now())

	got := map[string]string{} // 资产 ID → 记下的读数
	for _, o := range obs {
		got[o.AssetID] = o.ValueText
	}
	if v := got["紫菡雅集::zihan_energy::z1_energy_meter"]; v != "300" {
		t.Errorf("Z1能耗表 记下的读数是 %q,应该是它所在那一格(第 3 格)的 300 —— "+
			"台账上就是「照片是 Z1 的、读数是别的表的」", v)
	}
	if v := got["紫菡雅集::zihan_energy::z3_energy_meter"]; v != "100" {
		t.Errorf("Z3能耗表 记下的读数是 %q,应该是 100", v)
	}
}

// 【默认名不许和人选过的撞】第 1 格上人选了「Z2能耗表」,第 2 格就不能再被
// 默认猜成「Z2能耗表」—— 否则同一台表挂在两格上。猜出来的要打上标记,
// 前端靠它判断"这一格其实是空的"。
func TestDefaultNameIsNotReusedAndIsMarked(t *testing.T) {
	s, _, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "Z1能耗表", "Z2能耗表", "Z3能耗表")
	rec := &Record{
		ID: "rec_dflt", TenantID: defaultTenantID, TemplateID: "zihan_energy", Project: "紫菡雅集",
		Fields: []FieldValue{
			{Code: "z1_reading", Label: "Z1 能耗表读数", AssetName: "Z2能耗表", Value: "1"},
			{Code: "z2_reading", Label: "Z2 能耗表读数"},
			{Code: "z3_reading", Label: "Z3 能耗表读数"},
		},
	}
	s.fillReadingAssetOptions(rec)

	z1, _ := fieldByCode(rec.Fields, "z1_reading")
	z2, _ := fieldByCode(rec.Fields, "z2_reading")
	z3, _ := fieldByCode(rec.Fields, "z3_reading")
	if z2.AssetName != "" {
		t.Errorf("第 2 格被默认猜成了 %q,而第 1 格已经选了它 —— 同一台表挂在两格上", z2.AssetName)
	}
	if z3.AssetName != "Z3能耗表" || !z3.AssetDefaulted {
		t.Errorf("没人选过的第 3 格应该默认成 Z3能耗表 并标记为默认,得到 %q defaulted=%v",
			z3.AssetName, z3.AssetDefaulted)
	}
	if z1.AssetDefaulted {
		t.Error("人选的设备被标成了默认值 —— 前端会把这一格当成空位,拿去给别的照片")
	}
}

func patchField(t *testing.T, s *Server, tok, rid, code, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch,
		"/api/inspection/records/"+rid+"/fields/"+code, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	w := httptest.NewRecorder()
	s.router(w, req)
	return w
}

// 【一台表不能同时在两格上有东西】Z1 在第 1 格、有读数;第 2 格再选 Z1 要被拒 ——
// 前端变灰拦得住正常点击,拦不住两台手机同时改或者连点两下。
func TestPatchRefusesMeterAlreadyInUseOnAnotherRow(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "Z1", "Z2")
	rec := &Record{
		ID: "rec_guard", TenantID: defaultTenantID, TemplateID: "zihan_energy", Project: "紫菡雅集",
		CreatedAt: time.Now(),
		Fields: []FieldValue{
			{Code: "z1_reading", Label: "Z1 能耗表读数", AssetName: "Z1", Value: "100"},
			{Code: "z2_reading", Label: "Z2 能耗表读数", Value: "200"},
		},
	}
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	w := patchField(t, s, tok, rec.ID, "z2_reading", `{"assetName":"Z1"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("把已经在第 1 格上的 Z1 又选到第 2 格,接口返回 %d —— 应该拒绝", w.Code)
	}
	after, _ := s.store.GetRecord(defaultTenantID, rec.ID)
	z2, _ := fieldByCode(after.Fields, "z2_reading")
	if z2.AssetName == "Z1" {
		t.Error("被拒了却还是存上了")
	}
}

// 【清除一个"默认名"的格子也得存下来】界面上显示「生活水表」,库里其实是空 ——
// 那是后端按字段名猜的。点「清除」时名字"从空改成空",原来被当成空请求直接
// 返回 200,清除过的标记没存,下次读记录默认名又回来了:点了没反应。
func TestClearingADefaultedNameSticks(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "生活水表")
	rec := &Record{
		ID: "rec_clear_dflt", TenantID: defaultTenantID, TemplateID: "zihan_energy", Project: "紫菡雅集",
		CreatedAt: time.Now(),
		Fields: []FieldValue{
			{Code: "living_water_reading", Label: "生活水表读数", Value: "1992"}, // 名字没存,靠默认
		},
	}
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	if w := patchField(t, s, tok, rec.ID, "living_water_reading", `{"assetName":""}`); w.Code != http.StatusOK {
		t.Fatalf("清除被拒:%d %s", w.Code, w.Body.String())
	}
	after, _ := s.store.GetRecord(defaultTenantID, rec.ID)
	s.fillReadingAssetOptions(after) // 和 GET 一样再猜一遍默认值
	f, _ := fieldByCode(after.Fields, "living_water_reading")
	if f.AssetName != "" {
		t.Errorf("清除之后默认名又回来了(%q)—— 界面上就是点了没反应", f.AssetName)
	}
}

// 【线上的表叫 Z1,格子叫「Z1 能耗表读数」】第一档对不上时,去掉表型尾巴再对,
// 只认唯一的一台;对得上全名的永远优先;不是表型尾巴的不去。
func TestDefaultMatchesBareMeterNumber(t *testing.T) {
	prod := []string{"Z1", "Z2", "Z3", "Z4", "生活水表", "消防水表"}
	cases := []struct {
		label string
		opts  []string
		want  string
		why   string
	}{
		{"Z1 能耗表读数", prod, "Z1", "线上电表叫 Z1,应该默认对上"},
		{"Z4 能耗表读数", prod, "Z4", ""},
		{"生活水表读数", prod, "生活水表", "全名对得上的照旧"},
		{"Z1 能耗表读数", []string{"Z1", "Z1能耗表"}, "Z1能耗表", "全名对得上的优先于去尾巴"},
		{"Z1 能耗表读数", []string{"Z1", "z1"}, "", "两台都像就不猜"},
		{"Z5 能耗表读数", prod, "", "台账里没有 Z5 就不猜"},
		{"水泵房总表读数", []string{"水泵房"}, "", "「总表」不是表型尾巴,不去"},
		{"能耗表读数", []string{""}, "", "去完什么都不剩,不猜"},
	}
	for _, c := range cases {
		if got := defaultAssetForField(c.label, c.opts); got != c.want {
			t.Errorf("%s / %v → %q,应该是 %q(%s)", c.label, c.opts, got, c.want, c.why)
		}
	}
}

// 【只挂着名字的空格不算占用】读数被挪走后,原来那一格还挂着「Z5」这个名字、
// 但什么都没有。这时在别的格子上选 Z5 应该成功,并且把那个空名字放掉 ——
// 不放掉的话,下一次读记录两格都叫 Z5。
func TestPatchReleasesNameLeftOnAnEmptySlot(t *testing.T) {
	s, tok, _ := newSwapAPIServer(t)
	seedDefaultNamedMeters(t, s, "Z5")
	rec := &Record{
		ID: "rec_release", TenantID: defaultTenantID, TemplateID: "zihan_energy", Project: "紫菡雅集",
		CreatedAt: time.Now(),
		Fields: []FieldValue{
			{Code: "z1_reading", Label: "Z1 能耗表读数", AssetName: "Z5"}, // 空格,只挂着名字
			{Code: "z2_reading", Label: "Z2 能耗表读数", Value: "200"},
		},
	}
	if err := s.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	if w := patchField(t, s, tok, rec.ID, "z2_reading", `{"assetName":"Z5"}`); w.Code != http.StatusOK {
		t.Fatalf("选一台只被空格挂着名字的表被拒了:%d %s", w.Code, w.Body.String())
	}
	after, _ := s.store.GetRecord(defaultTenantID, rec.ID)
	z1, _ := fieldByCode(after.Fields, "z1_reading")
	z2, _ := fieldByCode(after.Fields, "z2_reading")
	if z2.AssetName != "Z5" {
		t.Errorf("第 2 格没选上 Z5:%q", z2.AssetName)
	}
	if z1.AssetName == "Z5" {
		t.Error("空格上的「Z5」没放掉 —— 两格同时叫 Z5")
	}
}
