package main

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// ===== 计划负责人 → 账号 的绑定 =====
//
// 存量计划的 owner_name 是历史上手打进去的自由文本。要让「我的计划」和
// 点名提醒真正准,得把它对应到账号 ID(见 EngineeringPlanItem.OwnerID)。
//
// 【这件事分两半,中间必须有人】
//
//	报告(只读) 把名字和账号的对应关系算出来,分成三堆:
//	           唯一命中 / 命中多个 / 谁都不是
//	应用(写库) 只接受一份显式的 planId → userId 清单
//
// 【为什么不做"跑一遍自动全绑"】按名字模糊匹配去猜谁是谁,重名、改过名、
// 名字写法不一致的会被静默绑错。而"绑错人"的表现是每日提醒发给了不该发的人 ——
// 没有任何报错,得等到有人抱怨才发现,那时候已经错了很多天。
// 一次人工确认换掉这个风险,很划算。

// ownerBindingCandidate 一个可能对应的账号。
type ownerBindingCandidate struct {
	UserID         string `json:"userId"`
	DisplayName    string `json:"displayName"`
	Username       string `json:"username"`
	DepartmentName string `json:"departmentName,omitempty"`
	RoleName       string `json:"roleName,omitempty"`
}

// ownerBindingGroup 一个负责人名字,以及挂在它下面的计划。
type ownerBindingGroup struct {
	OwnerName string   `json:"ownerName"`
	PlanIDs   []string `json:"planIds"`
	PlanCount int      `json:"planCount"`
	// Candidates 匹配到的账号。唯一命中时只有一个;命中多个时全列出来让人挑。
	Candidates []ownerBindingCandidate `json:"candidates,omitempty"`
	// BlockedPlanIDs 名字对上了,但这个人的数据范围里没有那条计划的项目 ——
	// 绑上去他也看不到,所以写入时会被拒。
	//
	// 【报告必须先说】不说的话,界面显示"唯一命中"、人点了"全部绑定",
	// 然后整批被拒 —— 而报告刚刚还告诉他这些是能绑的。
	// 只在唯一命中时算:命中多个时选哪个还没定,算不出结果。
	BlockedPlanIDs []string `json:"blockedPlanIds,omitempty"`
	// BlockedNote 给人看的原因,形如「胡晓悱 看不到:会议中心」
	BlockedNote string `json:"blockedNote,omitempty"`
	// projects 与 PlanIDs 一一对应,只在算 BlockedPlanIDs 时用,不出 JSON。
	projects []string
}

// ownerBindingReport 绑定现状报告。纯只读,不改任何数据。
type ownerBindingReport struct {
	Matched      []ownerBindingGroup `json:"matched"`
	Ambiguous    []ownerBindingGroup `json:"ambiguous"`
	Unmatched    []ownerBindingGroup `json:"unmatched"`
	AlreadyBound int                 `json:"alreadyBound"`
	NoOwner      int                 `json:"noOwner"`
	TotalPlans   int                 `json:"totalPlans"`
}

// ownerNameKey 把名字归一成用于比对的键。
//
// 【去掉全部空白,不只是首尾】"胡 晓悱" 和 "胡晓悱" 是同一个人,而 Excel
// 里粘出来的名字中间带空格是常事。全角空格(U+3000)也要算 —— 中文输入法
// 下敲出来的空格默认就是它,肉眼和半角完全一样。
//
// 【这只是"给人看的候选",不是判定】归一化越激进,误配的可能越大 ——
// 所以它的输出只用来生成报告,最终绑不绑由人点头。
func ownerNameKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '　', ' ':
			continue
		}
		b.WriteRune(r)
	}
	return strings.ToLower(b.String())
}

