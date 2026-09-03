package main

import (
	"net/http"
	"strings"
	"sync"
)

// ===== 角色×能力 权限体系 =====
//
// 路由不直接暴露给用户配置(35 条太细,勾错即坏);对外可配的是 7 个「能力」,
// 每条路由在 routes.go 声明所属能力。矩阵存 role_permissions 表,后台可视化编辑。
// 两条保险丝:admin 永远全通过;「用户与权限管理」锁定 admin 不可配置(防自锁)。

type permDef struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Desc   string `json:"desc"`
	Locked bool   `json:"locked"` // 锁定项:矩阵中展示但不可修改
}

var permCatalog = []permDef{
	{"user_manage", "用户与权限管理", "账号增改/密码重置/停启用/权限矩阵配置", true},
	{"asset_manage", "资产台账管理", "新增/编辑/删除资产,更换标准图", false},
	{"approval_review", "审批处理", "修改申请通过/驳回", false},
	{"task_dispatch", "任务派发", "AI 派单确认,工程执行任务下发", false},
	{"wework_send", "企业微信通知", "手动触发企微消息/群机器人", false},
	// 【这两项锁定给管理员】
	// 操作日志是审计证据,里面有谁改了什么、谁批了什么 —— 能看审计的人
	// 和被审计的人不该是同一批,否则审计就没有意义了。
	// 提示词模板是 AI 的行为本身,改一句话就能让所有识别结果变样,
	// 而且没有任何报错 —— 这不是"业务操作",是改产品。
	{"audit_view", "操作日志查看", "全量操作审计日志", true},
	{"prompt_manage", "提示词模板管理", "识别模板查看/编辑/渲染预览", true},
	// 巡检模板同样锁定给管理员:改一个字段就改变了所有巡检员填什么、
	// AI 提取什么、记录怎么存 —— 和提示词一样,这是改产品,不是改业务。
	{"template_manage", "巡检模板管理", "模板与字段的新建/编辑/删除", true},
}

// defaultPermMatrix — 默认矩阵 = 引入本体系前的固化行为:
// 三档管理角色(admin/manager/supervisor)全开,巡检员不开;用户管理仅 admin。
func defaultPermMatrix() map[string][]string {
	m := map[string][]string{}
	for _, p := range permCatalog {
		if p.Locked {
			m[p.Key] = []string{"admin"}
			continue
		}
		m[p.Key] = []string{"manager", "supervisor"} // admin 隐式全通过,不必入表
	}
	return m
}

func validPermKey(key string) (permDef, bool) {
	for _, p := range permCatalog {
		if p.Key == key {
			return p, true
		}
	}
	return permDef{}, false
}

// ===== Server 侧缓存与检查 =====

type permCache struct {
	mu    sync.RWMutex
	allow map[string]map[string]bool // permKey -> roleCode -> true
}

func (c *permCache) set(matrix map[string][]string) {
	next := map[string]map[string]bool{}
	for k, roles := range matrix {
		next[k] = map[string]bool{}
		for _, role := range roles {
			next[k][role] = true
		}
	}
	c.mu.Lock()
	c.allow = next
	c.mu.Unlock()
}

func (c *permCache) allowed(permKey, role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.allow[permKey][role]
}

// anyAllowed — 该角色是否被授予过至少一项能力(自定义角色的"管理角色"判定)。
func (c *permCache) anyAllowed(role string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	for _, roles := range c.allow {
		if roles[role] {
			return true
		}
	}
	return false
}

// loadPermissions 启动/保存后刷新缓存;库里缺的能力项落默认值(升级兼容)。
func (s *Server) loadPermissions() error {
	matrix, err := s.store.ListRolePermissions()
	if err != nil {
		return err
	}
	defaults := defaultPermMatrix()
	for k, v := range defaults {
		if _, ok := matrix[k]; !ok {
			matrix[k] = v
		}
	}
	// 【锁定项一律以代码为准,覆盖库里的值】
	//
	// 少了这一段,"加锁"只对新库生效:老库里早就存着
	// 「audit_view → 经理、主管」这几行,读取时原样用,加锁等于没加 ——
	// 代码看着对、线上照旧,而且不报错。
	//
	// 顺带也堵住了另一条路:有人直接改库插一行,同样不生效。
	// 锁定的意思就是"只有代码说了算"。
	for _, p := range permCatalog {
		if p.Locked {
			matrix[p.Key] = defaults[p.Key]
		}
	}
	s.permCache.set(matrix)
	return nil
}

// hasPermission — 能力检查:admin 永远通过;其余角色查矩阵。
func (s *Server) hasPermission(r *http.Request, permKey string) bool {
	role := s.userRole(r)
	if role == roleAdmin {
		return true
	}
	return s.permCache.allowed(permKey, role)
}

