package main

import (
	"sort"
	"strings"
)

// fillAssetNoOptions 给设备编号字段填上可选项 —— 台账里同类设备已有的编号。
//
// 【为什么要有】
// 原来两个电梯模板给 asset_no 写死了默认值 "HYZX-WJ-DT01"。asset_no 是资产
// 台账的主键(buildAsset 拿它当资产名),于是识别失败的记录全挂到同一个编号
// 下,不同的电梯在台账里被并成一台;而且默认值会被标成 manual/已确认,
// 看起来像人核对过。现在改成人工从台账里选。
//
// 【为什么按 assetType 而不是 templateId 取】
// 同一类设备可能有多个模板(有机房/无机房电梯),但它们是同一批实体设备。
// 按 templateId 取会让无机房模板看不到有机房那批,反而选不到自己的设备。
//
// 【为什么允许选项为空】
// 新点位第一次巡检时台账里还没有同类设备。前端 FieldRow 的规则是
// "有 options 渲染下拉、没有渲染输入框",空选项自然回落成手填。
// 不能为了做成下拉就把新设备挡在门外。
func (s *Server) fillAssetNoOptions(rec *Record) {
	if rec == nil {
		return
	}
	idx := -1
	for i := range rec.Fields {
		if rec.Fields[i].Code == "asset_no" {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}

	tpl, ok := templateByID(rec.TemplateID)
	if !ok || strings.TrimSpace(tpl.AssetType) == "" {
		return
	}
	assets, err := s.store.ListAssets(rec.TenantID)
	if err != nil {
		return // 取不到就保持输入框,不让建记录失败
	}

	seen := map[string]bool{}
	opts := make([]string, 0, 16)
	for _, a := range assets {
		if a == nil || a.AssetType != tpl.AssetType {
			continue
		}
		// 同项目优先:跨项目的设备编号可能重名,混在一起选错了很难查
		if rec.Project != "" && a.Project != "" && a.Project != rec.Project {
			continue
		}
		name := strings.TrimSpace(a.AssetName)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		opts = append(opts, name)
	}
	if len(opts) == 0 {
		return
	}
	sort.Strings(opts)
	rec.Fields[idx].Options = opts
	rec.Fields[idx].Kind = "choice"
}
