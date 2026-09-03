package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 模板编辑的护栏。
//
// 【这些规则漏一条,后果都落在历史数据上,而且不报错】改了字段标识,
// 旧记录里那一项就永远读不出来;删了模板,那些记录的字段定义就没了。
// 症状是"以前填过的内容不见了",要等有人翻旧记录才发现,那时候
// 已经没法回溯是哪次改动造成的。

func tplWith(id string, fields ...TemplateField) ReportTemplate {
	return ReportTemplate{ID: id, Name: "测试模板", Fields: fields}
}

func fieldOf(code, label, kind string, opts ...string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: kind, Options: opts}
}

func assetNoOf() TemplateField {
	return TemplateField{Code: "asset_no", Label: "设备编号", Kind: "text", ManualOnly: true}
}

// ===== 静态校验 =====

func TestTemplateIDFormat(t *testing.T) {
	bad := []string{"", "Foo", "1abc", "有中文", "a-b", "a b"}
	for _, id := range bad {
		if err := validateReportTemplate(tplWith(id, assetNoOf())); !errors.Is(err, errTplIDFormat) {
			t.Errorf("标识 %q 应被拒,实际 %v", id, err)
		}
	}
	if err := validateReportTemplate(tplWith("hot_water_2", assetNoOf())); err != nil {
		t.Errorf("合法标识被拒了: %v", err)
	}
}

// 【重复的字段标识是静默数据丢失】两个字段共用一个键,后填的盖掉先填的,
// 而表单上明明是两栏 —— 人以为都填了。
func TestDuplicateFieldCodeRejected(t *testing.T) {
	tpl := tplWith("t_dup", assetNoOf(),
		fieldOf("temp", "温度", "number"),
		fieldOf("temp", "湿度", "number"))
	if err := validateReportTemplate(tpl); !errors.Is(err, errTplDupCode) {
		t.Errorf("重复的字段标识应被拒,实际 %v", err)
	}
}

// 【没有设备编号字段 = 记录挂不到任何设备】提交之后台账里既看不到这次巡检,
// 也不知道少了谁 —— 而错误发生在提交那一刻,要过很久对账时才发现。
func TestTemplateMustIdentifyTheAsset(t *testing.T) {
	tpl := tplWith("t_no_asset", fieldOf("temp", "温度", "number"))
	if err := validateReportTemplate(tpl); !errors.Is(err, errTplNoAssetNo) {
		t.Errorf("缺设备编号字段应被拒,实际 %v", err)
	}
	// 但那两个"按读数拆多台设备"的模板是代码特判的,不能拿这条卡它们
	zihan := tplWith("zihan_energy", fieldOf("z1_reading", "Z1 读数", "number"))
	if err := validateReportTemplate(zihan); err != nil {
		t.Errorf("按读数建资产的模板不该被这条拦下: %v", err)
	}
}

// 只有一个选项的单选等于没得选,而它会被当成"填了"。
func TestChoiceNeedsAtLeastTwoOptions(t *testing.T) {
	tpl := tplWith("t_choice", assetNoOf(), fieldOf("ok", "是否正常", "choice", "正常"))
	if err := validateReportTemplate(tpl); !errors.Is(err, errTplNoOptions) {
		t.Errorf("单选少于两个选项应被拒,实际 %v", err)
	}
	tpl.Fields[1] = fieldOf("ok", "是否正常", "choice", "正常", "异常")
	if err := validateReportTemplate(tpl); err != nil {
		t.Errorf("两个选项的单选被拒了: %v", err)
	}
}

func TestFieldKindRestricted(t *testing.T) {
	tpl := tplWith("t_kind", assetNoOf(), fieldOf("x", "某项", "date"))
	if err := validateReportTemplate(tpl); !errors.Is(err, errTplBadKind) {
		t.Errorf("未知字段类型应被拒,实际 %v", err)
	}
}

// ===== 有历史记录之后的改动限制 =====

