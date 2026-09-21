package main

import (
	"path/filepath"
	"testing"
)

// ===== 「选照片」那一屏靠什么判断"这些照片能不能进同一条记录" =====
//
// 那一屏只认得照片上的 AssetID。光靠设备个数是不够的:
//   电梯:一台一条记录 —— 两台混在一起必须拦
//   抄表:一条记录六台表 —— 拦了就等于这条路走不通
// 差别在模板身上,所以后端要把"这台设备归哪个模板、那个模板抄不抄多台"
// 一起给出去。2026-09-21 线上就是因为没有这个信息,抄表在这一屏被卡死。

func newShotAnnotateServer(t *testing.T) *Server {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "shots.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, a := range []*AssetEntry{
		// 抄表:模板字段带设备类型,一条记录覆盖多台
		{ID: "紫菡雅集::zihan_energy::Z1", AssetName: "Z1", AssetType: "电表", TemplateID: "zihan_energy"},
		{ID: "紫菡雅集::zihan_energy::生活水表", AssetName: "生活水表", AssetType: "水表", TemplateID: "zihan_energy"},
	} {
		a.TenantID, a.Project, a.LastStatus = defaultTenantID, "紫菡雅集", "未巡检"
		if err := store.CreateAsset(a); err != nil {
			t.Fatalf("CreateAsset %s: %v", a.AssetName, err)
		}
	}
	return &Server{store: store, storeKind: "sqlite"}
}

func shotWithAsset(id, assetID string) *OfflineShot {
	return &OfflineShot{ID: id, TenantID: defaultTenantID, AssetID: assetID, Status: "pending"}
}

// 抄表的设备要带上 multiDevice —— 这一位就是放行的依据。
func TestShotsOfMeterTemplateAreMarkedMultiDevice(t *testing.T) {
	s := newShotAnnotateServer(t)
	shots := []*OfflineShot{
		shotWithAsset("s1", "紫菡雅集::zihan_energy::Z1"),
		shotWithAsset("s2", "紫菡雅集::zihan_energy::生活水表"),
	}
	s.annotateShotTemplates(defaultTenantID, shots)
	for _, sh := range shots {
		if sh.AssetTemplateID != "zihan_energy" {
			t.Errorf("%s 没带上模板,得到 %q", sh.ID, sh.AssetTemplateID)
		}
		if !sh.MultiDevice {
			t.Errorf("%s 该标成「一条记录抄多台」,却没标 —— 抄表会在选照片那屏被拦住", sh.ID)
		}
	}
}

// 手动拍的照片没有设备,这两个字段就该留空,不能瞎猜一个。
func TestShotsWithoutAssetStayBlank(t *testing.T) {
	s := newShotAnnotateServer(t)
	shots := []*OfflineShot{shotWithAsset("s1", "")}
	s.annotateShotTemplates(defaultTenantID, shots)
	if shots[0].AssetTemplateID != "" || shots[0].MultiDevice {
		t.Errorf("没指定设备的照片被贴上了模板:%+v", shots[0])
	}
}

// 【设备被删了要当"不知道",不是当"可以放行"】
// 标成 multiDevice 的话,两台电梯的照片会因为查不到设备而被放进同一条记录。
func TestShotWithMissingAssetIsNotTrusted(t *testing.T) {
	s := newShotAnnotateServer(t)
	shots := []*OfflineShot{shotWithAsset("s1", "紫菡雅集::zihan_energy::已删掉的表")}
	s.annotateShotTemplates(defaultTenantID, shots)
	if shots[0].MultiDevice || shots[0].AssetTemplateID != "" {
		t.Errorf("查不到的设备被当成了可放行:%+v", shots[0])
	}
}

// 一张设备都没有时不该去查台账 —— 这一屏一次几十张照片,多数是手动拍的。
// 用一个会在被调用时失败的 store 来证明"根本没调"。
func TestNoAssetsMeansNoLedgerQuery(t *testing.T) {
	s := &Server{store: nil, storeKind: "sqlite"} // store 为 nil:一旦查库就 panic
	shots := []*OfflineShot{shotWithAsset("s1", ""), shotWithAsset("s2", "")}
	s.annotateShotTemplates(defaultTenantID, shots) // 不 panic 就说明没查
}
