package main

import (
	"regexp"
	"strings"
)

// ===== 同一项目里"看着是同一台"的设备 =====
//
// 2026-09 规划里排第一:台账里同一台设备登记了两次(会议中心两台 K01、K07 和 K7),
// 一台设备的巡检记录、趋势被拆成两半,一键派单遇到同名还会被拒。
// resolveAssetIdentity 只认名字/编号完全相同的,K07 和 K7 这种认不出来。
//
// 这里给"近似"一个统一口径:新建设备时的提醒用它,一次性查重的 SQL 也按同一套规则写。
// 【只在同一项目里比】不同项目下编号重名很常见(每栋楼都有 K01),跨项目比只会误报。

var (
	// 数字前面的 0:K07 → K7、007 → 7;100 里的 0 不动(前面是数字)
	assetLeadingZeroRe = regexp.MustCompile(`(^|[^0-9])0+([0-9])`)
	// 分隔符:K-07、K_07、1#、Z1·能耗表 都当没有
	assetSepReplacer = strings.NewReplacer("-", "", "_", "", "#", "", "·", "", ".", "", "/", "")
)

// assetSimilarKey 把编号/名字压成比较用的样子:去空格和分隔符、统一大写、
// 去掉结尾的"读数"和"能耗表"这类表种尾巴、去掉数字前面的 0。
func assetSimilarKey(s string) string {
	k := normalizeAssetKey(s)
	for _, suf := range meterTypeSuffixes {
		if strings.HasSuffix(k, suf) && len(k) > len(suf) {
			k = strings.TrimSuffix(k, suf)
			break
		}
	}
	k = assetSepReplacer.Replace(k)
	return assetLeadingZeroRe.ReplaceAllString(k, "${1}${2}")
}

// resolveAssetIdentityLoose 先按 resolveAssetIdentity 的严格规则找;找不到,再按"看着是同一台"找。
//
// 【为什么要宽松的这一步】合并重复设备后,被并掉的那台(K7、Z3能耗表)从台账里删了,
// 可老巡检记录里写的还是旧编号。只认完全相同的话,重启回填按老记录一算,
// 那台又被建了回来 —— 合并等于白做,而且不报错。
//
// 【只认唯一的一台,只在同项目、同模板(或手工建档)里找】两台都像就不猜,
// 退回"这是台新设备"的老路 —— 认错比多一台更糟。
func resolveAssetIdentityLoose(candidates []*AssetEntry, project, templateID, assetKey, assetName string) string {
	if id := resolveAssetIdentity(candidates, project, templateID, assetKey); id != "" {
		return id
	}
	proj, tpl := strings.TrimSpace(project), strings.TrimSpace(templateID)
	hit := ""
	for _, a := range similarAssets(candidates, proj, assetKey, assetName) {
		if t := assetIDTemplatePart(a.ID); t != tpl && t != "manual" {
			continue
		}
		if hit != "" && hit != a.ID {
			return "" // 不止一台像,不猜
		}
		hit = a.ID
	}
	return hit
}

// similarAssets 同一项目里,编号或名字和这次要建的"看着是同一台"的设备。
func similarAssets(all []*AssetEntry, project, assetKey, assetName string) []*AssetEntry {
	want := map[string]bool{}
	for _, s := range []string{assetKey, assetName} {
		if k := assetSimilarKey(s); k != "" {
			want[k] = true
		}
	}
	if len(want) == 0 {
		return nil
	}
	proj := strings.TrimSpace(project)
	var out []*AssetEntry
	for _, a := range all {
		if a == nil || strings.TrimSpace(a.Project) != proj {
			continue
		}
		if want[assetSimilarKey(a.AssetKey)] || want[assetSimilarKey(a.AssetName)] {
			out = append(out, a)
		}
	}
	return out
}
