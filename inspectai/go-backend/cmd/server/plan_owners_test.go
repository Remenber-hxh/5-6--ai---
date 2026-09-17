package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ===== 计划的多位负责人 =====
//
// 【要守住的事】
//   - 群提醒里要把几个人的名字都点出来(这是加这个功能的原因)
//   - 老计划(只有一个负责人)读出来、存回去,行为和以前完全一样
//   - 每一位有账号的负责人都要核实,不能只核第一位

// ---- 同步规则 ----

func TestLegacySingleOwnerBecomesOneItemList(t *testing.T) {
	p := &EngineeringPlanItem{OwnerName: "李磊", OwnerID: "u_li"}
	syncPlanOwners(p)
	if len(p.Owners) != 1 || p.Owners[0].ID != "u_li" || p.Owners[0].Name != "李磊" {
		t.Fatalf("老数据没变成一人列表:%+v", p.Owners)
	}
	if p.OwnerName != "李磊" || p.OwnerID != "u_li" {
		t.Errorf("老字段被改变了:name=%q id=%q —— 升级后老计划的显示和提醒会变", p.OwnerName, p.OwnerID)
	}
}

func TestMultipleOwnersJoinForDisplay(t *testing.T) {
	p := &EngineeringPlanItem{Owners: []PlanOwner{{ID: "u2", Name: "周新宇"}, {ID: "u3", Name: "苑文涛"}}}
	syncPlanOwners(p)
	if p.OwnerName != "周新宇、苑文涛" {
		t.Errorf("合并写法不对:%q —— 群提醒和计划列表读的就是它", p.OwnerName)
	}
	if p.OwnerID != "u2" {
		t.Errorf("OwnerID 应是第一位有账号的人,得到 %q", p.OwnerID)
	}
}

// 外委人员排在前面时,OwnerID 要跳过他取第一位有账号的人 ——
// 否则「负责人绑定」工具会以为这条计划还没绑。
func TestOwnerIDSkipsExternalOwner(t *testing.T) {
	p := &EngineeringPlanItem{Owners: []PlanOwner{{Name: "外委张三"}, {ID: "u3", Name: "苑文涛"}}}
	syncPlanOwners(p)
	if p.OwnerID != "u3" {
		t.Errorf("OwnerID=%q,应跳过外委取 u3", p.OwnerID)
	}
	if p.OwnerName != "外委张三、苑文涛" {
		t.Errorf("外委人员的名字丢了:%q", p.OwnerName)
	}
}

// 同一个人选了两次,提醒里不能出现两遍他的名字。
func TestDuplicateOwnersCollapse(t *testing.T) {
	p := &EngineeringPlanItem{Owners: []PlanOwner{
		{ID: "u2", Name: "周新宇"}, {ID: "u2", Name: "周新宇"},
		{Name: "外委 张三"}, {Name: "外委张三"}, // 空格不同也是同一个人
		{Name: "  "},                          // 空的扔掉
	}}
	syncPlanOwners(p)
	if len(p.Owners) != 2 {
		t.Fatalf("去重后应剩 2 位,得到 %d:%+v", len(p.Owners), p.Owners)
	}
}

// 历史上手打的「张三、李四」可能就是一个外委班组的叫法,不能擅自拆成两个人。
func TestLegacyJoinedNameIsNotSplit(t *testing.T) {
	p := &EngineeringPlanItem{OwnerName: "张三、李四"}
	syncPlanOwners(p)
	if len(p.Owners) != 1 {
		t.Errorf("老的合并名字被拆开了:%+v —— 会凭空多出负责人", p.Owners)
	}
}

// 负责人全部清空要能生效(前端会同时清掉老字段)。
func TestClearingOwners(t *testing.T) {
	p := &EngineeringPlanItem{Owners: []PlanOwner{}, OwnerName: "", OwnerID: ""}
	syncPlanOwners(p)
	if len(p.Owners) != 0 || p.OwnerName != "" || p.OwnerID != "" {
		t.Errorf("清空没生效:%+v name=%q id=%q", p.Owners, p.OwnerName, p.OwnerID)
	}
}

// ---- 存进库、读出来 ----

