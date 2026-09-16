package main

import (
	"testing"
	"time"
)

// ===== 一条记录派生多台设备时,每台要用自己那张照片 =====
//
// 【线上现象】台账里紫菡那六台表(四块电表两块水表)的卡片长得一模一样,
// 全是同一张水表照片 —— 电表那几台看着就像贴错了图。
//
// 原因:buildAssetEntry 写死了 LastPhotoPath = rec.Images[0],
// 六台设备是同一条记录派生的,于是全拿第一张。实测六台的 lastPhotoPath
// 是同一个文件名。
//
// 而映射本来就有:每个读数字段上记着它是从哪张照片读出来的(SourceImageID),
// 那是确认页上人一张一张认过的。只是建台账时没用。

func meterRecordWithPhotos() *Record {
	imgs := []ImageInfo{
		{ID: "img1", Path: "/uploads/r/1.jpg"},
		{ID: "img2", Path: "/uploads/r/2.jpg"},
		{ID: "img3", Path: "/uploads/r/3.jpg"},
		{ID: "img4", Path: "/uploads/r/4.jpg"},
		{ID: "img5", Path: "/uploads/r/5.jpg"},
		{ID: "img6", Path: "/uploads/r/6.jpg"},
	}
	// 故意不按顺序:消防水表是第 1 张,Z1 是第 3 张 —— 和线上那条记录一样。
	fields := []FieldValue{
		{Code: "fire_water_reading", Label: "消防水表读数", Value: "60197.92", SourceImageID: "img1"},
		{Code: "living_water_reading", Label: "生活水表读数", Value: "1992", SourceImageID: "img2"},
		{Code: "z1_reading", Label: "Z1能耗表读数", Value: "60199.924", SourceImageID: "img3"},
		{Code: "z2_reading", Label: "Z2能耗表读数", Value: "60200", SourceImageID: "img4"},
		{Code: "z3_reading", Label: "Z3能耗表读数", Value: "60201", SourceImageID: "img5"},
		{Code: "z4_reading", Label: "Z4能耗表读数", Value: "60202", SourceImageID: "img6"},
	}
	return &Record{
		ID: "rec_meter", TenantID: defaultTenantID, TemplateID: "zihan_energy",
		Project: "紫菡雅集", Images: imgs, Fields: fields,
	}
}

func TestEachMeterGetsItsOwnPhoto(t *testing.T) {
	assets := buildZihanEnergyAssets(meterRecordWithPhotos(), time.Now())
	if len(assets) != 6 {
		t.Fatalf("应派生 6 台,得到 %d 台", len(assets))
	}
	want := map[string]string{
		"消防水表":  "/uploads/r/1.jpg",
		"生活水表":  "/uploads/r/2.jpg",
		"Z1能耗表": "/uploads/r/3.jpg",
		"Z2能耗表": "/uploads/r/4.jpg",
		"Z3能耗表": "/uploads/r/5.jpg",
		"Z4能耗表": "/uploads/r/6.jpg",
	}
	seen := map[string]bool{}
	for _, a := range assets {
		if got := a.LastPhotoPath; got != want[a.AssetName] {
			t.Errorf("%s 的封面是 %s,应该是 %s", a.AssetName, got, want[a.AssetName])
		}
		if seen[a.LastPhotoPath] {
			t.Errorf("%s 和别的设备共用同一张照片(%s)—— 台账里会长得一模一样",
				a.AssetName, a.LastPhotoPath)
		}
		seen[a.LastPhotoPath] = true
	}
}

// 老记录没有 SourceImageID,必须退回第一张 —— 退回去至少和改之前一样,
// 不能变成没有封面。
func TestMeterWithoutSourceImageFallsBackToFirst(t *testing.T) {
	rec := meterRecordWithPhotos()
	for i := range rec.Fields {
		rec.Fields[i].SourceImageID = ""
	}
	for _, a := range buildZihanEnergyAssets(rec, time.Now()) {
		if a.LastPhotoPath != "/uploads/r/1.jpg" {
			t.Errorf("%s 没退回第一张,得到 %q", a.AssetName, a.LastPhotoPath)
		}
	}
}

// 指向一张不存在的照片(照片被删过)时也要退回第一张,不能给空封面。
func TestDanglingSourceImageFallsBack(t *testing.T) {
	rec := meterRecordWithPhotos()
	rec.Fields[0].SourceImageID = "img_已删除"
	assets := buildZihanEnergyAssets(rec, time.Now())
	for _, a := range assets {
		if a.AssetName == "消防水表" && a.LastPhotoPath != "/uploads/r/1.jpg" {
			t.Errorf("来源图已删除时没退回第一张,得到 %q", a.LastPhotoPath)
		}
	}
}
