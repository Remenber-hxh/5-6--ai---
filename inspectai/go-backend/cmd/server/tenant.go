package main

// ===== 多租户地基 =====
//
// 隔离策略:共享 schema + tenant_id 行级(见 docs/tenant-and-auth-design.md)。
// 本文件是租户域的落脚点,随 Phase 0 推进逐步补充 Tenant 类型与 Store 方法。

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"
)

const (
	// 默认租户 = 璟邑科技。多租户改造前的全部存量数据(资产/记录/用户…)
	// 由 migration 009 回填到这个租户,现有单租户行为保持不变。
	defaultTenantID   = "tenant_default"
	defaultTenantName = "璟邑科技"
	defaultTenantCode = "jadeast"
)

// Tenant — 一个客户公司。数据以 tenant_id 行级隔离。
type Tenant struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	Status    string    `json:"status"` // active / suspended
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ===== 租户 Store 实现 =====

func (s *SQLiteStore) CreateTenant(t *Tenant) error {
	if t.ID == "" {
		t.ID = newID("tenant")
	}
	if t.Status == "" {
		t.Status = "active"
	}
	now := nowStamp()
	_, err := s.db.Exec(
		`INSERT INTO tenants (id, name, code, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`,
		t.ID, t.Name, t.Code, t.Status, now, now)
	if err != nil && (strings.Contains(err.Error(), "Duplicate") || strings.Contains(err.Error(), "UNIQUE")) {
		return errors.New("tenant code already exists")
	}
	t.CreatedAt, _ = time.Parse(time.RFC3339Nano, now)
	t.UpdatedAt = t.CreatedAt
	return err
}

func (s *SQLiteStore) ListTenants() ([]*Tenant, error) {
	rows, err := s.db.Query(
		`SELECT id, name, code, status, created_at, updated_at FROM tenants ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Tenant
	for rows.Next() {
		t := &Tenant{}
		var created, updated string
		if err := rows.Scan(&t.ID, &t.Name, &t.Code, &t.Status, &created, &updated); err != nil {
			return nil, err
		}
		t.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		t.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *MemStore) CreateTenant(t *Tenant) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t.ID == "" {
		t.ID = newID("tenant")
	}
	if t.Status == "" {
		t.Status = "active"
	}
	for _, cur := range s.tenants {
		if cur.Code == t.Code {
			return errors.New("tenant code already exists")
		}
	}
	now := time.Now()
	t.CreatedAt, t.UpdatedAt = now, now
	s.tenants[t.ID] = t
	return nil
}

func (s *MemStore) ListTenants() ([]*Tenant, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Tenant, 0, len(s.tenants))
	for _, t := range s.tenants {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// ===== 两级管理员 =====
//
// 平台超管:唯一能跨租户(建/停客户、跨租户运维)。
// 租户管理员:admin 角色照旧,但作用域被 tenant_id 锁死,碰不到租户管理接口。

// isPlatformAdmin 当前请求是否来自平台超管。
// 只认账号上的标志位 —— 前端不能自报,与角色/权限矩阵正交。
func (s *Server) isPlatformAdmin(r *http.Request) bool {
	user, ok := s.userFromSessionToken(s.tokenFromRequest(r))
	return ok && user.IsPlatformAdmin
}

func (s *Server) requirePlatformAdmin(w http.ResponseWriter, r *http.Request) bool {
	if s.isPlatformAdmin(r) {
		return true
	}
	writeError(w, http.StatusForbidden, "forbidden", "仅平台超级管理员可操作")
	return false
}

// ===== 租户管理接口(全部仅平台超管) =====

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	tenants, err := s.store.ListTenants()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tenants": tenants})
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.TrimSpace(req.Code)
	if req.Name == "" || req.Code == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "客户名称与短码均不能为空")
		return
	}
	t := &Tenant{ID: newID("tenant"), Name: req.Name, Code: req.Code, Status: "active"}
	if err := s.store.CreateTenant(t); err != nil {
		if strings.Contains(err.Error(), "exists") {
			writeError(w, http.StatusConflict, "tenant_exists", "该短码已被占用")
			return
		}
		writeError(w, http.StatusInternalServerError, "create_failed", err.Error())
		return
	}
	_ = s.store.CreateOperationLog(&OperationLog{
		UserID:     "",
		ActorName:  s.currentUserName(r),
		Action:     "tenant.create",
		TargetType: "tenant",
		TargetID:   t.ID,
		Detail:     map[string]any{"name": t.Name, "code": t.Code},
	})
	writeJSON(w, http.StatusOK, map[string]any{"tenant": t})
}
