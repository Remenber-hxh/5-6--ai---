package main

import (
	"testing"
	"time"
)

// 读数趋势。
//
// 这块的价值全在"能不能一眼看出漂移",而判断漂移的规则很容易写出
// 两种失败:要么满屏红点(阈值太松),要么永远不报(阈值太紧)。
// 所以判定被抽成纯函数,这里把两个方向都钉住。

func obs(fieldKey, label string, day int, v float64) *FieldObservation {
	val := v
	return &FieldObservation{
		AssetID:    "a1",
		FieldKey:   fieldKey,
		FieldLabel: label,
		ValueNumber: &val,
		CreatedAt:  time.Date(2026, 8, day, 9, 0, 0, 0, time.UTC),
	}
}

var numericFields = map[string]string{"temperature": "温度℃", "tank_level": "水箱水位（米）"}

func TestTrendSortsByRealTimeNotInsertOrder(t *testing.T) {
	// 【按真实时间排,不按写入顺序】巡检不是等间隔发生的。
	// 照写入顺序画,坡度完全失真 —— 而人正是照坡度判断"在往哪走"。
	series := buildAssetTrend([]*FieldObservation{
		obs("temperature", "温度℃", 20, 30),
		obs("temperature", "温度℃", 5, 10),
		obs("temperature", "温度℃", 12, 20),
	}, numericFields)
	if len(series) != 1 {
		t.Fatalf("应只有一条曲线,实际 %d", len(series))
	}
	got := series[0].Points
	if got[0].Value != 10 || got[1].Value != 20 || got[2].Value != 30 {
		t.Errorf("没有按时间排序:%v", got)
	}
	if series[0].Latest != 30 {
		t.Errorf("Latest 应取时间上最后一次,实际 %v", series[0].Latest)
	}
}

// 【最要紧的一条】读数常年稳定时 σ 接近 0,不设下限的话
// 一点点正常波动全会被标成异常 —— 满屏红点等于没标。
func TestTrendDoesNotFlagTinyWobbleOnStableReading(t *testing.T) {
	var in []*FieldObservation
	vals := []float64{0.60, 0.61, 0.60, 0.59, 0.60, 0.61, 0.62}
	for i, v := range vals {
		in = append(in, obs("tank_level", "水箱水位（米）", i+1, v))
	}
	s := buildAssetTrend(in, numericFields)[0]
	if s.Drifting {
		t.Errorf("常年 0.6 上下的表不该被判漂移,最新 %v 基线 %.3f", s.Latest, s.Baseline)
	}
	for _, p := range s.Points {
		if p.Outlier {
			t.Errorf("正常波动被标成异常点:%+v", p)
		}
	}
}

// 反过来:真的走偏了必须报出来,否则这个功能白做。
func TestTrendFlagsRealDrift(t *testing.T) {
	var in []*FieldObservation
	for i, v := range []float64{0.60, 0.61, 0.60, 0.59, 0.60, 0.61, 0.30} {
		in = append(in, obs("tank_level", "水箱水位（米）", i+1, v))
	}
	s := buildAssetTrend(in, numericFields)[0]
	if !s.Drifting {
		t.Errorf("水位掉到一半应判漂移:最新 %v 基线 %.3f", s.Latest, s.Baseline)
	}
	if s.Deviation == nil || *s.Deviation > -20 {
		t.Errorf("偏离百分比应明显为负,实际 %v", s.Deviation)
	}
}

// 模板改过之后,历史里可能留着已经不是数值的字段。
// 照旧画的话,图上会出现一条界面上根本找不到对应字段的曲线。
func TestTrendIgnoresFieldsNotDeclaredNumericAnymore(t *testing.T) {
	s := buildAssetTrend([]*FieldObservation{
		obs("temperature", "温度℃", 1, 20),
		obs("temperature", "温度℃", 2, 21),
		obs("temperature", "温度℃", 3, 22),
		obs("legacy_field", "早年的字段", 1, 99),
		obs("legacy_field", "早年的字段", 2, 98),
		obs("legacy_field", "早年的字段", 3, 97),
	}, numericFields)
	if len(s) != 1 || s[0].FieldKey != "temperature" {
		t.Errorf("不该画模板里已经不存在的数值字段:%+v", s)
	}
}

// 漂移的排前面 —— 这一屏是给人"哪个指标不对劲"用的
func TestTrendPutsDriftingSeriesFirst(t *testing.T) {
	var in []*FieldObservation
	for i, v := range []float64{20, 20.1, 20, 19.9, 20} {
		in = append(in, obs("temperature", "温度℃", i+1, v)) // 稳
	}
	for i, v := range []float64{0.6, 0.61, 0.6, 0.59, 0.2} {
		in = append(in, obs("tank_level", "水箱水位（米）", i+1, v)) // 漂
	}
	s := buildAssetTrend(in, numericFields)
	if len(s) != 2 {
		t.Fatalf("应有两条曲线,实际 %d", len(s))
	}
	if !s[0].Drifting || s[0].FieldKey != "tank_level" {
		t.Errorf("漂移的那条应排在最前:%+v", s[0])
	}
}
