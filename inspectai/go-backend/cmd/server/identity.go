package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

var errInvalidCredentials = errors.New("invalid username or password")

const (
	sessionTTL          = 8 * time.Hour
	defaultAdminUser    = "admin"
	defaultAdminPass    = "InspectAI@2026"
	defaultAdminName    = "张管理员"
	passwordHashVersion = "pbkdf2-sha256"
	passwordIterations  = 120000
)

func normalizeIdentitySeed(seed IdentitySeed) IdentitySeed {
	seed.Username = strings.TrimSpace(seed.Username)
	seed.Password = strings.TrimSpace(seed.Password)
	seed.DisplayName = strings.TrimSpace(seed.DisplayName)
	if seed.Username == "" {
		seed.Username = defaultAdminUser
	}
	if seed.Password == "" {
		seed.Password = defaultAdminPass
	}
	if seed.DisplayName == "" {
		seed.DisplayName = defaultAdminName
	}
	return seed
}

func randomHex(n int) string {
	buf := make([]byte, n)
	_, _ = rand.Read(buf)
	return hex.EncodeToString(buf)
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func hashPassword(password string) (string, error) {
	salt := randomHex(16)
	dk := pbkdf2SHA256([]byte(password), []byte(salt), passwordIterations, 32)
	return fmt.Sprintf("%s$%d$%s$%s", passwordHashVersion, passwordIterations, salt, hex.EncodeToString(dk)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != passwordHashVersion {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter <= 0 {
		return false
	}
	salt := parts[2]
	want, err := hex.DecodeString(parts[3])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), []byte(salt), iter, len(want))
	return hmac.Equal(got, want)
}

func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	hLen := sha256.Size
	numBlocks := (keyLen + hLen - 1) / hLen
	var out []byte
	for block := 1; block <= numBlocks; block++ {
		mac := hmac.New(sha256.New, password)
		mac.Write(salt)
		mac.Write([]byte{byte(block >> 24), byte(block >> 16), byte(block >> 8), byte(block)})
		u := mac.Sum(nil)
		t := make([]byte, len(u))
		copy(t, u)
		for i := 1; i < iter; i++ {
			mac = hmac.New(sha256.New, password)
			mac.Write(u)
			u = mac.Sum(nil)
			for j := range t {
				t[j] ^= u[j]
			}
		}
		out = append(out, t...)
	}
	return out[:keyLen]
}

func cloneUser(u *User) *User {
	if u == nil {
		return nil
	}
	cp := *u
	if u.LastLoginAt != nil {
		t := *u.LastLoginAt
		cp.LastLoginAt = &t
	}
	return &cp
}

// ===== MemStore identity =====

func (s *MemStore) EnsureIdentitySeed(seed IdentitySeed) error {
	seed = normalizeIdentitySeed(seed)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.users) > 0 {
		return nil
	}
	now := time.Now()
	hash, err := hashPassword(seed.Password)
	if err != nil {
		return err
	}
	u := &User{
		ID:              "user_admin",
		TenantID:        defaultTenantID,
		IsPlatformAdmin: true,
		Username:        seed.Username,
		DisplayName:     seed.DisplayName,
		RoleID:          "role_admin",
		RoleCode:        "admin",
		RoleName:        "系统管理员",
		DepartmentID:    "dept_default",
		DepartmentName:  "默认部门",
		Status:          "active",
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	s.users[u.ID] = &memUser{user: u, passwordHash: hash}
	return nil
}

func (s *MemStore) AuthenticateUser(username, password string) (*User, *LoginSession, error) {
	username = strings.TrimSpace(username)
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.users {
		if item.user.Username != username {
			continue
		}
		if item.user.Status != "active" || !verifyPassword(password, item.passwordHash) {
			return nil, nil, errInvalidCredentials
		}
		now := time.Now()
		item.user.LastLoginAt = &now
		item.user.UpdatedAt = now
		token := randomHex(32)
		sess := &LoginSession{
			ID:        newID("sess"),
			UserID:    item.user.ID,
			Token:     token,
			ExpiresAt: now.Add(sessionTTL),
			CreatedAt: now,
			UpdatedAt: now,
		}
		s.sessions[hashToken(token)] = sess
		return cloneUser(item.user), sess, nil
	}
	return nil, nil, errInvalidCredentials
}

