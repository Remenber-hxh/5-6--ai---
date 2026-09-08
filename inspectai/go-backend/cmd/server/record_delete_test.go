package main

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// 删草稿的边界(真 SQLite)。这几条每一条错了都会造成真实损失:
// 删掉已提交记录 = 台账对不上账;跨租户能删 = 越权;照片被销毁 = 丢证据。
func TestDeleteDraftRecordBoundaries(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rec_del.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	mk := func(tenant, id string, submitted bool) {
		t.Helper()
		if err := store.CreateRecord(&Record{
			ID: id, TenantID: tenant, Inspector: "巡检", InspectorUserID: "u1",
			Project: "P", TemplateID: "zihan_energy", Submitted: submitted,
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateRecord(%s): %v", id, err)
		}
	}
	mk("t_a", "draft", false)
	mk("t_a", "done", true)
	mk("t_b", "other", false)

	// 已提交的不能删 —— 它进了台账,还写了资产快照和字段观测
	if _, err := store.DeleteDraftRecord("t_a", "done"); !errors.Is(err, errRecordSubmitted) {
		t.Fatalf("删已提交记录应当被拒,得到 %v", err)
	}
	if rec, err := store.GetRecord("t_a", "done"); err != nil || rec == nil {
		t.Fatal("已提交的记录被删掉了")
	}

	// 跨租户等同不存在 —— 不能因为知道 id 就能删别家的
	if _, err := store.DeleteDraftRecord("t_a", "other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("跨租户删除应当 ErrNoRows,得到 %v", err)
	}
	if rec, err := store.GetRecord("t_b", "other"); err != nil || rec == nil {
		t.Fatal("别家租户的记录被删掉了")
	}

	// 不存在的 id
	if _, err := store.DeleteDraftRecord("t_a", "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删不存在的记录应当 ErrNoRows,得到 %v", err)
	}

	// 正常路径
	if _, err := store.DeleteDraftRecord("t_a", "draft"); err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}
	if _, err := store.GetRecord("t_a", "draft"); err == nil {
		t.Fatal("草稿删了还能读到")
	}
}

// 删草稿 = 连照片一起真删掉。
//
// 【这条前后翻过两次,都是因为"删了但东西还在"】
//
//	一版把照片退回「待处理」—— 它们重新堆在列表里,人以为没删干净,
//	  又去删一遍,而他点删除时的意思就是"这一趟不要了";
//	二版只标 discarded 留着 —— 行和文件继续占地方,越攒越多。
//
// 现在按人真正的意思来:行删掉,文件路径交给上层去删磁盘。
func TestDeleteDraftReallyDeletesShots(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rec_del_shots.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if err := store.CreateRecord(&Record{
		ID: "r1", TenantID: "t_a", Inspector: "巡检", InspectorUserID: "u1",
		Project: "P", TemplateID: "zihan_energy", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	shot := &OfflineShot{
		ID: "s1", TenantID: "t_a", UserID: "u1", Inspector: "巡检",
		IdempotencyKey: "k1", ImagePath: "/srv/storage/offline/t_a/s1.jpg", FileName: "s1.jpg",
		ReceivedAt: time.Now().Format(time.RFC3339),
	}
	if _, _, err := store.CreateOfflineShot(shot); err != nil {
		t.Fatalf("CreateOfflineShot: %v", err)
	}
	if err := store.MarkOfflineShotConsumed("t_a", "s1", "r1"); err != nil {
		t.Fatalf("MarkOfflineShotConsumed: %v", err)
	}

	paths, err := store.DeleteDraftRecord("t_a", "r1")
	if err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}

	// 【路径必须回出来】行删掉之后就再也不知道该删哪些文件了 ——
	// 漏了的话磁盘上的照片永远留着,而库里查不到,谁也不会去清。
	if len(paths) != 1 || paths[0] != "/srv/storage/offline/t_a/s1.jpg" {
		t.Fatalf("应返回待删的图片路径,实际 %v", paths)
	}

	left, err := store.ListOfflineShots("t_a", nil, 0)
	if err != nil {
		t.Fatalf("ListOfflineShots: %v", err)
	}
	if len(left) != 0 {
		t.Fatalf("照片行应被真删掉,实际还剩 %d 条(状态 %q)", len(left), left[0].Status)
	}
}

// 内存版和 SQLite 版必须一致 —— 两边行为不同的话,测试里跑通的路
// 到线上是另一回事。
func TestMemStoreDeleteDraftMatchesSQLite(t *testing.T) {
	store := NewMemStore()
	if err := store.CreateRecord(&Record{
		ID: "r1", TenantID: defaultTenantID, Inspector: "巡检", InspectorUserID: "u1",
		TemplateID: "zihan_energy", CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	shot := &OfflineShot{
		ID: "s1", TenantID: defaultTenantID, UserID: "u1", Inspector: "巡检",
		IdempotencyKey: "k1", ImagePath: "/srv/storage/offline/x/s1.jpg", FileName: "s1.jpg",
		ReceivedAt: time.Now().Format(time.RFC3339),
	}
	if _, _, err := store.CreateOfflineShot(shot); err != nil {
		t.Fatalf("CreateOfflineShot: %v", err)
	}
	if err := store.MarkOfflineShotConsumed(defaultTenantID, "s1", "r1"); err != nil {
		t.Fatalf("MarkOfflineShotConsumed: %v", err)
	}

	paths, err := store.DeleteDraftRecord(defaultTenantID, "r1")
	if err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}
	if len(paths) != 1 {
		t.Fatalf("内存版也要返回待删路径,实际 %v", paths)
	}
	left, _ := store.ListOfflineShots(defaultTenantID, nil, 0)
	if len(left) != 0 {
		t.Fatalf("内存版照片行也该真删掉,实际还剩 %d 条", len(left))
	}
}
