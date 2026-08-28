package main

import (
	"strconv"
	"strings"
	"time"

	// 【必须内嵌时区库】Windows 上没有系统 tzdata,LoadLocation("Asia/Shanghai")
	// 会直接失败。而这个功能的正确性完全建立在"东八区的 17:00"上 ——
	// 本地开发机是太平洋时区,不内嵌的话本地根本跑不起来,也就测不了。
	_ "time/tzdata"
)

// ===== 什么时候该发 =====
//
// 定时任务本身很难测(总不能等到 17 点),所以判定被抽成一个纯函数:
// 给它任意时间点和上次发送日期,它就能回答"现在该不该发"。
// 能挪进这里的判断都挪了进来。

const pushKindDailyUndone = "daily_undone"

// pushTZ 推送一律按东八区算。
//
// 【不吃 time.Now() 的本地时区】开发机是太平洋时区,服务器靠容器 TZ ——
// 用本地时区的话,本地测试会在太平洋 17:00 触发,测的和线上跑的不是一回事;
// 而容器 TZ 万一哪天没配对,线上会安静地在错误的时间发。
// 显式写死,两边行为一致。
var pushTZ = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		// 内嵌了 tzdata 还失败,说明构建有问题。固定 +8 兜底,总好过用本地时区。
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

// dailyPushConfig 运营参数。存在 app_settings 里(迁移 019 建的表)。
type dailyPushConfig struct {
	Enabled bool
	// HourMin 形如 "17:00"
	HourMin string
	// Weekdays "1,2,3,4,5";空 = 每天
	Weekdays string
	// SilentWhenDone 今天全巡完了就不发
	SilentWhenDone bool
}

const (
	keyPushEnabled  = "daily_push.enabled"
	keyPushTime     = "daily_push.time"
	keyPushWeekdays = "daily_push.weekdays"
	keyPushSilent   = "daily_push.silent_when_done"
)

func defaultDailyPushConfig() dailyPushConfig {
	return dailyPushConfig{
		// 【默认关】接上定时之后,谁也不希望某次部署完当天就开始往群里发。
		// 打开这件事必须是人做的一个明确动作。
		Enabled:        false,
		HourMin:        "17:00",
		Weekdays:       "1,2,3,4,5",
		SilentWhenDone: true,
	}
}

func dailyPushConfigFrom(kv map[string]string) dailyPushConfig {
	c := defaultDailyPushConfig()
	if v, ok := kv[keyPushEnabled]; ok {
		c.Enabled = v == "1" || strings.EqualFold(v, "true")
	}
	if v := strings.TrimSpace(kv[keyPushTime]); validHourMin(v) {
		c.HourMin = v
	}
	if v, ok := kv[keyPushWeekdays]; ok {
		c.Weekdays = strings.TrimSpace(v)
	}
	if v, ok := kv[keyPushSilent]; ok {
		c.SilentWhenDone = v == "1" || strings.EqualFold(v, "true")
	}
	return c
}

func (c dailyPushConfig) toSettings() map[string]string {
	b := func(v bool) string {
		if v {
			return "1"
		}
		return "0"
	}
	return map[string]string{
		keyPushEnabled:  b(c.Enabled),
		keyPushTime:     c.HourMin,
		keyPushWeekdays: c.Weekdays,
		keyPushSilent:   b(c.SilentWhenDone),
	}
}

// validHourMin 只认 HH:MM。
//
// 【脏值一律当没配】存进一个 "17点" 或 "5pm",解析失败之后如果回落到
// "永远满足",这条推送会每分钟发一次。宁可用默认值。
func validHourMin(v string) bool {
	parts := strings.Split(strings.TrimSpace(v), ":")
	if len(parts) != 2 {
		return false
	}
	h, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	return err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59
}

func parseHourMin(v string) (int, int) {
	parts := strings.Split(v, ":")
	h, _ := strconv.Atoi(parts[0])
	m, _ := strconv.Atoi(parts[1])
	return h, m
}

// shouldFireDailyPush 现在该不该发。
//
// now 传的是【东八区】的时间;lastDay 是 push_log 里最近一次占位的日期。
//
// 【补发窗口 catchUpMinutes】容器 17:30 才起来,17:00 那一次没发。
// 当天内补发一次 —— "今天谁没巡"到 18 点仍然有用;跨天不补,
// 第二天早上收到昨天的提醒是纯噪音。
func shouldFireDailyPush(c dailyPushConfig, now time.Time, lastDay string, catchUpMinutes int) (bool, string) {
	if !c.Enabled {
		return false, "未启用"
	}
	today := now.Format("2006-01-02")
	if lastDay == today {
		// 【最要紧的一条】容器 16:59 重启,17:00 定时器重新起算 ——
		// 没有这一句,群里就会收到第二遍;崩溃循环时是十遍。
		return false, "今天已经发过"
	}
	wd := isoWeekday(int(now.Weekday()))
	if !runsOnWeekday(c.Weekdays, wd) {
		return false, "今天不在推送日内"
	}
	h, m := parseHourMin(c.HourMin)
	target := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if now.Before(target) {
		return false, "还没到点"
	}
	// 超过补发窗口就不补了。窗口给得宽(默认到当天结束前),
	// 但仍然要有个上界:否则 23:59 起来的容器会补发一条谁也来不及处理的提醒。
	if catchUpMinutes > 0 && now.Sub(target) > time.Duration(catchUpMinutes)*time.Minute {
		return false, "已过补发窗口"
	}
	return true, ""
}
