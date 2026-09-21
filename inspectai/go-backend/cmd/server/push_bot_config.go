package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ===== 一个项目一个时间:按群机器人的单独设置 =====
//
// 【原来是什么样】发到哪个群、群里看得到哪些项目,这两件事已经分开了
// (见 push_targets.go);但"几点发、周几发、要不要发"是一套全局参数 ——
// 紫菡雅集和会议中心必须同一分钟收到提醒,要停也只能一起停。
//
// 现在每个群可以自己带一份【覆盖值】:填了就用自己的,留空就跟着全局走。
// 线上现有的配置一个字都不用改 —— 没有任何覆盖时,行为和以前完全一样。
//
// 【为什么是一个键存一整份 JSON,而不是 daily_push.bot2.time 这样一项一个键】
// app_settings 只能写不能删(SetAppSettings 是 upsert)。一项一个键的话,
// "把这个群改回跟随全局"就没法表达 —— 只能写个空字符串,而空字符串在
// weekdays 上是有意义的("每天"),两种含义撞在同一个值上,迟早出事。
// 整份存的话,"跟随全局"就是这个键为空,干净利落。

// dailyPushOverride 某个群机器人自己的那份设置。
//
// 【每一项都是指针】nil = 这一项跟随全局。用零值表达"没设"是不行的:
// enabled=false 和"没设过 enabled"是两件完全相反的事。
type dailyPushOverride struct {
	Enabled        *bool   `json:"enabled,omitempty"`
	HourMin        *string `json:"time,omitempty"`
	Weekdays       *string `json:"weekdays,omitempty"`
	SilentWhenDone *bool   `json:"silentWhenDone,omitempty"`
}

// IsEmpty 一项都没设 —— 存的时候直接存空串,别往库里塞一个 "{}"。
func (o dailyPushOverride) IsEmpty() bool {
	return o.Enabled == nil && o.HourMin == nil && o.Weekdays == nil && o.SilentWhenDone == nil
}

// botOverrideKey 第 N 个群的设置存在哪个键。
//
// 【这里第 1 个也带编号,和 WEWORK_BOT_WEBHOOK 那边不一样】那边不带编号是
// 为了迁就线上已有的配置,改名等于让推送静默失效。这几个键是全新的,
// 没有存量要迁就,写整齐即可。
func botOverrideKey(index int) string {
	return fmt.Sprintf("daily_push.bot%d", index)
}

// parseDailyPushOverride 读一份覆盖值。
//
// 【解析不了就当没设】库里躺着一行手改坏的 JSON,不该让这个群从此按
// 一份半成品的参数发消息 —— 退回全局那套是个可预测的行为。
func parseDailyPushOverride(raw string) dailyPushOverride {
	var o dailyPushOverride
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return o
	}
	if err := json.Unmarshal([]byte(raw), &o); err != nil {
		return dailyPushOverride{}
	}
	// 时间脏了就丢掉这一项,别丢掉整份 —— 星期和开关是好的,没理由陪葬。
	if o.HourMin != nil && !validHourMin(*o.HourMin) {
		o.HourMin = nil
	}
	return o
}

func (o dailyPushOverride) encode() string {
	if o.IsEmpty() {
		return ""
	}
	b, err := json.Marshal(o)
	if err != nil {
		return ""
	}
	return string(b)
}

// dailyPushConfigForBot 全局那套 + 这个群自己的覆盖。
//
// 【开关是"与",不是覆盖】全局开关是总闸:关了就谁也别发。单个群的开关
// 只能把自己关掉,不能在总闸关着的时候把自己打开。
//
// 这么定是因为两者的用途不一样 —— 总闸是"这套功能现在该不该动",
// 单群开关是"这个群这阵子别打扰"。让单群开关能反向打开总闸的话,
// 关掉总闸就不再等于停止推送,而运维最需要的恰恰是那个能一把停住的东西。
func dailyPushConfigForBot(kv map[string]string, index int) dailyPushConfig {
	c := dailyPushConfigFrom(kv)
	o := parseDailyPushOverride(kv[botOverrideKey(index)])
	if o.Enabled != nil {
		c.Enabled = c.Enabled && *o.Enabled
	}
	if o.HourMin != nil {
		c.HourMin = *o.HourMin
	}
	if o.Weekdays != nil {
		c.Weekdays = strings.TrimSpace(*o.Weekdays)
	}
	if o.SilentWhenDone != nil {
		c.SilentWhenDone = *o.SilentWhenDone
	}
	return c
}
