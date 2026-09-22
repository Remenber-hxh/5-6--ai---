package main

import (
	"path/filepath"
	"testing"
)

// ===== 提醒不能发进别的项目的群 =====
//
// 2026-09-22 现场:紫菡雅集的异常提醒发进了会议中心的群。
// "一个项目一个群"当初只做了每日未巡提醒那条路,异常提醒/修改申请走的
// sendWeWorkBotMarkdownAsync 里写死第 1 个群(线上绑的是会议中心)。
//
// 这不只是发错群 —— 群里能看到别的项目的设备名、巡检人、点位。
// 权限那套按项目隔离的口径,在推送这条路上等于没有。

func routeServer(bots ...weworkBotTarget) *Server {
	return &Server{
		// s.weworkBot 是第 1 个群,手动发消息那几个接口还在用它。
		// 这里给上,正是为了证明按项目路由【不会】退回到它。
		weworkBot:  NewWeWorkBotClient("https://example.invalid/bot1"),
		weworkBots: bots,
	}
}

func twoProjectBots() []weworkBotTarget {
	return []weworkBotTarget{
		{Index: 1, Name: "机器人1(会议中心)", Projects: []string{"会议中心"},
			Client: NewWeWorkBotClient("https://example.invalid/hy")},
		{Index: 2, Name: "机器人2(紫菡雅集)", Projects: []string{"紫菡雅集"},
			Client: NewWeWorkBotClient("https://example.invalid/zh")},
	}
}

func botNames(bots []weworkBotTarget) []string {
	out := make([]string, 0, len(bots))
	for _, b := range bots {
		out = append(out, b.Name)
	}
	return out
}

// 【最要紧的一条】紫菡的提醒只能进紫菡的群。
func TestAlertGoesOnlyToItsOwnProjectGroup(t *testing.T) {
	s := routeServer(twoProjectBots()...)

	got := s.botsForProject("紫菡雅集")
	if len(got) != 1 || got[0].Index != 2 {
		t.Fatalf("紫菡的提醒发错群了:%v", botNames(got))
	}

	got = s.botsForProject("会议中心")
	if len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("会议中心的提醒发错群了:%v", botNames(got))
	}
}

// 【配了分项目却没有一个匹配时,宁可不发】退回第 1 个群就是把 A 项目的内容
// 发进 B 项目的群 —— 正是这次要修的事。项目改名/打错字就会走到这里。
func TestUnknownProjectDoesNotFallBackToFirstGroup(t *testing.T) {
	s := routeServer(twoProjectBots()...)
	if got := s.weworkTargetsFor("inspection.submitted", "某个没登记的项目"); len(got) != 0 {
		t.Errorf("项目对不上时退回了别的群,会把内容发给不相干的人:%d 个目标", len(got))
	}
}

// 项目不明(拿不到项目名)时,只发给"收全部项目"的群,不塞进某个项目专用的群。
func TestUnknownProjectOnlyReachesCatchAllGroup(t *testing.T) {
	bots := append(twoProjectBots(), weworkBotTarget{
		Index: 3, Name: "机器人3(全部项目)",
		Client: NewWeWorkBotClient("https://example.invalid/all"),
	})
	s := routeServer(bots...)

	got := s.botsForProject("")
	if len(got) != 1 || got[0].Index != 3 {
		t.Fatalf("项目不明时该只发给收全部项目的群,实得 %v", botNames(got))
	}
	// 收全部项目的群,任何项目的提醒都该收到
	if got := s.botsForProject("紫菡雅集"); len(got) != 2 {
		t.Errorf("紫菡的提醒该同时进专用群和全量群,实得 %v", botNames(got))
	}
}

// 【一个分项目机器人都没配时退回旧行为】线上有一阵子只有 WEWORK_BOT_WEBHOOK。
// 这种部署不该因为这次改动就再也收不到提醒。
func TestLegacySingleBotStillWorks(t *testing.T) {
	s := &Server{weworkBot: NewWeWorkBotClient("https://example.invalid/bot1")}
	if got := s.weworkTargetsFor("inspection.submitted", "紫菡雅集"); len(got) != 1 {
		t.Errorf("只配了单群的老部署收不到提醒了:%d", len(got))
	}
	if !s.weworkBotAvailable() {
		t.Error("单群部署被判成没有可用机器人")
	}
}

