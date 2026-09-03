package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 「提交规则」页:每个字段必填还是选填 + 每单最少几张照片。
//
// 【覆盖层已撤(迁移 027)】这些配置以前存在 template_field_rules /
// template_settings 里、读的时候盖到模板上;现在直接写模板底表。
// 所以这里全部走真实接口验证,不再有"往缓存里塞一份配置"那种测法 ——
// 那种测法证明不了保存这条路是通的。
//
// 这一屏最怕两件事:改了不生效(用户以为改了、线上照旧),
// 以及把不该放开的字段放开了(记录挂不上设备,很久之后对账才发现)。

func findField(tplID, code string) (TemplateField, bool) {
	tpl, ok := templateByID(tplID)
	if !ok {
		return TemplateField{}, false
	}
	for _, f := range tpl.Fields {
		if f.Code == code {
			return f, true
		}
	}
	return TemplateField{}, false
}

// saveRules 走真实接口保存一次提交规则。
func saveRules(t *testing.T, srv *Server, auth *http.Request, tplID, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPut,
		"/api/report/templates/"+tplID+"/fields", strings.NewReader(body))
	r.Header = auth.Header
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSaveTemplateFields(w, r, tplID)
	return w
}

// 准备一个模板已经入库的服务端 —— 保存要写库,库里没有就无从改起。
func rulesTestServer(t *testing.T) (*Server, *http.Request) {
	t.Helper()
	isolateTemplateCache(t)
	srv, auth := newScopeRequest(t, roleAdmin, "")
	if err := loadReportTemplates(srv.store); err != nil {
		t.Fatalf("模板入库失败: %v", err)
	}
	return srv, auth
}

// 【asset_no 必须锁死】它是台账认归属的字段。放开成选填之后,提交时不填,
// 这条记录就挂不到任何设备上 —— 台账里既看不到这次巡检,也不知道少了谁。
func TestAssetNoCannotBeMadeOptional(t *testing.T) {
	srv, auth := rulesTestServer(t)

	// 接口层面要当场拒绝
	w := saveRules(t, srv, auth, "escalator", `{"required":{"asset_no":false}}`)
	if w.Code == http.StatusOK {
		t.Error("接口接受了把 asset_no 改成选填 —— 必须当场拒绝")
	}
	// 而且不管怎么绕,模板里它永远是必填
	f, ok := findField("escalator", "asset_no")
	if !ok {
		t.Fatal("扶梯模板里应该有 asset_no")
	}
	if !f.Required {
		t.Fatal("asset_no 变成选填了 —— 记录会挂不到设备上,而且很久之后才发现")
	}
}

// 保存之后必须【立刻生效】。
//
// 【漏了刷缓存的表现是"我改了没用"】人会反复点保存、反复确认自己填对了,
// 而问题在别处,要重启才行。
func TestSaveTemplateFieldsTakesEffectImmediately(t *testing.T) {
	srv, auth := rulesTestServer(t)

	before, ok := findField("zihan_energy", "z1_reading")
	if !ok {
		t.Fatal("能耗抄表里应该有 z1_reading")
	}
	if before.Required {
		t.Fatal("前置条件不成立:这个字段本来就是必填,断言看不出变化")
	}

	if w := saveRules(t, srv, auth, "zihan_energy", `{"required":{"z1_reading":true}}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	if f, _ := findField("zihan_energy", "z1_reading"); !f.Required {
		t.Fatal("保存成功但没生效 —— 表现是「我改了没用」")
	}

	// 改回去也要立刻生效(单向生效等于半个 bug)
	if w := saveRules(t, srv, auth, "zihan_energy", `{"required":{"z1_reading":false}}`); w.Code != http.StatusOK {
		t.Fatalf("改回失败:%d %s", w.Code, w.Body.String())
	}
	if f, _ := findField("zihan_energy", "z1_reading"); f.Required {
		t.Fatal("改回选填没生效")
	}
}

// 请求里没提到的字段保持原样。
//
// 【语义随覆盖层一起变了】以前空请求 = 全部回退到代码默认值(那是覆盖层的
// 语义:清掉覆盖就露出底下的默认)。现在底表就是唯一的值,没有"底下那层"
// 可以露出来 —— 空请求只能是"什么都不改"。
// 要是仍按老语义清空,一次只想调一个字段的保存会把整份配置抹掉。
func TestUnmentionedFieldsAreLeftAlone(t *testing.T) {
	srv, auth := rulesTestServer(t)

	// 先把两个字段设成必填
	if w := saveRules(t, srv, auth, "zihan_energy",
		`{"required":{"z1_reading":true,"note":true}}`); w.Code != http.StatusOK {
		t.Fatalf("准备失败:%d %s", w.Code, w.Body.String())
	}
	// 再只改其中一个
	if w := saveRules(t, srv, auth, "zihan_energy",
		`{"required":{"z1_reading":false}}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	if f, _ := findField("zihan_energy", "note"); !f.Required {
		t.Error("没提到的字段被改掉了 —— 一次只想调一个字段会把别的配置抹掉")
	}
	if f, _ := findField("zihan_energy", "z1_reading"); f.Required {
		t.Error("提到的字段没改成")
	}
}

// 模板里没有的字段要当场拒绝。存下去也永远不生效,而后台会显示"已保存"。
func TestSaveTemplateFieldsRejectsUnknownField(t *testing.T) {
	srv, auth := rulesTestServer(t)
	w := saveRules(t, srv, auth, "zihan_energy", `{"required":{"根本没有这个字段":true}}`)
	if w.Code == http.StatusOK {
		t.Fatal("接受了模板里不存在的字段 —— 会存下一份永远不生效的配置")
	}
}

// 每单最少几张照片:改完立刻生效。
//
// 【这条规则会影响线上正在填的草稿】上线那一刻,已经存在但不足张数的草稿
// 会突然提交不了。所以移动端必须在【拍照那一步】就说清还差几张,
// 而不是让人填完一整张表、点提交才被打回来。
func TestMinImagesTakesEffect(t *testing.T) {
	srv, auth := rulesTestServer(t)

	tpl, ok := templateByID("zihan_energy")
	if !ok {
		t.Fatal("能耗抄表模板不见了")
	}
	if tpl.MinImages != 5 {
		t.Fatalf("默认应为 5 张,得到 %d —— 周计划的要求是每单不少于五张", tpl.MinImages)
	}

	if w := saveRules(t, srv, auth, "zihan_energy",
		`{"required":{},"minImages":2}`); w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	if got, _ := templateByID("zihan_energy"); got.MinImages != 2 {
		t.Fatalf("配置没生效,仍是 %d", got.MinImages)
	}
}

// 最少张数不能超过单次上传上限 —— 超过就永远提交不了,
// 而巡检员在现场只看到「还差 N 张」,拍到死也够不着。
func TestMinImagesCannotExceedMax(t *testing.T) {
	srv, auth := rulesTestServer(t)

	tpl, _ := templateByID("zihan_energy")
	tooMany := tpl.MaxImages + 1
	w := saveRules(t, srv, auth, "zihan_energy",
		`{"required":{},"minImages":`+itoaSafe(tooMany)+`}`)
	if w.Code == http.StatusOK {
		t.Fatalf("允许把最少张数设成 %d,而上限只有 %d —— 这个模板会永远提交不了",
			tooMany, tpl.MaxImages)
	}
}
