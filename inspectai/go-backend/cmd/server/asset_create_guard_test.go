package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// ===== 新增设备的两道闸,必须和编辑那边完全一致 =====
//
// 【为什么专门测"两边一致"】2026-09-16 我给编辑路径(PATCH)加了设备类型校验,
// 却漏了新增路径(POST)。结果是:在界面上改类型会被拦,直接调接口新建
// 却能建出一台"瞎打的类型"—— 而它从此挂不上模板、也不会出现在抄表候选里,
// 全程不报错。本地实测发现的,发现时 POST 返回的是 201。
//
// 规则不对称比规则缺失更难查:人会因为"我试过,它会拦"而不再怀疑这条路。

func newAssetGuardServer(t *testing.T) (*Server, string) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "acg.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s := &Server{store: store, storeKind: "sqlite", storageDir: t.TempDir(),
		frontendDir: t.TempDir(), corsAllowedOrigins: map[string]bool{}}
	if err := s.loadPermissions(); err != nil {
		t.Fatalf("loadPermissions: %v", err)
	}
	if err := store.CreateProject(&Project{
		ID: "p_zihan", TenantID: defaultTenantID, Name: "紫菡雅集",
	}); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	admin := &User{ID: "user_admin", Username: "admin", DisplayName: "系统管理员", RoleCode: roleAdmin}
	if err := store.CreateUser(admin, "test-password"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, session, err := store.AuthenticateUser("admin", "test-password")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	return s, session.Token
}

func postAsset(t *testing.T, s *Server, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/assets", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", token)
	rec := httptest.NewRecorder()
	s.router(rec, req)
	return rec
}

func TestCreateAssetRejectsUnknownType(t *testing.T) {
	s, tok := newAssetGuardServer(t)
	got := postAsset(t, s, tok,
		`{"project":"紫菡雅集","assetKey":"X1","assetName":"X1","assetType":"瞎打的类型"}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("未知设备类型竟建成功了(status=%d):这台设备从此挂不上模板也选不到,而且不报错\n%s",
			got.Code, got.Body.String())
	}
}

func TestCreateAssetRejectsUnknownProject(t *testing.T) {
	s, tok := newAssetGuardServer(t)
	got := postAsset(t, s, tok,
		`{"project":"紫涵雅集","assetKey":"X2","assetName":"X2","assetType":"电表"}`) // 涵 ≠ 菡
	if got.Code != http.StatusBadRequest {
		t.Fatalf("错别字项目竟建成功了(status=%d):这台设备建完就谁都看不见\n%s",
			got.Code, got.Body.String())
	}
}

// 正常路径不能被误伤:类型留空是允许的(后台表单里它本来就不是必填)。
func TestCreateAssetAcceptsKnownTypeAndBlankType(t *testing.T) {
	s, tok := newAssetGuardServer(t)
	if got := postAsset(t, s, tok,
		`{"project":"紫菡雅集","assetKey":"Z9","assetName":"Z9能耗表","assetType":"电表"}`); got.Code != http.StatusCreated {
		t.Fatalf("建电表被拦了(status=%d)\n%s", got.Code, got.Body.String())
	}
	if got := postAsset(t, s, tok,
		`{"project":"紫菡雅集","assetKey":"Z10","assetName":"Z10"}`); got.Code != http.StatusCreated {
		t.Fatalf("不填设备类型被拦了(status=%d)——它本来就不是必填\n%s", got.Code, got.Body.String())
	}
}
