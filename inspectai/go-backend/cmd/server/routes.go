package main

import "net/http"

// ===== 路由权限表(权限看板) =====
//
// 精确匹配的 API 与其准入角色的唯一清单。审计"哪个接口谁能调"看这一张表即可。
//   - guard 只声明 handler 体内已存在的同款检查(双层都在,纵深防御);
//     表层先拦,函数体内的检查作为兜底保留。
//   - 前缀/动态路由(/api/assets/{id} 等)按方法在各自 handleXxxRoutes 内部分权,
//     迁入 internal/ 分包时再表化。
//   - 新增接口的规矩:先在这里登记(方法/路径/角色),再写 handler。

type guard int

const (
	guardNone          guard = iota // 已登录即可(authorized 层已挡未登录)
	guardSupervisor                 // 管理角色:admin / manager / supervisor
	guardAdmin                      // 仅系统管理员(= 租户管理员,作用域锁本租户)
	guardPlatformAdmin              // 仅平台超管:唯一能跨租户(建/停客户)
)

type apiRoute struct {
	method string
	path   string
	guard  guard
	perm   string // 所属能力键(见 permissions.go 目录);空 = 不按能力配置
	handle func(s *Server, w http.ResponseWriter, r *http.Request)
}

var apiRoutes = []apiRoute{
	// —— 开放/会话 ——
	{http.MethodGet, "/health", guardNone, "", (*Server).handleHealth},
	{http.MethodPost, "/api/auth/login", guardNone, "", (*Server).handleLogin},
	{http.MethodGet, "/api/auth/me", guardNone, "", (*Server).handleMe},
	{http.MethodPost, "/api/auth/logout", guardNone, "", (*Server).handleLogout},
	// 注册是【免鉴权】的,还要在 authorized() 里放行(和 login 一样),
	// 光在这里写 guardNone 不够 —— guard 是登录之后的分级,不是登录本身。
	{http.MethodPost, "/api/auth/register", guardNone, "", (*Server).handleRegister},
	// 自助改密码:任何已登录用户改【自己的】。改别人的仍是 /api/users/<id>/password(仅管理员)。
	{http.MethodPost, "/api/auth/me/password", guardNone, "", (*Server).handleChangeMyPassword},
	// 自助改头像:不带 userID,只认会话里的当前用户 —— 没有越权改他人的面。
	// (PUT /api/users/<id> 整体是管理员门控,巡检员改不了自己的)
	{http.MethodPost, "/api/auth/me/avatar", guardNone, "", (*Server).handleUpdateMyAvatar},

	// —— 用户与权限(user_manage 锁定 admin;users/roles/departments 读口保持
	//     supervisor 固定:派发任务的责任人下拉依赖它,不入矩阵) ——
	{http.MethodGet, "/api/users", guardSupervisor, "", (*Server).handleListUsers},
	{http.MethodPost, "/api/users", guardAdmin, "", (*Server).handleCreateUser},

	// —— 项目(读口给管理角色:选项目、看台账都要;写口仅管理员:
	//     项目归属直接决定谁能看到哪些数据,是权限动作) ——
	{http.MethodGet, "/api/projects", guardSupervisor, "", (*Server).handleListProjects},
	{http.MethodPost, "/api/projects", guardAdmin, "", (*Server).handleCreateProject},

	// —— 注册码(仅管理员:一张能自助注册的码流出去就是一道敞开的门)
	{http.MethodGet, "/api/registration-codes", guardAdmin, "", (*Server).handleListRegistrationCodes},
	{http.MethodPost, "/api/registration-codes", guardAdmin, "", (*Server).handleCreateRegistrationCode},
	{http.MethodGet, "/api/roles", guardSupervisor, "", (*Server).handleListRoles},
	{http.MethodPost, "/api/roles", guardAdmin, "", (*Server).handleCreateRole},
	{http.MethodGet, "/api/departments", guardSupervisor, "", (*Server).handleListDepartments},
	{http.MethodGet, "/api/operation-logs", guardNone, "audit_view", (*Server).handleListOperationLogs},
	{http.MethodGet, "/api/permissions", guardAdmin, "", (*Server).handleGetPermissions},
	{http.MethodPut, "/api/permissions", guardAdmin, "", (*Server).handleSavePermissions},

	// —— 客户租户管理(仅平台超管;租户管理员 admin 也进不来) ——
	{http.MethodGet, "/api/tenants", guardPlatformAdmin, "", (*Server).handleListTenants},
	{http.MethodPost, "/api/tenants", guardPlatformAdmin, "", (*Server).handleCreateTenant},

	// —— 企业微信 ——
	{http.MethodPost, "/api/wework/message", guardNone, "wework_send", (*Server).handleSendWeWorkMessage},
	{http.MethodPost, "/api/wework/group-message", guardNone, "wework_send", (*Server).handleSendWeWorkGroupMessage},

	// —— 工程巡检计划/任务(任务状态更新在前缀路由,巡检员完成任务要用,保持开放) ——
	{http.MethodGet, "/api/engineering/plans", guardNone, "", (*Server).handleListEngineeringPlans},
	{http.MethodPost, "/api/engineering/plans", guardNone, "task_dispatch", (*Server).handleCreateEngineeringPlan},
	{http.MethodGet, "/api/engineering/tasks", guardNone, "", (*Server).handleListEngineeringTasks},
	{http.MethodPost, "/api/engineering/tasks", guardNone, "task_dispatch", (*Server).handleCreateEngineeringTask},

	// —— 巡检与识别 ——
	{http.MethodGet, "/api/inspection/points", guardNone, "", (*Server).handleListPoints},
	{http.MethodGet, "/api/report/templates", guardNone, "", (*Server).handleListTemplates},
	// 离线照片:弱网现场先存手机、联网后上传(带幂等键,重传不产生重复)
	{http.MethodPost, "/api/inspection/offline-shots", guardNone, "", (*Server).handleUploadOfflineShot},
	{http.MethodGet, "/api/inspection/offline-shots", guardNone, "", (*Server).handleListOfflineShots},
	// 底栏角标只要两个数字,不必拉两个完整列表(实测省 120KB/次)
	{http.MethodGet, "/api/inspection/badge-counts", guardNone, "", (*Server).handleBadgeCounts},
	// 用已上传的离线照片做识别(照片已在服务器上,不重传)
	{http.MethodPost, "/api/inspection/offline-shots/classify", guardNone, "", (*Server).handleClassifyOfflineShots},
	// 批量删除未成单的离线照片(已并入记录的不在此删)
	{http.MethodPost, "/api/inspection/offline-shots/delete", guardNone, "", (*Server).handleDeleteOfflineShots},
	{http.MethodGet, "/api/inspection/records", guardNone, "", (*Server).handleListRecords},
	{http.MethodPost, "/api/inspection/records", guardNone, "", (*Server).handleCreateRecord},
	{http.MethodPost, "/api/scene/classify", guardNone, "", (*Server).handleClassifyScene},
	{http.MethodPost, "/api/ai/chat", guardNone, "", (*Server).handleAIChat},

	// —— 资产台账 ——
	{http.MethodGet, "/api/assets/summary", guardNone, "", (*Server).handleAssetSummary},
	{http.MethodGet, "/api/assets", guardNone, "", (*Server).handleListAssets},
	{http.MethodPost, "/api/assets", guardNone, "asset_manage", (*Server).handleCreateAsset},

	// —— 修改申请审批(创建对巡检员开放;通过/驳回在前缀路由内按 approval_review 分权) ——
	{http.MethodPost, "/api/change-requests", guardNone, "", (*Server).handleCreateChangeRequest},
	{http.MethodGet, "/api/change-requests", guardNone, "", (*Server).handleListChangeRequests},
	{http.MethodPost, "/api/change-requests/draft-photos", guardNone, "", (*Server).handleUploadDraftPhotos},

	// —— 管理 AI ——
	// 【guard 从 guardNone 改成 guardSupervisor】这四个 handler 体内本来就有
	// requireSupervisorAccess,但表里写的是 guardNone —— 而本文件开头写着
	// "审计哪个接口谁能调看这一张表即可",照它审计会得出"巡检员能调管理 AI"
	// 的错误结论。表要么准确,要么不如没有。
	{http.MethodGet, "/api/management-ai/snapshot", guardSupervisor, "", (*Server).handleManagementSnapshot},
	{http.MethodGet, "/api/management-ai/attention", guardSupervisor, "", (*Server).handleManagementAttention},
	{http.MethodPost, "/api/management-ai/chat", guardSupervisor, "", (*Server).handleManagementChat},
	{http.MethodGet, "/api/management-ai/report", guardSupervisor, "", (*Server).handleManagementReport},
	{http.MethodGet, "/api/management-ai/today", guardSupervisor, "", (*Server).handleHomeCounts},
	{http.MethodPost, "/api/management-ai/act", guardNone, "task_dispatch", (*Server).handleManagementAct},

	// —— 提示词模板(读写均在 prompt_manage 能力下,单条读写在前缀路由内检查) ——
	{http.MethodGet, "/api/prompt/templates", guardNone, "", (*Server).handleListPromptTemplates},
}

// allow — 表层准入:先角色档位,再能力矩阵。不通过时已写好 403 响应。
func (s *Server) allow(w http.ResponseWriter, r *http.Request, rt apiRoute) bool {
	switch rt.guard {
	case guardSupervisor:
		if !s.hasSupervisorAccess(r) {
			writeError(w, http.StatusForbidden, "forbidden", "需要管理权限")
			return false
		}
	case guardAdmin:
		if !s.hasAdminAccess(r) {
			writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可操作")
			return false
		}
	case guardPlatformAdmin:
		if !s.requirePlatformAdmin(w, r) {
			return false
		}
	}
	if rt.perm != "" && !s.requirePermission(w, r, rt.perm) {
		return false
	}
	return true
}
