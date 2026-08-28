package main

import (
	"net/http"
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
func (s *Server) handleDailyPushPreview(w http.ResponseWriter, r *http.Request) {
	// 【用请求者的可见范围算,不是"全部数据"】预览是给人看的,
	// 应该和他在页面上看到的今日看板完全一致。真发时才用系统视角
	// (那时它代表系统本身,不代表某个人)。
	board, err := s.buildTodayBoardFor(s.tenantForRequest(r), s.visibilityFor(r), time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build_failed", err.Error())
		return
	}
	silent := r.URL.Query().Get("silentWhenDone") == "1"
	writeJSON(w, http.StatusOK, buildDailyPushDigest(board, silent))
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
			"silentWhenDone": c.SilentWhenDone,
			// 【把"通道通不通"一起告诉前端】没配 webhook 的话,开关打开了也发不出去。
			// 不说的话用户会打开开关、等到第二天、然后来问"为什么没发"。
			"botReady": s.weworkBot != nil && s.weworkBot.Enabled(),
			"timezone": pushTZ.String(),
		})
		return
	}

	var req struct {
		Enabled        bool   `json:"enabled"`
		Time           string `json:"time"`
		Weekdays       string `json:"weekdays"`
		SilentWhenDone bool   `json:"silentWhenDone"`
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
	for _, part := range strings.Split(strings.TrimSpace(req.Weekdays), ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if n, err := strconv.Atoi(part); err != nil || n < 1 || n > 7 {
			writeError(w, http.StatusBadRequest, "bad_weekdays", "执行日只能是 1-7(1=周一,7=周日)")
			return
		}
	}
	cfg := dailyPushConfig{
		Enabled: req.Enabled, HourMin: strings.TrimSpace(req.Time),
		Weekdays: strings.TrimSpace(req.Weekdays), SilentWhenDone: req.SilentWhenDone,
	}
	if err := s.store.SetAppSettings(cfg.toSettings(), s.currentUserName(r)); err != nil {
		writeError(w, http.StatusInternalServerError, "save_settings_failed", err.Error())
		return
	}
	s.recordOperation(r, "daily_push_config", "app_settings", keyPushEnabled, map[string]any{
		"enabled": cfg.Enabled, "time": cfg.HourMin, "weekdays": cfg.Weekdays,
	})
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
