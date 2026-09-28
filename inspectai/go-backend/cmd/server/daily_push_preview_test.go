package main

import (
	"slices"
	"testing"
	"time"
)

// ===== 每日提醒预览:一个群一份,说的必须和真发一致 =====
//
// 2026-09-28:紫菡那个群开了「暂停推送」,预览却写着"今天 17:00 会发出下面这条",
// 下面是会议中心 + 紫菡两段 —— 那条消息根本不存在。真发是一个群一个群算的,
// 预览原来是全部项目拼一条、只看全局设置。

func TestPreviewSaysPausedGroupWillNotSend(t *testing.T) {
	global := dailyPushConfig{Enabled: true, HourMin: "17:00"}
	paused := dailyPushConfig{Enabled: false, HourMin: "17:00"}
	d := dailyPushDigest{WouldSend: true}
	mon := time.Date(2026, 9, 28, 9, 0, 0, 0, time.Local) // 周一

	if kind, _ := describeBotPushToday(global, paused, d, true, "", mon); kind != "paused" {
		t.Errorf("暂停的群被说成了 %q —— 预览会谎报这个群今天要发", kind)
	}
	if kind, _ := describeBotPushToday(global, global, d, true, "", mon); kind != "send" {
		t.Errorf("正常的群应该是 send,得到 %q", kind)
	}
}

// 【理由要和真正不发的原因一致】总开关关着时,不管这个群暂停没有,
// 都得先说"没开";发过了就说发过了,不能说成"没内容"。
func TestPreviewReasonOrderMatchesSender(t *testing.T) {
	mon := time.Date(2026, 9, 28, 18, 0, 0, 0, time.Local)
	on := dailyPushConfig{Enabled: true, HourMin: "17:00"}
	off := dailyPushConfig{Enabled: false, HourMin: "17:00"}
	cases := []struct {
		global, eff dailyPushConfig
		d           dailyPushDigest
		ready       bool
		lastDay     string
		want        string
	}{
		{off, off, dailyPushDigest{WouldSend: true}, true, "", "off"},
		{on, dailyPushConfig{Enabled: true, HourMin: "17:00", Weekdays: "2,3"}, dailyPushDigest{WouldSend: true}, true, "", "weekday"},
		{on, on, dailyPushDigest{WouldSend: false, SkipReason: "x"}, true, "2026-09-28", "sent"},
		{on, on, dailyPushDigest{WouldSend: false, SkipReason: "今天没有排定的每日计划"}, true, "", "nothing"},
		{on, on, dailyPushDigest{WouldSend: true}, false, "", "no_address"},
	}
	for i, c := range cases {
		if got, _ := describeBotPushToday(c.global, c.eff, c.d, c.ready, c.lastDay, mon); got != c.want {
			t.Errorf("#%d 得到 %q,应该是 %q", i, got, c.want)
		}
	}
}

// 【每日提醒和异常提醒同一套路由】后台「项目管理」给项目选了群,每日提醒也得跟着走。
// 原来只看服务器上的 WEWORK_BOT_[N]_PROJECTS:收"全部项目"的第 1 个群会把
// 已经分给第 2 个群的紫菡也算进去。
func TestDailyPushFollowsAdminProjectRouting(t *testing.T) {
	allBot := weworkBotTarget{Index: 1}                                  // 服务器上没写项目 = 全部
	zihanBot := weworkBotTarget{Index: 2, Projects: []string{"紫菡雅集"}} // 服务器上写了紫菡

	// 后台一个都没选过:和原来完全一样
	s := projectRouteServer(t, map[string]int{"会议中心": 0, "紫菡雅集": 0})
	if _, all := s.dailyPushProjectsFor(defaultTenantID, allBot); !all {
		t.Error("后台没选过群时,没写项目的群应该照旧收全部")
	}

	// 后台把紫菡选到第 2 个群:第 1 个群就不能再算紫菡
	s = projectRouteServer(t, map[string]int{"会议中心": 0, "紫菡雅集": 2})
	names, all := s.dailyPushProjectsFor(defaultTenantID, allBot)
	if all || !slices.Equal(names, []string{"会议中心"}) {
		t.Errorf("第 1 个群应该只剩会议中心,得到 %v all=%v —— 会议中心群会收到紫菡的待巡", names, all)
	}
	names, _ = s.dailyPushProjectsFor(defaultTenantID, zihanBot)
	if !slices.Equal(names, []string{"紫菡雅集"}) {
		t.Errorf("第 2 个群应该是紫菡,得到 %v", names)
	}

	// 两个项目都选到第 2 个群:第 1 个群一个项目都没有 —— 必须是"什么都不看",不是"看全部"
	s = projectRouteServer(t, map[string]int{"会议中心": 2, "紫菡雅集": 2})
	if v := s.dailyPushVisibilityFor(defaultTenantID, allBot); !v.Blocked {
		t.Errorf("一个项目都没分到的群,可见范围应该是 Blocked,得到 %+v —— 空清单等于看全部", v)
	}
}

// 【只给看自己看得到的群】只管会议中心的人,看不到紫菡那个群的原文。
func TestPreviewHidesGroupsOutsideViewersProjects(t *testing.T) {
	mgr := dataVisibility{Projects: []string{"会议中心"}}
	if visibilityCovers(mgr, []string{"紫菡雅集"}) {
		t.Error("只管会议中心的人看到了紫菡那个群的预览")
	}
	if !visibilityCovers(mgr, []string{"会议中心"}) {
		t.Error("自己项目的群看不到")
	}
	if visibilityCovers(mgr, nil) {
		t.Error("收全部项目的群里有别的项目,受限的人不该看到")
	}
	if !visibilityCovers(dataVisibility{AllData: true}, []string{"紫菡雅集"}) {
		t.Error("管理员应该看得到所有群")
	}
}
