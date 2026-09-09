package main

import (
	"strings"
	"testing"
)

// 合并之后:判定规则存在模板字段表上,改了要【立刻】反映到渲染结果里。
//
// 【这条以前测的是另一条路】合并前提示词单独一张表,这里验的是"写那张表
// 能不能渲染出来"。现在那张表不再作为事实来源 —— 继续那样测的话,
// 测试会一直绿着,而后台改提示词其实完全不生效。
func TestPromptRendersFromMergedTemplate(t *testing.T) {
	isolateTemplateCache(t)
	store := NewMemStore()
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{"elevator_machine_room", "elevator_no_room"} {
		out, ok := renderPromptViaStore(store, id)
		if !ok {
			t.Fatalf("%s 应该渲染得出来", id)
		}
		for _, want := range []string{"字段映射", "置信度", "输出"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s 渲染结果缺少「%s」段", id, want)
			}
		}
	}

	// 改一个字段的判定规则,渲染应立刻反映 —— 不生效的话人会反复保存,
	// 而问题在别处。
	srv := &Server{store: store}
	view, ok := promptTemplateOrDraft(store, "elevator_machine_room")
	if !ok {
		t.Fatal("取不到有机房电梯的提示词视图")
	}
	if len(view.Fields) == 0 {
		t.Fatal("有机房电梯本该有判定规则 —— 断言没生效")
	}
	// 【改判定规则,不改中文名】中文名归模板页管,提示词页写不动
	view.Fields[0].Mode = ModeVisual
	view.Fields[0].YesWhen = "改过的判定依据"
	if err := srv.applyPromptToTemplate(view); err != nil {
		t.Fatal(err)
	}
	out, _ := renderPromptViaStore(store, "elevator_machine_room")
	if !strings.Contains(out, "改过的判定依据") {
		t.Error("改完没有立刻生效 —— 后台改提示词会变成「保存了但没变」")
	}
}

// 取消一个字段的判定,AI 就必须真的不再判它。
//
// 【交互变了,保证没变】以前字段表只列配过的字段,所以"不让 AI 判"是
// 把那一行删掉。现在表里列的是模板【全部】字段,取消的方式是把判定模式
// 清空(界面上那个下拉可以清空)。两种做法在接口上是一回事:
// 提交的 payload 里没有它、或者它的 mode 是空 —— 判定规则都要被清掉。
//
// 【只覆盖不清除是个静默 bug】人在界面上取消了、保存成功了,而 AI 照旧在判 ——
// 界面和实际行为对不上,谁都看不出来。
func TestClearingJudgeModeStopsAIJudging(t *testing.T) {
	isolateTemplateCache(t)
	store := NewMemStore()
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}
	srv := &Server{store: store}

	view, _ := promptTemplateOrDraft(store, "elevator_machine_room")
	judgedBefore := len(promptViewOfTemplate(mustTemplate(t, "elevator_machine_room")).Fields)
	if judgedBefore < 2 {
		t.Fatalf("前置条件不成立,只有 %d 个已配判定的字段", judgedBefore)
	}
	// 找一个配过的,把它的模式清空 —— 这就是界面上"不让 AI 判这一项"的动作
	var dropped string
	for i, f := range view.Fields {
		if f.Mode != "" {
			dropped = f.Code
			view.Fields[i].Mode = ""
			view.Fields[i].YesWhen = ""
			view.Fields[i].NoWhen = ""
			break
		}
	}
	if dropped == "" {
		t.Fatal("前置条件不成立:没找到配过判定模式的字段")
	}
	if err := srv.applyPromptToTemplate(view); err != nil {
		t.Fatal(err)
	}

	tpl := mustTemplate(t, "elevator_machine_room")

	// 【核心】渲染出去的提示词里不能再有它 —— 那才是 AI 实际收到的东西
	judged := promptViewOfTemplate(tpl)
	if len(judged.Fields) != judgedBefore-1 {
		t.Errorf("判定字段数:期望 %d,实际 %d", judgedBefore-1, len(judged.Fields))
	}
	for _, f := range judged.Fields {
		if f.Code == dropped {
			t.Errorf("字段 %s 的判定已取消,却还出现在渲染视图里 —— AI 会照旧判它", dropped)
		}
	}

	// 它仍留在编辑视图里,只是模式为空 —— 否则人再也没法把它配回来
	edit := promptEditViewOfTemplate(tpl)
	var seen bool
	for _, f := range edit.Fields {
		if f.Code == dropped {
			seen = true
			if f.Mode != "" {
				t.Errorf("模式该被清空,实际 %q", f.Mode)
			}
		}
	}
	if !seen {
		t.Errorf("字段 %s 从编辑视图里消失了 —— 那就再也配不回来了", dropped)
	}

	// 表单定义不能跟着被删 —— 那是模板页在管的,提示词页无权删表单字段
	var stillInForm bool
	for _, f := range tpl.Fields {
		if f.Code == dropped {
			stillInForm = true
		}
	}
	if !stillInForm {
		t.Errorf("字段 %s 连表单定义一起被删了 —— 提示词页不该动表单", dropped)
	}
}

