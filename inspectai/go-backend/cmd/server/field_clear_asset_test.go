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
