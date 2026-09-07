package main

import (
	"regexp"
	"strings"
)

// ===== 业务状态 =====
//
// 就是界面上那个标签:人工填写 / 需补图 / 异常 / 待复核 / 已完成 / 正常。
//
// 【为什么要搬到后端来】这条规则原来只有 admin-web 一份实现
// (admin-web/src/lib/status.ts)。于是任何需要它的后端能力都得再写一遍:
// 按状态导出、看板按状态聚合、将来按状态查询,全都要。
//
// 两份实现迟早会对不上,而对不上的表现是【导出里的状态和页面上显示的不一样】——
// 没人会怀疑是两套代码,只会怀疑数据错了,然后去查数据库。
//
// 所以规则只留一份,放在数据这一侧;前端不再自己算,直接读
// businessStatus 字段。字段在 sanitizeRecordForCurrentTemplate 里填 ——
// 那是所有记录出站的唯一收口,填在那里就漏不掉。
//
// 【搬迁口径】这份 Go 实现是 status.ts 的逐行翻译,不是"重新设计"。
// 借机改口径的话,全系统的状态会在某次部署后集体变一遍,而变更原因
// 埋在一次重构里,没人对得上。要改口径,单独改、单独说。

// abnormalValueRe 字段值里出现这些词就算异常。
//
// 【匹配的是人填/AI 填进去的值,不是标签】标签里带"异常"的字段
// (比如"有无异响")是常态,拿标签匹配会让整批记录永远显示异常。
var abnormalValueRe = regexp.MustCompile(
	`异常|告警|故障|离线|不合格|超标|漏水|渗漏|报警|破损|损坏|缺失|跳闸|烧毁`)

// recordLevel 危险程度:danger / warning / normal。
func recordLevel(rec *Record) string {
	values := make([]string, 0, len(rec.Fields))
	for _, f := range rec.Fields {
		values = append(values, f.Value)
	}
	// 【只看 Value,不看 AIValue】AIValue 是模型的原始输出,人可能已经
	// 改过了。拿它判异常的话,人工订正过的记录还会顶着"异常"不消失。
	if abnormalValueRe.MatchString(strings.Join(values, " ")) {
		return "danger"
	}
	// 已提交 = 人看过并认可了。后面那两条"待复核"的理由就不再成立。
	if rec.Submitted {
		return "normal"
	}
	for _, f := range rec.Fields {
		if f.NeedsReview {
			return "warning"
		}
	}
	if rec.RecognitionStatus == "retake_required" {
		return "warning"
	}
	return "normal"
}

// hasInspectionResult 这条记录到底有没有巡检结果。
//
// 用来把"还没开始/传了图还没识别"和"已经有结论"分开 ——
// 两者都不是异常,但前者不该显示成「正常」。
func hasInspectionResult(rec *Record) bool {
	if rec.Submitted || rec.RecognitionStatus == "recognized" {
		return true
	}
	for _, f := range rec.Fields {
		if strings.TrimSpace(f.Value) != "" {
			return true
		}
	}
	return false
}

// 【为什么 BusinessStatus 不入库】它整个是从别的字段推出来的。存一份的话,
// 改了 submitted 却忘了同步它,界面就会一直显示旧状态,而且不报错 ——
// 派生值落库就是给自己埋一个必然会过期的副本。所以只在出站时算。
// (记录行是列映射写库的,这个字段不会被带进去。)

// recordBusinessStatus 给一条记录算业务状态。
//
// 顺序有讲究,不能调:「人工填写」和「需补图」是流程状态,优先级高于
// 内容判断 —— 一条还等着补图的记录,即使已填的字段都正常,也不该显示
// 成「正常」,那会让人以为它已经巡完了。
func recordBusinessStatus(rec *Record) string {
	if rec == nil {
		return ""
	}
	if rec.ManualRequired || rec.RecognitionStatus == "manual_required" {
		return "人工填写"
	}
	if rec.RecognitionStatus == "retake_required" {
		return "需补图"
	}
	switch recordLevel(rec) {
	case "danger":
		return "异常"
	case "warning":
		return "待复核"
	}
	if rec.Submitted {
		return "已完成"
	}
	if hasInspectionResult(rec) {
		return "正常"
	}
	// 【兜底是"待复核",不是"正常"】没有任何结果的记录说成正常,
	// 等于告诉人"这里巡过了没问题" —— 而实际上一个字段都没填。
	return "待复核"
}