func (s *MemStore) GetUserBySession(token string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[hashToken(token)]
	if !ok || time.Now().After(sess.ExpiresAt) {
		return nil, sql.ErrNoRows
	}
	item, ok := s.users[sess.UserID]
	if !ok || item.user.Status != "active" {
		return nil, sql.ErrNoRows
	}
	return cloneUser(item.user), nil
}

func (s *MemStore) ListUsers() ([]*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*User, 0, len(s.users))
	for _, item := range s.users {
		out = append(out, cloneUser(item.user))
	}
	return out, nil
}

func (s *MemStore) DeleteSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, hashToken(token))
	return nil
}

func (s *MemStore) DeleteUserSessions(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for token, sess := range s.sessions {
		if sess.UserID == userID {
			delete(s.sessions, token)
		}
	}
	return nil
}

func (s *MemStore) GetUser(id string) (*User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.users[id]
	if !ok {
		return nil, sql.ErrNoRows
	}
	return cloneUser(item.user), nil
}

func (s *MemStore) CreateUser(user *User, password string) error {
	if strings.TrimSpace(user.Username) == "" {
		return errors.New("username required")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password required")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.users {
		if item.user.Username == user.Username {
			return errors.New("username already exists")
		}
	}
	if user.ID == "" {
		user.ID = newID("user")
	}
	now := time.Now()
	user.CreatedAt = now
	user.UpdatedAt = now
	if user.Status == "" {
		user.Status = "active"
	}
	user.RoleName = roleNameByCode(user.RoleCode)
	if user.DepartmentID == "" {
		user.DepartmentID = "dept_default"
		user.DepartmentName = "默认部门"
	}
	if user.TenantID == "" {
		user.TenantID = defaultTenantID
	}
	s.users[user.ID] = &memUser{user: cloneUser(user), passwordHash: hash}
	return nil
}

// errDisplayNameTaken 同租户里姓名重了。
//
// 【为什么姓名要唯一】系统里到处按姓名认人:计划的负责人、任务的执行人、
// 巡检记录的提交人,历史数据里存的都是名字而不是账号。两个人同名,
// 这些地方就分不清谁是谁 —— 而分错的表现是"提醒发给了另一个同名的人",
// 不报错,得等有人抱怨才发现。
//
// 【代价说清楚】现实里重名很常见。真遇到第二个「张伟」,让他登记成
// 「张伟(工程部)」这类带区分的写法 —— 麻烦一次,好过一直分不清。
var errDisplayNameTaken = errors.New("display name taken")

// ensureDisplayNameFree 同租户内姓名不能重。excludeUserID 用于改名时排除自己。
//
// 【必须是所有入口共用的一个函数】建账号、扫码注册、改资料是三条路,
// 各写一份的话总有一条会漏 —— 而漏掉的那条就是重名进来的入口。
func (s *Server) ensureDisplayNameFree(tenantID, name, excludeUserID string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	users, err := s.store.ListUsers()
	if err != nil {
		return err
	}
	key := ownerNameKey(name)
	for _, u := range users {
		if u == nil || u.ID == excludeUserID {
			continue
		}
		if tenantOrDefault(u.TenantID) != tenantOrDefault(tenantID) {
			continue
		}
		// 【按归一化后的键比,不是原样比】"张 伟" 和 "张伟" 在所有按姓名
		// 认人的地方都会被当成同一个人,那这里就不该放行第二个。
		if ownerNameKey(u.DisplayName) == key {
			return errDisplayNameTaken
		}
	}
	return nil
}

