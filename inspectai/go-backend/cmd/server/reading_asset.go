package main

import (
	"slices"
	"sort"
	"strings"
)

// ===== 读数属于哪台设备 =====
//
// 【要解决的是抄表的错位】一条抄表记录抄四块电表加两块水表,而照片上没有
// Z1/Z2/Z3/Z4 任何标识。模型只能按上传顺序猜 —— 实测中间夹一张读不出的,
// 后面就整体错位一格:读数一个不差,全填错了格子,而且哪儿都不报错。
//
// 所以每个读数行带一个设备选择器:默认按字段名对上台账里那台,
// 现场发现错位就点开改。默认值只是默认,人说了算。
//
// 【为什么不用 asset_no 那套】asset_no 是「这条记录巡的是哪台设备」,
// 一条记录一台。抄表是一条记录六台,塞不进一个格子。

// fillReadingAssetOptions 给配了 AssetType 的字段填上设备候选和默认设备。
//
// 【候选每次现算,不落库】台账会增删改,存下来的候选过几天就和台账对不上,
// 而界面上看不出它已经过期 —— 人会以为"这台设备没建",其实是候选是旧的。
func (s *Server) fillReadingAssetOptions(rec *Record) {
	if rec == nil {
		return
	}
	tpl, ok := templateByID(rec.TemplateID)
	if !ok {
		return
	}
	assetTypeOf := map[string]string{}
	for _, f := range tpl.Fields {
		if at := strings.TrimSpace(f.AssetType); at != "" {
			assetTypeOf[f.Code] = at
		}
	}
	if len(assetTypeOf) == 0 {
		return
	}

	assets, err := s.store.ListAssets(rec.TenantID)
	if err != nil {
		return // 取不到台账就不给选项,记录照常能建 —— 选设备是加分项
	}

	// 按设备类型归拢候选,同项目优先(跨项目重名选错了很难查)
	byType := map[string][]string{}
	for _, a := range assets {
		if a == nil {
			continue
		}
		at := strings.TrimSpace(a.AssetType)
		if at == "" {
			continue
		}
		if rec.Project != "" && a.Project != "" && a.Project != rec.Project {
			continue
		}
		if name := strings.TrimSpace(a.AssetName); name != "" {
			byType[at] = append(byType[at], name)
		}
	}
	for at := range byType {
		byType[at] = dedupSorted(byType[at])
	}

	for i := range rec.Fields {
		f := &rec.Fields[i]
		at, watched := assetTypeOf[f.Code]
		if !watched {
			continue
		}
		opts := byType[at]
		if len(opts) == 0 {
			continue // 台账里还没建这类设备,这一格就先不给选择器
		}
		// 已经选过的值必须留在候选里 —— 否则人打开下拉发现自己选的那台不在
		// 列表里,想确认一下都选不回来(设备被改名或删了就会这样)。
		cur := strings.TrimSpace(f.AssetName)
		if cur != "" && !slices.Contains(opts, cur) {
			opts = dedupSorted(append(append([]string{}, opts...), cur))
		}
		f.AssetOptions = opts
		if cur == "" {
			f.AssetName = defaultAssetForField(f.Label, opts)
		}
	}
}

// defaultAssetForField 按字段名猜默认是哪台设备。
//
// 【只做"去掉读数二字之后名字一模一样"这一档】"Z1 能耗表读数" → "Z1能耗表",
// "生活水表读数" → "生活水表",对得上就填,对不上就留空让人选。
//
// 【不做模糊匹配】猜错一台比不猜更糟:选择器上摆着一个错的默认值,人扫一眼
// 觉得"系统填好了"就过去了 —— 那正是今天要修的那个毛病(确认变成下一步)。
func defaultAssetForField(label string, options []string) string {
	want := normalizeAssetKey(label)
	if want == "" {
		return ""
	}
	for _, o := range options {
		if normalizeAssetKey(o) == want {
			return o
		}
	}
	return ""
}

// normalizeAssetKey 去掉空格和结尾的"读数",大小写统一。
func normalizeAssetKey(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "") // 全角空格
	s = strings.TrimSuffix(s, "读数")
	return strings.ToUpper(s)
}

func dedupSorted(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// orNone 留痕里空设备写成「未指定」,别留一个空括号让人猜。
func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未指定设备"
	}
	return s
}
