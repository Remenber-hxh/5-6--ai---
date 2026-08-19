package main

import (
	"net/http"
	"strings"
)

// ===== 「我的在办任务」的唯一口径 =====
//
// 【为什么要单独抽出来】
// 底栏角标数出 5 条,任务页只显示 2 条 —— 因为角标数的是全租户在办任务,
// 而页面在客户端按"派给我的"又筛了一遍。用户看到的就是"任务对不上"。
//
// 这和照片那次是同一个病:后端发全量、前端各自筛,于是同一件事有两个口径。
// 现在把规则收到这一个函数里,列表接口和角标接口都调它 —— 想让它们不一致
// 都难。改规则也只改这一处。
//
// 【规则】
//   在办 = 进行中 / 待整改 / 逾期
//     「待执行/待下发」是管理员还没下发的,巡检员不该看到;已完成/已取消同理。
//   主管、管理员 → 全部(他们负责派活,要看全局)
//   巡检员       → 指派给自己的 + 【未指派的】
//     未指派的要给巡检员看:任务没填负责人时不能让人看到空列表。
//     但【不】退回显示全部 —— 原来的兜底是"我一条都没有就显示所有人的",
//     那既让人看到别人的活,又正是角标对不上的来源。

var openTaskStatuses = map[string]bool{
	engTaskStatusProcessing: true,
	engTaskStatusRectify:    true,
	engTaskStatusOverdue:    true,
}

// openTasksFor 从全量任务里挑出该给这个人看的在办任务。
// displayName 为空(主管/管理员)表示不按人过滤。
func openTasksFor(all []*EngineeringTask, displayName string) []*EngineeringTask {
	out := make([]*EngineeringTask, 0, len(all))
	for _, t := range all {
		if t == nil || !openTaskStatuses[t.Status] {
			continue
		}
		if displayName != "" {
			owner := strings.TrimSpace(t.AssigneeName)
			if owner != "" && owner != displayName {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// taskScopeName 返回该按谁过滤:主管/管理员看全部(返回空串),
// 巡检员按自己的显示名过滤。
//
// 用显示名而不是用户 ID,是因为任务表里存的就是 AssigneeName ——
// 这是历史结构,改成 ID 要迁数据,先按现状对齐口径。
func (s *Server) taskScopeName(r *http.Request) string {
	// 数据范围:能看全部的人不限归属;其余只看派给自己的
	if s.canSeeAllData(r) {
		return ""
	}
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		return strings.TrimSpace(user.DisplayName)
	}
	return ""
}