func (s *MemStore) UpdateUserProfile(id string, mutate func(*User)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.users[id]
	if !ok {
		return sql.ErrNoRows
	}
	mutate(item.user)
	item.user.UpdatedAt = time.Now()
	item.user.RoleName = roleNameByCode(item.user.RoleCode)
	return nil
}

func (s *MemStore) SetUserPassword(id, password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("password required")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.users[id]
	if !ok {
		return sql.ErrNoRows
	}
	item.passwordHash = hash
	item.user.UpdatedAt = time.Now()
	return nil
}

func (s *MemStore) SetUserStatus(id, status string) error {
	if status != "active" && status != "disabled" {
		return errors.New("invalid status")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	item, ok := s.users[id]
	if !ok {
		return sql.ErrNoRows
	}
	item.user.Status = status
	item.user.UpdatedAt = time.Now()
	if status == "disabled" {
		for token, sess := range s.sessions {
			if sess.UserID == id {
				delete(s.sessions, token)
			}
		}
	}
	return nil
}

func (s *MemStore) ListRoles() ([]*Role, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.roles) == 0 {
		return defaultRoles(), nil
	}
	out := make([]*Role, 0, len(s.roles))
	for _, r := range s.roles {
		cp := *r
		out = append(out, &cp)
	}
	sortRoles(out)
	return out, nil
}

func (s *MemStore) ensureRolesLocked() {
	if len(s.roles) == 0 {
		s.roles = map[string]*Role{}
		for _, r := range defaultRoles() {
			s.roles[r.ID] = r
		}
	}
}

func (s *MemStore) GetRoleByCode(code string) (*Role, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRolesLocked()
	for _, r := range s.roles {
		if r.Code == code {
			cp := *r
			return &cp, true, nil
		}
	}
	return nil, false, nil
}

func (s *MemStore) CreateRole(role *Role) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRolesLocked()
	for _, r := range s.roles {
		if r.Code == role.Code || r.Name == role.Name {
			return errors.New("role already exists")
		}
	}
	role.CreatedAt = time.Now()
	cp := *role
	s.roles[role.ID] = &cp
	return nil
}

func (s *MemStore) UpdateRole(id, name, description string) (*Role, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRolesLocked()
	r, ok := s.roles[id]
	if !ok {
		return nil, errors.New("role not found")
	}
	if name != "" {
		r.Name = name
	}
	r.Description = description
	cp := *r
	return &cp, nil
}

func (s *MemStore) DeleteRole(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureRolesLocked()
	r, ok := s.roles[id]
	if !ok {
		return errors.New("role not found")
	}
	for _, item := range s.users {
		if item.user.RoleID == id || item.user.RoleCode == r.Code {
			return errors.New("role in use")
		}
	}
	delete(s.roles, id)
	delete(s.rolePerms, r.Code)
	return nil
}

func (s *MemStore) ListDepartments() ([]*Department, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Department, 0, len(s.departments))
	for _, d := range s.departments {
		cp := *d
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func roleNameByCode(code string) string {
	for _, r := range defaultRoles() {
		if r.Code == code {
			return r.Name
		}
	}
	return code
}

func defaultRoles() []*Role {
	now := time.Now()
	return []*Role{
		{ID: "role_admin", Code: "admin", Name: "系统管理员", Description: "系统配置、用户管理、全部数据权限", CreatedAt: now},
		{ID: "role_manager", Code: "manager", Name: "管理人员", Description: "资产台账、统计报表、项目管理", CreatedAt: now},
		{ID: "role_supervisor", Code: "supervisor", Name: "复核审批人员", Description: "异常复核、修改审批、巡检记录查看", CreatedAt: now},
		{ID: "role_inspector", Code: "inspector", Name: "一线巡检员", Description: "移动端拍照巡检、日报提交、修改申请", CreatedAt: now},
	}
}

func (s *MemStore) CreateOperationLog(logItem *OperationLog) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if logItem.ID == "" {
		logItem.ID = newID("oplog")
	}
	if logItem.CreatedAt.IsZero() {
		logItem.CreatedAt = time.Now()
	}
	s.operationLogs[logItem.ID] = logItem
	return nil
}

