package main

import "net/http"

// 底栏角标计数。
//
// 【为什么要单开一个接口】
// 移动端底栏要显示两个数字:待处理照片数、在办任务数。原来的做法是把
// /api/inspection/offline-shots 和 /api/engineering/tasks 两个【完整列表】
// 拉下来再取 length —— 实测一次 120 KB,而且底栏常驻、每次切标签都重拉。
// 量过:连切 5 次标签下行 2 MB。巡检员在机房弱网下,这个代价是实打实的。
//
// 【口径必须和列表页一致】
// 角标数字和用户点进去看到的条数对不上,比不显示更糟 —— 会让人以为丢数据。
// 所以这里复用与两个列表 handler 完全相同的可见性规则:
//
//	照片:主管看全租户,巡检员只看自己(与 handleListOfflineShots 同)
//	任务:走同一个 filter,再按"在办"三态过滤(与移动端原先的前端过滤同)
//
// 改动其中任何一处,这里都要跟着改。
func (s *Server) handleBadgeCounts(w http.ResponseWriter, r *http.Request) {
	// 【和列表共用同一个函数,不再各写一遍】原来这里是自己那份
	// canSeeAllData 二选一,和 handleListOfflineShots 一模一样地重复着 ——
	// 于是那边修了 fail-open、加了四档数据范围,这边还是老口径,
	// 角标和点进去的条数就对不上了。而"改动其中任何一处这里都要跟着改"
	// 这句注释,恰恰是靠人记住的,记不住才是常态。
	owners, ok := s.shotOwnersFor(w, r)
	if !ok {
		return // 已写过响应
	}

	// 角标是辅助信息:某一路取不到就返回 0,不让整个接口失败 ——
	// 底栏挂掉会让人以为 app 坏了,而它其实只是个数字。
	// 走 COUNT(*),不把行拉回来。
	//
	// 【踩过的坑】第一版是 ListOfflineShots(..., 0) 再逐条筛。问题有两层:
	// limit<=0 会被 store 当成"默认 100",而且筛在 LIMIT 之后 —— 最新 100 条里
	// 成单的多,未成单的就被截没了。实测页面显示 20 张,角标却报 6。
	// 角标和用户点进去看到的条数对不上,比不显示更糟。
	shots, err := s.store.CountPendingOfflineShots(s.tenantForRequest(r), owners)
	if err != nil {
		shots = 0 // 角标是辅助信息,取不到就显示 0,不让整个接口失败
	}

	// 和任务页共用 openTasksFor 这一个规则(见 open_tasks.go)。
	// 第一版这里是自己数了一遍在办状态 —— 于是角标数全租户 5 条,而页面按
	// "派给我的"只显示 2 条。同一件事有两个口径,用户看到的就是"任务对不上"。
	tasks := 0
	if list, err := s.store.ListEngineeringTasks(s.engineeringTaskFilterFromRequest(r)); err == nil {
		// 项目范围也要跟上:派给他但属于别的项目的任务,列表里看不到,
		// 角标却数进去了 —— 又是一处"数字和页面对不上"。
		list = filterTasksByScope(list, s.projectScopeFor(r, ""))
		tasks = len(openTasksFor(list, s.taskScopeName(r)))
	}

	writeJSON(w, http.StatusOK, map[string]any{"shots": shots, "tasks": tasks})
}
