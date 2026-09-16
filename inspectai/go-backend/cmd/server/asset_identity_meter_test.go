package main

import "testing"

// 字段级设备类型也要能认出模板。
//
// 【为什么需要】抄表模板的模板级类型是"能耗表组"——那是【一次巡检的对象】,
// 台账里根本没有这种实体;台账里是一台一台的"电表""水表",配在字段上。
// 只看模板级的话,后台建一台电表会落成 manual 段,和它将来被巡检时算出的
// ID 对不上,于是台账里裂成两条。
func TestFieldLevelAssetTypeResolvesTemplate(t *testing.T) {
	if got := templateIDForAssetType("电表"); got != "zihan_energy" {
		t.Errorf("电表 没认出抄表模板,得到 %q —— 建出来的设备会落成 manual 段", got)
	}
	if got := templateIDForAssetType("水表"); got != "zihan_energy" {
		t.Errorf("水表 没认出抄表模板,得到 %q", got)
	}
}

// 模板级仍然优先,行为不变。
func TestTemplateLevelAssetTypeStillWins(t *testing.T) {
	if got := templateIDForAssetType("能耗表组"); got != "zihan_energy" {
		t.Errorf("模板级类型认不出来了,得到 %q", got)
	}
}

// 认不出来的仍然返回空(落 manual,靠名字回查),不是瞎猜一个。
func TestUnknownAssetTypeStaysEmpty(t *testing.T) {
	if got := templateIDForAssetType("没这种东西"); got != "" {
		t.Errorf("未知类型猜出了 %q —— 猜错会把设备挂到别的模板上", got)
	}
}
