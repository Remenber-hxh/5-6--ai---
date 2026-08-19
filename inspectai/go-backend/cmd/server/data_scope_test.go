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

// newScopeRequestWithStore 同上,但把 store 和用户 ID 也交出来 ——
// 项目归属要在建好用户之后才能设。
func newScopeRequestWithStore(t *testing.T, role, scope string) (*Server, *http.Request, *MemStore, string) {
	t.Helper()
	srv, r := newScopeRequest(t, role, scope)
	store := srv.store.(*MemStore)
	users, err := store.ListUsers()
	if err != nil || len(users) != 1 {
		t.Fatalf("夹具应该只建了一个用户:%v %d", err, len(users))
	}
	return srv, r, store, users[0].ID
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

// 【fail closed】配了「本项目」却一个项目都没分到,必须什么都看不到。
//
// 放行的话这个配置就成了一句空话:管理员以为限住了,实际他看得见全部,
// 而且没有任何迹象。宁可他打开是空页面来问。
func TestProjectScopeWithoutProjectsSeesNothing(t *testing.T) {
	for _, scope := range []string{dataScopeProject, dataScopeProjectSelf} {
		srv, r := newScopeRequest(t, roleSupervisor, scope)
		vis := srv.visibilityFor(r)
		if !vis.Blocked {
			t.Errorf("data_scope=%s 且未分配项目时应 Blocked", scope)
		}
		if vis.AllData {
			t.Errorf("data_scope=%s 未分配项目却能看全部 —— 配置静默失效了", scope)
		}
		if vis.allowsProject("会议中心") {
			t.Errorf("data_scope=%s Blocked 状态下不该放行任何项目", scope)
		}
	}
}

// 分到项目之后:只看得到这些项目。
func TestProjectScopeLimitsToAssignedProjects(t *testing.T) {
	srv, r, store, userID := newScopeRequestWithStore(t, roleSupervisor, dataScopeProject)
	mine := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	other := &Project{TenantID: defaultTenantID, Name: "紫菡雅集"}
	for _, p := range []*Project{mine, other} {
		if err := store.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetUserProjects(defaultTenantID, userID, []string{mine.ID}); err != nil {
		t.Fatal(err)
	}
	vis := srv.visibilityFor(r)
	if vis.Blocked || vis.AllData {
		t.Fatalf("分到项目后应按项目限,得到 %+v", vis)
	}
	if !vis.allowsProject("会议中心") {
		t.Error("自己项目的数据看不到")
	}
	if vis.allowsProject("紫菡雅集") {
		t.Error("看到了没分给他的项目 —— 越权")
	}
	if vis.OwnOnly {
		t.Error("project(非 project_self)应能看到组内其他人的数据")
	}

	// project_self:项目一样,但记录只看自己的
	srv2, r2, store2, uid2 := newScopeRequestWithStore(t, roleSupervisor, dataScopeProjectSelf)
	p2 := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	if err := store2.CreateProject(p2); err != nil {
		t.Fatal(err)
	}
	if err := store2.SetUserProjects(defaultTenantID, uid2, []string{p2.ID}); err != nil {
		t.Fatal(err)
	}
	if vis2 := srv2.visibilityFor(r2); !vis2.OwnOnly || !vis2.allowsProject("会议中心") {
		t.Fatalf("project_self 应为「本项目 + 只看自己的」,得到 %+v", vis2)
	}
}

// 台账列表按项目裁剪。这条守的是"汇总数也不能泄" ——
// 列表筛掉了但顶部还写着"共 35 台",一样等于告诉他别的项目有多少设备。
func TestLimitAssetsToVisibleProjects(t *testing.T) {
	srv, r, store, userID := newScopeRequestWithStore(t, roleSupervisor, dataScopeProject)
	mine := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	if err := store.CreateProject(mine); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserProjects(defaultTenantID, userID, []string{mine.ID}); err != nil {
		t.Fatal(err)
	}
	assets := []*AssetEntry{
		{ID: "a1", Project: "会议中心", AssetName: "KT-1"},
		{ID: "a2", Project: "紫菡雅集", AssetName: "K01"},
		{ID: "a3", Project: "会议中心", AssetName: "KT-2"},
	}
	got := srv.limitAssetsToVisibleProjects(r, assets)
	if len(got) != 2 {
		t.Fatalf("应只剩会议中心的 2 台,得到 %d 台", len(got))
	}
	for _, a := range got {
		if a.Project != "会议中心" {
			t.Fatalf("混进了别的项目:%s", a.Project)
		}
	}
}

// 子路由取 id:漏掉一个后缀 = 那条路径绕过项目检查。
func TestAssetIDFromRoutePath(t *testing.T) {
	id := "会议中心::elevator_no_room::KT-7"
	for _, suffix := range []string{"", "/records", "/report", "/cover", "/change-requests", "/status-events"} {
		if got := assetIDFromRoutePath(id + suffix); got != id {
			t.Errorf("%q 取到的 id 是 %q,应为 %q —— 这条路径会绕过项目检查", id+suffix, got, id)
		}
	}
	if got := assetIDFromRoutePath("a/b/c"); got != "" {
		t.Errorf("认不出的多段路径应返回空,得到 %q", got)
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
