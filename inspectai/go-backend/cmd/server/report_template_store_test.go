package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

// 模板从代码搬进数据库。
//
// 【这一步最怕的不是报错,是"搬完之后悄悄变了"】少一个字段、选项顺序乱了、
// 必填变成选填 —— 全都不报错,要等巡检员提交时才发现填不了或校验不住。
// 所以这里逐字段比对搬运前后是否完全一致。

func templateStoreForTest(t *testing.T) Store {
	t.Helper()
	return NewMemStore()
}

// 【核心】灌进去再读出来,必须和代码里那份一模一样。
func TestSeedRoundTripIsIdentical(t *testing.T) {
	store := templateStoreForTest(t)
	base := baseReportTemplates()

	for _, tpl := range base {
		if err := store.UpsertReportTemplate(tpl); err != nil {
			t.Fatalf("写入 %s: %v", tpl.ID, err)
		}
	}
	got, err := store.ListReportTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(base) {
		t.Fatalf("模板数量对不上:代码 %d 个,库里 %d 个", len(base), len(got))
	}
	for i := range base {
		if !reflect.DeepEqual(base[i], got[i]) {
			t.Errorf("模板 %s 搬运前后不一致\n代码:%+v\n库里:%+v",
				base[i].ID, base[i], got[i])
		}
	}
}

// 字段顺序必须保住 —— 顺序错了表单就乱了,而这不会报错。
func TestFieldOrderPreserved(t *testing.T) {
	store := templateStoreForTest(t)
	src, ok := templateByID("ups_room") // 字段最多的一个
	if !ok {
		t.Fatal("找不到 ups_room")
	}
	if len(src.Fields) < 10 {
		t.Fatalf("ups_room 应该有十几个字段,实际 %d", len(src.Fields))
	}
	if err := store.UpsertReportTemplate(src); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ListReportTemplates()
	if len(got) != 1 {
		t.Fatalf("应有 1 个模板,实际 %d", len(got))
	}
	for i, f := range src.Fields {
		if got[0].Fields[i].Code != f.Code {
			t.Fatalf("第 %d 个字段错位:期望 %s,实际 %s", i, f.Code, got[0].Fields[i].Code)
		}
	}
}

// 选项(单选的可选值)不能在往返中丢。
func TestChoiceOptionsSurviveRoundTrip(t *testing.T) {
	store := templateStoreForTest(t)
	src, _ := templateByID("hot_water_room")
	_ = store.UpsertReportTemplate(src)
	got, _ := store.ListReportTemplates()

	var checked int
	for _, f := range got[0].Fields {
		if f.Kind != "choice" {
			continue
		}
		checked++
		if len(f.Options) == 0 {
			t.Errorf("单选字段 %s 的选项丢了", f.Code)
		}
	}
	if checked == 0 {
		t.Fatal("这个模板本该有单选字段 —— 断言没生效")
	}
}

// 保存是整份替换:删掉的字段必须真的没了,不能残留。
func TestUpsertReplacesFieldsWholesale(t *testing.T) {
	store := templateStoreForTest(t)
	src, _ := templateByID("hot_water_room")
	_ = store.UpsertReportTemplate(src)

	trimmed := src
	trimmed.Fields = src.Fields[:2]
	_ = store.UpsertReportTemplate(trimmed)

	got, _ := store.ListReportTemplates()
	if len(got[0].Fields) != 2 {
		t.Errorf("删掉的字段没被清掉,实际还剩 %d 个", len(got[0].Fields))
	}
}

// ===== 缓存与回退 =====

// 【最要紧的一条】缓存绝不能被清空。
//
// 模板列表为空 = 全系统建不了记录、表单渲染不出来、移动端一个模板都选不到。
// 而症状是"什么都点不了",没人会想到是模板表空了。
func TestEmptyNeverClearsTheCache(t *testing.T) {
	setReportTemplateCache(baseReportTemplates())
	defer setReportTemplateCache(baseReportTemplates()) // 别影响其他测试

	before := len(currentReportTemplates())
	if before == 0 {
		t.Fatal("前置条件不成立:缓存本该有模板")
	}
	setReportTemplateCache(nil)
	setReportTemplateCache([]ReportTemplate{})
	if after := len(currentReportTemplates()); after != before {
		t.Errorf("空列表把缓存清了:%d → %d —— 现场会整个停工", before, after)
	}
}

// 库里没有时回退到代码里那份,而不是返回空。
func TestFallsBackToCodeWhenCacheEmpty(t *testing.T) {
	templateCache.mu.Lock()
	saved := templateCache.tpls
	templateCache.tpls = nil
	templateCache.mu.Unlock()
	defer func() {
		templateCache.mu.Lock()
		templateCache.tpls = saved
		templateCache.mu.Unlock()
	}()

	if got := len(currentReportTemplates()); got != len(baseReportTemplates()) {
		t.Errorf("缓存为空时应回退到代码里那份,实际 %d 个", got)
	}
}

