package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// tenantForRequest:登录用户解析出各自租户;不同租户互不串;无会话回落默认租户。
func TestTenantForRequest(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)

	reqWith := func(token string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/x", nil)
		if token != "" {
			r.Header.Set("X-InspectAI-Token", token)
		}
		return r
	}

	// helper 建的用户未指定租户 → 落默认租户
	if got := srv.tenantForRequest(reqWith(tokens["inspector"])); got != defaultTenantID {
		t.Errorf("默认租户用户 = %q, 期望 %q", got, defaultTenantID)
	}

	// 另建一个指定租户的用户 → 解析出该租户
	if err := srv.store.CreateUser(&User{
		Username: "acme_u", DisplayName: "Acme 巡检", RoleCode: roleInspector, TenantID: "tenant_acme",
	}, "pw"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	_, sess, err := srv.store.AuthenticateUser("acme_u", "pw")
	if err != nil {
		t.Fatalf("AuthenticateUser: %v", err)
	}
	if got := srv.tenantForRequest(reqWith(sess.Token)); got != "tenant_acme" {
		t.Errorf("指定租户用户 = %q, 期望 tenant_acme", got)
	}

	// 无会话 → 回落默认租户(单租户安全行为)
	if got := srv.tenantForRequest(reqWith("")); got != defaultTenantID {
		t.Errorf("无会话 = %q, 期望回落 %q", got, defaultTenantID)
	}
}
