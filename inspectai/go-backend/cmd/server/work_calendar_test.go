package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===== 工作日历 =====
//
// 2026-09-28:国庆前问到"放假和调休怎么办"。原来只认周几 —— 放假照样列待巡、
// 照样推"还差 N 台";调休上班的周六,只勾了周一到周五的反而不推。

func TestWorkdayRuleFollowsCalendar(t *testing.T) {
	off := WorkCalendarDay{Kind: workDayOff, Name: "国庆节"}
	on := WorkCalendarDay{Kind: workDayOn, Name: "国庆节"}
	cases := []struct {
		name     string
		weekdays string
		wd       int
		cal      WorkCalendarDay
		want     bool
	}{
		{"平常的周一", dayRuleWorkday, 1, WorkCalendarDay{}, true},
		{"平常的周六", dayRuleWorkday, 6, WorkCalendarDay{}, false},
		{"放假的周四", dayRuleWorkday, 4, off, false},
		{"调休上班的周六", dayRuleWorkday, 6, on, true},
		// 【没选法定工作日的不受日历影响】机房、泵房节假日照样要巡
		{"每天:放假也执行", "", 4, off, true},
		{"按星期:放假也执行", "1,2,3,4,5", 4, off, true},
		{"按星期:调休周六不因日历执行", "1,2,3,4,5", 6, on, false},
	}
	for _, c := range cases {
		if got := runsOnDay(c.weekdays, c.wd, c.cal); got != c.want {
			t.Errorf("%s: 得到 %v,应该是 %v", c.name, got, c.want)
		}
	}
}

func TestWorkdayRuleIsAValidWeekdaysValue(t *testing.T) {
	if msg := validWeekdays(dayRuleWorkday); msg != "" {
		t.Fatalf("「法定工作日」被当成了脏值:%s", msg)
	}
	if msg := validWeekdays("workdays"); msg == "" {
		t.Fatal("拼错的规则名被放过了 —— 存进去之后这条计划永远不执行,而且不报错")
	}
}

// 样例文字只用来测写法,日期以当年国务院通知为准。
const sampleNotice = `国务院办公厅关于2025年部分节假日安排的通知
一、元旦：1月1日（周三）放假1天，不调休。
二、春节：1月28日（农历除夕、周二）至2月4日（农历正月初七、周二）放假调休，共8天。1月26日（周日）、2月8日（周六）上班。
三、清明节：4月4日（周五）至6日（周日）放假，共3天。
四、劳动节：5月1日（周四）至5日（周一）放假调休，共5天。4月27日（周日）上班。
五、端午节：5月31日（周六）至6月2日（周一）放假，共3天。
六、国庆节、中秋节：10月1日（周三）至8日（周三）放假调休，共8天。9月28日（周日）、10月11日（周六）上班。`

func calendarIndex(days []WorkCalendarDay) map[string]WorkCalendarDay {
	m := map[string]WorkCalendarDay{}
	for _, d := range days {
		m[d.Date] = d
	}
	return m
}

func TestParseHolidayNotice(t *testing.T) {
	year, days, warnings := parseHolidayNotice(sampleNotice, 2026)
	if year != 2025 {
		t.Fatalf("标题里写的是 2025 年,得到 %d", year)
	}
	if len(warnings) != 0 {
		t.Fatalf("不该有认不出的地方:%v", warnings)
	}
	m := calendarIndex(days)
	off, on := 0, 0
	for _, d := range days {
		if d.Kind == workDayOff {
			off++
		} else {
			on++
		}
	}
	if off != 28 || on != 5 {
		t.Fatalf("应认出放假 28 天、上班 5 天,得到 %d / %d", off, on)
	}
	checks := map[string]string{
		"2025-01-28": workDayOff, "2025-02-04": workDayOff, // 跨月的区间
		"2025-04-06": workDayOff, // "至6日"省了月份
		"2025-06-02": workDayOff,
		"2025-01-26": workDayOn, "2025-02-08": workDayOn,
		"2025-09-28": workDayOn, "2025-10-11": workDayOn,
	}
	for date, kind := range checks {
		if m[date].Kind != kind {
			t.Errorf("%s 应该是 %s,得到 %q", date, kind, m[date].Kind)
		}
	}
	if m["2025-10-05"].Name != "国庆节、中秋节" || m["2025-01-26"].Name != "春节" {
		t.Errorf("节日名没对上:%q / %q", m["2025-10-05"].Name, m["2025-01-26"].Name)
	}
	if _, ok := m["2025-01-02"]; ok {
		t.Error("元旦只放 1 天,1月2日不该在日历里")
	}
}

// 【网页上复制下来常常没有换行】只认行首编号的话,后面几个节日全算进「元旦」。
func TestParseHolidayNoticeOnOneLine(t *testing.T) {
	flat := strings.ReplaceAll(sampleNotice, "\n", "")
	_, days, warnings := parseHolidayNotice(flat, 2025)
	if len(warnings) != 0 {
		t.Fatalf("不该有认不出的地方:%v", warnings)
	}
	m := calendarIndex(days)
	if len(days) != 33 {
		t.Fatalf("应认出 33 天,得到 %d", len(days))
	}
	if m["2025-05-03"].Name != "劳动节" {
		t.Errorf("连成一行后节日名串了:5月3日 得到 %q", m["2025-05-03"].Name)
	}
}

