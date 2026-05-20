package main

import (
	"strings"
	"time"
)

type ReportTemplate struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Project      string          `json:"project"`
	AssetType    string          `json:"assetType"`
	Fields       []TemplateField `json:"fields"`
	MissingNotes []string        `json:"missingNotes,omitempty"`
}

type TemplateField struct {
	Code       string   `json:"code"`
	Label      string   `json:"label"`
	Kind       string   `json:"kind"`
	Required   bool     `json:"required"`
	Source     string   `json:"source"`
	Options    []string `json:"options,omitempty"`
	NeedPhoto  bool     `json:"needPhoto,omitempty"`
	NormalHint string   `json:"normalHint,omitempty"`
}

type FieldValue struct {
	Code        string   `json:"code"`
	Label       string   `json:"label"`
	Kind        string   `json:"kind"`
	Required    bool     `json:"required"`
	Value       string   `json:"value"`
	AIValue     string   `json:"aiValue"`
	Source      string   `json:"source"`
	Options     []string `json:"options,omitempty"`
	Confidence  float64  `json:"confidence"`
	NeedsReview bool     `json:"needsReview"`
	Reason      string   `json:"reason,omitempty"`
	Version     int      `json:"version"`
}

type RecognizedField struct {
	Code       string  `json:"code"`
	Label      string  `json:"label"`
	Value      string  `json:"value"`
	Confidence float64 `json:"confidence"`
	Reason     string  `json:"reason,omitempty"`
}

type AssetEntry struct {
	ID           string `json:"id"`
	AssetType    string `json:"assetType"`
	AssetName    string `json:"assetName"`
	Project      string `json:"project"`
	LastRecordID string `json:"lastRecordId"`
	Status       string `json:"status"`
	Summary      string `json:"summary"`
	UpdatedAt    string `json:"updatedAt"`
}

