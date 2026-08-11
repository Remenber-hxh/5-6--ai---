package main

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// 审批列表的「设备」列。
//
// 后端存的 TargetID 对资产是 "会议中心::elevator_no_room::KT-5"、对记录是
// "rec_1783..." —— 审批的人看这两串东西判断不了在改哪台设备,所以要翻成名字。
//
// 这一列一度整列空白:前端读的是 assetName,而后端压根没返回过名字。
// 修好之后又发现第二层问题:清理历史数据时删掉过记录,引用它的修改申请
// 还留着,查不到目标 —— 那种情况【必须说出来】,不能显示成空白让人以为
// 系统坏了。它其实是永远也批不了的(applyChangeRequest 同样查不到目标)。

func crTargetTestServer(t *testing.T) (*Server, *SQLiteStore) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "crt.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return &Server{store: store, storageDir: t.TempDir()}, store
}

func TestChangeRequestTargetNames(t *testing.T) {
	srv, store := crTargetTestServer(t)

	// 一台真实设备
	asset := &AssetEntry{
		ID: "会议中心::elevator_no_room::KT-5", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "无机房电梯", AssetKey: "KT-5",
		AssetName: "KT-5", LastStatus: "正常",
	}
	if err := store.CreateAsset(asset); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	// 一条真实记录(带设备编号)
	tpl, ok := templateByID("elevator_no_room")
	if !ok {
		t.Fatal("找不到无机房电梯模板")
	}
	now := time.Now()
	rec := &Record{
		ID: "rec_real", TenantID: defaultTenantID, Project: "会议中心",
		PointID: "p_elevator_no_room", PointName: "无机房电梯",
		TemplateID: tpl.ID, TemplateName: tpl.Name, Inspector: "胡晓悱",
		Fields: initialFieldValues(tpl, "胡晓悱"), Submitted: true,
		CreatedAt: now, UpdatedAt: now, SubmittedAt: &now,
	}
	for i := range rec.Fields {
		if rec.Fields[i].Code == "asset_no" {
			rec.Fields[i].Value = "KT-5"
		}
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	list := []*ChangeRequest{
		{ID: "cr1", TargetType: "asset", TargetID: asset.ID},
		{ID: "cr2", TargetType: "record", TargetID: "rec_real"},
		// 目标已被删除:清理历史数据时的真实形状
		{ID: "cr3", TargetType: "record", TargetID: "rec_已经删掉了"},
		{ID: "cr4", TargetType: "asset", TargetID: "会议中心::manual::已经删掉了"},
	}
	r := httptest.NewRequest(http.MethodGet, "/api/change-requests", nil)
	srv.fillChangeRequestTargetNames(r, list)

	want := map[string]string{
		"cr1": "KT-5",
		"cr2": "KT-5",
		"cr3": "记录已不存在",
		"cr4": "设备已不存在",
	}
	for _, cr := range list {
		if got := cr.TargetName; got != want[cr.ID] {
			t.Errorf("%s 的 targetName = %q, 期望 %q", cr.ID, got, want[cr.ID])
		}
	}
}

// 记录没有设备编号字段时(比如消防、配电这类模板),退回点位名 ——
// 总比空白强:审批的人至少知道是哪一类巡检。
func TestChangeRequestTargetNameFallsBackToPointName(t *testing.T) {
	srv, store := crTargetTestServer(t)
	tpl, ok := templateByID("elevator_no_room")
	if !ok {
		t.Fatal("找不到模板")
	}
	now := time.Now()
	rec := &Record{
		ID: "rec_nokey", TenantID: defaultTenantID, Project: "会议中心",
		PointID: "p_x", PointName: "消防通道", TemplateID: tpl.ID,
		TemplateName: tpl.Name, Inspector: "胡晓悱",
		Fields: initialFieldValues(tpl, "胡晓悱"), Submitted: true,
		CreatedAt: now, UpdatedAt: now, SubmittedAt: &now,
	}
	// 刻意不填 asset_no
	for i := range rec.Fields {
		if rec.Fields[i].Code == "asset_no" {
			rec.Fields[i].Value = ""
		}
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}

	list := []*ChangeRequest{{ID: "cr", TargetType: "record", TargetID: "rec_nokey"}}
	srv.fillChangeRequestTargetNames(
		httptest.NewRequest(http.MethodGet, "/api/change-requests", nil), list)
	if list[0].TargetName != "消防通道" {
		t.Fatalf("没有设备编号时应退回点位名,得到 %q", list[0].TargetName)
	}
}
