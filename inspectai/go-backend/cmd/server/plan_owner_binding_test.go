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

func bindStore(t *testing.T) (*Server, *MemStore) {
	t.Helper()
	srv, _, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	return srv, store
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
	srv, store := bindStore(t)
	addOwnerUser(t, store, "huxf", "胡晓悱")
	// 重名:两个账号都叫「余红星」
	addOwnerUser(t, store, "yuhx1", "余红星")
	addOwnerUser(t, store, "yuhx2", "余红星")

	addPlanWithOwner(t, store, "p-match-1", "胡晓悱", "")
	addPlanWithOwner(t, store, "p-match-2", "胡 晓悱", "") // 带空格,仍应命中
	addPlanWithOwner(t, store, "p-ambi", "余红星", "")
	addPlanWithOwner(t, store, "p-unmatched", "外委-张工", "")

	report, err := srv.buildOwnerBindingReport()
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
	srv, store := bindStore(t)
	u := addOwnerUser(t, store, "leaver", "离职的人")
	if err := store.SetUserStatus(u.ID, userStatusDisabled); err != nil {
		t.Fatal(err)
	}
	addPlanWithOwner(t, store, "p1", "离职的人", "")

	report, err := srv.buildOwnerBindingReport()
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
	srv, store := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p-bound", "胡晓悱", u.ID)
	addPlanWithOwner(t, store, "p-free", "胡晓悱", "")

	report, err := srv.buildOwnerBindingReport()
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
	srv, _ := bindStore(t)
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
	srv, store := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")

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
