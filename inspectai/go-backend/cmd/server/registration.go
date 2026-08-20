package main

import (
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ===== 注册码 =====
//
// 巡检员自助注册必须有门槛。原因很具体:/api/assets 是 guardNone,
// 任何【已登录】用户都能拿到客户的全部设备台账和健康状态。开放注册
// 等于把这些数据对所有拿到网址的人开放。
//
// 选注册码而不是"注册后待审核",是因为班组临时加人时没人盯审批队列:
// 码提前发给班组长,新人自己注册完当场就能干活,管理员事后在后台能看到
// 每个码被谁用了多少次、随时停用。
//
// 码上带角色和租户 —— 注册出来的账号直接落到对的位置,不需要管理员
// 事后再改一遍身份。

type RegistrationCode struct {
	ID           string `json:"id"`
	Code         string `json:"code"`
	TenantID     string `json:"tenantId"`
	RoleCode     string `json:"roleCode"`
	DepartmentID string `json:"departmentId,omitempty"`
	Note         string `json:"note,omitempty"`
	/** 0 = 不限次数(发给整个班组的长期码) */
	MaxUses   int    `json:"maxUses"`
	UsedCount int    `json:"usedCount"`
	ExpiresAt string `json:"expiresAt,omitempty"` // 空 = 不过期
	Disabled  bool   `json:"disabled"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt string `json:"createdAt"`
}

// Usable 判断这个码此刻还能不能用。
//
// 三条独立的失效原因分开返回,是因为注册页要能告诉人【为什么】不能用 ——
// "注册码无效"这种统一话术会让新人反复重试同一个已经用完的码。
func (rc *RegistrationCode) Usable(now time.Time) error {
	if rc.Disabled {
		return errors.New("这个注册码已被停用,请找管理员要新的")
	}
	if rc.MaxUses > 0 && rc.UsedCount >= rc.MaxUses {
		return errors.New("这个注册码的可用次数已用完,请找管理员要新的")
	}
	if strings.TrimSpace(rc.ExpiresAt) != "" {
		exp, err := time.Parse(time.RFC3339, rc.ExpiresAt)
		if err == nil && now.After(exp) {
			return errors.New("这个注册码已过期,请找管理员要新的")
		}
	}
	return nil
}

type RegistrationStore interface {
	CreateRegistrationCode(rc *RegistrationCode) error
	ListRegistrationCodes(tenantID string) ([]*RegistrationCode, error)
	// GetRegistrationCode 按码本身查(注册时用)。查不到返回 sql.ErrNoRows。
	GetRegistrationCode(code string) (*RegistrationCode, error)
	// ConsumeRegistrationCode 原子地把 used_count +1。
	//
	// 【必须在 SQL 里带上限判断】先查再写会有竞态:同一个只剩 1 次的码被两个人
	// 同时提交,两次查询都看到"还能用",于是超发。次数限制是这个功能的全部意义,
	// 靠"大概率不会同时"来保证是不行的。
	ConsumeRegistrationCode(id string) error
	SetRegistrationCodeDisabled(tenantID, id string, disabled bool) error
	// DeleteRegistrationCode 直接删:没有别的东西引用它,
	// 用它注册出来的账号是独立的,删码不影响已有账号。
	DeleteRegistrationCode(tenantID, code string) error
}

// newRegistrationCode 生成人能念、能抄的码。
//
// 去掉了 0/O/1/I/L 这些形近字符:码要通过微信发、口头念、写在纸上传给
// 现场的巡检员,形近字符会直接变成"我明明输对了却说无效"的报错。
func newRegistrationCode() string {
	const alphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 8)
	_, _ = rand.Read(buf)
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out[:4]) + "-" + string(out[4:])
}

// normalizeRegistrationCode 让输入宽容一点:大小写、空格、中英文连字符都当同一个码。
// 现场用手机输入,自动大写和输入法替换的全角符号是常态。
func normalizeRegistrationCode(raw string) string {
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = strings.NewReplacer(" ", "", "—", "-", "–", "-", "－", "-").Replace(s)
	return s
}

// ===== SQLiteStore(MySQL 同一套 SQL)=====

const regCodeCols = `id, code, tenant_id, role_code, department_id, note,
	max_uses, used_count, expires_at, disabled, created_by, created_at`

func scanRegistrationCode(sc interface{ Scan(...any) error }) (*RegistrationCode, error) {
	var rc RegistrationCode
	var disabled int
	if err := sc.Scan(&rc.ID, &rc.Code, &rc.TenantID, &rc.RoleCode, &rc.DepartmentID,
		&rc.Note, &rc.MaxUses, &rc.UsedCount, &rc.ExpiresAt, &disabled,
		&rc.CreatedBy, &rc.CreatedAt); err != nil {
		return nil, err
	}
	rc.Disabled = disabled != 0
	return &rc, nil
}

func (s *SQLiteStore) CreateRegistrationCode(rc *RegistrationCode) error {
	disabled := 0
	if rc.Disabled {
		disabled = 1
	}
	_, err := s.db.Exec(`INSERT INTO registration_codes (`+regCodeCols+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rc.ID, rc.Code, rc.TenantID, rc.RoleCode, rc.DepartmentID, rc.Note,
		rc.MaxUses, rc.UsedCount, rc.ExpiresAt, disabled, rc.CreatedBy, rc.CreatedAt)
	return err
}

func (s *SQLiteStore) ListRegistrationCodes(tenantID string) ([]*RegistrationCode, error) {
	rows, err := s.db.Query(`SELECT `+regCodeCols+`
		FROM registration_codes WHERE tenant_id=?
		ORDER BY created_at DESC LIMIT 200`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*RegistrationCode{}
	for rows.Next() {
		rc, err := scanRegistrationCode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rc)
	}
	return out, rows.Err()
}

func (s *SQLiteStore) GetRegistrationCode(code string) (*RegistrationCode, error) {
	return scanRegistrationCode(s.db.QueryRow(
		`SELECT `+regCodeCols+` FROM registration_codes WHERE code=? LIMIT 1`, code))
}

func (s *SQLiteStore) ConsumeRegistrationCode(id string) error {
	// 上限判断写进 WHERE:并发下靠"影响行数=0"发现抢完了,不靠先查后写
	res, err := s.db.Exec(`UPDATE registration_codes
		SET used_count = used_count + 1
		WHERE id=? AND disabled=0 AND (max_uses=0 OR used_count < max_uses)`, id)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New("这个注册码刚好被用完了,请找管理员要新的")
	}
	return nil
}

func (s *SQLiteStore) SetRegistrationCodeDisabled(tenantID, id string, disabled bool) error {
	v := 0
	if disabled {
		v = 1
	}
	res, err := s.db.Exec(
		`UPDATE registration_codes SET disabled=? WHERE id=? AND tenant_id=?`,
		v, id, tenantID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// ===== MemStore =====

func (m *MemStore) CreateRegistrationCode(rc *RegistrationCode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.regCodes[rc.Code]; exists {
		return fmt.Errorf("注册码 %s 已存在", rc.Code)
	}
	cp := *rc
	m.regCodes[rc.Code] = &cp
	return nil
}

func (m *MemStore) ListRegistrationCodes(tenantID string) ([]*RegistrationCode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := []*RegistrationCode{}
	for _, rc := range m.regCodes {
		if rc.TenantID != tenantID {
			continue
		}
		cp := *rc
		out = append(out, &cp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt > out[j].CreatedAt })
	return out, nil
}

func (m *MemStore) GetRegistrationCode(code string) (*RegistrationCode, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rc, ok := m.regCodes[code]
	if !ok {
		return nil, sql.ErrNoRows
	}
	cp := *rc
	return &cp, nil
}

func (m *MemStore) ConsumeRegistrationCode(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rc := range m.regCodes {
		if rc.ID != id {
			continue
		}
		if rc.Disabled || (rc.MaxUses > 0 && rc.UsedCount >= rc.MaxUses) {
			return errors.New("这个注册码刚好被用完了,请找管理员要新的")
		}
		rc.UsedCount++
		return nil
	}
	return sql.ErrNoRows
}

func (m *MemStore) SetRegistrationCodeDisabled(tenantID, id string, disabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rc := range m.regCodes {
		if rc.ID == id && rc.TenantID == tenantID {
			rc.Disabled = disabled
			return nil
		}
	}
	return sql.ErrNoRows
}

// ===== 只验证、不建会话的密码校验 =====
//
// 自助改密码要先确认"你确实是本人"。不能直接借用 AuthenticateUser:
// 它会插一行 login_sessions、还会把 last_login_at 改成现在 —— 改个密码
// 就多一条登录记录、"上次登录"也被改掉,审计口径全乱了。

func (s *SQLiteStore) VerifyUserPassword(id, password string) error {
	var hash string
	err := s.db.QueryRow(
		`SELECT password_hash FROM users WHERE id=? AND status='active' LIMIT 1`,
		id).Scan(&hash)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errInvalidCredentials
		}
		return err
	}
	if !verifyPassword(password, hash) {
		return errInvalidCredentials
	}
	return nil
}

func (m *MemStore) VerifyUserPassword(id, password string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	item, ok := m.users[id]
	if !ok || item.user.Status != "active" || !verifyPassword(password, item.passwordHash) {
		return errInvalidCredentials
	}
	return nil
}

// isDuplicateKeyErr 判断"这条记录已经存在"。
//
// 三种来源都要认:CreateUser 自己的预检返回一句英文 message,而 SQLite 和
// MySQL 的唯一约束报错文案又各不相同(UNIQUE / Duplicate)。只认其中一种,
// 用户看到的就会是"注册失败"这种查不出原因的话。
// (offline_shots.go 里对幂等键做过同样的判断,那处是内联写的。)
func isDuplicateKeyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "UNIQUE") ||
		strings.Contains(msg, "Duplicate")
}
