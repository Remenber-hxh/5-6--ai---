package main

import (
	"path/filepath"
	"testing"
	"time"
)

// 项目实体化。这一步和第 1 步一样,**建表不改行为** ——
// user_projects 空着就等于没人被限定,所有查询照旧。
// 所以测试盯的是:回填别漏、别重、跨租户别串。

func newProjectStore(t *testing.T) *SQLiteStore {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "proj.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

// 升级后后台项目列表【必须】能看到台账里已有的项目。
// 空列表会让管理员以为项目丢了,然后手建一个同名的 —— 差一个字就和台账脱钩。
func TestProjectBackfillPicksUpExistingNames(t *testing.T) {
	store := newProjectStore(t)
	for _, name := range []string{"会议中心", "紫菡雅集"} {
		if err := store.CreateAsset(&AssetEntry{
			ID: name + "::elevator_no_room::K01", TenantID: defaultTenantID,
			Project: name, AssetType: "无机房电梯", AssetKey: "K01",
			AssetName: "K01", LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 迁移在建库时已经跑过一次(那时还没有资产),这里重跑一次模拟"升级到有数据的库"
	if err := store.migProjects(); err != nil {
		t.Fatalf("migProjects: %v", err)
	}
	list, err := store.ListProjects(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Project{}
	for _, p := range list {
		got[p.Name] = p
	}
	for _, name := range []string{"会议中心", "紫菡雅集"} {
		p, ok := got[name]
		if !ok {
			t.Fatalf("台账里有 %s,回填后项目列表却没有 —— 管理员会以为项目丢了", name)
		}
		if p.AssetCount != 1 {
			t.Errorf("%s 的设备数应为 1,得到 %d", name, p.AssetCount)
		}
	}
	// 【幂等】迁移重跑不能建出第二条同名项目
	if err := store.migProjects(); err != nil {
		t.Fatal(err)
	}
	again, _ := store.ListProjects(defaultTenantID)
	if len(again) != len(list) {
		t.Fatalf("重跑迁移后项目从 %d 条变成 %d 条 —— 回填不幂等", len(list), len(again))
	}
}

// 归属关系:设了才限,没设等于不限。
func TestUserProjectsRoundTrip(t *testing.T) {
	store := newProjectStore(t)
	a := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	b := &Project{TenantID: defaultTenantID, Name: "紫菡雅集"}
	for _, p := range []*Project{a, b} {
		if err := store.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	names, err := store.ListUserProjectNames(defaultTenantID, "user_x")
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 0 {
		t.Fatalf("没分配过的人应返回空(= 不受项目限制),得到 %v", names)
	}

	if err := store.SetUserProjects(defaultTenantID, "user_x", []string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	if names, _ = store.ListUserProjectNames(defaultTenantID, "user_x"); len(names) != 2 {
		t.Fatalf("一个人可以同时属于多个项目,得到 %v", names)
	}

	// 覆盖式:重设成一个,另一个必须消失(不是累加)
	if err := store.SetUserProjects(defaultTenantID, "user_x", []string{a.ID}); err != nil {
		t.Fatal(err)
	}
	if names, _ = store.ListUserProjectNames(defaultTenantID, "user_x"); len(names) != 1 || names[0] != "会议中心" {
		t.Fatalf("重设应覆盖而不是追加,得到 %v", names)
	}

	// 停用的项目不再参与过滤 —— 否则停用了还在限制人,查不出原因
	if err := store.UpdateProjectMeta(defaultTenantID, a.ID, "", true); err != nil {
		t.Fatal(err)
	}
	if names, _ = store.ListUserProjectNames(defaultTenantID, "user_x"); len(names) != 0 {
		t.Fatalf("停用的项目不该再出现在归属里,得到 %v", names)
	}

	// 清空
	if err := store.SetUserProjects(defaultTenantID, "user_x", nil); err != nil {
		t.Fatal(err)
	}
	if ids, _ := store.ListUserProjectIDs(defaultTenantID, "user_x"); len(ids) != 0 {
		t.Fatalf("清空后应无归属,得到 %v", ids)
	}
}

// 【跨租户】传一个别家租户的项目 id 进来,不能把人挂上去。
func TestSetUserProjectsRejectsOtherTenant(t *testing.T) {
	store := newProjectStore(t)
	mine := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	theirs := &Project{TenantID: "tenant_other", Name: "别家项目"}
	for _, p := range []*Project{mine, theirs} {
		if err := store.CreateProject(p); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.SetUserProjects(defaultTenantID, "user_x", []string{mine.ID, theirs.ID}); err != nil {
		t.Fatal(err)
	}
	ids, _ := store.ListUserProjectIDs(defaultTenantID, "user_x")
	if len(ids) != 1 || ids[0] != mine.ID {
		t.Fatalf("别家租户的项目被挂上了:%v —— 这是跨租户越权", ids)
	}
}

// 项目名在租户内唯一。名字是业务表的关联键,重名之后成员挂哪一条都对不上台账。
func TestProjectNameUniquePerTenant(t *testing.T) {
	store := newProjectStore(t)
	if err := store.CreateProject(&Project{TenantID: defaultTenantID, Name: "会议中心"}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(&Project{TenantID: defaultTenantID, Name: "会议中心"}); err == nil {
		t.Fatal("同租户重名应被拒 —— 重名项目会让成员和台账对不上")
	}
	// 不同租户可以同名(各家的"会议中心"互不相干)
	if err := store.CreateProject(&Project{TenantID: "tenant_other", Name: "会议中心"}); err != nil {
		t.Fatalf("不同租户应允许同名:%v", err)
	}
}

// MemStore 要和 SQLite 表现一致,否则用内存实现写的测试保不住真库的行为。
func TestMemStoreProjectsMatchSQLiteBehaviour(t *testing.T) {
	store := NewMemStore()
	p := &Project{TenantID: defaultTenantID, Name: "会议中心"}
	if err := store.CreateProject(p); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateProject(&Project{TenantID: defaultTenantID, Name: "会议中心"}); err == nil {
		t.Fatal("MemStore 也要拒绝重名")
	}
	if err := store.SetUserProjects(defaultTenantID, "u1", []string{p.ID, "proj_not_exist"}); err != nil {
		t.Fatal(err)
	}
	ids, _ := store.ListUserProjectIDs(defaultTenantID, "u1")
	if len(ids) != 1 {
		t.Fatalf("不存在的项目 id 应被忽略,得到 %v", ids)
	}
}

// 【这个项目反复踩的坑】"先取一个窗口再在内存里筛"。
//
// 记录列表默认只取最新 100 条。如果按项目过滤是在拿到这 100 条之后做的,
// 那么一个安静的项目(最近没人巡)会被前面 100 条热闹项目的记录挤掉,
// 结果是【一条都查不出来】,而且页面不会报错,只会显示"暂无数据"。
// 所以过滤必须发生在 SQL 里。
func TestListRecordsInProjectsFiltersInSQLNotInWindow(t *testing.T) {
	store := newProjectStore(t)
	base := time.Now()
	// 会议中心:120 条最近的
	for i := range 120 {
		if err := store.CreateRecord(&Record{
			ID: "hot_" + itoaSafe(i), TenantID: defaultTenantID,
			Project: "会议中心", Inspector: "甲", InspectorUserID: "u_a",
			TemplateID: "zihan_energy", CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 紫菡雅集:5 条更早的 —— 全部落在最新 100 条之外
	for i := range 5 {
		if err := store.CreateRecord(&Record{
			ID: "cold_" + itoaSafe(i), TenantID: defaultTenantID,
			Project: "紫菡雅集", Inspector: "乙", InspectorUserID: "u_b",
			TemplateID: "zihan_energy", CreatedAt: base.Add(-time.Duration(i+1) * time.Hour),
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListRecordsInProjects(defaultTenantID, []string{"紫菡雅集"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 5 {
		t.Fatalf("紫菡雅集应有 5 条,得到 %d 条 —— 多半是先取窗口再筛,冷项目被挤没了", len(got))
	}
	for _, rec := range got {
		if rec.Project != "紫菡雅集" {
			t.Fatalf("混进了别的项目:%s", rec.Project)
		}
	}

	// 多个项目一起取
	both, err := store.ListRecordsInProjects(defaultTenantID, []string{"会议中心", "紫菡雅集"}, 500)
	if err != nil {
		t.Fatal(err)
	}
	if len(both) != 125 {
		t.Fatalf("两个项目共 125 条,得到 %d", len(both))
	}

	// 【空项目列表返回空,不是返回全部】空的语义是"没有可见项目"。
	// 返回全部的话,一个没分到项目的人就看到了整个租户。
	none, err := store.ListRecordsInProjects(defaultTenantID, nil, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(none) != 0 {
		t.Fatalf("没有可见项目时应返回空,得到 %d 条 —— 这是越权", len(none))
	}
}

// 【后门】页面上过滤掉了,但问一句 AI 就说出来。
//
// find_asset 的 project 参数是"模型说要在哪个项目找",不是权限判断 ——
// 模型不传或传错,结果里就会混进别的项目。所以出口必须再筛一道。
func TestAgentResultsRespectProjectScope(t *testing.T) {
	vis := dataVisibility{Projects: []string{"会议中心"}}
	found := map[string]any{
		"count": 3,
		"assets": []map[string]any{
			{"name": "KT-7", "project": "会议中心"},
			{"name": "K01", "project": "紫菡雅集"},
			{"name": "KT-8", "project": "会议中心"},
		},
		"nearMatches": []map[string]any{
			{"name": "K02", "project": "紫菡雅集"},
		},
	}
	got := limitAgentAssetsToProjects(found, vis)
	list, _ := got["assets"].([]map[string]any)
	if len(list) != 2 {
		t.Fatalf("应只剩会议中心的 2 台,得到 %d", len(list))
	}
	for _, a := range list {
		if a["project"] != "会议中心" {
			t.Fatalf("别的项目的设备漏给了 AI:%v", a)
		}
	}
	// count 必须跟着改,否则模型会说"一共 3 台"而列表里只有 2 台
	if got["count"] != 2 {
		t.Fatalf("count 应随裁剪改成 2,得到 %v —— 模型会照旧数字回答", got["count"])
	}
	// 候选里全是别的项目 → 整个键去掉,别给模型一个空列表去猜
	if _, has := got["nearMatches"]; has {
		t.Fatalf("越界的候选没被清掉:%v", got["nearMatches"])
	}

	// 能看全部的人不受影响
	all := limitAgentAssetsToProjects(map[string]any{
		"count":  1,
		"assets": []map[string]any{{"name": "K01", "project": "紫菡雅集"}},
	}, dataVisibility{AllData: true})
	if len(all["assets"].([]map[string]any)) != 1 {
		t.Fatal("能看全部的人被误伤了")
	}
}
