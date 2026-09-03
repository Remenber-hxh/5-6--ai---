package main

import (
	"encoding/json"
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
	{13, "user_data_scope", (*SQLiteStore).migUserDataScope},
	{14, "projects", (*SQLiteStore).migProjects},
	{15, "offline_shot_asset", (*SQLiteStore).migOfflineShotAsset},
	{16, "template_field_rules", (*SQLiteStore).migTemplateFieldRules},
	{17, "template_settings", (*SQLiteStore).migTemplateSettings},
	{18, "plan_type", (*SQLiteStore).migPlanType},
	{19, "app_settings", (*SQLiteStore).migAppSettings},
	{20, "plan_assets", (*SQLiteStore).migPlanAssets},
	{21, "plan_owner_id", (*SQLiteStore).migPlanOwnerID},
	{22, "push_log", (*SQLiteStore).migPushLog},
	{23, "asset_profile_fields", (*SQLiteStore).migAssetProfileFields},
	{24, "prompt_versions", (*SQLiteStore).migPromptVersions},
	{25, "report_templates", (*SQLiteStore).migReportTemplates},
	{26, "merge_prompt_into_template", (*SQLiteStore).migMergePromptIntoTemplate},
}

// 026 — 把提示词那张表并进模板字段表。
//
// 【为什么合并】同一个字段原来被描述了两遍:report_template_fields 存"怎么填"
// (类型/选项/必填),prompt_templates 存"怎么判"(判是看什么、判否看什么)。
// 两边的 code 一个不差地重合 —— 它们本来就是同一批字段。
//
// 分着放的代价是永久的:每加一个功能都要问"这个改动要不要推到另一边",
// 而漏掉的那次不报错,表现是两个页面对同一个字段说不一样的话。
//
// 【旧表不删】prompt_templates / prompt_versions 都留着:
//   - 历史版本要能翻(那是"改坏了能退回去"的唯一依据)
//   - 合并出问题时还能回去看原始数据
// 只是从此不再从它读。
func (s *SQLiteStore) migMergePromptIntoTemplate() error {
	// 判定规则并到字段上
	// 【两种数据库的列定义必须一致】MySQL 的 TEXT 加不了 DEFAULT,存量行是 NULL;
	// 而我最初给 SQLite 写了 NOT NULL DEFAULT ''。结果是测试环境比生产环境严格 ——
	// 读 NULL 会炸的 bug 在测试里根本复现不出来,直接溜到运行环境才暴露
	// (整份模板加载失败 → 退回代码里那份 → 后台改的模板全部不生效)。
	// 统一成可空,读取一侧按可空处理。
	fieldCols := []assetColumnMigration{
		{"judge_mode", `VARCHAR(32)`, `TEXT`},
		{"judge_group", `VARCHAR(64)`, `TEXT`},
		{"yes_when", `TEXT`, `TEXT`},
		{"no_when", `TEXT`, `TEXT`},
		{"skip_when", `TEXT`, `TEXT`},
		{"judge_note", `TEXT`, `TEXT`},
	}
	if err := s.addColumns("report_template_fields", fieldCols); err != nil {
		return err
	}
	// 提示词的模板头并到模板上
	tplCols := []assetColumnMigration{
		{"scene", `TEXT`, `TEXT`},
		{"expected_photos", `TEXT`, `TEXT`},
		{"prompt_mode", `VARCHAR(32)`, `TEXT`},
		{"raw_text", `LONGTEXT`, `TEXT`},
	}
	if err := s.addColumns("report_templates", tplCols); err != nil {
		return err
	}
	return s.backfillPromptIntoTemplates()
}

// addColumns 逐列加,已存在就跳过。
//
// 【必须逐列判断】一次 ALTER 加多列在 SQLite 上不支持,而且升级可能中途失败 ——
// 重跑时已经加过的那几列会报错,把整条迁移卡死。
func (s *SQLiteStore) addColumns(table string, cols []assetColumnMigration) error {
	for _, c := range cols {
		exists, err := s.hasColumn(table, c.name)
		if err != nil {
			return fmt.Errorf("inspect %s.%s: %w", table, c.name, err)
		}
		if exists {
			continue
		}
		def := c.sqlite
		if s.dialect == "mysql" {
			def = c.mysql
		}
		if _, err := s.db.Exec(`ALTER TABLE ` + table + ` ADD COLUMN ` + c.name + ` ` + def); err != nil {
			return fmt.Errorf("add %s.%s: %w", table, c.name, err)
		}
	}
	return nil
}

