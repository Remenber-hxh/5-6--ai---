package main

import (
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// ===== 工作日历:法定节假日 + 调休上班日 =====
//
// 原来只有"周几",放假和调休都认不出来:国庆期间照样列今日待巡、照样往群里推
// "还差 N 台";调休上班的周六,只勾了周一到周五的反而不出任务、不推。
//
// 【不接外部接口】国务院每年 11–12 月公布第二年的安排,公布后录一次即可。
// 服务器访问外网本来就要绕代理,而放假安排不会临时变 —— 为一年一次的事
// 引入一个"对方接口挂了,今天所有提醒都不发"的依赖不划算。
//
// 【不分租户】法定节假日全国统一;和 app_settings 一样是部署级的数据。
//
// 【谁跟着日历走,是计划和推送群自己选的】打开「跳过法定节假日」才看日历,
// 没打开的完全不受影响 —— 机房、泵房、消防这类节假日照样要巡的,
// 录了日历也不会被跳过。
//
// 【星期和日历是两个独立的设置】执行日 = 勾哪几天(weekdays)+ 要不要跳过
// 法定节假日(followCalendar)。周一到周五 + 跳过 就是通常说的"法定工作日";
// 周一到周六 + 跳过、七天全勾 + 跳过(平时天天推、放假才停)也都能表达。

const (
	// dayRuleWorkday 【老写法】第一版把「法定工作日」做成和星期互斥的第三个选项,
	// weekdays 里存的就是这个词。线上已经存了,读的时候一律换算成
	// "周一到周五 + 跳过节假日"(见 normalizeDayRule),新保存的不会再出现它。
	dayRuleWorkday = "workday"

	workDayOff = "off" // 放假
	workDayOn  = "on"  // 调休上班
)

// normalizeDayRule 把老写法 "workday" 换算成 周一到周五 + 跳过节假日。
func normalizeDayRule(weekdays string, follow bool) (string, bool) {
	weekdays = strings.TrimSpace(weekdays)
	if weekdays == dayRuleWorkday {
		return "1,2,3,4,5", true
	}
	return weekdays, follow
}

// WorkCalendarDay 日历里的一天。没录的日子不存 —— 按平常的周一到周五算。
type WorkCalendarDay struct {
	Date string `json:"date"` // 2026-10-01
	Kind string `json:"kind"` // off 放假 / on 调休上班
	Name string `json:"name,omitempty"`
}

// runsOnDay 这一天要不要执行。cal 是这一天在日历里的记录(没录就是零值)。
//
// 跳过节假日(follow)时:放假那天不执行;调休上班那天执行 —— 不管勾没勾那个星期,
// 那天按上班日算;其余日子看勾了哪几天。
// 日历里没录的日子只看星期 —— 宁可节假日多推一次,
// 也不要因为没录日历,工作日一条提醒都不发。
func runsOnDay(weekdays string, follow bool, wd int, cal WorkCalendarDay) bool {
	weekdays, follow = normalizeDayRule(weekdays, follow)
	if follow {
		switch cal.Kind {
		case workDayOff:
			return false
		case workDayOn:
			return true
		}
	}
	return runsOnWeekday(weekdays, wd)
}

// skippedForHoliday 今天不执行,是不是因为放假(按星期本来要执行)。
// 看板和预览要把"因为放假"单独说出来 —— 和"今天本来就不排"是两回事。
func skippedForHoliday(weekdays string, follow bool, wd int, cal WorkCalendarDay) bool {
	weekdays, follow = normalizeDayRule(weekdays, follow)
	return follow && cal.Kind == workDayOff && runsOnWeekday(weekdays, wd)
}

// workCalendarOn 这一天在日历里的记录。
//
// 【查不到就当没录】日历读失败时回落到周一到周五,不让提醒因为一次查库失败而断掉。
func (s *Server) workCalendarOn(day string) WorkCalendarDay {
	days, err := s.store.ListWorkCalendar(day, day)
	if err != nil {
		log.Printf("WARN: 读工作日历失败(%s),按周一到周五算: %v", day, err)
		return WorkCalendarDay{}
	}
	for _, d := range days {
		if d.Date == day {
			return d
		}
	}
	return WorkCalendarDay{}
}

// ===== 接口 =====

// handleGetWorkCalendar 一年的日历。
func (s *Server) handleGetWorkCalendar(w http.ResponseWriter, r *http.Request) {
	year, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("year")))
	if year == 0 {
		year = time.Now().In(pushTZ).Year()
	}
	if year < 2000 || year > 2100 {
		writeError(w, http.StatusBadRequest, "bad_year", "年份不对")
		return
	}
	days, err := s.store.ListWorkCalendar(fmt.Sprintf("%04d-01-01", year), fmt.Sprintf("%04d-12-31", year))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_calendar_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"year": year, "days": days})
}

