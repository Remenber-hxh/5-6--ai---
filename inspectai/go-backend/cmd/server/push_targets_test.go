package main

import (
	"strings"
	"testing"
)

// 配置读取器的替身:只认给定的键,别的返回空。
func fakeEnv(kv map[string]string) func(string, string) string {
	return func(k, def string) string {
		if v, ok := kv[k]; ok {
			return v
		}
		return def
	}
}

// 【最要紧的一条】线上那台已经配好了 WEWORK_BOT_WEBHOOK。
// 改成多机器人之后,不改任何配置的行为必须和以前【完全一样】——
// 否则升级那天推送静默失效,而健康检查照样显示正常。
func TestSingleBotConfigKeepsOldBehaviour(t *testing.T) {
	bots := loadWeWorkBotTargets(fakeEnv(map[string]string{
		"WEWORK_BOT_WEBHOOK": "https://example.invalid/hook-a",
	}))
	if len(bots) != 1 {
		t.Fatalf("只配了一个,却读出 %d 个", len(bots))
	}
	if len(bots[0].Projects) != 0 {
		t.Errorf("没配项目就该是全部项目,得到 %v", bots[0].Projects)
	}
	if !bots[0].visibility().AllData {
		t.Error("没配项目的机器人应该看全量数据 —— 那是升级前的行为")
	}
	// 记账 kind 也不能变:变了的话升级当天会被当成"还没发过",再发一遍
	if bots[0].SlotKind != pushKindDailyUndone {
		t.Errorf("第一个机器人的记账 kind 变了:%q —— 升级当天会重复推送", bots[0].SlotKind)
	}
}

// 两个群各绑各的项目。
func TestTwoBotsEachScopedToItsProject(t *testing.T) {
	bots := loadWeWorkBotTargets(fakeEnv(map[string]string{
		"WEWORK_BOT_WEBHOOK":    "https://example.invalid/hook-a",
		"WEWORK_BOT_PROJECTS":   "会议中心",
		"WEWORK_BOT_2_WEBHOOK":  "https://example.invalid/hook-b",
		"WEWORK_BOT_2_PROJECTS": "紫菡雅集",
	}))
	if len(bots) != 2 {
		t.Fatalf("配了两个,读出 %d 个", len(bots))
	}
	if bots[0].visibility().AllData || !bots[0].visibility().allowsProject("会议中心") {
		t.Error("第 1 个群没有被限到会议中心")
	}
	if bots[0].visibility().allowsProject("紫菡雅集") {
		t.Error("会议中心那个群能看到紫菡的数据 —— 那就是混着发,等于没分")
	}
	if !bots[1].visibility().allowsProject("紫菡雅集") {
		t.Error("第 2 个群看不到紫菡")
	}

	// 【记账必须分开】共用一个 kind 的话,发完第一个群就记成"今天已发",
	// 第二个群永远收不到,而日志显示成功。
	if bots[0].SlotKind == bots[1].SlotKind {
		t.Errorf("两个机器人共用记账 kind %q —— 第二个群永远收不到", bots[0].SlotKind)
	}
}

// 日志里只能出现项目名,绝不能出现地址。
//
// 【为什么单独测】日志会被复制进工单、截图发到群里。凭证一旦进日志
// 就等于公开了,而这种泄露没有任何报错,通常是很久以后才被发现。
func TestBotNameNeverLeaksWebhook(t *testing.T) {
	const secret = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=SUPER-SECRET-123"
	bots := loadWeWorkBotTargets(fakeEnv(map[string]string{
		"WEWORK_BOT_WEBHOOK":  secret,
		"WEWORK_BOT_PROJECTS": "会议中心",
	}))
	if len(bots) != 1 {
		t.Fatal("夹具没生效")
	}
	if strings.Contains(bots[0].Name, "SUPER-SECRET") || strings.Contains(bots[0].Name, "qyapi") {
		t.Errorf("机器人显示名里带上了 webhook:%s", bots[0].Name)
	}
	if !strings.Contains(bots[0].Name, "会议中心") {
		t.Errorf("显示名里没有项目名,运维看到报错也不知道是哪个群:%s", bots[0].Name)
	}
}

// 中间空一个编号不该让后面的失效 —— 删掉第 2 个群之后,第 3 个还得能用。
func TestGapInBotNumberingDoesNotStopLater(t *testing.T) {
	bots := loadWeWorkBotTargets(fakeEnv(map[string]string{
		"WEWORK_BOT_WEBHOOK":    "https://example.invalid/a",
		"WEWORK_BOT_3_WEBHOOK":  "https://example.invalid/c",
		"WEWORK_BOT_3_PROJECTS": "紫菡雅集",
	}))
	if len(bots) != 2 {
		t.Fatalf("中间空号把后面的弄丢了,读出 %d 个", len(bots))
	}
	if !bots[1].visibility().allowsProject("紫菡雅集") {
		t.Error("第 3 个群的项目没读对")
	}
}

// 一个都没配 = 空列表,不是崩溃、也不是造一个发不出去的假机器人。
func TestNoBotConfigured(t *testing.T) {
	if bots := loadWeWorkBotTargets(fakeEnv(nil)); len(bots) != 0 {
		t.Errorf("没配却读出 %d 个", len(bots))
	}
}

// 项目名支持中英文逗号和分号分隔 —— 现场配置时全角半角都可能打出来,
// 因为这个而静默漏掉一个项目太不值。
func TestProjectListSeparators(t *testing.T) {
	for _, raw := range []string{"会议中心,紫菡雅集", "会议中心，紫菡雅集", "会议中心; 紫菡雅集", " 会议中心 ；紫菡雅集 "} {
		got := splitProjects(raw)
		if len(got) != 2 || got[0] != "会议中心" || got[1] != "紫菡雅集" {
			t.Errorf("分隔符 %q 没解对:%v", raw, got)
		}
	}
	if got := splitProjects("  "); len(got) != 0 {
		t.Errorf("空配置该是空列表(=全部项目),得到 %v", got)
	}
}
