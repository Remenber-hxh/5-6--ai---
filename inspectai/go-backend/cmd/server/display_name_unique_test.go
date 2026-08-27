package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// postAs 用带登录态的请求打一个 POST handler。
func postAs(t *testing.T, base *http.Request, path string, body any) (*httptest.ResponseRecorder, *http.Request) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
	r.Header = base.Header.Clone()
	r.Header.Set("Content-Type", "application/json")
	return httptest.NewRecorder(), r
}

// 姓名唯一。
//
// 【为什么要有】系统里到处按姓名认人:计划负责人、任务执行人、巡检记录的
// 提交人,历史数据里存的都是名字而不是账号。两个人同名就分不清谁是谁,
// 而分错的表现是"提醒发给了另一个同名的人" —— 不报错。
//
// 【三条入口都要覆盖】建账号 / 扫码注册 / 改资料。少测一条,
// 漏掉的那条就是重名进来的入口。

func TestDisplayNameFreeAcrossPaths(t *testing.T) {
	srv, store, _ := bindStore(t)
	addOwnerUser(t, store, "huxf", "胡晓悱")

	// 同名 → 拒
	if err := srv.ensureDisplayNameFree(defaultTenantID, "胡晓悱", ""); err == nil {
		t.Error("同名应被拒")
	}
	// 【空格不算区别】"胡 晓悱" 在所有按姓名认人的地方都会被当成同一个人,
	// 那这里就不该放行第二个。全角空格也一样。
	for _, v := range []string{"胡 晓悱", "胡　晓悱", " 胡晓悱 "} {
		if err := srv.ensureDisplayNameFree(defaultTenantID, v, ""); err == nil {
			t.Errorf("%q 应被当成重名拒掉", v)
		}
	}
	// 不同名 → 放行
	if err := srv.ensureDisplayNameFree(defaultTenantID, "胡晓明", ""); err != nil {
		t.Errorf("不同名不该被拒:%v", err)
	}
}

// 改名时要排除自己 —— 不排除的话,任何人保存一次资料都会被自己挡住。
func TestDisplayNameExcludesSelfOnRename(t *testing.T) {
	srv, store, _ := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	if err := srv.ensureDisplayNameFree(defaultTenantID, "胡晓悱", u.ID); err != nil {
		t.Errorf("改自己的名字为原值不该被拒:%v", err)
	}
}

// 别的租户里同名不算重 —— 租户之间本来就是隔离的。
func TestDisplayNameScopedToTenant(t *testing.T) {
	srv, store, _ := bindStore(t)
	addOwnerUser(t, store, "huxf", "胡晓悱")
	if err := srv.ensureDisplayNameFree("tenant_other", "胡晓悱", ""); err != nil {
		t.Errorf("另一个租户里同名不该被拒:%v", err)
	}
}

// 建账号这条路要真的拦住。
func TestCreateUserRejectsDuplicateDisplayName(t *testing.T) {
	srv, store, req := bindStore(t)
	addOwnerUser(t, store, "huxf", "胡晓悱")

	w, r := postAs(t, req, "/api/users", map[string]any{
		"username": "huxf2", "displayName": "胡晓悱",
		"password": "pw-for-test-only", "roleCode": roleInspector,
	})
	srv.handleCreateUser(w, r)
	if w.Code != 409 {
		t.Fatalf("重名应 409,实际 %d:%s", w.Code, w.Body.String())
	}
}

// ===== 批量算范围要和逐个算的结果一致 =====
//
// 【这是这次改动唯一的风险】为了不让用户页在人数上百时变卡,范围改成一次查全。
// 两条路给出不同答案的话,表现是"用户列表里显示他能看全部,实际却看不到" ——
// 没人会去比对这两处,所以只能靠测试钉住。
func TestBatchProjectScopesMatchPerUser(t *testing.T) {
	srv, req, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	_ = req

	limited := addOwnerUser(t, store, "huxf", "胡晓悱")
	scopeUserToProject(t, store, limited.ID, "紫菡雅集")

	plain := addOwnerUser(t, store, "demo9", "普通巡检员") // 没配范围 = 只看自己的

	boss := addOwnerUser(t, store, "boss", "管理员甲")
	if err := store.UpdateUserProfile(boss.ID, func(u *User) { u.DataScope = dataScopeAll }); err != nil {
		t.Fatal(err)
	}

	// 配了项目范围、但一个项目都没分到 —— 应该是 Blocked
	orphan := addOwnerUser(t, store, "orphan", "没分项目的人")
	if err := store.UpdateUserProfile(orphan.ID, func(u *User) { u.DataScope = dataScopeProject }); err != nil {
		t.Fatal(err)
	}

	users, err := store.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	batch, err := srv.projectScopesForUsers(defaultTenantID, users)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		vis := srv.visibilityForUser(defaultTenantID, u)
		want := projectScopeDTO{
			SeesAll:  vis.AllData || (!vis.Blocked && len(vis.Projects) == 0),
			Projects: vis.Projects,
			Blocked:  vis.Blocked,
		}
		got := batch[u.ID]
		if got.SeesAll != want.SeesAll || got.Blocked != want.Blocked ||
			len(got.Projects) != len(want.Projects) {
			t.Errorf("%s(%s):批量 %+v ≠ 逐个 %+v", u.Username, u.DisplayName, got, want)
			continue
		}
		for i := range want.Projects {
			if got.Projects[i] != want.Projects[i] {
				t.Errorf("%s 的项目清单不一致:批量 %v ≠ 逐个 %v", u.Username, got.Projects, want.Projects)
				break
			}
		}
	}
	// 顺带钉住几个关键档位,防止两条路【一起】错
	if !batch[plain.ID].SeesAll {
		t.Error("没配范围的人应不受项目限制")
	}
	if !batch[boss.ID].SeesAll {
		t.Error("看全部数据的人应不受项目限制")
	}
	if !batch[orphan.ID].Blocked {
		t.Error("配了项目范围却一个项目都没分到,应是 Blocked")
	}
	if batch[limited.ID].SeesAll || len(batch[limited.ID].Projects) != 1 {
		t.Errorf("被限定的人应只有一个项目,实际 %+v", batch[limited.ID])
	}
}
