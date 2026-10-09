package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// ===== 历次巡检照片:一条记录拍几台设备时,按这台设备挑 =====
//
// 2026-10-09 线上:Z3 的设备档案里,9/23、9/30 两次排出来的是水表 ——
// 抄表一条记录拍六块表,原来每次一律取第一张。

func assetPhotosOf(t *testing.T, store Store, assetID string) []assetPhoto {
	t.Helper()
	s := &Server{store: store}
	w := httptest.NewRecorder()
	s.handleAssetPhotos(w, httptest.NewRequest(http.MethodGet, "/api/assets/x/photos", nil), assetID)
	if w.Code != http.StatusOK {
		t.Fatalf("取照片失败:%d %s", w.Code, w.Body.String())
	}
	var body struct {
		Photos []assetPhoto `json:"photos"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.Photos
}

func TestMeterPhotoTimelineShowsOnlyThatMeter(t *testing.T) {
	store := NewMemStore()
	const z3 = "紫菡雅集::zihan_energy::Z3"
	if err := store.CreateAsset(&AssetEntry{ID: z3, TenantID: defaultTenantID, Project: "紫菡雅集",
		TemplateID: "zihan_energy", AssetType: "电表", AssetKey: "Z3", AssetName: "Z3"}); err != nil {
		t.Fatal(err)
	}
	energy := func(id string, day int, z3Image string) *Record {
		rec := zihanMeterRecord(t, id, map[string]string{"z3_reading": "86025.608", "living_water_reading": "2119"})
		rec.Submitted = true
		// 第一张是水表 —— 原来每次都取到它
		rec.Images = []ImageInfo{{ID: "img_water_" + id, Path: "water_" + id + ".jpg"}, {ID: "img_z3_" + id, Path: "z3_" + id + ".jpg"}}
		recField(t, rec, "living_water_reading").SourceImageID = "img_water_" + id
		z := recField(t, rec, "z3_reading")
		z.AssetName = "Z3"
		z.SourceImageID = z3Image
		if err := store.CreateRecord(rec); err != nil {
			t.Fatal(err)
		}
		if err := store.WriteAssetSnapshots([]*AssetSnapshot{{AssetID: z3, RecordID: id, Status: "正常",
			CreatedAt: time.Date(2026, 9, day, 10, 0, 0, 0, time.UTC)}}, nil); err != nil {
			t.Fatal(err)
		}
		return rec
	}
	energy("rec_0923", 23, "img_z3_rec_0923")
	energy("rec_0930", 30, "img_z3_rec_0930")
	energy("rec_0921", 21, "") // 这一次 Z3 那格没认领照片

	got := assetPhotosOf(t, store, z3)
	if len(got) != 2 {
		t.Fatalf("应有 2 张(没认领照片的那次跳过),得到 %+v", got)
	}
	for _, p := range got {
		if p.Path != "z3_"+p.RecordID+".jpg" {
			t.Errorf("%s 排出来的不是 Z3 的照片:%s", p.RecordID, p.Path)
		}
	}
}

// 一条记录只拍一台设备的,照旧取第一张。
func TestSingleAssetPhotoTimelineUnchanged(t *testing.T) {
	store := NewMemStore()
	const pump = "某项目::fire_pump::1号泵"
	if err := store.CreateAsset(&AssetEntry{ID: pump, TenantID: defaultTenantID, Project: "某项目",
		TemplateID: "fire_pump", AssetName: "1号泵"}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"rec_a", "rec_b"} {
		rec := &Record{ID: id, TenantID: defaultTenantID, Project: "某项目", TemplateID: "fire_pump",
			Submitted: true, CreatedAt: time.Now(),
			Images: []ImageInfo{{ID: "i1", Path: id + "_overview.jpg"}, {ID: "i2", Path: id + "_detail.jpg"}}}
		if err := store.CreateRecord(rec); err != nil {
			t.Fatal(err)
		}
		_ = store.WriteAssetSnapshots([]*AssetSnapshot{{AssetID: pump, RecordID: id,
			CreatedAt: time.Date(2026, 9, 20+i, 10, 0, 0, 0, time.UTC)}}, nil)
	}
	got := assetPhotosOf(t, store, pump)
	if len(got) != 2 || got[0].Path != "rec_a_overview.jpg" || got[1].Path != "rec_b_overview.jpg" {
		t.Fatalf("单台设备应照旧取每次的第一张、老的在左:%+v", got)
	}
}
