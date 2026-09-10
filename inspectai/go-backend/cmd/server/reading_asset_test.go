package main

import (
	"slices"
	"testing"
)

func assetSrv(t *testing.T, assets ...*AssetEntry) *Server {
	t.Helper()
	store := NewMemStore()
	for _, a := range assets {
		if err := store.UpsertAsset(a); err != nil {
			t.Fatal(err)
		}
	}
	return &Server{store: store}
}

func zihanAssets() []*AssetEntry {
	mk := func(name, at string) *AssetEntry {
		return &AssetEntry{
			ID: "紫菡雅集::" + at + "::" + name, TenantID: defaultTenantID,
			Project: "紫菡雅集", AssetType: at, AssetName: name,
		}
	}
	return []*AssetEntry{
		mk("Z1能耗表", "电表"), mk("Z2能耗表", "电表"),
		mk("Z3能耗表", "电表"), mk("Z4能耗表", "电表"),
		mk("生活水表", "水表"), mk("消防水表", "水表"),
	}
}

func energyRec(t *testing.T) *Record {
	t.Helper()
	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("找不到 zihan_energy")
	}
	return &Record{
		ID: "rec_x", TenantID: defaultTenantID, TemplateID: tpl.ID,
		Project: "紫菡雅集", Fields: initialFieldValues(tpl, "巡检员"),
	}
}

// 抄表一条记录抄六台表,照片上没有任何表号标识 —— 每个读数行必须能选设备。
func TestReadingRowsGetAssetPicker(t *testing.T) {
	srv := assetSrv(t, zihanAssets()...)
	rec := energyRec(t)
	// 模板里的 AssetType 平时由迁移 035 写进库;测试进程走的是代码默认模板,
	// 这里手动配上,测的是 fillReadingAssetOptions 本身
	srv.fillReadingAssetOptions(rec)

	for _, code := range []string{"z1_reading", "z2_reading", "z3_reading", "z4_reading"} {
		f, _ := fieldByCode(rec.Fields, code)
		if f == nil {
			t.Fatalf("没有字段 %s", code)
		}
		if len(f.AssetOptions) != 4 {
			t.Errorf("%s 的候选应为 4 块电表,实际 %v", code, f.AssetOptions)
		}
		if slices.Contains(f.AssetOptions, "生活水表") {
			t.Errorf("%s 的候选里混进了水表:%v", code, f.AssetOptions)
		}
	}
	w, _ := fieldByCode(rec.Fields, "living_water_reading")
	if len(w.AssetOptions) != 2 {
		t.Errorf("水表字段的候选应为 2 块水表,实际 %v", w.AssetOptions)
	}
}

// 默认值按字段名对上台账那台:Z1 能耗表读数 → Z1能耗表。
func TestDefaultAssetMatchesFieldLabel(t *testing.T) {
	srv := assetSrv(t, zihanAssets()...)
	rec := energyRec(t)
	srv.fillReadingAssetOptions(rec)

	want := map[string]string{
		"z1_reading": "Z1能耗表", "z2_reading": "Z2能耗表",
		"z3_reading": "Z3能耗表", "z4_reading": "Z4能耗表",
		"living_water_reading": "生活水表", "fire_water_reading": "消防水表",
	}
	for code, name := range want {
		f, _ := fieldByCode(rec.Fields, code)
		if f.AssetName != name {
			t.Errorf("%s 的默认设备应为 %s,实际 %q", code, name, f.AssetName)
		}
	}
}

// 人已经选过的不许被默认值盖掉 —— 那正是"改了又被系统改回去"。
func TestExistingAssetChoiceIsKept(t *testing.T) {
	srv := assetSrv(t, zihanAssets()...)
	rec := energyRec(t)
	f, _ := fieldByCode(rec.Fields, "z1_reading")
	f.AssetName = "Z3能耗表" // 现场发现错位,改到了 Z3
	srv.fillReadingAssetOptions(rec)
	if f.AssetName != "Z3能耗表" {
		t.Errorf("人选的设备被覆盖成了 %q", f.AssetName)
	}
}

