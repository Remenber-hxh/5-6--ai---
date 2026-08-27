package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// savePlanDirect 直接打 handler,绕开路由和鉴权 —— 这里要考的是入参校验,
// 不是权限。权限有它自己的用例。
func savePlanDirect(t *testing.T, srv *Server, item *EngineeringPlanItem) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/engineering/plans", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateEngineeringPlan(w, r)
	return w
}

// 负责人绑定。
//
// 这里错一次的代价很具体:每日提醒发给了不该发的人,而且不报错 ——
// 要等到有人抱怨才会发现。所以匹配规则本身要有测试兜着。

func bindStore(t *testing.T) (*Server, *MemStore, *http.Request) {
	t.Helper()
	srv, req, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	return srv, store, req
}

// applyBindings 打应用接口。带上带登录态的请求 —— 里面要按数据范围裁。
func applyBindings(t *testing.T, srv *Server, base *http.Request, body any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/engineering/plans/owner-binding",
		bytes.NewReader(raw))
	r.Header = base.Header.Clone()
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleOwnerBindingApply(w, r)
	return w
}

type bindPair struct {
	PlanID string `json:"planId"`
	UserID string `json:"userId"`
}

func bindBody(pairs ...bindPair) map[string]any {
	return map[string]any{"bindings": pairs}
}

func addOwnerUser(t *testing.T, store *MemStore, username, display string) *User {
	t.Helper()
	u := &User{
		Username: username, DisplayName: display,
		RoleCode: roleInspector, TenantID: defaultTenantID,
	}
	if err := store.CreateUser(u, "pw-for-test-only"); err != nil {
		t.Fatalf("CreateUser(%s): %v", username, err)
	}
	return u
}

// scopeUserToProject 把一个人限定到某个项目。
//
// 【user_projects 存的是项目 ID,不是名字】直接塞名字进去,
// ListUserProjectNames 那一步 join 不到任何东西,结果是"这个人一个项目都
// 看不到"—— 于是"该拦的拦住了"的用例会因为错误的原因通过,
// 而"该放行的放行"的用例才会暴露出来。我第一版就是这么写的。
func scopeUserToProject(t *testing.T, store *MemStore, userID, projectName string) {
	t.Helper()
	p := &Project{Name: projectName, TenantID: defaultTenantID}
	if err := store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateUserProfile(userID, func(x *User) { x.DataScope = dataScopeProject }); err != nil {
		t.Fatal(err)
	}
	if err := store.SetUserProjects(defaultTenantID, userID, []string{p.ID}); err != nil {
		t.Fatal(err)
	}
}

func addPlanWithOwner(t *testing.T, store *MemStore, id, ownerName, ownerID string) {
	t.Helper()
	if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: id, Project: "会议中心", WorkContent: "计划 " + id,
		PlanType: planTypeMonthly, OwnerName: ownerName, OwnerID: ownerID,
	}); err != nil {
		t.Fatal(err)
	}
}

// 名字里的空格不该让同一个人被当成两个。
//
// Excel 粘出来的名字中间带空格是常事,中文输入法下敲的还是全角空格 ——
// 和半角肉眼完全一样,靠看是看不出来的。
func TestOwnerNameKeyIgnoresWhitespace(t *testing.T) {
	want := ownerNameKey("胡晓悱")
	for _, variant := range []string{" 胡晓悱", "胡晓悱 ", "胡 晓悱", "胡　晓悱", "\t胡晓悱\n"} {
		if got := ownerNameKey(variant); got != want {
			t.Errorf("ownerNameKey(%q)=%q,应与 %q 相同", variant, got, want)
		}
	}
	// 但不同的人仍要区分开 —— 归一化过头会把两个人合并成一个
	if ownerNameKey("胡晓悱") == ownerNameKey("胡晓明") {
		t.Error("不同的名字被归一成了同一个键")
	}
}

