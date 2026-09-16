package main

import "testing"

// ===== 项目名单也要按项目范围裁 =====
//
// 【为什么补这一条】原来只有数据在裁,名单一点不裁:只分到会议中心的人,
// 看不到紫菡的任何一条数据,却在项目管理页看得见"紫菡雅集"这个项目名、
// 编号、有几台设备、几个人。
//
// 除了泄露,这个不一致还会直接骗到人 —— 项目页列得出的项目,台账里一台
// 设备都没有,看着就是"我建的设备不见了"。线上真为这个查了半天。

func projList() []*Project {
	return []*Project{
		{ID: "p1", Name: "会议中心"},
		{ID: "p2", Name: "紫菡雅集"},
	}
}

func namesOf(list []*Project) []string {
	out := make([]string, 0, len(list))
	for _, p := range list {
		out = append(out, p.Name)
	}
	return out
}

// 【最要紧的一条】没配项目范围的人(管理员、以及 data_scope 为空回退成
// 全部数据的人)行为必须和以前【完全一样】—— 裁错了的表现是
// "升级当天管理员打开后台,项目一个都没有了"。
func TestUnrestrictedSeesEveryProject(t *testing.T) {
	for name, vis := range map[string]dataVisibility{
		"全部数据":    {AllData: true},
		"没配项目范围":  {},
		"只看自己提交的": {OwnOnly: true},
	} {
		if !unrestricted(vis) {
			t.Errorf("%s 被当成了受限,项目名单会被裁空", name)
		}
	}
}

// 分到会议中心的人,名单里不该出现紫菡雅集。
func TestScopedUserSeesOnlyOwnProjects(t *testing.T) {
	vis := dataVisibility{Projects: []string{"会议中心"}}
	if unrestricted(vis) {
		t.Fatal("配了项目却被当成不受限")
	}
	kept := []*Project{}
	for _, p := range projList() {
		if vis.allowsProject(p.Name) {
			kept = append(kept, p)
		}
	}
	got := namesOf(kept)
	if len(got) != 1 || got[0] != "会议中心" {
		t.Errorf("裁剪结果不对:%v —— 名单里能看到别的项目就是泄露", got)
	}
}

// 被拦下的人(配了项目范围却查不出他属于哪个项目)一个项目都不该看到。
// 【不能退化成"看全部"】—— 判断不了归属时放行,等于权限形同虚设。
func TestBlockedUserSeesNoProject(t *testing.T) {
	vis := dataVisibility{Blocked: true, OwnOnly: true}
	if unrestricted(vis) {
		t.Fatal("被拦下的人却被当成不受限 —— 会看到全部项目")
	}
	for _, p := range projList() {
		if vis.allowsProject(p.Name) {
			t.Errorf("被拦下的人仍看得到 %s", p.Name)
		}
	}
}

// 名单和数据必须用同一个判断 —— 分叉的表现是"项目页有、台账里没有",
// 而两边都不报错。这条直接对着 unrestricted 守,它就是那个唯一的判断。
func TestSameRuleForProjectsAndAssets(t *testing.T) {
	cases := []dataVisibility{
		{AllData: true},
		{},
		{OwnOnly: true},
		{Projects: []string{"会议中心"}},
		{Blocked: true, OwnOnly: true},
	}
	for _, vis := range cases {
		// limitAssetsToVisibleProjects 和 limitProjectsToVisible 都走 unrestricted,
		// 这里断言的是"同一个输入得到同一个结论"。
		want := vis.AllData || (len(vis.Projects) == 0 && !vis.Blocked)
		if unrestricted(vis) != want {
			t.Errorf("%+v:名单和数据的裁剪口径分叉了", vis)
		}
	}
}
