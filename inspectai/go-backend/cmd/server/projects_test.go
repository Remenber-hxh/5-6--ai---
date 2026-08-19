package main

import (
	"path/filepath"
	"testing"
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
