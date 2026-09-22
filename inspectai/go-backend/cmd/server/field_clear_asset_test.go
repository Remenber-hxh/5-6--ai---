package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// ===== 把一格的设备归属清掉 =====
//
// 确认页的「清除」靠的就是 PATCH assetName="" 。
//
// 【为什么值得单独钉一条】这个接口对 assetName 做了候选校验:不在台账候选里的
// 名字一律拒绝。空串正好是"不在候选里"的那一类 —— 校验要是不放它过去,
// 「清除」就是个点了没反应的按钮,而接口返回什么现场也看不见。
//
// 它是解开归属错位的唯一入口:已经归了别行的设备在下拉里是灰的,
// 六张照片都认领完之后,不先让出一台就谁也换不了(2026-09-22 线上卡死过)。

func TestPatchFieldCanClearAssetName(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)

	req := httptest.NewRequest(http.MethodPatch,
		"/api/inspection/records/"+rid+"/fields/z1_reading",
		bytes.NewBufferString(`{"assetName":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	s.router(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("清空设备被拒了 status=%d %s", rec.Code, rec.Body.String())
	}
	after, err := s.store.GetRecord(defaultTenantID, rid)
	if err != nil || after == nil {
		t.Fatalf("读回记录失败: %v", err)
	}
	f, _ := fieldByCode(after.Fields, "z1_reading")
	if f == nil {
		t.Fatal("字段没了")
	}
	if f.AssetName != "" {
		t.Errorf("设备没被清掉,还是 %q —— 「清除」会变成点了没反应", f.AssetName)
	}
	// 【读数和照片必须留着】清的是归属,不是这一次抄到的东西。
	// 一起清掉的话,人为了换个归属得把数重抄一遍。
	if f.Value != "1167636.5" {
		t.Errorf("读数被一起清掉了:%q", f.Value)
	}
	if f.SourceImageID != "img_elec" {
		t.Errorf("照片被一起清掉了:%q", f.SourceImageID)
	}
}

// 【清掉之后不能被默认值又猜回来】fillReadingAssetOptions 每次读记录都会在
// 设备为空时按字段名猜一个填回去(「消防水表读数」→「消防水表」)。
// 不认 AssetCleared 这一位的话,人点完「清除」下一次刷新它又回来了 ——
// 点了跟没点一样,而接口全程返回 200。这条是「清除」能不能用的命门。
func TestClearedAssetIsNotRefilledByDefault(t *testing.T) {
	s, _, rid := newSwapAPIServer(t)
	// 台账里有「消防水表」,默认值才猜得出来 —— 否则这条测试是空跑的
	if err := s.store.CreateAsset(&AssetEntry{
		ID: "紫菡雅集::zihan_energy::fire", TenantID: defaultTenantID, Project: "紫菡雅集",
		TemplateID: "zihan_energy", AssetType: "水表", AssetName: "消防水表", LastStatus: "未巡检",
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	rec, err := s.store.GetRecord(defaultTenantID, rid)
	if err != nil {
		t.Fatal(err)
	}

	// 先证明"没清过的空设备"确实会被猜回来 —— 这是这一位存在的理由
	f, _ := fieldByCode(rec.Fields, "fire_water_reading")
	f.AssetName, f.AssetCleared = "", false
	s.fillReadingAssetOptions(rec)
	if got, _ := fieldByCode(rec.Fields, "fire_water_reading"); got.AssetName != "消防水表" {
		t.Fatalf("前提不成立:空设备本该被默认值填上,却得到 %q", got.AssetName)
	}

	// 人点了「清除」之后,就不该再猜
	f, _ = fieldByCode(rec.Fields, "fire_water_reading")
	f.AssetName, f.AssetCleared = "", true
	s.fillReadingAssetOptions(rec)
	if got, _ := fieldByCode(rec.Fields, "fire_water_reading"); got.AssetName != "" {
		t.Errorf("清除之后默认值又被猜回来了(%q)—— 界面上就是「点了没反应」", got.AssetName)
	}
	// 候选还得照常给,否则清完就没得选了
	if got, _ := fieldByCode(rec.Fields, "fire_water_reading"); len(got.AssetOptions) == 0 {
		t.Error("清除之后连候选都没了,那就谁也选不上")
	}
}

// 重新选一台之后,这一位要放掉 —— 否则这一格从此再也拿不到默认值。
func TestPickingAgainClearsTheClearedFlag(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)
	if err := s.store.CreateAsset(&AssetEntry{
		ID: "紫菡雅集::zihan_energy::fire", TenantID: defaultTenantID, Project: "紫菡雅集",
		TemplateID: "zihan_energy", AssetType: "水表", AssetName: "消防水表", LastStatus: "未巡检",
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	patch := func(body string) {
		req := httptest.NewRequest(http.MethodPatch,
			"/api/inspection/records/"+rid+"/fields/fire_water_reading",
			bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-InspectAI-Token", tok)
		s.router(httptest.NewRecorder(), req)
	}
	patch(`{"assetName":""}`)
	patch(`{"assetName":"消防水表"}`)

	after, err := s.store.GetRecord(defaultTenantID, rid)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := fieldByCode(after.Fields, "fire_water_reading")
	if f.AssetCleared {
		t.Error("重新选了设备,「清除过」这一位还挂着 —— 这一格从此拿不到默认值")
	}
	if f.AssetName != "消防水表" {
		t.Errorf("重新选的设备没存上:%q", f.AssetName)
	}
}

// 清掉之后,这台设备在别的行里就该重新可选 —— 也就是后端算出来的候选里
// 仍然有它(候选来自台账,不受这一格归属影响),而且没有任何一格再占着它。
func TestClearedAssetIsFreeAgain(t *testing.T) {
	s, tok, rid := newSwapAPIServer(t)

	req := httptest.NewRequest(http.MethodPatch,
		"/api/inspection/records/"+rid+"/fields/fire_water_reading",
		bytes.NewBufferString(`{"assetName":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	s.router(httptest.NewRecorder(), req)

	after, err := s.store.GetRecord(defaultTenantID, rid)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range after.Fields {
		if f.AssetName == "消防水表" {
			t.Errorf("清完了还有一格占着「消防水表」:%s", f.Code)
		}
	}
}
