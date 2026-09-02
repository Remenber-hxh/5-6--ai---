package main

import "net/http"

// ===== 照片能看谁的 =====
//
// 照片表里没有项目字段,只有上传人(user_id)。所以"本项目的照片"只能
// 翻译成"同项目那些人上传的照片" —— 这一层专门做这个翻译。
//
// 【单独一个函数,不写在 handler 里】它的每一条分支都是"能不能看到别人
// 现场照"的答案,而 owners 返回 nil 的含义是【不限】—— 漏掉一条分支
// 就是把全租户的照片交出去,而且不报错。集中在一处才看得全。

// shotOwnersFor 返回允许查看的上传人 ID 名单。
//
//	nil, true   → 不限(能看全部)
//	[...], true → 只看这些人的
//	_, false    → 已经写过响应,调用方直接 return
func (s *Server) shotOwnersFor(w http.ResponseWriter, r *http.Request) ([]string, bool) {
	vis := s.visibilityFor(r)

	// 能看全部:不加归属限制
	if vis.AllData {
		return nil, true
	}

	// 配了项目范围却一个项目都没分到 → 什么都看不到。
	// 【返回一个不可能匹配的名单,不是 nil】nil 的含义是"不限",
	// 在这条最该收紧的分支上返回它,后果正好相反。
	if vis.Blocked {
		return []string{"__blocked__"}, true
	}

	user, ok := s.userFromSessionToken(s.tokenFromRequest(r))
	if !ok {
		// 【认不出是谁就拒绝,不能放行】原来这里什么都不做,于是 owners 留空 =
		// 不限 = 看到全租户所有照片 —— 而进到这个分支恰恰说明"他不能看全部"。
		// 本地免鉴权是开发用的口子,单独放过。
		if s.localNoAuthAllowed(r) {
			return nil, true
		}
		writeError(w, http.StatusForbidden, "forbidden", "请使用巡检员账号登录后查看照片")
		return nil, false
	}

	// 只看自己:仅自己 / 本项目(记录仅自己)
	if vis.OwnOnly || len(vis.Projects) == 0 {
		return []string{user.ID}, true
	}

	// 本项目:同项目成员上传的都能看。
	// 【自己永远在名单里】项目归属查询出问题时,至少不该把自己的照片也弄丢 ——
	// 那会表现成"我刚拍的照片不见了",比看不到同事的严重得多。
	ids, err := s.store.ListUserIDsInProjects(s.tenantForRequest(r), vis.Projects)
	if err != nil {
		return []string{user.ID}, true
	}
	owners := make([]string, 0, len(ids)+1)
	owners = append(owners, user.ID)
	for _, id := range ids {
		if id != "" && id != user.ID {
			owners = append(owners, id)
		}
	}
	return owners, true
}
