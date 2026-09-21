package main

import (
	"testing"
	"time"
)

// ===== 一个项目一个时间:全局设置 + 每个群自己的覆盖 =====
//
// 【最要紧的一条在最前面】线上现在没有任何覆盖值。所以"没设覆盖时
// 和以前一字不差"这条要是坏了,表现是两个群在错的时间发、或者干脆不发 ——
// 而后台页面上看起来一切正常。

func globalKV() map[string]string {
	return map[string]string{
		keyPushEnabled:  "1",
		keyPushTime:     "17:00",
		keyPushWeekdays: "1,2,3,4,5",
		keyPushSilent:   "1",
	}
}

func bptr(v bool) *bool     { return &v }
func sptr(v string) *string { return &v }

// 【线上现状】一条覆盖都没有 → 每个群拿到的就是全局那套。
func TestNoOverrideMatchesGlobal(t *testing.T) {
	kv := globalKV()
	want := dailyPushConfigFrom(kv)
	for _, idx := range []int{1, 2, 3} {
		if got := dailyPushConfigForBot(kv, idx); got != want {
			t.Errorf("第 %d 个群没设覆盖却和全局不一样\n全局 %+v\n实得 %+v", idx, want, got)
		}
	}
}

// 两个群各按各的时间发 —— 这就是这次要的东西。
func TestEachBotKeepsItsOwnTime(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = dailyPushOverride{HourMin: sptr("18:30")}.encode()

	if got := dailyPushConfigForBot(kv, 1).HourMin; got != "17:00" {
		t.Errorf("没设覆盖的群时间被带偏了:%s", got)
	}
	if got := dailyPushConfigForBot(kv, 2).HourMin; got != "18:30" {
		t.Errorf("设了 18:30 的群没用上自己的时间:%s", got)
	}
}

// 只覆盖时间,别的项必须还跟着全局 —— 否则改个时间会把"周末不发"一起弄丢。
func TestPartialOverrideLeavesRestAlone(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = dailyPushOverride{HourMin: sptr("09:15")}.encode()
	got := dailyPushConfigForBot(kv, 2)
	if got.Weekdays != "1,2,3,4,5" {
		t.Errorf("没覆盖的执行日被改了:%q", got.Weekdays)
	}
	if !got.SilentWhenDone || !got.Enabled {
		t.Errorf("没覆盖的开关被改了:%+v", got)
	}
}

// 【总闸关了谁也别发】单个群把自己设成 enabled=true 也不行 ——
// 否则"关掉总开关"就不再等于停止推送,而那正是运维最需要的那个动作。
func TestGlobalSwitchIsMaster(t *testing.T) {
	kv := globalKV()
	kv[keyPushEnabled] = "0"
	kv[botOverrideKey(2)] = dailyPushOverride{Enabled: bptr(true)}.encode()
	if dailyPushConfigForBot(kv, 2).Enabled {
		t.Error("总闸关着,单个群却把自己打开了")
	}
}

// 单个群可以只把自己停掉,不影响别的群。
func TestBotCanMuteItself(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = dailyPushOverride{Enabled: bptr(false)}.encode()
	if dailyPushConfigForBot(kv, 2).Enabled {
		t.Error("这个群设了不发,却还是开着")
	}
	if !dailyPushConfigForBot(kv, 1).Enabled {
		t.Error("停掉第 2 个群把第 1 个也带停了")
	}
}

// 【坏值退回全局,不是退回零值】库里躺着一行手改坏的 JSON,
// 这个群该按全局那套照常发,而不是按一份半成品的参数发。
func TestBrokenOverrideFallsBackToGlobal(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = `{"time": 这不是 JSON`
	if got, want := dailyPushConfigForBot(kv, 2), dailyPushConfigFrom(kv); got != want {
		t.Errorf("坏 JSON 没退回全局\n想要 %+v\n实得 %+v", want, got)
	}
}

// 时间写坏了只丢时间这一项,星期和开关没理由陪葬。
func TestBadTimeDropsOnlyTheTime(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = `{"time":"下午五点","weekdays":"6,7"}`
	got := dailyPushConfigForBot(kv, 2)
	if got.HourMin != "17:00" {
		t.Errorf("坏时间没退回全局:%q", got.HourMin)
	}
	if got.Weekdays != "6,7" {
		t.Errorf("好好的执行日被坏时间带下水了:%q", got.Weekdays)
	}
}

// 一项都没设就存空串,别往库里塞 "{}" —— 空串才是"跟随全局"的写法,
// 而 "{}" 会让"这个群设过没有"变成要解析之后才知道的事。
func TestEmptyOverrideEncodesToEmptyString(t *testing.T) {
	if got := (dailyPushOverride{}).encode(); got != "" {
		t.Errorf("空覆盖存成了 %q", got)
	}
	if !parseDailyPushOverride("").IsEmpty() {
		t.Error("空串读出来不是「跟随全局」")
	}
}

// 存进去再读出来,四项都还在(含 false —— 它最容易在往返里被当成"没设")。
func TestOverrideRoundTrip(t *testing.T) {
	in := dailyPushOverride{
		Enabled: bptr(false), HourMin: sptr("08:05"),
		Weekdays: sptr("6,7"), SilentWhenDone: bptr(false),
	}
	out := parseDailyPushOverride(in.encode())
	if out.Enabled == nil || *out.Enabled != false {
		t.Errorf("enabled=false 往返后丢了:%v", out.Enabled)
	}
	if out.SilentWhenDone == nil || *out.SilentWhenDone != false {
		t.Errorf("silentWhenDone=false 往返后丢了:%v", out.SilentWhenDone)
	}
	if out.HourMin == nil || *out.HourMin != "08:05" {
		t.Errorf("时间往返后变了:%v", out.HourMin)
	}
	if out.Weekdays == nil || *out.Weekdays != "6,7" {
		t.Errorf("执行日往返后变了:%v", out.Weekdays)
	}
}

// 【端到端的那一问】17:00 这一分钟,该发的是哪个群?
// 合并逻辑对了但接不到 shouldFireDailyPush 上的话,前面那些全白测。
func TestOnlyTheDueBotFiresAtThatMinute(t *testing.T) {
	kv := globalKV()
	kv[botOverrideKey(2)] = dailyPushOverride{HourMin: sptr("18:30")}.encode()
	at := func(hhmm string) time.Time {
		ts, err := time.ParseInLocation("2006-01-02 15:04", "2026-09-21 "+hhmm, pushTZ)
		if err != nil {
			t.Fatalf("造时间失败: %v", err)
		}
		if ts.Weekday() != time.Monday {
			t.Fatalf("这个日期不是周一(%s),测的就不是工作日那条路径了", ts.Weekday())
		}
		return ts
	}
	// 【补发窗口给 1 分钟,不能给 0】给 0 的话 shouldFireDailyPush 里
	// `catchUpMinutes > 0` 这个判断直接跳过窗口检查 —— 于是 17:00 的群
	// 在 18:30 也算"该发"(当成补发),这条测试就永远证明不了各按各的时间。
	fires := func(idx int, hhmm string) bool {
		ok, _ := shouldFireDailyPush(dailyPushConfigForBot(kv, idx), at(hhmm), "", 1)
		return ok
	}
	if !fires(1, "17:00") || fires(2, "17:00") {
		t.Error("17:00 该只有第 1 个群发")
	}
	if fires(1, "18:30") || !fires(2, "18:30") {
		t.Error("18:30 该只有第 2 个群发")
	}
}