// handleSaveWorkCalendar 改日历。
//
// days 里 kind 为空的 = 恢复成平常日子(删掉那一天)。
// replaceYear 非 0 = 先清空那一年再写 —— 粘贴一整份通知时用,
// 否则改了一次的错日子会一直留在日历里。
func (s *Server) handleSaveWorkCalendar(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ReplaceYear int               `json:"replaceYear"`
		Days        []WorkCalendarDay `json:"days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if req.ReplaceYear != 0 && (req.ReplaceYear < 2000 || req.ReplaceYear > 2100) {
		writeError(w, http.StatusBadRequest, "bad_year", "年份不对")
		return
	}
	if len(req.Days) > 800 {
		writeError(w, http.StatusBadRequest, "too_many_days", "一次改的日子太多了")
		return
	}
	for i := range req.Days {
		d := &req.Days[i]
		d.Date = strings.TrimSpace(d.Date)
		d.Kind = strings.TrimSpace(d.Kind)
		d.Name = strings.TrimSpace(d.Name)
		if _, err := time.Parse("2006-01-02", d.Date); err != nil {
			writeError(w, http.StatusBadRequest, "bad_date", "日期要写成 2026-10-01 这样:"+d.Date)
			return
		}
		if d.Kind != "" && d.Kind != workDayOff && d.Kind != workDayOn {
			writeError(w, http.StatusBadRequest, "bad_kind", "只能是放假或调休上班")
			return
		}
		if utf8.RuneCountInString(d.Name) > 32 {
			writeError(w, http.StatusBadRequest, "bad_name", "节日名称太长了")
			return
		}
	}
	clearFrom, clearTo := "", ""
	if req.ReplaceYear != 0 {
		clearFrom = fmt.Sprintf("%04d-01-01", req.ReplaceYear)
		clearTo = fmt.Sprintf("%04d-12-31", req.ReplaceYear)
	}
	if err := s.store.SaveWorkCalendar(clearFrom, clearTo, req.Days, s.currentUserName(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "save_calendar_failed", err.Error())
		return
	}
	s.recordOperation(r, "work_calendar_save", "work_calendar", strconv.Itoa(req.ReplaceYear), map[string]any{
		"replaceYear": req.ReplaceYear, "days": len(req.Days),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleParseHolidayNotice 把粘贴进来的放假通知认成日子。只认不存 —— 人看过再保存。
func (s *Server) handleParseHolidayNotice(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Year int    `json:"year"`
		Text string `json:"text"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	if utf8.RuneCountInString(req.Text) > 5000 {
		writeError(w, http.StatusBadRequest, "text_too_long", "粘贴的内容太长了,只要放假安排那几段")
		return
	}
	year, days, warnings := parseHolidayNotice(req.Text, req.Year)
	writeJSON(w, http.StatusOK, map[string]any{"year": year, "days": days, "warnings": warnings})
}

// ===== 放假通知解析 =====
//
// 认的是国务院办公厅通知的写法,例如:
//
//	二、春节:1月28日(农历除夕、周二)至2月4日(农历正月初七、周二)放假调休,共8天。
//	1月26日(周日)、2月8日(周六)上班。
//
// 【只认不猜】认不出来的段落原样报出来,不去补一个"大概是这几天"的日期 ——
// 日历错一天,就是一整天的提醒发错。

