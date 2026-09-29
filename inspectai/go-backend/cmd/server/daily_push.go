package main

import (
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===== 每日未巡提醒:算什么、发什么 =====
//
// 这个文件【只负责算和拼文案,不负责发】。发出去那一步(定时器、去重、
// 企微通道)是下一步的事。
//
// 【为什么先做到"能预览"就停】口径不准的自动推送比不推送更糟:群里天天
// 收到错的数字,很快就没人看了,而且很难挽回。先让文案在页面上跑准,
// 再让它自己发出去 —— 和当初做今日看板是同一个顺序(见 daily_plan.go)。

// dailyPushLine 一个人 + 他今天还没巡的设备。
type dailyPushLine struct {
	OwnerName string   `json:"ownerName"`
	Assets    []string `json:"assets"`
}

// dailyPushGroup 一个项目下的待巡情况。
//
// 【按项目分组】一条群消息里混着两个项目的设备,收消息的人得自己挑出
// 跟自己有关的那几行 —— 而现场是在手机上、赶着下班的时候看这条。
type dailyPushGroup struct {
	Project string          `json:"project"`
	Lines   []dailyPushLine `json:"lines"`
	Pending int             `json:"pending"`
}

// dailyPushDigest 今天这条提醒的全部内容。预览和真发用的是同一份。
type dailyPushDigest struct {
	Date    string `json:"date"`
	Weekday int    `json:"weekday"`
	Total   int    `json:"total"`
	Done    int    `json:"done"`
	Pending int    `json:"pending"`
	// Missing 计划里点了、但台账里已经查不到的设备数。
	// 【要单独说】它永远算作未完成,不说的话完成率永远差一截而没人知道为什么。
	Missing int              `json:"missing"`
	Groups  []dailyPushGroup `json:"groups"`
	// Text 最终发出去的原文。预览要给到逐字,不是"大概长这样"。
	Text string `json:"text"`
	// WouldSend 按当前口径,今天这个点会不会真发。
	WouldSend  bool   `json:"wouldSend"`
	SkipReason string `json:"skipReason,omitempty"`
}

var pushWeekdayCN = []string{"", "周一", "周二", "周三", "周四", "周五", "周六", "周日"}

// buildDailyPushDigest 把今日看板变成一条提醒。
//
// 【纯函数,不碰数据库也不看时间】所以它可以被任意构造的看板喂进来测 ——
// 而定时任务本身是很难测的东西,能挪到这里的判断都要挪过来。
//
// silentWhenDone:今天全都巡完了要不要发。默认不发 —— 空洞的推送是让人
// 取关最快的方式;但"今天全部完成"对管理者确实是汇报,所以做成开关,
// 不替人决定。
func buildDailyPushDigest(board *TodayInspectionBoard, silentWhenDone bool) dailyPushDigest {
	d := dailyPushDigest{Groups: []dailyPushGroup{}}
	if board == nil {
		d.SkipReason = "今天没有每日计划"
		return d
	}
	d.Date, d.Weekday, d.Total, d.Done = board.Date, board.Weekday, board.Total, board.Done

	// 项目 → 负责人 → 设备名。
	//
	// 【按设备去重】两条计划可能都点了同一台("每日例检"和"重点关注"),
	// 不去重的话同一台会在消息里出现两次,读的人会以为是两台。
	// 谁负责按先遇到的那条计划算 —— 看板已经把未完成的排在前面。
	type ownerKey struct{ project, owner string }
	byOwner := map[ownerKey][]string{}
	seen := map[string]bool{}
	order := []ownerKey{}
	for _, p := range board.Plans {
		for _, a := range p.Assets {
			if a.Done || seen[a.AssetID] {
				continue
			}
			seen[a.AssetID] = true
			if a.Missing {
				d.Missing++
			}
			k := ownerKey{
				project: firstNonEmpty(a.Project, p.Project, "未指定项目"),
				owner:   firstNonEmpty(p.OwnerName, "未指定负责人"),
			}
			if _, ok := byOwner[k]; !ok {
				order = append(order, k)
			}
			byOwner[k] = append(byOwner[k], a.AssetName)
			d.Pending++
		}
	}

	// 按项目聚起来,项目内按待巡台数多的排前面 —— 谁欠得多谁先被看到
	groups := map[string]*dailyPushGroup{}
	projOrder := []string{}
	for _, k := range order {
		g := groups[k.project]
		if g == nil {
			g = &dailyPushGroup{Project: k.project}
			groups[k.project] = g
			projOrder = append(projOrder, k.project)
		}
		g.Lines = append(g.Lines, dailyPushLine{OwnerName: k.owner, Assets: byOwner[k]})
		g.Pending += len(byOwner[k])
	}
	for _, name := range projOrder {
		g := groups[name]
		sort.SliceStable(g.Lines, func(i, j int) bool {
			return len(g.Lines[i].Assets) > len(g.Lines[j].Assets)
		})
		d.Groups = append(d.Groups, *g)
	}
	sort.SliceStable(d.Groups, func(i, j int) bool { return d.Groups[i].Pending > d.Groups[j].Pending })

	d.Text = renderDailyPushText(d)
	switch {
	case d.Total == 0:
		d.SkipReason = "今天没有排定的每日计划"
	case d.Pending == 0 && silentWhenDone:
		d.SkipReason = "今天已全部巡完(设置为「全部完成时不发」)"
	default:
		d.WouldSend = true
	}
	return d
}

// renderDailyPushText 拼企微群机器人的 markdown。
//
// 【不 @人】@ 需要企业微信的 userid,而现在账号表里一个都没填。
// 用姓名点名是能做到的最强提示 —— 假装能 @ 反而会让人以为被提醒了。
func renderDailyPushText(d dailyPushDigest) string {
	wd := ""
	if d.Weekday >= 1 && d.Weekday < len(pushWeekdayCN) {
		wd = " " + pushWeekdayCN[d.Weekday]
	}
	var b strings.Builder
	if d.Pending == 0 {
		b.WriteString("**今日巡检已全部完成**\n")
		b.WriteString("> " + d.Date + wd + " · 共 " + strconv.Itoa(d.Total) + " 台\n")
		return b.String()
	}
	b.WriteString("**今日巡检未完成**\n")
	b.WriteString("> " + d.Date + wd + " · 共 " + strconv.Itoa(d.Total) +
		" 台,还差 <font color=\"warning\">" + strconv.Itoa(d.Pending) + "</font> 台\n")
	for _, g := range d.Groups {
		b.WriteString("\n**" + g.Project + "**\n")
		for _, ln := range g.Lines {
			b.WriteString("> " + ln.OwnerName + ":" + strings.Join(ln.Assets, "、") + "\n")
		}
	}
	if d.Missing > 0 {
		// 【这一条要说】这些设备永远算不完,完成率永远到不了 100%,
		// 而没人会想到是因为计划里挂着几台已经删掉的设备。
		b.WriteString("\n> 其中 " + strconv.Itoa(d.Missing) +
			" 台已从台账删除但仍挂在计划里,请编辑计划移除\n")
	}
	return b.String()
}

// handleDailyPushPreview —— GET /api/engineering/plans/daily-push/preview
//
// 只算不发。给的是【逐字的原文】,不是"大概长这样" ——
// 要确认的正是那些字会不会出现在领导的群里。
//
// 【一个群一份,和真发走同一条路】原来这里把请求者能看到的所有项目拼成一条、
// 用全局时间算 —— 而真发是一个群一个群发、各看各的项目、各守各的时间和暂停
// (push_runner.go pushOneBot)。于是紫菡那个群明明暂停了,预览里还写着
// "今天 17:00 会发出下面这条",下面列着会议中心和紫菡两段:一条根本不存在的消息。
// 现在每个群按 pushOneBot 的同一套口径算:同样的项目范围、同样的覆盖设置、
// 同样的"今天发过没有"。
func (s *Server) handleDailyPushPreview(w http.ResponseWriter, r *http.Request) {
	tenantID := s.tenantForRequest(r)
	vis := s.visibilityFor(r)
	now := time.Now()

	// 一个群都没配:照旧给一条按请求者可见范围算的,至少能看文案长什么样
	if len(s.weworkBots) == 0 {
		board, err := s.buildTodayBoardFor(tenantID, vis, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "build_failed", err.Error())
			return
		}
		silent := r.URL.Query().Get("silentWhenDone") == "1"
		d := buildDailyPushDigest(board, silent)
		// 【今天不在推送日也要说】这里原来只看有没有内容 —— 执行日选了法定工作日、
		// 今天又放假,页面上还写着"今天 17:00 会发出下面这条"。
		if kv, kvErr := s.store.ListAppSettings(); kvErr == nil {
			global := dailyPushConfigFrom(kv)
			cn := now.In(cnLoc)
			wd := isoWeekday(int(cn.Weekday()))
			if cal := s.workCalendarOn(dayStamp(cn)); !runsOnDay(global.Weekdays, global.FollowCalendar, wd, cal) {
				d.WouldSend = false
				d.SkipReason = "今天不在推送日内"
				if skippedForHoliday(global.Weekdays, global.FollowCalendar, wd, cal) {
					d.SkipReason = "今天" + cal.Name + "放假,跳过法定节假日"
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"bots":   []dailyPushBotPreview{},
			"digest": d,
		})
		return
	}

	kv, err := s.store.ListAppSettings()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "read_settings_failed", err.Error())
		return
	}
	global := dailyPushConfigFrom(kv)
	cal := s.workCalendarOn(dayStamp(now))
	out := make([]dailyPushBotPreview, 0, len(s.weworkBots))
	for _, b := range s.weworkBots {
		// 【只给看自己看得到的群】一个只管会议中心的主管,不该在这里读到
		// 紫菡那个群今天会收到的原文 —— 那等于绕过项目权限看别的项目的待巡清单。
		names, all := s.dailyPushProjectsFor(tenantID, b)
		if all {
			names = nil
		}
		if !visibilityCovers(vis, names) {
			continue
		}
		eff := dailyPushConfigForBot(kv, b.Index)
		board, err := s.buildTodayBoardFor(tenantID, s.dailyPushVisibilityFor(tenantID, b), now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "build_failed", err.Error())
			return
		}
		d := buildDailyPushDigest(board, eff.SilentWhenDone)
		lastDay, _ := s.store.LastPushDay(tenantID, b.SlotKind)
		ready := b.Client != nil && b.Client.Enabled()
		kind, status := describeBotPushToday(global, eff, d, ready, lastDay, now, cal)
		out = append(out, dailyPushBotPreview{
			Index: b.Index, Projects: append([]string{}, names...), AllProjects: all,
			Time: eff.HourMin, Kind: kind, Status: status, Digest: d,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"bots": out})
}

