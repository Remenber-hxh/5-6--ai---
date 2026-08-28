package main

import (
	"strconv"
	"strings"
)

// ===== 计划类型 =====
//
// 原来只有一个自由文本的「周期」字段,人写什么都行 ——「每周一次」「每月」
// 「季度」各种写法混在一起,程序没法据此算出"今天该巡什么"。
//
// 分成五档。【刻意不做逐级分解】年度拆月度、月度拆周那一套要父子关系和分解界面,
// 而实际需要的只是"这条属于哪一档"。等真要看分解关系时再说。
const (
	planTypeYearly  = "yearly"  // 年度计划
	planTypeMonthly = "monthly" // 月度计划
	planTypeWeekly  = "weekly"  // 周计划
	planTypeDaily   = "daily"   // 每日计划 —— 只有这一档参与每日提醒
	planTypeAdhoc   = "adhoc"   // 临时计划:对外部项目组的对接
)

var planTypeNames = map[string]string{
	planTypeYearly:  "年度计划",
	planTypeMonthly: "月度计划",
	planTypeWeekly:  "周计划",
	planTypeDaily:   "每日计划",
	planTypeAdhoc:   "临时计划",
}

func validPlanType(t string) bool {
	_, ok := planTypeNames[t]
	return ok
}

// runsOnWeekday 这条每日计划今天要不要执行。
//
// weekdays 形如 "1,2,3,4,5"(1=周一 … 7=周日)。空 = 每天都执行。
//
// 【用 1..7 而不是 Go 的 0..6】Go 的 time.Weekday 里周日是 0,而中国人排班表
// 一律是"周一到周日",周日排最后。用 Go 的口径会让周日变成第一天,
// 界面上勾选顺序和实际排班对不上 —— 排班表是给人看的,按人的习惯来。
func runsOnWeekday(weekdays string, wd int) bool {
	weekdays = strings.TrimSpace(weekdays)
	if weekdays == "" {
		return true
	}
	for _, part := range strings.Split(weekdays, ",") {
		if strings.TrimSpace(part) == strconv.Itoa(wd) {
			return true
		}
	}
	return false
}

// isoWeekday 把 Go 的星期几转成 1..7(周一=1,周日=7)。
func isoWeekday(w int) int {
	if w == 0 { // time.Sunday
		return 7
	}
	return w
}
