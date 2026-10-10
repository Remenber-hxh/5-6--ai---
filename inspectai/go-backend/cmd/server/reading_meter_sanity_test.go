package main

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

// ===== 量级检查按"这块表"比,不按"这一格"比 =====
//
// 2026-10 查线上紫菡:Z1 的读数先后记在 Z4、Z4、Z2、Z1 四格里。原来拿"这一格上次的值"
// 当基准,比的常常是另一块表 —— 9/30 那次 Z1、Z2 两张照片对调、小数点又各偏一位
// (Z1 记成 11689.519、Z2 记成 20508.754),被人确认后进了台账。
// 下面的数字都是线上真实读数。

// 紫菡四块电表,带上历史读数(已按设备记好,不管当时在哪一格)
func zihanMeterStore(t *testing.T, history map[string][][2]any) *MemStore {
	t.Helper()
	store := NewMemStore()
	var obs []*FieldObservation
	for _, name := range []string{"Z1", "Z2", "Z3", "Z4"} {
		id := "紫菡雅集::zihan_energy::" + name
		if err := store.CreateAsset(&AssetEntry{ID: id, TenantID: defaultTenantID, Project: "紫菡雅集",
			TemplateID: "zihan_energy", AssetKey: name, AssetName: name, AssetType: "电表"}); err != nil {
			t.Fatal(err)
		}
		for i, h := range history[name] {
			v := h[1].(float64)
			obs = append(obs, &FieldObservation{
				AssetID: id, RecordID: "hist_" + name + string(rune('a'+i)), FieldKey: h[0].(string),
				ValueNumber: &v, CreatedAt: time.Date(2026, 9, 21+i, 10, 0, 0, 0, time.UTC),
			})
		}
	}
	if err := store.WriteAssetSnapshots(nil, obs); err != nil {
		t.Fatal(err)
	}
	return store
}

var zihanHistory = map[string][][2]any{
	// 线上原样的栏位;Z2 用的是改正后的读数
	"Z1": {{"z4_reading", 203551.94}, {"z4_reading", 203712.26}, {"z2_reading", 203904.50}},
	"Z2": {{"z1_reading", 116748.24}, {"z3_reading", 116763.65}, {"z1_reading", 116776.64}},
}

func zihanMeterRecord(t *testing.T, id string, vals map[string]string) *Record {
	t.Helper()
	rec := zihanEnergyRecord(t, id, false, vals)
	rec.Project = "紫菡雅集"
	return rec
}

// 9/30 那次:照片对调 + 小数点偏一位。和哪块表都对不上 —— 两格都清空、清掉设备,请人选设备。
func TestSwappedAndShiftedMeterReadingsAreFlagged(t *testing.T) {
	store := zihanMeterStore(t, zihanHistory)
	rec := zihanMeterRecord(t, "rec_0930", map[string]string{
		"z1_reading": "11689.519", // 其实是 Z2 的 116895.19
		"z2_reading": "20508.754", // 其实是 Z1 的 205087.54
	})
	issues := flagImplausibleReadings(store, rec)
	if len(issues) != 2 {
		t.Fatalf("两格都该拦下,得到 %d 条:%v", len(issues), issues)
	}
	for code, ai := range map[string]string{"z1_reading": "11689.519", "z2_reading": "20508.754"} {
		f := recField(t, rec, code)
		if f.Value != "" || !f.AssetCleared || f.Reason != sanityNoteMark+"AI 读作 "+ai+",请选择设备" {
			t.Errorf("%s 应清空读数和设备、只留一句提示:value=%q cleared=%v reason=%q", code, f.Value, f.AssetCleared, f.Reason)
		}
	}
}