var (
	// 一个日子:年、月可省(区间的后一半常写成"至6日")
	noticeDate = `(?:(\d{4})\s*年\s*)?(?:(\d{1,2})\s*月\s*)?(\d{1,2})\s*日(?:\s*[（(][^）)]*[）)])?`
	// 放假:"X月X日(…)至X日(…)放假" 或 "X月X日(…)放假"。
	//
	// 【日子和"放假"之间不许出现数字】否则"1月26日上班,1月28日至2月4日放假"
	// 会从 1月26日 一路吞到"放假",认成 1月26日 放假一天。
	noticeOffRe = regexp.MustCompile(noticeDate + `(?:\s*(?:至|到|~|～|—|-)\s*` + noticeDate + `)?[^。；;\n\d]*?放假`)
	// 上班那句里的每个日子。月份必须写 —— 上班日从来是一个个列出来的
	noticeFullDateRe = regexp.MustCompile(`(?:(\d{4})\s*年\s*)?(\d{1,2})\s*月\s*(\d{1,2})\s*日`)
	// 一段节日:"二、春节:"。
	//
	// 【不要求在行首】从网页上复制下来的通知常常是一整段、没有换行,
	// 标题和"一、元旦"也连在一起;只认行首的话,元旦丢了、后面几个节日全算进一段。
	// 【前一个字不能是数字或"初"】括号里的农历写法"正月初七、周二"不是一段的开头;
	// 真正挡住误认的是后面那条:编号之后 20 个字以内、同一句里必须有冒号。
	noticeItemRe     = regexp.MustCompile(`(?m)(?:^|[^一二三四五六七八九十初])[一二三四五六七八九十]+[ \t　]*[、.．][ \t　]*([^：:\n。]{1,20}?)[ \t　]*[：:]`)
	noticeSentenceRe = regexp.MustCompile(`[。；;\n]`)
	// 通知标题里的年份:"关于2026年部分节假日安排的通知"
	noticeYearRe  = regexp.MustCompile(`(\d{4})\s*年[^。\n]{0,6}节假日安排`)
	noticeParenRe = regexp.MustCompile(`[（(][^）)]*[）)]`)
)

// maxHolidaySpan 一段假期最多几天。春节最长也就九天 —— 认出来超过这个数,
// 多半是把两个不相干的日子连成了区间,宁可报错也不要写进日历。
const maxHolidaySpan = 15

type noticeItem struct {
	name string
	body string
	// preamble 第一段编号之前的文字(标题、"经国务院批准……")。
	// 里面有日期也照认,但没日期不报 —— 它本来就不是某个节日。
	preamble bool
}

// splitNoticeItems 按"一、二、三、"切成一段一段。没有编号的话每行算一段。
func splitNoticeItems(text string) []noticeItem {
	locs := noticeItemRe.FindAllStringSubmatchIndex(text, -1)
	if len(locs) == 0 {
		var out []noticeItem
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			name := ""
			if i := strings.IndexAny(line, "：:"); i > 0 && utf8.RuneCountInString(line[:i]) <= 12 {
				name = line[:i]
			}
			out = append(out, noticeItem{name: name, body: line})
		}
		return out
	}
	out := make([]noticeItem, 0, len(locs)+1)
	if head := strings.TrimSpace(text[:locs[0][0]]); head != "" {
		out = append(out, noticeItem{body: head, preamble: true})
	}
	for i, loc := range locs {
		end := len(text)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		out = append(out, noticeItem{name: text[loc[2]:loc[3]], body: text[loc[1]:end]})
	}
	return out
}

// noticeDay 拼出一个日期;不存在的日子(2月30日)返回 false。
func noticeDay(year, month, day int) (time.Time, bool) {
	t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
	return t, t.Year() == year && int(t.Month()) == month && t.Day() == day
}

