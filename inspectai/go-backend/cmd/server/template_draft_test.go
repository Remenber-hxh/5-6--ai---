package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 需求 → 字段表的【规范化】那一层。
//
// 【模型提的 code 会变成永久的键】字段 code 是记录里字段值的键,有记录之后
// 就再也改不了;读数趋势也是按 code 归集的。同一个"温度"这次叫 temp、
// 下次叫 temperature,趋势断成两截而且不报错。
// 所以模型只负责"提哪些检查项",合法性和唯一性必须由这一层兜住。

func rawField(code, label, kind, mode string) map[string]any {
	return map[string]any{"code": code, "label": label, "kind": kind, "judgeMode": mode}
}

func codesOf(fs []TemplateField) []string {
	out := make([]string, 0, len(fs))
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func hasCode(fs []TemplateField, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

// 非法或重复的 code 要被换掉,但那一条检查项不能丢。
//
// 【直接跳过是更糟的选择】人要的检查项凭空少了一条,而界面上不会说少了什么。
func TestIllegalCodesAreReplacedNotDropped(t *testing.T) {
	got := normalizeDraftFields([]map[string]any{
		rawField("门窗标识", "机房门窗标识", "choice", "visual"), // 中文 code
		rawField("room_clean", "机房卫生", "choice", "visual_lenient"),
		rawField("room_clean", "重复的", "choice", "visual"), // 重复
		rawField("", "没有 code 的", "choice", "visual"),
	})
	// 四条 + 骨架两条
	if len(got) != 6 {
		t.Fatalf("应有 4 条 + 骨架 2 条 = 6,实际 %d: %v", len(got), codesOf(got))
	}
	seen := map[string]bool{}
	for _, f := range got {
		if !tplIDPattern.MatchString(f.Code) {
			t.Errorf("非法 code 没被换掉: %q", f.Code)
		}
		if seen[f.Code] {
			t.Errorf("重复的 code 没去重: %q", f.Code)
		}
		seen[f.Code] = true
		if strings.TrimSpace(f.Label) == "" {
			t.Errorf("字段 %s 没有中文名 —— 表单上会是一行空白", f.Code)
		}
	}
}

// 【两条骨架字段不是可选项】
//   - 没有设备编号,提交的记录挂不到任何设备上
//   - 没有不符合项汇总,判"否"时没地方写清楚问题是什么
//
// 提示词里要求模型带上它们,但要求 ≠ 保证。漏了的话人要到第一次提交失败
// 才发现,而那时候整个模板已经配完了。
func TestSkeletonFieldsAlwaysPresent(t *testing.T) {
	got := normalizeDraftFields([]map[string]any{
		rawField("room_clean", "机房卫生", "choice", "visual"),
	})
	if !hasCode(got, "asset_no") {
		t.Error("模型没给设备编号时要自动补上 —— 否则记录挂不到设备")
	}
	if !hasCode(got, "nonconformity") {
		t.Error("模型没给不符合项汇总时要自动补上")
	}
	// 设备编号必须排在最前、必填、且不让 AI 代填
	if got[0].Code != "asset_no" {
		t.Errorf("设备编号应排在最前,实际第一条是 %s", got[0].Code)
	}
	for _, f := range got {
		if f.Code == "asset_no" {
			if !f.Required || !f.ManualOnly {
				t.Errorf("设备编号必须必填且人工填,实际 required=%v manualOnly=%v",
					f.Required, f.ManualOnly)
			}
		}
	}
	// 模型自己给了的话不重复补
	twice := normalizeDraftFields([]map[string]any{
		rawField("asset_no", "设备编号", "text", "read_text"),
		rawField("room_clean", "机房卫生", "choice", "visual"),
		rawField("nonconformity", "不符合项", "text", "summary"),
	})
	var n int
	for _, f := range twice {
		if f.Code == "asset_no" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("设备编号被补重了 %d 次", n)
	}
}

// 【自创的判定模式渲染不出话术】渲染器按模式展开成标准句子,认不出的模式
// 会渲染成一条空规则 —— 提示词里那一行只有字段名没有判断依据,
// 模型照跑,结果随机。
func TestUnknownJudgeModeFallsBack(t *testing.T) {
	got := normalizeDraftFields([]map[string]any{
		rawField("a", "甲", "choice", "我自创的模式"),
		rawField("b", "乙", "choice", ""),
		rawField("c", "丙", "choice", "sensory"),
	})
	for _, f := range got {
		switch f.Code {
		case "a", "b":
			if f.JudgeMode != ModeVisual {
				t.Errorf("未知模式应回落 visual,实际 %q", f.JudgeMode)
			}
		case "c":
			if f.JudgeMode != ModeSensory {
				t.Errorf("合法模式被改掉了: %q", f.JudgeMode)
			}
		}
	}
}

// 单选的选项统一成是/否 —— 全系统的判定语义就是这两个值,
// 模型自创选项会让识别结果对不上。
func TestChoiceOptionsAreNormalized(t *testing.T) {
	got := normalizeDraftFields([]map[string]any{
		rawField("a", "甲", "choice", "visual"),
	})
	for _, f := range got {
		if f.Kind != "choice" {
			continue
		}
		if len(f.Options) != 2 || f.Options[0] != "是" || f.Options[1] != "否" {
			t.Errorf("单选选项没统一成是/否,实际 %v", f.Options)
		}
	}
}

// 模型偶尔会失控吐几十条。全盘接受的话,现场要拍的照片和要点的选项
// 会多到没人拍得全,最后变成随便点 —— 比没有这些检查项更糟。
func TestTooManyFieldsAreCapped(t *testing.T) {
	raw := make([]map[string]any, 0, 100)
	for i := 0; i < 100; i++ {
		raw = append(raw, rawField("f"+strings.Repeat("x", i%5+1)+string(rune('a'+i%26)),
			"字段", "choice", "visual"))
	}
	got := normalizeDraftFields(raw)
	if len(got) > draftFieldLimit+2 { // +2 是骨架
		t.Errorf("没有截断,实际 %d 条", len(got))
	}
}

// 生成出来的字段表要能直接通过保存校验 —— 生成完却存不进去等于没用。
func TestDraftPassesTemplateValidation(t *testing.T) {
	fields := normalizeDraftFields([]map[string]any{
		rawField("door_sign", "机房门窗标识", "choice", "visual"),
		rawField("room_clean", "机房卫生", "choice", "visual_lenient"),
	})
	tpl := ReportTemplate{ID: "tpl_abcd", Name: "测试模板", Fields: fields}
	if err := validateReportTemplate(tpl); err != nil {
		t.Errorf("生成的字段表存不进去: %v", err)
	}
}

// 生成的新检查项要真的进到字段表里。
//
// 【只匹配已有字段的话会静默丢掉】界面显示"已保存",而字段表一条没多 ——
// 人会以为生成功能坏了。而"生成字段表"正是这个功能的全部意义。
func TestGeneratedFieldsGetAdded(t *testing.T) {
	isolateTemplateCache(t)
	store := NewMemStore()
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}
	srv := &Server{store: store}

	before, _ := templateByID("fire_pump")
	beforeCount := len(before.Fields)

	// 模拟"采用生成结果":一批全新的 code
	view := PromptTemplate{
		ID: "fire_pump", Mode: PromptModeStructured,
		Fields: []PromptField{
			{Code: "asset_no", Label: "设备编号", Mode: ModeReadText, YesWhen: "编号清晰"},
			{Code: "pump_pressure_ok", Label: "泵组压力正常", Mode: ModeVisual, YesWhen: "压力表在绿区"},
			{Code: "valve_open", Label: "阀门处于常开位", Mode: ModeVisual, YesWhen: "手轮在全开位置"},
			{Code: "nonconformity", Label: "不符合项处理情况记录", Mode: ModeSummary},
		},
	}
	if err := srv.applyPromptToTemplate(view); err != nil {
		t.Fatal(err)
	}

	after, _ := templateByID("fire_pump")
	if len(after.Fields) <= beforeCount {
		t.Fatalf("新检查项没进字段表:%d → %d", beforeCount, len(after.Fields))
	}
	byCode := map[string]TemplateField{}
	for _, f := range after.Fields {
		byCode[f.Code] = f
	}
	nf, ok := byCode["pump_pressure_ok"]
	if !ok {
		t.Fatal("新生成的字段 pump_pressure_ok 不见了")
	}
	// 新字段要能直接用:是/否类型 + 选项齐 + 判定规则带上
	if nf.Kind != "choice" || len(nf.Options) != 2 {
		t.Errorf("新字段的表单定义不完整:kind=%q options=%v —— 存不进去也填不了",
			nf.Kind, nf.Options)
	}
	if nf.YesWhen != "压力表在绿区" {
		t.Errorf("判定规则没带上:%q", nf.YesWhen)
	}

	// 【汇总和编号那两条不是是/否】给它们套上选项的话,表单上会变成
	// 让人从"是/否"里选一个设备编号。
	if byCode["nonconformity"].Kind != "text" || len(byCode["nonconformity"].Options) > 0 {
		t.Errorf("不符合项汇总不该是单选:%+v", byCode["nonconformity"])
	}
	if byCode["asset_no"].Kind != "text" || !byCode["asset_no"].Required {
		t.Errorf("设备编号应是必填文本:%+v", byCode["asset_no"])
	}

	// 整份要能通过保存校验 —— 生成完存不进去等于没用
	if err := validateReportTemplate(after); err != nil {
		t.Errorf("采用之后的模板存不进去: %v", err)
	}
}

// ===== Go → ai-service 这一跳 =====

// 用一个假 ai-service 走完整条链路:接口 → 客户端 → 规范化 → 返回。
//
// 【真模型那半截单独验过】这里要证明的是中间这一段没接错 ——
// 路由、权限、字段名映射,任何一处错了都会表现成"生成不出来",
// 而人分不清是模型的问题还是接线的问题。
func TestDraftEndpointEndToEnd(t *testing.T) {
	isolateTemplateCache(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prompt/draft-fields" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"model":"test-model","fields":[
			{"code":"asset_no","label":"设备编号","kind":"text","judgeMode":"read_text"},
			{"code":"pump_leak","label":"水泵无漏水","kind":"choice","judgeMode":"visual","yesWhen":"泵体干燥"},
			{"code":"中文码","label":"压力表读数正常","kind":"choice","judgeMode":"我自创的"},
			{"code":"nonconformity","label":"不符合项处理情况记录","kind":"text","judgeMode":"summary"}
		]}`))
	}))
	defer fake.Close()

	server, tokens := newRecordAccessTestServer(t)
	server.aiClient = NewAIClient(fake.URL)

	body := `{"requirement":"看水泵漏不漏水、压力表正不正常","templateName":"生活水泵房"}`
	req := httptest.NewRequest(http.MethodPost, "/api/report/templates/draft", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tokens["admin"])
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("生成失败 code=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Fields []TemplateField `json:"fields"`
		Model  string          `json:"model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Model != "test-model" {
		t.Errorf("模型名没传回来: %q", out.Model)
	}
	if len(out.Fields) != 4 {
		t.Fatalf("应有 4 条,实际 %d: %v", len(out.Fields), codesOf(out.Fields))
	}
	for _, f := range out.Fields {
		if !tplIDPattern.MatchString(f.Code) {
			t.Errorf("非法 code 漏到了前端: %q —— 它会变成永久的键", f.Code)
		}
		if f.JudgeMode != "" && normalizeJudgeMode(f.JudgeMode) != f.JudgeMode {
			t.Errorf("未知判定模式漏了过去: %q", f.JudgeMode)
		}
	}
}

