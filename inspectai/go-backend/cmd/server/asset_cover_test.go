package main

import (
	"path/filepath"
	"testing"
	"time"
)

// ===== 台账卡片上显示的封面,必须是这台设备自己的照片 =====
//
// 【为什么单独测 enrichAssetForDisplay】建台账时 LastPhotoPath 已经按字段挑对了
// (asset_photo_test.go 守着)。但卡片【优先用 CoverImage】,而 CoverImage 是读取时
// 在 enrichAssetForDisplay 里现算的 —— 那里原来写死 rec.Images[0]。
//
// 2026-09-16 只修了建台账那处,接口返回的 lastPhotoPath 六个各不相同,
// 就说"修好了"。实际页面上六张卡片一张没变:显示走的是另一个字段。
// 这条测试守的正是页面真正用的那个字段。

func newCoverServer(t *testing.T) *Server {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "cover.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	rec := &Record{
		ID: "rec_cover", TenantID: defaultTenantID, TemplateID: "zihan_energy",
		Project: "紫菡雅集", CreatedAt: time.Now(),
		Images: []ImageInfo{
			{ID: "img1", Path: "/uploads/rec_cover/1.jpg"},
			{ID: "img2", Path: "/uploads/rec_cover/2.jpg"},
			{ID: "img3", Path: "/uploads/rec_cover/3.jpg"},
		},
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	return &Server{store: store, storeKind: "sqlite"}
}

func TestCoverFollowsAssetsOwnPhoto(t *testing.T) {
	s := newCoverServer(t)
	a := &AssetEntry{
		ID: "紫菡雅集::zihan_energy::z1_energy_meter", TenantID: defaultTenantID,
		AssetName: "Z1能耗表", LastRecordID: "rec_cover",
		LastPhotoPath: "/uploads/rec_cover/3.jpg", // Z1 读数来自第 3 张
	}
	s.enrichAssetForDisplay(a)
	if a.CoverImage == nil {
		t.Fatal("没有封面")
	}
	if a.CoverImage.Path != "/uploads/rec_cover/3.jpg" {
		t.Errorf("封面是 %s,应该是它自己的第 3 张 —— 卡片会显示成别的设备的照片",
			a.CoverImage.Path)
	}
	if a.CoverImage.ID != "img3" {
		t.Errorf("封面没带上原图的 ID(得到 %q),点开大图时对不上", a.CoverImage.ID)
	}
}

// 同一条记录派生的两台设备,封面必须不同。
func TestTwoAssetsFromOneRecordGetDifferentCovers(t *testing.T) {
	s := newCoverServer(t)
	z1 := &AssetEntry{ID: "a1", TenantID: defaultTenantID, LastRecordID: "rec_cover",
		LastPhotoPath: "/uploads/rec_cover/3.jpg"}
	water := &AssetEntry{ID: "a2", TenantID: defaultTenantID, LastRecordID: "rec_cover",
		LastPhotoPath: "/uploads/rec_cover/1.jpg"}
	s.enrichAssetForDisplay(z1)
	s.enrichAssetForDisplay(water)
	if z1.CoverImage.Path == water.CoverImage.Path {
		t.Errorf("两台设备封面相同(%s)—— 台账卡片会长得一模一样", z1.CoverImage.Path)
	}
}

// 它的照片不在最近这条记录里(来自更早那次)时,仍用它自己的那张 ——
// 不能退回这条记录的第一张,那张是别的设备的。
func TestCoverUsesOwnPhotoEvenFromOlderRecord(t *testing.T) {
	s := newCoverServer(t)
	a := &AssetEntry{ID: "a3", TenantID: defaultTenantID, LastRecordID: "rec_cover",
		LastPhotoPath: "/uploads/rec_old/9.jpg"}
	s.enrichAssetForDisplay(a)
	if a.CoverImage == nil || a.CoverImage.Path != "/uploads/rec_old/9.jpg" {
		t.Errorf("没用它自己的照片,退回了 %+v", a.CoverImage)
	}
}

// 老数据没有 LastPhotoPath 时退回第一张,和改之前一样,不能变成没封面。
func TestCoverFallsBackToFirstWhenNoOwnPhoto(t *testing.T) {
	s := newCoverServer(t)
	a := &AssetEntry{ID: "a4", TenantID: defaultTenantID, LastRecordID: "rec_cover"}
	s.enrichAssetForDisplay(a)
	if a.CoverImage == nil || a.CoverImage.Path != "/uploads/rec_cover/1.jpg" {
		t.Errorf("没有自己的照片时应退回第一张,得到 %+v", a.CoverImage)
	}
}

// 人工换过的标准图永远优先,这条不能被改坏。
func TestManualCoverStillWins(t *testing.T) {
	s := newCoverServer(t)
	a := &AssetEntry{ID: "a5", TenantID: defaultTenantID, LastRecordID: "rec_cover",
		CoverImagePath: "/uploads/manual/std.jpg", LastPhotoPath: "/uploads/rec_cover/3.jpg"}
	s.enrichAssetForDisplay(a)
	if a.CoverImage == nil || a.CoverImage.Path != "/uploads/manual/std.jpg" {
		t.Errorf("人工标准图被覆盖了,得到 %+v", a.CoverImage)
	}
}
