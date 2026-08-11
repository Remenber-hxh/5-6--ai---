package main

import (
	"fmt"
	"strings"
)

// ===== 版本化数据库迁移 =====
//
// 规则:
//   - 每次 schema 变更 = migrationList 里一个带编号的条目;schema_migrations 表记录已应用版本。
//   - 001~004 收编自历史的 ensureXxxSchema 路径,全部幂等(IF NOT EXISTS / hasColumn 守卫),
//     因此存量库(本地/生产)首次启动会把它们空跑一遍并记账,之后不再执行。
//   - 005 起的新迁移按"只执行一次"的语义编写即可(可以是非幂等 SQL);
//     双方言差异用 s.dialect 分支("sqlite" / "mysql")。
//   - 只往后加,不修改/删除已发布的历史条目。

type migration struct {
	Version int
	Name    string
	Run     func(s *SQLiteStore) error
}

var migrationList = []migration{
	{1, "baseline_schema", (*SQLiteStore).applyBaselineSchema},
	{2, "asset_display_columns", (*SQLiteStore).ensureAssetDisplaySchema},
	{3, "record_ownership", (*SQLiteStore).ensureRecordOwnershipSchema},
	{4, "prompt_templates", (*SQLiteStore).ensurePromptTemplateSchema},
	{5, "role_permissions", (*SQLiteStore).migRolePermissions},
	{6, "roles_code_widen", (*SQLiteStore).migRolesCodeWiden},
	{7, "role_permissions_code_widen", (*SQLiteStore).migRolePermCodeWiden},
	{8, "tenants", (*SQLiteStore).migTenants},
	{9, "tenant_columns", (*SQLiteStore).migTenantColumns},
	{10, "platform_admin_flag", (*SQLiteStore).migPlatformAdminFlag},
	{11, "offline_shots", (*SQLiteStore).migOfflineShots},
	{12, "registration_codes", (*SQLiteStore).migRegistrationCodes},
}

