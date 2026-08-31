package main

import (
	"database/sql"
	"strings"
)

// UpdateAssetProfile 只改传进来的那几个字段。
//
// 【动态拼 SET,不是把六个字段全写一遍】全写的话,前端只想改维保周期,
// 提交里没带的厂家会被写成空 —— 而且不报错。这和计划页那个
// "编辑一次清空一批字段"是同一类事故,那次是整行覆盖,这次要提前避开。
func (s *SQLiteStore) UpdateAssetProfile(tenantID, id string, p AssetProfilePatch) (*AssetEntry, error) {
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		sets = append(sets, col+"=?")
		args = append(args, v)
	}
	if p.Manufacturer != nil {
		add("manufacturer", strings.TrimSpace(*p.Manufacturer))
	}
	if p.Model != nil {
		add("model", strings.TrimSpace(*p.Model))
	}
	if p.CommissionedAt != nil {
		add("commissioned_at", strings.TrimSpace(*p.CommissionedAt))
	}
	if p.LastMaintainedAt != nil {
		add("last_maintained_at", strings.TrimSpace(*p.LastMaintainedAt))
	}
	if p.MaintenanceCycleDays != nil {
		add("maintenance_cycle_days", *p.MaintenanceCycleDays)
	}
	if p.AssetNote != nil {
		add("asset_note", strings.TrimSpace(*p.AssetNote))
	}
	if len(sets) == 0 {
		// 一个字段都没传:不是错误,直接把当前值读回去
		return getAssetByID(s.db, id)
	}
	add("updated_at", nowStamp())

	args = append(args, id, tenantOrDefault(tenantID))
	q := `UPDATE assets SET ` + strings.Join(sets, ", ") + ` WHERE id=? AND tenant_id=?`
	res, err := s.db.Exec(q, args...)
	if err != nil {
		return nil, err
	}
	// 【影响 0 行要报错】不报的话改一个不存在的 id 会返回成功,
	// 界面提示"已保存",而什么都没变。
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return getAssetByID(s.db, id)
}

func (s *MemStore) UpdateAssetProfile(tenantID, id string, p AssetProfilePatch) (*AssetEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.assets[id]
	if !ok || tenantOrDefault(a.TenantID) != tenantOrDefault(tenantID) {
		return nil, sql.ErrNoRows
	}
	if p.Manufacturer != nil {
		a.Manufacturer = strings.TrimSpace(*p.Manufacturer)
	}
	if p.Model != nil {
		a.Model = strings.TrimSpace(*p.Model)
	}
	if p.CommissionedAt != nil {
		a.CommissionedAt = strings.TrimSpace(*p.CommissionedAt)
	}
	if p.LastMaintainedAt != nil {
		a.LastMaintainedAt = strings.TrimSpace(*p.LastMaintainedAt)
	}
	if p.MaintenanceCycleDays != nil {
		a.MaintenanceCycleDays = *p.MaintenanceCycleDays
	}
	if p.AssetNote != nil {
		a.AssetNote = strings.TrimSpace(*p.AssetNote)
	}
	cp := *a
	return &cp, nil
}