// backfillPromptIntoTemplates 把 prompt_templates 里已有的判定规则搬到字段上。
//
// 【按 template_id + code 精确对应】不按顺序、不按标签 —— 顺序会变,标签会改,
// 只有 code 是稳定的。对不上的直接跳过:宁可少搬一条(提示词那边还留着原始数据),
// 也不能搬到错的字段上 —— 那会让 AI 拿着 A 字段的判定规则去判 B 字段,
// 而且不报错。
func (s *SQLiteStore) backfillPromptIntoTemplates() error {
	rows, err := s.db.Query(`SELECT id, data FROM prompt_templates`)
	if err != nil {
		// 提示词表还不存在(全新部署)—— 没有要搬的东西,不是错误
		return nil
	}
	type pending struct{ id, data string }
	var all []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.data); err != nil {
			rows.Close()
			return err
		}
		all = append(all, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, p := range all {
		var old struct {
			Scene          string   `json:"scene"`
			ExpectedPhotos []string `json:"expectedPhotos"`
			Mode           string   `json:"mode"`
			RawText        string   `json:"rawText"`
			Fields         []struct {
				Code     string `json:"code"`
				Group    string `json:"group"`
				Mode     string `json:"mode"`
				YesWhen  string `json:"yesWhen"`
				NoWhen   string `json:"noWhen"`
				SkipWhen string `json:"skipWhen"`
				Note     string `json:"note"`
			} `json:"fields"`
		}
		if err := json.Unmarshal([]byte(p.data), &old); err != nil {
			continue // 一条解不开不该卡住整条迁移
		}
		photos := ""
		if len(old.ExpectedPhotos) > 0 {
			if b, err := json.Marshal(old.ExpectedPhotos); err == nil {
				photos = string(b)
			}
		}
		if _, err := s.db.Exec(
			`UPDATE report_templates SET scene=?, expected_photos=?, prompt_mode=?, raw_text=? WHERE id=?`,
			old.Scene, photos, old.Mode, old.RawText, p.id); err != nil {
			return fmt.Errorf("backfill template %s: %w", p.id, err)
		}
		for _, f := range old.Fields {
			if strings.TrimSpace(f.Code) == "" {
				continue
			}
			if _, err := s.db.Exec(
				`UPDATE report_template_fields
				 SET judge_mode=?, judge_group=?, yes_when=?, no_when=?, skip_when=?, judge_note=?
				 WHERE template_id=? AND code=?`,
				f.Mode, f.Group, f.YesWhen, f.NoWhen, f.SkipWhen, f.Note, p.id, f.Code); err != nil {
				return fmt.Errorf("backfill field %s.%s: %w", p.id, f.Code, err)
			}
		}
	}
	return nil
}

