package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// 注册码是【自助注册的唯一门槛】。这些测试盯的都是"错了不会报错,只会悄悄放人进来"
// 的地方:次数超发、管理员角色被自助注册出来、旧密码没验就让改。

func regTestServer(t *testing.T) (*Server, *SQLiteStore) {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "reg.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	// 【必须跟生产一样先种身份】内置角色(inspector/admin/...)是
	// EnsureIdentitySeed 建的,main.go 启动时会调。少了这步,注册时
	// GetRoleByCode("inspector") 查不到,handler 会回一句
	// "注册码上的角色已不存在" —— 那是测试环境不完整,不是代码有问题。
	if err := store.EnsureIdentitySeed(IdentitySeed{
		Username: "seedadmin", DisplayName: "种子管理员",
	}); err != nil {
		t.Fatalf("EnsureIdentitySeed: %v", err)
	}
	return &Server{
		store:      store,
		storageDir: t.TempDir(),
		loginGuard: newLoginGuard(),
	}, store
}

func seedCode(t *testing.T, store *SQLiteStore, code string, maxUses int, role string) *RegistrationCode {
	t.Helper()
	rc := &RegistrationCode{
		ID: newID("regcode"), Code: code, TenantID: defaultTenantID,
		RoleCode: role, MaxUses: maxUses, CreatedAt: nowStamp(),
	}
	if err := store.CreateRegistrationCode(rc); err != nil {
		t.Fatalf("CreateRegistrationCode: %v", err)
	}
	return rc
}

func postJSON(t *testing.T, srv *Server, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	buf, _ := json.Marshal(body)
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(buf))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "203.0.113.7:1234" // 非回环:别走进本地免鉴权那条路
	if token != "" {
		r.Header.Set("X-InspectAI-Token", token)
	}
	w := httptest.NewRecorder()
	srv.router(w, r)
	return w
}

