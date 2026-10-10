package main

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 合并要把 from 名下的一切改挂到 into:巡检历史、读数历史、任务、计划清单、修改申请。
// 漏一样,那一样就挂在一台已经不存在的设备上,而且不报错。两种存储各走一遍。
func TestMergeAssetMovesEverything(t *testing.T) {
	sqlite, err := NewSQLiteStore(filepath.Join(t.TempDir(), "merge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlite.Close() })
	for name, store := range map[string]Store{"sqlite": sqlite, "mem": NewMemStore()} {
		t.Run(name, func(t *testing.T) { checkMergeMovesEverything(t, store) })
	}
}

func checkMergeMovesEverything(t *testing.T, store Store) {
	const into, from = "会议中心::elevator_machine_room::K07", "会议中心::elevator_machine_room::K7"
	day := func(d int) time.Time { return time.Date(2026, 9, d, 10, 0, 0, 0, time.UTC) }
	for _, a := range []*AssetEntry{
		{ID: into, TenantID: defaultTenantID, Project: "会议中心", TemplateID: "elevator_machine_room",
			AssetKey: "K07", AssetName: "K07", LastStatus: "正常", LastRecordID: "r2", LastInspectedAt: day(2)},
		{ID: from, TenantID: defaultTenantID, Project: "会议中心", TemplateID: "elevator_machine_room",
			AssetKey: "K7", AssetName: "K7", LastStatus: "异常", LastRecordID: "r3", LastInspectedAt: day(3)},
	} {
		// 用提交时写台账的那条路:CreateAsset 只存建档信息,不存"最近一次巡检"那几列
		if err := store.UpsertAsset(a); err != nil {
			t.Fatal(err)
		}
	}
	v := 1.0
	var snaps []*AssetSnapshot
	var obs []*FieldObservation
	for _, x := range []struct {
		asset, rec string
		d          int
	}{{into, "r1", 1}, {into, "r2", 2}, {from, "r2", 2}, {from, "r3", 3}} {
		snaps = append(snaps, &AssetSnapshot{AssetID: x.asset, RecordID: x.rec, Status: "正常", CreatedAt: day(x.d)})
		obs = append(obs, &FieldObservation{AssetID: x.asset, RecordID: x.rec, FieldKey: "reading", ValueNumber: &v, CreatedAt: day(x.d)})
	}
	if err := store.WriteAssetSnapshots(snaps, obs); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateEngineeringTask(&EngineeringTask{ID: "t1", AssetID: from,
		Project: "会议中心", Status: "待执行", CreatedAt: day(3), UpdatedAt: day(3)}); err != nil {
		t.Fatal(err)
	}
	if err := store.UpsertEngineeringPlan(&EngineeringPlanItem{ID: "p1", Project: "会议中心",
		AssetIDs: []string{from, into, "会议中心::elevator_machine_room::K08"}, CreatedAt: day(1), UpdatedAt: day(1)}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateChangeRequest(&ChangeRequest{ID: "cr1", TargetType: "asset",
		TargetID: from, Reason: "改名", Status: "pending", RequestedAt: day(3)}); err != nil {
		t.Fatal(err)
	}

	if err := store.MergeAsset(defaultTenantID, from, into); err != nil {
		t.Fatalf("合并失败:%v", err)
	}

	if a, _ := store.GetAsset(defaultTenantID, from); a != nil {
		t.Error("被并掉的那台还在台账里")
	}
	got, _ := store.GetAsset(defaultTenantID, into)
	if got == nil || got.LastStatus != "异常" || got.LastRecordID != "r3" {
		t.Errorf("from 的最近一次更新,应带到 into 上:%+v", got)
	}
	if n, _ := store.CountAssetSnapshots(into); n != 3 {
		t.Errorf("巡检历史应是 r1、r2、r3 三条(r2 两边都有只留一份),得到 %d", n)
	}
	if n, _ := store.CountAssetSnapshots(from); n != 0 {
		t.Errorf("from 名下还剩 %d 条巡检历史", n)
	}
	if o, _ := store.ListFieldObservations(into, "", 50); len(o) != 3 {
		t.Errorf("读数历史应是 3 条,得到 %d", len(o))
	}
	if task, _ := store.GetEngineeringTask("t1"); task == nil || task.AssetID != into {
		t.Errorf("任务没改挂到 into:%+v", task)
	}
	if p, _ := store.GetEngineeringPlan("p1"); p == nil || strings.Join(p.AssetIDs, ",") != into+",会议中心::elevator_machine_room::K08" {
		t.Errorf("计划清单应换成 into 并去重:%+v", p)
	}
	if cr, _ := store.GetChangeRequest("cr1"); cr == nil || cr.TargetID != into {
		t.Errorf("修改申请没改挂到 into:%+v", cr)
	}
}

// 后台接口:同一项目里才许合并;合并后 into 带回来
func TestMergeAssetAPI(t *testing.T) {
	s, tok := newAssetGuardServer(t)
	for _, body := range []string{
		`{"project":"紫菡雅集","assetKey":"Z3","assetName":"Z3","assetType":"电表"}`,
		`{"project":"紫菡雅集","assetKey":"Z3能耗表","assetName":"Z3能耗表","assetType":"电表","confirmSimilar":true}`,
	} {
		if got := postAsset(t, s, tok, body); got.Code != http.StatusCreated {
			t.Fatalf("建档失败:%d %s", got.Code, got.Body.String())
		}
	}
	into, from := "紫菡雅集::zihan_energy::Z3", "紫菡雅集::zihan_energy::Z3能耗表"

	req := func(method, path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-InspectAI-Token", tok)
		w := httptest.NewRecorder()
		s.router(w, r)
		return w
	}
	base := "/api/assets/" + url.PathEscape(from)
	if w := req(http.MethodGet, base+"/similar", ""); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"Z3"`) {
		t.Fatalf("相似设备里应有 Z3:%d %s", w.Code, w.Body.String())
	}
	if w := req(http.MethodPost, base+"/merge", `{"intoId":"`+from+`"}`); w.Code != http.StatusBadRequest {
		t.Errorf("并到自己应被拒,得到 %d %s", w.Code, w.Body.String())
	}
	if w := req(http.MethodPost, base+"/merge", `{"intoId":"`+into+`"}`); w.Code != http.StatusOK {
		t.Fatalf("合并失败:%d %s", w.Code, w.Body.String())
	}
	if a, _ := s.store.GetAsset(defaultTenantID, from); a != nil {
		t.Error("合并后被并掉的那台还在")
	}
}

// 合并后,老记录里还写着旧名字(Z3能耗表):认设备时要认到留下的 Z3 上,不能重建出那台。
func TestLooseIdentityFindsMergedAsset(t *testing.T) {
	all := []*AssetEntry{{ID: "紫菡雅集::zihan_energy::Z3", Project: "紫菡雅集", TemplateID: "zihan_energy", AssetKey: "Z3", AssetName: "Z3"}}
	if id := resolveAssetIdentityLoose(all, "紫菡雅集", "zihan_energy", "z3_energy_meter", "Z3能耗表"); id != "紫菡雅集::zihan_energy::Z3" {
		t.Errorf("旧名字 Z3能耗表 应认到 Z3,得到 %q", id)
	}
	if id := resolveAssetIdentityLoose(all, "别的项目", "zihan_energy", "z3_energy_meter", "Z3能耗表"); id != "" {
		t.Errorf("跨项目不该认:%q", id)
	}
	two := append(all, &AssetEntry{ID: "紫菡雅集::zihan_energy::Z03", Project: "紫菡雅集", TemplateID: "zihan_energy", AssetKey: "Z03", AssetName: "Z03"})
	if id := resolveAssetIdentityLoose(two, "紫菡雅集", "zihan_energy", "z3_energy_meter", "Z3能耗表"); id != "" {
		t.Errorf("两台都像就不该猜:%q", id)
	}
}
