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

// ===== 职责边界:一份数据只有一个写入口 =====

// 【模板页改不动必填和照片张数】它们归「提交规则」页。
//
// 光在界面上把输入框藏起来不够:同一份数据两个写入口,迟早有一个把另一个
// 冲掉,而且不报错 —— 表现是"我在那边改好的,过一会儿又变回去了",
// 查起来极难。所以这条规则落在接口上,界面只是跟着它走。
func TestTemplatePageCannotChangeSubmissionRules(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	before, _ := templateByID("hot_water_room")
	if before.MinImages == 0 {
		t.Fatal("前置条件不成立:这个模板本该有最少照片数")
	}
	var requiredBefore bool
	for _, f := range before.Fields {
		if f.Code == "cabinet_temperature" {
			requiredBefore = f.Required
		}
	}

	// 请求里把张数和必填都改掉 —— 应该被忽略
	body := `{"name":"热水机房巡检","project":"会议中心","assetType":"热水机房",
		"minImages":1,"maxImages":2,
		"fields":[{"code":"asset_no","label":"设备编号","kind":"text","required":false},
		          {"code":"cabinet_temperature","label":"控制柜温度℃","kind":"number","required":` +
		map[bool]string{true: "false", false: "true"}[requiredBefore] + `}]}`
	got := putTpl(t, server, "/api/report/templates/hot_water_room", tokens["admin"], body)
	if got.Code != http.StatusOK {
		t.Fatalf("保存失败 code=%d body=%s", got.Code, got.Body.String())
	}

	after, _ := templateByID("hot_water_room")
	if after.MinImages != before.MinImages || after.MaxImages != before.MaxImages {
		t.Errorf("照片张数被模板页改掉了:%d/%d → %d/%d",
			before.MinImages, before.MaxImages, after.MinImages, after.MaxImages)
	}
	for _, f := range after.Fields {
		if f.Code == "cabinet_temperature" && f.Required != requiredBefore {
			t.Errorf("必填被模板页改掉了:%v → %v", requiredBefore, f.Required)
		}
	}
	// 但字段名这类归模板页管的,必须改得动 —— 否则这一页就没用了
	var renamed bool
	for _, f := range after.Fields {
		if f.Code == "cabinet_temperature" && f.Label == "控制柜温度℃" {
			renamed = true
		}
	}
	if !renamed {
		t.Error("字段中文名归模板页管,应该改得动")
	}
}

// 【提示词页改不动模板名和字段中文名】它们归模板页。
func TestPromptPageCannotRenameTemplateOrFields(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	before, _ := templateByID("elevator_machine_room")
	nameBefore := before.Name
	var labelBefore string
	for _, f := range before.Fields {
		if f.Code == "room_clean" {
			labelBefore = f.Label
		}
	}
	if labelBefore == "" {
		t.Fatal("前置条件不成立:找不到 room_clean")
	}

	body := `{"name":"提示词页改的名字","mode":"structured","scene":"新场景",
		"fields":[{"code":"room_clean","label":"提示词页改的字段名","mode":"visual","yesWhen":"看着干净"}]}`
	got := putJSON(t, server, "/api/prompt/templates/elevator_machine_room", tokens["admin"], body)
	if got.Code != http.StatusOK {
		t.Fatalf("保存失败 code=%d body=%s", got.Code, got.Body.String())
	}

	after, _ := templateByID("elevator_machine_room")
	if after.Name != nameBefore {
		t.Errorf("模板名被提示词页改掉了:%q → %q", nameBefore, after.Name)
	}
	for _, f := range after.Fields {
		if f.Code == "room_clean" {
			if f.Label != labelBefore {
				t.Errorf("字段中文名被提示词页改掉了:%q → %q", labelBefore, f.Label)
			}
			// 但判定规则归它管,必须写得进去
			if f.YesWhen != "看着干净" {
				t.Errorf("判定规则没写进去:%q", f.YesWhen)
			}
		}
	}
	// 场景归提示词页管
	if after.Scene != "新场景" {
		t.Errorf("场景描述归提示词页管,应该写得进去,实际 %q", after.Scene)
	}
}

// 新建的模板必须【三页都能看到】。
//
// 这是"一份数据三个视角"的直接检验:巡检模板页、提交规则页、提示词页
// 读的都是同一个 reportTemplates()。任何一处看不到,就说明还有第二份数据。
func TestNewTemplateAppearsEverywhere(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	if err := loadReportTemplates(server.store); err != nil {
		t.Fatal(err)
	}
	tok := tokens["admin"]

	body := `{"id":"zihan_meter_new","name":"紫菡抄表(新建)","project":"紫菡雅集",
		"assetType":"抄表点","maxImages":20,"minImages":5,
		"fields":[{"code":"asset_no","label":"设备编号","kind":"text","required":true,"source":"manual"},
		          {"code":"reading","label":"读数","kind":"number","source":"ai"}]}`
	req := httptest.NewRequest(http.MethodPost, "/api/report/templates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tok)
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("新建失败 code=%d body=%s", rec.Code, rec.Body.String())
	}

	// ① 巡检模板页 / 提交规则页 —— 都读 /api/report/templates
	got := requestWithToken(server, http.MethodGet, "/api/report/templates", tok)
	if !strings.Contains(got.Body.String(), "zihan_meter_new") {
		t.Error("新模板没出现在模板列表里 —— 模板页和提交规则页都看不到它")
	}

	// ② 提示词页 —— 读 /api/prompt/templates
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates", tok)
	if !strings.Contains(got.Body.String(), "zihan_meter_new") {
		t.Error("新模板没出现在提示词列表里 —— 没法给它配 AI 判定规则")
	}

	// ③ 能打开它的提示词编辑器(新模板还没有判定规则,应给空草稿而不是 404)
	got = requestWithToken(server, http.MethodGet, "/api/prompt/templates/zihan_meter_new", tok)
	if got.Code != http.StatusOK {
		t.Errorf("新模板的提示词编辑器打不开 code=%d", got.Code)
	}

	// ④ 提交规则页能给它配必填(读得到 ≠ 配得了)
	w := saveRules(t, server, req, "zihan_meter_new", `{"required":{"reading":true}}`)
	if w.Code != http.StatusOK {
		t.Errorf("给新模板配必填失败 code=%d body=%s", w.Code, w.Body.String())
	}
	if f, _ := findField("zihan_meter_new", "reading"); !f.Required {
		t.Error("必填没配上 —— 提交规则页对新模板不生效")
	}
}
