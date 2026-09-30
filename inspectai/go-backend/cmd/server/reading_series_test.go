package main

import (
	"testing"
	"time"
)

// ===== 抄表读数按设备合成一条 =====
//
// 2026-09-29 线上:Z1 的 4 次读数分别记在 Z1、Z2、Z4 三栏(1、1、2 个)。
// 按栏位分组每组都不到 3 个点 —— 电表一条趋势都没有,只有水表有。

func meterObs(assetID, fieldKey string, day int, v float64) *FieldObservation {
	val := v
	return &FieldObservation{
		AssetID: assetID, RecordID: "rec" + string(rune('a'+day)), FieldKey: fieldKey,
		FieldLabel: fieldKey, ValueNumber: &val,
		CreatedAt: time.Date(2026, 9, day, 10, 0, 0, 0, time.UTC),
	}
}

// 线上 Z1 那样散在三栏的 4 个读数,应该画成一条 4 个点的"Z1 读数"。
func TestMeterReadingsScatteredAcrossColumnsMakeOneTrend(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	z1 := &AssetEntry{ID: "紫菡雅集::zihan_energy::Z1", TenantID: defaultTenantID, Project: "紫菡雅集",
		TemplateID: "zihan_energy", AssetKey: "Z1", AssetName: "Z1", AssetType: "电表"}
	if err := store.CreateAsset(z1); err != nil {
		t.Fatal(err)
	}
	if err := store.WriteAssetSnapshots(nil, []*FieldObservation{
		meterObs(z1.ID, "z4_reading", 21, 60190),
		meterObs(z1.ID, "z1_reading", 22, 60195),
		meterObs(z1.ID, "z2_reading", 23, 60199),
		meterObs(z1.ID, "z4_reading", 30, 60210),
	}); err != nil {
		t.Fatal(err)
	}

	resp, err := srv.assetTrendFor(z1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Series) != 1 {
		t.Fatalf("应合成 1 条曲线,得到 %d 条(单点字段:%v)", len(resp.Series), resp.SingleReading)
	}
	s := resp.Series[0]
	if len(s.Points) != 4 || s.FieldLabel != "Z1 读数" || s.Latest != 60210 {
		t.Fatalf("曲线不对:%d 个点、名字 %q、最新 %v", len(s.Points), s.FieldLabel, s.Latest)
	}

	// 漂移计数和漂移列表也按同一条算:最近两次 60199 → 60210,没超过 10%
	if n := srv.countAssetDrift(z1, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)); n != 0 {
		t.Fatalf("漂移计数应为 0,得到 %d", n)
	}
}

// 水表的名字不变(中文名直接接"读数"),照旧一条。
func TestWaterMeterTrendKeepsItsName(t *testing.T) {
	water := []*FieldObservation{
		meterObs("w", "living_water_reading", 21, 2002),
		meterObs("w", "living_water_reading", 22, 2002),
		meterObs("w", "living_water_reading", 23, 2008),
	}
	numeric := map[string]string{"living_water_reading": "生活水表读数"}
	series := buildAssetTrend(water, numeric, readingFieldsOf("zihan_energy"), "生活水表")
	if len(series) != 1 || series[0].FieldLabel != "生活水表读数" {
		t.Fatalf("水表曲线名字变了:%+v", series)
	}
}

// 不是"一格一台设备"的数值字段(温度、湿度)照旧各画各的,不能被合成一条。
func TestOtherNumericFieldsStaySeparate(t *testing.T) {
	in := []*FieldObservation{
		meterObs("env", "temperature", 21, 26), meterObs("env", "humidity", 21, 55),
		meterObs("env", "temperature", 22, 27), meterObs("env", "humidity", 22, 56),
		meterObs("env", "temperature", 23, 26), meterObs("env", "humidity", 23, 54),
	}
	numeric := map[string]string{"temperature": "温度", "humidity": "湿度"}
	series := buildAssetTrend(in, numeric, readingFieldsOf("zihan_daily"), "环境")
	if len(series) != 2 {
		t.Fatalf("温度和湿度应是两条,得到 %d 条", len(series))
	}
}

func TestReadingFieldsOfEnergyTemplate(t *testing.T) {
	got := readingFieldsOf("zihan_energy")
	for _, code := range []string{"z1_reading", "z2_reading", "z3_reading", "z4_reading", "living_water_reading", "fire_water_reading"} {
		if !got[code] {
			t.Errorf("%s 应算读数字段", code)
		}
	}
	if got["note"] || got["site"] {
		t.Error("备注、地点不是读数字段")
	}
}

func TestReadingSeriesLabel(t *testing.T) {
	cases := map[string]string{"Z1": "Z1 读数", "生活水表": "生活水表读数", "": "读数"}
	for in, want := range cases {
		if got := readingSeriesLabel(in); got != want {
			t.Errorf("%q → %q,应为 %q", in, got, want)
		}
	}
}