func newPlanOwnerSQLite(t *testing.T) *SQLiteStore {
	t.Helper()
	st, err := NewSQLiteStore(filepath.Join(t.TempDir(), "owners.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return st
}

func TestOwnersSurviveDatabaseRoundTrip(t *testing.T) {
	st := newPlanOwnerSQLite(t)
	if err := st.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: "p_multi", Project: "紫菡雅集", WorkContent: "每日抄表", PlanType: planTypeMonthly,
		Owners: []PlanOwner{{ID: "u2", Name: "周新宇"}, {ID: "u3", Name: "苑文涛"}, {Name: "外委张三"}},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEngineeringPlan("p_multi")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Owners) != 3 {
		t.Fatalf("存进去 3 位,读出来 %d 位:%+v", len(got.Owners), got.Owners)
	}
	if got.OwnerName != "周新宇、苑文涛、外委张三" {
		t.Errorf("读出来的合并写法不对:%q", got.OwnerName)
	}
	if got.Owners[2].ID != "" || got.Owners[2].Name != "外委张三" {
		t.Errorf("外委人员存丢了:%+v", got.Owners[2])
	}
}

// 【最要紧的一条】升级前存的老计划,owners_json 那一列是空数组 ——
// 读出来必须还是原来那一位负责人,不能变成"没有负责人"。
func TestLegacyRowReadsAsSingleOwner(t *testing.T) {
	st := newPlanOwnerSQLite(t)
	if err := st.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: "p_old", Project: "会议中心", WorkContent: "老计划", PlanType: planTypeMonthly,
		OwnerName: "李磊", OwnerID: "u_li",
	}); err != nil {
		t.Fatal(err)
	}
	// 模拟升级前的行:列表那一列是空的
	if _, err := st.db.Exec(`UPDATE engineering_plan_items SET owners_json='[]' WHERE id='p_old'`); err != nil {
		t.Fatal(err)
	}
	got, err := st.GetEngineeringPlan("p_old")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Owners) != 1 || got.Owners[0].Name != "李磊" || got.Owners[0].ID != "u_li" {
		t.Fatalf("老计划的负责人读丢了:%+v —— 升级当天所有老计划的提醒都会变成「未指定负责人」", got.Owners)
	}
}

// ---- 按人筛选 ----

func TestFilterByOneOfSeveralOwners(t *testing.T) {
	p := &EngineeringPlanItem{Owners: []PlanOwner{{ID: "u2", Name: "周新宇"}, {ID: "u3", Name: "苑文涛"}}}
	syncPlanOwners(p)
	if !engineeringPlanMatches(p, EngineeringPlanFilter{Owner: "苑文涛"}) {
		t.Error("按第二位负责人筛不出来 —— 他会以为这条计划不归他")
	}
	if engineeringPlanMatches(p, EngineeringPlanFilter{Owner: "李磊"}) {
		t.Error("不相干的人也筛出来了")
	}
}

// ---- 保存接口:每一位都要核实 ----

func TestSavePlanWithTwoOwners(t *testing.T) {
	srv, store, _ := bindStore(t)
	a := addOwnerUser(t, store, "zxy", "周新宇")
	b := addOwnerUser(t, store, "ywt", "苑文涛")
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划", PlanType: planTypeMonthly,
		Owners: []PlanOwner{{ID: a.ID, Name: "小周"}, {ID: b.ID}, {Name: "外委张三"}},
	})
	if w.Code != 201 {
		t.Fatalf("应保存成功,实际 %d:%s", w.Code, w.Body.String())
	}
	got, err := store.GetEngineeringPlan("p1")
	if err != nil {
		t.Fatal(err)
	}
	// 名字跟着账号走("小周"→周新宇;没传名字的也补上),外委保留原样
	if got.OwnerName != "周新宇、苑文涛、外委张三" {
		t.Errorf("保存后的负责人=%q", got.OwnerName)
	}
}

// 【第二位看不到项目也要拦】只核第一位的话,第二位照样能存进去 ——
// 提醒点了他的名,他打开什么都没有。
func TestSavePlanRejectsSecondOwnerWhoCannotSeeProject(t *testing.T) {
	srv, store, _ := bindStore(t)
	a := addOwnerUser(t, store, "zxy", "周新宇")
	b := addOwnerUser(t, store, "ywt", "苑文涛")
	scopeUserToProject(t, store, b.ID, "紫菡雅集") // 苑文涛只看得到紫菡
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划", PlanType: planTypeMonthly,
		Owners: []PlanOwner{{ID: a.ID}, {ID: b.ID}},
	})
	if w.Code != 400 || !strings.Contains(w.Body.String(), "owner_cannot_see_project") {
		t.Fatalf("第二位负责人看不到项目却没被拦:%d %s", w.Code, w.Body.String())
	}
}

