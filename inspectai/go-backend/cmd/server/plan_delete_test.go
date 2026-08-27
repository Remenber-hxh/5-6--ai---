package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// 删除是不可逆的。这几条用例钉住"什么时候必须拒绝" ——
// 拒绝的规则一旦悄悄失效,丢的是现场真拍的证据,而且没人会立刻发现。

func delReq(t *testing.T, base *http.Request, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodDelete, path, nil)
	r.Header = base.Header.Clone()
	return r
}

// 挂着巡检记录的任务不给删:那条记录是现场真拍的照片和结论,
// 任务删了它就没了来处,台账里查"这次巡检是因为什么"会断链。
func TestDeleteTaskRefusedWhenItHasRecord(t *testing.T) {
	srv, store, req := bindStore(t)
	if err := store.CreateEngineeringTask(&EngineeringTask{
		ID: "t1", Project: "会议中心", Title: "异常复查",
		Status: "已完成", RecordID: "rec_123",
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringTask(w, delReq(t, req, "/api/engineering/tasks/t1"), "t1")
	if w.Code != 409 {
		t.Fatalf("有巡检记录的任务应拒删(409),实际 %d:%s", w.Code, w.Body.String())
	}
	if got, _ := store.GetEngineeringTask("t1"); got == nil {
		t.Error("被拒之后任务不该消失")
	}
}

func TestDeleteTaskWorksWhenNoRecord(t *testing.T) {
	srv, store, req := bindStore(t)
	if err := store.CreateEngineeringTask(&EngineeringTask{
		ID: "t1", Project: "会议中心", Title: "建错了", Status: "待执行",
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringTask(w, delReq(t, req, "/api/engineering/tasks/t1"), "t1")
	if w.Code != 200 {
		t.Fatalf("没有记录的任务应可删,实际 %d:%s", w.Code, w.Body.String())
	}
	if got, _ := store.GetEngineeringTask("t1"); got != nil {
		t.Error("删完还查得到")
	}
}

// 派发过任务的计划不给删:任务的 plan_item_id 会指向一个不存在的东西,
// 而任务还在移动端等人做。
func TestDeletePlanRefusedWhenItHasTasks(t *testing.T) {
	srv, store, req := bindStore(t)
	addPlanWithOwner(t, store, "p1", "朱佳伟", "")
	if err := store.CreateEngineeringTask(&EngineeringTask{
		ID: "t1", PlanItemID: "p1", Project: "会议中心", Title: "已派发", Status: "进行中",
	}); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringPlan(w, delReq(t, req, "/api/engineering/plans/p1"), "p1")
	if w.Code != 409 {
		t.Fatalf("派发过任务的计划应拒删(409),实际 %d:%s", w.Code, w.Body.String())
	}
	if got, _ := store.GetEngineeringPlan("p1"); got == nil {
		t.Error("被拒之后计划不该消失")
	}
}

func TestDeletePlanWorksWhenNoTasks(t *testing.T) {
	srv, store, req := bindStore(t)
	addPlanWithOwner(t, store, "p1", "1", "") // 名字叫「1」的测试垃圾行
	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringPlan(w, delReq(t, req, "/api/engineering/plans/p1"), "p1")
	if w.Code != 200 {
		t.Fatalf("没有任务的计划应可删,实际 %d:%s", w.Code, w.Body.String())
	}
	if got, _ := store.GetEngineeringPlan("p1"); got != nil {
		t.Error("删完还查得到")
	}
}

// 删一个不存在的 ID 必须报 404。
//
// 【为什么要专门钉住】DELETE 影响 0 行时数据库不报错。不检查行数的话
// 接口会返回成功、界面提示"已删除",而那条数据好端端在别人屏幕上。
func TestDeleteMissingReturns404(t *testing.T) {
	srv, _, req := bindStore(t)
	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringPlan(w, delReq(t, req, "/api/engineering/plans/nope"), "nope")
	if w.Code != 404 {
		t.Errorf("不存在的计划应 404,实际 %d:%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	srv.handleDeleteEngineeringTask(w, delReq(t, req, "/api/engineering/tasks/nope"), "nope")
	if w.Code != 404 {
		t.Errorf("不存在的任务应 404,实际 %d:%s", w.Code, w.Body.String())
	}
}

// 越权删除要回"不存在",不回"无权限" —— 否则拿 ID 试一遍就能问出
// 哪些数据存在但我看不到。
func TestDeleteRespectsProjectScope(t *testing.T) {
	srv, req, store, userID := newScopeRequestWithStore(t, roleAdmin, dataScopeProject)
	scopeUserToProject(t, store, userID, "紫菡雅集")
	addPlanWithOwner(t, store, "p-other", "朱佳伟", "") // 属于会议中心

	w := httptest.NewRecorder()
	srv.handleDeleteEngineeringPlan(w, delReq(t, req, "/api/engineering/plans/p-other"), "p-other")
	if w.Code != 404 {
		t.Fatalf("越权删除应回 404(而不是 403),实际 %d:%s", w.Code, w.Body.String())
	}
	if got, _ := store.GetEngineeringPlan("p-other"); got == nil {
		t.Error("越权的删除不该真的删掉")
	}
}
