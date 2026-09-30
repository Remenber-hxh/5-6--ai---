package main

import (
	"strings"
	"unicode/utf8"
)

// ===== 读数类字段:按设备归成一条,不按栏位 =====
//
// 抄表模板的每一格都自带设备类型(Z1~Z4 电表、两块水表):现场可以把任意一块表
// 选进任意一格,读数就记在那一格里。于是同一块表的历史散在好几个栏位上 ——
// 2026-09-29 查线上:Z1 的 4 次读数分别记在 Z1、Z2、Z4 三栏,Z2 的记在 Z1、Z2、Z3 三栏。
// 按栏位分组的话每组都不到 3 个点,读数趋势一条都画不出,漂移也比不出来;
// 两块水表的读数一直在自己那一栏,所以只有水表有趋势。
//
// 这种字段"记在哪一格"不重要,"是哪台表的读数"才重要。观测本来就是按设备记的
// (asset_id 是对的,见 buildRecordObservations 的 sourceFields),
// 分组时把这些栏位合成一条就行。
//
// 【合在一起不会串】一次巡检里,一台表只能选进一格(handlePatchField 的 asset_taken),
// 所以同一台设备、同一次记录,这些栏位里最多只有一个读数。
//
// 【五处共用这一套】读数趋势、设备健康报告、数值漂移列表、漂移计数、风险分里的漂移,
// 各按各的分组的话,会出现"图上涨了、漂移那边说没变"—— 同一个数两种说法。

// mergedReadingKey 合并之后那条序列的键
const mergedReadingKey = "reading"

// readingFieldsOf 这个模板里哪些字段是"一格就是一台设备的读数"(数值字段且自带设备类型)。
func readingFieldsOf(templateID string) map[string]bool {
	out := map[string]bool{}
	if tpl, ok := templateByID(templateID); ok {
		for _, f := range tpl.Fields {
			if f.Kind == "number" && strings.TrimSpace(f.AssetType) != "" {
				out[f.Code] = true
			}
		}
	}
	return out
}

// seriesKeyOf 分组用的键:读数类字段合成一条,其余照旧按字段。
func seriesKeyOf(fieldKey string, readingFields map[string]bool) string {
	if readingFields[fieldKey] {
		return mergedReadingKey
	}
	return fieldKey
}

// readingSeriesLabel 合并后那条序列叫什么:"生活水表读数"、"Z1 读数"。
//
// 【不用栏位名】Z1 的曲线上写"Z4 能耗表读数",看的人会以为挂错了表。
func readingSeriesLabel(assetName string) string {
	name := strings.TrimSpace(assetName)
	if name == "" {
		return "读数"
	}
	// 编号结尾(Z1)空一格,中文结尾(生活水表)直接接
	if r, _ := utf8.DecodeLastRuneInString(name); r < utf8.RuneSelf {
		return name + " 读数"
	}
	return name + "读数"
}

// seriesLabelOf 一条序列给人看的名字。
func seriesLabelOf(key, fieldLabel, assetName string) string {
	if key == mergedReadingKey {
		return readingSeriesLabel(assetName)
	}
	return fieldLabel
}
