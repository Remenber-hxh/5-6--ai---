package main

import (
	"net/http"
	"strings"
)

// ===== 数据范围:一个人能看到多少数据 =====
//
// 这和角色(能做什么动作)是【两件正交的事】。企业软件里的通行做法就是分开:
// 角色回答"他能不能点这个按钮",数据范围回答"他点开之后能看到几条"。
//
// 在这之前,"能看多少"是写死在代码里的两档 —— 七处 hasSupervisorAccess(r)
// 各自判断"管理角色看全部、其余看自己的"。要接入多个项目组,中间那一层
// (看本项目组的)没有地方挂,而且七处散着改极容易漏掉一处 —— 漏掉的后果
// 不是"看不到",是【越权看到别人的数据】,而且不报错。
//
// 【空 = 按角色推导,不改行为】存量用户这一列全是空,升级上线后一切照旧。
// 只有显式配过的人才走新逻辑。

const (
	// dataScopeAll 本租户全部数据。总经理、系统管理员。
	dataScopeAll = "all"
	// dataScopeProject 他所属项目的全部数据(含组内其他人提交的)。项目经理。
	dataScopeProject = "project"
	// dataScopeProjectSelf 能看本项目的设备台账,但巡检记录只看自己提交的。
	dataScopeProjectSelf = "project_self"
	// dataScopeSelf 只看自己提交的。外包、临时人员。
	dataScopeSelf = "self"
)

// validDataScopes 可以配置的取值。写成集合是为了让"改配置"这个入口
// 只能设出系统认识的值 —— 拼错一个字母就变成"未知范围",而未知范围
// 按哪一档处理都是错的。
var validDataScopes = map[string]bool{
	dataScopeAll:         true,
	dataScopeProject:     true,
	dataScopeProjectSelf: true,
	dataScopeSelf:        true,
}

// effectiveDataScope 算出这次请求实际生效的数据范围。
//
// 【空值必须回落到角色推导】而不是回落到某个固定档:
//   - 回落 self  → 所有存量管理角色瞬间只能看自己的,系统看起来"数据全没了"
//   - 回落 all   → 所有存量巡检员瞬间能看全租户,是越权
//
// 两个都是灾难。按角色推导才等于"行为不变"。
func (s *Server) effectiveDataScope(r *http.Request) string {
	scope := ""
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		scope = strings.TrimSpace(user.DataScope)
	}
	if scope == "" || !validDataScopes[scope] {
		// 没配过(或配了个系统不认识的值)→ 按角色推导 = 现有行为
		if s.hasSupervisorAccess(r) {
			return dataScopeAll
		}
		return dataScopeSelf
	}
	return scope
}

// dataVisibility 这次请求实际能看到什么。
//
// 把"范围"翻译成查询能用的三个开关。分开表达是因为不同数据的可过滤性不同:
// 设备和记录带项目名,审批和角标不带 —— 见 visibilityFor 的说明。
type dataVisibility struct {
	// AllData 本租户全部,不加任何限制。
	AllData bool
	// OwnOnly 只看自己提交的。
	OwnOnly bool
	// Projects 限定到这些项目(按项目名)。空 = 不按项目限。
	Projects []string
	// Blocked 配了项目范围却一个项目都没分到 —— 【什么都看不到】。
	//
	// 这里必须 fail closed。放行的话,"限定到项目"就成了一句空话:
	// 管理员以为限住了,实际这个人看得到全部。宁可他打开是空页面来问,
	// 也不能让配置默默失效。后台在选「本项目」时会要求至少选一个项目。
	Blocked bool
}

// allowsProject 这个项目的数据能不能给他看。
func (v dataVisibility) allowsProject(name string) bool {
	if v.Blocked {
		return false
	}
	if v.AllData || len(v.Projects) == 0 {
		return true
	}
	name = strings.TrimSpace(name)
	for _, p := range v.Projects {
		if p == name {
			return true
		}
	}
	return false
}

