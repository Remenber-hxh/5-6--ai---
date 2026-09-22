package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// ===== 对调接口走完整条路:路由 → 权限 → 落库 =====
//
// 【为什么单测 swapFieldPayload 还不够】这条路由和 PATCH 撞在同一个形状上
// (都是 records/{id}/fields/{三段}),顺序排错的话 "swap" 会被当成字段标识
// 走进 handlePatchField —— 那时接口返回 200、字段没换、前端也不报错。
// 这种错只有把请求真正发一遍才看得见。

func newSwapAPIServer(t *testing.T) (*Server, string, string) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "swap.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := &Server{store: store, storeKind: "sqlite", storageDir: t.TempDir(),
		frontendDir: t.TempDir(), corsAllowedOrigins: map[string]bool{}}
	if err := s.loadPermissions(); err != nil {
		t.Fatalf("loadPermissions: %v", err)
	}
	admin := &User{ID: "user_admin", Username: "admin", DisplayName: "系统管理员", RoleCode: roleAdmin}
	if err := store.CreateUser(admin, "test-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, session, err := store.AuthenticateUser("admin", "test-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	rec := &Record{
		ID: "rec_swap", TenantID: defaultTenantID, TemplateID: "zihan_energy",
		Project: "紫菡雅集", Inspector: "苑文涛", CreatedAt: time.Now(),
		Fields: []FieldValue{
			{Code: "z1_reading", Label: "Z1能耗表读数", AssetName: "Z1",
				Value: "1167636.5", SourceImageID: "img_elec", Version: 2},
			{Code: "fire_water_reading", Label: "消防水表读数", AssetName: "消防水表",
				Value: "203712.26", SourceImageID: "img_elec2", Version: 1},
		},
	}
	if err := store.CreateRecord(rec); err != nil {
		t.Fatalf("CreateRecord: %v", err)
	}
	return s, session.Token, rec.ID
}

func postSwap(t *testing.T, s *Server, token, recID, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost,
		"/api/inspection/records/"+recID+"/fields/swap", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", token)
	rec := httptest.NewRecorder()
	s.router(rec, req)
	return rec
}

// 【最要紧的一条】发一次请求,两格的读数和照片真的换过来了,而且落了库。
func TestSwapEndpointExchangesBothFields(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)

	got := postSwap(t, s, tok, rid,
		`{"aCode":"z1_reading","bCode":"fire_water_reading"}`)
	if got.Code != http.StatusOK {
		t.Fatalf("对调失败 status=%d %s", got.Code, got.Body.String())
	}

	// 重新从库里读,证明不是只改了返回值
	after, err := s.store.GetRecord(defaultTenantID, rid)
	if err != nil || after == nil {
		t.Fatalf("读回记录失败: %v", err)
	}
	byCode := map[string]FieldValue{}
	for _, f := range after.Fields {
		byCode[f.Code] = f
	}
	if byCode["z1_reading"].Value != "203712.26" ||
		byCode["fire_water_reading"].Value != "1167636.5" {
		t.Errorf("读数没换:z1=%q fire=%q",
			byCode["z1_reading"].Value, byCode["fire_water_reading"].Value)
	}
	if byCode["z1_reading"].SourceImageID != "img_elec2" ||
		byCode["fire_water_reading"].SourceImageID != "img_elec" {
		t.Errorf("照片没跟着换:z1=%q fire=%q",
			byCode["z1_reading"].SourceImageID, byCode["fire_water_reading"].SourceImageID)
	}
	// 设备归属不动 —— 日报的栏位是固定的
	if byCode["z1_reading"].AssetName != "Z1" ||
		byCode["fire_water_reading"].AssetName != "消防水表" {
		t.Errorf("设备归属被换走了:%+v", byCode)
	}
}

// 【路由没被 PATCH 抢走】抢走的话这里会返回 200 而字段纹丝不动 ——
// 接口成功、界面没反应,最难查的那种。
func TestSwapRouteIsNotSwallowedByPatch(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)
	// 故意传一个不存在的字段:走对了路由会说"字段不存在",
	// 被 PATCH 抢走的话报的会是别的错(或者干脆成功)。
	got := postSwap(t, s, tok, rid, `{"aCode":"z1_reading","bCode":"没这个字段"}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("期望 400 bad_field,实得 %d %s", got.Code, got.Body.String())
	}
	var body struct{ Error string }
	_ = json.Unmarshal(got.Body.Bytes(), &body)
	if body.Error != "bad_field" {
		t.Errorf("错误码不对,说明可能没走到对调那个 handler:%q", body.Error)
	}
}

// 已提交的记录不能再这样改 —— 要改走修改申请,那里有审批留痕。
func TestSwapRefusesSubmittedRecord(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)
	rec, _ := s.store.GetRecord(defaultTenantID, rid)
	now := time.Now()
	rec.Submitted, rec.SubmittedAt = true, &now
	if err := s.store.UpdateRecord(rec); err != nil {
		t.Fatal(err)
	}
	got := postSwap(t, s, tok, rid, `{"aCode":"z1_reading","bCode":"fire_water_reading"}`)
	if got.Code != http.StatusConflict {
		t.Errorf("已提交的记录被改了,status=%d %s", got.Code, got.Body.String())
	}
}

// 换到自己头上不该悄悄成功 —— 那通常是前端算错了目标格。
func TestSwapRejectsSameField(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)
	got := postSwap(t, s, tok, rid, `{"aCode":"z1_reading","bCode":"z1_reading"}`)
	if got.Code != http.StatusBadRequest {
		t.Errorf("同一格对调应被拒,实得 %d", got.Code)
	}
}