func TestSavePlanRejectsUnknownSecondOwner(t *testing.T) {
	srv, store, _ := bindStore(t)
	a := addOwnerUser(t, store, "zxy", "周新宇")
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划", PlanType: planTypeMonthly,
		Owners: []PlanOwner{{ID: a.ID}, {ID: "user_does_not_exist"}},
	})
	if w.Code != 400 {
		t.Fatalf("不存在的第二位负责人应被拦,实际 %d", w.Code)
	}
}

// 只传老字段的客户端(没刷新的旧页面)照样能用。
func TestSavePlanOldClientSingleOwnerStillWorks(t *testing.T) {
	srv, store, _ := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	w := savePlanDirect(t, srv, &EngineeringPlanItem{
		ID: "p1", Project: "会议中心", WorkContent: "月度计划", PlanType: planTypeMonthly,
		OwnerID: u.ID, OwnerName: "老胡",
	})
	if w.Code != 201 {
		t.Fatalf("老客户端保存失败:%d %s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p1")
	if len(got.Owners) != 1 || got.OwnerName != "胡晓悱" || got.OwnerID != u.ID {
		t.Errorf("老客户端存出来不对:%+v name=%q id=%q", got.Owners, got.OwnerName, got.OwnerID)
	}
}

// ---- 群提醒:这是加这个功能的目的 ----

func TestDailyPushNamesAllOwners(t *testing.T) {
	srv, store, _ := bindStore(t)
	now := time.Now().In(pushTZ)
	for _, a := range []*AssetEntry{
		{ID: "zh::z1", TenantID: defaultTenantID, Project: "紫菡雅集", AssetName: "Z1能耗表", LastStatus: "正常"},
		{ID: "zh::z2", TenantID: defaultTenantID, Project: "紫菡雅集", AssetName: "Z2能耗表", LastStatus: "正常"},
	} {
		if err := store.CreateAsset(a); err != nil {
			t.Fatal(err)
		}
	}
	if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: "p_daily", Project: "紫菡雅集", WorkContent: "每日抄表", PlanType: planTypeDaily,
		AssetIDs: []string{"zh::z1", "zh::z2"},
		Owners:   []PlanOwner{{ID: "u2", Name: "周新宇"}, {ID: "u3", Name: "苑文涛"}},
	}); err != nil {
		t.Fatal(err)
	}
	board, err := srv.buildTodayBoardFor(defaultTenantID, dataVisibility{AllData: true}, now)
	if err != nil {
		t.Fatal(err)
	}
	d := buildDailyPushDigest(board, false)
	if !strings.Contains(d.Text, "周新宇、苑文涛") {
		t.Fatalf("提醒里没有把两位负责人都点出来:\n%s", d.Text)
	}
	if !strings.Contains(d.Text, "Z1能耗表") {
		t.Errorf("提醒里没有待巡设备:\n%s", d.Text)
	}
}

// ---- 负责人绑定工具不能碰多人计划 ----

func TestOwnerBindingRefusesMultiOwnerPlan(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{
		ID: "p_multi", Project: "会议中心", WorkContent: "多人计划", PlanType: planTypeMonthly,
		Owners: []PlanOwner{{Name: "外委张三"}, {Name: "外委李四"}},
	}); err != nil {
		t.Fatal(err)
	}
	w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p_multi", UserID: u.ID}))
	if w.Code != 400 {
		t.Fatalf("多人计划被绑定工具改了(%d)—— 其余负责人会被静默删掉", w.Code)
	}
	got, _ := store.GetEngineeringPlan("p_multi")
	if len(got.Owners) != 2 {
		t.Errorf("负责人被改掉了:%+v", got.Owners)
	}
}

// 绑定工具改单人计划后,负责人列表也要跟着变(列表才是事实来源)。
func TestOwnerBindingUpdatesOwnersList(t *testing.T) {
	srv, store, req := bindStore(t)
	u := addOwnerUser(t, store, "huxf", "胡晓悱")
	addPlanWithOwner(t, store, "p1", "胡 晓悱", "")
	if w := applyBindings(t, srv, req, bindBody(bindPair{PlanID: "p1", UserID: u.ID})); w.Code != 200 {
		t.Fatalf("绑定失败:%d %s", w.Code, w.Body.String())
	}
	got, _ := store.GetEngineeringPlan("p1")
	if len(got.Owners) != 1 || got.Owners[0].ID != u.ID {
		t.Errorf("绑定后负责人列表没更新:%+v —— 下次保存会被列表算回没绑的状态", got.Owners)
	}
}