func reportTemplates() []ReportTemplate {
	return []ReportTemplate{
		{
			ID:        "ups_room",
			Name:      "会议中心UPS机房巡检报批",
			Project:   "会议中心",
			AssetType: "UPS机房",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", true, "manual"),
				textField("asset_no", "UPS机房编号", true, "manual"),
				choiceField("battery_alarm", "蓄电池组：是否有报警", true, []string{"否", "是"}),
				choiceField("battery_appearance", "蓄电池组：外观检查是否良好", true, []string{"是", "否"}),
				textField("battery_exception", "蓄电池组异常说明", false, "ai"),
				numberField("battery_voltage", "蓄电池组：电池电压", true),
				numberField("output_current", "蓄电池组：输出电流", true),
				numberField("output_voltage", "蓄电池组：输出电压", true),
				numberField("input_voltage", "蓄电池组：输入电压", true),
				choiceField("battery_status", "蓄电池组：电池状态", true, []string{"正常", "异常", "待复核"}),
				choiceField("box_smell", "配电箱：电箱内有无异味", true, []string{"无", "有"}),
				choiceField("indicator_status", "配电箱：指示灯是否正常", true, []string{"是", "否"}),
				choiceField("fire_equipment", "机房空间：消防器材是否完好", true, []string{"是", "否"}),
				choiceField("ventilation", "机房空间：室内通风是否正常", true, []string{"是", "否"}),
				choiceField("room_clean", "机房空间：机房卫生是否干净", true, []string{"是", "否"}),
				choiceField("lighting", "机房空间：机内照明是否正常", true, []string{"是", "否"}),
				textField("temperature_humidity", "机房空间：温湿度", false, "ai"),
				textField("room_exception", "机房异常说明", false, "ai"),
			},
		},
		{
			ID:        "power_room",
			Name:      "会议中心变电所巡检报批",
			Project:   "会议中心",
			AssetType: "变电所",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", true, "manual"),
				textField("asset_no", "变电所编号", true, "manual"),
				choiceField("hv_appearance", "高压柜：外观检查是否良好", true, []string{"是", "否"}),
				choiceField("meter_abnormal", "高压柜：电度表是否显示异常", true, []string{"否", "是"}),
				choiceField("voltage_normal", "高压柜：电压是否正常", true, []string{"是", "否"}),
				choiceField("hv_alarm", "高压柜：是否有报警", true, []string{"否", "是"}),
				choiceField("temperature_humidity_abnormal", "高压柜：温湿度显示是否异常", true, []string{"否", "是"}),
				choiceField("hv_smell", "高压柜：有无异常气味", true, []string{"无", "有"}),
				textField("hv_exception", "高压柜异常说明", false, "ai"),
				textField("transformer_temperature", "变压器：温度及三相温差", false, "ai"),
				choiceField("transformer_appearance", "变压器：外观检查是否良好", true, []string{"是", "否"}),
				choiceField("transformer_noise", "变压器：声音是否异常", true, []string{"否", "是"}),
				choiceField("room_lighting", "机房照明是否正常", true, []string{"是", "否"}),
				textField("room_temperature_humidity", "机房温湿度", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
			},
		},
		{
			ID:        "fire_pump",
			Name:      "会议中心消防泵巡检报批",
			Project:   "会议中心",
			AssetType: "消防泵房",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", true, "manual"),
				numberField("tank_level", "水箱水位", true),
				choiceField("sewage_auto", "污水泵是否自动", true, []string{"是", "否"}),
				textField("sewage_exception", "污水泵异常说明", false, "ai"),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				textField("leak_alarm_exception", "漏水/报警异常说明", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
			},
		},
		{
			ID:        "hot_water_room",
			Name:      "会议中心热水机房巡检报批",
			Project:   "会议中心",
			AssetType: "热水机房",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", true, "manual"),
				numberField("tank_level", "水箱水位", true),
				numberField("water_pressure", "供水压力", true),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				textField("leak_alarm_exception", "漏水/报警异常说明", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
				numberField("cabinet_temperature", "控制柜显示温度", false),
				textField("exception_note", "异常说明", false, "ai"),
			},
		},
		{
			ID:        "water_pump",
			Name:      "会议中心生活水泵巡检报批",
			Project:   "会议中心",
			AssetType: "生活水泵房",
			Fields: []TemplateField{
				textField("inspection_time", "日期+时间", true, "manual"),
				numberField("tank_level", "水箱水位", true),
				numberField("water_pressure", "供水压力", true),
				choiceField("leak_alarm", "是否漏水/报警", true, []string{"否", "是"}),
				textField("leak_alarm_exception", "漏水/报警异常说明", false, "ai"),
				choiceField("room_clean", "机房卫生", true, []string{"正常", "异常"}),
				choiceField("room_lighting", "机房照明", true, []string{"正常", "异常"}),
			},
		},
		{
			ID:        "escalator",
			Name:      "会议中心扶梯巡检",
			Project:   "会议中心",
			AssetType: "扶梯",
			Fields: []TemplateField{
				textField("inspection_time", "检查时间", true, "manual"),
				textField("asset_no", "扶梯编号", true, "manual"),
				textField("inspector", "巡检人员", true, "manual"),
				choiceField("warning_sign", "安全警示标志", true, []string{"完好", "缺失", "破损"}),
				choiceField("anti_climb", "防攀爬装置", true, []string{"正常", "异常"}),
				choiceField("emergency_stop", "紧急停止按钮", true, []string{"正常", "异常"}),
				choiceField("comb_plate", "梳齿板", true, []string{"正常", "异常"}),
				choiceField("steps", "梯级/踏板", true, []string{"正常", "异常"}),
				choiceField("handrail", "扶手带", true, []string{"正常", "异常"}),
				choiceField("running_noise", "运行声音", true, []string{"正常", "异常"}),
				textField("nonconformity", "不符合项说明", false, "manual"),
			},
		},
		{
			ID:        "elevator_no_room",
			Name:      "会议中心电梯巡检（无机房）",
			Project:   "会议中心",
			AssetType: "无机房电梯",
			Fields: []TemplateField{
				textField("inspection_time", "检查时间", true, "manual"),
				textField("asset_no", "电梯编号", true, "manual"),
				textField("inspector", "检查人员", true, "manual"),
				choiceField("registration_mark", "使用登记标志", true, []string{"完好", "缺失", "破损"}),
				choiceField("alarm_device", "报警装置", true, []string{"正常", "异常"}),
				choiceField("door_protection", "门防夹保护装置", true, []string{"正常", "异常"}),
				choiceField("door_running", "层门/轿门运行", true, []string{"正常", "异常"}),
				choiceField("buttons_display", "按钮与显示", true, []string{"正常", "异常"}),
				choiceField("lighting", "轿厢照明", true, []string{"正常", "异常"}),
				choiceField("fire_switch_glass", "消防开关玻璃", true, []string{"完好", "破损", "缺失"}),
				textField("nonconformity", "不符合项处理情况记录", false, "manual"),
			},
			MissingNotes: []string{"需要补充正式电梯编号清单，用于形成稳定资产台账。"},
		},
		{
			ID:        "elevator_machine_room",
			Name:      "会议中心电梯巡检（有机房）",
			Project:   "会议中心",
			AssetType: "有机房电梯",
			Fields: []TemplateField{
				textField("asset_no", "电梯编号", true, "manual"),
				textField("inspection_time", "日期", true, "manual"),
				textField("inspector", "检查人", true, "manual"),
				choiceField("door_window_sign", "机房门窗、警示标识完好", true, []string{"是", "否"}),
				choiceField("room_clean", "机房干净无杂物", true, []string{"是", "否"}),
				choiceField("lighting_ac", "机房照明及空调正常", true, []string{"是", "否"}),
				choiceField("extinguisher_valid", "灭火器材未过期", true, []string{"是", "否"}),
				choiceField("noise_smell", "设备无异响、异味", true, []string{"是", "否"}),
				choiceField("rescue_device", "紧急救援设备装置齐全", true, []string{"是", "否"}),
				textField("nonconformity", "不符合项目处理情况记录", false, "manual"),
			},
			MissingNotes: []string{"当前参考图片较少，建议补充不同电梯编号和异常案例照片。"},
		},
		{
			ID:        "zihan_daily",
			Name:      "紫涵雅集每日综合巡检（员工）",
			Project:   "紫涵雅集",
			AssetType: "综合巡检",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "manual"),
				textField("location", "位置", true, "manual"),
				choiceField("strong_room_01", "强电井巡检01-强电井室内情况", true, []string{"正常", "异常"}),
				textField("strong_room_01_note", "强电井巡检01-备注", false, "manual"),
				choiceField("distribution_box", "强电井巡检02-配电箱情况", true, []string{"正常", "异常"}),
				choiceField("distribution_box_inside", "强电井巡检03-配电箱内部情况", true, []string{"正常", "异常"}),
				choiceField("weak_room", "弱电机房巡检-上述问题是否正常", true, []string{"正常", "异常"}),
				choiceField("fire_pump_room", "消防泵房巡检-上述问题是否正常", true, []string{"正常", "异常"}),
				textField("progress", "项目进度", true, "manual"),
				textField("extra_work", "日常增项汇报01-施工内容说明", false, "manual"),
			},
		},
		{
			ID:        "zihan_energy",
			Name:      "紫涵雅集能耗抄表",
			Project:   "紫涵雅集",
			AssetType: "能耗表",
			Fields: []TemplateField{
				textField("site", "巡检地点", true, "manual"),
				textField("location", "位置", true, "manual"),
				numberField("z1_reading", "Z1能耗表读数", false),
				numberField("z2_reading", "Z2能耗表读数", false),
				numberField("z3_reading", "Z3能耗表读数", false),
				numberField("z4_reading", "Z4能耗表读数", false),
				numberField("living_water_reading", "生活水表读数", false),
				numberField("fire_water_reading", "消防水表读数", false),
			},
			MissingNotes: []string{
				"历史企业微信导出的能耗抄表字段主要是照片，没有独立读数字段；当前先预留读数字段，需由用户确认是否作为日报提交字段。",
			},
		},
	}
}

