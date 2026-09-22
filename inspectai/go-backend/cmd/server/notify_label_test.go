package main

import (
	"strings"
	"testing"
)

// ===== 推送文案里不许出现内部主键 =====
//
// 现场收到的原文:
//   对象:资产台账 紫菡雅集::zihan_energy::生活水表
// 双冒号、模板 id 这些东西对现场没有任何意义。这条消息是发到客户群里的。

func TestChangeTargetLabelNeverLeaksID(t *testing.T) {
	s, _, _ := newSwapAPIServer(t)
	if err := s.store.CreateAsset(&AssetEntry{
		ID: "紫菡雅集::zihan_energy::生活水表", TenantID: defaultTenantID,
		Project: "紫菡雅集", TemplateID: "zihan_energy", AssetType: "水表",
		AssetName: "生活水表", AssetKey: "生活水表", LastStatus: "正常",
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}

	got := s.changeTargetLabel(&ChangeRequest{
		TargetType: "asset", TargetID: "紫菡雅集::zihan_energy::生活水表",
	})
	if got != "资产台账 生活水表" {
		t.Errorf("对象文案不对:%q", got)
	}
	for _, leak := range []string{"::", "zihan_energy", "rec_"} {
		if strings.Contains(got, leak) {
			t.Errorf("文案里漏出了内部标识 %q:%s", leak, got)
		}
	}
}

// 【设备已经被删了也不许回落到 ID】孤儿申请照样会触发推送。
func TestDeletedTargetStillHidesID(t *testing.T) {
	s, _, _ := newSwapAPIServer(t)
	got := s.changeTargetLabel(&ChangeRequest{
		TargetType: "asset", TargetID: "紫菡雅集::zihan_energy::已删掉的表",
	})
	if strings.Contains(got, "::") {
		t.Errorf("查不到设备时把主键发了出去:%s", got)
	}
	if !strings.Contains(got, "不存在") {
		t.Errorf("该说清楚这台设备没了:%s", got)
	}
}

// 认不出类型时也不能回落到 TargetID —— 那是我们的问题,不该让主键替我们背。
func TestUnknownTargetTypeHidesID(t *testing.T) {
	s, _, _ := newSwapAPIServer(t)
	got := s.changeTargetLabel(&ChangeRequest{
		TargetType: "某种新类型", TargetID: "紫菡雅集::zihan_energy::生活水表",
	})
	if strings.Contains(got, "::") || strings.Contains(got, "zihan_energy") {
		t.Errorf("兜底分支把主键发了出去:%s", got)
	}
}

// 记录类的对象也要是人话 —— 「巡检记录 rec_1754...」同样不能发出去。
func TestRecordTargetUsesReadableName(t *testing.T) {
	s, _, rid := newSwapAPIServer(t)
	got := s.changeTargetLabel(&ChangeRequest{TargetType: "record", TargetID: rid})
	if strings.Contains(got, "rec_") {
		t.Errorf("记录 id 被发了出去:%s", got)
	}
}