// 次数上限必须在 SQL 里判,不能先查后写。
//
// 这条盯的是超发:一个只剩几次的码被同时提交,如果实现是"先查还剩几次、
// 再 +1",两个请求都会看到"还能用"。次数限制是这个功能的全部意义,
// 靠"大概率不会同时"保证是不行的 —— 而且超发了【不报错】,只是多进来一个人。
func TestRegistrationCodeCannotOverrunConcurrently(t *testing.T) {
	_, store := regTestServer(t)
	rc := seedCode(t, store, "TEST-0001", 3, roleInspector)

	const racers = 12
	var wg sync.WaitGroup
	var mu sync.Mutex
	okCount := 0
	wg.Add(racers)
	for range racers {
		go func() {
			defer wg.Done()
			if err := store.ConsumeRegistrationCode(rc.ID); err == nil {
				mu.Lock()
				okCount++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if okCount != 3 {
		t.Fatalf("上限 3 次,实际放过 %d 次 —— 并发下超发了", okCount)
	}
	got, err := store.GetRegistrationCode("TEST-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedCount != 3 {
		t.Fatalf("used_count 应为 3,实际 %d", got.UsedCount)
	}
}

// 不限次数的码(max_uses=0)不能被"上限判断"误伤成用完了。
func TestRegistrationCodeUnlimitedUses(t *testing.T) {
	_, store := regTestServer(t)
	rc := seedCode(t, store, "TEST-0002", 0, roleInspector)
	for i := range 5 {
		if err := store.ConsumeRegistrationCode(rc.ID); err != nil {
			t.Fatalf("第 %d 次应放行,却报错: %v", i+1, err)
		}
	}
}

// 停用后立刻失效 —— 管理员按下停用,是因为码已经外泄了,不能还能再用一次。
func TestRegistrationCodeDisabledStopsConsuming(t *testing.T) {
	_, store := regTestServer(t)
	rc := seedCode(t, store, "TEST-0003", 0, roleInspector)
	if err := store.SetRegistrationCodeDisabled(defaultTenantID, rc.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ConsumeRegistrationCode(rc.ID); err == nil {
		t.Fatal("停用后仍然放行了 —— 停用等于没用")
	}
}

// 过期判断。空的 expires_at 表示不过期,别被当成"1970 年就过期了"。
func TestRegistrationCodeExpiry(t *testing.T) {
	now := time.Now()
	never := &RegistrationCode{}
	if err := never.Usable(now); err != nil {
		t.Fatalf("空 expiresAt 应视为不过期,却报: %v", err)
	}
	expired := &RegistrationCode{ExpiresAt: now.Add(-time.Hour).Format(time.RFC3339)}
	if err := expired.Usable(now); err == nil {
		t.Fatal("已过期的码仍然可用")
	}
	future := &RegistrationCode{ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	if err := future.Usable(now); err != nil {
		t.Fatalf("未过期的码被判为不可用: %v", err)
	}
}

// 码要人能念、能抄:不能出现 0/O/1/I/L 这些形近字符。
// 现场是微信发码、口头念码,形近字符会直接变成"我明明输对了却说无效"。
func TestRegistrationCodeAvoidsLookalikeChars(t *testing.T) {
	for range 200 {
		code := newRegistrationCode()
		if strings.ContainsAny(code, "0O1IL") {
			t.Fatalf("生成的码含形近字符: %s", code)
		}
	}
}

// 输入宽容:大小写、空格、全角连字符都该当同一个码。
func TestNormalizeRegistrationCode(t *testing.T) {
	for _, raw := range []string{"abcd-2345", " ABCD-2345 ", "abcd—2345", "ABCD－2345", "AB CD-2345"} {
		if got := normalizeRegistrationCode(raw); got != "ABCD-2345" {
			t.Errorf("normalizeRegistrationCode(%q) = %q, 期望 ABCD-2345", raw, got)
		}
	}
}

// 注册出来的账号必须落在码指定的角色和租户上,不能默认成别的。
func TestRegisterCreatesUserWithCodeRoleAndTenant(t *testing.T) {
	srv, store := regTestServer(t)
	seedCode(t, store, "TEAM-2345", 2, roleInspector)

	w := postJSON(t, srv, "/api/auth/register", map[string]any{
		"username": "newguy", "displayName": "新来的",
		"password": "pw123456", "code": "team-2345", // 小写也该认
	}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("注册应成功,得到 %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		User  *User  `json:"user"`
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应: %v", err)
	}
	if resp.User == nil || resp.User.RoleCode != roleInspector {
		t.Fatalf("角色应为 %s,得到 %+v", roleInspector, resp.User)
	}
	if resp.User.TenantID != defaultTenantID {
		t.Fatalf("租户应为 %s,得到 %q", defaultTenantID, resp.User.TenantID)
	}
	if resp.Token == "" {
		t.Fatal("注册完应直接给会话,免得再手动登一次")
	}
	// 名额要扣掉
	rc, _ := store.GetRegistrationCode("TEAM-2345")
	if rc.UsedCount != 1 {
		t.Fatalf("used_count 应为 1,得到 %d", rc.UsedCount)
	}
}

// 错码不能放人进来,而且【不能吃掉名额】。
func TestRegisterRejectsBadCode(t *testing.T) {
	srv, store := regTestServer(t)
	seedCode(t, store, "GOOD-2345", 1, roleInspector)

	w := postJSON(t, srv, "/api/auth/register", map[string]any{
		"username": "nope", "displayName": "无码", "password": "pw123456", "code": "WRNG-9999",
	}, "")
	if w.Code == http.StatusOK {
		t.Fatal("错误的注册码竟然注册成功了")
	}
	rc, _ := store.GetRegistrationCode("GOOD-2345")
	if rc.UsedCount != 0 {
		t.Fatalf("错码不该消耗正确码的名额,used_count = %d", rc.UsedCount)
	}
}

// 重名注册失败时,名额不能被吃掉。
// 一个 5 次的码被三个人打错字就废了 —— 这种损耗没有任何提示,最难查。
func TestRegisterDuplicateUsernameDoesNotConsumeCode(t *testing.T) {
	srv, store := regTestServer(t)
	seedCode(t, store, "DUPE-2345", 5, roleInspector)
	if err := store.CreateUser(&User{
		ID: "u_exist", Username: "taken", DisplayName: "已存在",
		RoleCode: roleInspector, TenantID: defaultTenantID, Status: "active",
	}, "pw123456"); err != nil {
		t.Fatal(err)
	}

	w := postJSON(t, srv, "/api/auth/register", map[string]any{
		"username": "taken", "displayName": "重名", "password": "pw123456", "code": "DUPE-2345",
	}, "")
	if w.Code != http.StatusConflict {
		t.Fatalf("重名应返回 409,得到 %d: %s", w.Code, w.Body.String())
	}
	rc, _ := store.GetRegistrationCode("DUPE-2345")
	if rc.UsedCount != 0 {
		t.Fatalf("注册失败不该扣名额,used_count = %d", rc.UsedCount)
	}
}

// 【管理员角色不能用注册码创建】。一张能自助注册出管理员的码要是流出去,
// 整个租户的数据就没门槛了 —— 这是本功能最严重的失手方式。
func TestCannotIssueAdminRegistrationCode(t *testing.T) {
	srv, store := regTestServer(t)
	admin := &User{
		ID: "u_admin", Username: "boss", DisplayName: "管理员",
		RoleCode: roleAdmin, TenantID: defaultTenantID, Status: "active",
	}
	if err := store.CreateUser(admin, "pw123456"); err != nil {
		t.Fatal(err)
	}
	_, sess, err := store.AuthenticateUser("boss", "pw123456")
	if err != nil {
		t.Fatal(err)
	}

	w := postJSON(t, srv, "/api/registration-codes",
		map[string]any{"roleCode": roleAdmin}, sess.Token)
	if w.Code == http.StatusOK {
		t.Fatal("竟然签发出了管理员注册码 —— 这张码流出去等于把后台送人")
	}

	// 巡检员的码要能正常签发,别把门关死了
	if w2 := postJSON(t, srv, "/api/registration-codes",
		map[string]any{"roleCode": roleInspector, "maxUses": 5}, sess.Token); w2.Code != http.StatusOK {
		t.Fatalf("巡检员注册码应能签发,得到 %d: %s", w2.Code, w2.Body.String())
	}
}

// 非管理员不能签发注册码,也不能停用别人的。
func TestRegistrationCodeManageRequiresAdmin(t *testing.T) {
	srv, store := regTestServer(t)
	if err := store.CreateUser(&User{
		ID: "u_insp", Username: "worker", DisplayName: "巡检员",
		RoleCode: roleInspector, TenantID: defaultTenantID, Status: "active",
	}, "pw123456"); err != nil {
		t.Fatal(err)
	}
	_, sess, err := store.AuthenticateUser("worker", "pw123456")
	if err != nil {
		t.Fatal(err)
	}
	rc := seedCode(t, store, "MINE-2345", 1, roleInspector)

	if w := postJSON(t, srv, "/api/registration-codes",
		map[string]any{"roleCode": roleInspector}, sess.Token); w.Code == http.StatusOK {
		t.Fatal("巡检员签发出了注册码")
	}
	// 前缀路由是在 handleRegistrationCodeRoutes 里自己分权的,单独盯一下
	if w := postJSON(t, srv, "/api/registration-codes/"+rc.ID+"/disable",
		map[string]any{"disabled": true}, sess.Token); w.Code == http.StatusOK {
		t.Fatal("巡检员停用了注册码 —— 前缀路由漏了权限检查")
	}
}

// 改密码必须先验旧密码。否则借用了没锁屏的手机就能把本人锁在外面。
func TestChangePasswordRequiresOldPassword(t *testing.T) {
	srv, store := regTestServer(t)
	if err := store.CreateUser(&User{
		ID: "u_me", Username: "me", DisplayName: "我",
		RoleCode: roleInspector, TenantID: defaultTenantID, Status: "active",
	}, "oldpass123"); err != nil {
		t.Fatal(err)
	}
	_, sess, err := store.AuthenticateUser("me", "oldpass123")
	if err != nil {
		t.Fatal(err)
	}

	// 旧密码错 → 拒绝
	if w := postJSON(t, srv, "/api/auth/me/password",
		map[string]any{"oldPassword": "wrong", "newPassword": "brandnew123"}, sess.Token); w.Code == http.StatusOK {
		t.Fatal("旧密码错误却改成功了")
	}
	// 旧密码仍然有效,说明没被改掉
	if err := store.VerifyUserPassword("u_me", "oldpass123"); err != nil {
		t.Fatal("旧密码失效了 —— 校验失败的那次把密码改掉了")
	}

	// 太短 → 拒绝
	if w := postJSON(t, srv, "/api/auth/me/password",
		map[string]any{"oldPassword": "oldpass123", "newPassword": "123"}, sess.Token); w.Code == http.StatusOK {
		t.Fatal("过短的新密码被接受了")
	}

	// 正确 → 改成功,且旧会话全部失效
	w := postJSON(t, srv, "/api/auth/me/password",
		map[string]any{"oldPassword": "oldpass123", "newPassword": "brandnew123"}, sess.Token)
	if w.Code != http.StatusOK {
		t.Fatalf("改密码应成功,得到 %d: %s", w.Code, w.Body.String())
	}
	if err := store.VerifyUserPassword("u_me", "brandnew123"); err != nil {
		t.Fatalf("新密码不生效: %v", err)
	}
	// 改密码的动机常常是"密码可能泄露了",别处还登着就等于没改
	if _, err := store.GetUserBySession(sess.Token); err == nil {
		t.Fatal("改完密码旧会话仍然有效 —— 别的设备还登着")
	}
}

// 没登录不能改密码(这条接口是 guardNone,靠 handler 自己认人)。
func TestChangePasswordRejectsAnonymous(t *testing.T) {
	srv, _ := regTestServer(t)
	if w := postJSON(t, srv, "/api/auth/me/password",
		map[string]any{"oldPassword": "x", "newPassword": "brandnew123"}, ""); w.Code == http.StatusOK {
		t.Fatal("未登录竟然能改密码")
	}
}