// 【只配了第 2 个群、没配第 1 个】原来的判断只看 s.weworkBot,
// 这种部署会一条提醒都发不出去,而日志里什么都没有。
func TestOnlySecondBotConfiguredStillSends(t *testing.T) {
	s := &Server{
		weworkBot: NewWeWorkBotClient(""), // 第 1 个群没配
		weworkBots: []weworkBotTarget{
			{Index: 2, Name: "机器人2(紫菡雅集)", Projects: []string{"紫菡雅集"},
				Client: NewWeWorkBotClient("https://example.invalid/zh")},
		},
	}
	if !s.weworkBotAvailable() {
		t.Fatal("配了第 2 个群却被判成没有可用机器人 —— 一条提醒都发不出去")
	}
	if got := s.weworkTargetsFor("inspection.submitted", "紫菡雅集"); len(got) != 1 {
		t.Errorf("第 2 个群没收到:%d", len(got))
	}
}

// 地址没配好的群不算数 —— 算进去会让"没有合适的群"这个判断失真。
func TestDisabledBotIsSkipped(t *testing.T) {
	s := routeServer(
		weworkBotTarget{Index: 1, Name: "机器人1", Projects: []string{"紫菡雅集"},
			Client: NewWeWorkBotClient("")},
	)
	if got := s.botsForProject("紫菡雅集"); len(got) != 0 {
		t.Errorf("地址没配的群被当成可用:%v", botNames(got))
	}
}

// 修改申请拿不到项目名就会发错群:资产的 TargetID 第一段就是项目。
func TestChangeRequestProjectFromAssetID(t *testing.T) {
	s := &Server{}
	cr := &ChangeRequest{TargetType: "asset", TargetID: "紫菡雅集::zihan_energy::Z1"}
	if got := s.changeRequestProject(cr); got != "紫菡雅集" {
		t.Errorf("没从资产 ID 里取出项目:%q", got)
	}
	// 取不到就返回空 —— 上层会当成"项目不明",只发给收全部项目的群
	if got := s.changeRequestProject(&ChangeRequest{TargetType: "asset", TargetID: "没有分隔符"}); got != "" {
		t.Errorf("分不出项目时不该猜一个:%q", got)
	}
}

// ===== 后台给项目选群之后,以后台为准 =====

// projectRouteServer 带一个真实 store 的路由测试服务器。
func projectRouteServer(t *testing.T, projects map[string]int) *Server {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "route.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for name, idx := range projects {
		p := &Project{ID: "p_" + name, TenantID: defaultTenantID, Name: name}
		if err := store.CreateProject(p); err != nil {
			t.Fatalf("CreateProject %s: %v", name, err)
		}
		if idx > 0 {
			if err := store.SetProjectBotIndex(defaultTenantID, p.ID, idx); err != nil {
				t.Fatalf("SetProjectBotIndex %s: %v", name, err)
			}
		}
	}
	s := routeServer(twoProjectBots()...)
	s.store = store
	return s
}

// 后台把紫菡改到第 1 个群 → 就该发第 1 个群,环境变量里那份不再作数。
func TestAdminChoiceOverridesEnv(t *testing.T) {
	s := projectRouteServer(t, map[string]int{"紫菡雅集": 1})
	got := s.botsForProject("紫菡雅集")
	if len(got) != 1 || got[0].Index != 1 {
		t.Fatalf("后台选的群没生效,仍按环境变量走:%v", botNames(got))
	}
}

// 【没在后台配过的项目继续按环境变量走】存量部署一条都不用动。
func TestUnconfiguredProjectStillFollowsEnv(t *testing.T) {
	s := projectRouteServer(t, map[string]int{"紫菡雅集": 0})
	got := s.botsForProject("紫菡雅集")
	if len(got) != 1 || got[0].Index != 2 {
		t.Fatalf("没配过的项目该按环境变量走(第2个群),实得:%v", botNames(got))
	}
}

// 【后台选了一个没配地址的群 → 不发,也不退回环境变量】
// 退回去就等于"我在后台选了 A 群,它却发去了 B 群"。
func TestChosenButUnconfiguredBotSendsNothing(t *testing.T) {
	s := projectRouteServer(t, map[string]int{"紫菡雅集": 7}) // 第 7 个群不存在
	if got := s.botsForProject("紫菡雅集"); len(got) != 0 {
		t.Errorf("选了不存在的群却发了出去:%v", botNames(got))
	}
}

// 存了又改回 0 = 交回环境变量决定。
func TestClearingChoiceFallsBackToEnv(t *testing.T) {
	s := projectRouteServer(t, map[string]int{"紫菡雅集": 1})
	if err := s.store.SetProjectBotIndex(defaultTenantID, "p_紫菡雅集", 0); err != nil {
		t.Fatal(err)
	}
	got := s.botsForProject("紫菡雅集")
	if len(got) != 1 || got[0].Index != 2 {
		t.Errorf("改回「跟随服务器配置」没生效:%v", botNames(got))
	}
}
