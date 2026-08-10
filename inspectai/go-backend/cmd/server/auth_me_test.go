package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// /api/auth/me 是两个前端【开机校验登录态】唯一的依据。
//
// 在这之前,前端只看 localStorage 里有没有 token 这个字符串就认定已登录。
// token 失效了(会话过期、账号停用、服务端换了库)本地照样认:进来是完整的
// App 外壳,然后每个页面各自弹"加载失败" —— 用户的感受是"登着录但什么都
// 打不开",而不是"该重新登录了"。
//
// 这条测试盯两件事:
//   一、真会话:返回真实身份,status 【不能】是 "local"。
//   二、坏 token:必须 401(前端据此清登录态回登录页)。
//      注意 handleMe 自己在没会话时也会回 200 —— 拦截发生在更外层的
//      authorized() 里。所以这里必须走完整的 handler 链,不能直接调 handleMe。
//
// 还有一层隐性契约:免鉴权(本地回环)时后端回的是 status="local" 的占位身份,
// 前端靠这个字符串判断"这不是真会话,别拿它覆盖本地用户"。改了这个字符串
// 前端会静默把真实用户显示成占位角色 —— 所以第三条把它一起钉死。

func meResponse(t *testing.T, srv *Server, token, remoteAddr string) (int, *User) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/api/auth/me", nil)
	if token != "" {
		r.Header.Set("X-InspectAI-Token", token)
	}
	if remoteAddr != "" {
		r.RemoteAddr = remoteAddr
	}
	w := httptest.NewRecorder()
	// 走 router 而不是直接调 handleMe:401 是在 router 里的 authorized() 拦的,
	// 绕过它这条测试就测不到任何东西。
	srv.router(w, r)
	if w.Code != http.StatusOK {
		return w.Code, nil
	}
	var body struct {
		User  *User    `json:"user"`
		Perms []string `json:"perms"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 /api/auth/me 响应: %v (原文 %s)", err, w.Body.String())
	}
	return w.Code, body.User
}

func TestMeReturnsRealIdentityForValidSession(t *testing.T) {
	srv, tokens := newClosedLoopServer(t)

	code, user := meResponse(t, srv, tokens["inspector"], "203.0.113.9:1234")
	if code != http.StatusOK {
		t.Fatalf("有效会话应 200,得到 %d", code)
	}
	if user == nil {
		t.Fatal("响应里没有 user —— 前端拿不到身份")
	}
	if user.Username != "inspector" {
		t.Fatalf("身份应是 inspector,得到 %q", user.Username)
	}
	// 前端用 status=="local" 判断"这不是真会话"。真会话要是也叫 local,
	// 前端就会把真实用户当占位身份丢掉,登录态白校验。
	if user.Status == "local" {
		t.Fatal(`真实会话的 status 不能是 "local" —— 前端会据此忽略这个身份`)
	}
}

func TestMeRejectsStaleToken(t *testing.T) {
	srv, _ := newClosedLoopServer(t)

	// 外网地址 + 一个不存在的 token:免鉴权那条路走不通,必须 401。
	// 前端的全局 401 出口就靠这个把过期登录态清掉。
	code, _ := meResponse(t, srv, "tok_早就过期了", "203.0.113.9:1234")
	if code != http.StatusUnauthorized {
		t.Fatalf("失效 token 应 401,得到 %d —— 前端会一直以为自己登着录", code)
	}

	// 完全不带 token 同理
	if code, _ := meResponse(t, srv, "", "203.0.113.9:1234"); code != http.StatusUnauthorized {
		t.Fatalf("无 token 应 401,得到 %d", code)
	}
}

func TestMeMarksLocalNoAuthIdentity(t *testing.T) {
	srv, _ := newClosedLoopServer(t)

	// 回环地址且未配置静态 token → 本地免鉴权,回占位身份。
	code, user := meResponse(t, srv, "", "127.0.0.1:5555")
	if code != http.StatusOK {
		t.Fatalf("本地免鉴权应放行,得到 %d", code)
	}
	if user == nil {
		t.Fatal("响应里没有 user")
	}
	if user.Status != "local" {
		t.Fatalf(`免鉴权占位身份的 status 应为 "local",得到 %q —— `+
			`前端靠这个字符串区分"占位"和"真会话",改了就会拿占位身份覆盖真实用户`,
			user.Status)
	}
}