// 设备被改名/删掉后,已经选过的那个必须还留在候选里,否则人想确认都选不回来。
func TestRenamedAssetStaysInOptions(t *testing.T) {
	srv := assetSrv(t, zihanAssets()...)
	rec := energyRec(t)
	f, _ := fieldByCode(rec.Fields, "z1_reading")
	f.AssetName = "已经拆掉的老表"
	srv.fillReadingAssetOptions(rec)
	if !slices.Contains(f.AssetOptions, "已经拆掉的老表") {
		t.Errorf("已选的值掉出候选了:%v", f.AssetOptions)
	}
}

// 名字对不上就留空 —— 猜错一台比不猜更糟:
// 选择器上摆着个错的默认值,人扫一眼觉得"系统填好了"就过去了。
func TestNoFuzzyGuessing(t *testing.T) {
	opts := []string{"Z1能耗表", "Z2能耗表"}
	if got := defaultAssetForField("水泵房总表读数", opts); got != "" {
		t.Errorf("名字对不上不该猜,却填了 %q", got)
	}
	if got := defaultAssetForField("Z1 能耗表读数", opts); got != "Z1能耗表" {
		t.Errorf("去掉空格和「读数」后应该对上,实际 %q", got)
	}
	if got := defaultAssetForField("　生活水表　读数", []string{"生活水表"}); got != "生活水表" {
		t.Errorf("全角空格没处理:%q", got)
	}
}

// 台账里还没建这类设备时,不给选择器,记录照常能建。
func TestNoAssetsNoPicker(t *testing.T) {
	srv := assetSrv(t) // 空台账
	rec := energyRec(t)
	srv.fillReadingAssetOptions(rec)
	f, _ := fieldByCode(rec.Fields, "z1_reading")
	if len(f.AssetOptions) != 0 || f.AssetName != "" {
		t.Errorf("空台账不该给候选:%v / %q", f.AssetOptions, f.AssetName)
	}
}

// 跨项目的设备不能混进候选 —— 编号重名时选错了很难查。
func TestOtherProjectAssetsExcluded(t *testing.T) {
	other := &AssetEntry{
		ID: "会议中心::电表::Z1能耗表", TenantID: defaultTenantID,
		Project: "会议中心", AssetType: "电表", AssetName: "会议中心Z1",
	}
	srv := assetSrv(t, append(zihanAssets(), other)...)
	rec := energyRec(t)
	srv.fillReadingAssetOptions(rec)
	f, _ := fieldByCode(rec.Fields, "z1_reading")
	if slices.Contains(f.AssetOptions, "会议中心Z1") {
		t.Errorf("别的项目的设备混进来了:%v", f.AssetOptions)
	}
}

// 没配 AssetType 的模板一个字段都不该被动到(电梯、扶梯那些)。
func TestTemplatesWithoutAssetTypeUntouched(t *testing.T) {
	srv := assetSrv(t, zihanAssets()...)
	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到电梯模板")
	}
	rec := &Record{
		ID: "rec_e", TenantID: defaultTenantID, TemplateID: tpl.ID,
		Project: "会议中心", Fields: initialFieldValues(tpl, "巡检员"),
	}
	srv.fillReadingAssetOptions(rec)
	for _, f := range rec.Fields {
		if len(f.AssetOptions) != 0 || f.AssetName != "" {
			t.Errorf("没配 AssetType 的模板被动了:%s → %v / %q", f.Code, f.AssetOptions, f.AssetName)
		}
	}
}

// 代码种子和迁移 035 必须配同一份设备类型。
//
// 【为什么要专门守】新装的空库只灌种子、不跑 035;升级的库只跑 035、不看种子。
// 两边漂了的话,一边有选择器一边没有,而且哪边都不报错 —— 只表现成
// "换个环境这个功能就没了"。
func TestSeedAndMigrationAgreeOnAssetTypes(t *testing.T) {
	want := map[string]string{
		"z1_reading": "电表", "z2_reading": "电表",
		"z3_reading": "电表", "z4_reading": "电表",
		"living_water_reading": "水表", "fire_water_reading": "水表",
	}
	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("找不到 zihan_energy")
	}
	got := map[string]string{}
	for _, f := range tpl.Fields {
		if f.AssetType != "" {
			got[f.Code] = f.AssetType
		}
	}
	if len(got) != len(want) {
		t.Fatalf("代码种子配的设备类型和迁移 035 对不上:种子 %v,期望 %v", got, want)
	}
	for code, at := range want {
		if got[code] != at {
			t.Errorf("%s 的设备类型:种子是 %q,迁移 035 写的是 %q", code, got[code], at)
		}
	}
}
