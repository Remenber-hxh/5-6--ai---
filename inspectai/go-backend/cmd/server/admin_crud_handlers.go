package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// ===== 部门管理接口 =====
//
// 部门表一直只有一条种子「默认部门」,没有任何写入口 —— 后台的部门下拉
// 永远只有一项,等于这个功能不存在。

func (s *Server) handleCreateDepartment(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string `json:"name"`
		ParentID string `json:"parentId"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	d, err := s.store.CreateDepartment(req.Name, req.ParentID)
	if errors.Is(err, errDeptNameTaken) {
		writeError(w, http.StatusConflict, "name_taken", "已有同名部门")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "create_failed", err.Error())
		return
	}
	s.recordOperation(r, "department.create", "department", d.ID, map[string]any{"name": d.Name})
	writeJSON(w, http.StatusOK, map[string]any{"department": d})
}

// handleDepartmentRoutes 处理 /api/departments/<id>
func (s *Server) handleDepartmentRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理部门")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/departments/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "未匹配的部门路由")
		return
	}
	switch r.Method {
	case http.MethodPut:
		var req struct {
			Name string `json:"name"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		err := s.store.UpdateDepartment(id, req.Name)
		switch {
		case errors.Is(err, errDeptNameTaken):
			writeError(w, http.StatusConflict, "name_taken", "已有同名部门")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "not_found", "部门不存在")
		case err != nil:
			writeError(w, http.StatusBadRequest, "update_failed", err.Error())
		default:
			s.recordOperation(r, "department.update", "department", id, map[string]any{"name": req.Name})
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}
	case http.MethodDelete:
		err := s.store.DeleteDepartment(id)
		switch {
		case errors.Is(err, errInUse):
			// 【说清楚为什么不能删,以及下一步做什么】只说"删除失败"
			// 等于让管理员去猜,他会反复点。
			writeError(w, http.StatusConflict, "department_in_use",
				"该部门下还有用户(或它是默认部门),请先把用户调到其他部门")
		case errors.Is(err, sql.ErrNoRows):
			writeError(w, http.StatusNotFound, "not_found", "部门不存在")
		case err != nil:
			writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
		default:
			s.recordOperation(r, "department.delete", "department", id, nil)
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		}
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 PUT / DELETE")
	}
}

// ===== 删除用户 =====
//
// 【停用才是常规做法,删除是例外】所以拒绝的时候一定要把"改用停用"说出来。
func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request, userID string) {
	operatorID := ""
	if u, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		operatorID = u.ID
	}
	target, _ := s.store.GetUser(userID)
	if target == nil || tenantOrDefault(target.TenantID) != s.tenantForRequest(r) {
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
		return
	}
	err := s.store.DeleteUser(userID, operatorID)
	switch {
	case errors.Is(err, errDeleteSelf):
		writeError(w, http.StatusForbidden, "cannot_delete_self", "不能删除自己的账号")
	case errors.Is(err, errLastAdmin):
		writeError(w, http.StatusForbidden, "last_admin",
			"这是最后一个系统管理员,删掉就没人能进后台了")
	case errors.Is(err, errInUse):
		writeError(w, http.StatusConflict, "user_has_records",
			"该用户已提交过巡检记录,删除会让这些记录失去提交人。请改用「停用」——"+
				"账号立即无法登录,历史记录完整保留")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
	default:
		s.recordOperation(r, "user.delete", "user", userID, map[string]any{
			"username": target.Username, "role": target.RoleCode,
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ===== 删除项目 =====

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request, id string) {
	err := s.store.DeleteProject(s.tenantForRequest(r), id)
	switch {
	case errors.Is(err, errInUse):
		writeError(w, http.StatusConflict, "project_in_use",
			"该项目下还有设备台账,删除会让这些设备失去归属。现场已交付的话请改用「停用」")
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "project_not_found", "项目不存在")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
	default:
		s.recordOperation(r, "project.delete", "project", id, nil)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

// ===== 删除注册码 =====

func (s *Server) handleDeleteRegistrationCode(w http.ResponseWriter, r *http.Request, code string) {
	err := s.store.DeleteRegistrationCode(s.tenantForRequest(r), code)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		writeError(w, http.StatusNotFound, "code_not_found", "注册码不存在")
	case err != nil:
		writeError(w, http.StatusInternalServerError, "delete_failed", err.Error())
	default:
		// 码本身写进日志是安全的:走到这里它已经被删掉了,再也注册不出账号。
		// (而且操作日志现在只有系统管理员看得到。)
		s.recordOperation(r, "registration_code.delete", "registration_code", code, nil)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}
