package main

import "strings"

// ===== 资产身份 =====
//
// 资产的主键是 assetIDFor() 拼出来的:
//     项目 :: 模板ID :: 设备编号
//
// 三段里有两段会"变"，这就是线上台账被拆成一堆重复条目的根源。已经踩到的
// 两种裂法，都记在这里，别再各自为战地补：
//
// 【裂法一:手工建档没有模板】
// 后台的新建资产表单只问项目/编号/名称/类型，【没有模板这一项】，所以
// req.TemplateID 永远是空，handleCreateAsset 就填了个 "manual"：
//     会议中心::manual::K09
// 这台设备真被巡检时，记录算出的是：
//     会议中心::elevator_machine_room::K09
// 两条。手工建的资产【只要被巡检就必然重复】——线上"新建的大多数都重了"
// 就是这么来的。
// 修法:表单里的"设备类型"和模板是一一对应的(有 TestAssetTypeMapsToSingleTemplate
// 守着)，从类型反推模板即可，不必让用户多填一项。
//
// 【裂法二:改名不改身份】
// 后台改设备名只更新 asset_name，不动 asset_key 和 id。AI 当初把铭牌读成
// "K6"，有人在后台改成了正确的 "K06"——于是"姓名"是 K06、"身份证号"还是 K6。
// 移动端下拉列的是姓名，巡检员选 K06 → 算出 ...::K06 → 库里没有 → 又新建一台。
// 修法(产品定的方案 C):身份不动，提交时按姓名回查。见 resolveAssetIdentity。

// templateIDForAssetType 由设备类型反推模板 ID。
// 一个 assetType 只属于一个模板(TestAssetTypeMapsToSingleTemplate 守着这个
// 前提)，所以这个反推是确定的。查不到返回空，由调用方决定怎么兜底。
func templateIDForAssetType(assetType string) string {
	at := strings.TrimSpace(assetType)
	if at == "" {
		return ""
	}
	for _, tpl := range reportTemplates() {
		if strings.EqualFold(strings.TrimSpace(tpl.AssetType), at) {
			return tpl.ID
		}
	}
	return ""
}

// resolveAssetIdentity 在建立"这条记录属于哪台设备"时，优先复用已有资产。
//
// 【为什么要有这一步】
// 直接按 项目::模板::编号 算 ID，只要编号和当初建档时不是同一个字符串，就会
// 凭空多出一台设备。而编号恰恰是最容易不一致的东西：AI 读错过、后台改过名、
// 手工建档时模板段是 "manual"。
//
// 【匹配顺序】从最可信到最宽松，命中即停：
//  1. ID 完全一致           —— 正常情况
//  2. 同项目 + 同模板 + 名字或编号相同  —— 改名造成的分家(裂法二)
//  3. 同项目 + 模板是 manual + 名字或编号相同 —— 手工建档遗留(裂法一)
//
// 【为什么不跨项目匹配】不同项目下的设备编号重名很常见(每个楼都有 K01)，
// 跨项目合并会把两台真设备并成一台，比多出一台还糟。
//
// candidates 传全租户资产列表(调用方已经查过就别重复查)。找不到返回空串，
// 调用方继续用算出来的新 ID —— 那是"这确实是台新设备"的正常路径。
func resolveAssetIdentity(candidates []*AssetEntry, project, templateID, assetNo string) string {
	want := strings.TrimSpace(assetNo)
	if want == "" {
		return ""
	}
	proj := strings.TrimSpace(project)
	tpl := strings.TrimSpace(templateID)
	exact := project + "::" + templateID + "::" + sanitizeAssetIdent(assetNo)

	var byTemplate, byManual string
	for _, a := range candidates {
		if a == nil || strings.TrimSpace(a.Project) != proj {
			continue
		}
		if a.ID == exact {
			return a.ID // 1. 完全命中,不用再看
		}
		// 名字或编号任一对上都算同一台:改名后 asset_name 是新的、asset_key 是旧的,
		// 巡检员可能报出其中任意一个。
		hit := strings.EqualFold(strings.TrimSpace(a.AssetName), want) ||
			strings.EqualFold(strings.TrimSpace(a.AssetKey), want)
		if !hit {
			continue
		}
		switch {
		case tpl != "" && strings.TrimSpace(a.TemplateID) == tpl && byTemplate == "":
			byTemplate = a.ID // 2. 同模板
		case assetIDTemplatePart(a.ID) == "manual" && byManual == "":
			byManual = a.ID // 3. 手工建档遗留
		}
	}
	if byTemplate != "" {
		return byTemplate
	}
	return byManual
}

// assetIDTemplatePart 取出资产 ID 的模板段。
// ID 形如 项目::模板::编号，而【项目名和编号里都可能含 "::"】,所以既不能
// 简单 Split 也不能取 [1] —— 取第一个和最后一个分隔符之间的部分。
func assetIDTemplatePart(id string) string {
	first := strings.Index(id, "::")
	last := strings.LastIndex(id, "::")
	if first < 0 || last <= first {
		return ""
	}
	return id[first+2 : last]
}

// reuseExistingAssetIdentity 把待写入资产的 ID 换成台账里已有的那台(如果找得到)。
//
// 就地改 assets 里每个元素的 ID/AssetKey/TemplateID。快照的 asset_id 是在这之后
// 由 buildRecordObservations 拿 asset.ID 生成的,所以顺序不能反 —— 先换 ID
// 再造快照,否则快照会挂到那个不存在的新 ID 上,详情页的历史就是空的。
//
// 查一次全量资产给所有待写资产共用:一条记录最多产出 6 台(能耗表组),
// 逐台查库是 6 次往返,没必要。
func (s *Server) reuseExistingAssetIdentity(tenantID string, assets []*AssetEntry) {
	if len(assets) == 0 {
		return
	}
	all, err := s.store.ListAssets(tenantID)
	if err != nil {
		return // 查不到就按原样走:宁可多建一台,也不能让提交失败
	}
	for _, a := range assets {
		if a == nil {
			continue
		}
		// 用 AssetKey 而不是 AssetName 去找:AssetKey 才是从记录字段里取出来的
		// 那个"巡检员填/选的编号"。
		if id := resolveAssetIdentity(all, a.Project, a.TemplateID, a.AssetKey); id != "" && id != a.ID {
			a.ID = id
			// 模板段可能不同(挂到 manual:: 的遗留资产上),同步过去,
			// 免得 upsert 把已有那台的 template_id 覆盖成新算的值。
			if tplPart := assetIDTemplatePart(id); tplPart != "" {
				a.TemplateID = tplPart
			}
		}
	}
}

// assetBackfillMaxRecords 启动回填一次扫多少条记录。
//
// 【为什么要有这么个数,而不是"全部"】ListRecords 的 limit<=0 会被当成默认
// 100 条,所以不能靠传 0 表达"不限"。而真正无上限地把记录全读进内存,记录量
// 涨上去之后会变成启动时的一次内存尖峰。
//
// 10 万条对这个项目远超实际量级(线上目前 600 条),同时把内存占用限在可控
// 范围。撞到上限会打 WARN —— 那说明该把回填改成分页扫描了。
const assetBackfillMaxRecords = 100000
