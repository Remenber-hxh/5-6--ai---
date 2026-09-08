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

// 删草稿【不】把照片放回待处理 —— 删掉就是删掉了。
//
// 【为什么反过来了】原来是放回去的,理由是"照片是复制进记录目录的,原件
// 还在,不能因为删了草稿就让现场拍的东西消失"。听起来合理,用起来不是:
// 退回去的照片重新堆在待处理列表里,人以为没删干净、又去删一遍 ——
// 而他点删除时本来的意思就是"这一趟不要了"。
//
// 现在只把状态标成 discarded、record_id 保持指向那条已删的记录:
// 待处理只看 record_id 是否为空,所以它不会再冒出来;行和文件都还在,
// 真要找回来还能查。
func TestDeleteDraftDoesNotReturnShotsToPending(t *testing.T) {
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

	all := func() []*OfflineShot {
		t.Helper()
		got, err := store.ListOfflineShots("t_a", nil, 0)
		if err != nil {
			t.Fatalf("ListOfflineShots: %v", err)
		}
		return got
	}
	pendingCount := func() int {
		t.Helper()
		n := 0
		for _, s := range all() {
			if shotIsPending(s) {
				n++
			}
		}
		return n
	}

	if got := pendingCount(); got != 0 {
		t.Fatalf("成单后待处理应为空,得到 %d 条", got)
	}

	if err := store.DeleteDraftRecord("t_a", "r1"); err != nil {
		t.Fatalf("删草稿失败: %v", err)
	}

	if got := pendingCount(); got != 0 {
		t.Fatalf("删草稿后照片【不该】回到待处理,实际有 %d 条 —— "+
			"回去的话人会以为没删干净,又去删一遍", got)
	}
	// 【但也不能凭空消失】行还在、文件还在,只是标成了 discarded。
	// 真删掉的话,万一是误删就再也找不回来了。
	list := all()
	if len(list) != 1 || list[0].ID != "s1" {
		t.Fatalf("照片行不该被删,实际 %d 条", len(list))
	}
	if list[0].Status != "discarded" {
		t.Errorf("状态应标成 discarded(便于日后查证),实际 %q", list[0].Status)
	}
}
