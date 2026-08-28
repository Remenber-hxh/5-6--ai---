package main

import (
	"testing"
	"time"
)

// 「该不该发」的判定。
//
// 【为什么全押在这个纯函数上】定时任务本身没法测 —— 总不能等到 17 点。
// 所以判断都挪进了 shouldFireDailyPush,这里把每一条都钉住。
// 而这些条件错一条的后果都很具体:群里被刷屏、或者该提醒的那天没提醒。

func at(hhmm string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", "2026-08-27 "+hhmm, pushTZ)
	if err != nil {
		panic(err)
	}
	return t // 2026-08-27 是周四
}

func baseCfg() dailyPushConfig {
	c := defaultDailyPushConfig()
	c.Enabled = true
	return c
}

func TestPushNotBeforeTargetTime(t *testing.T) {
	ok, why := shouldFireDailyPush(baseCfg(), at("16:59"), "", 0)
	if ok {
		t.Errorf("没到点就发了")
	}
	if why != "还没到点" {
		t.Errorf("原因应说清楚,实际 %q", why)
	}
	if ok, _ := shouldFireDailyPush(baseCfg(), at("17:00"), "", 0); !ok {
		t.Error("到点了应该发")
	}
}

// 【最要紧的一条】容器 16:59 重启,17:00 定时器重新起算 ——
// 没有这一句,群里就会收到第二遍;崩溃循环时是十遍,
// 而这功能的可信度一次就毁了。
func TestPushOncePerDay(t *testing.T) {
	if ok, why := shouldFireDailyPush(baseCfg(), at("17:05"), "2026-08-27", 0); ok {
		t.Errorf("今天发过了还要发:%s", why)
	}
	// 昨天发过不影响今天
	if ok, _ := shouldFireDailyPush(baseCfg(), at("17:05"), "2026-08-26", 0); !ok {
		t.Error("昨天发过不该挡住今天")
	}
}

func TestPushRespectsWeekdays(t *testing.T) {
	c := baseCfg()
	c.Weekdays = "1,2,3" // 周一到周三;测试日是周四
	if ok, why := shouldFireDailyPush(c, at("17:30"), "", 0); ok {
		t.Errorf("不在推送日内还发:%s", why)
	}
	c.Weekdays = "" // 空 = 每天
	if ok, _ := shouldFireDailyPush(c, at("17:30"), "", 0); !ok {
		t.Error("空的执行日应当每天都发")
	}
}

func TestPushDisabledNeverFires(t *testing.T) {
	c := baseCfg()
	c.Enabled = false
	if ok, _ := shouldFireDailyPush(c, at("17:00"), "", 0); ok {
		t.Error("关掉了还发")
	}
}

// 补发窗口:容器 17:30 才起来,17:00 那次没发,当天内补一次;
// 但 23:59 起来就别补了 —— 补一条谁也来不及处理的提醒只是噪音。
func TestPushCatchUpWindow(t *testing.T) {
	if ok, _ := shouldFireDailyPush(baseCfg(), at("17:30"), "", 120); !ok {
		t.Error("窗口内应当补发")
	}
	if ok, why := shouldFireDailyPush(baseCfg(), at("23:30"), "", 120); ok {
		t.Errorf("已过补发窗口不该发:%s", why)
	}
	// 窗口为 0 = 不限,当天任何时候起来都补
	if ok, _ := shouldFireDailyPush(baseCfg(), at("23:30"), "", 0); !ok {
		t.Error("窗口为 0 时应当不限")
	}
}

// 【脏值一律当没配】存进 "17点" 之后如果解析失败又回落到"永远满足",
// 这条推送会每分钟发一次。
func TestPushRejectsBadTimeValue(t *testing.T) {
	for _, bad := range []string{"17点", "5pm", "25:00", "17:99", "", "17"} {
		if validHourMin(bad) {
			t.Errorf("%q 不该被当成合法时间", bad)
		}
	}
	for _, good := range []string{"00:00", "9:05", "17:00", "23:59"} {
		if !validHourMin(good) {
			t.Errorf("%q 应该是合法时间", good)
		}
	}
	// 脏值要回落到默认 17:00,不能变成"永远满足"
	c := dailyPushConfigFrom(map[string]string{keyPushEnabled: "1", keyPushTime: "17点"})
	if c.HourMin != "17:00" {
		t.Errorf("脏值应回落默认,实际 %q", c.HourMin)
	}
	if ok, _ := shouldFireDailyPush(c, at("09:00"), "", 0); ok {
		t.Error("脏值回落后不该在早上就发")
	}
}

// 默认必须是关的:谁也不希望某次部署完当天就开始往群里发。
func TestPushDefaultsOff(t *testing.T) {
	if defaultDailyPushConfig().Enabled {
		t.Error("默认必须关闭 —— 打开是人做的明确动作")
	}
	if c := dailyPushConfigFrom(map[string]string{}); c.Enabled {
		t.Error("没有任何设置时也必须是关的")
	}
}

// 存进去再读出来要一致 —— 不然界面上关了、下次打开又是开的。
func TestPushConfigRoundTrip(t *testing.T) {
	in := dailyPushConfig{Enabled: true, HourMin: "18:30", Weekdays: "1,3,5", SilentWhenDone: false}
	out := dailyPushConfigFrom(in.toSettings())
	if out != in {
		t.Errorf("往返不一致:\n存 %+v\n读 %+v", in, out)
	}
}