// 原来的误报:大表 Z1 上次用过 Z1 这一格,这次小表 Z2 挂在这一格上,
// 读数完全正确也会被说成"比上一次小"。按表比之后不该再报。
func TestCorrectReadingInBorrowedSlotIsNotFlagged(t *testing.T) {
	store := zihanMeterStore(t, zihanHistory)
	// 上一条记录:Z1 那一格记的是 Z1 的 203904.5 —— 按格子比时它就是 Z1 格的基准
	prev := zihanMeterRecord(t, "rec_prev", map[string]string{"z1_reading": "203904.50", "z2_reading": "116776.64"})
	prev.Submitted = true
	prev.CreatedAt = time.Now().Add(-24 * time.Hour)
	if err := store.CreateRecord(prev); err != nil {
		t.Fatal(err)
	}
	rec := zihanMeterRecord(t, "rec_now", map[string]string{
		"z1_reading": "116895.19", // Z2 的正确读数,挂在 Z1 那一格
		"z2_reading": "205087.54", // Z1 的正确读数,挂在 Z2 那一格
	})
	recField(t, rec, "z1_reading").AssetName = "Z2"
	recField(t, rec, "z2_reading").AssetName = "Z1"
	if issues := flagImplausibleReadings(store, rec); len(issues) != 0 {
		t.Fatalf("读数都是对的,却报了:%v", issues)
	}
}

// 小数点右移一位、表又没走字:正好 10 倍,也要拦下。
func TestExactlyTenTimesIsFlagged(t *testing.T) {
	store := zihanMeterStore(t, zihanHistory)
	rec := zihanMeterRecord(t, "rec_x10", map[string]string{"z2_reading": "1167766.4"})
	issues := flagImplausibleReadings(store, rec)
	if len(issues) != 1 || recField(t, rec, "z2_reading").Value != "" {
		t.Fatalf("整整 10 倍没拦下:%v", issues)
	}
}

// 现场发现挂错表,把读数挪到另一格:理由要换成和新那块表比的,不能还挂着旧表的。
func TestMovedReadingIsRecheckedAgainstNewMeter(t *testing.T) {
	srv, r, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	for name, hs := range zihanHistory {
		id := "紫菡雅集::zihan_energy::" + name
		_ = store.CreateAsset(&AssetEntry{ID: id, TenantID: defaultTenantID, Project: "紫菡雅集",
			TemplateID: "zihan_energy", AssetKey: name, AssetName: name, AssetType: "电表"})
		for i, h := range hs {
			v := h[1].(float64)
			_ = store.WriteAssetSnapshots(nil, []*FieldObservation{{AssetID: id, RecordID: "h" + name + string(rune('a'+i)),
				FieldKey: h[0].(string), ValueNumber: &v, CreatedAt: time.Date(2026, 9, 21+i, 10, 0, 0, 0, time.UTC)}})
		}
	}
	rec := zihanMeterRecord(t, "rec_move", map[string]string{"z1_reading": "11689.519"})
	flagImplausibleReadings(store, rec)
	if !strings.Contains(recField(t, rec, "z1_reading").Reason, sanityNoteMark) {
		t.Fatal("前提不成立:Z1 格应先被拦下")
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/inspection/records/rec_move/move-reading",
		strings.NewReader(`{"fromCode":"z1_reading","toCode":"z2_reading"}`))
	req.Header = r.Header.Clone()
	w := httptest.NewRecorder()
	srv.handleMoveReading(w, req, "rec_move")
	if w.Code != http.StatusOK {
		t.Fatalf("挪格子失败:%d %s", w.Code, w.Body.String())
	}
	got, _ := store.GetRecord(defaultTenantID, "rec_move")
	f, _ := fieldByCode(got.Fields, "z2_reading")
	// 挪到 Z2 格:按 Z2 再比一次,还是对不上 → 清空,提示照照片填(这回设备是人定的,不再让选设备)
	if f.Value != "" || f.Reason != sanityNoteMark+"AI 读作 11689.519,请照照片填写" || !f.NeedsReview {
		t.Errorf("应按 Z2 重新检查并要求复核,得到 value=%q reason=%q needsReview=%v", f.Value, f.Reason, f.NeedsReview)
	}
}

