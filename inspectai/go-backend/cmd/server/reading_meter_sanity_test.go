package main

import (
	"net/http"
	"net/http/httptest"
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

// 9/30 那次:照片对调 + 小数点偏一位。两格都要拦下来,而且说清楚是和哪块表比的。
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
	if r := recField(t, rec, "z1_reading").Reason; !strings.Contains(r, "Z1 上一次的 203904.5") {
		t.Errorf("Z1 格应和 Z1 自己的上一次比,理由是:%s", r)
	}
	if r := recField(t, rec, "z2_reading").Reason; !strings.Contains(r, "Z2 上一次的 116776.64") {
		t.Errorf("Z2 格应和 Z2 自己的上一次比,理由是:%s", r)
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
	if len(issues) != 1 || !strings.Contains(issues[0].Reason, "10 倍") {
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
	if !strings.Contains(recField(t, rec, "z1_reading").Reason, "Z1 上一次") {
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
	if strings.Contains(f.Reason, "Z1 上一次") {
		t.Errorf("挪到 Z2 格后理由还在说 Z1:%s", f.Reason)
	}
	if !strings.Contains(f.Reason, "Z2 上一次的 116776.64") || !f.NeedsReview {
		t.Errorf("应按 Z2 重新检查并要求复核,得到 reason=%q needsReview=%v", f.Reason, f.NeedsReview)
	}
}
