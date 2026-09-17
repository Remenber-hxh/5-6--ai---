package main

import (
	"strings"
	"time"
)

// ===== 写台账时,抄表记录的每一格"是哪台表" =====
//
// buildAssets 是纯函数(不查库),十几处在用。但抄表记录要写对台账,有两件事
// 必须查库:
//
//  1. 每一格的候选设备和默认设备(fillReadingAssetOptions,按台账现算)——
//     有候选却没选的格子不写台账,否则会按模板默认名字新建假设备。
//  2. 选中的设备名 → 台账里那台真设备的 ID。
//     【不能拿名字直接算 ID】ID 是 项目::模板::编号,而编号和名称不一定一样:
//     线上 Z1 的编号是「Z1」,名称是「Z1能耗表」。拿名字算出来的 ID 对不上,
//     按编号回查(reuseExistingAssetIdentity)也对不上 —— 又是一台新设备。
//
// 【只给会新建设备的两条路用】提交(handleSubmitRecord)和启动回填
// (ensureAssetLedgerFromRecords)。两条路规则必须一样:提交时不建的假设备,
// 回填时要是照建,每次重启都会把它补回来。
func (s *Server) buildLedgerAssets(rec *Record, at time.Time) []*AssetEntry {
	if rec == nil {
		return nil
	}
	// 【在副本上算】候选设备不落库(台账会变,存下来的候选会过期),
	// 直接往 rec 上填的话,提交时会一起写进记录。
	probe := *rec
	probe.Fields = append([]FieldValue(nil), rec.Fields...)
	s.fillReadingAssetOptions(&probe)
	assets := buildAssets(&probe, at)

	if !templateHasReadingAssets(rec.TemplateID) {
		return assets
	}
	ledger, err := s.store.ListAssets(rec.TenantID)
	if err != nil {
		return assets // 查不到就按原样走,后面还有 reuseExistingAssetIdentity 兜一层
	}
	for _, a := range assets {
		hit := findLedgerAssetByName(ledger, a.Project, a.AssetType, a.AssetName)
		if hit == nil {
			continue
		}
		a.ID = hit.ID
		a.AssetKey = hit.AssetKey
		if tp := assetIDTemplatePart(hit.ID); tp != "" {
			a.TemplateID = tp
		}
	}
	return assets
}

// templateHasReadingAssets 这个模板有没有"每一格是哪台设备"的字段(抄表类)。
func templateHasReadingAssets(templateID string) bool {
	tpl, ok := templateByID(templateID)
	if !ok {
		return false
	}
	for _, f := range tpl.Fields {
		if strings.TrimSpace(f.AssetType) != "" {
			return true
		}
	}
	return false
}

// findLedgerAssetByName 同项目、同设备类型、名字一字不差的那台设备。
//
// 【为什么要同类型】候选就是按类型从台账取的,所以选中的名字一定属于这个类型。
// 不限类型的话,同项目里一台叫「消防水表」的水表和一个叫「消防水表」的巡检点
// 会被当成同一台。
// 【有两台同名同类型就不认】猜一个比不认更糟:读数记到另一台上,不报错。
func findLedgerAssetByName(ledger []*AssetEntry, project, assetType, name string) *AssetEntry {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	var hit *AssetEntry
	for _, a := range ledger {
		if a == nil ||
			strings.TrimSpace(a.Project) != strings.TrimSpace(project) ||
			strings.TrimSpace(a.AssetType) != strings.TrimSpace(assetType) ||
			strings.TrimSpace(a.AssetName) != name {
			continue
		}
		if hit != nil {
			return nil // 同名同类型不止一台,不猜
		}
		hit = a
	}
	return hit
}
