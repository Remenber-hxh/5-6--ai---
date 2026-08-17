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
// 【按 assetType 取,以及这里埋着的一个坑】
// 当前 10 个模板对 10 种 assetType,是一一对应的(有机房电梯 / 无机房电梯是
// 两种类型、两批实体设备),所以按 assetType 取 == 按 templateId 取。
//
// 但提交时资产的主键是 assetIDFor():
//
//	project + "::" + templateID + "::" + asset_no
//
// 【带 templateID】。所以一旦将来出现"两个模板共用同一个 assetType",这里就会
// 把另一个模板的设备编号也列进选项,巡检员选中后提交,算出来的 assetID 与那台
// 设备不同 —— 台账里会凭空多出一台重复设备,而不是挂到原来那台上。
// 真到那一天,这里要改成按 templateID 取,或者把 templateID 从 assetID 里去掉。
// asset_no_test.go 里有一条测试守着"1:1"这个前提,破了会红。
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
	// 已经填了的值必须留在选项里。
	//
	// 两种情况会让它不在台账候选里:一是新设备(台账里还没有,巡检员手填的),
	// 二是这条记录建好后那台设备被改名或删了。这时如果不并进去,巡检员打开
	// 下拉会发现自己填的那个编号不在列表里 —— 想确认一下都选不回来,
	// 只能改成别的。宁可多一个选项,不能让人丢掉已经填对的值。
	if cur := strings.TrimSpace(rec.Fields[idx].Value); cur != "" && !seen[cur] {
		opts = append(opts, cur)
	}

	if len(opts) == 0 {
		return
	}
	sort.Strings(opts)
	rec.Fields[idx].Options = opts
	rec.Fields[idx].Kind = "choice"
}
