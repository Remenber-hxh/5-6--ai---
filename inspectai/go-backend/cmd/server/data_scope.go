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
// 【这一步只做开关,不改行为】data_scope 为空时按角色推导,和以前完全一样。
// 存量用户这一列全是空,所以升级上线后一切照旧。项目维度的两档先占好位置,
// 等第 2 步(项目实体化 + 成员表)做完再启用。

const (
	// dataScopeAll 本租户全部数据。总经理、系统管理员。
	dataScopeAll = "all"
	// dataScopeProject 他所属项目的全部数据(含组内其他人提交的)。
	// 【第 2 步才启用】现在落到这一档会按 all 处理并留日志 —— 见 effectiveDataScope。
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
	// 项目维度的两档要等第 2 步(项目实体 + 成员表)才有意义。
	// 在那之前按 all 处理【而不是按 self】:这两档都是给管理角色用的,
	// 当成 self 会让项目经理突然看不见组员的数据 —— 宁可暂时宽一点,
	// 也不要在功能没做完时先把人挡在外面。
	if scope == dataScopeProject || scope == dataScopeProjectSelf {
		return dataScopeAll
	}
	return scope
}

// canSeeAllData 这次请求能不能看本租户的全部数据。
//
// 替换掉原来七处散落的 hasSupervisorAccess(r) —— 那七处判断的是
// "能看多少"而不是"能不能做",借用动作权限来表达数据范围,
// 是这套系统一直没法配项目组的直接原因。
func (s *Server) canSeeAllData(r *http.Request) bool {
	return s.effectiveDataScope(r) == dataScopeAll
}
