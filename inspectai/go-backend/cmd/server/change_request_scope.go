package main

import (
	"net/http"
	"strings"
)

// ===== 修改申请的项目归属 =====
//
// change_requests 表里【没有项目字段】,只有 TargetType + TargetID。
// 所以项目要顺着目标反查:资产直接取它的 Project,记录取记录的 Project。
//
// 【为什么不从 TargetID 前缀切】资产 id 长得像 "会议中心::模板::编号",
// 看起来切一刀就有项目名。但那只是【约定】,不是保证 —— 哪天有一条资产
// 是别的方式建出来的,切出来就是个错项目名,而错项目名的后果是
// 静默地把申请分到别人那边,或者让本该看到的人看不到。查库慢一点,但不会错。
//
// 一台设备往往有多条申请,所以按目标做了缓存;列表本身有 500 条上限。

// changeRequestProjectResolver 带缓存的"这条申请属于哪个项目"。
type changeRequestProjectResolver struct {
	srv    *Server
	tenant string
	cache  map[string]string
}

func (s *Server) newChangeRequestProjectResolver(r *http.Request) *changeRequestProjectResolver {
	return &changeRequestProjectResolver{
		srv:    s,
		tenant: s.tenantForRequest(r),
		cache:  map[string]string{},
	}
}

// projectOf 返回该申请所属项目。查不到目标时返回空串 —— 见 allows 的处理。
func (c *changeRequestProjectResolver) projectOf(cr *ChangeRequest) string {
	if cr == nil {
		return ""
	}
	key := cr.TargetType + "|" + cr.TargetID
	if p, ok := c.cache[key]; ok {
		return p
	}
	project := ""
	switch cr.TargetType {
	case "asset":
		if a, err := c.srv.store.GetAsset(c.tenant, cr.TargetID); err == nil && a != nil {
			project = strings.TrimSpace(a.Project)
		}
	case "record":
		if rec, err := c.srv.store.GetRecord(c.tenant, cr.TargetID); err == nil && rec != nil {
			project = strings.TrimSpace(rec.Project)
		}
	}
	c.cache[key] = project
	return project
}

// allows 这条申请能不能给他看。
//
// 【目标已不存在的孤儿申请:给能看全部的人】清理历史数据时删掉了设备/记录,
// 引用它的申请还留着,这种查不出项目。藏起来的话没人处理得了它,
// 而它又是永远批不了的(applyChangeRequest 同样查不到目标)——
// 留给管理员看见并清掉,比让它彻底消失强。
func (c *changeRequestProjectResolver) allows(vis dataVisibility, cr *ChangeRequest) bool {
	if vis.AllData {
		return true
	}
	if vis.Blocked {
		return false
	}
	project := c.projectOf(cr)
	if project == "" {
		return false
	}
	return vis.allowsProject(project)
}

// filterChangeRequestsByProject 按项目范围裁剪申请列表。
// 不受项目限制时原样返回,不做任何查库。
func (s *Server) filterChangeRequestsByProject(r *http.Request, vis dataVisibility, list []*ChangeRequest) []*ChangeRequest {
	if vis.AllData || (len(vis.Projects) == 0 && !vis.Blocked) {
		return list
	}
	resolver := s.newChangeRequestProjectResolver(r)
	out := make([]*ChangeRequest, 0, len(list))
	for _, cr := range list {
		if cr != nil && resolver.allows(vis, cr) {
			out = append(out, cr)
		}
	}
	return out
}

// changeRequestInScope 单条申请的项目检查(详情 / 审批 / 驳回 / 撤回都走它)。
func (s *Server) changeRequestInScope(r *http.Request, cr *ChangeRequest) bool {
	vis := s.visibilityFor(r)
	if vis.AllData || (len(vis.Projects) == 0 && !vis.Blocked) {
		return true
	}
	return s.newChangeRequestProjectResolver(r).allows(vis, cr)
}