// 报告要把三种情况分开,而不是给一个"匹配率 80%"了事。
func TestOwnerBindingReportSplitsThreeWays(t *testing.T) {
	srv, store, req := bindStore(t)
	addOwnerUser(t, store, "huxf", "胡晓悱")
	// 重名:两个账号都叫「余红星」
	addOwnerUser(t, store, "yuhx1", "余红星")
	addOwnerUser(t, store, "yuhx2", "余红星")

	addPlanWithOwner(t, store, "p-match-1", "胡晓悱", "")
	addPlanWithOwner(t, store, "p-match-2", "胡 晓悱", "") // 带空格,仍应命中
	addPlanWithOwner(t, store, "p-ambi", "余红星", "")
	addPlanWithOwner(t, store, "p-unmatched", "外委-张工", "")

	report, err := srv.buildOwnerBindingReport(req)
	if err != nil {
		t.Fatal(err)
	}

	if len(report.Matched) != 2 {
		// 「胡晓悱」和「胡 晓悱」写法不同,是两个分组,但各自都唯一命中
		t.Fatalf("唯一命中应有 2 组,实际 %d:%+v", len(report.Matched), report.Matched)
	}
	for _, g := range report.Matched {
		if len(g.Candidates) != 1 || g.Candidates[0].Username != "huxf" {
			t.Errorf("分组 %q 的候选不对:%+v", g.OwnerName, g.Candidates)
		}
	}
	if len(report.Ambiguous) != 1 || len(report.Ambiguous[0].Candidates) != 2 {
		t.Errorf("重名应进 ambiguous 且列出两个候选,实际:%+v", report.Ambiguous)
	}
	if len(report.Unmatched) != 1 || report.Unmatched[0].OwnerName != "外委-张工" {
		t.Errorf("外委人员应进 unmatched,实际:%+v", report.Unmatched)
	}
}

// 停用的人不该做候选:绑上去提醒发不出去,而且没人负责 ——
// 比不绑更糟,不绑至少还能从名字看出该找谁。
func TestOwnerBindingSkipsDisabledUsers(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "leaver", "离职的人")
	if err := store.SetUserStatus(u.ID, userStatusDisabled); err != nil {
		t.Fatal(err)
	}
	addPlanWithOwner(t, store, "p1", "离职的人", "")

	report, err := srv.buildOwnerBindingReport(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Matched) != 0 {
		t.Errorf("停用账号不该成为候选,实际匹配到:%+v", report.Matched)
	}
	if len(report.Unmatched) != 1 {
		t.Errorf("应落在 unmatched,实际:%+v", report.Unmatched)
	}
}

// 已经绑过的不再进报告 —— 否则每跑一次都要把全部历史再确认一遍。
func TestOwnerBindingSkipsAlreadyBound(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p-bound", "胡晓悱", u.ID)
	addPlanWithOwner(t, store, "p-free", "胡晓悱", "")

	report, err := srv.buildOwnerBindingReport(req)
	if err != nil {
		t.Fatal(err)
	}
	if report.AlreadyBound != 1 {
		t.Errorf("AlreadyBound=%d,应为 1", report.AlreadyBound)
	}
	if len(report.Matched) != 1 || report.Matched[0].PlanCount != 1 {
		t.Errorf("只有未绑的那条该出现在报告里,实际:%+v", report.Matched)
	}
	if len(report.Matched) == 1 && report.Matched[0].PlanIDs[0] != "p-free" {
		t.Errorf("报告里的计划应是 p-free,实际 %v", report.Matched[0].PlanIDs)
	}
}

// 保存计划时给了不存在的 owner_id,必须当场拒绝。
//
// 放过去的话「我的计划」和点名提醒会安静地查不到这条 ——
// 表现是"这条计划谁都不归",而库里明明写着个 ID。
func TestSavePlanRejectsUnknownOwnerID(t *testing.T) {
	srv, _, _ := bindStore(t)
	// 直接走 handler 才能覆盖到校验 —— store 层是不校验的
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划",
		PlanType: planTypeMonthly, OwnerID: "user_does_not_exist",
	})
	if w.Code != 400 {
		t.Fatalf("应 400 拒绝,实际 %d:%s", w.Code, w.Body.String())
	}
}

