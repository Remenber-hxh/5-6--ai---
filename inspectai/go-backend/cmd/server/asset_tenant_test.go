package main

import (
	"path/filepath"
	"testing"
)

// 资产域跨租户隔离(真 SQLite,验证 SQL 层 WHERE tenant_id 实际生效):
// 两个租户各建一个资产,List 只见自己、跨租户 Get 等同不存在。
func TestAssetTenantIsolation(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "iso.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	mk := func(tenant, id string) {
		t.Helper()
		if err := store.CreateAsset(&AssetEntry{
			ID: id, TenantID: tenant, Project: "P", AssetKey: id, AssetName: id, LastStatus: "正常",
		}); err != nil {
			t.Fatalf("CreateAsset(%s): %v", id, err)
		}
	}
	mk("t_a", "a1")
	mk("t_b", "b1")

	// List:各租户只见自己的资产
	aList, err := store.ListAssets("t_a")
	if err != nil || len(aList) != 1 || aList[0].ID != "a1" {
		t.Fatalf("ListAssets(t_a) = %v (err=%v), 期望仅 [a1]", aList, err)
	}
	bList, err := store.ListAssets("t_b")
	if err != nil || len(bList) != 1 || bList[0].ID != "b1" {
		t.Fatalf("ListAssets(t_b) = %v (err=%v), 期望仅 [b1]", bList, err)
	}

	// 跨租户 Get:等同不存在
	if _, err := store.GetAsset("t_a", "b1"); err == nil {
		t.Error("GetAsset(t_a, b1) 应跨租户不可见,却查到了")
	}
	// 本租户 Get:正常可见,且带回正确租户
	got, err := store.GetAsset("t_b", "b1")
	if err != nil || got == nil || got.TenantID != "t_b" {
		t.Errorf("GetAsset(t_b, b1) = %v (err=%v), 期望本租户可见且 TenantID=t_b", got, err)
	}
}

// 跨租户「写」隔离。此前 handlePatchAsset 未做前置校验、直接调 UpdateAssetMeta,
// 租户 A 可改租户 B 的资产 —— 该洞由本用例锁死:三个 mutation 一律在 Store 层按租户过滤。
func TestAssetTenantIsolationOnMutations(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "iso_w.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if err := store.CreateAsset(&AssetEntry{
		ID: "b1", TenantID: "t_b", Project: "P", AssetKey: "b1", AssetName: "原名", LastStatus: "异常",
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	// 租户 A 改租户 B 的资产 → 必须失败
	if _, err := store.UpdateAssetMeta("t_a", "b1", "被篡改", "正常", "x"); err == nil {
		t.Error("UpdateAssetMeta(t_a, b1) 跨租户写入竟成功了")
	}
	if _, err := store.UpdateAssetCover("t_a", "b1", "/evil.jpg"); err == nil {
		t.Error("UpdateAssetCover(t_a, b1) 跨租户写入竟成功了")
	}
	if err := store.DeleteAsset("t_a", "b1"); err == nil {
		t.Error("DeleteAsset(t_a, b1) 跨租户删除竟成功了")
	}

	// 数据未被动过
	got, err := store.GetAsset("t_b", "b1")
	if err != nil || got == nil {
		t.Fatalf("GetAsset(t_b, b1): %v", err)
	}
	if got.AssetName != "原名" || got.LastStatus != "异常" || got.CoverImagePath != "" {
		t.Errorf("资产被跨租户改动了: name=%q status=%q cover=%q",
			got.AssetName, got.LastStatus, got.CoverImagePath)
	}

	// 本租户操作正常
	if _, err := store.UpdateAssetMeta("t_b", "b1", "新名", "", ""); err != nil {
		t.Errorf("本租户 UpdateAssetMeta 失败: %v", err)
	}
	if err := store.DeleteAsset("t_b", "b1"); err != nil {
		t.Errorf("本租户 DeleteAsset 失败: %v", err)
	}
}