// buildOwnerBindingReport 算出当前的绑定现状。只读。
//
// 【要按数据范围裁】角色和数据范围是正交的:一个管理员也可以被限定只看某几个
// 项目。不裁的话,他能从这份报告里读到别的项目有谁、各有多少条计划 ——
// 名单本身就是信息,而且和列表页的口径对不上(那边是裁过的)。
func (s *Server) buildOwnerBindingReport(r *http.Request) (*ownerBindingReport, error) {
	plans, err := s.store.ListEngineeringPlans(EngineeringPlanFilter{})
	if err != nil {
		return nil, err
	}
	plans = filterPlansByScope(plans, s.projectScopeFor(r, ""))
	users, err := s.store.ListUsers()
	if err != nil {
		return nil, err
	}
	tenant := s.tenantForRequest(r)

	// 名字键 → 账号。一个键可能对上多个账号(重名),所以是切片不是单值。
	byKey := map[string][]ownerBindingCandidate{}
	add := func(key string, c ownerBindingCandidate) {
		if key == "" {
			return
		}
		for _, exist := range byKey[key] {
			if exist.UserID == c.UserID {
				return // 同一个人的姓名和用户名都命中,只算一次
			}
		}
		byKey[key] = append(byKey[key], c)
	}
	for _, u := range users {
		if u == nil || u.Status == userStatusDisabled {
			// 【停用的人不做候选】把计划绑到停用账号上,提醒发不出去而且没人负责,
			// 比不绑更糟 —— 不绑至少还能从名字看出该找谁。
			continue
		}
		c := ownerBindingCandidate{
			UserID:         u.ID,
			DisplayName:    u.DisplayName,
			Username:       u.Username,
			DepartmentName: u.DepartmentName,
			RoleName:       u.RoleName,
		}
		add(ownerNameKey(u.DisplayName), c)
		add(ownerNameKey(u.Username), c)
	}

	report := &ownerBindingReport{
		Matched:   []ownerBindingGroup{},
		Ambiguous: []ownerBindingGroup{},
		Unmatched: []ownerBindingGroup{},
	}
	groups := map[string]*ownerBindingGroup{} // 按原始名字分组,保留人看得懂的写法
	for _, p := range plans {
		if p == nil {
			continue
		}
		report.TotalPlans++
		if strings.TrimSpace(p.OwnerID) != "" {
			report.AlreadyBound++
			continue
		}
		name := strings.TrimSpace(p.OwnerName)
		if name == "" {
			report.NoOwner++
			continue
		}
		g := groups[name]
		if g == nil {
			g = &ownerBindingGroup{OwnerName: name, Candidates: byKey[ownerNameKey(name)]}
			groups[name] = g
		}
		g.PlanIDs = append(g.PlanIDs, p.ID)
		g.PlanCount++
		g.projects = append(g.projects, p.Project)
	}

	// 唯一命中的,再核一遍"这个人看得到那条计划的项目吗"。
	// 写入时反正会拒,不如在报告里就说清楚 —— 否则人点了"全部绑定"
	// 才被整批打回来,而报告刚刚还说这些是能绑的。
	userByID := map[string]*User{}
	for _, u := range users {
		if u != nil {
			userByID[u.ID] = u
		}
	}
	for _, g := range groups {
		if len(g.Candidates) != 1 {
			continue
		}
		u := userByID[g.Candidates[0].UserID]
		if u == nil {
			continue
		}
		vis := s.visibilityForUser(tenant, u)
		seen := map[string]bool{}
		var badProjects []string
		for i, pid := range g.PlanIDs {
			proj := g.projects[i]
			if vis.allowsProject(proj) {
				continue
			}
			g.BlockedPlanIDs = append(g.BlockedPlanIDs, pid)
			if !seen[proj] {
				seen[proj] = true
				badProjects = append(badProjects, firstNonEmpty(proj, "(未指定项目)"))
			}
		}
		if len(badProjects) > 0 {
			g.BlockedNote = g.Candidates[0].DisplayName + " 的数据范围里没有:" +
				strings.Join(badProjects, "、")
		}
	}

	for _, g := range groups {
		switch len(g.Candidates) {
		case 0:
			report.Unmatched = append(report.Unmatched, *g)
		case 1:
			report.Matched = append(report.Matched, *g)
		default:
			report.Ambiguous = append(report.Ambiguous, *g)
		}
	}
	// 计划多的排前面 —— 人工确认时先处理影响面大的
	byImpact := func(a []ownerBindingGroup) {
		sort.Slice(a, func(i, j int) bool {
			if a[i].PlanCount != a[j].PlanCount {
				return a[i].PlanCount > a[j].PlanCount
			}
			return a[i].OwnerName < a[j].OwnerName
		})
	}
	byImpact(report.Matched)
	byImpact(report.Ambiguous)
	byImpact(report.Unmatched)
	return report, nil
}

