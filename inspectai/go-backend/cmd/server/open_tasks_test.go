package main

import "testing"

// 底栏角标和任务页必须数出同一批任务。
//
// 立这个测试是因为真踩过:角标数全租户在办 5 条,任务页在客户端按"派给我的"
// 又筛了一遍只剩 2 条 —— 用户看到的就是"任务对不上"。
// 现在两边都走 openTasksFor,这里把规则本身钉死。
func TestOpenTasksScope(t *testing.T) {
	all := []*EngineeringTask{
		{ID: "a", Status: engTaskStatusProcessing, AssigneeName: "胡晓悱"},
		{ID: "b", Status: engTaskStatusRectify, AssigneeName: "胡晓悱"},
		{ID: "c", Status: engTaskStatusOverdue, AssigneeName: "余红星"},
		{ID: "d", Status: engTaskStatusProcessing, AssigneeName: ""}, // 未指派
		// 下面这些都不算"在办",谁都不该看到
		{ID: "e", Status: engTaskStatusDone, AssigneeName: "胡晓悱"},
		{ID: "f", Status: engTaskStatusCanceled, AssigneeName: "胡晓悱"},
		{ID: "g", Status: engTaskStatusPending, AssigneeName: "胡晓悱"}, // 待执行=还没下发
		{ID: "h", Status: engTaskStatusDraft, AssigneeName: "胡晓悱"},
		nil, // 防御:列表里混进 nil 不能 panic
	}

	ids := func(list []*EngineeringTask) []string {
		out := make([]string, 0, len(list))
		for _, t := range list {
			out = append(out, t.ID)
		}
		return out
	}
	eq := func(got []string, want ...string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	// 主管 / 管理员:全部在办,不按人过滤
	if g := ids(openTasksFor(all, "")); !eq(g, "a", "b", "c", "d") {
		t.Fatalf("主管应看到全部在办 [a b c d],得到 %v", g)
	}

	// 巡检员:自己的 + 未指派的。不能看到别人的(c 是余红星的)
	if g := ids(openTasksFor(all, "胡晓悱")); !eq(g, "a", "b", "d") {
		t.Fatalf("胡晓悱应看到 [a b d],得到 %v", g)
	}

	// 一条都没派到的人:只看到未指派的,【不】退回显示所有人的 ——
	// 原来的兜底既让人看到别人的活,又是角标对不上的来源
	if g := ids(openTasksFor(all, "查无此人")); !eq(g, "d") {
		t.Fatalf("没派到活的人只该看到未指派的 [d],得到 %v", g)
	}

	// 已完成/已取消/待执行/待下发一个都不能漏出来
	for _, name := range []string{"", "胡晓悱"} {
		for _, task := range openTasksFor(all, name) {
			if !openTaskStatuses[task.Status] {
				t.Fatalf("非在办任务漏出来了: %s(%s)", task.ID, task.Status)
			}
		}
	}
}
