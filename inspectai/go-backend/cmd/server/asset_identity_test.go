package main

import "testing"

// 这一组测试对应线上台账被拆成 71 条(其中十几条是重复)的两种裂法。
// 逻辑错了不会报错、不会崩，只会静默地多建一台设备 —— 只能靠测试守。

func TestTemplateIDFromAssetType(t *testing.T) {
	// 后台建档表单没有"模板"这一项，只能从"设备类型"反推。
	// 反推不出来的话 ID 会落 manual::，这台设备一被巡检就裂成两条。
	cases := map[string]string{
		"有机房电梯":  "elevator_machine_room",
		"无机房电梯":  "elevator_no_room",
		"扶梯":     "escalator",
		"消防泵房":   "fire_pump",
		"":       "",
		"不存在的类型": "",
	}
	for at, want := range cases {
		if got := templateIDForAssetType(at); got != want {
			t.Errorf("类型 %q 应反推出模板 %q，得到 %q", at, want, got)
		}
	}
}

func TestAssetIDTemplatePart(t *testing.T) {
	// 项目名和设备编号里都可能出现 "::"，所以不能简单 Split 取 [1]
	cases := map[string]string{
		"会议中心::elevator_machine_room::K07": "elevator_machine_room",
		"会议中心::manual::K09":                "manual",
		"A::B::C::D":                       "B::C", // 编号里含 :: —— 取首尾之间
		"没有分隔符":                            "",
		"只有::一个":                           "",
	}
	for id, want := range cases {
		if got := assetIDTemplatePart(id); got != want {
			t.Errorf("ID %q 的模板段应为 %q，得到 %q", id, want, got)
		}
	}
}

func TestResolveAssetIdentity(t *testing.T) {
	// 线上真实存在的三种形态
	existing := []*AssetEntry{
		// 正常:名字和编号一致
		{ID: "会议中心::elevator_machine_room::K07", Project: "会议中心",
			TemplateID: "elevator_machine_room", AssetKey: "K07", AssetName: "K07"},
		// 裂法二:AI 当初读成 K6，后台把名字改成了 K06，id 还是 K6
		{ID: "会议中心::elevator_machine_room::K6", Project: "会议中心",
			TemplateID: "elevator_machine_room", AssetKey: "K6", AssetName: "K06"},
		// 裂法一:后台手工建档，模板段是 manual
		{ID: "会议中心::manual::K09", Project: "会议中心",
			TemplateID: "manual", AssetKey: "K09", AssetName: "K09"},
		// 另一个项目下的同名设备 —— 绝不能跨项目匹配上
		{ID: "紫菡雅集::elevator_machine_room::K07", Project: "紫菡雅集",
			TemplateID: "elevator_machine_room", AssetKey: "K07", AssetName: "K07"},
	}

	cases := []struct {
		name                         string
		project, templateID, assetNo string
		want                         string
	}{
		{"完全命中", "会议中心", "elevator_machine_room", "K07",
			"会议中心::elevator_machine_room::K07"},
		{"改名分家:报新名 K06 应挂回旧身份 K6", "会议中心", "elevator_machine_room", "K06",
			"会议中心::elevator_machine_room::K6"},
		{"改名分家:报旧编号 K6 也挂同一台", "会议中心", "elevator_machine_room", "K6",
			"会议中心::elevator_machine_room::K6"},
		{"手工建档遗留:巡检时模板是真模板,应挂到 manual:: 那台", "会议中心", "elevator_machine_room", "K09",
			"会议中心::manual::K09"},
		{"确实是新设备:返回空,由调用方建新档", "会议中心", "elevator_machine_room", "K99", ""},
		{"跨项目不匹配", "新项目", "elevator_machine_room", "K07", ""},
		{"空编号不匹配", "会议中心", "elevator_machine_room", "", ""},
	}
	for _, c := range cases {
		if got := resolveAssetIdentity(existing, c.project, c.templateID, c.assetNo); got != c.want {
			t.Errorf("%s：期望 %q，得到 %q", c.name, c.want, got)
		}
	}
}

// 同一台设备连着巡检两次，第二次绝不能再多出一条台账。
// 这正是线上"每次巡检完就多一台"的形状。
func TestResolveIsStableAcrossSubmits(t *testing.T) {
	existing := []*AssetEntry{
		{ID: "会议中心::elevator_machine_room::K6", Project: "会议中心",
			TemplateID: "elevator_machine_room", AssetKey: "K6", AssetName: "K06"},
	}
	first := resolveAssetIdentity(existing, "会议中心", "elevator_machine_room", "K06")
	if first == "" {
		t.Fatal("第一次就没匹配上")
	}
	// 模拟第一次提交后台账里那台的样子(名字被 upsert 覆盖成巡检报上来的值)
	existing[0].AssetName = "K06"
	second := resolveAssetIdentity(existing, "会议中心", "elevator_machine_room", "K06")
	if second != first {
		t.Fatalf("两次巡检解析到不同身份：%q vs %q —— 台账会多出一条", first, second)
	}
}
