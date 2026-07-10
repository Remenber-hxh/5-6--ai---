package main

import (
	"strings"
	"time"
)

// 当前恢复完整业务入口：紫菡雅集 + 会议中心相关模板均可选。
// 只有已沉淀 prompt 的模板接 AI；其他模板保留人工填写路径，避免生成假识别结果。

func reportTemplates() []ReportTemplate {
	return []ReportTemplate{
		// === 3 主场景 ===
		{
			ID:        "zihan_energy",
			Name:      "能耗抄表",
			Project:   "紫菡雅集",
			AssetType: "能耗表组",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
			AIPrompt:  "energy_meter",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "ai"),
				numberField("z1_reading", "Z1 能耗表读数", false, "ai"),
				numberField("z2_reading", "Z2 能耗表读数", false, "ai"),
				numberField("z3_reading", "Z3 能耗表读数", false, "ai"),
				numberField("z4_reading", "Z4 能耗表读数", false, "ai"),
				numberField("living_water_reading", "生活水表读数", false, "ai"),
				numberField("fire_water_reading", "消防水表读数", false, "ai"),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "zihan_daily",
			Name:      "综合巡检",
			Project:   "紫菡雅集",
			AssetType: "综合巡检点",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
			AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "ai"),
				textField("location", "位置", false, "ai"),
				numberField("temperature", "温度℃", false, "ai"),
				numberField("humidity", "湿度%", false, "ai"),
				choiceField("strong_room_01", "强电井室内情况", true, []string{"正常", "异常"}),
				textField("strong_room_01_note", "强电井备注", false, "ai"),
				choiceField("distribution_box", "配电箱情况", true, []string{"正常", "异常"}),
				choiceField("distribution_box_inside", "配电箱内部情况", true, []string{"正常", "异常"}),
				choiceField("weak_room", "弱电机房情况", true, []string{"正常", "异常"}),
				choiceField("fire_pump_room", "消防泵房情况", true, []string{"正常", "异常"}),
				textField("progress", "项目进度", false, "ai"),
				textField("extra_work", "日常增项汇报", false, "ai"),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "hot_water_room",
			Name:      "热水机房巡检",
			Project:   "会议中心",
			AssetType: "热水机房",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
			AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", false, "ai"),
				numberField("cabinet_temperature", "控制柜显示温度℃", true, "ai"),
				numberField("tank_level", "水箱水位（米）", false, "ai"),
				numberField("water_pressure", "供水压力（MPa）", false, "ai"),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "fire_pump",
			Name:      "消防泵房巡检",
			Project:   "会议中心",
			AssetType: "消防泵房",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", false, "ai"),
				numberField("tank_level", "水箱水位（米）", true, "ai"),
				choiceField("sewage_auto", "污水泵是否自动", true, []string{"是", "否"}),
				textField("sewage_exception", "污水泵异常说明", false, "ai"),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				textField("leak_alarm_exception", "漏水/报警异常说明", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "ups_room",
			Name:      "UPS 机房巡检",
			Project:   "会议中心",
			AssetType: "UPS 机房",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", false, "ai"),
				textField("asset_no", "UPS 主机编号", true, "ai"),
				choiceField("battery_alarm", "蓄电池组：是否有报警", true, []string{"否", "是"}),
				choiceField("battery_appearance", "蓄电池组：外观", true, []string{"良好", "异常"}),
				textField("battery_exception", "蓄电池组异常说明", false, "ai"),
				numberField("battery_voltage", "电池电压（V）", false, "ai"),
				numberField("input_voltage", "输入电压（V）", false, "ai"),
				numberField("output_current", "输出电流（A）", false, "ai"),
				numberField("output_voltage", "输出电压（V）", false, "ai"),
				choiceField("battery_status", "蓄电池组：电池状态", false, []string{"正常", "异常", "待复核"}),
				choiceField("box_smell", "配电箱：电箱内有无异味", false, []string{"无", "有"}),
				choiceField("indicator_status", "配电箱：指示灯", true, []string{"正常", "异常"}),
				choiceField("fire_equipment", "机房空间：消防器材是否完好", false, []string{"是", "否"}),
				choiceField("ventilation", "机房空间：室内通风是否正常", false, []string{"正常", "异常"}),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("lighting", "机房照明", false, []string{"正常", "异常"}),
				textField("temperature_humidity", "温湿度", false, "ai"),
				textField("room_exception", "机房异常说明", false, "ai"),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "power_room",
			Name:      "变电所巡检",
			Project:   "会议中心",
			AssetType: "变电所",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "substation",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", false, "ai"),
				textField("asset_no", "变电所编号", true, "ai"),
				choiceField("hv_appearance", "高压柜：外观检查是否良好", true, []string{"是", "否"}),
				choiceField("meter_abnormal", "高压柜：电度表是否显示异常", true, []string{"否", "是"}),
				choiceField("voltage_normal", "高压柜：电压是否正常", true, []string{"是", "否"}),
				choiceField("hv_alarm", "高压柜：是否有报警", true, []string{"否", "是"}),
				choiceField("temperature_humidity_abnormal", "高压柜：温湿度显示是否异常", true, []string{"否", "是"}),
				choiceField("hv_smell", "高压柜：有无异常气味", true, []string{"无", "有"}),
				textField("hv_exception", "高压柜异常说明", false, "ai"),
				textField("transformer_temperature", "变压器：温度及三相温差", false, "ai"),
				choiceField("transformer_appearance", "变压器：外观检查是否良好", false, []string{"是", "否"}),
				choiceField("transformer_noise", "变压器：声音是否异常", true, []string{"否", "是"}),
				choiceField("room_lighting", "机房照明是否正常", false, []string{"是", "否"}),
				textField("room_temperature_humidity", "机房温湿度", true, "manual"),
				choiceField("room_clean", "机房卫生", false, []string{"正常", "异常"}),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "water_pump",
			Name:      "生活水泵房巡检",
			Project:   "会议中心",
			AssetType: "生活水泵房",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "screen_reading",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", false, "ai"),
				numberField("tank_level", "水箱水位（米）", true, "ai"),
				numberField("water_pressure", "供水压力（MPa）", true, "ai"),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				textField("leak_alarm_exception", "漏水/报警异常说明", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				textField("note", "备注", false, "ai"),
			},
		},
		{
			ID:        "escalator",
			Name:      "扶梯巡检",
			Project:   "会议中心",
			AssetType: "扶梯",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "escalator",
			Fields: []TemplateField{
				textField("inspection_time", "检查时间", false, "ai"),
				textField("asset_no", "扶梯编号", false, "ai"),
				textField("inspector", "巡检人员", false, "ai"),
				// 自动扶梯及自动人行道
				choiceField("entrance_sign", "出入口警示标识是否完好", false, []string{"是", "否"}),
				choiceField("anti_climb", "防档防攀爬是否完好", false, []string{"是", "否"}),
				choiceField("emergency_stop", "紧急制动开关是否正常", false, []string{"是", "否"}),
				choiceField("comb_plate", "疏齿板是否牢固、齿是否完整", false, []string{"是", "否"}),
				choiceField("steps", "梯级是否完好", false, []string{"是", "否"}),
				choiceField("pit_cover", "上、下基坑盖板是否牢固", false, []string{"是", "否"}),
				choiceField("handrail", "扶手带是否完好", false, []string{"是", "否"}),
				choiceField("skirt_panel", "扶梯护壁板是否牢固", false, []string{"是", "否"}),
				choiceField("safety_brush", "安全毛刷是否完好", false, []string{"是", "否"}),
				choiceField("running_speed", "扶梯运行速度是否正常", false, []string{"是", "否"}),
				choiceField("running_noise", "扶梯运行是否有异响", false, []string{"是", "否"}),
				choiceField("inspection_cert", "《安全检验合格》标志是否完好", false, []string{"是", "否"}),
				textField("nonconformity", "不符合项说明", false, "ai"),
			},
		},
		{
			ID:        "elevator_machine_room",
			Name:      "电梯巡检（有机房）",
			Project:   "会议中心",
			AssetType: "有机房电梯",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "elevator_machine_room",
			Fields: []TemplateField{
				textField("inspection_time", "检查时间", false, "ai"),
				textFieldDefault("asset_no", "电梯编号", false, "ai", "HYZX-WJ-DT01"),
				textField("inspector", "检查人员", false, "ai"),
				// 机房及其设备
				choiceField("door_window_sign", "机房门窗、警示标识完好", false, []string{"是", "否"}),
				choiceField("room_clean", "机房干净无杂物", false, []string{"是", "否"}),
				choiceField("lighting_ac", "机房照明及空调正常", false, []string{"是", "否"}),
				choiceField("extinguisher_valid", "灭火器材未过期", false, []string{"是", "否"}),
				choiceField("noise_smell", "设备无异响、异味", false, []string{"是", "否"}),
				choiceField("rescue_device", "紧急救援装置齐全", false, []string{"是", "否"}),
				// 轿厢及层站
				choiceField("reg_mark", "电梯使用登记标志完好", false, []string{"是", "否"}),
				choiceField("alarm_device", "紧急报警装置有效", false, []string{"是", "否"}),
				choiceField("anti_clip", "轿门防夹人装置有效", false, []string{"是", "否"}),
				choiceField("door_smooth", "开关门运行无卡阻", false, []string{"是", "否"}),
				choiceField("floor_buttons", "选层按钮及显示正常", false, []string{"是", "否"}),
				choiceField("car_lighting", "候梯厅、轿厢内照明正常", false, []string{"是", "否"}),
				choiceField("fire_switch_glass", "消防开关玻璃完好", false, []string{"是", "否"}),
				textField("nonconformity", "不符合项处理情况记录", false, "ai"),
			},
		},
		{
			ID:        "elevator_no_room",
			Name:      "电梯巡检（无机房）",
			Project:   "会议中心",
			AssetType: "无机房电梯",
			MaxImages: 20,
			Featured:  true,
			HasAI:     true,
            AIPrompt:  "elevator_no_room",
			Fields: []TemplateField{
				textField("inspection_time", "日期", false, "ai"),
				textFieldDefault("asset_no", "电梯编号", false, "ai", "HYZX-WJ-DT01"),
				textField("inspector", "检查人", false, "ai"),
				// 无机房电梯无独立机房,只查轿厢/层站
				choiceField("reg_mark", "电梯使用登记标志完好", false, []string{"是", "否"}),
				choiceField("alarm_device", "紧急报警装置有效", false, []string{"是", "否"}),
				choiceField("anti_clip", "轿门防夹人装置有效", false, []string{"是", "否"}),
				choiceField("door_smooth", "开关门运行无卡阻", false, []string{"是", "否"}),
				choiceField("floor_buttons", "选层按钮及显示正常", false, []string{"是", "否"}),
				choiceField("car_lighting", "候梯厅、轿厢内照明正常", false, []string{"是", "否"}),
				choiceField("fire_switch_glass", "消防开关玻璃完好", false, []string{"是", "否"}),
				textField("nonconformity", "不符合项处理情况记录", false, "ai"),
			},
		},
	}
}

