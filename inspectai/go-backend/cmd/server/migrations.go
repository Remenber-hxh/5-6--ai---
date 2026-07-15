package main

import (
	"fmt"
	"strings"
	"time"
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
			m.Version, m.Name, time.Now().Format(time.RFC3339),
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