func mustTemplate(t *testing.T, id string) ReportTemplate {
	t.Helper()
	tpl, ok := templateByID(id)
	if !ok {
		t.Fatalf("模板 %s 不存在", id)
	}
	return tpl
}

func TestRenderElevatorTemplates(t *testing.T) {
	for _, id := range []string{"elevator_machine_room", "elevator_no_room"} {
		out, ok := renderPromptFromSeed(id)
		if !ok {
			t.Fatalf("render %s failed", id)
		}
		t.Logf("\n========== %s ==========\n%s", id, out)
	}
}

// 有机房应覆盖机房组 + 轿厢组所有字段,且模式渲染正确
func TestElevatorMachineRoomFieldsCovered(t *testing.T) {
	out, _ := renderPromptFromSeed("elevator_machine_room")
	must := []string{
		"door_window_sign", "room_clean", "lighting_ac", "extinguisher_valid", "noise_smell", "rescue_device",
		"reg_mark", "alarm_device", "anti_clip", "door_smooth", "floor_buttons", "car_lighting", "fire_switch_glass",
		"nonconformity", "asset_no", "inspection_time", "inspector",
	}
	for _, code := range must {
		if !strings.Contains(out, "`"+code+"`") {
			t.Errorf("有机房缺字段: %s", code)
		}
	}
	// 关键规则渲染检查
	checks := map[string]string{
		"灭火器用 current_date 比对": "current_date",
		"灭火器生产日期≠有效期提示":        "生产日期 ≠ 有效期",
		"防夹从宽":    "判定从宽",
		"异响异味留人工": "留人工",
		"判否写不符合项": "逐条写明问题",
	}
	for name, frag := range checks {
		if !strings.Contains(out, frag) {
			t.Errorf("缺规则[%s]: 未找到 %q", name, frag)
		}
	}
}

// 无机房应只有轿厢组(无机房字段),不含机房组字段
func TestElevatorNoRoomNoMachineFields(t *testing.T) {
	out, _ := renderPromptFromSeed("elevator_no_room")
	machineOnly := []string{"door_window_sign", "room_clean", "lighting_ac", "extinguisher_valid", "rescue_device"}
	for _, code := range machineOnly {
		if strings.Contains(out, "`"+code+"`") {
			t.Errorf("无机房不该有机房字段: %s", code)
		}
	}
	// 轿厢组应在
	for _, code := range []string{"floor_buttons", "car_lighting", "anti_clip", "reg_mark"} {
		if !strings.Contains(out, "`"+code+"`") {
			t.Errorf("无机房缺轿厢字段: %s", code)
		}
	}
}