// dailyPushBotPreview 一个群今天会怎样。
type dailyPushBotPreview struct {
	Index    int      `json:"index"`
	Projects    []string `json:"projects"`
	AllProjects bool     `json:"allProjects"` // 收全部项目;false 且 projects 为空 = 一个项目都没分到
	Time        string   `json:"time"`        // 这个群实际几点发(已叠上覆盖值)
	// Kind 机器可读的结论:send 会发 / off 总开关没开 / paused 这个群暂停了 /
	// weekday 今天不在推送日 / sent 今天已发过 / nothing 今天没什么可发 /
	// no_address 群地址没配好
	Kind   string          `json:"kind"`
	Status string          `json:"status"` // 给人看的一句话
	Digest dailyPushDigest `json:"digest"`
}

// describeBotPushToday 这个群今天会不会发、为什么。
//
// 【判断顺序和 pushOneBot 一致】先看开关和暂停,再看日子、发过没有,
// 最后才看有没有内容、地址好不好 —— 顺序不同,给出的理由就会和
// 实际不发的原因对不上("说是没内容,其实是暂停了")。
func describeBotPushToday(global, eff dailyPushConfig, d dailyPushDigest, ready bool, lastDay string, now time.Time, cal WorkCalendarDay) (string, string) {
	now = now.In(cnLoc) // 和真发一样按东八区的日子算
	switch {
	case !global.Enabled:
		return "off", "自动推送还没开 —— 开启后,今天 " + eff.HourMin + " 会发出下面这条"
	case !eff.Enabled:
		return "paused", "这个群已暂停推送,今天不发"
	case !runsOnDay(eff.Weekdays, eff.FollowCalendar, isoWeekday(int(now.Weekday())), cal):
		// 【放假不发要说是放假】只说"不在推送日内",人会去查是不是勾错了周几
		if skippedForHoliday(eff.Weekdays, eff.FollowCalendar, isoWeekday(int(now.Weekday())), cal) {
			return "weekday", "今天" + cal.Name + "放假,这个群跳过法定节假日,不发"
		}
		return "weekday", "今天不在这个群的推送日内,不发"
	case lastDay == now.Format("2006-01-02"):
		return "sent", "今天已经发过了"
	case !d.WouldSend:
		return "nothing", "今天不发 —— " + d.SkipReason
	case !ready:
		return "no_address", "群地址没配好,到点也发不出去"
	default:
		return "send", "今天 " + eff.HourMin + " 会发出下面这条"
	}
}

