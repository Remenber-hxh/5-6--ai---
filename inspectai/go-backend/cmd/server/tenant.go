package main

// ===== 多租户地基 =====
//
// 隔离策略:共享 schema + tenant_id 行级(见 docs/tenant-and-auth-design.md)。
// 本文件是租户域的落脚点,随 Phase 0 推进逐步补充 Tenant 类型与 Store 方法。

const (
	// 默认租户 = 璟邑科技。多租户改造前的全部存量数据(资产/记录/用户…)
	// 由 migration 009 回填到这个租户,现有单租户行为保持不变。
	defaultTenantID   = "tenant_default"
	defaultTenantName = "璟邑科技"
	defaultTenantCode = "jadeast"
)