func TestBuildChatSourcesPrecision(t *testing.T) {
	store := NewMemStore()
	if err := ensurePromptTemplateSeeds(store); err != nil {
		t.Fatalf("seed: %v", err)
	}
	srv := &Server{store: store}
	att := []*AttentionItem{{AssetID: "a1", AssetName: "HYZX-WJ-DT01", Title: "风险高", LastRecordID: "r1", Reasons: []string{"按钮异常"}}}

	count := func(src []map[string]any, typ string) int {
		n := 0
		for _, x := range src {
			if x["type"] == typ {
				n++
			}
		}
		return n
	}

	// 问检查项 → 只给标准源,不给设备源
	s1 := srv.buildChatSources("灭火器怎么判过期", "", att)
	if count(s1, "standard") == 0 {
		t.Errorf("灭火器问句应出现标准源, got %v", s1)
	}
	// 标准源 detail 应是大白话,不含技术占位符 current_date
	for _, x := range s1 {
		if x["type"] == "standard" {
			d, _ := x["detail"].(string)
			if strings.Contains(d, "current_date") {
				t.Errorf("标准源 detail 仍是技术文本(含 current_date): %s", d)
			}
			if !strings.Contains(d, "灭火器") {
				t.Errorf("标准源 detail 不像大白话说明: %s", d)
			}
		}
	}
	if count(s1, "record")+count(s1, "asset") > 0 {
		t.Errorf("灭火器问句不应出现设备源, got %v", s1)
	}
	// 问重点关注 → 给设备源
	s2 := srv.buildChatSources("最近哪些设备要重点关注", "", att)
	if count(s2, "asset") == 0 {
		t.Errorf("重点关注应出现设备源, got %v", s2)
	}
	// 无关问句 → 空
	s3 := srv.buildChatSources("你好", "", att)
	if len(s3) != 0 {
		t.Errorf("无关问句不应有来源, got %v", s3)
	}
	// 审批/计划类问句撞上"处理"等泛词也不给设备来源
	s4 := srv.buildChatSources("目前有哪些待审批工单需要处理？", "", att)
	if len(s4) != 0 {
		t.Errorf("审批问句不应挂设备来源, got %v", s4)
	}
	// 同名资产(台账重复)只给一组来源
	att2 := append(att, &AttentionItem{AssetID: "a2", AssetName: "HYZX-WJ-DT01", Title: "重复登记", LastRecordID: "r2"})
	s5 := srv.buildChatSources("最近哪些设备要重点关注", "", att2)
	if count(s5, "asset") != 1 {
		t.Errorf("同名资产应去重为 1 组, got %v", s5)
	}
	// 证据跟着答案走:答案只点名 K07 → 只给 K07,不给风险更高但没被提到的
	att3 := []*AttentionItem{
		{AssetID: "a1", AssetName: "HYZX-WJ-DT01", Title: "风险高", LastRecordID: "r1"},
		{AssetID: "a3", AssetName: "K07", Title: "留意", LastRecordID: "r3"},
	}
	s6 := srv.buildChatSources("哪些设备要关注", "K07 电梯近期异常需留意。", att3)
	if count(s6, "asset") != 1 {
		t.Errorf("答案点名时应只给被点名设备, got %v", s6)
	}
	for _, x := range s6 {
		title, _ := x["title"].(string)
		if strings.Contains(title, "HYZX") {
			t.Errorf("未被答案点名的设备不应出现: %v", s6)
		}
	}
}

