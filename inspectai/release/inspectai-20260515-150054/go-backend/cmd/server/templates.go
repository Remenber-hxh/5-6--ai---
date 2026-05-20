package main

import (
	"strings"
	"time"
)

// 第一版只对 5 个场景接 AI prompt（3 主 + 2 辅，详见 plan/06-场景收敛.md）。
// 其他模板保留入口但不调 AI，走"人工填写"路径，避免点入死路。

func reportTemplates() []ReportTemplate {
	return []ReportTemplate{
		// === 3 主场景 ===
		{
			ID:        "zihan_energy",
			Name:      "能耗抄表",
			Project:   "紫涵雅集",
			AssetType: "能耗表组",
			MaxImages: 5,
			Featured:  true,
			HasAI:     true,
			AIPrompt:  "energy_meter",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "manual"),
				numberField("z1_reading", "Z1 能耗表读数", false, "ai"),
				numberField("z2_reading", "Z2 能耗表读数", false, "ai"),
				numberField("z3_reading", "Z3 能耗表读数", false, "ai"),
				numberField("z4_reading", "Z4 能耗表读数", false, "ai"),
				numberField("living_water_reading", "生活水表读数", false, "ai"),
				textField("note", "备注", false, "manual"),
			},
		},
		{
			ID:        "zihan_daily",
			Name:      "综合巡检",
			Project:   "紫涵雅集",
			AssetType: "综合巡检点",
			MaxImages: 3,
			Featured:  true,
			HasAI:     true,
			AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "manual"),
				numberField("temperature", "温度℃", false, "ai"),
				numberField("humidity", "湿度%", false, "ai"),
				choiceField("strong_room_01", "强电井室内情况", true, []string{"正常", "异常"}),
				choiceField("distribution_box", "配电箱情况", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "manual"),
			},
		},
		{
			ID:        "hot_water_room",
			Name:      "热水机房巡检",
			Project:   "会议中心",
			AssetType: "热水机房",
			MaxImages: 3,
			Featured:  false, // codex/22 第一阶段：业务范围收敛，会议中心暂不展示
			HasAI:     true,
			AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				numberField("cabinet_temperature", "控制柜显示温度℃", true, "ai"),
				numberField("tank_level", "水箱水位（米）", false, "manual"),
				numberField("water_pressure", "供水压力（MPa）", false, "manual"),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "manual"),
			},
		},
		// === 2 辅场景 ===
		{
			ID:        "fire_pump",
			Name:      "消防泵房巡检",
			Project:   "会议中心",
			AssetType: "消防泵房",
			MaxImages: 3,
			Featured:  false, // codex/22 第一阶段：业务范围收敛
			HasAI:     false, // 第一版不接 AI，纯人工填写（图片质量限制）
			Fields: []TemplateField{
				numberField("tank_level", "水箱水位（米）", true, "manual"),
				choiceField("sewage_auto", "污水泵是否自动", true, []string{"是", "否"}),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "manual"),
			},
		},
		{
			ID:        "ups_room",
			Name:      "UPS 机房巡检",
			Project:   "会议中心",
			AssetType: "UPS 机房",
			MaxImages: 3,
			Featured:  false, // codex/22 第一阶段：业务范围收敛
			HasAI:     false, // 环境照为主，第一版不接 AI
			Fields: []TemplateField{
				textField("asset_no", "UPS 主机编号", true, "manual"),
				choiceField("battery_alarm", "蓄电池组：是否有报警", true, []string{"否", "是"}),
				choiceField("battery_appearance", "蓄电池组：外观", true, []string{"良好", "异常"}),
				numberField("battery_voltage", "电池电压（V）", false, "manual"),
				numberField("output_current", "输出电流（A）", false, "manual"),
				numberField("output_voltage", "输出电压（V）", false, "manual"),
				choiceField("indicator_status", "配电箱：指示灯", true, []string{"正常", "异常"}),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "manual"),
			},
		},
	}
}

// 静态点位列表 — 与模板一一对应，前端默认只显示 Featured=true 的
// codex/22 第一阶段：当前业务入口只保留紫涵雅集两个点位，会议中心暂不展示
func seedPoints() []Point {
	return []Point{
		{ID: "p_zihan_energy", Project: "紫涵雅集", Name: "能耗抄表点位", Type: "能耗抄表", Location: "园区水电表点位", TemplateID: "zihan_energy", Featured: true},
		{ID: "p_zihan_daily", Project: "紫涵雅集", Name: "综合巡检点位", Type: "综合巡检", Location: "园区", TemplateID: "zihan_daily", Featured: true},
		{ID: "p_hot_water", Project: "会议中心", Name: "热水机房", Type: "日常巡检", Location: "B1 机房", TemplateID: "hot_water_room", Featured: false},
		{ID: "p_fire_pump", Project: "会议中心", Name: "消防泵房", Type: "日常巡检", Location: "B1 水泵房", TemplateID: "fire_pump", Featured: false},
		{ID: "p_ups", Project: "会议中心", Name: "UPS 机房", Type: "日常巡检", Location: "B1 设备区", TemplateID: "ups_room", Featured: false},
	}
}

func textField(code, label string, required bool, source string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "text", Required: required, Source: source}
}

func numberField(code, label string, required bool, source string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "number", Required: required, Source: source}
}

func choiceField(code, label string, required bool, options []string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "choice", Required: required, Source: "ai", Options: options}
}

func templateByID(id string) (ReportTemplate, bool) {
	for _, tpl := range reportTemplates() {
		if tpl.ID == id {
			return tpl, true
		}
	}
	return ReportTemplate{}, false
}

func pointByID(id string) (Point, bool) {
	for _, p := range seedPoints() {
		if p.ID == id {
			return p, true
		}
	}
	return Point{}, false
}

// initialFieldValues — 创建记录时初始化字段值（manual 字段填默认，ai 字段留空）
func initialFieldValues(tpl ReportTemplate, inspector string) []FieldValue {
	values := make([]FieldValue, 0, len(tpl.Fields))
	for _, field := range tpl.Fields {
		value := ""
		source := field.Source
		switch field.Code {
		case "inspection_time":
			value = time.Now().Format("2006-01-02 15:04")
		case "inspector":
			value = inspector
		}
		// 修 plan/08- S3：manual 自动填好的字段不设 NeedsReview=true
		needsReview := field.Required && field.Source == "ai"
		if field.Required && strings.TrimSpace(value) == "" && field.Source != "ai" {
			needsReview = true
		}
		values = append(values, FieldValue{
			Code:        field.Code,
			Label:       field.Label,
			Kind:        field.Kind,
			Required:    field.Required,
			Value:       value,
			Source:      source,
			Options:     field.Options,
			NeedsReview: needsReview,
			Version:     1,
		})
	}
	return values
}
