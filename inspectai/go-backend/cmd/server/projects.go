package main

import (
	"database/sql"
	"errors"
	"sort"
	"strings"
)

// ===== 项目 =====
//
// 一个项目 = 一个现场(会议中心、紫菡雅集)。一个人可以同时属于多个项目。
//
// 【关联键仍然是项目名】assets / records / change_requests / engineering_tasks
// 之间本来就用中文项目名互相认。改成 project_id 要动四张表和几十处查询,
// 收益是"改名不断",代价是一次大范围数据迁移 —— 现阶段不值。
// 代价记在这里:**项目一旦有数据就不能改名**,所以接口只给登记和停用。

// Project 项目登记。
type Project struct {
	ID        string `json:"id"`
	TenantID  string `json:"tenantId,omitempty"`
	Name      string `json:"name"`
	Code      string `json:"code,omitempty"`
	Note      string `json:"note,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
	// AssetCount 台账里挂在这个项目下的设备数。列表页顺带给出 ——
	// 管理员看到"0 台"就知道这个项目名和台账对不上(多半是手工建重了)。
	AssetCount int `json:"assetCount"`
	// MemberCount 已分配到这个项目的人数。
	MemberCount int `json:"memberCount"`
}

// ProjectStore — 项目登记与成员关系
type ProjectStore interface {
	ListProjects(tenantID string) ([]*Project, error)
	CreateProject(p *Project) error
	// UpdateProjectMeta 只改备注和启用状态。【名字不能改】—— 业务表按名字关联。
	UpdateProjectMeta(tenantID, id, note string, disabled bool) error
	// ListUserProjectNames 这个人被分到了哪些项目(返回项目名,直接用于过滤)。
	// 返回空切片 = 没被分配过 = 不受项目限制。
	ListUserProjectNames(tenantID, userID string) ([]string, error)
	// AllUserProjectNames 批量版:一次取全租户的 用户 → 可见项目名。
	// 用户列表要显示每个人的范围,逐个查在人数上百之后会明显拖慢页面。
	AllUserProjectNames(tenantID string) (map[string][]string, error)
	// SetUserProjects 覆盖式设置某人的项目归属。传空切片 = 清空归属。
	SetUserProjects(tenantID, userID string, projectIDs []string) error
	// ListUserProjectIDs 后台回显用。
	ListUserProjectIDs(tenantID, userID string) ([]string, error)
	// DeleteProject 硬删除。台账里还有设备时报 errInUse ——
	// 项目名是业务表的关联键,删了那些设备就成了指向不存在项目的孤儿。
	DeleteProject(tenantID, id string) error
}

var errProjectNameRequired = errors.New("project name required")

// ===== SQLiteStore(SQLite + MySQL) =====

func (s *SQLiteStore) ListProjects(tenantID string) ([]*Project, error) {
	tenantID = tenantOrDefault(tenantID)
	rows, err := s.db.Query(`
		SELECT id, tenant_id, name, code, note, disabled, created_at, updated_at
		FROM projects WHERE tenant_id = ? ORDER BY disabled ASC, name ASC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Project
	byName := map[string]*Project{}
	for rows.Next() {
		p := &Project{}
		var disabled int
		if err := rows.Scan(&p.ID, &p.TenantID, &p.Name, &p.Code, &p.Note,
			&disabled, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, err
		}
		p.Disabled = disabled != 0
		out = append(out, p)
		byName[p.Name] = p
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return out, nil
	}
	// 设备数和人数各一条聚合查询。【不要按项目逐个查】—— 项目多了就是 N+1,
	// 而且这个页面每次打开都跑。
	assetRows, err := s.db.Query(
		`SELECT project, COUNT(1) FROM assets WHERE tenant_id = ? GROUP BY project`, tenantID)
	if err != nil {
		return nil, err
	}
	defer assetRows.Close()
	for assetRows.Next() {
		var name string
		var n int
		if err := assetRows.Scan(&name, &n); err != nil {
			return nil, err
		}
		if p, ok := byName[strings.TrimSpace(name)]; ok {
			p.AssetCount = n
		}
	}
	byID := map[string]*Project{}
	for _, p := range out {
		byID[p.ID] = p
	}
	memberRows, err := s.db.Query(
		`SELECT project_id, COUNT(1) FROM user_projects GROUP BY project_id`)
	if err != nil {
		return nil, err
	}
	defer memberRows.Close()
	for memberRows.Next() {
		var pid string
		var n int
		if err := memberRows.Scan(&pid, &n); err != nil {
			return nil, err
		}
		if p, ok := byID[pid]; ok {
			p.MemberCount = n
		}
	}
	return out, nil
}

func (s *SQLiteStore) CreateProject(p *Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errProjectNameRequired
	}
	p.TenantID = tenantOrDefault(p.TenantID)
	if p.ID == "" {
		p.ID = newID("proj")
	}
	if strings.TrimSpace(p.Code) == "" {
		p.Code = businessProjectCode(p.Name)
	}
	now := nowStamp()
	p.CreatedAt, p.UpdatedAt = now, now
	_, err := s.db.Exec(`
		INSERT INTO projects (id, tenant_id, name, code, note, disabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.TenantID, p.Name, p.Code, p.Note, boolToInt(p.Disabled), now, now)
	return err
}

func (s *SQLiteStore) UpdateProjectMeta(tenantID, id, note string, disabled bool) error {
	res, err := s.db.Exec(
		`UPDATE projects SET note=?, disabled=?, updated_at=? WHERE tenant_id=? AND id=?`,
		note, boolToInt(disabled), nowStamp(), tenantOrDefault(tenantID), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) ListUserProjectNames(tenantID, userID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT p.name FROM user_projects up
		JOIN projects p ON p.id = up.project_id
		WHERE up.user_id = ? AND p.tenant_id = ? AND p.disabled = 0
		ORDER BY p.name ASC`, userID, tenantOrDefault(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	names := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		names = append(names, n)
	}
	return names, rows.Err()
}

