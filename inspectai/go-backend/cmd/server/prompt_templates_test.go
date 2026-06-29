package main

import (
	"strings"
	"testing"
)

func TestRenderElevatorTemplates(t *testing.T) {
	for _, id := range []string{"elevator_machine_room", "elevator_no_room"} {
		out, ok := RenderPrompt(id)
		if !ok {
			t.Fatalf("render %s failed", id)
		}
		t.Logf("\n========== %s ==========\n%s", id, out)
	}
}

// 有机房应覆盖机房组 + 轿厢组所有字段,且模式渲染正确
func TestElevatorMachineRoomFieldsCovered(t *testing.T) {
	out, _ := RenderPrompt("elevator_machine_room")
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
	out, _ := RenderPrompt("elevator_no_room")
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
