package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strings"
)

// ===== 项目管理接口 =====
//
// 读口给管理角色(派任务、看台账都要选项目);写口只给系统管理员 ——
// 项目归属直接决定谁能看到哪些数据,是权限动作。

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListProjects(s.tenantForRequest(r))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	if list == nil {
		list = []*Project{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": list})
}

type projectUpsertRequest struct {
	Name     string `json:"name"`
	Note     string `json:"note"`
	Disabled bool   `json:"disabled"`
}

func (s *Server) handleCreateProject(w http.ResponseWriter, r *http.Request) {
	var req projectUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "项目名称不能为空")
		return
	}
	tenantID := s.tenantForRequest(r)
	// 【先查重名】项目名是业务表的关联键,建了同名的第二条,成员挂到哪一条
	// 都对不上台账。数据库唯一约束会拦,但那里报出来的是一句英文。
	existing, err := s.store.ListProjects(tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	for _, p := range existing {
		if p.Name == name {
			writeError(w, http.StatusConflict, "project_exists", "同名项目已存在")
			return
		}
	}
	p := &Project{TenantID: tenantID, Name: name, Note: strings.TrimSpace(req.Note)}
	if err := s.store.CreateProject(p); err != nil {
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	s.recordOperation(r, "project.create", "project", p.ID, map[string]any{"name": p.Name})
	writeJSON(w, http.StatusOK, map[string]any{"project": p})
}

// handleProjectRoutes 处理 /api/projects/<id>
func (s *Server) handleProjectRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.hasAdminAccess(r) {
		writeError(w, http.StatusForbidden, "forbidden", "仅系统管理员可管理项目")
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/projects/")
	if id == "" || strings.Contains(id, "/") {
		writeError(w, http.StatusNotFound, "not_found", "未匹配的项目路由")
		return
	}
	if r.Method != http.MethodPut {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 PUT")
		return
	}
	var req projectUpsertRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	// 刻意不接受改名:业务表按名字关联,改了名台账就认不出来了。
	err := s.store.UpdateProjectMeta(s.tenantForRequest(r), id, strings.TrimSpace(req.Note), req.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "project_not_found", "项目不存在")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}
	s.recordOperation(r, "project.update", "project", id, map[string]any{"disabled": req.Disabled})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ===== 某人的项目归属:GET / PUT /api/users/<id>/projects =====

func (s *Server) handleUserProjects(w http.ResponseWriter, r *http.Request, userID string) {
	tenantID := s.tenantForRequest(r)
	switch r.Method {
	case http.MethodGet:
		ids, err := s.store.ListUserProjectIDs(tenantID, userID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"projectIds": ids})
	case http.MethodPut:
		var req struct {
			ProjectIDs []string `json:"projectIds"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		// 【确认这个人在本租户】否则跨租户传个 userID 就能改别家的人员归属。
		target, err := s.store.GetUser(userID)
		if err != nil || target == nil || tenantOrDefault(target.TenantID) != tenantID {
			writeError(w, http.StatusNotFound, "user_not_found", "用户不存在")
			return
		}
		if err := s.store.SetUserProjects(tenantID, userID, req.ProjectIDs); err != nil {
			writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
			return
		}
		s.recordOperation(r, "user.projects", "user", userID, map[string]any{
			"count": len(req.ProjectIDs),
		})
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "仅支持 GET / PUT")
	}
}
