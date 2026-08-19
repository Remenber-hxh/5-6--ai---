package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 端到端:一个被限定在「紫菡雅集」的账号,打接口到底看到什么。
//
// 【为什么要有这一层测试】前面那些测试验的是判定函数,而实际出问题的地方
// 往往是"判定是对的,但某个 handler 根本没调它"。这里直接打 HTTP,
// 走完整条路 —— 少接一个 handler 这里就红。
func newScopedServer(t *testing.T) (*Server, *http.Request, *MemStore) {
	t.Helper()
	srv, r, store, userID := newScopeRequestWithStore(t, roleManager, dataScopeProject)
	zihan := &Project{TenantID: defaultTenantID, Name: "紫菡雅集"}
	huiyi := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	for _, p := range []*Project{zihan, huiyi} {
		if err := store.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	// 只分紫菡雅集
	if err := store.SetUserProjects(defaultTenantID, userID, []string{zihan.ID}); err != nil {
		t.Fatal(err)
	}
	for _, a := range []struct{ project, name string }{
		{"紫菡雅集", "K01"}, {"紫菡雅集", "K02"}, {"会议中心", "KT-7"},
	} {
		if err := store.CreateAsset(&AssetEntry{
			ID: a.project + "::elevator_no_room::" + a.name, TenantID: defaultTenantID,
			Project: a.project, AssetType: "无机房电梯", AssetKey: a.name,
			AssetName: a.name, LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, r, store
}

func TestScopedUserSeesOnlyOwnProjectAssets(t *testing.T) {
	srv, r, _ := newScopedServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/assets", nil)
	req.Header = r.Header
	w := httptest.NewRecorder()
	srv.handleListAssets(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Assets []struct {
			Project   string `json:"project"`
			AssetName string `json:"assetName"`
		} `json:"assets"`
		TotalSummary struct {
			Total int `json:"total"`
		} `json:"totalSummary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Assets) != 2 {
		t.Fatalf("只分到紫菡雅集,应看到 2 台,得到 %d 台", len(resp.Assets))
	}
	for _, a := range resp.Assets {
		if a.Project != "紫菡雅集" {
			t.Fatalf("看到了别的项目的设备:%s / %s", a.Project, a.AssetName)
		}
	}
	// 【汇总数也不能泄】列表筛掉了但顶上写"共 3 台",一样告诉他别处还有设备
	if resp.TotalSummary.Total != 2 {
		t.Fatalf("totalSummary 应为 2,得到 %d —— 汇总数把别的项目也数进去了", resp.TotalSummary.Total)
	}
}

// 直接拿别的项目的设备 id 打详情/历史,必须一律 404。
func TestScopedUserCannotOpenOtherProjectAsset(t *testing.T) {
	srv, r, _ := newScopedServer(t)
	outside := "会议中心::elevator_no_room::KT-7"
	mine := "紫菡雅集::elevator_no_room::K01"

	for _, suffix := range []string{"", "/records", "/report", "/change-requests", "/status-events"} {
		req := httptest.NewRequest(http.MethodGet, "/api/assets/"+outside+suffix, nil)
		req.Header = r.Header
		w := httptest.NewRecorder()
		srv.handleAssetRoutes(w, req)
		if w.Code != http.StatusNotFound {
			t.Errorf("/api/assets/<别的项目>%s 返回 %d,应为 404 —— 这条路径绕过了项目检查",
				suffix, w.Code)
		}
	}
	// 自己项目的必须打得开(别一刀切全挡了)
	req := httptest.NewRequest(http.MethodGet, "/api/assets/"+mine, nil)
	req.Header = r.Header
	w := httptest.NewRecorder()
	srv.handleAssetRoutes(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("自己项目的设备打不开:%d %s", w.Code, w.Body.String())
	}
}

// 巡检记录同样只出本项目的。
func TestScopedUserSeesOnlyOwnProjectRecords(t *testing.T) {
	srv, r, store := newScopedServer(t)
	for _, rec := range []struct{ id, project string }{
		{"r_zihan", "紫菡雅集"}, {"r_huiyi", "会议中心"},
	} {
		if err := store.CreateRecord(&Record{
			ID: rec.id, TenantID: defaultTenantID, Project: rec.project,
			Inspector: "某人", InspectorUserID: "u_other", TemplateID: "zihan_energy",
		}); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(http.MethodGet, "/api/records", nil)
	req.Header = r.Header
	w := httptest.NewRecorder()
	srv.handleListRecords(w, req)

	var resp struct {
		Records []struct {
			ID      string `json:"id"`
			Project string `json:"project"`
		} `json:"records"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(resp.Records) != 1 || resp.Records[0].Project != "紫菡雅集" {
		t.Fatalf("应只看到紫菡雅集的 1 条,得到 %+v", resp.Records)
	}
	// 注意这条记录是【别人】提交的 —— project(非 project_self)要看得到组内其他人的
	if resp.Records[0].ID != "r_zihan" {
		t.Fatalf("拿错了记录:%s", resp.Records[0].ID)
	}
}

// 【这次真实踩到的】台账和记录都筛干净了,一问 AI 全说出来。
//
// 原因是管理 AI 的所有聚合都走 buildInsightsContext,而它只认"用户选了哪个项目",
// 不认"这个人能看哪些项目"。用户不选项目,它就把全租户的数据端上去。
func TestInsightsContextRespectsProjectScope(t *testing.T) {
	srv, r, store := newScopedServer(t) // 只分到「紫菡雅集」
	for _, rec := range []struct{ id, project string }{
		{"r_zihan", "紫菡雅集"}, {"r_huiyi", "会议中心"},
	} {
		if err := store.CreateRecord(&Record{
			ID: rec.id, TenantID: defaultTenantID, Project: rec.project,
			Inspector: "某人", InspectorUserID: "u_other", TemplateID: "zihan_energy",
			CreatedAt: time.Now(),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 用户没有指定项目 —— 这正是泄露发生的场景
	ctx, err := srv.buildInsightsContext(srv.projectScopeFor(r, ""), "30d")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range ctx.assets {
		if a.Project != "紫菡雅集" {
			t.Fatalf("AI 拿到了别的项目的设备:%s / %s", a.Project, a.AssetName)
		}
	}
	if len(ctx.assets) != 2 {
		t.Fatalf("应只有紫菡雅集的 2 台,得到 %d 台", len(ctx.assets))
	}
	for _, rec := range ctx.records {
		if rec.Project != "紫菡雅集" {
			t.Fatalf("AI 拿到了别的项目的记录:%s", rec.ID)
		}
	}
}

// 缓存键必须带上可见项目,否则受限的人会读到管理员刚算好的那份,
// 一分钟内问什么都能问出来 —— 过一分钟又好了,最难复现的那种。
func TestChatContextCacheKeySeparatesScopes(t *testing.T) {
	unrestricted := projectScope{Requested: "会议中心"}
	scoped := projectScope{Requested: "会议中心", Allowed: []string{"会议中心"}}
	if unrestricted.cacheKey() == scoped.cacheKey() {
		t.Fatal("受限和不受限用了同一个缓存键 —— 会串数据")
	}
	// 不受限时必须和加这个字段之前的键完全一致,否则升级瞬间缓存全废
	if unrestricted.cacheKey() != "会议中心" {
		t.Fatalf("不受限时的键变了:%q —— 存量缓存会全部作废", unrestricted.cacheKey())
	}
	blocked := projectScope{Blocked: true}
	empty := projectScope{}
	if blocked.cacheKey() == empty.cacheKey() {
		t.Fatal("Blocked 必须有独立的桶")
	}
}

// 选一个自己没权限的项目,不能借此看到它的数据。
func TestScopeRejectsRequestingForeignProject(t *testing.T) {
	srv, r, _ := newScopedServer(t) // 只分到「紫菡雅集」
	scope := srv.projectScopeFor(r, "会议中心")
	if scope.allows("会议中心") {
		t.Fatal("主动指定别的项目就绕过了范围 —— 这是最容易被试出来的一种")
	}
	if !scope.empty() {
		t.Fatal("指定了无权访问的项目,结果集应为空")
	}
}

// 巡检计划和工程任务也带项目名,一样按项目筛。
func TestPlansAndTasksRespectProjectScope(t *testing.T) {
	srv, r, store := newScopedServer(t) // 只分到「紫菡雅集」
	for _, p := range []struct{ id, project string }{
		{"plan_z", "紫菡雅集"}, {"plan_h", "会议中心"},
	} {
		if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{
			ID: p.id, Project: p.project, Category: "电梯", WorkContent: "月度保养",
		}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tk := range []struct{ id, project string }{
		{"task_z", "紫菡雅集"}, {"task_h", "会议中心"},
	} {
		if err := store.CreateEngineeringTask(&EngineeringTask{
			ID: tk.id, Project: tk.project, Title: "异常复查",
			TaskType: "异常复查", Status: "待整改", AssigneeName: "别人",
		}); err != nil {
			t.Fatal(err)
		}
	}

	planReq := httptest.NewRequest(http.MethodGet, "/api/engineering/plans", nil)
	planReq.Header = r.Header
	w := httptest.NewRecorder()
	srv.handleListEngineeringPlans(w, planReq)
	var planResp struct {
		Plans []struct {
			ID      string `json:"id"`
			Project string `json:"project"`
		} `json:"plans"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &planResp); err != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if len(planResp.Plans) != 1 || planResp.Plans[0].ID != "plan_z" {
		t.Fatalf("计划应只剩紫菡雅集的一条,得到 %+v", planResp.Plans)
	}

	taskReq := httptest.NewRequest(http.MethodGet, "/api/engineering/tasks", nil)
	taskReq.Header = r.Header
	w2 := httptest.NewRecorder()
	srv.handleListEngineeringTasks(w2, taskReq)
	var taskResp struct {
		Tasks []struct {
			ID      string `json:"id"`
			Project string `json:"project"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &taskResp); err != nil {
		t.Fatalf("%d %s", w2.Code, w2.Body.String())
	}
	if len(taskResp.Tasks) != 1 || taskResp.Tasks[0].ID != "task_z" {
		t.Fatalf("任务应只剩紫菡雅集的一条,得到 %+v", taskResp.Tasks)
	}
}
