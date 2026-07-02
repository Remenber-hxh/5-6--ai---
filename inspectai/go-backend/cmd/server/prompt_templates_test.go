package main

import (
	"strings"
	"testing"
)

// 种子灌库 → 从库取出渲染,应与直接用种子渲染一致(存取无损 + 即时生效基础)
func TestPromptStoreRoundtrip(t *testing.T) {
	store := NewMemStore()
	if err := ensurePromptTemplateSeeds(store); err != nil {
		t.Fatalf("seed failed: %v", err)
	}
	for _, id := range []string{"elevator_machine_room", "elevator_no_room"} {
		fromStore, ok := renderPromptViaStore(store, id)
		if !ok {
			t.Fatalf("renderViaStore %s failed", id)
		}
		fromSeed, _ := renderPromptFromSeed(id)
		if fromStore != fromSeed {
			t.Errorf("%s: 库渲染与种子渲染不一致", id)
		}
	}
	// 改库后立即生效:改一个字段名,再渲染应能看到
	tpl, _, _ := store.GetPromptTemplate("elevator_machine_room")
	tpl.Fields[3].Label = "机房门窗_改过了"
	_ = store.UpsertPromptTemplate(tpl)
	out, _ := renderPromptViaStore(store, "elevator_machine_room")
	if !strings.Contains(out, "机房门窗_改过了") {
		t.Errorf("改库后渲染未反映新内容(即时生效失败)")
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
		"灭火器生产日期≠有效期提示":     "生产日期 ≠ 有效期",
		"防夹从宽":               "判定从宽",
		"异响异味留人工":            "留人工",
		"判否写不符合项":            "逐条写明问题",
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
	s1 := srv.buildChatSources("灭火器怎么判过期", att)
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
	s2 := srv.buildChatSources("最近哪些设备要重点关注", att)
	if count(s2, "asset") == 0 {
		t.Errorf("重点关注应出现设备源, got %v", s2)
	}
	// 无关问句 → 空
	s3 := srv.buildChatSources("你好", att)
	if len(s3) != 0 {
		t.Errorf("无关问句不应有来源, got %v", s3)
	}
	// 审批/计划类问句撞上"处理"等泛词也不给设备来源
	s4 := srv.buildChatSources("目前有哪些待审批工单需要处理？", att)
	if len(s4) != 0 {
		t.Errorf("审批问句不应挂设备来源, got %v", s4)
	}
	// 同名资产(台账重复)只给一组来源
	att2 := append(att, &AttentionItem{AssetID: "a2", AssetName: "HYZX-WJ-DT01", Title: "重复登记", LastRecordID: "r2"})
	s5 := srv.buildChatSources("最近哪些设备要重点关注", att2)
	if count(s5, "asset") != 1 {
		t.Errorf("同名资产应去重为 1 组, got %v", s5)
	}
}
