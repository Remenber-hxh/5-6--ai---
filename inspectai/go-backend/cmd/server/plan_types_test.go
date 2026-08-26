package main

import (
	"testing"
	"time"
)

// 每日计划"今天要不要执行"的判定。
//
// 这里最容易出错的是星期几的口径:Go 的 time.Weekday 里【周日是 0】,
// 而中国人的排班表一律是"周一到周日",周日排最后。混用的结果是
// 勾了"周一到周五"实际在周日也推送 —— 而且只有周末才暴露,平时测不出来。

func TestISOWeekdayPutsSundayLast(t *testing.T) {
	cases := []struct {
		in   time.Weekday
		want int
		name string
	}{
		{time.Monday, 1, "周一"},
		{time.Tuesday, 2, "周二"},
		{time.Friday, 5, "周五"},
		{time.Saturday, 6, "周六"},
		{time.Sunday, 7, "周日"}, // 【关键】Go 里它是 0,排班表里它是第 7 天
	}
	for _, c := range cases {
		if got := isoWeekday(int(c.in)); got != c.want {
			t.Errorf("%s 应为 %d,得到 %d —— 周日错位会让「周一到周五」在周末也触发",
				c.name, c.want, got)
		}
	}
}

func TestRunsOnWeekday(t *testing.T) {
	// 空 = 每天。【不能当成"一天都不执行"】—— 那样存量计划全部静默失效,
	// 而且没有任何报错,只是提醒突然不来了。
	for wd := 1; wd <= 7; wd++ {
		if !runsOnWeekday("", wd) {
			t.Errorf("没配执行日应视为每天,周%d 却不执行", wd)
		}
	}

	// 工作日排班
	work := "1,2,3,4,5"
	for _, wd := range []int{1, 2, 3, 4, 5} {
		if !runsOnWeekday(work, wd) {
			t.Errorf("周%d 应执行", wd)
		}
	}
	for _, wd := range []int{6, 7} {
		if runsOnWeekday(work, wd) {
			t.Errorf("周%d 不该执行 —— 周末推送会让人开始忽略这条提醒", wd)
		}
	}

	// 【别被前缀匹配骗了】"1" 不该匹配上 "17" 之类的脏数据;
	// 而带空格的配置("1, 3, 5")是人手填出来的,必须容忍
	if !runsOnWeekday("1, 3, 5", 3) {
		t.Error("带空格的配置应该照样认")
	}
	if runsOnWeekday("2,4", 1) {
		t.Error("没勾的日子不该执行")
	}
}

// 类型校验:只认这五档,别的一律拒绝 —— 存进去一个拼错的类型,
// 那条计划会在所有按类型筛选的地方消失,而且不报错。
func TestValidPlanType(t *testing.T) {
	for _, ok := range []string{planTypeYearly, planTypeMonthly, planTypeWeekly, planTypeDaily, planTypeAdhoc} {
		if !validPlanType(ok) {
			t.Errorf("%s 应该是合法类型", ok)
		}
	}
	for _, bad := range []string{"", "week", "每日", "DAILY", "quarterly"} {
		if validPlanType(bad) {
			t.Errorf("%q 不该被当成合法类型", bad)
		}
	}
}
