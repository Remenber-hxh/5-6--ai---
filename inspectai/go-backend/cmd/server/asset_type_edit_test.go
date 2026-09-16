package main

import (
	"path/filepath"
	"testing"
)

// ===== 设备类型必须能改,而且改不坏 =====
//
// 【为什么要能改】设备类型决定这台设备挂哪个模板、会不会出现在抄表确认页的
// 候选设备里(fillReadingAssetOptions 按类型字符串精确匹配)。填错一次,
// 原来只能删掉重建 —— 而删掉会连它的巡检历史和二维码一起没了。
// 线上 Z1 建于 2026-09-11,类型填成了"能耗表组",就卡在这儿。

func newAssetStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "at.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestUpdateAssetTypeTakesEffect(t *testing.T) {
	store := newAssetStore(t)
	if err := store.CreateAsset(&AssetEntry{
		ID: "紫菡雅集::zihan_energy::Z1", TenantID: defaultTenantID,
		Project: "紫菡雅集", AssetType: "能耗表组", AssetKey: "Z1", AssetName: "Z1能耗表",
		LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.UpdateAssetMeta(defaultTenantID, "紫菡雅集::zihan_energy::Z1", "", "", "", "电表")
	if err != nil {
		t.Fatalf("改类型失败: %v", err)
	}
	if got.AssetType != "电表" {
		t.Errorf("类型没改过来,还是 %q", got.AssetType)
	}
	// 【别的字段不能被顺手清掉】空字符串的含义是"这一项不改",不是"清空"
	if got.AssetName != "Z1能耗表" {
		t.Errorf("只改类型却把名字弄没了:%q", got.AssetName)
	}
	if got.LastStatus != "正常" {
		t.Errorf("只改类型却把状态弄没了:%q", got.LastStatus)
	}
}

// 不传类型时,原来的类型必须原样留着 —— 改个状态就把类型清空的话,
// 这台设备会悄悄从抄表候选里消失。
func TestEmptyAssetTypeKeepsExisting(t *testing.T) {
	store := newAssetStore(t)
	if err := store.CreateAsset(&AssetEntry{
		ID: "p::t::A", TenantID: defaultTenantID,
		Project: "紫菡雅集", AssetType: "电表", AssetKey: "A", AssetName: "A",
		LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := store.UpdateAssetMeta(defaultTenantID, "p::t::A", "", "异常", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.AssetType != "电表" {
		t.Errorf("只改状态却把设备类型清空了:%q —— 这台设备会从抄表候选里消失", got.AssetType)
	}
}

// 类型必须是模板里配过的。手打一个没人认识的,这台设备从此挂不上模板
// 也选不到,而且不报错 —— 和"项目必须真实存在"是同一条规矩。
func TestUnknownAssetTypeRejected(t *testing.T) {
	for _, at := range []string{"电表", "水表", "能耗表组", "无机房电梯"} {
		if !isKnownAssetType(at) {
			t.Errorf("%s 是模板里配过的类型,却被当成未知", at)
		}
	}
	for _, at := range []string{"随便打的", "电錶", "", "  "} {
		if isKnownAssetType(at) {
			t.Errorf("%q 不该被当成已知类型", at)
		}
	}
}