func (s *MemStore) ListOperationLogs(limit int) ([]*OperationLog, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*OperationLog, 0, len(s.operationLogs))
	for _, item := range s.operationLogs {
		cp := *item
		out = append(out, &cp)
	}
	sortOperationLogs(out)
	if limit <= 0 || limit > len(out) {
		return out, nil
	}
	return out[:limit], nil
}

// ===== SQLStore identity =====

func (s *SQLiteStore) EnsureIdentitySeed(seed IdentitySeed) error {
	seed = normalizeIdentitySeed(seed)
	if err := s.ensureDefaultRoles(); err != nil {
		return err
	}
	if err := s.ensureDefaultDepartment(); err != nil {
		return err
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	hash, err := hashPassword(seed.Password)
	if err != nil {
		return err
	}
	now := nowStamp()
	_, err = s.db.Exec(`
		INSERT INTO users (
			id, username, display_name, phone, avatar, role_id, department_id,
			wework_user_id, password_hash, status, created_at, updated_at, tenant_id, is_platform_admin
		) VALUES (?, ?, ?, '', '', 'role_admin', 'dept_default', '', ?, 'active', ?, ?, ?, 1)`,
		"user_admin", seed.Username, seed.DisplayName, hash, now, now, defaultTenantID,
	)
	return err
}

func (s *SQLiteStore) ensureDefaultRoles() error {
	roles := []Role{
		{ID: "role_admin", Code: "admin", Name: "系统管理员", Description: "系统配置、用户管理、全部数据权限"},
		{ID: "role_manager", Code: "manager", Name: "管理人员", Description: "资产台账、统计报表、项目管理"},
		{ID: "role_supervisor", Code: "supervisor", Name: "复核审批人员", Description: "异常复核、修改审批、巡检记录查看"},
		{ID: "role_inspector", Code: "inspector", Name: "一线巡检员", Description: "移动端拍照巡检、日报提交、修改申请"},
	}
	for _, role := range roles {
		var exists int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM roles WHERE id=?`, role.ID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO roles (id, code, name, description, created_at) VALUES (?, ?, ?, ?, ?)`,
			role.ID, role.Code, role.Name, role.Description, nowStamp(),
		); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteStore) ensureDefaultDepartment() error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM departments WHERE id='dept_default'`).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil
	}
	_, err := s.db.Exec(
		`INSERT INTO departments (id, name, parent_id, created_at) VALUES ('dept_default', '默认部门', NULL, ?)`,
		nowStamp(),
	)
	return err
}

func (s *SQLiteStore) AuthenticateUser(username, password string) (*User, *LoginSession, error) {
	username = strings.TrimSpace(username)
	u, hash, err := s.getUserByUsernameWithHash(username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, errInvalidCredentials
		}
		return nil, nil, err
	}
	if u.Status != "active" || !verifyPassword(password, hash) {
		return nil, nil, errInvalidCredentials
	}
	token := randomHex(32)
	now := time.Now()
	expires := now.Add(sessionTTL)
	_, err = s.db.Exec(`
		INSERT INTO login_sessions (id, user_id, token_hash, expire_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		newID("sess"), u.ID, hashToken(token), fmtStamp(expires),
		fmtStamp(now), fmtStamp(now),
	)
	if err != nil {
		return nil, nil, err
	}
	_, _ = s.db.Exec(`UPDATE users SET last_login_at=?, updated_at=? WHERE id=?`, fmtStamp(now), fmtStamp(now), u.ID)
	u.LastLoginAt = &now
	u.UpdatedAt = now
	return u, &LoginSession{
		ID:        "",
		UserID:    u.ID,
		Token:     token,
		ExpiresAt: expires,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

func (s *SQLiteStore) getUserByUsernameWithHash(username string) (*User, string, error) {
	q := userSelectSQL() + ` WHERE u.username=? LIMIT 1`
	row := s.db.QueryRow(q, username)
	return scanUserWithHash(row)
}

func (s *SQLiteStore) GetUserBySession(token string) (*User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, sql.ErrNoRows
	}
	q := userSelectSQL() + `
		JOIN login_sessions s ON s.user_id=u.id
		WHERE s.token_hash=? AND s.expire_at>? AND u.status='active'
		LIMIT 1`
	u, _, err := scanUserWithHash(s.db.QueryRow(q, hashToken(token), nowStamp()))
	return u, err
}

func (s *SQLiteStore) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(userSelectSQL() + ` ORDER BY u.created_at DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, _, err := scanUserWithHash(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM login_sessions WHERE token_hash=?`, hashToken(token))
	return err
}

