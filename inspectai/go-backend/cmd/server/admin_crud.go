package main

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ===== 部门增删改 + 用户/项目/注册码的删除 =====
//
// 删除这件事在这套系统里一直缺位:只有"停用"。停用对大多数场景是【更好】的选择 ——
// 巡检记录里存着提交人,人删了记录就成了无主的。所以这里的规矩是:
//
//	【有东西引用它 → 拒绝删除,并告诉他该怎么办】
//
// 而不是连带删除、也不是悄悄置空。参照本仓库里 DeleteRole 已有的做法。
//
// 之所以不做"软删除"(加一列 deleted_at):这套系统已经有 status=disabled 表达
// "不再使用",再加一层只会让"停用"和"已删除"两个状态互相打架。

// 这些错误让 handler 能给出人话,而不是把 SQL 错误直接抛给用户。
var (
	errInUse         = errors.New("in use")
	errLastAdmin     = errors.New("last admin")
	errDeleteSelf    = errors.New("cannot delete self")
	errDeptNameTaken = errors.New("department name taken")
)

// ===== 部门 =====

func (s *SQLiteStore) CreateDepartment(name, parentID string) (*Department, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name required")
	}
	var dup int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM departments WHERE name=?`, name).Scan(&dup); err != nil {
		return nil, err
	}
	if dup > 0 {
		return nil, errDeptNameTaken
	}
	d := &Department{ID: newID("dept"), Name: name, ParentID: strings.TrimSpace(parentID), CreatedAt: time.Now()}
	_, err := s.db.Exec(
		`INSERT INTO departments (id, name, parent_id, created_at) VALUES (?, ?, ?, ?)`,
		d.ID, d.Name, nullableString(d.ParentID), nowStamp())
	return d, err
}

func (s *SQLiteStore) UpdateDepartment(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	var dup int
	if err := s.db.QueryRow(
		`SELECT COUNT(1) FROM departments WHERE name=? AND id<>?`, name, id).Scan(&dup); err != nil {
		return err
	}
	if dup > 0 {
		return errDeptNameTaken
	}
	res, err := s.db.Exec(`UPDATE departments SET name=? WHERE id=?`, name, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	// 不用同步用户表:部门名是查询时 JOIN departments 出来的,不是存一份副本
	// (见 userSelectSQL)。改这一行就够了。
	return nil
}

func (s *SQLiteStore) DeleteDepartment(id string) error {
	if id == "dept_default" {
		// 默认部门是新建用户的兜底归属,删了之后建出来的人没有部门
		return errInUse
	}
	var inUse int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM users WHERE department_id=?`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse > 0 {
		return errInUse
	}
	res, err := s.db.Exec(`DELETE FROM departments WHERE id=?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ===== 删除用户 =====

// DeleteUser 硬删除。
//
// 【有巡检记录就不能删】记录里存着 inspector_user_id 和提交人姓名,
// 人删了记录就成了无主的 —— 台账是给客户看的证据,不能出现查不到人的记录。
// 这种情况请用"停用":账号登不上,历史记录完好。
//
// 【操作日志不跟着删】那是审计证据,而且它单独存了 actor_name,
// 人没了照样读得懂。删审计日志这件事本身就该被审计。
func (s *SQLiteStore) DeleteUser(id, operatorID string) error {
	if id == operatorID {
		return errDeleteSelf
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var roleID, tenantID string
	if err := tx.QueryRow(`SELECT role_id, tenant_id FROM users WHERE id=?`, id).Scan(&roleID, &tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	var records int
	if err := tx.QueryRow(
		`SELECT COUNT(1) FROM records WHERE inspector_user_id=?`, id).Scan(&records); err != nil {
		return err
	}
	if records > 0 {
		return errInUse
	}
	// 【不能删掉最后一个管理员】删完没人能进后台了,而且没有任何提示 ——
	// 只能靠改库救回来。这种"把自己锁在门外"的操作必须在这里挡住。
	if roleID == "role_admin" {
		var admins int
		if err := tx.QueryRow(
			`SELECT COUNT(1) FROM users WHERE role_id='role_admin' AND status='active' AND tenant_id=?`,
			tenantID).Scan(&admins); err != nil {
			return err
		}
		if admins <= 1 {
			return errLastAdmin
		}
	}
	for _, stmt := range []string{
		`DELETE FROM login_sessions WHERE user_id=?`,
		`DELETE FROM user_projects WHERE user_id=?`,
		`DELETE FROM users WHERE id=?`,
	} {
		if _, err := tx.Exec(stmt, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ===== 删除项目 =====

// DeleteProject 硬删除。台账里还挂着设备就不给删 ——
// 项目名是业务表的关联键,删了项目那些设备就成了指向不存在项目的孤儿。
// 现场交付完了想让它从列表里消失,用"停用"。
func (s *SQLiteStore) DeleteProject(tenantID, id string) error {
	tenantID = tenantOrDefault(tenantID)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var name string
	if err := tx.QueryRow(
		`SELECT name FROM projects WHERE id=? AND tenant_id=?`, id, tenantID).Scan(&name); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return sql.ErrNoRows
		}
		return err
	}
	var assets int
	if err := tx.QueryRow(
		`SELECT COUNT(1) FROM assets WHERE project=? AND tenant_id=?`, name, tenantID).Scan(&assets); err != nil {
		return err
	}
	if assets > 0 {
		return errInUse
	}
	// 成员关系跟着删:它只是"谁属于这个项目",项目没了就没有意义,
	// 留着反而会让那个人变成"分了项目但查不到" —— 那是 fail closed 的空页面。
	if _, err := tx.Exec(`DELETE FROM user_projects WHERE project_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE id=? AND tenant_id=?`, id, tenantID); err != nil {
		return err
	}
	return tx.Commit()
}

// ===== 删除注册码 =====

// DeleteRegistrationCode 直接删。注册码没有别的东西引用它 ——
// 用它注册出来的账号是独立的,删码不影响已有账号。
func (s *SQLiteStore) DeleteRegistrationCode(tenantID, code string) error {
	res, err := s.db.Exec(
		`DELETE FROM registration_codes WHERE code=? AND tenant_id=?`, code, tenantOrDefault(tenantID))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ===== MemStore(测试 / 无库回落) =====
//
// 守卫规则必须和 SQLiteStore 完全一致 —— 否则拿内存实现写的测试
// 保不住真库的行为,这是最容易骗过自己的一种测试。

func (s *MemStore) CreateDepartment(name, parentID string) (*Department, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.departments {
		if d.Name == name {
			return nil, errDeptNameTaken
		}
	}
	d := &Department{ID: newID("dept"), Name: name, ParentID: strings.TrimSpace(parentID), CreatedAt: time.Now()}
	s.departments[d.ID] = d
	cp := *d
	return &cp, nil
}

func (s *MemStore) UpdateDepartment(id, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, d := range s.departments {
		if d.Name == name && d.ID != id {
			return errDeptNameTaken
		}
	}
	d, ok := s.departments[id]
	if !ok {
		return sql.ErrNoRows
	}
	d.Name = name
	return nil
}

func (s *MemStore) DeleteDepartment(id string) error {
	if id == "dept_default" {
		return errInUse
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.users {
		if item.user.DepartmentID == id {
			return errInUse
		}
	}
	if _, ok := s.departments[id]; !ok {
		return sql.ErrNoRows
	}
	delete(s.departments, id)
	return nil
}

func (s *MemStore) DeleteUser(id, operatorID string) error {
	if id == operatorID {
		return errDeleteSelf
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.users[id]
	if !ok {
		return sql.ErrNoRows
	}
	for _, rec := range s.records {
		if rec.InspectorUserID == id {
			return errInUse
		}
	}
	if item.user.RoleCode == roleAdmin {
		admins := 0
		for _, other := range s.users {
			if other.user.RoleCode == roleAdmin && other.user.Status == "active" &&
				other.user.TenantID == item.user.TenantID {
				admins++
			}
		}
		if admins <= 1 {
			return errLastAdmin
		}
	}
	for token, sess := range s.sessions {
		if sess.UserID == id {
			delete(s.sessions, token)
		}
	}
	delete(s.userProjects, id)
	delete(s.users, id)
	return nil
}

func (s *MemStore) DeleteProject(tenantID, id string) error {
	tenantID = tenantOrDefault(tenantID)
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.projects[id]
	if !ok || p.TenantID != tenantID {
		return sql.ErrNoRows
	}
	for _, a := range s.assets {
		if a.TenantID == tenantID && a.Project == p.Name {
			return errInUse
		}
	}
	for uid, ids := range s.userProjects {
		kept := ids[:0]
		for _, pid := range ids {
			if pid != id {
				kept = append(kept, pid)
			}
		}
		if len(kept) == 0 {
			delete(s.userProjects, uid)
		} else {
			s.userProjects[uid] = kept
		}
	}
	delete(s.projects, id)
	return nil
}

func (s *MemStore) DeleteRegistrationCode(tenantID, code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	rc, ok := s.regCodes[code]
	if !ok || tenantOrDefault(rc.TenantID) != tenantOrDefault(tenantID) {
		return sql.ErrNoRows
	}
	delete(s.regCodes, code)
	return nil
}
