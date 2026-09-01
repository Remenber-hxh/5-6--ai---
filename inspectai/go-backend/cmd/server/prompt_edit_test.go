package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 提示词编辑。
//
// 这一块的失败方式全是【静默】的:改完不报错、界面照常、下一张照片开始
// 悄悄误判。所以每一条"看不见的规则"都要钉住 —— 尤其是回退语义,
// 因为它决定的是"模型到底收到了哪一段文字",而那件事在界面上看不出来。

func promptTestStore(t *testing.T) Store {
	t.Helper()
	store := NewMemStore()
	if err := ensurePromptTemplateSeeds(store); err != nil {
		t.Fatalf("灌种子失败: %v", err)
	}
	return store
}

// raw 模式:人写什么,模型就收到什么,一个字不加。
//
// 【不能悄悄拼公共模块】编辑器里看到的必须就是模型收到的 ——
// 否则人调半天,调的是一份自己看不见的东西。
func TestRawPromptGoesThroughVerbatim(t *testing.T) {
	body := "# 我自己写的提示词\n只回答一个字段:temperature。"
	got := renderPromptText(PromptTemplate{
		ID: "x", Name: "X", Mode: PromptModeRaw, RawText: body,
	})
	if got != body {
		t.Errorf("raw 正文被改动了。\n期望:%q\n实际:%q", body, got)
	}
}

// 结构化模板不受影响 —— 加 raw 模式不能动到已经在跑的那两个。
func TestStructuredStillRenders(t *testing.T) {
	store := promptTestStore(t)
	text, ok := renderPromptViaStore(store, "elevator_machine_room")
	if !ok {
		t.Fatal("有机房电梯应该渲染得出来")
	}
	for _, want := range []string{"字段映射", "置信度", "输出"} {
		if !strings.Contains(text, want) {
			t.Errorf("渲染结果缺少「%s」段", want)
		}
	}
}

// 【最要紧的一条】raw 正文清空 = 回退内置,不是"发一段空提示词给模型"。
//
// 发空提示词的话,模型会自由发挥,返回一堆没人要的字段,而且不报错。
func TestEmptyRawFallsBackInsteadOfSendingBlank(t *testing.T) {
	store := NewMemStore()
	if err := store.UpsertPromptTemplate(PromptTemplate{
		ID: "hot_water_room", Name: "热水机房", Mode: PromptModeRaw, RawText: "   \n  ",
	}); err != nil {
		t.Fatal(err)
	}
	text, ok := renderPromptViaStore(store, "hot_water_room")
	if ok {
		t.Fatalf("正文为空时不该下发 promptText,实际下发了 %q", text)
	}
}

// 老数据没有 mode 字段 —— 默认必须落在"和以前一样"那一边。
func TestMissingModeDefaultsToStructured(t *testing.T) {
	tpl := PromptTemplate{ID: "x", Name: "X", Fields: []PromptField{
		{Code: "a", Label: "甲", Mode: ModeVisual, YesWhen: "看得见"},
	}}
	if tpl.isRaw() {
		t.Fatal("mode 为空时不能当成 raw —— 老数据会被整个当成空正文,直接失去提示词")
	}
	if !strings.Contains(renderPromptText(tpl), "字段映射") {
		t.Error("mode 为空时应按结构化渲染")
	}
}

// ===== 版本留痕 =====

// 存进去要能原样取回来 —— 取回来不一样的话,回滚回滚的是别的东西。
func TestVersionRoundTrip(t *testing.T) {
	store := NewMemStore()
	orig := PromptTemplate{
		ID: "hot_water_room", Name: "热水机房", Mode: PromptModeRaw,
		RawText: "第一版正文",
	}
	if err := snapshotPromptTemplate(store, orig, "Vesper", "初稿"); err != nil {
		t.Fatal(err)
	}
	vs, err := store.ListPromptVersions("hot_water_room", 10)
	if err != nil || len(vs) != 1 {
		t.Fatalf("应有 1 条版本,实际 %d 条 (%v)", len(vs), err)
	}
	if vs[0].Author != "Vesper" || vs[0].Note != "初稿" {
		t.Errorf("作者/备注没存住: %+v", vs[0])
	}
	full, ok, _ := store.GetPromptVersion(vs[0].ID)
	if !ok {
		t.Fatal("按 id 取不回版本")
	}
	back, err := full.Template()
	if err != nil {
		t.Fatal(err)
	}
	if back.RawText != "第一版正文" || back.Mode != PromptModeRaw {
		t.Errorf("取回来的内容不是存进去的那份: %+v", back)
	}
}

// 版本按时间倒序 —— 顺序反了的话,"最近一版"指向的是最老那份,
// 而人会照着它回滚。
func TestVersionsNewestFirst(t *testing.T) {
	store := NewMemStore()
	for _, txt := range []string{"一", "二", "三"} {
		if err := snapshotPromptTemplate(store,
			PromptTemplate{ID: "t", Name: "T", Mode: PromptModeRaw, RawText: txt},
			"Vesper", txt); err != nil {
			t.Fatal(err)
		}
	}
	vs, _ := store.ListPromptVersions("t", 10)
	if len(vs) != 3 || vs[0].Note != "三" {
		t.Fatalf("最新的应排在最前,实际 %+v", vs)
	}
}