// 【核心】有记录之后不许改字段标识、不许删字段。
//
// 记录里的字段值是按标识存的:改了或删了,历史记录里那一项就再也读不出来,
// 而且不报错 —— 表现是"以前填过的内容不见了"。
func TestCannotRenameOrDropFieldOnceUsed(t *testing.T) {
	old := tplWith("t_used", assetNoOf(), fieldOf("temp", "温度", "number"))

	renamed := tplWith("t_used", assetNoOf(), fieldOf("temperature", "温度", "number"))
	if err := validateTemplateChange(old, renamed, 5); !errors.Is(err, errTplCodeChanged) {
		t.Errorf("有记录时改字段标识应被拒,实际 %v", err)
	}

	dropped := tplWith("t_used", assetNoOf())
	if err := validateTemplateChange(old, dropped, 5); !errors.Is(err, errTplCodeChanged) {
		t.Errorf("有记录时删字段应被拒,实际 %v", err)
	}
}

// 但改中文标签、加新字段必须还能做 —— 否则这个编辑器对已经在用的模板
// (也就是全部十个)等于不可用。
func TestLabelChangeAndFieldAdditionStayAllowed(t *testing.T) {
	old := tplWith("t_used", assetNoOf(), fieldOf("temp", "温度", "number"))

	relabeled := tplWith("t_used", assetNoOf(), fieldOf("temp", "机房温度℃", "number"))
	if err := validateTemplateChange(old, relabeled, 5); err != nil {
		t.Errorf("改中文标签应当允许: %v", err)
	}

	added := tplWith("t_used", assetNoOf(),
		fieldOf("temp", "温度", "number"),
		fieldOf("humidity", "湿度", "number"))
	if err := validateTemplateChange(old, added, 5); err != nil {
		t.Errorf("加新字段应当允许: %v", err)
	}
}

// 还没人用过的模板可以随便改 —— 包括改标识、删字段。
func TestUnusedTemplateFullyEditable(t *testing.T) {
	old := tplWith("t_new", assetNoOf(), fieldOf("temp", "温度", "number"))
	reshaped := tplWith("t_new", assetNoOf(), fieldOf("brand_new", "全新字段", "text"))
	if err := validateTemplateChange(old, reshaped, 0); err != nil {
		t.Errorf("没有记录的模板应可随意改: %v", err)
	}
}

// ===== HTTP =====

func putTpl(t *testing.T, srv *Server, path, tok, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	srv.router(rec, req)
	return rec
}

// 巡检员改不了模板 —— 改一个字段就改变了所有人填什么。
func TestTemplateEditRequiresPermission(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	tok := tokens["inspector_a"]
	if tok == "" {
		t.Fatal("测试骨架里没有 inspector_a,这条断言就失去意义了")
	}
	got := putTpl(t, server, "/api/report/templates/hot_water_room", tok,
		`{"name":"改名","fields":[{"code":"asset_no","label":"设备编号","kind":"text"}]}`)
	if got.Code == http.StatusOK {
		t.Errorf("巡检员不该能改模板,实际 code=%d", got.Code)
	}
}

// 保存之后必须【立刻生效】。
//
// 【漏了刷新缓存的表现是"保存成功但没变"】人会反复点保存、反复确认自己
// 填对了,而问题在别处。模板规则那一套已经栽过这个坑。
func TestSaveTakesEffectImmediately(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}

	body := `{"name":"热水机房(改过)","project":"会议中心","maxImages":20,"minImages":5,
		"fields":[{"code":"asset_no","label":"设备编号","kind":"text"},
		          {"code":"cabinet_temperature","label":"控制柜温度℃","kind":"number"}]}`
	got := putTpl(t, server, "/api/report/templates/hot_water_room", tokens["admin"], body)
	if got.Code != http.StatusOK {
		t.Fatalf("保存失败 code=%d body=%s", got.Code, got.Body.String())
	}
	tpl, ok := templateByID("hot_water_room")
	if !ok {
		t.Fatal("保存后模板不见了")
	}
	if tpl.Name != "热水机房(改过)" {
		t.Errorf("保存成功却没生效,现在的名字还是 %q", tpl.Name)
	}
}

// 用过的模板不能删 —— 删了那些记录的字段定义就没了,
// 详情页会变成一堆没有名字的值。
func TestCannotDeleteTemplateInUse(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}

	if err := server.store.CreateRecord(&Record{
		ID: "r_uses_tpl", TenantID: defaultTenantID, TemplateID: "hot_water_room",
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodDelete, "/api/report/templates/hot_water_room", nil)
	req.Header.Set("X-InspectAI-Token", tokens["admin"])
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code != http.StatusConflict {
		t.Errorf("用过的模板应拒绝删除,实际 code=%d body=%s", rec.Code, rec.Body.String())
	}
}