// 025 — 巡检模板搬进数据库。
//
// 【为什么要搬】10 个模板 120 个字段写死在 templates.go 里,加一个模板、
// 改一个字段的中文标签,都要改代码重新部署。而这些是业务定义,不是程序逻辑。
//
// 【表建好不等于立刻生效】这一步只建表并把代码里那 10 个作为种子灌进去,
// 读取仍由 reportTemplates() 统一收口 —— 库里没有(或读失败)时回退到代码。
// 【模板列表绝不能为空】空列表 = 全系统建不了记录,所以回退是硬要求,
// 不是"顺手加的保险"。
//
// tenant_id 现在只占位:模板今天是全局的(代码里那份对所有租户一样),
// 要做成按租户隔离得把租户串进 reportTemplates(),而它有十几处调用 ——
// 那是另一次改动,不混在这一次里。
func (s *SQLiteStore) migReportTemplates() error {
	tplStmt := `CREATE TABLE IF NOT EXISTS report_templates (
		id          TEXT PRIMARY KEY,
		tenant_id   TEXT NOT NULL DEFAULT '` + defaultTenantID + `',
		name        TEXT NOT NULL DEFAULT '',
		project     TEXT NOT NULL DEFAULT '',
		asset_type  TEXT NOT NULL DEFAULT '',
		max_images  INTEGER NOT NULL DEFAULT 0,
		min_images  INTEGER NOT NULL DEFAULT 0,
		featured    INTEGER NOT NULL DEFAULT 0,
		has_ai      INTEGER NOT NULL DEFAULT 0,
		ai_prompt   TEXT NOT NULL DEFAULT '',
		disabled    INTEGER NOT NULL DEFAULT 0,
		sort_no     INTEGER NOT NULL DEFAULT 0,
		updated_at  TEXT NOT NULL DEFAULT ''
	)`
	fieldStmt := `CREATE TABLE IF NOT EXISTS report_template_fields (
		id          TEXT PRIMARY KEY,
		template_id TEXT NOT NULL DEFAULT '',
		code        TEXT NOT NULL DEFAULT '',
		label       TEXT NOT NULL DEFAULT '',
		kind        TEXT NOT NULL DEFAULT 'text',
		required    INTEGER NOT NULL DEFAULT 0,
		source      TEXT NOT NULL DEFAULT 'manual',
		options     TEXT,
		default_val TEXT NOT NULL DEFAULT '',
		manual_only INTEGER NOT NULL DEFAULT 0,
		sort_no     INTEGER NOT NULL DEFAULT 0
	)`
	if s.dialect == "mysql" {
		tplStmt = `CREATE TABLE IF NOT EXISTS report_templates (
			id          VARCHAR(64) PRIMARY KEY,
			tenant_id   VARCHAR(64) NOT NULL DEFAULT '` + defaultTenantID + `',
			name        VARCHAR(128) NOT NULL DEFAULT '',
			project     VARCHAR(128) NOT NULL DEFAULT '',
			asset_type  VARCHAR(128) NOT NULL DEFAULT '',
			max_images  INT NOT NULL DEFAULT 0,
			min_images  INT NOT NULL DEFAULT 0,
			featured    TINYINT NOT NULL DEFAULT 0,
			has_ai      TINYINT NOT NULL DEFAULT 0,
			ai_prompt   VARCHAR(64) NOT NULL DEFAULT '',
			disabled    TINYINT NOT NULL DEFAULT 0,
			sort_no     INT NOT NULL DEFAULT 0,
			updated_at  VARCHAR(40) NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
		fieldStmt = `CREATE TABLE IF NOT EXISTS report_template_fields (
			id          VARCHAR(64) PRIMARY KEY,
			template_id VARCHAR(64) NOT NULL DEFAULT '',
			code        VARCHAR(64) NOT NULL DEFAULT '',
			label       VARCHAR(128) NOT NULL DEFAULT '',
			kind        VARCHAR(32) NOT NULL DEFAULT 'text',
			required    TINYINT NOT NULL DEFAULT 0,
			source      VARCHAR(32) NOT NULL DEFAULT 'manual',
			options     TEXT,
			default_val VARCHAR(255) NOT NULL DEFAULT '',
			manual_only TINYINT NOT NULL DEFAULT 0,
			sort_no     INT NOT NULL DEFAULT 0,
			KEY idx_rtf_tpl (template_id, sort_no)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	}
	if _, err := s.db.Exec(tplStmt); err != nil {
		return fmt.Errorf("ensure report_templates: %w", err)
	}
	if _, err := s.db.Exec(fieldStmt); err != nil {
		return fmt.Errorf("ensure report_template_fields: %w", err)
	}
	if s.dialect != "mysql" {
		if _, err := s.db.Exec(
			`CREATE INDEX IF NOT EXISTS idx_rtf_tpl ON report_template_fields (template_id, sort_no)`,
		); err != nil {
			return fmt.Errorf("index report_template_fields: %w", err)
		}
	}
	return nil
}

// 024 — 提示词版本留痕。
//
// 【建索引是必须的,不是优化】列表页每次打开一个模板都按 template_id 查一次,
// 而这张表只增不删 —— 一年后全表扫描会拖慢一个"看起来只是打开个下拉框"的动作。
func (s *SQLiteStore) migPromptVersions() error {
	stmt := `CREATE TABLE IF NOT EXISTS prompt_versions (
		id TEXT PRIMARY KEY,
		template_id TEXT NOT NULL DEFAULT '',
		name TEXT NOT NULL DEFAULT '',
		mode TEXT NOT NULL DEFAULT '',
		data TEXT NOT NULL DEFAULT '',
		note TEXT NOT NULL DEFAULT '',
		author TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT ''
	)`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS prompt_versions (
			id VARCHAR(64) PRIMARY KEY,
			template_id VARCHAR(64) NOT NULL DEFAULT '',
			name VARCHAR(255) NOT NULL DEFAULT '',
			mode VARCHAR(32) NOT NULL DEFAULT '',
			data LONGTEXT NOT NULL,
			note VARCHAR(255) NOT NULL DEFAULT '',
			author VARCHAR(128) NOT NULL DEFAULT '',
			created_at VARCHAR(40) NOT NULL DEFAULT '',
			KEY idx_prompt_versions_tpl (template_id, created_at)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("ensure prompt_versions: %w", err)
	}
	if s.dialect != "mysql" {
		if _, err := s.db.Exec(
			`CREATE INDEX IF NOT EXISTS idx_prompt_versions_tpl ON prompt_versions (template_id, created_at)`,
		); err != nil {
			return fmt.Errorf("index prompt_versions: %w", err)
		}
	}
	return nil
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

// migUserDataScope — 013:users 加 data_scope(数据范围)。
//
// 在这之前,"能看到多少数据"是【写死在代码里】的两档:管理角色看全部、
// 巡检员看自己的。要接入多个项目组,就需要中间那一层("看本项目组的"),
// 而写死的判断没有地方挂。
//
// 【空值 = 按角色推导】这一步只把开关做出来,不改变任何现有行为:
// 存量用户这一列全是空,一律走原来的角色判断。要改某个人的范围时才填值。
// 分两步走的意义就在这里 —— 地基先铺好,行为一点不动,风险接近零。
func (s *SQLiteStore) migUserDataScope() error {
	exists, err := s.hasColumn("users", "data_scope")
	if err != nil {
		return fmt.Errorf("inspect users.data_scope: %w", err)
	}
	if exists {
		return nil
	}
	stmt := `ALTER TABLE users ADD COLUMN data_scope TEXT NOT NULL DEFAULT ''`
	if s.dialect == "mysql" {
		stmt = `ALTER TABLE users ADD COLUMN data_scope VARCHAR(24) NOT NULL DEFAULT ''`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("add users.data_scope: %w", err)
	}
	return nil
}

// migProjects — 014:项目实体化 + 人和项目的对应关系。
//
// 在这之前,"项目"只是 assets.project / records.project 里的一串中文名。
// 名字当键有两个后果:改个名全断,而且【没有地方挂人】—— 要让张三只看
// 会议中心,系统里根本没有"张三属于会议中心"这件事可以记。
//
// 表建了,但这一步【不改任何查询】:
//   - projects 只是把已有的项目名登记成实体,给成员关系当锚点
//   - user_projects 空着 = 没人被限定到项目 = 行为不变
//
// 过滤等第 2b 步接上,而且只对显式配了 project 范围的人生效。
// 和第 1 步同一个套路:先铺地基,不动行为。
//
// 【为什么仍然用项目名做关联键】assets / records / change_requests /
// engineering_tasks 全都用中文项目名互相认,一次性改成 project_id 要动
// 四张表和几十处查询,风险远大于收益。projects.name 在租户内唯一,
// 改名必须连带更新业务表 —— 所以这一步【不提供改名】,只提供登记和停用。
func (s *SQLiteStore) migProjects() error {
	stmt := `CREATE TABLE IF NOT EXISTS projects (
		id         TEXT PRIMARY KEY,
		tenant_id  TEXT NOT NULL DEFAULT '` + defaultTenantID + `',
		name       TEXT NOT NULL,
		code       TEXT NOT NULL DEFAULT '',
		note       TEXT NOT NULL DEFAULT '',
		disabled   INTEGER NOT NULL DEFAULT 0,
		created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '')`
	memberStmt := `CREATE TABLE IF NOT EXISTS user_projects (
		user_id    TEXT NOT NULL,
		project_id TEXT NOT NULL,
		created_at TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (user_id, project_id))`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS projects (
			id         VARCHAR(64)  NOT NULL PRIMARY KEY,
			tenant_id  VARCHAR(64)  NOT NULL DEFAULT '` + defaultTenantID + `',
			name       VARCHAR(191) NOT NULL,
			code       VARCHAR(64)  NOT NULL DEFAULT '',
			note       VARCHAR(255) NOT NULL DEFAULT '',
			disabled   TINYINT      NOT NULL DEFAULT 0,
			created_at VARCHAR(40)  NOT NULL DEFAULT '',
			updated_at VARCHAR(40)  NOT NULL DEFAULT '',
			UNIQUE KEY uniq_projects_tenant_name (tenant_id, name)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
		memberStmt = `CREATE TABLE IF NOT EXISTS user_projects (
			user_id    VARCHAR(64) NOT NULL,
			project_id VARCHAR(64) NOT NULL,
			created_at VARCHAR(40) NOT NULL DEFAULT '',
			PRIMARY KEY (user_id, project_id),
			INDEX idx_user_projects_project (project_id)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create projects: %w", err)
	}
	if _, err := s.db.Exec(memberStmt); err != nil {
		return fmt.Errorf("create user_projects: %w", err)
	}
	if s.dialect == "sqlite" {
		if _, err := s.db.Exec(
			`CREATE UNIQUE INDEX IF NOT EXISTS uniq_projects_tenant_name ON projects(tenant_id, name)`); err != nil {
			return fmt.Errorf("index projects: %w", err)
		}
		_, _ = s.db.Exec(`CREATE INDEX IF NOT EXISTS idx_user_projects_project ON user_projects(project_id)`)
	}
	return s.backfillProjectsFromAssets()
}

// backfillProjectsFromAssets 把台账和巡检记录里已经出现过的项目名登记成项目。
//
// 【必须回填】否则升级后后台项目列表是空的,管理员会以为项目丢了,
// 然后手动新建一个同名的 —— 名字要是差一个字,和台账就对不上了。
//
// 只登记不存在的;已登记的一律不动(管理员可能已经改过备注、停用过)。
func (s *SQLiteStore) backfillProjectsFromAssets() error {
	rows, err := s.db.Query(`
		SELECT DISTINCT project FROM assets WHERE project <> ''
		UNION
		SELECT DISTINCT project FROM records WHERE project <> ''`)
	if err != nil {
		return fmt.Errorf("scan existing projects: %w", err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if name = strings.TrimSpace(name); name != "" {
			names = append(names, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	now := nowStamp()
	for _, name := range names {
		// 租户:存量数据早于多租户,一律归默认租户 —— 和 assets/records 的
		// 兜底一致。真多租户之后新项目由接口带租户创建。
		var exists int
		if err := s.db.QueryRow(
			`SELECT COUNT(1) FROM projects WHERE tenant_id=? AND name=?`,
			defaultTenantID, name).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			continue
		}
		if _, err := s.db.Exec(
			`INSERT INTO projects (id, tenant_id, name, code, note, disabled, created_at, updated_at)
			 VALUES (?, ?, ?, ?, '', 0, ?, ?)`,
			newID("proj"), defaultTenantID, name, businessProjectCode(name), now, now); err != nil {
			return fmt.Errorf("backfill project %q: %w", name, err)
		}
	}
	return nil
}

// migOfflineShotAsset — 015:offline_shots 加 asset_id,记住这张照片拍的是哪台设备。
//
// 【为什么需要】在这之前照片是"无主"的:拍完丢进队列,到成单那一步才由
// 当前的扫码上下文决定归属。于是一次巡多台会串 ——
//
//	扫 FT-6 → 拍 3 张 → 走到隔壁扫 FT-7 → 拍 3 张 → 进"选照片"
//	6 张混在一起,而上下文是 FT-7,全选就是 6 张全落到 FT-7 上,FT-6 等于没巡
//
// 扫码之前这个问题被 AI 场景分类挡了一半(它至少会发现照片不是同一个场景),
// 而扫码流程跳过了分类 —— 安全网没了。所以照片必须在【拍的时候】就记住是哪台。
//
// 空值 = 没扫码拍的(手动路径),行为和以前一样。
func (s *SQLiteStore) migOfflineShotAsset() error {
	exists, err := s.hasColumn("offline_shots", "asset_id")
	if err != nil {
		return fmt.Errorf("inspect offline_shots.asset_id: %w", err)
	}
	if exists {
		return nil
	}
	stmt := `ALTER TABLE offline_shots ADD COLUMN asset_id TEXT NOT NULL DEFAULT ''`
	if s.dialect == "mysql" {
		stmt = `ALTER TABLE offline_shots ADD COLUMN asset_id VARCHAR(191) NOT NULL DEFAULT ''`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("add offline_shots.asset_id: %w", err)
	}
	return nil
}

// migTemplateFieldRules — 016:模板字段的必填/选填配置。
//
// 模板本身写死在 templates.go 里。调整某个字段填不填原来只能改代码重新部署,
// 而这是业务规则,不该每次都排一次上线。这张表只覆盖 required 这一项 ——
// 字段类型、选项、AI 提示词仍然来自代码,不做全量搬库(那是大改,风险不划算)。
//
// 表为空 = 全部按代码里的默认值,行为和加这个功能之前一样。
func (s *SQLiteStore) migTemplateFieldRules() error {
	stmt := `CREATE TABLE IF NOT EXISTS template_field_rules (
		template_id TEXT NOT NULL,
		field_code  TEXT NOT NULL,
		required    INTEGER NOT NULL DEFAULT 0,
		updated_at  TEXT NOT NULL DEFAULT '',
		updated_by  TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (template_id, field_code))`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS template_field_rules (
			template_id VARCHAR(64) NOT NULL,
			field_code  VARCHAR(64) NOT NULL,
			required    TINYINT     NOT NULL DEFAULT 0,
			updated_at  VARCHAR(40) NOT NULL DEFAULT '',
			updated_by  VARCHAR(64) NOT NULL DEFAULT '',
			PRIMARY KEY (template_id, field_code)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create template_field_rules: %w", err)
	}
	return nil
}

// migTemplateSettings — 017:模板级的设置(目前只有"每单最少几张照片")。
//
// 【为什么不塞进 016 那张表】那张是 (template_id, field_code) 两列主键的
// 字段级配置。硬塞的话得编一个假的 field_code(比如 "__min_images"),
// 之后每个读那张表的地方都要记得把它排除掉 —— 迟早有人忘掉一处,
// 那个假字段就会跑到界面上、跑进校验里。多一张表比多一个特例便宜。
//
// 表为空 = 全按模板代码里的默认值(现在是 5),行为不变。
func (s *SQLiteStore) migTemplateSettings() error {
	stmt := `CREATE TABLE IF NOT EXISTS template_settings (
		template_id TEXT PRIMARY KEY,
		min_images  INTEGER NOT NULL DEFAULT 0,
		updated_at  TEXT NOT NULL DEFAULT '',
		updated_by  TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS template_settings (
			template_id VARCHAR(64) NOT NULL PRIMARY KEY,
			min_images  INT         NOT NULL DEFAULT 0,
			updated_at  VARCHAR(40) NOT NULL DEFAULT '',
			updated_by  VARCHAR(64) NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create template_settings: %w", err)
	}
	return nil
}

// migPlanType — 018:计划分类型 + 每日计划的执行日。
//
// 原来只有一个自由文本的「周期」字段(CycleText),人写什么都行 ——
// "每周一次""每月""季度" 各种写法混在一起,程序没法据此算出"今天该巡什么"。
//
// 现在分成五类:年度 / 月度 / 周 / 每日 / 临时(对外部项目组的对接计划)。
// 【刻意不做逐级分解】年度拆月度、月度拆周那一套要父子关系和分解界面,
// 而实际需要的只是"这条属于哪一档"。等真的要看分解关系时再说。
//
// weekdays 只对每日计划有意义:一周哪几天执行。存 "1,2,3,4,5" 这样的串
// (1=周一 … 7=周日)。空 = 每天。
//
// 空的 plan_type 一律当「临时」处理 —— 存量数据没有这个字段,
// 而它们本来就是零散录进来的。
func (s *SQLiteStore) migPlanType() error {
	for _, c := range []struct{ name, sqlite, mysql string }{
		{"plan_type", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(24) NOT NULL DEFAULT ''`},
		{"weekdays", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(32) NOT NULL DEFAULT ''`},
	} {
		exists, err := s.hasColumn("engineering_plan_items", c.name)
		if err != nil {
			return fmt.Errorf("inspect engineering_plan_items.%s: %w", c.name, err)
		}
		if exists {
			continue
		}
		def := c.sqlite
		if s.dialect == "mysql" {
			def = c.mysql
		}
		if _, err := s.db.Exec(
			`ALTER TABLE engineering_plan_items ADD COLUMN ` + c.name + ` ` + def); err != nil {
			return fmt.Errorf("add engineering_plan_items.%s: %w", c.name, err)
		}
	}
	return nil
}

// migAppSettings — 019:通用设置表(键值对)。
//
// 目前只放两样:每日提醒的推送时间、开关。做成键值对而不是给每个设置加一列,
// 是因为这类"运营参数"会一直长 —— 每加一个就改一次表结构,
// 而它们之间没有任何关系,不值得各占一列。
//
// 【不放密钥】密钥走 secrets 文件,不进这张表 —— 这张表管理员在后台看得到。
func (s *SQLiteStore) migAppSettings() error {
	stmt := `CREATE TABLE IF NOT EXISTS app_settings (
		k          TEXT PRIMARY KEY,
		v          TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '',
		updated_by TEXT NOT NULL DEFAULT '')`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS app_settings (
			k          VARCHAR(64)  NOT NULL PRIMARY KEY,
			v          VARCHAR(255) NOT NULL DEFAULT '',
			updated_at VARCHAR(40)  NOT NULL DEFAULT '',
			updated_by VARCHAR(64)  NOT NULL DEFAULT ''
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create app_settings: %w", err)
	}
	return nil
}

// migPlanAssets — 020:每日计划要巡哪些设备。
//
// 【没有这一列就算不出完成率】"完成"的判定是自动的:这些设备今天有没有巡检
// 快照。前提是计划得说清"这些"是哪些 —— 只写一句"巡查配电房"程序没法判。
//
// 存 JSON 数组而不是逗号分隔:资产 ID 形如「会议中心::elevator_no_room::KT-7」,
// 项目名里出现逗号虽然少见但不是不可能,一旦出现整份清单就被切错,
// 而且错得很安静(少几台设备,完成率反而更好看)。
func (s *SQLiteStore) migPlanAssets() error {
	exists, err := s.hasColumn("engineering_plan_items", "asset_ids_json")
	if err != nil {
		return fmt.Errorf("inspect engineering_plan_items.asset_ids_json: %w", err)
	}
	if exists {
		return nil
	}
	def := `TEXT NOT NULL DEFAULT '[]'`
	if s.dialect == "mysql" {
		def = `TEXT`
	}
	if _, err := s.db.Exec(
		`ALTER TABLE engineering_plan_items ADD COLUMN asset_ids_json ` + def); err != nil {
		return fmt.Errorf("add engineering_plan_items.asset_ids_json: %w", err)
	}
	return nil
}

// migPlanOwnerID — 021:计划负责人绑到账号。
//
// 【为什么不直接把 owner_name 换成 owner_id】外委班组的人没有账号。
// 换掉的话这些计划要么录不进来,要么被迫挂到一个不相干的账号上。
// 两列并存:对得上账号的存 ID,对不上的只留名字照常显示。
//
// 【这次迁移【不】回填任何数据】按名字模糊匹配去猜谁是谁,重名和改过名的
// 会被静默绑错,而"绑错人"的表现是提醒发给了不该发的人 —— 得等有人抱怨
// 才发现,那时已经错了很多天。存量数据的对应关系走一个只读报告,
// 人看过之后按精确 ID 更新(见 handleOwnerBindingReport)。
func (s *SQLiteStore) migPlanOwnerID() error {
	exists, err := s.hasColumn("engineering_plan_items", "owner_id")
	if err != nil {
		return fmt.Errorf("inspect engineering_plan_items.owner_id: %w", err)
	}
	if exists {
		return nil
	}
	// 【MySQL 不给 TEXT 加 DEFAULT】所以两个方言的列定义不一样。
	// 这里用 VARCHAR:owner_id 是定长的账号 ID,而且以后要按它建索引,
	// TEXT 在 MySQL 上建索引还得指定前缀长度。
	def := `TEXT NOT NULL DEFAULT ''`
	if s.dialect == "mysql" {
		def = `VARCHAR(64) NOT NULL DEFAULT ''`
	}
	if _, err := s.db.Exec(
		`ALTER TABLE engineering_plan_items ADD COLUMN owner_id ` + def); err != nil {
		return fmt.Errorf("add engineering_plan_items.owner_id: %w", err)
	}
	return nil
}

// migPushLog — 022:推送发过什么的流水。
//
// 【这张表存在的唯一理由是去重】容器 16:59 重启,17:00 定时器重新起算,
// 群里就会收到第二遍;崩溃循环时是十遍。而这功能的可信度一次就毁了 ——
// 一旦大家觉得"这机器人乱发",以后真发对了也没人看。
//
// UNIQUE(tenant_id, kind, day):同一个租户、同一类推送、同一天,只允许一条。
// 发之前【先插占位】—— 插入冲突就说明别人已经发过了。反过来"先发再记"的话,
// 发成功但记失败,下次照样重发。
//
// status 记结果而不是只记"发过":发失败的要能重试,而"发失败"和"没发过"
// 在界面上必须分得清 —— 前者要人去看企微配置,后者只是还没到点。
func (s *SQLiteStore) migPushLog() error {
	stmt := `CREATE TABLE IF NOT EXISTS push_log (
		id         TEXT PRIMARY KEY,
		tenant_id  TEXT NOT NULL DEFAULT '` + defaultTenantID + `',
		kind       TEXT NOT NULL DEFAULT '',
		day        TEXT NOT NULL DEFAULT '',
		status     TEXT NOT NULL DEFAULT '',
		detail     TEXT NOT NULL DEFAULT '',
		created_at TEXT NOT NULL DEFAULT '',
		UNIQUE(tenant_id, kind, day))`
	if s.dialect == "mysql" {
		stmt = `CREATE TABLE IF NOT EXISTS push_log (
			id         VARCHAR(64)  NOT NULL PRIMARY KEY,
			tenant_id  VARCHAR(64)  NOT NULL DEFAULT 'tenant_default',
			kind       VARCHAR(32)  NOT NULL DEFAULT '',
			day        VARCHAR(16)  NOT NULL DEFAULT '',
			status     VARCHAR(24)  NOT NULL DEFAULT '',
			detail     TEXT,
			created_at VARCHAR(40)  NOT NULL DEFAULT '',
			UNIQUE KEY uk_push_once (tenant_id, kind, day)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci`
	}
	if _, err := s.db.Exec(stmt); err != nil {
		return fmt.Errorf("create push_log: %w", err)
	}
	return nil
}

// migAssetProfileFields — 023:设备的静态档案字段。
//
// 【为什么要有它们:趋势只有相对值,没有绝对判断】
// 供水压力 0.55 MPa 是低还是正常?看曲线只知道"比平时低了 8%",
// 而"是不是已经低到该报修"要对着设计值才说得出来。
// 同理:一台 2011 年投运的电梯和 2024 年的,同样的读数含义完全不同。
//
// 【维保周期是唯一一个直接参与判断的】其余几项是给人看的背景,
// 而它能算出"距上次维保已 380 天,超过 365 天的周期" ——
// 这句话不需要人懂设备也能行动。
//
// 全部可空:这批数据要向甲方索要,拿到多少填多少,
// 没填的字段在界面上直接不显示,不摆一行"—"占位。
func (s *SQLiteStore) migAssetProfileFields() error {
	type col struct{ name, sqlite, mysql string }
	cols := []col{
		{"manufacturer", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(128) NOT NULL DEFAULT ''`},
		{"model", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(128) NOT NULL DEFAULT ''`},
		// 投运日期 / 上次维保:存 YYYY-MM-DD 字符串,和计划那边的日期口径一致
		{"commissioned_at", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(16) NOT NULL DEFAULT ''`},
		{"last_maintained_at", `TEXT NOT NULL DEFAULT ''`, `VARCHAR(16) NOT NULL DEFAULT ''`},
		// 维保周期(天)。0 = 没定 —— 【不能当成"不用维保"】,
		// 只是还没填,所以判断那边遇到 0 直接跳过,不报"已超期"
		{"maintenance_cycle_days", `INTEGER NOT NULL DEFAULT 0`, `INT NOT NULL DEFAULT 0`},
		{"asset_note", `TEXT NOT NULL DEFAULT ''`, `TEXT`},
	}
	for _, c := range cols {
		exists, err := s.hasColumn("assets", c.name)
		if err != nil {
			return fmt.Errorf("inspect assets.%s: %w", c.name, err)
		}
		if exists {
			continue
		}
		def := c.sqlite
		if s.dialect == "mysql" {
			def = c.mysql
		}
		if _, err := s.db.Exec(`ALTER TABLE assets ADD COLUMN ` + c.name + ` ` + def); err != nil {
			return fmt.Errorf("add assets.%s: %w", c.name, err)
		}
	}
	return nil
}
