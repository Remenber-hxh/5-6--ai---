package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// 今日应巡的完成率计算。
//
// 这个数字要拿去做每日推送,算错的后果是群里天天收到错的 ——
// 而错的数字比没有数字更糟:没人会再信它,也就没人看了。

func newBoardServer(t *testing.T) (*Server, *http.Request, *MemStore) {
	t.Helper()
	srv, r, store, _ := newScopeRequestWithStore(t, roleAdmin, "")
	for _, name := range []string{"KT-1", "KT-2", "KT-3"} {
		if err := store.CreateAsset(&AssetEntry{
			ID: "会议中心::escalator::" + name, TenantID: defaultTenantID,
			Project: "会议中心", AssetType: "扶梯", AssetKey: name,
			AssetName: name, LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	return srv, r, store
}

func addDailyPlan(t *testing.T, store *MemStore, id string, weekdays string, assets []string) {
	t.Helper()
	if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: id, Project: "会议中心", WorkContent: "每日例检 " + id,
		PlanType: planTypeDaily, Weekdays: weekdays, AssetIDs: assets,
		Status: "进行中",
	}); err != nil {
		t.Fatal(err)
	}
}

// 完成 = 这些设备今天有巡检快照。不给现场加"打勾"的动作。
func TestTodayBoardCountsInspectedAssets(t *testing.T) {
	srv, r, store := newBoardServer(t)
	all := []string{
		"会议中心::escalator::KT-1",
		"会议中心::escalator::KT-2",
		"会议中心::escalator::KT-3",
	}
	addDailyPlan(t, store, "p1", "", all) // 空 = 每天

	now := time.Now()
	board, err := srv.buildTodayBoard(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if board.Total != 3 || board.Done != 0 {
		t.Fatalf("还没巡时应为 0/3,得到 %d/%d", board.Done, board.Total)
	}

	// 巡了一台
	if err := store.WriteAssetSnapshots([]*AssetSnapshot{{
		ID: "s1", AssetID: all[0], RecordID: "r1", Status: "正常", CreatedAt: now,
	}}, nil); err != nil {
		t.Fatal(err)
	}
	board, _ = srv.buildTodayBoard(r, now)
	if board.Done != 1 {
		t.Fatalf("巡了一台应为 1/3,得到 %d/%d", board.Done, board.Total)
	}

	// 【昨天巡的不算今天的】不排除的话每天的完成率都是满的,这个看板就废了
	if err := store.WriteAssetSnapshots([]*AssetSnapshot{{
		ID: "s2", AssetID: all[1], RecordID: "r2", Status: "正常",
		CreatedAt: now.AddDate(0, 0, -1),
	}}, nil); err != nil {
		t.Fatal(err)
	}
	board, _ = srv.buildTodayBoard(r, now)
	if board.Done != 1 {
		t.Fatalf("昨天巡的被算进今天了,得到 %d/%d", board.Done, board.Total)
	}
}

// 两条计划点了同一台设备时,顶部总数要按设备去重 ——
// 否则巡一次总数涨 2,完成率永远差一截。
func TestTodayBoardDeduplicatesAssets(t *testing.T) {
	srv, r, store := newBoardServer(t)
	a1 := "会议中心::escalator::KT-1"
	addDailyPlan(t, store, "p1", "", []string{a1, "会议中心::escalator::KT-2"})
	addDailyPlan(t, store, "p2", "", []string{a1}) // 重复点了 KT-1

	board, err := srv.buildTodayBoard(r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if board.Total != 2 {
		t.Fatalf("两条计划共涉及 2 台设备,得到 %d —— 没按设备去重", board.Total)
	}
	// 但每条计划自己的进度仍然分别算
	if len(board.Plans) != 2 {
		t.Fatalf("应有 2 条计划,得到 %d", len(board.Plans))
	}
}

// 今天不该执行的计划不进看板。
func TestTodayBoardRespectsWeekdays(t *testing.T) {
	srv, r, store := newBoardServer(t)
	now := time.Now()
	today := isoWeekday(int(now.Weekday()))
	other := today%7 + 1 // 随便挑一个不是今天的

	addDailyPlan(t, store, "today", itoaSafe(today), []string{"会议中心::escalator::KT-1"})
	addDailyPlan(t, store, "other", itoaSafe(other), []string{"会议中心::escalator::KT-2"})

	board, err := srv.buildTodayBoard(r, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plans) != 1 || board.Plans[0].PlanID != "today" {
		t.Fatalf("只有今天该执行的那条能进看板,得到 %+v", board.Plans)
	}
}

// 【没指定设备的计划要标出来】静默当成 0/0 会让它看起来像"已完成"。
func TestTodayBoardFlagsPlanWithoutAssets(t *testing.T) {
	srv, r, store := newBoardServer(t)
	addDailyPlan(t, store, "empty", "", nil)
	board, err := srv.buildTodayBoard(r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plans) != 1 || !board.Plans[0].NoAssets {
		t.Fatalf("没指定设备的计划要标 NoAssets,得到 %+v", board.Plans)
	}
}

// 计划里点了、但台账里已经删掉的设备要标出来 ——
// 不标的话完成率永远到不了 100%,而没人知道是因为一台不存在的设备。
func TestTodayBoardFlagsMissingAsset(t *testing.T) {
	srv, r, store := newBoardServer(t)
	addDailyPlan(t, store, "p1", "", []string{"会议中心::escalator::已经删掉了"})
	board, err := srv.buildTodayBoard(r, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(board.Plans) != 1 || len(board.Plans[0].Assets) != 1 {
		t.Fatalf("结构不对:%+v", board.Plans)
	}
	if !board.Plans[0].Assets[0].Missing {
		t.Fatal("台账里查不到的设备要标 Missing")
	}
}

// 接口能通。
func TestTodayBoardEndpoint(t *testing.T) {
	srv, r, store := newBoardServer(t)
	addDailyPlan(t, store, "p1", "", []string{"会议中心::escalator::KT-1"})
	req := httptest.NewRequest(http.MethodGet, "/api/engineering/plans/today", nil)
	req.Header = r.Header
	w := httptest.NewRecorder()
	srv.handleTodayBoard(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
