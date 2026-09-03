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

// isolateTemplateCache 用完把全局模板缓存还原。
//
// 【必须每个碰模板的测试都调】templateCache 是进程级的可变状态:
// 一个测试把它设成自己那份,后面所有测试都吃到脏数据 ——
// 而症状是"单独跑过、一起跑挂",最难查的那一类。
func isolateTemplateCache(t *testing.T) {
	t.Helper()
	templateCache.mu.RLock()
	saved := templateCache.tpls
	templateCache.mu.RUnlock()
	t.Cleanup(func() {
		templateCache.mu.Lock()
		templateCache.tpls = saved
		templateCache.mu.Unlock()
	})
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
	isolateTemplateCache(t)
	setReportTemplateCache(defaultReportTemplates())
	defer setReportTemplateCache(defaultReportTemplates()) // 别影响其他测试

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
	isolateTemplateCache(t)
	setReportTemplateCache(defaultReportTemplates())

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
	isolateTemplateCache(t)
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

// ===== 合并迁移(真库) =====

// 升级路径:库里已有旧的 prompt_templates,迁移要把判定规则搬到字段上。
//
// 【搬错比没搬更糟】按 code 精确对应,对不上就跳过 —— 贴错的话 AI 会拿着
// A 字段的判定规则去判 B 字段,而且不报错。
func TestMigrationMovesPromptRulesOntoFields(t *testing.T) {
	isolateTemplateCache(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "merge.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// 模拟升级前的状态:模板在库里(没有判定规则),提示词在旧表里
	for _, tpl := range baseReportTemplates() {
		if err := store.UpsertReportTemplate(tpl); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range promptTemplateSeeds() {
		if err := store.UpsertPromptTemplate(p); err != nil {
			t.Fatal(err)
		}
	}
	// 确认前置:此刻字段上还没有判定规则
	pre, _ := store.ListReportTemplates()
	for _, tpl := range pre {
		if templateHasJudgeRules(tpl) {
			t.Fatalf("前置条件不成立:%s 还没迁移就已经有判定规则了", tpl.ID)
		}
	}

	if err := store.backfillPromptIntoTemplates(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	post, _ := store.ListReportTemplates()
	byID := map[string]ReportTemplate{}
	for _, tpl := range post {
		byID[tpl.ID] = tpl
	}
	for _, seed := range promptTemplateSeeds() {
		tpl, ok := byID[seed.ID]
		if !ok {
			t.Errorf("模板 %s 不见了", seed.ID)
			continue
		}
		if tpl.Scene != seed.Scene {
			t.Errorf("%s 的场景描述没搬过来", seed.ID)
		}
		if len(tpl.ExpectedPhotos) != len(seed.ExpectedPhotos) {
			t.Errorf("%s 的期望照片没搬全:%d → %d",
				seed.ID, len(seed.ExpectedPhotos), len(tpl.ExpectedPhotos))
		}
		// 逐条核判定规则,按 code 对
		want := map[string]PromptField{}
		for _, f := range seed.Fields {
			want[f.Code] = f
		}
		var checked int
		for _, f := range tpl.Fields {
			w, ok := want[f.Code]
			if !ok {
				continue
			}
			checked++
			if f.JudgeMode != w.Mode || f.YesWhen != w.YesWhen ||
				f.NoWhen != w.NoWhen || f.SkipWhen != w.SkipWhen {
				t.Errorf("%s.%s 的判定规则搬错了\n期望 mode=%q yes=%q\n实际 mode=%q yes=%q",
					seed.ID, f.Code, w.Mode, w.YesWhen, f.JudgeMode, f.YesWhen)
			}
		}
		if checked == 0 {
			t.Errorf("%s 一条判定规则都没搬过来 —— AI 会失去这个模板的全部判断依据", seed.ID)
		}
	}
}

// 迁移可以重复跑(升级中途失败要能重来)。
func TestMigrationIsRepeatable(t *testing.T) {
	isolateTemplateCache(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "twice.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, tpl := range baseReportTemplates() {
		_ = store.UpsertReportTemplate(tpl)
	}
	for _, p := range promptTemplateSeeds() {
		_ = store.UpsertPromptTemplate(p)
	}
	for i := 0; i < 3; i++ {
		if err := store.migMergePromptIntoTemplate(); err != nil {
			t.Fatalf("第 %d 次跑迁移失败: %v", i+1, err)
		}
	}
	post, _ := store.ListReportTemplates()
	if len(post) != len(baseReportTemplates()) {
		t.Errorf("跑三次之后模板数变了:%d", len(post))
	}
}

// 新加的列在存量行上是 NULL,读的时候不能炸。
//
// 【这个 bug 真的溜到了运行环境才暴露】MySQL 的 TEXT 列加不了 DEFAULT,
// 升级后存量行全是 NULL,直接扫进 string 会报
// "converting NULL to string is unsupported" —— 整份模板加载失败,
// 系统退回代码里那份,于是【后台改的模板全部不生效】,而界面上只有
// 一行启动日志能看出来。
//
// 之前测不出来是因为测试只跑 SQLite,而那边我给了 NOT NULL DEFAULT ''。
// 这里显式把列置成 NULL 来复现。
func TestNullColumnsDoNotBreakLoading(t *testing.T) {
	isolateTemplateCache(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "nulls.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	src, _ := templateByID("hot_water_room")
	if err := store.UpsertReportTemplate(src); err != nil {
		t.Fatal(err)
	}
	// 模拟 MySQL 升级后的样子:后加的列全是 NULL
	for _, col := range []string{"scene", "expected_photos", "prompt_mode", "raw_text"} {
		if _, err := store.db.Exec(`UPDATE report_templates SET ` + col + `=NULL`); err != nil {
			t.Fatalf("置 NULL 失败 %s: %v", col, err)
		}
	}
	for _, col := range []string{"judge_mode", "judge_group", "yes_when", "no_when", "skip_when", "judge_note", "options"} {
		if _, err := store.db.Exec(`UPDATE report_template_fields SET ` + col + `=NULL`); err != nil {
			t.Fatalf("置 NULL 失败 %s: %v", col, err)
		}
	}

	got, err := store.ListReportTemplates()
	if err != nil {
		t.Fatalf("存量行有 NULL 就读不出来了: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应读回 1 个模板,实际 %d", len(got))
	}
	if len(got[0].Fields) != len(src.Fields) {
		t.Errorf("字段数对不上:期望 %d,实际 %d", len(src.Fields), len(got[0].Fields))
	}
	// NULL 读成空串,不是让整份加载失败
	if got[0].Scene != "" || got[0].RawText != "" {
		t.Errorf("NULL 应读成空串,实际 scene=%q raw=%q", got[0].Scene, got[0].RawText)
	}
}

// ===== 迁移 027:覆盖层并进底表 =====

// 【覆盖层的值必须赢】它是当前正在生效的那份;底表里可能还是代码默认值。
// 搬反的话,所有人在后台配过的必填和张数会一次性回退到出厂设置,
// 而且没有任何提示 —— 等有人发现"我配的必填怎么没了"已经过去几天。
func TestOverlayWinsWhenMerging(t *testing.T) {
	isolateTemplateCache(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "rules.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })

	// 升级前:模板在库里(用代码默认值),配置在覆盖层里
	for _, tpl := range defaultReportTemplates() {
		if err := store.UpsertReportTemplate(tpl); err != nil {
			t.Fatal(err)
		}
	}
	base, _ := store.ListReportTemplates()
	var target ReportTemplate
	for _, tpl := range base {
		if tpl.ID == "zihan_energy" {
			target = tpl
		}
	}
	// 找一个默认非必填的字段,在覆盖层里把它设成必填
	var code string
	for _, f := range target.Fields {
		if !f.Required && f.Code != "asset_no" {
			code = f.Code
			break
		}
	}
	if code == "" {
		t.Fatal("前置条件不成立:找不到默认非必填的字段")
	}
	if err := store.ReplaceTemplateFieldRules("zihan_energy", []*TemplateFieldRule{
		{TemplateID: "zihan_energy", FieldCode: code, Required: true},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SetTemplateMinImages("zihan_energy", 3, "测试"); err != nil {
		t.Fatal(err)
	}

	if err := store.migMergeFieldRulesIntoTemplate(); err != nil {
		t.Fatalf("迁移失败: %v", err)
	}

	after, _ := store.ListReportTemplates()
	for _, tpl := range after {
		if tpl.ID != "zihan_energy" {
			continue
		}
		if tpl.MinImages != 3 {
			t.Errorf("最少张数没搬过来:期望 3,实际 %d —— 后台配过的值被出厂默认冲掉了", tpl.MinImages)
		}
		for _, f := range tpl.Fields {
			if f.Code == code && !f.Required {
				t.Errorf("字段 %s 的必填没搬过来 —— 后台配过的必填全丢了", code)
			}
		}
	}
}

// 【最少张数为 0 的一律跳过】0 的含义是"没配过",不是"不限"。
// 当成配置搬过去会把模板自带的默认张数清成 0 —— 现场从此一张照片就能提交,
// 而且没人会注意到要求消失了。
func TestZeroMinImagesIsNotMigrated(t *testing.T) {
	isolateTemplateCache(t)
	store, err := NewSQLiteStore(filepath.Join(t.TempDir(), "zero.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	for _, tpl := range defaultReportTemplates() {
		_ = store.UpsertReportTemplate(tpl)
	}
	if err := store.SetTemplateMinImages("zihan_energy", 0, "测试"); err != nil {
		t.Fatal(err)
	}
	if err := store.migMergeFieldRulesIntoTemplate(); err != nil {
		t.Fatal(err)
	}
	after, _ := store.ListReportTemplates()
	for _, tpl := range after {
		if tpl.ID == "zihan_energy" && tpl.MinImages == 0 {
			t.Error("配置里的 0 被当成了「不限」搬进底表 —— 五张照片的要求悄悄消失了")
		}
	}
}
