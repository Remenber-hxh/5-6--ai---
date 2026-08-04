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
//   照片:主管看全租户,巡检员只看自己(与 handleListOfflineShots 同)
//   任务:走同一个 filter,再按"在办"三态过滤(与移动端原先的前端过滤同)
// 改动其中任何一处,这里都要跟着改。
func (s *Server) handleBadgeCounts(w http.ResponseWriter, r *http.Request) {
	userID := ""
	if !s.hasSupervisorAccess(r) {
		if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
			userID = user.ID
		}
	}

	// 角标是辅助信息:某一路取不到就返回 0,不让整个接口失败 ——
	// 底栏挂掉会让人以为 app 坏了,而它其实只是个数字。
	shots := 0
	if list, err := s.store.ListOfflineShots(s.tenantForRequest(r), userID, 0); err == nil {
		// 必须只数未成单的:ListOfflineShots 返回全量,已成单的照片列表页不显示。
		// 直接 len(list) 会让角标比用户点进去看到的条数多 —— 那比不显示更糟。
		for _, shot := range list {
			if shotIsPending(shot) {
				shots++
			}
		}
	}

	tasks := 0
	if list, err := s.store.ListEngineeringTasks(engineeringTaskFilterFromRequest(r)); err == nil {
		for _, t := range list {
			if t == nil {
				continue
			}
			switch t.Status {
			case engTaskStatusProcessing, engTaskStatusRectify, engTaskStatusOverdue:
				tasks++
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"shots": shots, "tasks": tasks})
}
