package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// 模板字段的必填/选填配置。
//
// 这层覆盖最怕两件事:改了不生效(用户以为改了,线上照旧),
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

// 【asset_no 必须锁死】它是台账认归属的字段。放开成选填之后,提交时不填,
// 这条记录就挂不到任何设备上 —— 台账里既看不到这次巡检,也不知道少了谁。
func TestAssetNoCannotBeMadeOptional(t *testing.T) {
	setTemplateRules(nil)
	t.Cleanup(func() { setTemplateRules(nil) })

	// 就算配置里硬塞一条,也不能生效
	setTemplateRules(map[string]map[string]bool{
		"escalator": {"asset_no": false},
	})
	f, ok := findField("escalator", "asset_no")
	if !ok {
		t.Fatal("扶梯模板里应该有 asset_no")
	}
	if !f.Required {
		t.Fatal("asset_no 被改成选填了 —— 记录会挂不到设备上,而且很久之后才发现")
	}

	// 接口层也要拒绝,并且说清为什么。
	// 【必须用真管理员发】用裸请求会被 403 挡在门外,那样这条用例就变成
	// 在验鉴权,而不是在验锁定字段 —— 通过了也证明不了任何事。
	srv, auth := newScopeRequest(t, roleAdmin, "")
	body := `{"required":{"asset_no":false}}`
	r := httptest.NewRequest(http.MethodPut, "/api/report/templates/escalator/fields", strings.NewReader(body))
	r.Header = auth.Header
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSaveTemplateFields(w, r, "escalator")
	if w.Code == http.StatusOK {
		t.Fatal("接口允许把 asset_no 改成选填了")
	}
	if !strings.Contains(w.Body.String(), "归属") {
		t.Errorf("拒绝时要说清为什么,得到:%s", w.Body.String())
	}
}

// 覆盖要真的生效:改了之后 templateByID 拿到的就是新值。
func TestTemplateRuleOverridesRequired(t *testing.T) {
	setTemplateRules(nil)
	t.Cleanup(func() { setTemplateRules(nil) })

	before, ok := findField("zihan_energy", "z1_reading")
	if !ok {
		t.Fatal("能耗抄表里应该有 z1_reading")
	}
	if before.Required {
		t.Fatal("这条用例假设 z1_reading 默认是选填,模板改了就要跟着改用例")
	}

	setTemplateRules(map[string]map[string]bool{
		"zihan_energy": {"z1_reading": true},
	})
	after, _ := findField("zihan_energy", "z1_reading")
	if !after.Required {
		t.Fatal("配置了必填却没生效 —— 用户会以为改了,而线上照旧")
	}

	// 【别误伤同名字段的其他模板】各模板的字段编码会重复(比如 site、note)
	other, ok := findField("zihan_daily", "site")
	if ok && other.Required != true {
		// zihan_daily 的 site 本来就是必填,这里只是确认没被上面那条配置改动
		t.Errorf("别的模板的字段被改到了")
	}

	// 清掉配置就回到代码默认值 —— "改回默认"必须真的能改回去
	setTemplateRules(nil)
	back, _ := findField("zihan_energy", "z1_reading")
	if back.Required {
		t.Fatal("清掉配置后没回到默认值 —— 改错了就回不去")
	}
}

// 保存之后必须【立刻】生效,不能等重启。
func TestSaveTemplateFieldsTakesEffectImmediately(t *testing.T) {
	setTemplateRules(nil)
	t.Cleanup(func() { setTemplateRules(nil) })
	srv, auth := newScopeRequest(t, roleAdmin, "")

	body := `{"required":{"z1_reading":true,"note":true}}`
	r := httptest.NewRequest(http.MethodPut, "/api/report/templates/zihan_energy/fields", strings.NewReader(body))
	r.Header = auth.Header
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSaveTemplateFields(w, r, "zihan_energy")
	if w.Code != http.StatusOK {
		t.Fatalf("保存失败:%d %s", w.Code, w.Body.String())
	}
	f, _ := findField("zihan_energy", "z1_reading")
	if !f.Required {
		t.Fatal("保存成功但没生效 —— 表现是「我改了没用」,要重启才行")
	}

	// 再保存一次空的:等于全部改回默认
	r2 := httptest.NewRequest(http.MethodPut, "/api/report/templates/zihan_energy/fields",
		strings.NewReader(`{"required":{}}`))
	r2.Header = auth.Header
	r2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.handleSaveTemplateFields(w2, r2, "zihan_energy")
	if w2.Code != http.StatusOK {
		t.Fatalf("清空失败:%d %s", w2.Code, w2.Body.String())
	}
	if back, _ := findField("zihan_energy", "z1_reading"); back.Required {
		t.Fatal("清空之后仍是必填 —— 留下了幽灵配置")
	}
}

// 模板里没有的字段要当场拒绝。存下去也永远不生效,而后台会显示"已保存"。
func TestSaveTemplateFieldsRejectsUnknownField(t *testing.T) {
	setTemplateRules(nil)
	t.Cleanup(func() { setTemplateRules(nil) })
	srv, auth := newScopeRequest(t, roleAdmin, "")
	r := httptest.NewRequest(http.MethodPut, "/api/report/templates/zihan_energy/fields",
		strings.NewReader(`{"required":{"根本没有这个字段":true}}`))
	r.Header = auth.Header
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSaveTemplateFields(w, r, "zihan_energy")
	if w.Code == http.StatusOK {
		t.Fatal("接受了模板里不存在的字段 —— 会存下一份永远不生效的配置")
	}
}
