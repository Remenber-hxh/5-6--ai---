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
		ID:             "user_admin",
		Username:       seed.Username,
		DisplayName:    seed.DisplayName,
		RoleID:         "role_admin",
		RoleCode:       "admin",
		RoleName:       "系统管理员",
		DepartmentID:   "dept_default",
		DepartmentName: "默认部门",
		Status:         "active",
		CreatedAt:      now,
		UpdatedAt:      now,
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
	now := time.Now().Format(time.RFC3339Nano)
	_, err = s.db.Exec(`
		INSERT INTO users (
			id, username, display_name, phone, avatar, role_id, department_id,
			wework_user_id, password_hash, status, created_at, updated_at
		) VALUES (?, ?, ?, '', '', 'role_admin', 'dept_default', '', ?, 'active', ?, ?)`,
		"user_admin", seed.Username, seed.DisplayName, hash, now, now,
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
			role.ID, role.Code, role.Name, role.Description, time.Now().Format(time.RFC3339Nano),
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
		time.Now().Format(time.RFC3339Nano),
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
		newID("sess"), u.ID, hashToken(token), expires.Format(time.RFC3339Nano),
		now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano),
	)
	if err != nil {
		return nil, nil, err
	}
	_, _ = s.db.Exec(`UPDATE users SET last_login_at=?, updated_at=? WHERE id=?`, now.Format(time.RFC3339Nano), now.Format(time.RFC3339Nano), u.ID)
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
	u, _, err := scanUserWithHash(s.db.QueryRow(q, hashToken(token), time.Now().Format(time.RFC3339Nano)))
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
		logItem.TargetType, logItem.TargetID, string(detailJSON), logItem.CreatedAt.Format(time.RFC3339Nano),
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
			u.password_hash
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
	err := row.Scan(
		&u.ID, &u.Username, &u.DisplayName, &phone, &avatar, &u.RoleID,
		&u.RoleCode, &u.RoleName, &deptID, &deptName, &wework, &u.Status,
		&lastLogin, &created, &updated, &passwordHash,
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
