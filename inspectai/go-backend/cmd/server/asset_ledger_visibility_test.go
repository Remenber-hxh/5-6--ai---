package main

import (
	"path/filepath"
	"testing"
)

// ===== 台账不许悄悄吃掉设备 =====
//
// 【这条测试守的是什么】loadAssetsForDisplay 里曾经有一句按名单跳过:
// 紫菡雅集项目下、模板是 zihan_energy/zihan_daily 的设备,编号不在一份写死的
// 白名单里就整条不返回。本意是藏抄表拆分前的汇总行。
//
// 2026-09-16 线上盘点:它藏掉的是【全部 2 台真实设备】(Z1、Z2,后台手工建的),
// 历史汇总行一条都没藏到。现象是建档成功、不报错,然后这台设备谁都看不见 ——
// 连超级管理员也看不见。为这个查了一整天,期间怀疑过权限、租户、登录态。
//
// 白名单是写死的,而设备是会一直加的 —— 这个方向本身就是错的。

func newLedgerServer(t *testing.T) *Server {
	t.Helper()
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "ledger.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return &Server{store: store, storeKind: "sqlite"}
}

func addAsset(t *testing.T, s *Server, project, tpl, key, name string) {
	t.Helper()
	if err := s.store.CreateAsset(&AssetEntry{
		ID: project + "::" + tpl + "::" + sanitizeAssetIdent(key), TenantID: defaultTenantID,
		Project: project, TemplateID: tpl, AssetKey: key, AssetName: name,
		LastStatus: "未巡检",
	}); err != nil {
		t.Fatalf("CreateAsset %s: %v", key, err)
	}
}

// 后台手工建的紫菡抄表设备必须出现在台账里。
// 编号是人填的("Z1"/"Z2"),不会长成 z1_energy_meter 那种内部写法。
func TestManuallyCreatedZihanMeterIsVisible(t *testing.T) {
	s := newLedgerServer(t)
	addAsset(t, s, "紫菡雅集", "zihan_energy", "Z1", "Z1能耗表")
	addAsset(t, s, "紫菡雅集", "zihan_energy", "Z2", "Z2")
	addAsset(t, s, "会议中心", "elevator_no_room", "K01", "K01")

	got, err := s.loadAssetsForDisplay(defaultTenantID)
	if err != nil {
		t.Fatalf("loadAssetsForDisplay: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("台账少了设备:期望 3 台,得到 %d 台 —— 建档成功却看不见是最难查的一类故障", len(got))
	}
	seen := map[string]bool{}
	for _, a := range got {
		seen[a.AssetKey] = true
	}
	for _, key := range []string{"Z1", "Z2", "K01"} {
		if !seen[key] {
			t.Errorf("%s 没出现在台账里", key)
		}
	}
}

// 同一条规则:紫菡的综合巡检设备也不该按编号被筛掉。
func TestZihanDailyAssetWithFreeformKeyIsVisible(t *testing.T) {
	s := newLedgerServer(t)
	addAsset(t, s, "紫菡雅集", "zihan_daily", "PDX-2", "二号配电箱")

	got, err := s.loadAssetsForDisplay(defaultTenantID)
	if err != nil {
		t.Fatalf("loadAssetsForDisplay: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("自定义编号的设备被台账吃掉了,得到 %d 台", len(got))
	}
}