// dailyPushProjectsFor 这个群的每日提醒该算哪几个项目。all=true 表示收全部项目(不裁)。
//
// 【和异常提醒同一套路由】2026-09-22 起,「项目管理」里能给每个项目选发到哪个群
// (projects.bot_index),异常提醒按它走(botsForProject)。每日提醒原来只看服务器上的
// WEWORK_BOT_[N]_PROJECTS —— 后台改了群,异常提醒跟着换,每日提醒还往原来的群发。
// 规则:后台选过群的项目,只归它选的那个群;没选过的,按环境变量那份。
// 后台一个项目都没选过时,和原来一模一样(直接用环境变量)。
func (s *Server) dailyPushProjectsFor(tenantID string, b weworkBotTarget) (names []string, all bool) {
	env := make([]string, 0, len(b.Projects))
	for _, p := range b.Projects {
		if p = strings.TrimSpace(p); p != "" {
			env = append(env, p)
		}
	}
	projects, err := s.store.ListProjects(tenantID)
	if err != nil {
		return env, len(env) == 0 // 查不到就退回环境变量那份,不让提醒因为一次查库失败而断掉
	}
	configured := false
	for _, p := range projects {
		if p != nil && p.BotIndex > 0 {
			configured = true
			break
		}
	}
	if !configured {
		return env, len(env) == 0
	}
	for _, p := range projects {
		if p == nil {
			continue
		}
		name := strings.TrimSpace(p.Name)
		if p.BotIndex > 0 {
			if p.BotIndex == b.Index {
				names = append(names, name)
			}
			continue
		}
		if len(env) == 0 || slices.Contains(env, name) {
			names = append(names, name)
		}
	}
	return names, false
}

