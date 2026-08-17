package main

import "testing"

// 设备简写匹配。
//
// 现场用手机打字,用户说的是「K7」而设备实际叫「KT-7」;也可能说「kt 7」「KT7」。
// 匹配太严 → 查不到,AI 只能说"台账里没这台"(线上实测踩过);
// 匹配太松 → 把 KT-17 当成 KT-7 答出去,**答错设备比查不到更糟**。
//
// 用【数字部分完全相同】作锚点:编号里的数字是设备身份,字母是分类前缀。

func TestLooseAssetMatchAcceptsRealShorthand(t *testing.T) {
	ok := [][2]string{
		{"K7", "KT7"},        // 线上真实案例:用户打 K7,设备叫 KT-7(归一化后 KT7)
		{"K7", "KX7"},        // 字母不同但数字对上 —— 作为候选给出,由模型确认
		{"KT7", "KT7"},       // 完全相同(虽然这条会先被精确匹配吃掉)
		{"7", "KT7"},         // 用户只说"7 号"
		{"HYZX7", "HYZXWJ7"}, // 长前缀里插了字母
		// 【反方向也要认】用户打多了字母:打 KT-7 而设备真名叫 K7。
		// 第一版只判单向,这种就漏了(本机库里正好有一台真名叫 K7 的)。
		{"KT7", "K7"},
	}
	for _, c := range ok {
		if !looseAssetMatch(c[0], c[1]) {
			t.Errorf("looseAssetMatch(%q, %q) 应为 true —— 这是真实会发生的简写", c[0], c[1])
		}
	}
}

func TestLooseAssetMatchRejectsDifferentDevices(t *testing.T) {
	no := [][2]string{
		{"K7", "KT17"}, // 【最关键的一条】数字 7 != 17,不是同一台
		{"K7", "KT70"}, // 7 != 70
		{"K7", "KT8"},  // 数字不同
		{"KX7", "KT7"}, // 数字对上,但 X 不在 KT 里 —— 用户打的字母对不上
		{"KT", "KT7"},  // 用户没给数字,无法确认是哪一台
		{"K7", ""},     // 空目标
	}
	for _, c := range no {
		if looseAssetMatch(c[0], c[1]) {
			t.Errorf("looseAssetMatch(%q, %q) 应为 false —— 会把两台不同设备混成一台", c[0], c[1])
		}
	}
}

// 精确匹配必须压过包含匹配:用户打 KT-7,不能答 KT-70。
func TestFindAssetsPrefersExactOverPartial(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	for _, name := range []string{"KT-70", "KT-7", "KT-71"} {
		if err := store.CreateAsset(&AssetEntry{
			ID: "会议中心::elevator_no_room::" + name, TenantID: defaultTenantID,
			Project: "会议中心", AssetType: "无机房电梯",
			AssetKey: name, AssetName: name, LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := srv.findAssetsForAgent(defaultTenantID, "KT-7")
	if err != nil {
		t.Fatal(err)
	}
	list, _ := got["assets"].([]map[string]any)
	if len(list) == 0 {
		t.Fatal("应该找到设备")
	}
	if list[0]["name"] != "KT-7" {
		t.Fatalf("第一个应该是精确匹配的 KT-7,得到 %v —— 模型会用第一个", list[0]["name"])
	}
}

// 完全匹配不到时,把编号相近的作为【候选】返回,并且明确标成需要确认 ——
// 不能混进 assets 里让模型当成命中直接用。
func TestFindAssetsReturnsNearMatchesSeparately(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	if err := store.CreateAsset(&AssetEntry{
		ID: "会议中心::elevator_no_room::KT-7", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "无机房电梯",
		AssetKey: "KT-7", AssetName: "KT-7", LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := srv.findAssetsForAgent(defaultTenantID, "K7")
	if err != nil {
		t.Fatal(err)
	}
	if _, hasAssets := got["assets"]; hasAssets {
		t.Fatal("K7 不是确定命中,不该出现在 assets 里 —— 模型会当成确定答案")
	}
	near, _ := got["nearMatches"].([]map[string]any)
	if len(near) != 1 || near[0]["name"] != "KT-7" {
		t.Fatalf("应给出 KT-7 作为相近候选,得到 %+v", got)
	}
	if got["count"] != 0 {
		t.Fatalf("count 必须是 0 —— 它表示确定命中的数量,候选不算")
	}
}

// 真的没有这台设备时,如实返回空,别硬凑一个候选出来。
func TestFindAssetsEmptyWhenNothingClose(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	if err := store.CreateAsset(&AssetEntry{
		ID: "会议中心::elevator_no_room::KT-7", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "无机房电梯",
		AssetKey: "KT-7", AssetName: "KT-7", LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := srv.findAssetsForAgent(defaultTenantID, "FT-99")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got["nearMatches"]; has {
		t.Fatalf("FT-99 和 KT-7 毫无关系,不该给候选: %+v", got)
	}
	if got["count"] != 0 {
		t.Fatal("应返回 0")
	}
}