// 绑定时名字要跟着账号走 —— 两列并存的代价就是它们可能说不一样的话。
func TestSavePlanAlignsOwnerNameWithAccount(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	_ = req

	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划",
		PlanType: planTypeMonthly,
		OwnerID:  u.ID, OwnerName: "老胡", // 名字和账号不一致
	})
	if w.Code != 201 {
		t.Fatalf("应保存成功,实际 %d:%s", w.Code, w.Body.String())
	}
	got, err := store.GetEngineeringPlan("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerName != "胡晓悱" {
		t.Errorf("OwnerName=%q,应对齐成账号的名字「胡晓悱」", got.OwnerName)
	}
}

// ===== 应用绑定(会写库的那一半)=====

func TestOwnerBindingApplyBindsAndAlignsName(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p1", "胡 晓悱", "") // 名字写法不规范

	w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p1", UserID: u.ID}))
	if w.Code != 200 {
		t.Fatalf("应 200,实际 %d:%s", w.Code, w.Body.String())
	}
	got, err := store.GetEngineeringPlan("p1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OwnerID != u.ID {
		t.Errorf("OwnerID=%q,应为 %q", got.OwnerID, u.ID)
	}
	if got.OwnerName != "胡晓悱" {
		t.Errorf("OwnerName=%q,绑定时应对齐成账号的名字", got.OwnerName)
	}
}

// 解绑:ID 清掉,名字保留 —— 否则这条计划连"该找谁"都看不出来了。
func TestOwnerBindingApplyUnbindKeepsName(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p1", "胡晓悱", u.ID)

	w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p1", UserID: ""}))
	if w.Code != 200 {
		t.Fatalf("应 200,实际 %d:%s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p1")
	if got.OwnerID != "" {
		t.Errorf("OwnerID 应被清空,实际 %q", got.OwnerID)
	}
	if got.OwnerName != "胡晓悱" {
		t.Errorf("解绑不该动名字,实际 %q", got.OwnerName)
	}
}

// 【最要紧的一条】清单里有一条坏的,整批都不能生效。
//
// 放行一半的话,报告是照动手之前那一刻算的 —— 重跑一次结果对不上,
// 人就不知道到底哪些绑上了。宁可一条都不改。
func TestOwnerBindingApplyIsAllOrNothing(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p-good", "胡晓悱", "")

	w := applyBindings(t, srv, req, bindBody(
		bindPair{PlanID: "p-good", UserID: u.ID},
		bindPair{PlanID: "p-does-not-exist", UserID: u.ID},
	))
	if w.Code != 400 {
		t.Fatalf("应 400 拒绝整批,实际 %d:%s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p-good")
	if got.OwnerID != "" {
		t.Errorf("整批被拒时 p-good 不该被改动,实际 OwnerID=%q", got.OwnerID)
	}
}

func TestOwnerBindingApplyRejectsUnknownAndDisabledUser(t *testing.T) {
	srv, store, req := bindStore(t)
	gone := addOwnerUser(t, store, "leaver", "离职的人")
	if err := store.SetUserStatus(gone.ID, userStatusDisabled); err != nil {
		t.Fatal(err)
	}
	addPlanWithOwner(t, store, "p1", "谁", "")

	for name, userID := range map[string]string{
		"不存在的账号": "user_nope",
		"停用的账号":  gone.ID,
	} {
		w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p1", UserID: userID}))
		if w.Code != 400 {
			t.Errorf("%s 应被拒,实际 %d:%s", name, w.Code, w.Body.String())
		}
	}
}

// 同一条计划出现两次:两条相反的指令谁生效取决于顺序,而调用方不会知道
// 自己发了矛盾的东西。直接拒掉,不要挑一条执行。
func TestOwnerBindingApplyRejectsDuplicatePlan(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p1", "胡晓悱", "")

	w := applyBindings(t, srv, req, bindBody(
		bindPair{PlanID: "p1", UserID: u.ID},
		bindPair{PlanID: "p1", UserID: ""},
	))
	if w.Code != 400 {
		t.Fatalf("重复的计划 ID 应被拒,实际 %d:%s", w.Code, w.Body.String())
	}
}

// ===== 看不到就不许派 =====

// 【最要紧的一条】负责人看不到自己被派的活,是个不会报错的死结:
// 派的人以为派出去了,被派的人打开什么都没有。必须在录入时就拦住。
func TestSavePlanRejectsOwnerWhoCannotSeeProject(t *testing.T) {
	srv, store, _ := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	// 这个人被限定在「紫菡雅集」
	scopeUserToProject(t, store, u.ID, "紫菡雅集")

	// 派一条「会议中心」的计划给他 —— 应该被拒
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划",
		PlanType: planTypeMonthly, OwnerID: u.ID,
	})
	if w.Code != 400 {
		t.Fatalf("看不到该项目的人不该能被派活,实际 %d:%s", w.Code, w.Body.String())
	}

	// 派他看得到的项目 —— 应该放行
	w = savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p2", Project: "紫菡雅集", WorkContent: "月度计划",
		PlanType: planTypeMonthly, OwnerID: u.ID,
	})
	if w.Code != 201 {
		t.Fatalf("看得到的项目应该放行,实际 %d:%s", w.Code, w.Body.String())
	}
}

