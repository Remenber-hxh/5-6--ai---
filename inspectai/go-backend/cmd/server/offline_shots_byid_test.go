package main

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// 按 ID 取照片,不能受"最近多少张"这种窗口影响。
//
// 原实现是 ListOfflineShots(tenant, nil, 500) 拉最近 500 张建索引再查。照片总数
// 一旦超过 500,选中的旧照片就【静默丢掉】—— 巡检员选了 13 张,成单后记录里
// 只剩 9 张,不报错、不打日志、页面也不提示。照片是巡检的证据,这种丢法最难
// 发现:等有人核对时,现场早就过去了。

func seedShots(t *testing.T, store *SQLiteStore, tenantID string, n int, base time.Time) []string {
	t.Helper()
	ids := make([]string, 0, n)
	for i := range n {
		id := fmt.Sprintf("oshot_%s_%04d", tenantID, i)
		at := base.Add(time.Duration(i) * time.Minute)
		shot := &OfflineShot{
			ID: id, TenantID: tenantID, UserID: "u1", Inspector: "巡检",
			IdempotencyKey: "k_" + id, ImagePath: "/tmp/" + id + ".jpg",
			FileName:   id + ".jpg",
			CapturedAt: at.Format(time.RFC3339),
			ReceivedAt: at.Format(time.RFC3339),
		}
		if _, _, err := store.CreateOfflineShot(shot); err != nil {
			t.Fatalf("CreateOfflineShot(%s): %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

func TestOfflineShotsByIDsIgnoresRecencyWindow(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "shots.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// 600 张 > 原来的 500 窗口。最早的那几张正是当初会被丢掉的。
	base := time.Now().Add(-600 * time.Minute)
	ids := seedShots(t, store, defaultTenantID, 600, base)

	// 挑最早的 3 张 + 最新的 1 张 —— 前三张在旧实现里必然落窗口外
	want := []string{ids[0], ids[1], ids[2], ids[599]}
	got, err := store.OfflineShotsByIDs(defaultTenantID, want)
	if err != nil {
		t.Fatalf("OfflineShotsByIDs: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("选了 %d 张,只取回 %d 张 —— 又在按窗口截断了", len(want), len(got))
	}
	// 顺序必须与传入一致:成单和分类都依赖这个顺序
	for i, id := range want {
		if got[i].ID != id {
			t.Fatalf("第 %d 张应为 %s,得到 %s —— 返回顺序和传入不一致", i, id, got[i].ID)
		}
	}
}

func TestOfflineShotsByIDsRespectsTenant(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "shots2.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	base := time.Now().Add(-time.Hour)
	mine := seedShots(t, store, defaultTenantID, 3, base)
	theirs := seedShots(t, store, "tenant_other", 3, base)

	// 混入别家租户的 ID:必须被当作不存在,而不是返回给我
	got, err := store.OfflineShotsByIDs(defaultTenantID, append(append([]string{}, mine...), theirs...))
	if err != nil {
		t.Fatalf("OfflineShotsByIDs: %v", err)
	}
	if len(got) != len(mine) {
		t.Fatalf("应只返回本租户的 %d 张,得到 %d 张 —— 跨租户泄露", len(mine), len(got))
	}
	for _, s := range got {
		if s.TenantID != defaultTenantID {
			t.Fatalf("返回了别家租户的照片: %s", s.ID)
		}
	}

	// 不存在的 ID 不该报错,静静跳过即可(调用方按返回条数判断)
	got2, err := store.OfflineShotsByIDs(defaultTenantID, []string{mine[0], "oshot_不存在"})
	if err != nil {
		t.Fatalf("含不存在 ID 时报错了: %v", err)
	}
	if len(got2) != 1 {
		t.Fatalf("应返回 1 张,得到 %d 张", len(got2))
	}

	// 空列表:别拼出 `IN ()` 这种非法 SQL
	if _, err := store.OfflineShotsByIDs(defaultTenantID, nil); err != nil {
		t.Fatalf("空 ID 列表报错了: %v", err)
	}
}
