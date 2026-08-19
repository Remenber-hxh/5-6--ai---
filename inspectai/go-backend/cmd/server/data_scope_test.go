package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// scopeSeq 保证每个用例的用户名唯一(MemStore 会拒绝重名)。
var scopeSeq int

// 数据范围。这一步的全部价值在于【行为不变】—— 存量用户 data_scope 全是空,
// 上线后每个人看到的东西必须和以前一模一样。所以第一组测试才是重点:
// 空值必须完整复刻"管理角色看全部、其余看自己的"这条老规则。
//
// 反过来,越权是这块代码唯一不可接受的错误:少看几条是体验问题,
// 多看一条是事故。下面每条用例都盯着这两件事。

// newScopeRequest 造一个带真实会话的请求。走 AuthenticateUser 而不是伪造 token,
// 是为了让测试经过和线上一样的取用户路径 —— 会话查不到用户时的兜底行为也在覆盖范围内。
func newScopeRequest(t *testing.T, role, scope string) (*Server, *http.Request) {
	t.Helper()
	store := NewMemStore()
	srv := &Server{store: store}
	// 用户名不能由 scope 拼出来 —— 用例里的 scope 故意包含空格等非法值,
	// 拼进用户名会让 CreateUser(不 trim)和 AuthenticateUser(trim)对不上,
	// 测试就变成在考登录而不是考数据范围。
	scopeSeq++
	name := fmt.Sprintf("u%d_%s", scopeSeq, role)
	u := &User{
		ID: "user_" + name, Username: name,
		DisplayName: "测试" + role, RoleCode: role,
		TenantID: defaultTenantID, DataScope: scope,
	}
	if err := store.CreateUser(u, "pw-for-test-only"); err != nil {
		t.Fatal(err)
	}
	_, sess, err := store.AuthenticateUser(name, "pw-for-test-only")
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	r.Header.Set("X-InspectAI-Token", sess.Token)
	return srv, r
}

// 【最重要的一条】没配过 data_scope 时,行为必须和加这个字段之前完全一致。
func TestDataScopeEmptyKeepsRoleBehaviour(t *testing.T) {
	cases := []struct {
		role   string
		seeAll bool
	}{
		{roleAdmin, true},
		{roleManager, true},
		{roleSupervisor, true},
		{roleInspector, false}, // 巡检员只看自己的 —— 老规则
	}
	for _, c := range cases {
		srv, r := newScopeRequest(t, c.role, "")
		if got := srv.canSeeAllData(r); got != c.seeAll {
			t.Errorf("角色 %s + 空 data_scope: canSeeAllData=%v,应为 %v —— 升级后行为变了",
				c.role, got, c.seeAll)
		}
		// 和老判定逐个对齐:空值时两者永远相等,任何一处不等都是行为漂移
		if srv.canSeeAllData(r) != srv.hasSupervisorAccess(r) {
			t.Errorf("角色 %s: 空 data_scope 下新旧判定不一致", c.role)
		}
	}
}

// 显式配置压过角色 —— 这才是这一步换来的能力。
func TestDataScopeExplicitOverridesRole(t *testing.T) {
	// 收紧:主管被限定只看自己的(比如外派到某项目的临时主管)
	srv, r := newScopeRequest(t, roleSupervisor, dataScopeSelf)
	if srv.canSeeAllData(r) {
		t.Error("主管配了 self 仍能看全部 —— 配置没生效,收紧失败")
	}
	if !srv.hasSupervisorAccess(r) {
		t.Error("data_scope 不该影响动作权限:他还是主管,该能审批")
	}

	// 放宽:巡检员被授予看全部(比如兼任盘点的老员工)
	srv2, r2 := newScopeRequest(t, roleInspector, dataScopeAll)
	if !srv2.canSeeAllData(r2) {
		t.Error("巡检员配了 all 却看不到全部 —— 配置没生效")
	}
}

// 配错值(手滑、老数据、别的系统同步进来的枚举)必须回落到角色,
// 【绝不能】因为"不认识"就放行看全部。
func TestDataScopeUnknownValueFallsBackToRole(t *testing.T) {
	for _, bad := range []string{"everything", "ALL", "全部", "  "} {
		srv, r := newScopeRequest(t, roleInspector, bad)
		if srv.canSeeAllData(r) {
			t.Errorf("巡检员的 data_scope=%q(非法)被当成了看全部 —— 这是越权", bad)
		}
		srv2, r2 := newScopeRequest(t, roleSupervisor, bad)
		if !srv2.canSeeAllData(r2) {
			t.Errorf("主管的 data_scope=%q(非法)导致看不到数据 —— 应回落角色", bad)
		}
	}
}

// 项目维度两档现在还没有项目实体可挂,暂按 all 处理。
// 写成测试是为了钉住这个【临时】决定:等第 2 步做完,这条会被改掉,
// 改的时候能一眼看到当初为什么这么定(宁可暂时宽,不要先把管理者挡在外面)。
func TestDataScopeProjectTiersPendingStep2(t *testing.T) {
	for _, scope := range []string{dataScopeProject, dataScopeProjectSelf} {
		srv, r := newScopeRequest(t, roleSupervisor, scope)
		if got := srv.effectiveDataScope(r); got != dataScopeAll {
			t.Errorf("data_scope=%s 当前应按 all 处理,得到 %s", scope, got)
		}
	}
}

// 没有会话的请求(本地免鉴权、header 兜底)也必须走老路径。
func TestDataScopeWithoutSession(t *testing.T) {
	srv := &Server{store: NewMemStore()}
	r := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	if srv.canSeeAllData(r) != srv.hasSupervisorAccess(r) {
		t.Error("无会话时新旧判定不一致 —— 免鉴权路径行为变了")
	}
}

// 真库往返:配好的数据范围必须【存得下、读得回】。
//
// 这一类 bug 最阴 —— 接口返回 200、页面显示"已保存",重启或换个请求就没了,
// 而且没有任何报错。加字段时漏掉 INSERT / UPDATE 的列是常见做法性错误,
// 所以这里走真实的 SQLite,而不是内存实现。
func TestDataScopePersistsThroughSQLite(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "scope.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	u := &User{
		Username: "scope_user", DisplayName: "范围测试",
		RoleCode: roleInspector, TenantID: defaultTenantID,
		DataScope: dataScopeAll, // 新建时就带上
	}
	if err := store.CreateUser(u, "pw-for-test-only"); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	got, err := store.GetUser(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DataScope != dataScopeAll {
		t.Fatalf("新建时的 data_scope 没落库:期望 %q,得到 %q", dataScopeAll, got.DataScope)
	}

	// 改成只看自己的
	if err := store.UpdateUserProfile(u.ID, func(x *User) { x.DataScope = dataScopeSelf }); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.GetUser(u.ID); got.DataScope != dataScopeSelf {
		t.Fatalf("更新没落库:期望 %q,得到 %q", dataScopeSelf, got.DataScope)
	}

	// 【清回默认】配错之后必须改得回来,否则这个账号就永久卡在错误范围上
	if err := store.UpdateUserProfile(u.ID, func(x *User) { x.DataScope = "" }); err != nil {
		t.Fatal(err)
	}
	if got, _ = store.GetUser(u.ID); got.DataScope != "" {
		t.Fatalf("清空没生效,得到 %q —— 改错了就回不去", got.DataScope)
	}

	// 会话取到的用户也要带着这个字段(判定走的是这条路)
	if _, _, err := store.AuthenticateUser("scope_user", "pw-for-test-only"); err != nil {
		t.Fatal(err)
	}
}