// 版本按模板隔离 —— 串了的话,回滚 A 会把 B 的内容贴过来。
func TestVersionsScopedToTemplate(t *testing.T) {
	store := NewMemStore()
	_ = snapshotPromptTemplate(store, PromptTemplate{ID: "a", Name: "A"}, "u", "")
	_ = snapshotPromptTemplate(store, PromptTemplate{ID: "b", Name: "B"}, "u", "")
	vs, _ := store.ListPromptVersions("a", 10)
	if len(vs) != 1 || vs[0].TemplateID != "a" {
		t.Fatalf("只该拿到 a 的版本,实际 %+v", vs)
	}
}

// 【版本功能是后加的】库里早就存在、一条版本都没有的模板,
// 第一次保存前必须先把「改之前」存成基线 —— 否则原样再也拿不回来。
func TestFirstSaveKeepsThePreEditContent(t *testing.T) {
	store := promptTestStore(t)
	const id = "elevator_machine_room"

	before, ok, _ := store.GetPromptTemplate(id)
	if !ok {
		t.Fatal("种子里应该有有机房电梯")
	}
	fieldsBefore := len(before.Fields)
	if fieldsBefore == 0 {
		t.Fatal("种子模板不该是空字段表")
	}

	// 模拟一次「改坏了」:整表换成一个字段
	ensureBaselineVersion(store, id)
	broken := before
	broken.Fields = []PromptField{{Code: "x", Label: "只剩一个", Mode: ModeVisual}}
	if err := store.UpsertPromptTemplate(broken); err != nil {
		t.Fatal(err)
	}
	_ = snapshotPromptTemplate(store, broken, "Vesper", "改坏了")

	vs, _ := store.ListPromptVersions(id, 10)
	if len(vs) != 2 {
		t.Fatalf("应有基线 + 本次共 2 条版本,实际 %d 条", len(vs))
	}
	// 最老那条就是基线,回滚它应该拿回原来的字段表
	baseline, _, _ := store.GetPromptVersion(vs[len(vs)-1].ID)
	restored, err := baseline.Template()
	if err != nil {
		t.Fatal(err)
	}
	if len(restored.Fields) != fieldsBefore {
		t.Errorf("基线应保留改动前的 %d 个字段,实际 %d 个 —— 改之前的样子丢了",
			fieldsBefore, len(restored.Fields))
	}
}

// 基线只写一次 —— 每次保存都写一条"初始版本"的话,历史列表会被
// 一堆同名条目淹掉,真正想找的那一版反而翻不到。
func TestBaselineWrittenOnlyOnce(t *testing.T) {
	store := promptTestStore(t)
	const id = "elevator_no_room"
	ensureBaselineVersion(store, id)
	ensureBaselineVersion(store, id)
	ensureBaselineVersion(store, id)
	vs, _ := store.ListPromptVersions(id, 10)
	if len(vs) != 1 {
		t.Errorf("基线应只写一条,实际 %d 条", len(vs))
	}
}

// 未迁移的模板要能打开编辑器 —— 打不开的话,那八个模板在界面上
// 根本不存在,人只会以为"提示词就这两个能改"。
func TestDraftForTemplateWithoutDBRow(t *testing.T) {
	store := promptTestStore(t)
	for _, id := range []string{"fire_pump", "ups_room", "water_pump", "hot_water_room"} {
		draft, ok := promptTemplateOrDraft(store, id)
		if !ok {
			t.Errorf("%s 应该能打开编辑器(哪怕库里还没有)", id)
			continue
		}
		if draft.Name == "" {
			t.Errorf("%s 的草稿应带上模板名", id)
		}
		if draft.Mode != PromptModeRaw {
			t.Errorf("%s 的草稿应是 raw 模式,实际 %q", id, draft.Mode)
		}
		// 【草稿不能改变任何行为】只是打开看看,没保存,
		// 运行时必须还是走内置 .md。
		if _, ok := renderPromptViaStore(store, id); ok {
			t.Errorf("%s 没保存过就不该下发 promptText", id)
		}
	}
}

// 不存在的模板 id 仍然要拒绝 —— 否则随便一个字符串都能凭空造出模板。
func TestDraftRejectsUnknownTemplate(t *testing.T) {
	store := promptTestStore(t)
	if _, ok := promptTemplateOrDraft(store, "not_a_real_template"); ok {
		t.Error("不存在的模板不该造出草稿")
	}
}

// ===== 走完整 HTTP 链路 =====
//
// 单测能证明规则对,但证明不了"后台点下去真的走得通" ——
// 路由拆错一个 segment、权限漏一层,单测全绿而界面全是 404。

