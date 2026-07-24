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
