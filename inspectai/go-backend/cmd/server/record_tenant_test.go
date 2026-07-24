package main

import (
	"path/filepath"
	"testing"
	"time"
)

// 记录域跨租户隔离(真 SQLite)。读写一起验 —— 资产域的教训:
// 只验读会漏掉「知道 ID 就能改别家数据」的写入洞。
func TestRecordTenantIsolation(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rec_iso.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	mk := func(tenant, id, inspector string) {
		t.Helper()
		if err := store.CreateRecord(&Record{
			ID: id, TenantID: tenant, Inspector: inspector, InspectorUserID: "u_" + tenant,
			Project: "P", TemplateID: "zihan_energy", CreatedAt: time.Now(),
		}); err != nil {
			t.Fatalf("CreateRecord(%s): %v", id, err)
		}
	}
	mk("t_a", "ra", "巡检A")
	mk("t_b", "rb", "巡检B")

	// —— 读:List 各租户只见自己 ——
	aList, err := store.ListRecords("t_a", 100)
	if err != nil || len(aList) != 1 || aList[0].ID != "ra" {
		t.Fatalf("ListRecords(t_a) = %v (err=%v), 期望仅 [ra]", aList, err)
	}
	bList, err := store.ListRecords("t_b", 100)
	if err != nil || len(bList) != 1 || bList[0].ID != "rb" {
		t.Fatalf("ListRecords(t_b) = %v (err=%v), 期望仅 [rb]", bList, err)
	}

	// —— 读:跨租户 Get 等同不存在 ——
	if _, err := store.GetRecord("t_a", "rb"); err == nil {
		t.Error("GetRecord(t_a, rb) 应跨租户不可见,却查到了")
	}

	// —— 读:按归属列表也不能跨租户(租户过滤在归属条件之外) ——
	// 用 t_a 的租户 + t_b 的归属信息去查,必须空
	crossOwner, err := store.ListRecordsByOwner("t_a", "u_t_b", "巡检B", "", 100)
	if err != nil {
		t.Fatalf("ListRecordsByOwner: %v", err)
	}
	if len(crossOwner) != 0 {
		t.Errorf("ListRecordsByOwner 跨租户泄露 %d 条", len(crossOwner))
	}

	// —— 写:拿到别家记录后改租户号也改不动别家的行 ——
	rb, err := store.GetRecord("t_b", "rb")
	if err != nil {
		t.Fatalf("GetRecord(t_b, rb): %v", err)
	}
	forged := *rb
	forged.TenantID = "t_a" // 冒充自己租户去更新别家记录
	forged.Report = "被篡改"
	if err := store.UpdateRecord(&forged); err != nil {
		t.Logf("UpdateRecord 跨租户返回 err=%v(可接受)", err)
	}
	after, err := store.GetRecord("t_b", "rb")
	if err != nil {
		t.Fatalf("GetRecord(t_b, rb) after: %v", err)
	}
	if after.Report == "被篡改" {
		t.Error("跨租户 UpdateRecord 竟改动了别家记录")
	}

	// —— 写:本租户更新正常 ——
	rb.Report = "正常更新"
	if err := store.UpdateRecord(rb); err != nil {
		t.Fatalf("本租户 UpdateRecord 失败: %v", err)
	}
	got, _ := store.GetRecord("t_b", "rb")
	if got.Report != "正常更新" {
		t.Errorf("本租户更新未生效: report=%q", got.Report)
	}
}
