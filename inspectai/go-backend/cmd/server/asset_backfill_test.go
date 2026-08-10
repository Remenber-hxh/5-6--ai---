package main

import (
	"path/filepath"
	"testing"
	"time"
)

// 启动回填(ensureAssetLedgerFromRecords)在线上造过两类脏数据:
//   一、每次重启都长出重复设备 —— 它直接按 项目::模板::编号 算 ID 再插,
//       而模板是"这次巡检用了哪个模板",AI 认错一次就换一个。手工清理守不住。
//   二、巡检次数永远是 1 —— 新建走 INSERT ... VALUES(..., 1, ...)。
//       线上 K06 显示 1 次、实际 20 次就是这么来的。
// 两条都不会报错、不会崩,只能靠测试守。

func backfillTestServer(t *testing.T) (*Server, *SQLiteStore) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "backfill.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Server{store: store, storageDir: t.TempDir()}, store
}

// 造一条已提交的电梯记录,asset_no 用给定值。
func seedSubmittedRecord(t *testing.T, store *SQLiteStore, id, assetNo string, at time.Time) {
	t.Helper()
	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到有机房电梯模板")
	}
	rec := &Record{
		ID: id, TenantID: defaultTenantID, Project: "会议中心",
		PointID: "p_elevator_machine_room", PointName: "有机房电梯",
		TemplateID: tpl.ID, TemplateName: tpl.Name, Inspector: "胡晓悱",
		Fields: initialFieldValues(tpl, "胡晓悱"), Submitted: true,
		CreatedAt: at, UpdatedAt: at, SubmittedAt: &at,
	}
	for i := range rec.Fields {
		if rec.Fields[i].Code == "asset_no" {
			rec.Fields[i].Value = assetNo
		}
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord(%s): %v", id, err)
	}
}

// 同一台设备的多条记录，回填后台账里只该有一条，且次数=记录数。
func TestBackfillCountsRealRecordsNotOne(t *testing.T) {
	srv, store := backfillTestServer(t)
	base := time.Now().Add(-72 * time.Hour)
	for i := range 5 {
		seedSubmittedRecord(t, store, "rec_k07_"+itoa(i), "K07", base.Add(time.Duration(i)*time.Hour))
	}

	if err := srv.ensureAssetLedgerFromRecords(); err != nil {
		t.Fatalf("ensureAssetLedgerFromRecords: %v", err)
	}
	assets, err := store.ListAssets(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		for _, a := range assets {
			t.Logf("  台账: %s", a.ID)
		}
		t.Fatalf("5 条同一台设备的记录应只回填出 1 台，得到 %d 台", len(assets))
	}
	if assets[0].InspectionCount != 5 {
		t.Fatalf("巡检次数应为真实记录数 5，得到 %d —— 又被写成常量了", assets[0].InspectionCount)
	}
}

// 重启不能长出新设备:连跑两次回填，结果必须完全一样。
// 这正是线上"每次重构后都多几个资产"的形状。
func TestBackfillIsIdempotentAcrossRestarts(t *testing.T) {
	srv, store := backfillTestServer(t)
	base := time.Now().Add(-48 * time.Hour)
	seedSubmittedRecord(t, store, "rec_a", "K07", base)
	seedSubmittedRecord(t, store, "rec_b", "K08", base.Add(time.Hour))

	for round := 1; round <= 3; round++ {
		if err := srv.ensureAssetLedgerFromRecords(); err != nil {
			t.Fatalf("第 %d 次回填: %v", round, err)
		}
		assets, err := store.ListAssets(defaultTenantID)
		if err != nil {
			t.Fatal(err)
		}
		if len(assets) != 2 {
			for _, a := range assets {
				t.Logf("  台账: %s", a.ID)
			}
			t.Fatalf("第 %d 次回填后台账应仍是 2 台，得到 %d 台 —— 重启在长设备", round, len(assets))
		}
	}
}

// 后台手工建的档(模板段是 manual)不能被巡检记录再建一份出来。
// 线上"新建的资产大多数都重了"就是这条。
func TestBackfillReusesManuallyCreatedAsset(t *testing.T) {
	srv, store := backfillTestServer(t)
	manual := &AssetEntry{
		ID: "会议中心::manual::K09", TenantID: defaultTenantID,
		Project: "会议中心", ProjectCode: "会议中心",
		AssetType: "有机房电梯", AssetKey: "K09", AssetName: "K09",
		LastStatus: "未巡检",
	}
	if err := store.CreateAsset(manual); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	seedSubmittedRecord(t, store, "rec_k09", "K09", time.Now().Add(-time.Hour))

	if err := srv.ensureAssetLedgerFromRecords(); err != nil {
		t.Fatalf("ensureAssetLedgerFromRecords: %v", err)
	}
	assets, err := store.ListAssets(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 1 {
		for _, a := range assets {
			t.Logf("  台账: %s", a.ID)
		}
		t.Fatalf("手工建的 K09 被巡检后应仍是 1 台，得到 %d 台", len(assets))
	}
	if assets[0].ID != manual.ID {
		t.Fatalf("应挂回手工建的那台 %q，实际是 %q", manual.ID, assets[0].ID)
	}
}