func (s *SQLiteStore) DeleteUserSessions(userID string) error {
	_, err := s.db.Exec(`DELETE FROM login_sessions WHERE user_id=?`, userID)
	return err
}

func (s *SQLiteStore) GetUser(id string) (*User, error) {
	q := userSelectSQL() + ` WHERE u.id=? LIMIT 1`
	u, _, err := scanUserWithHash(s.db.QueryRow(q, id))
	return u, err
}

func (s *SQLiteStore) CreateUser(user *User, password string) error {
	if strings.TrimSpace(user.Username) == "" {
		return errors.New("username required")
	}
	if strings.TrimSpace(password) == "" {
		return errors.New("password required")
	}
	if err := s.ensureDefaultRoles(); err != nil {
		return err
	}
	if err := s.ensureDefaultDepartment(); err != nil {
		return err
	}
	// 唯一性预检（schema 也有 UNIQUE 约束兜底）
	var dup int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM users WHERE username=?`, user.Username).Scan(&dup); err != nil {
		return err
	}
	if dup > 0 {
		return errors.New("username already exists")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if user.ID == "" {
		user.ID = newID("user")
	}
	if user.Status == "" {
		user.Status = "active"
	}
	if user.RoleID == "" {
		user.RoleID = s.roleIDFor(user.RoleCode)
	}
	if user.DepartmentID == "" {
		user.DepartmentID = "dept_default"
	}
	if user.TenantID == "" {
		user.TenantID = defaultTenantID // 未指定则归默认租户(单租户过渡期)
	}
	now := nowStamp()
	_, err = s.db.Exec(`
		INSERT INTO users (
			id, username, display_name, phone, avatar, role_id, department_id,
			wework_user_id, password_hash, status, created_at, updated_at, tenant_id, is_platform_admin,
			data_scope
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.DisplayName, user.Phone, user.Avatar,
		user.RoleID, user.DepartmentID, user.WeworkUserID, hash, user.Status, now, now, user.TenantID,
		boolToInt(user.IsPlatformAdmin), user.DataScope,
	)
	if err != nil {
		return err
	}
	saved, getErr := s.GetUser(user.ID)
	if getErr == nil && saved != nil {
		*user = *saved
	}
	return nil
}

