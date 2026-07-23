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