func textField(code, label string, required bool, source string) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "text", Required: required, Source: source}
}

func numberField(code, label string, required bool) TemplateField {
	return TemplateField{Code: code, Label: label, Kind: "number", Required: required, Source: "ai"}
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

func initialFieldValues(tpl ReportTemplate, inspector string) []FieldValue {
	values := make([]FieldValue, 0, len(tpl.Fields))
	for _, field := range tpl.Fields {
		value := ""
		source := "manual"
		if field.Code == "inspection_time" {
			value = time.Now().Format("2006-01-02 15:04")
		}
		if field.Code == "inspector" {
			value = inspector
		}
		values = append(values, FieldValue{
			Code:        field.Code,
			Label:       field.Label,
			Kind:        field.Kind,
			Required:    field.Required,
			Value:       value,
			AIValue:     "",
			Source:      source,
			Options:     field.Options,
			Confidence:  0,
			NeedsReview: field.Required,
			Version:     1,
		})
	}
	return values
}

func inferOverallStatus(fields []FieldValue) string {
	status := "正常"
	for _, field := range fields {
		value := strings.TrimSpace(field.Value)
		label := field.Label
		if value == "异常" || value == "缺失" || value == "破损" || value == "有" || (value == "是" && strings.Contains(label, "报警")) {
			status = "异常"
		}
		if field.NeedsReview && status != "异常" {
			status = "待复核"
		}
	}
	return status
}