// 看全部数据的人(管理员)不该被这条规则挡住。
//
// 【这一条是防止规则写反】"没有项目清单"有两种含义:看全部,和一个都看不到。
// 混了的话管理员会被从所有候选里筛掉,而这个错在小数据量下很难注意到。
func TestSavePlanAllowsOwnerWhoSeesAllData(t *testing.T) {
	srv, store, _ := bindStore(t)
	u := addOwnerUser(t, store, "boss", "管理员甲")
	if err := store.UpdateUserProfile(u.ID, func(x *User) { x.DataScope = dataScopeAll }); err != nil {
		t.Fatal(err)
	}
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划",
		PlanType: planTypeMonthly, OwnerID: u.ID,
	})
	if w.Code != 201 {
		t.Fatalf("看全部数据的人应该能被派任何项目,实际 %d:%s", w.Code, w.Body.String())
	}
}

// 批量回填是最容易把人绑错的地方 —— 一次几十条,没人会逐条核对。
func TestOwnerBindingApplyRejectsOwnerWhoCannotSeeProject(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	scopeUserToProject(t, store, u.ID, "紫菡雅集")
	addPlanWithOwner(t, store, "p1", "胡晓悱", "") // 这条属于「会议中心」

	w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p1", UserID: u.ID}))
	if w.Code != 400 {
		t.Fatalf("绑给看不到该项目的人应被拒,实际 %d:%s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p1")
	if got.OwnerID != "" {
		t.Errorf("被拒的绑定不该落库,实际 OwnerID=%q", got.OwnerID)
	}
}

// 数据范围要在写这一侧也生效 —— 计划 ID 可以直接写在请求体里,
// 只在报告里裁等于没裁。
func TestOwnerBindingRespectsProjectScope(t *testing.T) {
	srv, req, store, userID := newScopeRequestWithStore(t, roleAdmin, dataScopeProject)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	scopeUserToProject(t, store, userID, "紫菡雅集")
	// 这条属于「会议中心」—— 当前用户被限定在「紫菡雅集」,看不到也不该改到
	addPlanWithOwner(t, store, "p-other", "胡晓悱", "")

	report, err := srv.buildOwnerBindingReport(req)
	if err != nil {
		t.Fatal(err)
	}
	if report.TotalPlans != 0 {
		t.Errorf("报告应裁掉看不到的项目,实际 TotalPlans=%d", report.TotalPlans)
	}

	w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p-other", UserID: u.ID}))
	if w.Code != 400 {
		t.Fatalf("越权改动应被拒,实际 %d:%s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p-other")
	if got.OwnerID != "" {
		t.Errorf("越权的绑定不该落库,实际 OwnerID=%q", got.OwnerID)
	}
}

// ===== 设备必须属于计划的项目 =====

// 混进别的项目的设备,那些设备名会出现在本项目巡检员的今日待巡里 ——
// 数据按计划的项目授权,设备却来自另一个项目,隔离在这里被绕过去。
func TestSavePlanRejectsAssetsFromAnotherProject(t *testing.T) {
	srv, store, _ := bindStore(t)
	mk := func(id, project string) {
		if err := store.CreateAsset(&AssetEntry{
			ID: id, TenantID: defaultTenantID, Project: project,
			AssetType: "电梯", AssetKey: id, AssetName: id,
		}); err != nil {
			t.Fatal(err)
		}
	}
	mk("A-1", "会议中心")
	mk("B-1", "紫菡雅集")

	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "每日例检",
		PlanType: planTypeDaily, AssetIDs: []string{"A-1", "B-1"},
	})
	if w.Code != 400 {
		t.Fatalf("跨项目的设备应被拒,实际 %d:%s", w.Code, w.Body.String())
	}

	// 全是本项目的设备 → 放行
	w = savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p2", Project: "会议中心", WorkContent: "每日例检",
		PlanType: planTypeDaily, AssetIDs: []string{"A-1"},
	})
	if w.Code != 201 {
		t.Fatalf("本项目的设备应放行,实际 %d:%s", w.Code, w.Body.String())
	}
}