// 需求为空要当场拒,不该白跑一次大模型。
func TestDraftRejectsEmptyRequirement(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/report/templates/draft",
		strings.NewReader(`{"requirement":"   "}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tokens["admin"])
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("空需求应被拒绝")
	}
}

// 模型什么也没提出来时要说实话。
//
// 【返回只有骨架的两条 = 假装成功】人会以为生成好了、直接采用,
// 结果得到一个没有任何检查项的模板。
func TestDraftSaysSoWhenNothingUseful(t *testing.T) {
	isolateTemplateCache(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"fields":[]}`))
	}))
	defer fake.Close()
	server, tokens := newRecordAccessTestServer(t)
	server.aiClient = NewAIClient(fake.URL)

	req := httptest.NewRequest(http.MethodPost, "/api/report/templates/draft",
		strings.NewReader(`{"requirement":"随便看看"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tokens["admin"])
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("一条检查项都没生成出来时应报错,而不是返回只有骨架的两条")
	}
}

// 巡检员不能改模板,自然也不能用这个生成。
func TestDraftRequiresTemplatePermission(t *testing.T) {
	isolateTemplateCache(t)
	server, tokens := newRecordAccessTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/report/templates/draft",
		strings.NewReader(`{"requirement":"随便"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-InspectAI-Token", tokens["inspector_a"])
	rec := httptest.NewRecorder()
	server.router(rec, req)
	if rec.Code == http.StatusOK {
		t.Error("巡检员不该能生成模板字段")
	}
}