// requirePermission — 不通过时已写好 403。
func (s *Server) requirePermission(w http.ResponseWriter, r *http.Request, permKey string) bool {
	if s.hasPermission(r, permKey) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden", "当前角色未被授予该操作权限")
	return false
}

// permsForRole — 登录响应携带,前端据此控制菜单/按钮可见性。
func (s *Server) permsForRole(role string) []string {
	out := []string{}
	for _, p := range permCatalog {
		if role == roleAdmin || s.permCache.allowed(p.Key, role) {
			out = append(out, p.Key)
		}
	}
	return out
}

// ===== 管理接口(仅 admin,见 routes.go) =====

func (s *Server) handleGetPermissions(w http.ResponseWriter, r *http.Request) {
	matrix, err := s.store.ListRolePermissions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	defaults := defaultPermMatrix()
	for k, v := range defaults {
		if _, ok := matrix[k]; !ok {
			matrix[k] = v
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"catalog": permCatalog, "matrix": matrix})
}

func (s *Server) handleSavePermissions(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Matrix map[string][]string `json:"matrix"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	validRoles := map[string]bool{}
	if roles, err := s.store.ListRoles(); err == nil {
		for _, role := range roles {
			if role.Code != roleAdmin {
				validRoles[role.Code] = true
			}
		}
	}
	clean := defaultPermMatrix() // 锁定项永远保持默认(admin)
	for key, roles := range req.Matrix {
		def, ok := validPermKey(key)
		if !ok || def.Locked {
			continue // 未知键/锁定键忽略
		}
		list := []string{}
		for _, role := range roles {
			if validRoles[role] {
				list = append(list, role)
			}
		}
		clean[key] = list
	}
	if err := s.store.ReplaceRolePermissions(clean); err != nil {
		writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
		return
	}
	s.permCache.set(clean)
	_ = s.store.CreateOperationLog(&OperationLog{
		ActorName:  s.currentUserName(r),
		Action:     "update_permissions",
		TargetType: "system",
		Detail:     map[string]any{"matrix": clean},
	})
	writeJSON(w, http.StatusOK, map[string]any{"matrix": clean})
}

// ===== 自定义角色管理(仅 admin,见 routes.go) =====

// builtinRoleCode — 内置四角色:可改名不可删;admin 全锁。
func builtinRoleCode(code string) bool {
	switch code {
	case roleAdmin, roleManager, roleSupervisor, roleInspector:
		return true
	}
	return false
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "角色名称不能为空")
		return
	}
	id := newID("role")
	role := &Role{ID: id, Code: id, Name: req.Name, Description: strings.TrimSpace(req.Description)}
	if err := s.store.CreateRole(role); err != nil {
		if strings.Contains(err.Error(), "already exists") {
			writeError(w, http.StatusConflict, "role_exists", "已存在同名角色")
			return
		}
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	s.recordOperation(r, "role.create", "role", role.ID, map[string]any{"name": role.Name})
	writeJSON(w, http.StatusCreated, map[string]any{"role": role})
}

// handleRoleRoutes — PUT/DELETE /api/roles/{id}
func (s *Server) handleRoleRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理角色")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/roles/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "")
		return
	}
	roles, err := s.store.ListRoles()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	var target *Role
	for _, item := range roles {
		if item.ID == id {
			target = item
			break
		}
	}
	if target == nil {
		writeError(w, http.StatusNotFound, "role_not_found", "角色不存在")
		return
	}
	switch r.Method {
	case http.MethodPut:
		if target.Code == roleAdmin {
			writeError(w, http.StatusForbidden, "role_locked", "系统管理员角色不可修改")
			return
		}
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if req.Name == "" {
			writeError(w, http.StatusBadRequest, "bad_request", "角色名称不能为空")
			return
		}
		role, err := s.store.UpdateRole(id, req.Name, strings.TrimSpace(req.Description))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		s.recordOperation(r, "role.update", "role", id, map[string]any{"name": req.Name})
		writeJSON(w, http.StatusOK, map[string]any{"role": role})
	case http.MethodDelete:
		if builtinRoleCode(target.Code) {
			writeError(w, http.StatusForbidden, "role_locked", "内置角色不可删除")
			return
		}
		if err := s.store.DeleteRole(id); err != nil {
			if strings.Contains(err.Error(), "in use") {
				writeError(w, http.StatusConflict, "role_in_use", "仍有用户使用该角色,请先调整用户角色")
				return
			}
			writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
			return
		}
		// 矩阵行已随事务清理,刷新缓存
		if err := s.loadPermissions(); err == nil {
			// no-op
		}
		s.recordOperation(r, "role.delete", "role", id, map[string]any{"name": target.Name})
		writeJSON(w, http.StatusOK, map[string]any{"deleted": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "")
	}
}