// AllUserProjectNames 一次取全租户的 用户 → 可见项目名 映射。
//
// 【为什么要有批量版】用户列表页要显示每个人能看到哪些项目。逐个查的话,
// 10 个人无感,300 个人就是打开一次页面问 300 次数据库 ——
// 这类慢不报错,只表现成"这个页面越来越卡",而且很难想到是这里。
//
// 和 ListUserProjectNames 用同一个 WHERE(启用中的项目、按名字排序),
// 只是去掉了 user_id 的限定 —— 两者口径必须一致,否则单个查和批量查
// 会给出不同的答案,而这种不一致没人会去比对。
func (s *SQLiteStore) AllUserProjectNames(tenantID string) (map[string][]string, error) {
	rows, err := s.db.Query(`
		SELECT up.user_id, p.name FROM user_projects up
		JOIN projects p ON p.id = up.project_id
		WHERE p.tenant_id = ? AND p.disabled = 0
		ORDER BY up.user_id ASC, p.name ASC`, tenantOrDefault(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]string{}
	for rows.Next() {
		var uid, name string
		if err := rows.Scan(&uid, &name); err != nil {
			return nil, err
		}
		out[uid] = append(out[uid], name)
	}
	return out, rows.Err()
}

func (s *MemStore) AllUserProjectNames(tenantID string) (map[string][]string, error) {
	tenantID = tenantOrDefault(tenantID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := map[string][]string{}
	for uid, pids := range s.userProjects {
		names := []string{}
		for _, pid := range pids {
			if p, ok := s.projects[pid]; ok && p.TenantID == tenantID && !p.Disabled {
				names = append(names, p.Name)
			}
		}
		if len(names) > 0 {
			sort.Strings(names)
			out[uid] = names
		}
	}
	return out, nil
}

func (s *SQLiteStore) ListUserProjectIDs(tenantID, userID string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT up.project_id FROM user_projects up
		JOIN projects p ON p.id = up.project_id
		WHERE up.user_id = ? AND p.tenant_id = ?`, userID, tenantOrDefault(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *SQLiteStore) SetUserProjects(tenantID, userID string, projectIDs []string) error {
	tenantID = tenantOrDefault(tenantID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM user_projects WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, pid := range projectIDs {
		if strings.TrimSpace(pid) == "" {
			continue
		}
		// 【校验项目属于本租户】否则跨租户传一个 id 进来就把人挂到别家项目上了。
		var n int
		if err := tx.QueryRow(
			`SELECT COUNT(1) FROM projects WHERE id=? AND tenant_id=?`, pid, tenantID).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			continue
		}
		if _, err := tx.Exec(
			`INSERT INTO user_projects (user_id, project_id, created_at) VALUES (?, ?, ?)`,
			userID, pid, nowStamp()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ===== MemStore =====

func (s *MemStore) ListProjects(tenantID string) ([]*Project, error) {
	tenantID = tenantOrDefault(tenantID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []*Project{}
	for _, p := range s.projects {
		if p.TenantID != tenantID {
			continue
		}
		cp := *p
		for _, a := range s.assets {
			if a.TenantID == tenantID && a.Project == p.Name {
				cp.AssetCount++
			}
		}
		for _, ids := range s.userProjects {
			for _, id := range ids {
				if id == p.ID {
					cp.MemberCount++
				}
			}
		}
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *MemStore) CreateProject(p *Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		return errProjectNameRequired
	}
	p.TenantID = tenantOrDefault(p.TenantID)
	if p.ID == "" {
		p.ID = newID("proj")
	}
	if strings.TrimSpace(p.Code) == "" {
		p.Code = businessProjectCode(p.Name)
	}
	now := nowStamp()
	p.CreatedAt, p.UpdatedAt = now, now
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, ex := range s.projects {
		if ex.TenantID == p.TenantID && ex.Name == p.Name {
			return errors.New("project already exists")
		}
	}
	cp := *p
	s.projects[p.ID] = &cp
	return nil
}

func (s *MemStore) UpdateProjectMeta(tenantID, id, note string, disabled bool) error {
	tenantID = tenantOrDefault(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok || p.TenantID != tenantID {
		return sql.ErrNoRows
	}
	p.Note, p.Disabled, p.UpdatedAt = note, disabled, nowStamp()
	return nil
}

func (s *MemStore) ListUserProjectNames(tenantID, userID string) ([]string, error) {
	tenantID = tenantOrDefault(tenantID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	names := []string{}
	for _, pid := range s.userProjects[userID] {
		if p, ok := s.projects[pid]; ok && p.TenantID == tenantID && !p.Disabled {
			names = append(names, p.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

func (s *MemStore) ListUserProjectIDs(tenantID, userID string) ([]string, error) {
	tenantID = tenantOrDefault(tenantID)
	s.mu.RLock()
	defer s.mu.RUnlock()
	ids := []string{}
	for _, pid := range s.userProjects[userID] {
		if p, ok := s.projects[pid]; ok && p.TenantID == tenantID {
			ids = append(ids, pid)
		}
	}
	return ids, nil
}

func (s *MemStore) SetUserProjects(tenantID, userID string, projectIDs []string) error {
	tenantID = tenantOrDefault(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := []string{}
	for _, pid := range projectIDs {
		if p, ok := s.projects[pid]; ok && p.TenantID == tenantID {
			kept = append(kept, pid)
		}
	}
	if len(kept) == 0 {
		delete(s.userProjects, userID)
		return nil
	}
	s.userProjects[userID] = kept
	return nil
}
