package main

import (
	"strings"
	"testing"
)

// 提示词让模型填的词,必须能原样落进表单选项。
//
// 【这是两端对不上的那道缝】渲染器以前写死"→ 是 / → 否",而表单上有 10 个
// 字段的选项是 正常/异常、良好/异常。模型照提示词答"是",到 applyAIFields
// 那步 optionContains 匹配不上,值被清空、标成"需人工复核" ——
// AI 每次都跑、每次都白跑,而两端各自都不报错,界面上只看得到"这项没填"。
//
// 所以这条测试特意走【真实的落库函数】normalizeChoiceValue + optionContains,
// 不是自己再判一遍:换了归一化规则也得继续对得上,才算没退化。
func TestPromptChoiceWordsSurviveIngest(t *testing.T) {
	checked := 0
	for _, tpl := range reportTemplates() {
		view := promptViewOfTemplate(tpl) // 渲染视角:只含配了判定规则的字段
		byCode := map[string]TemplateField{}
		for _, f := range tpl.Fields {
			byCode[f.Code] = f
		}
		for _, pf := range view.Fields {
			f := byCode[pf.Code]
			if f.Kind != "choice" || len(f.Options) == 0 {
				continue
			}
			pass, fail := choiceOptionPair(pf)
			for _, word := range []string{pass, fail} {
				got := normalizeChoiceValue(word, f.Options)
				if !optionContains(f.Options, got) {
					t.Errorf("%s.%s:提示词让模型填 %q,但落库时归一化成 %q,不在选项 %v 里 —— AI 填了会被清空",
						tpl.ID, f.Code, word, got, f.Options)
				}
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("一个已配判定规则的 choice 字段都没扫到,测试等于没跑")
	}
	t.Logf("已核对 %d 个 choice 字段", checked)
}

// 渲染出来的判断依据里,不能出现这个字段选项之外的判定词。
//
// 【为什么不能只测上面那条】上面测的是"该填的词对不对",这条测的是
// "有没有别的词混进去" —— 比如汇总项那句"任何字段判为「否」",
// 在一个选项是 正常/异常 的模板里就是句假话。
func TestRenderedPromptHasNoForeignChoiceWords(t *testing.T) {
	for _, tpl := range reportTemplates() {
		view := promptViewOfTemplate(tpl)
		v := vocabularyOf(view.Fields)
		byCode := map[string]TemplateField{}
		for _, f := range tpl.Fields {
			byCode[f.Code] = f
		}
		for _, pf := range view.Fields {
			f := byCode[pf.Code]
			if f.Kind != "choice" || len(f.Options) == 0 {
				continue
			}
			criteria := renderFieldCriteria(pf, v)
			pass, fail := choiceOptionPair(pf)
			// 判定词只会以这两种形式出现:表格里的 "→ X",和靠听闻那类的 才返回"X"
			asVerdict := func(w string) bool {
				return strings.Contains(criteria, "→ "+w) || strings.Contains(criteria, `"`+w+`"`)
			}
			if pass != "是" && asVerdict("是") {
				t.Errorf("%s.%s 选项是 %v,判断依据里却让模型填「是」:%s", tpl.ID, f.Code, f.Options, criteria)
			}
			if fail != "否" && asVerdict("否") {
				t.Errorf("%s.%s 选项是 %v,判断依据里却让模型填「否」:%s", tpl.ID, f.Code, f.Options, criteria)
			}
			// 【只要求出现一个,不是两个】只写了 NoWhen 的字段(照片判不出"正常"、
			// 只判得出"异常")渲染出来本来就只有一个判定词,那是对的。
			// 但一个都没有就等于没告诉模型该填什么。
			if !asVerdict(pass) && !asVerdict(fail) {
				t.Errorf("%s.%s 的判断依据里一个判定词都没有(选项 %v):%s", tpl.ID, f.Code, f.Options, criteria)
			}
		}
	}
}

// 选项不能按下标取:库里既有 ["是","否"] 也有 ["否","是"],
// 按位置取会把整个模板的判定反过来 —— 而提示词读起来照样通顺。
func TestChoiceOptionPairIgnoresOrder(t *testing.T) {
	cases := []struct {
		opts             []string
		wantPass, wantNo string
	}{
		{[]string{"是", "否"}, "是", "否"},
		{[]string{"否", "是"}, "是", "否"},
		{[]string{"正常", "异常"}, "正常", "异常"},
		{[]string{"异常", "正常"}, "正常", "异常"},
		{[]string{"良好", "异常"}, "良好", "异常"},
		{nil, "是", "否"},                        // 不是 choice → 历史默认值
		{[]string{"甲", "乙"}, "是", "否"},         // 认不出词义 → 宁可不改,也不拿下标硬猜
		{[]string{"完好", "破损", "缺失"}, "完好", "破损"}, // 三选项:取第一个认得出的正/反词
	}
	for _, c := range cases {
		pass, fail := choiceOptionPair(PromptField{Options: c.opts})
		if pass != c.wantPass || fail != c.wantNo {
			t.Errorf("选项 %v:期望 %s/%s,得到 %s/%s", c.opts, c.wantPass, c.wantNo, pass, fail)
		}
	}
}

// 全模板只有一种选项组合时,总则要把那两个词直接写死;并存时不能挑一个当代表。
func TestVocabularyWording(t *testing.T) {
	uniform := vocabularyOf([]PromptField{{Options: []string{"正常", "异常"}}, {Options: []string{"异常", "正常"}}})
	if !uniform.uniform() || uniform.failWord() != "异常" {
		t.Errorf("同一种组合应该算 uniform,不合格词应为「异常」,得到 %v / %s", uniform.pairs, uniform.failWord())
	}
	if c := promptCommons(uniform); !strings.Contains(c.YesNoSemantics, `["正常","异常"]`) {
		t.Errorf("总则没写出真实选项:%s", c.YesNoSemantics)
	}

	mixed := vocabularyOf([]PromptField{{Options: []string{"是", "否"}}, {Options: []string{"正常", "异常"}}})
	if mixed.uniform() || mixed.failWord() != "不通过" {
		t.Errorf("两种组合并存时不能挑一个当代表,得到 %v / %s", mixed.pairs, mixed.failWord())
	}

	// 一个 choice 字段都没有的抄表模板:再讲 是/否 只会诱导模型乱填
	none := vocabularyOf([]PromptField{{Code: "z1_reading", Mode: ModeReadText}})
	c := promptCommons(none)
	if c.YesNoSemantics != "" {
		t.Errorf("没有 choice 字段时不该讲选项语义:%s", c.YesNoSemantics)
	}
	if strings.Contains(c.OutputSchema, "choice 值") {
		t.Errorf("没有 choice 字段时输出说明不该提 choice:%s", c.OutputSchema)
	}
}

// 纯 是/否 的模板渲染结果必须和改渲染器之前一模一样 —— 电梯那两份和扶梯
// 是拿真实照片 A/B 测过的,这次改动不该碰它们一个字。
func TestYesNoTemplatesKeepLegacyWording(t *testing.T) {
	tpl, ok := templateByID("elevator_machine_room")
	if !ok {
		t.Fatal("找不到 elevator_machine_room")
	}
	text := renderTemplatePrompt(tpl)
	for _, want := range []string{
		"- 选项字段全部是 `[\"是\",\"否\"]`:**是 = 符合要求 / 完好 / 正常**;**否 = 不符合 / 缺失 / 破损 / 异常 / 过期**。",
		"**画面里出现的项一律主动给出\"是/否\",不要为\"求稳\"留空**;明显正常/完好就大胆判\"是\"。",
		"只有照片能明确支持时才给值;真的看不清、没拍到、角度不够 → **不返回该字段**,留人工复核,不要用\"否\"代替\"没拍到\"。",
		"**凡有任何字段判为「否」,必须同时在 `nonconformity` 里写明问题**",
		"choice 值只能是 `\"是\"`/`\"否\"`",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("是/否 模板的措辞变了,少了这句:\n%s", want)
		}
	}
}

// 紫菡「综合巡检」的 4 个必填分区检查项,必须进得了提示词。
//
// 【这是切模式要解决的那件事】它原来整段用 screen_reading.md,而那份 .md
// 只讲强电井除湿机 —— 模型不知道有这几个 code,永远不返回,现场每天手点 4 下。
func TestZihanDailyCoversAreaChecks(t *testing.T) {
	tpl, ok := templateByID("zihan_daily")
	if !ok {
		t.Fatal("找不到 zihan_daily")
	}
	text := renderTemplatePrompt(tpl)
	for _, code := range []string{
		"distribution_box", "distribution_box_inside", "weak_room", "fire_pump_room",
		"temperature", "humidity", "strong_room_01",
	} {
		if !strings.Contains(text, "`"+code+"`") {
			t.Errorf("提示词里没有 %s —— AI 不会返回这个字段", code)
		}
	}
	// 切回字段表会丢掉 .md 里的读屏细节,这几条必须已经搬进补充说明
	for _, want := range []string{"七段数码管", "retake_required", "时间戳"} {
		if !strings.Contains(text, want) {
			t.Errorf("补充说明里少了 screen_reading.md 的这部分:%s", want)
		}
	}
}
