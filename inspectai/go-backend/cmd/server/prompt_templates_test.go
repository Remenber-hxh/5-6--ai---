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
	view.Fields[0].Label = "机房门窗_改过了"
	view.Fields[0].Mode = ModeVisual
	if err := srv.applyPromptToTemplate(view); err != nil {
		t.Fatal(err)
	}
	out, _ := renderPromptViaStore(store, "elevator_machine_room")
	if !strings.Contains(out, "机房门窗_改过了") {
		t.Error("改完没有立刻生效 —— 后台改提示词会变成「保存了但没变」")
	}
}

// 从提示词字段表里删掉一行 = 这一项不再让 AI 判。
//
// 【只覆盖不清除是个静默 bug】人在界面上删了、保存成功了,而 AI 照旧在判 ——
// 界面和实际行为对不上,谁都看不出来。
func TestRemovingFieldFromPromptStopsAIJudging(t *testing.T) {
	isolateTemplateCache(t)
	store := NewMemStore()
	if err := loadReportTemplates(store); err != nil {
		t.Fatal(err)
	}
	srv := &Server{store: store}

	view, _ := promptTemplateOrDraft(store, "elevator_machine_room")
	before := len(view.Fields)
	if before < 2 {
		t.Fatalf("前置条件不成立,只有 %d 个判定字段", before)
	}
	dropped := view.Fields[0].Code
	view.Fields = view.Fields[1:]
	if err := srv.applyPromptToTemplate(view); err != nil {
		t.Fatal(err)
	}

	after, _ := promptTemplateOrDraft(store, "elevator_machine_room")
	if len(after.Fields) != before-1 {
		t.Errorf("删掉的字段还在:期望 %d 项,实际 %d 项", before-1, len(after.Fields))
	}
	for _, f := range after.Fields {
		if f.Code == dropped {
			t.Errorf("字段 %s 已从提示词里删掉,却还带着判定规则", dropped)
		}
	}
	// 表单定义不能跟着被删 —— 那是模板页在管的,提示词页无权删表单字段
	tpl, _ := templateByID("elevator_machine_room")
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