// 【调用方会就地改模板】所以每次必须给副本 —— 否则一次请求的改动
// 会悄悄写回全局缓存,下一次请求看到的就是被污染过的模板。
func TestCallersCannotMutateTheCache(t *testing.T) {
	setReportTemplateCache(baseReportTemplates())
	defer setReportTemplateCache(baseReportTemplates())

	first := currentReportTemplates()
	if len(first) == 0 {
		t.Fatal("前置条件不成立")
	}
	originalName := first[0].Name
	first[0].Name = "被改坏的名字"

	second := currentReportTemplates()
	if second[0].Name != originalName {
		t.Errorf("缓存被调用方改掉了:%q —— 下一次请求拿到的就是脏数据", second[0].Name)
	}
}

// 首次启动:库空 → 灌种子 → 之后以库为准。
func TestLoadSeedsWhenEmptyThenReadsFromStore(t *testing.T) {
	store := templateStoreForTest(t)
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}
	inStore, _ := store.ListReportTemplates()
	if len(inStore) != len(baseReportTemplates()) {
		t.Fatalf("种子没灌全:期望 %d,实际 %d", len(baseReportTemplates()), len(inStore))
	}

	// 再跑一次不该重复灌,也不该把库里的改动冲掉
	renamed := inStore[0]
	renamed.Name = "改过的名字"
	_ = store.UpsertReportTemplate(renamed)
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ListReportTemplates()
	if len(after) != len(inStore) {
		t.Errorf("重复灌种子了:%d → %d", len(inStore), len(after))
	}
	var found bool
	for _, x := range after {
		if x.Name == "改过的名字" {
			found = true
		}
	}
	if !found {
		t.Error("库里的改动被种子冲掉了 —— 那样后台改模板永远存不住")
	}
}

// ===== 真库往返 =====
//
// 【MemStore 那份证明不了这条】它把结构体原样存下来,序列化根本没发生。
// 而真正会出错的正是序列化:选项存成 JSON、布尔存成 0/1、字段顺序靠
// sort_no ——每一样都可能在往返中悄悄变形,且不报错。
func TestSQLiteRoundTripIsIdentical(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "tpl.db"))
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	base := baseReportTemplates()
	for _, tpl := range base {
		if err := store.UpsertReportTemplate(tpl); err != nil {
			t.Fatalf("写入 %s: %v", tpl.ID, err)
		}
	}
	got, err := store.ListReportTemplates()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(base) {
		t.Fatalf("模板数对不上:代码 %d,库里 %d", len(base), len(got))
	}
	// 库里按 sort_no/id 排,代码里是书写顺序 —— 按 id 对齐再比
	byID := map[string]ReportTemplate{}
	for _, g := range got {
		byID[g.ID] = g
	}
	for _, want := range base {
		g, ok := byID[want.ID]
		if !ok {
			t.Errorf("模板 %s 存进去了却读不回来", want.ID)
			continue
		}
		if !reflect.DeepEqual(want, g) {
			t.Errorf("模板 %s 经过真库往返后变了\n代码:%+v\n库里:%+v", want.ID, want, g)
		}
	}
}

// 单选选项经过 JSON 往返后,内容和顺序都不能变 ——
// 顺序变了,移动端下拉里"正常/异常"就可能颠倒。
func TestSQLiteChoiceOptionsExactOrder(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "opt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	src, _ := templateByID("ups_room")
	if err := store.UpsertReportTemplate(src); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ListReportTemplates()
	want := map[string][]string{}
	for _, f := range src.Fields {
		if len(f.Options) > 0 {
			want[f.Code] = f.Options
		}
	}
	if len(want) == 0 {
		t.Fatal("这个模板本该有单选字段 —— 断言没生效")
	}
	for _, f := range got[0].Fields {
		w, ok := want[f.Code]
		if !ok {
			continue
		}
		if !reflect.DeepEqual(w, f.Options) {
			t.Errorf("字段 %s 的选项变了:期望 %v,实际 %v", f.Code, w, f.Options)
		}
	}
}

// 停用的模板不出现在列表里 —— 停用是"下线但保留历史记录"的手段。
func TestSQLiteDisabledTemplateHidden(t *testing.T) {
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "dis.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	src, _ := templateByID("hot_water_room")
	_ = store.UpsertReportTemplate(src)
	if _, err := store.db.Exec(`UPDATE report_templates SET disabled=1 WHERE id=?`, src.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := store.ListReportTemplates()
	for _, g := range got {
		if g.ID == src.ID {
			t.Error("停用的模板不该出现在列表里")
		}
	}
}
