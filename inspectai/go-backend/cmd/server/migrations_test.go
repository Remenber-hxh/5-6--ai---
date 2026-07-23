package main

import (
	"path/filepath"
	"testing"
)

// migration 008:tenants 表建成、默认租户种入、迁移记账、重开幂等。
// 用纯 Go 的 sqlite 驱动跑真迁移(不依赖 Docker/MySQL)。
func TestMigrationTenants(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore(首次): %v", err)
	}

	// 1) 迁移 008 已记账
	var version int
	if err := store.db.QueryRow(
		`SELECT version FROM schema_migrations WHERE version = 8`).Scan(&version); err != nil {
		t.Fatalf("008 未记账: %v", err)
	}

	// 2) 默认租户存在且字段正确
	var name, code, status string
	if err := store.db.QueryRow(
		`SELECT name, code, status FROM tenants WHERE id = ?`, defaultTenantID,
	).Scan(&name, &code, &status); err != nil {
		t.Fatalf("默认租户不存在: %v", err)
	}
	if name != defaultTenantName || code != defaultTenantCode || status != "active" {
		t.Errorf("默认租户字段异常: name=%q code=%q status=%q", name, code, status)
	}

	// 3) 只有一行(没重复种)
	var cnt int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&cnt); err != nil {
		t.Fatalf("count tenants: %v", err)
	}
	if cnt != 1 {
		t.Errorf("tenants 行数 = %d, 期望 1", cnt)
	}
	store.db.Close()

	// 4) 幂等:同一个库再开一次(迁移会因已记账而跳过),仍是一行、无报错
	store2, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore(重开): %v", err)
	}
	defer store2.db.Close()
	if err := store2.db.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&cnt); err != nil {
		t.Fatalf("重开后 count tenants: %v", err)
	}
	if cnt != 1 {
		t.Errorf("重开后 tenants 行数 = %d, 期望 1(幂等)", cnt)
	}
}

// migration 009:14 张业务表都加上 tenant_id;插入不带 tenant_id 的行
// 会落到默认租户(证明默认值回填机制 = 存量回填与过渡期安全网同一保障)。
func TestMigrationTenantColumns(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "mig9.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.db.Close()

	tables := []string{
		"users", "departments", "assets", "asset_snapshots", "records",
		"change_requests", "engineering_tasks", "engineering_plan_items",
		"operation_logs", "prompt_templates", "ai_tasks",
		"management_ai_reports", "field_observations", "field_confirm_logs",
	}

	// 1) 每张表都有 tenant_id 列(缺列则该查询直接报错)
	for _, tbl := range tables {
		if _, err := store.db.Exec(`SELECT tenant_id FROM ` + tbl + ` LIMIT 0`); err != nil {
			t.Errorf("%s 缺 tenant_id 列: %v", tbl, err)
		}
	}

	// 2) roles/role_permissions 刻意未加(内置角色需全局共享,单独处理)
	for _, tbl := range []string{"roles", "role_permissions"} {
		if _, err := store.db.Exec(`SELECT tenant_id FROM ` + tbl + ` LIMIT 0`); err == nil {
			t.Errorf("%s 不应在 009 加 tenant_id(应留待专门处理)", tbl)
		}
	}

	// 3) 插入不带 tenant_id 的行 → 默认落到默认租户
	if _, err := store.db.Exec(
		`INSERT INTO departments (id, name, created_at) VALUES ('dept_probe', '探针部门', ?)`,
		nowStamp(),
	); err != nil {
		t.Fatalf("插入部门: %v", err)
	}
	var tid string
	if err := store.db.QueryRow(
		`SELECT tenant_id FROM departments WHERE id = 'dept_probe'`).Scan(&tid); err != nil {
		t.Fatalf("读回 tenant_id: %v", err)
	}
	if tid != defaultTenantID {
		t.Errorf("漏设 tenant_id 的新行 = %q, 期望默认租户 %q", tid, defaultTenantID)
	}
}
