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
	if err := store.DeleteDraftRecord("t_a", "done"); !errors.Is(err, errRecordSubmitted) {
		t.Fatalf("删已提交记录应当被拒,得到 %v", err)
	}
	if rec, err := store.GetRecord("t_a", "done"); err != nil || rec == nil {
		t.Fatal("已提交的记录被删掉了")
	}

	// 跨租户等同不存在 —— 不能因为知道 id 就能删别家的
	if err := store.DeleteDraftRecord("t_a", "other"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("跨租户删除应当 ErrNoRows,得到 %v", err)
	}
	if rec, err := store.GetRecord("t_b", "other"); err != nil || rec == nil {
		t.Fatal("别家租户的记录被删掉了")
	}

	// 不存在的 id
	if err := store.DeleteDraftRecord("t_a", "nope"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("删不存在的记录应当 ErrNoRows,得到 %v", err)
	}

	// 正常路径
	if err := store.DeleteDraftRecord("t_a", "draft"); err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}
	if _, err := store.GetRecord("t_a", "draft"); err == nil {
		t.Fatal("草稿删了还能读到")
	}
}

// 删草稿要把当初认领的离线照片放回待处理 —— 照片是【复制】进记录目录的,
// 原件还在,不能因为删了草稿就让现场拍的东西消失。
func TestDeleteDraftReleasesOfflineShots(t *testing.T) {
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
		IdempotencyKey: "k1", ImagePath: "/tmp/s1.jpg", FileName: "s1.jpg",
		ReceivedAt: time.Now().Format(time.RFC3339),
	}
	if _, _, err := store.CreateOfflineShot(shot); err != nil {
		t.Fatalf("CreateOfflineShot: %v", err)
	}
	if err := store.MarkOfflineShotConsumed("t_a", "s1", "r1"); err != nil {
		t.Fatalf("MarkOfflineShotConsumed: %v", err)
	}

	pending := func() []*OfflineShot {
		t.Helper()
		all, err := store.ListOfflineShots("t_a", "", 0)
		if err != nil {
			t.Fatalf("ListOfflineShots: %v", err)
		}
		out := make([]*OfflineShot, 0, len(all))
		for _, s := range all {
			if shotIsPending(s) {
				out = append(out, s)
			}
		}
		return out
	}

	// 成单后不该算在待处理里
	if got := pending(); len(got) != 0 {
		t.Fatalf("成单后待处理应为空,得到 %d 条", len(got))
	}

	if err := store.DeleteDraftRecord("t_a", "r1"); err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}

	list := pending()
	if len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("照片没有放回待处理,得到 %d 条", len(list))
	}
	if list[0].RecordID != "" {
		t.Fatalf("照片还挂着已删记录的 id: %q", list[0].RecordID)
	}
}
