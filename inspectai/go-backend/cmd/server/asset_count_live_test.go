package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ===== 巡检次数必须是现算的,每一条出口都是 =====
//
// 【为什么这一列不能信】assets.inspection_count 当初由三处各自维护:
// 提交时 +1、启动回填时按记录数覆盖、手工建档时置 0。三处各写各的,必然漂 ——
// 线上实测过 K06 显示 1 次实际 20 次、HYZX-WJ-DT01 显示 44 实际 26。
// 本机库里现在 12 台设备全对不上,消防水表显示 1 次、实际 56 条快照。
//
// 所以读的时候一律数 asset_snapshots(enrichAssetForDisplay)。
// 但"一律"是靠每条出口自觉调用 enrich 维持的 —— 漏一条就漏一条,
// 而且漏掉的那条返回的是个看起来很正常的数字,没人会去对。
// 这组测试就是替那个自觉性兜底。

func patchAsset(t *testing.T, srv *Server, tok, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPatch, "/api/assets/"+id, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 造一台有 N 条快照、但计数器停在别的数上的设备 —— 就是线上那个状态。
func seedAssetWithStaleCounter(t *testing.T, srv *Server, snapshots int) *AssetEntry {
	t.Helper()
	a := &AssetEntry{
		ID: "会议中心::hot_water_room::HW-01", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "热水机房", AssetName: "HW-01",
		AssetKey: "HW-01", TemplateID: "hot_water_room",
		LastStatus: "正常", LastInspectedAt: time.Now(),
		InspectionCount: 1, // 陈旧值:和真实快照数对不上
	}
	// 快照只能跟着提交一起写(SubmitRecordWithAssets),没有单独的写入口 ——
	// 这本身是对的:快照是"某次巡检留下的",不该凭空产生。
	snaps := make([]*AssetSnapshot, 0, snapshots)
	for i := 0; i < snapshots; i++ {
		snaps = append(snaps, &AssetSnapshot{
			ID: newID("snap"), AssetID: a.ID, RecordID: newID("rec"),
			Status: "正常", CreatedAt: time.Now().Add(-time.Duration(i) * time.Hour),
		})
	}
	rec := &Record{
		ID: newID("rec"), TenantID: defaultTenantID,
		TemplateID: "hot_water_room", TemplateName: "热水机房巡检",
		Project: "会议中心", Inspector: "巡检员", Submitted: true,
	}
	if err := srv.store.CreateRecord(rec); err != nil {
		t.Fatal(err)
	}
	if err := srv.store.SubmitRecordWithAssets(rec, []*AssetEntry{a}, snaps, nil); err != nil {
		t.Fatal(err)
	}
	return a
}

func countFromJSON(t *testing.T, body []byte, wrapped bool) int {
	t.Helper()
	if wrapped {
		var w struct {
			Asset struct {
				InspectionCount int `json:"inspectionCount"`
			} `json:"asset"`
		}
		if err := json.Unmarshal(body, &w); err != nil {
			t.Fatalf("解不开响应: %v\n%s", err, body)
		}
		return w.Asset.InspectionCount
	}
	var a struct {
		InspectionCount int `json:"inspectionCount"`
	}
	if err := json.Unmarshal(body, &a); err != nil {
		t.Fatalf("解不开响应: %v\n%s", err, body)
	}
	return a.InspectionCount
}

// 【这一条抓到过真问题】改完设备信息那条出口原来没走 enrich,
// 返回的是库里那个陈旧的数 —— 别的接口都对,只有改完那一下变了个数。
func TestPatchAssetReturnsLiveCount(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	a := seedAssetWithStaleCounter(t, server, 7)

	got := patchAsset(t, server, tokens["admin"], a.ID, `{"assetName":"HW-01 改名"}`)
	if got.Code != http.StatusOK {
		t.Fatalf("改不动 code=%d body=%s", got.Code, got.Body.String())
	}
	if n := countFromJSON(t, got.Body.Bytes(), false); n != 7 {
		t.Errorf("巡检次数不是现算的:返回 %d,实际快照 7 条", n)
	}
}

// 列表和详情那两条本来就现算,这里钉住别退化。
func TestAssetListAndDetailReturnLiveCount(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	a := seedAssetWithStaleCounter(t, server, 5)

	req := httptest.NewRequest(http.MethodGet, "/api/assets", nil)
	req.Header.Set("X-InspectAI-Token", tokens["admin"])
	list := httptest.NewRecorder()
	server.router(list, req)
	if list.Code != http.StatusOK {
		t.Fatalf("列表打不开 code=%d", list.Code)
	}
	var lr struct {
		Assets []struct {
			ID              string `json:"id"`
			InspectionCount int    `json:"inspectionCount"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &lr); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, x := range lr.Assets {
		if x.ID == a.ID {
			found = true
			if x.InspectionCount != 5 {
				t.Errorf("列表里的巡检次数不是现算的:%d,实际 5", x.InspectionCount)
			}
		}
	}
	if !found {
		t.Fatal("列表里没有这台设备")
	}
}

// 【最容易被忘的一条】存储层不许再维护这个计数器。
//
// MemStore 原来还在自己 +1,而 SQLite 那边早就不写了 —— 两边行为不一致,
// 测试跑在 MemStore 上就永远发现不了"某条出口忘了现算":内存里那个数恰好是对的。
func TestStoreDoesNotMaintainTheCounter(t *testing.T) {
	store := NewMemStore()
	a := &AssetEntry{
		ID: "t::x::A1", TenantID: defaultTenantID, Project: "t",
		AssetType: "x", AssetName: "A1", InspectionCount: 0,
	}
	for i := 0; i < 3; i++ {
		if err := store.UpsertAsset(a); err != nil {
			t.Fatal(err)
		}
	}
	back, err := store.ListAssets(defaultTenantID)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range back {
		if x.ID == a.ID && x.InspectionCount != 0 {
			t.Errorf("存储层又开始自己维护计数器了(存了 3 次变成 %d)—— "+
				"这会让 MemStore 的数是对的、生产的是错的,测试于是测不出问题",
				x.InspectionCount)
		}
	}
}
