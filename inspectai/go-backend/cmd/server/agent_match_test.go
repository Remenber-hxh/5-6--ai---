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
	got, err := srv.findAssetsForAgent(defaultTenantID, "", "KT-7")
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
	got, err := srv.findAssetsForAgent(defaultTenantID, "", "K7")
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

// 【按项目过滤】各楼的编号各自排,重名很常见。在"会议中心"的对话里问 K01,
// 不能答出另一栋楼的 K01 —— 而"挑名字最接近的那台"对两台同名设备完全无效。
func TestFindAssetsScopedByProject(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	for _, p := range []string{"会议中心", "紫菡雅集"} {
		if err := store.CreateAsset(&AssetEntry{
			ID: p + "::elevator_no_room::K01", TenantID: defaultTenantID,
			Project: p, AssetType: "无机房电梯",
			AssetKey: "K01", AssetName: "K01", LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// 不限项目:两台都返回(由模型反问是哪一台)
	all, err := srv.findAssetsForAgent(defaultTenantID, "", "K01")
	if err != nil {
		t.Fatal(err)
	}
	if all["count"] != 2 {
		t.Fatalf("不限项目应返回 2 台,得到 %v", all["count"])
	}
	// 限定项目:只返回那一台,且必须是对的那个项目
	one, err := srv.findAssetsForAgent(defaultTenantID, "会议中心", "K01")
	if err != nil {
		t.Fatal(err)
	}
	if one["count"] != 1 {
		t.Fatalf("限定项目应只返回 1 台,得到 %v —— 跨项目串数据", one["count"])
	}
	list, _ := one["assets"].([]map[string]any)
	if list[0]["project"] != "会议中心" {
		t.Fatalf("返回的是别的项目: %v", list[0]["project"])
	}
}

// 匹配到超过 10 台时,count 必须是【真实总数】而不是截断后的条数 ——
// 否则模型会理直气壮地说"一共 10 台"。
func TestFindAssetsCountIsBeforeTruncation(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	for i := range 14 {
		name := "DT" + itoaSafe(i)
		if err := store.CreateAsset(&AssetEntry{
			ID: "会议中心::elevator_no_room::" + name, TenantID: defaultTenantID,
			Project: "会议中心", AssetType: "无机房电梯",
			AssetKey: name, AssetName: name, LastStatus: "正常",
		}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := srv.findAssetsForAgent(defaultTenantID, "", "DT")
	if err != nil {
		t.Fatal(err)
	}
	if got["count"] != 14 {
		t.Fatalf("count 应为真实总数 14,得到 %v", got["count"])
	}
	list, _ := got["assets"].([]map[string]any)
	if len(list) != 10 {
		t.Fatalf("列表应截到 10 条,得到 %d", len(list))
	}
	if got["truncated"] != true {
		t.Fatal("截断了必须标出来,否则模型不知道手上是残缺的")
	}
}

// 补零不该影响识别:K07 和 KT-7 的数字都是 7。
func TestLooseAssetMatchIgnoresLeadingZeros(t *testing.T) {
	for _, c := range [][2]string{{"K07", "KT7"}, {"K7", "KT07"}, {"K007", "K7"}} {
		if !looseAssetMatch(c[0], c[1]) {
			t.Errorf("looseAssetMatch(%q,%q) 应为 true —— 补零与否全看当初谁录的", c[0], c[1])
		}
	}
	// 但 07 和 70 仍然是两台
	if looseAssetMatch("K07", "K70") {
		t.Error("K07 和 K70 是两台设备")
	}
}

// 子串命中但数字对不上的,必须降级成候选 —— 问 KT-7 不能确定地答 KT-70。
func TestFindAssetsDemotesAmbiguousSubstring(t *testing.T) {
	store := NewMemStore()
	srv := &Server{store: store}
	if err := store.CreateAsset(&AssetEntry{
		ID: "会议中心::elevator_no_room::KT-70", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "无机房电梯",
		AssetKey: "KT-70", AssetName: "KT-70", LastStatus: "正常",
	}); err != nil {
		t.Fatal(err)
	}
	got, err := srv.findAssetsForAgent(defaultTenantID, "", "KT-7")
	if err != nil {
		t.Fatal(err)
	}
	if _, has := got["assets"]; has {
		t.Fatalf("KT-70 不是 KT-7 的确定命中,不该进 assets: %+v", got)
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
	got, err := srv.findAssetsForAgent(defaultTenantID, "", "FT-99")
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