// 台账里查不到的设备 ID 也要拒:存进去之后这条计划的完成率永远算不出来
// (那台不会有巡检快照),而看板上它只会一直显示"未完成"。
func TestSavePlanRejectsUnknownAsset(t *testing.T) {
	srv, _, _ := bindStore(t)
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "每日例检",
		PlanType: planTypeDaily, AssetIDs: []string{"不存在的设备"},
	})
	if w.Code != 400 {
		t.Fatalf("台账里没有的设备应被拒,实际 %d:%s", w.Code, w.Body.String())
	}
}

// ===== /api/users 要把每人的项目范围一起给出来 =====
//
// 前端按它筛负责人候选。没给的话前端会退回"不筛"(宁可让后端拦,
// 也别静默少人),表现就是"选了项目也没区别"—— 而这正是被问到的那个现象。
func TestListUsersReturnsProjectScopes(t *testing.T) {
	srv, req, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	limited := addOwnerUser(t, store, "huxf", "胡晓悱")
	scopeUserToProject(t, store, limited.ID, "紫菡雅集")
	plain := addOwnerUser(t, store, "demo9", "普通巡检员") // 没配范围

	w := httptest.NewRecorder()
	srv.handleListUsers(w, req)
	if w.Code != 200 {
		t.Fatalf("应 200,实际 %d:%s", w.Code, w.Body.String())
	}
	var body struct {
		ProjectScopes map[string]struct {
			SeesAll  bool     `json:"seesAll"`
			Projects []string `json:"projects"`
			Blocked  bool     `json:"blocked"`
		} `json:"projectScopes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ProjectScopes == nil {
		t.Fatal("没有返回 projectScopes —— 前端会退回不筛,选项目就没有任何区别")
	}
	got := body.ProjectScopes[limited.ID]
	if got.SeesAll || len(got.Projects) != 1 || got.Projects[0] != "紫菡雅集" {
		t.Errorf("被限定的人应只有紫菡雅集,实际 %+v", got)
	}
	// 【这一条防止把两种"没有项目清单"搞混】没配范围 = 只看自己的,
	// 不按项目限制 —— 不是"一个项目都看不到"。搞混会把大多数人
	// 从所有候选里筛掉。
	if !body.ProjectScopes[plain.ID].SeesAll {
		t.Errorf("没配数据范围的人不该被按项目筛掉,实际 %+v", body.ProjectScopes[plain.ID])
	}
}