// 静态点位列表 — 与模板一一对应，前端默认只显示 Featured=true 的
func seedPoints() []Point {
	return []Point{
		{ID: "p_zihan_energy", Project: "紫菡雅集", Name: "能耗抄表点位", Type: "能耗抄表", Location: "园区水电表点位", TemplateID: "zihan_energy", Featured: true},
		{ID: "p_zihan_daily", Project: "紫菡雅集", Name: "综合巡检点位", Type: "综合巡检", Location: "园区", TemplateID: "zihan_daily", Featured: true},
		{ID: "p_hot_water", Project: "会议中心", Name: "热水机房", Type: "日常巡检", Location: "B1 机房", TemplateID: "hot_water_room", Featured: true},
		{ID: "p_fire_pump", Project: "会议中心", Name: "消防泵房", Type: "日常巡检", Location: "B1 水泵房", TemplateID: "fire_pump", Featured: true},
		{ID: "p_ups", Project: "会议中心", Name: "UPS 机房", Type: "日常巡检", Location: "B1 设备区", TemplateID: "ups_room", Featured: true},
		{ID: "p_power_room", Project: "会议中心", Name: "变电所", Type: "日常巡检", Location: "B1 强电间", TemplateID: "power_room", Featured: true},
		{ID: "p_water_pump", Project: "会议中心", Name: "生活水泵房", Type: "日常巡检", Location: "B1 生活水泵房", TemplateID: "water_pump", Featured: true},
		{ID: "p_escalator", Project: "会议中心", Name: "扶梯", Type: "特种设备巡检", Location: "公共区域扶梯", TemplateID: "escalator", Featured: true},
		{ID: "p_elevator_no_room", Project: "会议中心", Name: "无机房电梯", Type: "特种设备巡检", Location: "电梯轿厢", TemplateID: "elevator_no_room", Featured: true},
		{ID: "p_elevator_machine_room", Project: "会议中心", Name: "有机房电梯", Type: "特种设备巡检", Location: "电梯机房", TemplateID: "elevator_machine_room", Featured: true},
	}
}

func textField(code, label string, required bool, source string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "text", Required: required, Source: source}
}

// textFieldDefault — 带预填默认值的文本字段（移动端打开表单时自动填入）
func textFieldDefault(code, label string, required bool, source, def string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "text", Required: required, Source: source, Default: def}
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
		filledByDefault := false
		switch field.Code {
		case "inspection_time":
			value = time.Now().Format("2006-01-02 15:04")
		case "inspector":
			value = inspector
		}
		// 模板预设的 default 兜底（任何字段都生效，且优先于自动生成的）
		if field.Default != "" && value == "" {
			value = field.Default
			filledByDefault = true
		}
		// 修 plan/08- S3：manual 自动填好的字段不设 NeedsReview=true
		needsReview := field.Required && field.Source == "ai"
		if field.Required && strings.TrimSpace(value) == "" && field.Source != "ai" {
			needsReview = true
		}
		// 由模板默认值填充的字段视为"已确认"，提交时不再要求人工 review
		if filledByDefault {
			needsReview = false
			source = "manual"
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
