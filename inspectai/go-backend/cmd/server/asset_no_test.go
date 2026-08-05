package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// 设备编号必须是人填的，AI 不能碰。
//
// 这条是数据完整性，不是体验：asset_no 是资产台账的主键(buildAsset 拿它当
// 资产名)。AI 认错一个字符，两台设备就在台账里并成一台，或者凭空多出一台。
// 原来两个电梯模板还给它写死了默认值 "HYZX-WJ-DT01" —— 识别失败的记录全挂
// 到同一个编号下，而且被标成"已确认"，看起来像人核对过的。
func TestAssetNoIsNeverFilledByAI(t *testing.T) {
	// 所有模板的 asset_no 都必须是 ManualOnly，且不带默认值
	for _, tpl := range reportTemplates() {
		for _, f := range tpl.Fields {
			if f.Code != "asset_no" {
				continue
			}
			if !f.ManualOnly {
				t.Errorf("模板 %s 的 asset_no 不是 ManualOnly —— AI 会写它", tpl.Name)
			}
			if f.Source != "manual" {
				t.Errorf("模板 %s 的 asset_no source=%q，应为 manual", tpl.Name, f.Source)
			}
			if f.Default != "" {
				t.Errorf("模板 %s 的 asset_no 带默认值 %q —— 识别失败时所有记录会并成同一台设备",
					tpl.Name, f.Default)
			}
			if !f.Required {
				t.Errorf("模板 %s 的 asset_no 不是必填 —— 空编号会让记录落不到具体设备上", tpl.Name)
			}
		}
	}

	// applyRecognizedFields 必须跳过它，哪怕 AI 明确返回了值
	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到电梯模板")
	}
	rec := &Record{TemplateID: tpl.ID, Fields: initialFieldValues(tpl, "巡检员")}
	applyRecognizedFields(rec, []RecognizedField{
		{Code: "asset_no", Value: "AI瞎猜的编号", Confidence: 0.99},
		{Code: "inspection_time", Value: "2026-08-04 10:00", Confidence: 0.9},
	})
	for _, f := range rec.Fields {
		if f.Code == "asset_no" {
			if f.Value != "" {
				t.Fatalf("AI 把设备编号写成了 %q —— 必须留空等人选", f.Value)
			}
			if f.Source == "ai" {
				t.Fatalf("设备编号的 source 被改成了 ai")
			}
		}
		// 对照:非 ManualOnly 的字段该被正常填上，证明不是整个函数没生效
		if f.Code == "inspection_time" && f.Value != "2026-08-04 10:00" {
			t.Fatalf("普通字段没有被 AI 填上(value=%q)，测试本身可能失效了", f.Value)
		}
	}
}

// 守住 fillAssetNoOptions 依赖的前提:模板与 assetType 一一对应。
//
// 为什么这条要单独测:fillAssetNoOptions 按 assetType 取候选编号,而提交时
// 资产主键 assetIDFor() 是 project+templateID+asset_no —— 带 templateID。
// 两者口径只有在"一个 assetType 只属于一个模板"时才等价。一旦有人给两个
// 模板配了同一个 assetType,巡检员就会在下拉里看到另一个模板的设备编号,
// 选中提交后台账里会多出一台重复设备,而不是挂到原来那台上。
// 那种错在台账上很难查(两台设备名字一模一样),所以在这里拦住。
func TestAssetTypeMapsToSingleTemplate(t *testing.T) {
	owner := map[string]string{}
	for _, tpl := range reportTemplates() {
		at := strings.TrimSpace(tpl.AssetType)
		if at == "" {
			continue
		}
		if prev, dup := owner[at]; dup {
			t.Errorf("assetType %q 同时属于模板 %s 和 %s —— "+
				"设备编号下拉会跨模板串台,提交后台账会多出重复设备。"+
				"要么给它们不同的 assetType,要么把 fillAssetNoOptions 改成按 templateID 取",
				at, prev, tpl.ID)
			continue
		}
		owner[at] = tpl.ID
	}
}

// 设备编号候选必须每次读都重算，并且不能弄丢已经填好的值。
//
// 两条都来自真实场景：
//   1. 记录建好【之后】管理员才在台账里加了这台设备。若候选只在建记录时
//      算一次，巡检员就是选不到它，只能手打 —— 手打差一个字母，台账里就多
//      一台设备，正是这个下拉要防的事。
//   2. 巡检员填的是台账里还没有的新设备（或那台设备后来被改名/删了）。
//      刷新候选时若不把当前值并回去，他打开下拉会发现自己填的编号不在列表里，
//      想确认一下都选不回来。
func TestAssetNoOptionsRefreshAndKeepCurrentValue(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "assetno.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()
	srv := &Server{store: store}

	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到有机房电梯模板")
	}
	mkRec := func(value string) *Record {
		rec := &Record{
			ID: "rec_t", TenantID: defaultTenantID, Project: "会议中心",
			TemplateID: tpl.ID, Fields: initialFieldValues(tpl, "巡检员"),
		}
		for i := range rec.Fields {
			if rec.Fields[i].Code == "asset_no" {
				rec.Fields[i].Value = value
			}
		}
		return rec
	}
	assetNo := func(rec *Record) *FieldValue {
		f, _ := fieldByCode(rec.Fields, "asset_no")
		return f
	}

	// 台账为空 → 保持输入框，不能挡住新设备
	rec := mkRec("")
	srv.fillAssetNoOptions(rec)
	if got := assetNo(rec); len(got.Options) != 0 || got.Kind != "text" {
		t.Fatalf("台账为空时应保持手填，得到 kind=%s options=%v", got.Kind, got.Options)
	}

	// 台账后加的设备，必须在下一次读时就出现
	if err := store.CreateAsset(&AssetEntry{
		ID: "a1", TenantID: defaultTenantID, Project: "会议中心",
		AssetName: "K01", AssetType: tpl.AssetType, TemplateID: tpl.ID,
	}); err != nil {
		t.Fatalf("CreateAsset: %v", err)
	}
	rec = mkRec("")
	srv.fillAssetNoOptions(rec)
	if got := assetNo(rec); got.Kind != "choice" || len(got.Options) != 1 || got.Options[0] != "K01" {
		t.Fatalf("新加的设备没进候选：kind=%s options=%v", got.Kind, got.Options)
	}

	// 已填的新设备编号（台账里没有）必须留在候选里
	rec = mkRec("K99-新装")
	srv.fillAssetNoOptions(rec)
	got := assetNo(rec)
	if !slices.Contains(got.Options, "K99-新装") {
		t.Fatalf("已填的值被刷没了：options=%v", got.Options)
	}
	if !slices.Contains(got.Options, "K01") {
		t.Fatalf("台账里的设备丢了：options=%v", got.Options)
	}
}