// handleOwnerBindingReport —— GET /api/engineering/plans/owner-binding
func (s *Server) handleOwnerBindingReport(w http.ResponseWriter, r *http.Request) {
	report, err := s.buildOwnerBindingReport(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build_report_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

// ownerBindingApplyReq 显式的绑定清单。
//
// 【只吃 planId + userId,不吃名字】接口这一层不做任何匹配 ——
// 匹配的结果是报告,人看过之后把选中的那些回传。这样"绑给谁"这个决定
// 有且只有一个来源:那个点头的人。
type ownerBindingApplyReq struct {
	Bindings []struct {
		PlanID string `json:"planId"`
		// UserID 空 = 解绑(把 owner_id 清掉,名字照旧保留)
		UserID string `json:"userId"`
	} `json:"bindings"`
}

// handleOwnerBindingApply —— POST /api/engineering/plans/owner-binding
//
// 【先全量校验,再动手】中途失败会留下"一半绑了一半没绑"的状态,
// 而报告是照着动手之前那一刻算的 —— 重跑一次结果对不上,人就不知道
// 到底哪些生效了。宁可一条都不改,也不要改一半。
func (s *Server) handleOwnerBindingApply(w http.ResponseWriter, r *http.Request) {
	var req ownerBindingApplyReq
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if len(req.Bindings) == 0 {
		writeError(w, http.StatusBadRequest, "empty_bindings", "没有要应用的绑定")
		return
	}

	type resolved struct {
		plan *EngineeringPlanItem
		user *User
	}
	// 【写这一侧也要裁范围】只在报告里裁没用 —— 计划 ID 是可以直接写在
	// 请求体里的,不裁的话被限定项目的管理员能改到别的项目的计划,
	// 而他在界面上根本看不到那条。
	scope := s.projectScopeFor(r, "")
	list := make([]resolved, 0, len(req.Bindings))
	seen := map[string]bool{}
	for _, b := range req.Bindings {
		planID := strings.TrimSpace(b.PlanID)
		userID := strings.TrimSpace(b.UserID)
		if planID == "" {
			writeError(w, http.StatusBadRequest, "missing_plan_id", "绑定清单里有空的计划 ID")
			return
		}
		// 【同一条计划不许出现两次】两条相反的指令谁生效取决于顺序,
		// 而调用方不会知道自己发了矛盾的东西。
		if seen[planID] {
			writeError(w, http.StatusBadRequest, "duplicate_plan",
				"计划 "+planID+" 在清单里出现了多次")
			return
		}
		seen[planID] = true

		plan, err := s.store.GetEngineeringPlan(planID)
		if err != nil || plan == nil {
			writeError(w, http.StatusBadRequest, "plan_not_found", "计划不存在:"+planID)
			return
		}
		// 【和"不存在"给同一种回答】分开报的话,这个接口就成了一个探针:
		// 拿 ID 试一遍就能问出"哪些计划存在但我看不到"。
		if !scope.allows(plan.Project) {
			writeError(w, http.StatusBadRequest, "plan_not_found", "计划不存在:"+planID)
			return
		}
		var user *User
		if userID != "" {
			u, uErr := s.store.GetUser(userID)
			if uErr != nil || u == nil {
				writeError(w, http.StatusBadRequest, "user_not_found", "账号不存在:"+userID)
				return
			}
			if u.Status == userStatusDisabled {
				writeError(w, http.StatusBadRequest, "user_disabled",
					"账号已停用,不能作为负责人:"+firstNonEmpty(u.DisplayName, u.Username))
				return
			}
			// 和新建计划同一条规则:看不到这个项目就不许绑。
			// 批量回填是最容易把人绑错的地方 —— 一次几十条,没人会逐条核对。
			if !s.userCanSeeProject(s.tenantForRequest(r), u, plan.Project) {
				writeError(w, http.StatusBadRequest, "owner_cannot_see_project",
					firstNonEmpty(u.DisplayName, u.Username)+
						" 的数据范围里没有「"+plan.Project+"」,绑了也看不到(计划 "+planID+")")
				return
			}
			user = u
		}
		list = append(list, resolved{plan: plan, user: user})
	}

	// 【日志要记清楚绑给了谁,不能只记条数】这条操作改的是"提醒发给谁",
	// 事后要能回答"这条计划为什么归他"。只记 count 的话,查起来只知道
	// "某天有人绑了 8 条",8 条是哪些、绑给谁,全靠猜。
	trail := make([]map[string]any, 0, len(list))
	applied := 0
	for _, item := range list {
		p := item.plan
		entry := map[string]any{"planId": p.ID, "was": p.OwnerID}
		if item.user != nil {
			entry["userId"] = item.user.ID
			entry["userName"] = firstNonEmpty(item.user.DisplayName, item.user.Username)
		} else {
			entry["userId"] = "" // 解绑
		}
		trail = append(trail, entry)
		if item.user != nil {
			p.OwnerID = item.user.ID
			// 【名字跟着账号走】两列并存的代价是它们可能说不一样的话。
			// 绑定的那一刻对齐,之后界面上显示的名字就永远是这个账号的名字。
			p.OwnerName = firstNonEmpty(item.user.DisplayName, item.user.Username)
		} else {
			p.OwnerID = "" // 解绑:名字保留,只是不再指向账号
		}
		if err := s.store.UpsertEngineeringPlan(p); err != nil {
			// 校验全过了还失败,说明是库层面的问题。已经改掉的不回滚 ——
			// 把改了几条如实报出去,比假装什么都没发生有用。
			writeError(w, http.StatusInternalServerError, "apply_failed",
				"已应用 "+strconv.Itoa(applied)+" 条后失败:"+err.Error())
			return
		}
		applied++
	}

	s.recordOperation(r, "plan_owner_binding_apply", "engineering_plan", "", map[string]any{
		"count":    applied,
		"bindings": trail,
	})
	writeJSON(w, http.StatusOK, map[string]any{"applied": applied})
}