func putJSON(t *testing.T, srv *Server, path, tok, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 列表要列出全部十个业务模板 —— 只列 DB 的话,那八个在界面上根本不存在。
func TestListShowsEveryBusinessTemplate(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	if err := ensurePromptTemplateSeeds(server.store); err != nil {
		t.Fatal(err)
	}
	got := requestWithToken(server, http.MethodGet, "/api/prompt/templates", tokens["admin"])
	if got.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", got.Code, got.Body.String())
	}
	body := got.Body.String()
	// 这三个是从来没有提示词的点位 —— 最需要在界面上看得见
	for _, id := range []string{"fire_pump", "ups_room", "water_pump", "hot_water_room", "escalator"} {
		if !strings.Contains(body, id) {
			t.Errorf("列表里少了 %s —— 它在后台就不存在,没人改得了它的提示词", id)
		}
	}
	// 没自定义过的要标成 customized:false,否则界面看不出谁还在用内置
	if !strings.Contains(body, `"customized":false`) {
		t.Error("应能区分「已自定义」和「仍用内置」")
	}
}

// 整段文本:存进去 → 渲染出来必须一字不差。
func TestRawSaveThenRenderOverHTTP(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	tok := tokens["admin"]
	const id = "fire_pump"

	// 先能打开(库里还没有这一行)
	got := requestWithToken(server, http.MethodGet, "/api/prompt/templates/"+id, tok)
	if got.Code != http.StatusOK {
		t.Fatalf("未迁移的模板应能打开编辑器,code=%d body=%s", got.Code, got.Body.String())
	}

	rec := putJSON(t, server, "/api/prompt/templates/"+id, tok,
		`{"id":"fire_pump","name":"消防泵房","mode":"raw","rawText":"# 消防泵房\n只看压力表读数。","fields":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("保存失败 code=%d body=%s", rec.Code, rec.Body.String())
	}
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/"+id+"/render", tok)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "只看压力表读数") {
		t.Fatalf("渲染没反映刚存的正文: code=%d body=%s", got.Code, got.Body.String())
	}
}

// 结构化模板字段表被清空 → 必须拦住。
// 【放过去就是静默失灵】只有表头没有判定项的提示词,模型照跑,结果全空。
func TestEmptyFieldTableRejected(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	if err := ensurePromptTemplateSeeds(server.store); err != nil {
		t.Fatal(err)
	}
	rec := putJSON(t, server, "/api/prompt/templates/elevator_machine_room", tokens["admin"],
		`{"id":"elevator_machine_room","name":"电梯","mode":"structured","fields":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空字段表应被拒,实际 code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// 改坏 → 回滚 → 内容真的回来了。这条不过,版本功能就只是个装饰。
func TestSaveThenRollbackOverHTTP(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	tok := tokens["admin"]
	const id = "ups_room"

	putOK := func(body string) {
		t.Helper()
		if rec := putJSON(t, server, "/api/prompt/templates/"+id, tok, body); rec.Code != http.StatusOK {
			t.Fatalf("保存失败 code=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	putOK(`{"id":"ups_room","name":"UPS","mode":"raw","rawText":"好的那一版","fields":[]}`)
	putOK(`{"id":"ups_room","name":"UPS","mode":"raw","rawText":"改坏的那一版","fields":[]}`)

	got := requestWithToken(server, http.MethodGet, "/api/prompt/templates/"+id+"/versions", tok)
	if got.Code != http.StatusOK {
		t.Fatalf("版本列表 code=%d body=%s", got.Code, got.Body.String())
	}
	var vl struct {
		Versions []PromptVersion `json:"versions"`
	}
	if err := json.Unmarshal(got.Body.Bytes(), &vl); err != nil {
		t.Fatal(err)
	}
	if len(vl.Versions) != 2 {
		t.Fatalf("应有 2 条版本,实际 %d: %s", len(vl.Versions), got.Body.String())
	}
	// 倒序:[0] 是「改坏的」,[1] 是「好的」
	good := vl.Versions[1]

	req := httptest.NewRequest(http.MethodPost,
		"/api/prompt/templates/"+id+"/versions/"+good.ID+"/restore", nil)
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("回滚失败 code=%d body=%s", rec.Code, rec.Body.String())
	}

	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/"+id+"/render", tok)
	if !strings.Contains(got.Body.String(), "好的那一版") {
		t.Fatalf("回滚后没拿回旧内容: %s", got.Body.String())
	}
	// 【回滚本身也要留痕】否则「回滚错了」就没得救
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/"+id+"/versions", tok)
	vl.Versions = nil
	_ = json.Unmarshal(got.Body.Bytes(), &vl)
	if len(vl.Versions) != 3 {
		t.Errorf("回滚应再留一条版本(共 3 条),实际 %d 条", len(vl.Versions))
	}
}

// 版本接口同样要挡住没权限的人 —— 历史里是完整提示词正文。
func TestPromptVersionsRequirePermission(t *testing.T) {
	server, tokens := newRecordAccessTestServer(t)
	got := requestWithToken(server, http.MethodGet,
		"/api/prompt/templates/ups_room/versions", tokens["inspector"])
	if got.Code == http.StatusOK {
		t.Errorf("巡检员不该看得到提示词历史,实际 code=%d", got.Code)
	}
}
