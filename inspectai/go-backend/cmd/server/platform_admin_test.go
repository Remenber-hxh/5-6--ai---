package main

import (
	"net/http"
	"path/filepath"
	"testing"
)

// 两级管理员边界:
// 租户管理员(admin 角色)拿不到租户管理接口;只有平台超管能建/看客户。
func TestPlatformAdminBoundary(t *testing.T) {
	srv, _ := newClosedLoopServer(t)

	mkUser := func(id, username string, platform bool) string {
		t.Helper()
		u := &User{
			ID: id, Username: username, DisplayName: username,
			RoleCode: roleAdmin, IsPlatformAdmin: platform,
		}
		if err := srv.store.CreateUser(u, "pw"); err != nil {
			t.Fatalf("CreateUser(%s): %v", username, err)
		}
		_, sess, err := srv.store.AuthenticateUser(username, "pw")
		if err != nil {
			t.Fatalf("AuthenticateUser(%s): %v", username, err)
		}
		return sess.Token
	}
	tenantAdmin := mkUser("u_ta", "tenant_admin", false) // 客户的系统管理员
	platformAdmin := mkUser("u_pa", "platform_admin", true)

	// 租户管理员:读写租户接口都必须 403
	if got := requestJSON(srv, http.MethodGet, "/api/tenants", tenantAdmin, ""); got.Code != http.StatusForbidden {
		t.Errorf("租户管理员 GET /api/tenants = %d, want 403; body=%s", got.Code, got.Body.String())
	}
	body := `{"name":"新客户","code":"newco"}`
	if got := requestJSON(srv, http.MethodPost, "/api/tenants", tenantAdmin, body); got.Code != http.StatusForbidden {
		t.Errorf("租户管理员 POST /api/tenants = %d, want 403; body=%s", got.Code, got.Body.String())
	}

	// 平台超管:可建、可查
	if got := requestJSON(srv, http.MethodPost, "/api/tenants", platformAdmin, body); got.Code != http.StatusOK {
		t.Fatalf("平台超管建客户 = %d, want 200; body=%s", got.Code, got.Body.String())
	}
	if got := requestJSON(srv, http.MethodGet, "/api/tenants", platformAdmin, ""); got.Code != http.StatusOK {
		t.Fatalf("平台超管查客户 = %d, want 200; body=%s", got.Code, got.Body.String())
	}

	// 短码唯一
	if got := requestJSON(srv, http.MethodPost, "/api/tenants", platformAdmin, body); got.Code != http.StatusConflict {
		t.Errorf("重复短码 = %d, want 409; body=%s", got.Code, got.Body.String())
	}

	// 未登录也进不来
	if got := requestJSON(srv, http.MethodGet, "/api/tenants", "", ""); got.Code == http.StatusOK {
		t.Error("未登录竟能访问租户管理接口")
	}
}

// migration 010:标志位落库,初始管理员被提升为平台超管,普通新建用户不是。
func TestPlatformAdminFlagPersistence(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "pa.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	if err := store.EnsureIdentitySeed(IdentitySeed{}); err != nil {
		t.Fatalf("EnsureIdentitySeed: %v", err)
	}
	admin, err := store.GetUser("user_admin")
	if err != nil {
		t.Fatalf("GetUser(user_admin): %v", err)
	}
	if !admin.IsPlatformAdmin {
		t.Error("初始管理员应为平台超管(璟邑即平台方)")
	}

	// 新建的租户管理员默认不是平台超管
	u := &User{Username: "co_admin", DisplayName: "客户管理员", RoleCode: roleAdmin}
	if err := store.CreateUser(u, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUser(u.ID)
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if got.IsPlatformAdmin {
		t.Error("新建租户管理员不应自动获得平台超管")
	}
	if got.TenantID != defaultTenantID {
		t.Errorf("新用户租户 = %q, want %q", got.TenantID, defaultTenantID)
	}
}

// 建号接口两条边界:新账号归属创建者所在租户;超管资格不能由请求体自助获取。
func TestCreateUserInheritsTenantAndCannotSelfElevate(t *testing.T) {
	srv, _ := newClosedLoopServer(t)

	// 租户 acme 的管理员
	admin := &User{ID: "u_acme_admin", Username: "acme_admin", DisplayName: "Acme管理员",
		RoleCode: roleAdmin, TenantID: "tenant_acme"}
	if err := srv.store.CreateUser(admin, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, sess, err := srv.store.AuthenticateUser("acme_admin", "pw")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}

	// 请求体里塞 isPlatformAdmin/tenantId,试图提权与跨租户建号
	body := `{"username":"mole","displayName":"内鬼","roleCode":"admin","password":"pw123456",
	          "isPlatformAdmin":true,"tenantId":"tenant_default"}`
	if got := requestJSON(srv, http.MethodPost, "/api/users", sess.Token, body); got.Code != http.StatusOK {
		t.Fatalf("建号 = %d, want 200; body=%s", got.Code, got.Body.String())
	}

	users, err := srv.store.ListUsers()
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	var mole *User
	for _, u := range users {
		if u.Username == "mole" {
			mole = u
		}
	}
	if mole == nil {
		t.Fatal("未找到新建账号")
	}
	if mole.IsPlatformAdmin {
		t.Error("请求体竟能自助获取平台超管资格")
	}
	if mole.TenantID != "tenant_acme" {
		t.Errorf("新账号租户 = %q, 期望继承创建者租户 tenant_acme", mole.TenantID)
	}
}
