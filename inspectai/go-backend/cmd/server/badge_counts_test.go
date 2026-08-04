package main

import (
	"path/filepath"
	"testing"
	"time"
)

// 角标计数必须和列表页看到的条数一致。
//
// 这个测试是为一个真实踩过的坑立的:第一版实现是
// ListOfflineShots(tenant, user, 0) 再逐条筛未成单。两层问题叠加 ——
//   1. limit<=0 被 store 当成"默认 100"
//   2. 筛在 LIMIT 之后
// 于是最新 100 条里成单的多时,未成单的被截没:页面显示 20 张,角标报 6。
// 角标和用户点进去看到的条数对不上,比不显示更糟。
func TestCountPendingNotTruncatedByDefaultLimit(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "badge.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	// 150 张成单的(时间较新)+ 20 张未成单的(时间较旧)。
	// 按 captured_at DESC 排,未成单的全在 100 条窗口之外 —— 正是当初漏掉的形状。
	base := time.Now()
	mk := func(id string, consumed bool, at time.Time) {
		t.Helper()
		shot := &OfflineShot{
			ID: id, TenantID: "t_a", UserID: "u1", Inspector: "巡检",
			IdempotencyKey: "k_" + id, ImagePath: "/tmp/" + id + ".jpg",
			FileName:   id + ".jpg",
			CapturedAt: at.Format(time.RFC3339),
			ReceivedAt: at.Format(time.RFC3339),
		}
		if _, _, err := store.CreateOfflineShot(shot); err != nil {
			t.Fatalf("CreateOfflineShot(%s): %v", id, err)
		}
		if consumed {
			if err := store.MarkOfflineShotConsumed("t_a", id, "rec_x"); err != nil {
				t.Fatalf("MarkOfflineShotConsumed(%s): %v", id, err)
			}
		}
	}
	for i := range 150 {
		mk("done_"+itoa(i), true, base.Add(time.Duration(i)*time.Minute))
	}
	for i := range 20 {
		mk("pend_"+itoa(i), false, base.Add(-time.Duration(i+1)*time.Hour))
	}

	n, err := store.CountPendingOfflineShots("t_a", "")
	if err != nil {
		t.Fatalf("CountPendingOfflineShots: %v", err)
	}
	if n != 20 {
		t.Fatalf("角标应当数出 20 张未成单,得到 %d —— 又被默认 limit 截了", n)
	}

	// 列表页也一样:过滤必须在 LIMIT 之前
	list, err := store.ListPendingOfflineShots("t_a", "", 200)
	if err != nil {
		t.Fatalf("ListPendingOfflineShots: %v", err)
	}
	if len(list) != 20 {
		t.Fatalf("列表应当返回 20 张未成单,得到 %d", len(list))
	}
	for _, s := range list {
		if !shotIsPending(s) {
			t.Fatalf("列表里混进了已成单的照片: %s", s.ID)
		}
	}

	// 两者必须相等 —— 这才是"角标和页面对得上"的真正含义
	if n != len(list) {
		t.Fatalf("角标 %d 与列表 %d 对不上", n, len(list))
	}

	// 跨租户不能数到别家的
	if other, err := store.CountPendingOfflineShots("t_b", ""); err != nil || other != 0 {
		t.Fatalf("跨租户计数应为 0,得到 %d (err=%v)", other, err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}
