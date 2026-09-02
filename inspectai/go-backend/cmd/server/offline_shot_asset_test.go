package main

import (
	"path/filepath"
	"testing"
)

// 照片必须在【拍的时候】就记住是哪台设备。
//
// 在这之前照片是"无主"的:归属由成单那一刻的扫码上下文决定。于是一次巡多台会串 ——
// 扫 A 拍几张、走到 B 扫 B 再拍几张,进"选照片"时全混在一起,而上下文是 B,
// 全选就是全落到 B 上,A 等于没巡。
//
// 更糟的是扫码流程跳过了 AI 场景分类,连"这些照片不像同一个场景"这层
// 兜底提示都没有了 —— 错得悄无声息。
func TestOfflineShotRemembersItsAsset(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "shots.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	mk := func(key, assetID string) *OfflineShot {
		shot := &OfflineShot{
			TenantID: defaultTenantID, Inspector: "巡检员",
			IdempotencyKey: key, FileName: key + ".jpg", SizeBytes: 1024,
			AssetID: assetID,
		}
		saved, _, createErr := store.CreateOfflineShot(shot)
		if createErr != nil {
			t.Fatalf("CreateOfflineShot(%s): %v", key, createErr)
		}
		return saved
	}

	a := mk("k1", "会议中心::escalator::FT-6")
	b := mk("k2", "会议中心::escalator::FT-7")
	manual := mk("k3", "") // 手动路径拍的:没有归属,行为和以前一样

	got, err := store.ListOfflineShots(defaultTenantID, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, s := range got {
		byID[s.ID] = s.AssetID
	}
	if byID[a.ID] != "会议中心::escalator::FT-6" {
		t.Fatalf("FT-6 那张的归属丢了,得到 %q —— 照片存下去又读回来必须还认得自己那台", byID[a.ID])
	}
	if byID[b.ID] != "会议中心::escalator::FT-7" {
		t.Fatalf("FT-7 那张的归属丢了,得到 %q", byID[b.ID])
	}
	if byID[manual.ID] != "" {
		t.Fatalf("手动拍的不该凭空多出归属,得到 %q", byID[manual.ID])
	}

	// 【两台的照片必须分得开】这正是串台的根源:分不开就只能靠"当前上下文"猜
	if byID[a.ID] == byID[b.ID] {
		t.Fatal("两台设备的照片归属相同 —— 选照片时就分不出组,又会串回去")
	}
}