func atoiOr(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// parseHolidayNotice 返回认出来的年份、日子(按日期排好)和认不出的地方。
func parseHolidayNotice(text string, year int) (int, []WorkCalendarDay, []string) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	if m := noticeYearRe.FindStringSubmatch(text); m != nil {
		year = atoiOr(m[1], year)
	}
	if year == 0 {
		year = time.Now().In(pushTZ).Year()
	}

	byDate := map[string]WorkCalendarDay{}
	var warnings []string
	add := func(t time.Time, kind, name string) {
		key := t.Format("2006-01-02")
		if old, ok := byDate[key]; ok && old.Kind != kind {
			warnings = append(warnings, fmt.Sprintf("%s 既写了放假又写了上班,按先出现的「%s」算", key, kindText(old.Kind)))
			return
		}
		byDate[key] = WorkCalendarDay{Date: key, Kind: kind, Name: name}
	}

	for _, it := range splitNoticeItems(text) {
		name := strings.TrimSpace(noticeParenRe.ReplaceAllString(it.name, ""))
		label := name
		if label == "" {
			label = strings.TrimSpace(firstRunes(it.body, 12))
		}
		found := false

		// 放假。认过的那几段从正文里抹掉,剩下的才去找上班日 ——
		// 否则"1月26日上班,1月28日至2月4日放假"这种写在同一句里的,
		// 上班日会跟着"放假"那半句一起被跳过。
		rest := []byte(it.body)
		for _, loc := range noticeOffRe.FindAllStringSubmatchIndex(it.body, -1) {
			m := make([]string, len(loc)/2)
			for k := range m {
				if loc[2*k] >= 0 {
					m[k] = it.body[loc[2*k]:loc[2*k+1]]
				}
			}
			if m[2] == "" { // 起始日没写月份,认不准
				continue
			}
			for k := loc[0]; k < loc[1]; k++ {
				rest[k] = ' '
			}
			y1 := atoiOr(m[1], year)
			mo1, d1 := atoiOr(m[2], 0), atoiOr(m[3], 0)
			start, ok := noticeDay(y1, mo1, d1)
			if !ok {
				warnings = append(warnings, fmt.Sprintf("「%s」里有个不存在的日子:%d月%d日", label, mo1, d1))
				continue
			}
			end := start
			if m[6] != "" {
				mo2 := atoiOr(m[5], mo1)
				y2 := atoiOr(m[4], y1)
				if m[4] == "" && mo2 < mo1 { // 12月30日至1月1日
					y2 = y1 + 1
				}
				d2 := atoiOr(m[6], 0)
				e, ok := noticeDay(y2, mo2, d2)
				if !ok {
					warnings = append(warnings, fmt.Sprintf("「%s」里有个不存在的日子:%d月%d日", label, mo2, d2))
					continue
				}
				end = e
			}
			span := int(end.Sub(start).Hours()/24) + 1
			if span < 1 || span > maxHolidaySpan {
				warnings = append(warnings, fmt.Sprintf("「%s」认出来的假期是 %d 天,不像对的,没有采用", label, span))
				continue
			}
			for t := start; !t.After(end); t = t.AddDate(0, 0, 1) {
				add(t, workDayOff, name)
			}
			found = true
		}

		// 上班:带"上班"的那几句里剩下的日子
		for _, sent := range noticeSentenceRe.Split(string(rest), -1) {
			if !strings.Contains(sent, "上班") {
				continue
			}
			for _, m := range noticeFullDateRe.FindAllStringSubmatch(sent, -1) {
				mo, d := atoiOr(m[2], 0), atoiOr(m[3], 0)
				t, ok := noticeDay(atoiOr(m[1], year), mo, d)
				if !ok {
					warnings = append(warnings, fmt.Sprintf("「%s」里有个不存在的日子:%d月%d日", label, mo, d))
					continue
				}
				add(t, workDayOn, name)
				found = true
			}
		}

		if !found && !it.preamble && strings.TrimSpace(it.body) != "" {
			warnings = append(warnings, fmt.Sprintf("「%s」这一段没认出日期", label))
		}
	}

	days := make([]WorkCalendarDay, 0, len(byDate))
	for _, d := range byDate {
		days = append(days, d)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })
	if len(days) == 0 && len(warnings) == 0 {
		warnings = append(warnings, "没认出任何日期")
	}
	return year, days, warnings
}

func kindText(kind string) string {
	if kind == workDayOn {
		return "上班"
	}
	return "放假"
}

func firstRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}