// 【上班写在前面也不能认成放假】"1月26日上班,1月28日至2月4日放假"
// 不能从 1月26日 一路吞到"放假"。
func TestParseHolidayNoticeWorkdayBeforeHoliday(t *testing.T) {
	_, days, _ := parseHolidayNotice("春节：1月26日（周日）上班，1月28日至2月4日放假。", 2025)
	m := calendarIndex(days)
	if m["2025-01-26"].Kind != workDayOn {
		t.Fatalf("1月26日应该是上班,得到 %q", m["2025-01-26"].Kind)
	}
	if m["2025-01-28"].Kind != workDayOff || m["2025-02-04"].Kind != workDayOff {
		t.Fatal("1月28日至2月4日应该认成放假")
	}
}

// 【只认不猜】认不出、不存在的日子要报出来,不能静默丢掉。
func TestParseHolidayNoticeReportsWhatItCannotRead(t *testing.T) {
	_, days, warnings := parseHolidayNotice("一、某节：2月30日放假。\n二、另一节：待定。", 2026)
	if len(days) != 0 {
		t.Fatalf("不存在的日子被写进了日历:%v", days)
	}
	joined := strings.Join(warnings, "|")
	if !strings.Contains(joined, "2月30日") || !strings.Contains(joined, "另一节") {
		t.Fatalf("认不出的地方没报全:%v", warnings)
	}
}

// 放假那天,选了「法定工作日」的计划不进今日待巡,而且要说出有几条是因为放假没进。
func TestTodayBoardSkipsWorkdayPlansOnHoliday(t *testing.T) {
	srv, r, store := newBoardServer(t)
	now := time.Now()
	today := dayStamp(now)
	addDailyPlan(t, store, "office", dayRuleWorkday, []string{"会议中心::escalator::KT-1"})
	addDailyPlan(t, store, "pump", "", []string{"会议中心::escalator::KT-2"}) // 每天:放假照样巡

	if err := store.SaveWorkCalendar("", "", []WorkCalendarDay{{Date: today, Kind: workDayOff, Name: "国庆节"}}, ""); err != nil {
		t.Fatal(err)
	}
	board, err := srv.buildTodayBoard(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plans) != 1 || board.Plans[0].PlanID != "pump" {
		t.Fatalf("放假只该剩「每天」那条,得到 %+v", board.Plans)
	}
	if board.HolidaySkipped != 1 || board.DayKind != workDayOff || board.DayName != "国庆节" {
		t.Fatalf("没说清今天放假:skipped=%d kind=%q name=%q", board.HolidaySkipped, board.DayKind, board.DayName)
	}

	// 调休上班:哪怕今天是周末,法定工作日的计划也要出来
	if err := store.SaveWorkCalendar("", "", []WorkCalendarDay{{Date: today, Kind: workDayOn, Name: "国庆节"}}, ""); err != nil {
		t.Fatal(err)
	}
	board, err = srv.buildTodayBoard(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plans) != 2 || board.HolidaySkipped != 0 {
		t.Fatalf("调休上班日两条都该在,得到 %d 条、skipped=%d", len(board.Plans), board.HolidaySkipped)
	}
}

func TestPushFollowsCalendarWhenAskedTo(t *testing.T) {
	c := baseCfg()
	c.Weekdays = dayRuleWorkday
	thu := at("17:05") // 2026-08-27 周四
	if ok, _ := shouldFireDailyPush(c, thu, WorkCalendarDay{Kind: workDayOff}, "", 0); ok {
		t.Fatal("放假那天按法定工作日推送的群不该发")
	}
	sat := thu.AddDate(0, 0, 2)
	if ok, why := shouldFireDailyPush(c, sat, WorkCalendarDay{Kind: workDayOn}, "", 0); !ok {
		t.Fatalf("调休上班的周六应该发,却因为「%s」没发", why)
	}
	// 按星期的群不看日历
	c.Weekdays = "1,2,3,4,5"
	if ok, _ := shouldFireDailyPush(c, thu, WorkCalendarDay{Kind: workDayOff}, "", 0); !ok {
		t.Fatal("按星期推送的群被日历拦住了 —— 没选法定工作日的不该受影响")
	}
}

// 真库:清一年再写要在一个事务里;kind 为空 = 删掉那一天。
func TestWorkCalendarStoreRoundTrip(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "cal.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	_, days, _ := parseHolidayNotice(sampleNotice, 2025)
	if err := store.SaveWorkCalendar("2025-01-01", "2025-12-31", days, "tester"); err != nil {
		t.Fatal(err)
	}
	got, err := store.ListWorkCalendar("2025-01-01", "2025-12-31")
	if err != nil || len(got) != 33 {
		t.Fatalf("存进 33 天,读回 %d 天(%v)", len(got), err)
	}
	// 覆盖同一天 + 删一天
	if err := store.SaveWorkCalendar("", "", []WorkCalendarDay{
		{Date: "2025-10-08", Kind: workDayOn, Name: "改了"},
		{Date: "2025-10-11", Kind: ""},
	}, "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.ListWorkCalendar("2025-10-08", "2025-10-11")
	m := calendarIndex(got)
	if m["2025-10-08"].Kind != workDayOn || m["2025-10-08"].Name != "改了" {
		t.Errorf("同一天没被覆盖:%+v", m["2025-10-08"])
	}
	if _, ok := m["2025-10-11"]; ok {
		t.Error("kind 为空的那天没删掉")
	}
	// 重新粘贴一整年:之前手改的日子要被清掉
	if err := store.SaveWorkCalendar("2025-01-01", "2025-12-31", days[:3], "tester"); err != nil {
		t.Fatal(err)
	}
	got, _ = store.ListWorkCalendar("2025-01-01", "2025-12-31")
	if len(got) != 3 {
		t.Fatalf("覆盖一整年后应只剩 3 天,得到 %d", len(got))
	}
}