func (s *SQLiteStore) UpdateUserProfile(id string, mutate func(*User)) error {
	u, err := s.GetUser(id)
	if err != nil {
		return err
	}
	mutate(u)
	if u.RoleID == "" {
		u.RoleID = s.roleIDFor(u.RoleCode)
	}
	now := nowStamp()
	res, err := s.db.Exec(`
		UPDATE users SET
			display_name=?, phone=?, avatar=?, role_id=?, department_id=?,
			wework_user_id=?, data_scope=?, updated_at=?
		WHERE id=?`,
		u.DisplayName, u.Phone, u.Avatar, u.RoleID,
		nullableString(u.DepartmentID), u.WeworkUserID, u.DataScope, now, id,
	)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLiteStore) SetUserPassword(id, password string) error {
	if strings.TrimSpace(password) == "" {
		return errors.New("password required")
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	now := nowStamp()
	res, err := s.db.Exec(`UPDATE users SET password_hash=?, updated_at=? WHERE id=?`, hash, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	// 改密码后强制登出所有 session
	_, _ = s.db.Exec(`DELETE FROM login_sessions WHERE user_id=?`, id)
	return nil
}

func (s *SQLiteStore) SetUserStatus(id, status string) error {
	if status != "active" && status != "disabled" {
		return errors.New("invalid status")
	}
	now := nowStamp()
	res, err := s.db.Exec(`UPDATE users SET status=?, updated_at=? WHERE id=?`, status, now, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if status == "disabled" {
		_, _ = s.db.Exec(`DELETE FROM login_sessions WHERE user_id=?`, id)
	}
	return nil
}

func (s *SQLiteStore) GetRoleByCode(code string) (*Role, bool, error) {
	row := s.db.QueryRow(`SELECT id, code, name, description, created_at FROM roles WHERE code=?`, code)
	var r Role
	var created string
	if err := row.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &created); err != nil {
		if err == sql.ErrNoRows {
			return nil, false, nil
		}
		return nil, false, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &r, true, nil
}

func (s *SQLiteStore) CreateRole(role *Role) error {
	var dup int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM roles WHERE code=? OR name=?`, role.Code, role.Name).Scan(&dup); err != nil {
		return err
	}
	if dup > 0 {
		return errors.New("role already exists")
	}
	now := nowStamp()
	_, err := s.db.Exec(`INSERT INTO roles (id, code, name, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		role.ID, role.Code, role.Name, role.Description, now)
	return err
}

func (s *SQLiteStore) UpdateRole(id, name, description string) (*Role, error) {
	res, err := s.db.Exec(`UPDATE roles SET name=?, description=? WHERE id=?`, name, description, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, errors.New("role not found")
	}
	row := s.db.QueryRow(`SELECT id, code, name, description, created_at FROM roles WHERE id=?`, id)
	var r Role
	var created string
	if err := row.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &created); err != nil {
		return nil, err
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return &r, nil
}

func (s *SQLiteStore) DeleteRole(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var code string
	if err := tx.QueryRow(`SELECT code FROM roles WHERE id=?`, id).Scan(&code); err != nil {
		if err == sql.ErrNoRows {
			return errors.New("role not found")
		}
		return err
	}
	var inUse int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role_id=?`, id).Scan(&inUse); err != nil {
		return err
	}
	if inUse > 0 {
		return errors.New("role in use")
	}
	if _, err := tx.Exec(`DELETE FROM roles WHERE id=?`, id); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM role_permissions WHERE role_code=?`, code); err != nil {
		return err
	}
	return tx.Commit()
}

// roleIDFor — 角色 code → id,查库(支持自定义角色);查不到落 inspector。
func (s *SQLiteStore) roleIDFor(code string) string {
	if r, ok, err := s.GetRoleByCode(code); err == nil && ok {
		return r.ID
	}
	return "role_inspector"
}

func (s *SQLiteStore) ListRoles() ([]*Role, error) {
	rows, err := s.db.Query(`SELECT id, code, name, description, created_at FROM roles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Role
	for rows.Next() {
		r := &Role{}
		var createdStr string
		if err := rows.Scan(&r.ID, &r.Code, &r.Name, &r.Description, &createdStr); err != nil {
			return nil, err
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		out = append(out, r)
	}
	if len(out) == 0 {
		return defaultRoles(), nil
	}
	return out, rows.Err()
}

func (s *SQLiteStore) ListDepartments() ([]*Department, error) {
	rows, err := s.db.Query(`SELECT id, name, COALESCE(parent_id, ''), created_at FROM departments ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Department
	for rows.Next() {
		d := &Department{}
		var createdStr string
		if err := rows.Scan(&d.ID, &d.Name, &d.ParentID, &createdStr); err != nil {
			return nil, err
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdStr)
		out = append(out, d)
	}
	return out, rows.Err()
}

func roleIDFromCode(code string) string {
	for _, r := range defaultRoles() {
		if r.Code == code {
			return r.ID
		}
	}
	return "role_inspector"
}

func (s *SQLiteStore) CreateOperationLog(logItem *OperationLog) error {
	if logItem.ID == "" {
		logItem.ID = newID("oplog")
	}
	if logItem.CreatedAt.IsZero() {
		logItem.CreatedAt = time.Now()
	}
	detailJSON, _ := json.Marshal(logItem.Detail)
	_, err := s.db.Exec(`
		INSERT INTO operation_logs (
			id, user_id, actor_name, action, target_type, target_id, detail_json, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		logItem.ID, nullableString(logItem.UserID), logItem.ActorName, logItem.Action,
		logItem.TargetType, logItem.TargetID, string(detailJSON), fmtStamp(logItem.CreatedAt),
	)
	return err
}

func (s *SQLiteStore) ListOperationLogs(limit int) ([]*OperationLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.Query(`
		SELECT id, user_id, actor_name, action, target_type, target_id, detail_json, created_at
		FROM operation_logs
		ORDER BY created_at DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*OperationLog
	for rows.Next() {
		item, err := scanOperationLog(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func userSelectSQL() string {
	return `
		SELECT
			u.id, u.username, u.display_name, u.phone, u.avatar, u.role_id,
			COALESCE(r.code, ''), COALESCE(r.name, ''),
			COALESCE(u.department_id, ''), COALESCE(d.name, ''),
			u.wework_user_id, u.status, u.last_login_at, u.created_at, u.updated_at,
			COALESCE(u.tenant_id, ''), COALESCE(u.is_platform_admin, 0),
			COALESCE(u.data_scope, ''), u.password_hash
		FROM users u
		LEFT JOIN roles r ON r.id=u.role_id
		LEFT JOIN departments d ON d.id=u.department_id`
}

type userScanner interface {
	Scan(dest ...any) error
}

func scanUserWithHash(row userScanner) (*User, string, error) {
	u := &User{}
	var phone, avatar, deptID, deptName, wework, lastLogin sql.NullString
	var created, updated, passwordHash string
	var platformAdminInt int
	var dataScope sql.NullString
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &phone, &avatar, &u.RoleID,
		&u.RoleCode, &u.RoleName, &deptID, &deptName, &wework, &u.Status,
		&lastLogin, &created, &updated, &u.TenantID, &platformAdminInt,
		&dataScope, &passwordHash,
	)
	if err != nil {
		return nil, "", err
	}
	if phone.Valid {
		u.Phone = phone.String
	}
	if avatar.Valid {
		u.Avatar = avatar.String
	}
	if deptID.Valid {
		u.DepartmentID = deptID.String
	}
	if deptName.Valid {
		u.DepartmentName = deptName.String
	}
	if wework.Valid {
		u.WeworkUserID = wework.String
	}
	if lastLogin.Valid && lastLogin.String != "" {
		t, _ := time.Parse(time.RFC3339Nano, lastLogin.String)
		u.LastLoginAt = &t
	}
	u.IsPlatformAdmin = platformAdminInt != 0
	u.DataScope = dataScope.String
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	u.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return u, passwordHash, nil
}

func scanOperationLog(row scanner) (*OperationLog, error) {
	item := &OperationLog{}
	var userID, detailJSON sql.NullString
	var created string
	if err := row.Scan(&item.ID, &userID, &item.ActorName, &item.Action, &item.TargetType, &item.TargetID, &detailJSON, &created); err != nil {
		return nil, err
	}
	if userID.Valid {
		item.UserID = userID.String
	}
	if detailJSON.Valid && detailJSON.String != "" {
		_ = json.Unmarshal([]byte(detailJSON.String), &item.Detail)
	}
	if item.Detail == nil {
		item.Detail = map[string]any{}
	}
	item.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	return item, nil
}

func sortOperationLogs(items []*OperationLog) {
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})
}
