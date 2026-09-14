package main

import "testing"

// ===== 一个写入口:一次存盘写完整份 =====
//
// 【背景】同一张字段表原来有三个写入口,各写一部分、互相错开:
//   PUT /report/templates/{id}         字段定义(必填和张数被 keepSubmissionRules 剥掉)
//   PUT /report/templates/{id}/fields  只写必填 + 最少张数
//   PUT /prompt/templates/{id}         只写判定规则
//
// 是接口的形状逼出了界面的形状 —— 后台才会把「机房卫生」这一项拆到三个
// 页签去配。要把编辑器合成一处,先得让一个接口能写完整份。
//
// 【为什么不能直接把剥离删掉】老编辑器不传必填和张数。一删,Go 的零值
// (false / 0)就会被当成"人设成了不必填、张数为 0"悄悄写进去,而且返回
// 200 —— 要到下次现场提交被放行才发现。所以判据是【键在不在】。

func bodyProbeTemplate() ReportTemplate {
	return ReportTemplate{
		ID: "tpl_single_writer", Name: "单写入口测试", Project: "会议中心",
		AssetType: "单写入口测试点", MinImages: 3, MaxImages: 9,
		Fields: []TemplateField{
			{Code: "asset_no", Label: "设备编号", Kind: "text", Source: "manual", ManualOnly: true},
			{
				Code: "room_clean", Label: "机房卫生", Kind: "choice",
				Options: []string{"正常", "异常"}, Required: true, Source: "ai",
				JudgeMode: ModeVisualLenient, JudgeGroup: "机房",
				YesWhen: "地面基本整洁", NoWhen: "明显堆放杂物", SkipWhen: "未拍到地面",
				JudgeNote: "少量灰尘不算",
			},
		},
	}
}

// 老编辑器那种请求(不提必填、不提张数)→ 沿用旧值,一个字都不许动。
func TestSaveWithoutSubmissionRulesKeepsOldOnes(t *testing.T) {
	raw := []byte(`{"name":"改了个名","fields":[{"code":"room_clean","label":"机房卫生"}]}`)
	if bodyHasSubmissionRules(raw) {
		t.Fatal("这个请求没提必填也没提张数,不该被当成权威写入")
	}

	old := bodyProbeTemplate()
	next := bodyProbeTemplate()
	next.MinImages, next.MaxImages = 0, 0
	next.Fields[1].Required = false

	got := keepSubmissionRules(old, next)
	if !got.Fields[1].Required {
		t.Error("必填被零值冲掉了 —— 现场会突然可以不填就提交")
	}
	if got.MinImages != 3 || got.MaxImages != 9 {
		t.Errorf("张数被零值冲掉了:min=%d max=%d", got.MinImages, got.MaxImages)
	}
	// 判定规则不在剥离范围内,要原样带过去
	if got.Fields[1].JudgeMode != ModeVisualLenient || got.Fields[1].YesWhen != "地面基本整洁" {
		t.Errorf("判定规则丢了:mode=%q yes=%q", got.Fields[1].JudgeMode, got.Fields[1].YesWhen)
	}
}

// 新编辑器那种请求(明确带了必填 / 张数)→ 以请求为准。
func TestSaveWithSubmissionRulesIsAuthoritative(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{"带了 minImages", `{"minImages":5,"fields":[{"code":"a"}]}`},
		{"带了 maxImages", `{"maxImages":9,"fields":[{"code":"a"}]}`},
		{"字段里带了 required", `{"fields":[{"code":"a","required":true}]}`},
		{"required 明确传 false", `{"fields":[{"code":"a","required":false}]}`},
		{"张数明确传 0", `{"minImages":0,"fields":[{"code":"a"}]}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if !bodyHasSubmissionRules([]byte(c.raw)) {
				t.Errorf("这个请求明确提了提交约束,应该按权威写入:%s", c.raw)
			}
		})
	}
}

// 【最容易错的两条】明确传 false / 0,和根本没传,必须分得开 ——
// 这正是"人要把某一项改成不必填"和"老编辑器路过"的区别。
func TestSubmissionRulesDistinguishesAbsentFromZero(t *testing.T) {
	if bodyHasSubmissionRules([]byte(`{"fields":[{"code":"a","label":"x"}]}`)) {
		t.Error("字段里没有 required 键,不该算提了")
	}
	if !bodyHasSubmissionRules([]byte(`{"fields":[{"code":"a","required":false}]}`)) {
		t.Error("明确传了 required:false,必须算提了 —— 否则人永远改不成「不必填」")
	}
	if bodyHasSubmissionRules([]byte(`not json`)) {
		t.Error("解不开的 body 不该被当成权威写入")
	}
	if bodyHasSubmissionRules([]byte(`{}`)) {
		t.Error("空对象不该被当成权威写入")
	}
}

// 存储层本来就能存全 —— 拆分只在 HTTP 那一层,这条守住别把存储也改坏。
func TestStoreRoundTripsEverything(t *testing.T) {
	store := NewMemStore()
	tpl := bodyProbeTemplate()
	if err := store.UpsertReportTemplate(tpl); err != nil {
		t.Fatalf("存盘失败: %v", err)
	}
	back, err := store.ListReportTemplates()
	if err != nil {
		t.Fatal(err)
	}
	var saved *ReportTemplate
	for i := range back {
		if back[i].ID == tpl.ID {
			saved = &back[i]
		}
	}
	if saved == nil {
		t.Fatal("存进去又读不出来")
	}
	f := saved.Fields[1]
	if f.Label != "机房卫生" || f.Kind != "choice" || len(f.Options) != 2 {
		t.Errorf("表单定义丢了:label=%q kind=%q options=%v", f.Label, f.Kind, f.Options)
	}
	if f.JudgeMode != ModeVisualLenient || f.YesWhen != "地面基本整洁" {
		t.Errorf("判定规则丢了:mode=%q yes=%q", f.JudgeMode, f.YesWhen)
	}
	if !f.Required {
		t.Error("必填丢了")
	}
	if saved.MinImages != 3 || saved.MaxImages != 9 {
		t.Errorf("张数丢了:min=%d max=%d", saved.MinImages, saved.MaxImages)
	}
}