// dailyPushVisibilityFor 把上面那份项目清单变成看板的可见范围。
//
// 【一个项目都没分到要写成 Blocked,不能给空清单】dataVisibility 里 Projects 为空
// 表示"不按项目限" —— 一个被后台把项目全挪走的群,会反过来收到全部项目。
func (s *Server) dailyPushVisibilityFor(tenantID string, b weworkBotTarget) dataVisibility {
	names, all := s.dailyPushProjectsFor(tenantID, b)
	if all {
		return dataVisibility{AllData: true}
	}
	if len(names) == 0 {
		return dataVisibility{Blocked: true, BlockedReason: "这个群没有分到任何项目"}
	}
	return dataVisibility{Projects: names}
}

// visibilityCovers 请求者能不能看全这个群负责的项目。
// 群收全部项目(projects 为空)时,只有不受项目限制的人才看得全。
func visibilityCovers(v dataVisibility, projects []string) bool {
	if v.Blocked || v.OwnOnly {
		return false
	}
	if v.AllData || len(v.Projects) == 0 {
		return true
	}
	if len(projects) == 0 {
		return false
	}
	for _, p := range projects {
		if !slices.Contains(v.Projects, p) {
			return false
		}
	}
	return true
}

// ===== 推送设置 =====

// handleDailyPushConfig —— GET/PUT /api/engineering/plans/daily-push/config
//
// 【放在同一个 handler 里】读和写用的是同一份字段定义,分开两个函数
// 迟早会有一边漏改一个字段 —— 而漏改的表现是"我明明改了,保存后又变回去"。
func (s *Server) handleDailyPushConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		kv, err := s.store.ListAppSettings()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read_settings_failed", err.Error())
			return
		}
		c := dailyPushConfigFrom(kv)
		writeJSON(w, http.StatusOK, map[string]any{
			"enabled": c.Enabled, "time": c.HourMin, "weekdays": c.Weekdays,
			"silentWhenDone": c.SilentWhenDone, "followCalendar": c.FollowCalendar,
			// 【把"通道通不通"一起告诉前端】没配 webhook 的话,开关打开了也发不出去。
			// 不说的话用户会打开开关、等到第二天、然后来问"为什么没发"。
			"botReady": s.weworkBot != nil && s.weworkBot.Enabled(),
			"timezone": pushTZ.String(),
			"bots":     s.botConfigViews(kv, s.tenantForRequest(r)),
		})
		return
	}

	var req struct {
		Enabled        bool   `json:"enabled"`
		Time           string `json:"time"`
		Weekdays       string `json:"weekdays"`
		SilentWhenDone bool   `json:"silentWhenDone"`
		FollowCalendar bool   `json:"followCalendar"`
		// Bots 各个群自己的单独设置。
		//
		// 【不传 = 一个群的设置都别动】老版本后台发上来的请求里没有这个字段,
		// 当成"全部清空"的话,升级那天所有群的单独设置会被静默抹掉。
		Bots []botConfigReq `json:"bots"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_body", err.Error())
		return
	}
	// 【时间格式当场校验,不要存进去再说】存了个 "17点",调度器解析失败后
	// 回落到默认时间 —— 用户以为改成了别的点,实际还是 17:00,而且没有任何提示。
	if !validHourMin(req.Time) {
		writeError(w, http.StatusBadRequest, "bad_time", "推送时间要写成 HH:MM,例如 17:00")
		return
	}
	if msg := validWeekdays(req.Weekdays); msg != "" {
		writeError(w, http.StatusBadRequest, "bad_weekdays", msg)
		return
	}
	cfg := dailyPushConfig{
		Enabled: req.Enabled, HourMin: strings.TrimSpace(req.Time),
		Weekdays: strings.TrimSpace(req.Weekdays), SilentWhenDone: req.SilentWhenDone,
		FollowCalendar: req.FollowCalendar,
	}
	cfg.Weekdays, cfg.FollowCalendar = normalizeDayRule(cfg.Weekdays, cfg.FollowCalendar)
	settings := cfg.toSettings()

	// 【单独设置和全局设置一起存】分两次写的话,中间失败会留下
	// "全局改了、单独的没改"这种一半的状态,而页面显示的是改完的样子。
	for _, b := range req.Bots {
		if !s.knownBotIndex(b.Index) {
			writeError(w, http.StatusBadRequest, "unknown_bot",
				fmt.Sprintf("没有第 %d 个群机器人", b.Index))
			return
		}
		ov, msg := b.toOverride()
		if msg != "" {
			writeError(w, http.StatusBadRequest, "bad_bot_config", msg)
			return
		}
		settings[botOverrideKey(b.Index)] = ov.encode()
	}

	if err := s.store.SetAppSettings(settings, s.currentUserName(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "save_settings_failed", err.Error())
		return
	}
	s.recordOperation(r, "daily_push_config", "app_settings", keyPushEnabled, map[string]any{
		"enabled": cfg.Enabled, "time": cfg.HourMin, "weekdays": cfg.Weekdays,
		"followCalendar": cfg.FollowCalendar, "bots": len(req.Bots),
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// botConfigReq 后台发上来的"第 N 个群的单独设置"。
//
// 【字段是指针】null = 这一项跟随全局。用零值表达的话,"这个群停掉"
// 和"这个群跟随全局"会长得一模一样。
type botConfigReq struct {
	Index          int     `json:"index"`
	Enabled        *bool   `json:"enabled"`
	Time           *string `json:"time"`
	Weekdays       *string `json:"weekdays"`
	SilentWhenDone *bool   `json:"silentWhenDone"`
	FollowCalendar *bool   `json:"followCalendar"`
}

// toOverride 校验并转成存库的形状。第二个返回值非空 = 这份设置有问题。
func (b botConfigReq) toOverride() (dailyPushOverride, string) {
	o := dailyPushOverride{Enabled: b.Enabled, SilentWhenDone: b.SilentWhenDone, FollowCalendar: b.FollowCalendar}
	if b.Time != nil {
		t := strings.TrimSpace(*b.Time)
		if !validHourMin(t) {
			return o, fmt.Sprintf("第 %d 个群的推送时间要写成 HH:MM,例如 18:30", b.Index)
		}
		o.HourMin = &t
	}
	if b.Weekdays != nil {
		wd := strings.TrimSpace(*b.Weekdays)
		if msg := validWeekdays(wd); msg != "" {
			return o, fmt.Sprintf("第 %d 个群:%s", b.Index, msg)
		}
		o.Weekdays = &wd
	}
	o.normalizeLegacyWorkday()
	return o, ""
}

// validWeekdays 返回空串表示没问题。
//
// 【抽出来是因为全局和单独设置得是同一套规则】各写一份的话,
// 单独设置那边迟早会放过一个全局不收的值,而坏值的表现是"那个群不发了"。
//
// 「法定工作日」存的是 dayRuleWorkday,见 work_calendar.go。
func validWeekdays(raw string) string {
	if strings.TrimSpace(raw) == dayRuleWorkday {
		return ""
	}
	for _, part := range strings.Split(strings.TrimSpace(raw), ",") {
		if part = strings.TrimSpace(part); part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err != nil || n < 1 || n > 7 {
			return "执行日只能是 1-7(1=周一,7=周日)"
		}
	}
	return ""
}

func (s *Server) knownBotIndex(index int) bool {
	for _, b := range s.weworkBots {
		if b.Index == index {
			return true
		}
	}
	return false
}

// botConfigViews 后台要显示的每个群:它是谁、收哪些项目、现在实际几点发。
//
// 【绝不返回 webhook】这个接口是给浏览器的,返回值会进控制台、进截图、
// 进任何一次"帮我看看"的粘贴 —— 而 webhook 等价于往那个群发消息的权限。
//
// 【项目名对着库里核一遍再显示,不照抄配置】配置里写的是一串字符串,
// 库里的项目才是真的。两者对不上时这个群一台设备也筛不到、一条提醒也发不出去,
// 而配置本身看上去完全正常 —— 2026-09-20 本地就是这么坏的(见 start-local.ps1
// 里那段编码注释),后台页面把配置原样显示成一串乱码才被发现。
// 现在显示的是库里查到的那个项目,查不到的单独列出来,让这种故障自己说话。
func (s *Server) botConfigViews(kv map[string]string, tenantID string) []map[string]any {
	// 读不到项目不该让整个设置页打不开 —— 退回显示配置原文,
	// 那仍然比什么都不显示有用。
	live := map[string]string{}
	if projects, err := s.store.ListProjects(tenantID); err == nil {
		for _, p := range projects {
			if p != nil {
				live[p.Name] = p.Name
			}
		}
	}

	out := make([]map[string]any, 0, len(s.weworkBots))
	for _, b := range s.weworkBots {
		o := parseDailyPushOverride(kv[botOverrideKey(b.Index)])
		eff := dailyPushConfigForBot(kv, b.Index)
		// 【标题写的必须是真发时算的那几个项目】和 pushOneBot 用同一个函数 ——
		// 后台「项目管理」改过群的,这里跟着变;不然卡片上写着"紫菡雅集",
		// 实际发的是另一批。
		names, all := s.dailyPushProjectsFor(tenantID, b)
		matched, unknown := []string{}, []string{}
		for _, name := range names {
			if real, ok := live[name]; ok {
				matched = append(matched, real)
			} else if name != "" {
				unknown = append(unknown, name)
			}
		}
		out = append(out, map[string]any{
			"index": b.Index,
			"name":  b.Name,
			// projects:库里真实存在的那几个。
			"projects": matched,
			// allProjects:这个群收全部项目(没按项目分)。和"一个项目都没分到"
			// 必须分开说 —— 两者的 projects 都是空的。
			"allProjects": all,
			// unknownProjects:配置里写了、库里却没有的。非空 =
			// 这个群收不到任何东西,而且不会报错。页面必须把它喊出来。
			"unknownProjects": unknown,
			"ready":           b.Client != nil && b.Client.Enabled(),
			// follows:这个群现在是不是完全跟着全局走。前端据此决定
			// "单独设置"那一块是展开还是收着。
			"follows": o.IsEmpty(),
			// override:只有人真的设过的那几项。没设的是 null,不是零值 ——
			// 前端要靠这个区分"关掉了"和"没设过"。
			"override": map[string]any{
				"enabled": o.Enabled, "time": o.HourMin,
				"weekdays": o.Weekdays, "silentWhenDone": o.SilentWhenDone,
				"followCalendar": o.FollowCalendar,
			},
			// effective:全局和覆盖合并之后,这个群实际用的那套。
			// 【必须一起给】只给覆盖值的话,页面上一个群写着"18:30"、
			// 另一个什么都没写,人得自己在脑子里做一遍合并才知道几点发。
			"effective": map[string]any{
				"enabled": eff.Enabled, "time": eff.HourMin,
				"weekdays": eff.Weekdays, "silentWhenDone": eff.SilentWhenDone,
				"followCalendar": eff.FollowCalendar,
			},
		})
	}
	return out
}
