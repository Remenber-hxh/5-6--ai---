package main

import (
	"net/http"
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
	{"audit_view", "操作日志查看", "全量操作审计日志", false},
	{"prompt_manage", "提示词模板管理", "识别模板查看/编辑/渲染预览", false},
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
	validRoles := map[string]bool{"manager": true, "supervisor": true, "inspector": true}
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