// 011 — 离线照片:弱网现场先存本机、联网后上传的照片。
//
// 时间戳刻意分两个:
//
//	captured_at  手机声称的拍摄时间(可伪造,仅供参考)
//	received_at  服务器收到时间(权威,客户端伪造不了)
//
// 两者都存、在记录上分开展示,不隐藏离线造成的时间差 —— 对监管方而言,
// 公开时间差比藏起来更可信。
//
// idempotency_key 唯一:弱网下"其实传成功了但响应没回来"的重放不会产生重复行。
func (s *SQLiteStore) migOfflineShots() error {
	stmt := `CREATE TABLE IF NOT EXISTS offline_shots (
		id              TEXT PRIMARY KEY,
		tenant_id       TEXT NOT NULL DEFAULT '` + defaultTenantID + `',
		user_id         TEXT NOT NULL DEFAULT '',
		inspector       TEXT NOT NULL DEFAULT '',
		idempotency_key TEXT NOT NULL UNIQUE,
		image_path      TEXT NOT NULL DEFAULT '',
		file_name       TEXT NOT NULL DEFAULT '',
		size_bytes      INTEGER NOT NULL DEFAULT 0,
		captured_at     TEXT NOT NULL DEFAULT '',
		received_at     TEXT NOT NULL DEFAULT '',
		lat             REAL,
		lng             REAL,
		accuracy        REAL,
		record_id       TEXT NOT NULL DEFAULT '',
		status          TEXT NOT NULL DEFAULT 'uploaded')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS offline_shots (
			id              VARCHAR(64)  NOT NULL PRIMARY KEY,
			tenant_id       VARCHAR(64)  NOT NULL DEFAULT '` + defaultTenantID + `',
			user_id         VARCHAR(64)  NOT NULL DEFAULT '',
			inspector       VARCHAR(64)  NOT NULL DEFAULT '',
			idempotency_key VARCHAR(128) NOT NULL UNIQUE,
			image_path      VARCHAR(512) NOT NULL DEFAULT '',
			file_name       VARCHAR(255) NOT NULL DEFAULT '',
			size_bytes      BIGINT       NOT NULL DEFAULT 0,
			captured_at     VARCHAR(40)  NOT NULL DEFAULT '',
			received_at     VARCHAR(40)  NOT NULL DEFAULT '',
			lat             DOUBLE       NULL,
			lng             DOUBLE       NULL,
			accuracy        DOUBLE       NULL,
			record_id       VARCHAR(64)  NOT NULL DEFAULT '',
			status          VARCHAR(24)  NOT NULL DEFAULT 'uploaded',
			INDEX idx_offline_shots_tenant_user (tenant_id, user_id, status)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	if s.dialect == "sqlite" {
		_, _ = s.db.Exec(
			`CREATE INDEX IF NOT EXISTS idx_offline_shots_tenant_user
			 ON offline_shots(tenant_id, user_id, status)`)
	}
	return nil
}

// 010 — 两级管理员:users 加 is_platform_admin。
//
// 「能否跨租户」是与租户归属正交的能力,故用独立标志位,而不是拿特殊 tenant_id
// 表达 —— 后者会让超管自己的业务数据无家可归(璟邑既是平台方也是第一个客户)。
//
// 初始管理员(user_admin)提升为平台超管:现网部署由璟邑运营,它就是平台方。
// 新建的租户管理员默认 0,作用域锁死在自己租户内。
func (s *SQLiteStore) migPlatformAdminFlag() error {
	exists, err := s.hasColumn("users", "is_platform_admin")
	if err != nil {
		return fmt.Errorf("inspect users.is_platform_admin: %w", err)
	}
	if !exists {
		stmt := `ALTER TABLE users ADD COLUMN is_platform_admin INTEGER NOT NULL DEFAULT 0`
		if s.dialect == "mysql" {
			stmt = `ALTER TABLE users ADD COLUMN is_platform_admin TINYINT(1) NOT NULL DEFAULT 0`
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("add users.is_platform_admin: %w", err)
		}
	}
	// 幂等:只提升初始管理员,重跑无副作用
	_, err = s.db.Exec(`UPDATE users SET is_platform_admin = 1 WHERE id = 'user_admin'`)
	return err
}

// 009 — 给各业务表加 tenant_id 并把存量回填到默认租户。
//
// 关键手法:ADD COLUMN ... NOT NULL DEFAULT 'tenant_default' —— 加列时
// DB 自动把存量行填成默认租户(一步完成回填);且在「代码尚未把租户串进
// 各处 insert」的过渡期,任何漏设 tenant_id 的新行也会落到默认租户 =
// 维持单租户的安全行为。多客户全面铺开后,再由后续迁移收紧这个默认。
//
// roles / role_permissions 不在此列:内置角色要全局共享、自定义角色才
// 归租户,语义和"全部回填默认租户"不同,单独一步处理。
func (s *SQLiteStore) migTenantColumns() error {
	tables := []string{
		"users", "departments", "assets", "asset_snapshots", "records",
		"change_requests", "engineering_tasks", "engineering_plan_items",
		"operation_logs", "prompt_templates", "ai_tasks",
		"management_ai_reports", "field_observations", "field_confirm_logs",
	}
	def := "'" + defaultTenantID + "'" // 单一真源:默认值直接取自 tenant.go 的常量
	for _, table := range tables {
		exists, err := s.hasColumn(table, "tenant_id")
		if err != nil {
			return fmt.Errorf("inspect %s.tenant_id: %w", table, err)
		}
		if exists {
			continue // 幂等:已加过就跳过
		}
		stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN tenant_id TEXT NOT NULL DEFAULT %s", table, def)
		if s.dialect == "mysql" {
			stmt = fmt.Sprintf("ALTER TABLE %s ADD COLUMN tenant_id VARCHAR(64) NOT NULL DEFAULT %s", table, def)
		}
		if _, err := s.db.Exec(stmt); err != nil {
			return fmt.Errorf("add %s.tenant_id: %w", table, err)
		}
	}
	return nil
}

// 008 — 多租户地基:tenants 表 + 默认租户(璟邑)。
// 本步只建新表、种一行,不碰任何现有表 —— 各业务表加 tenant_id 在 009 单独做。
func (s *SQLiteStore) migTenants() error {
	stmt := `CREATE TABLE IF NOT EXISTS tenants (
		id         TEXT PRIMARY KEY,
		name       TEXT NOT NULL,
		code       TEXT NOT NULL UNIQUE,
		status     TEXT NOT NULL DEFAULT 'active',
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS tenants (
			id         VARCHAR(64)  NOT NULL PRIMARY KEY,
			name       VARCHAR(128) NOT NULL,
			code       VARCHAR(64)  NOT NULL UNIQUE,
			status     VARCHAR(16)  NOT NULL DEFAULT 'active',
			created_at VARCHAR(40)  NOT NULL DEFAULT '',
			updated_at VARCHAR(40)  NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	// 种默认租户(幂等:主键/唯一键冲突即已种,重跑无害)。存量数据 009 回填到它。
	now := nowStamp()
	insert := `INSERT OR IGNORE INTO tenants (id, name, code, status, created_at, updated_at)
		VALUES (?, ?, ?, 'active', ?, ?)`
	if s.dialect == "mysql" {
		insert = `INSERT IGNORE INTO tenants (id, name, code, status, created_at, updated_at)
			VALUES (?, ?, ?, 'active', ?, ?)`
	}
	_, err := s.db.Exec(insert, defaultTenantID, defaultTenantName, defaultTenantCode, now, now)
	return err
}

// 007 — 权限矩阵表的 role_code 同步放宽(006 只放了 roles.code,漏了这张)
func (s *SQLiteStore) migRolePermCodeWiden() error {
	if s.dialect != "mysql" {
		return nil
	}
	_, err := s.db.Exec(`ALTER TABLE role_permissions MODIFY role_code VARCHAR(64) NOT NULL`)
	return err
}

// 006 — 自定义角色的 code 用生成 id,32 位不够,放宽到 64(SQLite 无列宽概念,空跑)
func (s *SQLiteStore) migRolesCodeWiden() error {
	if s.dialect != "mysql" {
		return nil
	}
	_, err := s.db.Exec(`ALTER TABLE roles MODIFY code VARCHAR(64) NOT NULL`)
	return err
}

// 005 — 角色×能力权限矩阵表 + 默认值(=引入前的固化行为)
func (s *SQLiteStore) migRolePermissions() error {
	stmt := `CREATE TABLE IF NOT EXISTS role_permissions (
		perm_key TEXT NOT NULL,
		role_code TEXT NOT NULL,
		PRIMARY KEY (perm_key, role_code))`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS role_permissions (
			perm_key VARCHAR(64) NOT NULL,
			role_code VARCHAR(32) NOT NULL,
			PRIMARY KEY (perm_key, role_code)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	for k, roles := range defaultPermMatrix() {
		for _, role := range roles {
			// 幂等:主键冲突即已种过
			if s.dialect == "mysql" {
				_, _ = s.db.Exec(`INSERT IGNORE INTO role_permissions (perm_key, role_code) VALUES (?, ?)`, k, role)
			} else {
				_, _ = s.db.Exec(`INSERT OR IGNORE INTO role_permissions (perm_key, role_code) VALUES (?, ?)`, k, role)
			}
		}
	}
	return nil
}

// runMigrations 按序应用未记账的迁移。
func (s *SQLiteStore) runMigrations() error {
	if err := s.ensureMigrationsTable(); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	applied := map[int]bool{}
	rows, err := s.db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()

	for _, m := range migrationList {
		if applied[m.Version] {
			continue
		}
		if err := m.Run(s); err != nil {
			return fmt.Errorf("migration %03d_%s: %w", m.Version, m.Name, err)
		}
		if _, err := s.db.Exec(
			`INSERT INTO schema_migrations (version, name, applied_at) VALUES (?, ?, ?)`,
			m.Version, m.Name, nowStamp(),
		); err != nil {
			return fmt.Errorf("record migration %03d: %w", m.Version, err)
		}
	}
	return nil
}

func (s *SQLiteStore) ensureMigrationsTable() error {
	stmt := `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL DEFAULT '',
		applied_at TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			name VARCHAR(128) NOT NULL DEFAULT '',
			applied_at VARCHAR(40) NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	}
	_, err := s.db.Exec(stmt)
	return err
}

// applyBaselineSchema — 001:内嵌的基础建表(全部 IF NOT EXISTS,幂等)。
func (s *SQLiteStore) applyBaselineSchema() error {
	if s.dialect == "mysql" {
		// go-sql-driver 默认单语句,逐条执行
		for _, stmt := range splitSQLStatements(schemaMySQL) {
			if strings.TrimSpace(stmt) == "" {
				continue
			}
			if _, err := s.db.Exec(stmt); err != nil {
				return fmt.Errorf("%w (statement: %s)", err, truncateStmt(stmt))
			}
		}
		return nil
	}
	_, err := s.db.Exec(schemaSQL)
	return err
}

// migRegistrationCodes — 012:注册码。
//
// 巡检员自助注册需要一道门槛:/api/assets 对任何已登录用户开放,谁注册成功
// 谁就能看到客户的全部设备台账和健康状态。所以注册必须凭码,码由管理员发。
//
// 码上带角色和租户,注册出来的账号直接落到对的位置,不用管理员事后再改。
// max_uses = 0 表示不限次数(发给整个班组的长期码);expires_at 为空表示不过期。
func (s *SQLiteStore) migRegistrationCodes() error {
	stmt := `CREATE TABLE IF NOT EXISTS registration_codes (
		id            TEXT PRIMARY KEY,
		code          TEXT NOT NULL UNIQUE,
		tenant_id     TEXT NOT NULL DEFAULT '` + defaultTenantID + `',
		role_code     TEXT NOT NULL DEFAULT 'inspector',
		department_id TEXT NOT NULL DEFAULT '',
		note          TEXT NOT NULL DEFAULT '',
		max_uses      INTEGER NOT NULL DEFAULT 0,
		used_count    INTEGER NOT NULL DEFAULT 0,
		expires_at    TEXT NOT NULL DEFAULT '',
		disabled      INTEGER NOT NULL DEFAULT 0,
		created_by    TEXT NOT NULL DEFAULT '',
		created_at    TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS registration_codes (
			id            VARCHAR(64)  NOT NULL PRIMARY KEY,
			code          VARCHAR(64)  NOT NULL UNIQUE,
			tenant_id     VARCHAR(64)  NOT NULL DEFAULT '` + defaultTenantID + `',
			role_code     VARCHAR(64)  NOT NULL DEFAULT 'inspector',
			department_id VARCHAR(64)  NOT NULL DEFAULT '',
			note          VARCHAR(255) NOT NULL DEFAULT '',
			max_uses      INT          NOT NULL DEFAULT 0,
			used_count    INT          NOT NULL DEFAULT 0,
			expires_at    VARCHAR(40)  NOT NULL DEFAULT '',
			disabled      TINYINT      NOT NULL DEFAULT 0,
			created_by    VARCHAR(64)  NOT NULL DEFAULT '',
			created_at    VARCHAR(40)  NOT NULL DEFAULT '',
			INDEX idx_registration_codes_tenant (tenant_id, disabled)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return err
	}
	if s.dialect == "sqlite" {
		_, _ = s.db.Exec(
			`CREATE INDEX IF NOT EXISTS idx_registration_codes_tenant
			 ON registration_codes(tenant_id, disabled)`)
	}
	return nil
}