// ===== 走得慢的表:按"以往一天最多走多少"再拦一道 =====
//
// 10 倍的量级检查管不住消防水表:2026-10-09 实测,AI 把生活水表照片里的 211 放进了
// 消防水表那一格,211 只是 104 的 2 倍 —— 可消防水表以往一周都不走。

// 按设备建好历史读数(线上 9/21 ~ 10/08 的真实值,10/08 那格是改正后的)
func meterHistoryStore(t *testing.T) *MemStore {
	t.Helper()
	store := NewMemStore()
	days := []time.Time{
		time.Date(2026, 9, 21, 16, 54, 0, 0, time.UTC), time.Date(2026, 9, 22, 11, 45, 0, 0, time.UTC),
		time.Date(2026, 9, 23, 17, 24, 0, 0, time.UTC), time.Date(2026, 9, 30, 8, 38, 0, 0, time.UTC),
		time.Date(2026, 10, 8, 13, 51, 0, 0, time.UTC),
	}
	history := map[string]struct {
		typ, code string
		vals      []float64
	}{
		"消防水表": {"水表", "fire_water_reading", []float64{104, 104, 104, 104, 104}},
		"生活水表": {"水表", "living_water_reading", []float64{2002, 2002, 2008, 2047, 2119}},
		"Z1":   {"电表", "z1_reading", []float64{203551.94, 203712.26, 203904.50, 205087.54, 206171.68}},
	}
	for name, h := range history {
		id := "紫菡雅集::zihan_energy::" + name
		if err := store.CreateAsset(&AssetEntry{ID: id, TenantID: defaultTenantID, Project: "紫菡雅集",
			TemplateID: "zihan_energy", AssetKey: name, AssetName: name, AssetType: h.typ}); err != nil {
			t.Fatal(err)
		}
		for i, v := range h.vals {
			v := v
			if err := store.WriteAssetSnapshots(nil, []*FieldObservation{{AssetID: id, RecordID: "hist_" + strconv.Itoa(i),
				FieldKey: h.code, ValueNumber: &v, CreatedAt: days[i]}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return store
}

func readingAt(t *testing.T, store Store, at time.Time, code, asset, value string) *FieldValue {
	t.Helper()
	rec := zihanMeterRecord(t, "rec_new", map[string]string{code: value})
	rec.CreatedAt = at
	// 按格子名推出来的默认表(刚识别完就是这样),不是人选的 —— 人选的不会被自动认表挪动
	f := recField(t, rec, code)
	f.AssetName, f.AssetDefaulted = asset, true
	flagImplausibleReadings(store, rec)
	return recField(t, rec, code)
}

func TestSlowMeterBigRiseIsCleared(t *testing.T) {
	store := meterHistoryStore(t)
	next := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	f := readingAt(t, store, next, "fire_water_reading", "消防水表", "211")
	if f.Value != "" || f.Reason != sanityNoteMark+"AI 读作 211,请选择设备" {
		t.Errorf("消防水表一周涨 107、和生活水表也对不上 → 应清空:value=%q reason=%q", f.Value, f.Reason)
	}
}

func TestNormalRisesAreKept(t *testing.T) {
	store := meterHistoryStore(t)
	week := time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)
	twoMonths := time.Date(2026, 12, 8, 10, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		at                 time.Time
		code, asset, value string
	}{
		{week, "fire_water_reading", "消防水表", "105"},         // 偶尔正常用一点
		{week, "living_water_reading", "生活水表", "2190"},      // 一周 71 吨,和以往差不多
		{week, "z1_reading", "Z1", "207250.12"},             // 一周 1078 度
		{twoMonths, "z1_reading", "Z1", "216300.5"},         // 隔两个月没抄
		{twoMonths, "living_water_reading", "生活水表", "2700"}, // 隔两个月没抄
	} {
		if f := readingAt(t, store, c.at, c.code, c.asset, c.value); f.Value != c.value {
			t.Errorf("%s %s 是正常读数,不该清空:value=%q reason=%q", c.asset, c.value, f.Value, f.Reason)
		}
	}
}

// ===== 识别后先认表 =====
//
// 2026-10-11 紫菡:拍照顺序和格子顺序不一样。Z4、Z3 读对了却落在 Z1、Z2 两格;
// 落在 Z3、Z4 两格的是 Z1、Z2 的照片,小数点又各错一位。原来四格全被清空。
func TestAutoMatchPutsReadingsOnTheRightMeter(t *testing.T) {
	store := NewMemStore()
	d := func(day int) time.Time { return time.Date(2026, 10, day, 10, 0, 0, 0, time.UTC) }
	for name, vals := range map[string][2]float64{
		"Z1": {206171.68, 206317.78}, "Z2": {116969.15, 116978.84},
		"Z3": {86025.608, 86122.408}, "Z4": {61873.044, 61904.564},
	} {
		id := "紫菡雅集::zihan_energy::" + name
		if err := store.CreateAsset(&AssetEntry{ID: id, TenantID: defaultTenantID, Project: "紫菡雅集",
			TemplateID: "zihan_energy", AssetKey: name, AssetName: name, AssetType: "电表"}); err != nil {
			t.Fatal(err)
		}
		for i, v := range vals {
			v := v
			_ = store.WriteAssetSnapshots(nil, []*FieldObservation{{AssetID: id, RecordID: "h" + name + strconv.Itoa(i),
				FieldKey: "z1_reading", ValueNumber: &v, CreatedAt: d(8 + i)}})
		}
	}
	rec := zihanMeterRecord(t, "rec_1011", map[string]string{
		"z1_reading": "61922.564", // 其实是 Z4 的照片,读对了
		"z2_reading": "86217.840", // 其实是 Z3 的照片,读对了
		"z3_reading": "20647.632", // 其实是 Z1 的照片,小数点错一位
		"z4_reading": "11698.725", // 其实是 Z2 的照片,小数点错一位
	})
	rec.CreatedAt = d(11)
	flagImplausibleReadings(store, rec)

	for code, want := range map[string][2]string{"z1_reading": {"Z4", "61922.564"}, "z2_reading": {"Z3", "86217.840"}} {
		if f := recField(t, rec, code); f.AssetName != want[0] || f.Value != want[1] || f.NeedsReview {
			t.Errorf("%s 应自动挂到 %s、读数保留:asset=%q value=%q review=%v reason=%q",
				code, want[0], f.AssetName, f.Value, f.NeedsReview, f.Reason)
		}
	}
	for code, ai := range map[string]string{"z3_reading": "20647.632", "z4_reading": "11698.725"} {
		f := recField(t, rec, code)
		if f.Value != "" || f.AssetName != "" || !f.AssetCleared || f.Reason != sanityNoteMark+"AI 读作 "+ai+",请选择设备" {
			t.Errorf("%s 和哪块表都对不上,应清空并请人选设备:asset=%q value=%q reason=%q", code, f.AssetName, f.Value, f.Reason)
		}
	}
}

// 人选过的表不替他改:读数和 Z2 对得上,但人选的是 Z1 —— 不挪,按 Z1 对不上清空,请人照照片填
func TestAutoMatchLeavesHumanChosenMeterAlone(t *testing.T) {
	store := zihanMeterStore(t, zihanHistory)
	rec := zihanMeterRecord(t, "rec_h", map[string]string{"z1_reading": "116800.5"})
	f := recField(t, rec, "z1_reading")
	f.AssetName = "Z1" // 人选的(识别不会写设备名)
	flagImplausibleReadings(store, rec)
	if f.AssetName != "Z1" || f.Value != "" || f.Reason != sanityNoteMark+"AI 读作 116800.5,请照照片填写" {
		t.Errorf("人选的表不该被挪走,读数按 Z1 清空:asset=%q value=%q reason=%q", f.AssetName, f.Value, f.Reason)
	}
}