// 提示词页的字段表要列出模板里【所有】字段,包括还没配判定规则的。
//
// 【原来是个死路】编辑视图和渲染视图共用一份,只收配过 judgeMode 的字段:
// 字段要配过才出现在表里,而配置的唯一入口就是这张表。于是模板明明有
// 13 个字段,页面上却说"这个模板还没有字段表",人只能改用整段文本手写 ——
// 而手写的那份和字段表是两套东西,以后再想回到字段表就更难了。
func TestPromptEditViewListsUnconfiguredFields(t *testing.T) {
	tpl := ReportTemplate{
		ID: "tpl_x", Name: "综合巡检",
		Fields: []TemplateField{
			{Code: "configured", Label: "配过的", JudgeMode: ModeVisual, YesWhen: "看着正常"},
			{Code: "bare", Label: "没配过的"},
			{Code: "bare2", Label: "也没配过", JudgeMode: "   "}, // 只有空白也算没配
		},
	}

	edit := promptEditViewOfTemplate(tpl)
	if len(edit.Fields) != 3 {
		t.Fatalf("编辑视图要列出全部 3 个字段,实际 %d —— "+
			"少列的那些就永远配不上判定规则(配置入口就是这张表)", len(edit.Fields))
	}
	byCode := map[string]PromptField{}
	for _, f := range edit.Fields {
		byCode[f.Code] = f
	}
	if byCode["bare"].Label != "没配过的" {
		t.Errorf("没配过的字段也要带上中文名,实际 %+v", byCode["bare"])
	}
	if byCode["bare"].Mode != "" {
		t.Errorf("没配过的字段模式应为空(空 = 不让 AI 判),实际 %q", byCode["bare"].Mode)
	}
	if byCode["configured"].Mode != ModeVisual {
		t.Errorf("配过的字段规则要原样带出来,实际 %+v", byCode["configured"])
	}

	// 【渲染那边不能跟着变】没配 judgeMode 的字段渲染出来是"只有字段名、
	// 没有判断依据"的一行 —— 模型照跑,结果随机。
	render := promptViewOfTemplate(tpl)
	if len(render.Fields) != 1 || render.Fields[0].Code != "configured" {
		t.Errorf("渲染视图只应包含配过的那 1 个,实际 %d 个:%+v",
			len(render.Fields), render.Fields)
	}
}

// 本场景补充说明:和字段表【共存】,不是二选一。
//
// 【它存在的理由】有些话落不进字段表的任何一格,比如"防夹/开关门是现场
// 测试项,拍到测试动作就判,别一律留空"—— 它是对整份提示词的补充。
// 没有这个口子的话,想说这句话只能切「整段文本」自己写整封信,
// 等于为了加一句话放弃全部结构化配置。
func TestExtraNotesRendersAlongsideFieldTable(t *testing.T) {
	tpl := ReportTemplate{
		ID: "tpl_n", Name: "测试模板", Scene: "某机房",
		ExtraNotes: "防夹/开关门是现场测试项,拍到测试动作就判,别一律留空\n- 表盘反光时先判断能不能读清",
		Fields: []TemplateField{
			{Code: "a", Label: "甲项", JudgeMode: ModeVisual, YesWhen: "看着正常"},
		},
	}
	out := renderTemplatePrompt(tpl)

	if !strings.Contains(out, "别一律留空") {
		t.Errorf("补充说明没进提示词 —— 那这个口子等于没开\n%s", out)
	}
	// 【必须在总则之后、字段映射之前】先立通用规矩,再说本场景的例外;
	// 顺序反了通用那几条会把场景交代盖过去。
	iRule := strings.Index(out, "## 总则")
	iNote := strings.Index(out, "别一律留空")
	iField := strings.Index(out, "## 字段映射")
	if !(iRule < iNote && iNote < iField) {
		t.Errorf("补充说明的位置不对:总则=%d 补充=%d 字段映射=%d", iRule, iNote, iField)
	}
	// 字段表照常渲染 —— 两者共存,不是谁替代谁
	if !strings.Contains(out, "甲项") || !strings.Contains(out, "看着正常") {
		t.Errorf("字段表不见了 —— 补充说明不该顶替字段表\n%s", out)
	}
	// 人自己写了 "- " 的行不该出现 "- - "
	if strings.Contains(out, "- - ") {
		t.Errorf("行首重复加了 '- ':\n%s", out)
	}
}

// 【只写补充说明、一条判定都没配 → 仍然回退内置 .md】
//
// 渲染出来会是"有交代、没字段",模型不知道该返回哪些 code,结果全空。
// 与其发一份注定判不出东西的提示词,不如让它继续用内置那份。
func TestExtraNotesAloneStillFallsBack(t *testing.T) {
	tpl := ReportTemplate{
		ID: "tpl_only_notes", Name: "只有补充说明",
		ExtraNotes: "这个场景要特别注意反光",
		Fields:     []TemplateField{{Code: "a", Label: "甲项"}}, // 没有 JudgeMode
	}
	if got := renderTemplatePrompt(tpl); got != "" {
		t.Errorf("一条判定规则都没配时应回退内置 .md(返回空),实际渲染出了 %d 字", len(got))
	}
}