// visibilityFor 解析这次请求的可见范围。
//
// 【项目范围的语义,以及为什么不是所有页面都一样】
// 设备台账和巡检记录带项目名,按项目精确过滤。
// 修改申请、工程待办、角标计数、离线照片【过滤不到项目这一层】——
// 修改申请表里没有项目字段;工程待办按责任人姓名归属。这些一律退回"只看自己的"。
//
// 退回更严的一档而不是放行,是因为这两种错的代价不对称:
// 严了他会立刻发现并来问;松了没人会发现,而数据已经泄给了别的项目组。
func (s *Server) visibilityFor(r *http.Request) dataVisibility {
	scope := s.effectiveDataScope(r)
	switch scope {
	case dataScopeAll:
		return dataVisibility{AllData: true}
	case dataScopeProject, dataScopeProjectSelf:
		user, ok := s.userFromSessionToken(s.tokenFromRequest(r))
		if !ok {
			// 配了项目范围却拿不到用户 = 判断不了他属于哪个项目 → 不给看
			return dataVisibility{Blocked: true, OwnOnly: true}
		}
		names, err := s.store.ListUserProjectNames(s.tenantForRequest(r), user.ID)
		if err != nil || len(names) == 0 {
			return dataVisibility{Blocked: true, OwnOnly: true}
		}
		return dataVisibility{Projects: names, OwnOnly: scope == dataScopeProjectSelf}
	default:
		return dataVisibility{OwnOnly: true}
	}
}

// canSeeAllData 这次请求能不能看本租户的全部数据。
//
// 替换掉原来七处散落的 hasSupervisorAccess(r) —— 那七处判断的是
// "能看多少"而不是"能不能做",借用动作权限来表达数据范围,
// 是这套系统一直没法配项目组的直接原因。
func (s *Server) canSeeAllData(r *http.Request) bool {
	return s.effectiveDataScope(r) == dataScopeAll
}

// assetIDFromRoutePath 从 /api/assets/<id>[/子资源] 里取出资产 id。
//
// 【新增子资源时必须加进来】漏加会让那条路径绕过项目检查。
// 子资源名单和 handleAssetRoutes 里的分支一一对应。
func assetIDFromRoutePath(rest string) string {
	for _, suffix := range []string{"/records", "/report", "/cover", "/change-requests", "/status-events"} {
		if id := strings.TrimSuffix(rest, suffix); id != rest {
			return id
		}
	}
	if strings.Contains(rest, "/") {
		return ""
	}
	return rest
}

// assetVisibleToRequest 这台设备在不在他的项目范围内。
func (s *Server) assetVisibleToRequest(r *http.Request, assetID string) bool {
	vis := s.visibilityFor(r)
	if vis.Blocked {
		return false
	}
	if vis.AllData || len(vis.Projects) == 0 {
		// 不按项目限。注意"只看自己的"不限制设备台账 —— 巡检员本来就要
		// 看得到全部设备才能去巡,这是既有行为,这一步不动它。
		return true
	}
	asset, err := s.store.GetAsset(s.tenantForRequest(r), assetID)
	if err != nil || asset == nil {
		// 查不到:交给下游 handler 去报 404,别在这里把"不存在"说成"没权限"
		return true
	}
	return vis.allowsProject(asset.Project)
}

// limitAssetsToVisibleProjects 台账列表按项目范围裁剪。
func (s *Server) limitAssetsToVisibleProjects(r *http.Request, assets []*AssetEntry) []*AssetEntry {
	vis := s.visibilityFor(r)
	if vis.AllData || (len(vis.Projects) == 0 && !vis.Blocked) {
		return assets
	}
	out := make([]*AssetEntry, 0, len(assets))
	for _, a := range assets {
		if a != nil && vis.allowsProject(a.Project) {
			out = append(out, a)
		}
	}
	return out
}
