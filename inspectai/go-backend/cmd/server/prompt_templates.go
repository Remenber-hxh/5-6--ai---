package main

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// ===== 模块化提示词:结构化数据 → 渲染成模型用的提示词文本 =====
//
// 设计目标:把原来一长串自由文本的 .md 提示词,拆成"公共模块(固定) + 模板字段表(可编辑)",
// 后台以表单维护字段表,系统按"判定模式"自动渲染出等价的提示词。
// 本文件先用内存种子打通"数据 + 渲染器",验证渲染结果与现有 .md 等价;
// 后续再接 DB 持久化 + 后台表单 + ai-service 取用(即时生效)。

// 判定模式:每个检查项"怎么判"的固定套路。渲染器按模式展开成标准话术。
const (
	ModeSystem         = "system"          // 系统注入,不识别(日期/检查人)
	ModeReadText       = "read_text"       // 读取文本回填,清晰可读才返回(编号)
	ModeVisual         = "visual"          // 看外观/状态判是否
	ModeVisualLenient  = "visual_lenient"  // 主观项,宽松判定(少量瑕疵不算异常)
	ModeFunctionalTest = "functional_test" // 需现场测试动作照,拍到即按响应判
	ModeSensory        = "sensory"         // 靠听/闻,照片判不了 → 留人工
	ModeObjectiveDate  = "objective_date"  // 读日期与 current_date 比对
	ModeSummary        = "summary"         // 汇总:判否时写明问题(不符合项)
)

// PromptField — 一个检查项的结构化规则(对应后台表单一行)
type PromptField struct {
	Code     string `json:"code"`
	Label    string `json:"label"`
	Group    string `json:"group"`    // 分组:头部 / 机房 / 轿厢层站 / 汇总
	Mode     string `json:"mode"`     // 上面的判定模式
	YesWhen  string `json:"yesWhen"`  // 判"是"看什么
	NoWhen   string `json:"noWhen"`   // 判"否"看什么
	SkipWhen string `json:"skipWhen"` // 什么情况不返回(留人工)
	Note     string `json:"note"`     // 额外提示
}

// PromptCommons — 所有模板共享的公共模块(固定一份)
type PromptCommons struct {
	YesNoSemantics string   // 是/否语义
	GeneralRules   []string // 全局规则
	OutputSchema   string   // 输出 JSON schema 说明
	Confidence     []string // 置信度档位
}

// PromptTemplate — 一个模板(头部 + 字段表)
type PromptTemplate struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	Scene          string        `json:"scene"`          // 场景一句话
	ExpectedPhotos []string      `json:"expectedPhotos"` // 期望拍到哪些照片
	Fields         []PromptField `json:"fields"`
}

// ---------- 公共模块(固定一份,所有模板共享) ----------

func promptCommons() PromptCommons {
	return PromptCommons{
		YesNoSemantics: "选项字段全部是 `[\"是\",\"否\"]`:**是 = 符合要求 / 完好 / 正常**;**否 = 不符合 / 缺失 / 破损 / 异常 / 过期**。",
		GeneralRules: []string{
			"**画面里出现的项一律主动给出\"是/否\",不要为\"求稳\"留空**;明显正常/完好就大胆判\"是\"。",
			"只有照片能明确支持时才给值;真的看不清、没拍到、角度不够 → **不返回该字段**,留人工复核,不要用\"否\"代替\"没拍到\"。",
			"**凡有任何字段判为「否」,必须同时在 `nonconformity` 里写明问题**(哪一项+什么问题+依据,逐条简述),不能只判否却不写说明。",
			"照片**无法感知声音和气味**,涉及异响/异味的项无可见证据时一律不返回。",
			"关键照片缺失可在 `nonconformity` 写\"建议补拍 XX\",但缺照片 ≠ 设备异常。",
		},
		OutputSchema: "严格遵循 `_common.md` 的 JSON schema:`recognitionStatus` / `observations`(按图顺序)/ `recognizedFields`(只放有视觉依据的字段,code 必须完全等于下表,choice 值只能是 `\"是\"`/`\"否\"`)/ `warnings`。",
		Confidence: []string{
			"0.90-0.98:标识/装置/读数清晰、证据充分。",
			"0.70-0.89:可判断但有反光、角度或轻微遮挡,需人工快速复核。",
			"低于 0.70:不返回该字段。",
		},
	}
}

// ---------- 可复用字段组(定义一次,多个模板引用) ----------

// 头部组:日期/编号/检查人 —— 几乎所有模板都用
func fieldGroupHeader() []PromptField {
	return []PromptField{
		{Code: "inspection_time", Label: "日期", Group: "头部", Mode: ModeSystem},
		{Code: "asset_no", Label: "电梯编号", Group: "头部", Mode: ModeReadText,
			YesWhen: "编号牌/《特种设备使用标志》单位内编号清晰可读"},
		{Code: "inspector", Label: "检查人", Group: "头部", Mode: ModeSystem},
	}
}

// 电梯机房组:有机房电梯专属(机房环境 + 设备)
func fieldGroupElevatorMachineRoom() []PromptField {
	return []PromptField{
		{Code: "door_window_sign", Label: "机房门窗、警示标识完好", Group: "机房", Mode: ModeVisual,
			YesWhen: "机房门完好关闭、门上「电梯机房」标识及「危险/禁止入内/禁止攀爬」等警示标识齐全完好",
			NoWhen:  "标识缺失、破损、门损坏", SkipWhen: "未拍到该区域"},
		{Code: "room_clean", Label: "机房干净无杂物", Group: "机房", Mode: ModeVisualLenient,
			YesWhen: "机房地面基本整洁、无明显杂物堆放/积水/油污",
			NoWhen:  "明显堆放杂物/纸箱、积水漏油、大面积脏乱",
			SkipWhen: "未拍到机房地面",
			Note:    "少量灰尘、零星碎屑、脚印、地面线缆、反光、墙面陈旧都不算异常,不要因此判否"},
		{Code: "lighting_ac", Label: "机房照明及空调正常", Group: "机房", Mode: ModeVisual,
			YesWhen: "机房照明明亮,且空调/温控器在运行(亮屏显示温度、制冷标志,室温约 18-27℃)",
			NoWhen:  "明显无照明、空调黑屏/不工作/温度明显异常偏高", SkipWhen: "未拍到该区域"},
		{Code: "extinguisher_valid", Label: "灭火器材未过期", Group: "机房", Mode: ModeObjectiveDate,
			YesWhen: "年检标签/合格证上「有效期」或「下次检验/维修日期」晚于 current_date,且压力表指针在绿区",
			NoWhen:  "有效期早于 current_date(已过期)、压力表指针在红区(欠压/超压)、或检查记录卡长期空缺",
			SkipWhen: "只拍到瓶体、看不清压力表/日期/记录卡",
			Note:    "生产日期 ≠ 有效期,绝不能拿\"生产日期看着不旧\"当未过期依据;只有生产日期且距 current_date 已超 5 年(到强制维修/报废年限)→ 否"},
		{Code: "noise_smell", Label: "设备无异响、异味", Group: "机房", Mode: ModeSensory,
			NoWhen: "能看见曳引机漏油、线缆烧焦痕迹、部件松脱等明确异常"},
		{Code: "rescue_device", Label: "紧急救援设备装置齐全", Group: "机房", Mode: ModeVisual,
			YesWhen: "机房内盘车手轮、松闸扳手、救援说明牌三者齐备且就位",
			NoWhen:  "明显缺失某项", SkipWhen: "未拍到该区域"},
	}
}

// 电梯轿厢/层站组:有机房、无机房共用(轿厢照片完全一样)
func fieldGroupElevatorCar() []PromptField {
	return []PromptField{
		{Code: "reg_mark", Label: "电梯使用登记标志完好", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "《特种设备使用标志》/登记证在位、信息完整清晰、未破损褪色、未过下次检验日期",
			NoWhen:  "缺失/破损/字迹不清/已过下次检验日期", SkipWhen: "未拍到"},
		{Code: "alarm_device", Label: "紧急报警装置有效", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "轿厢操作面板上报警按钮(警铃)/五方对讲(对讲格栅、喇叭孔)在位且外观完好",
			NoWhen:  "装置缺失、按钮破损脱落", SkipWhen: "面板照都没拍到",
			Note: "报警按钮/对讲常在轿厢操作面板上,结合选层按钮那张面板照一起找"},
		{Code: "anti_clip", Label: "轿门防夹人装置有效", Group: "轿厢层站", Mode: ModeFunctionalTest,
			YesWhen: "画面里出现\"手/手臂/物体伸到轿门口或门边、且门保持开启\"(防夹测试的现场动作)",
			NoWhen:  "明显看到门把手/物夹住且不松开",
			SkipWhen: "画面里完全没有\"手/物在门口\"的测试动作",
			Note: "判定从宽:能拍到测试动作+门开着就是通过证据,不要纠结静态照看不到门回弹的瞬间"},
		{Code: "door_smooth", Label: "开关门运行无卡阻", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "轿门/层门门体平整无变形错位、门完全开到位或测试中、地坎/门槽无杂物卡阻",
			NoWhen:  "门体变形/错位/门缝明显不均/卡滞/有杂物卡阻", SkipWhen: "完全没拍到门",
			Note: "和「轿门防夹人」常在同一张门照里,只要拍到了门就一并判定,别漏"},
		{Code: "floor_buttons", Label: "选层按钮及显示正常", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "轿厢操作面板楼层按钮齐全无缺损、楼层显示屏点亮且数字正常",
			NoWhen:  "按钮缺损、显示黑屏/乱码", SkipWhen: "未拍到面板"},
		{Code: "car_lighting", Label: "候梯厅、轿厢内照明正常", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "轿厢内/候梯厅照明点亮、明亮均匀",
			NoWhen:  "明显熄灭、大面积不亮", SkipWhen: "未拍到"},
		{Code: "fire_switch_glass", Label: "消防开关玻璃完好", Group: "轿厢层站", Mode: ModeVisual,
			YesWhen: "消防返回开关的玻璃罩/保护盖完好、未破碎",
			NoWhen:  "玻璃破损/缺失/已击碎", SkipWhen: "未拍到"},
	}
}

// 不符合项汇总(几乎所有模板末尾)
func fieldSummaryNonconformity() PromptField {
	return PromptField{Code: "nonconformity", Label: "不符合项处理情况记录", Group: "汇总", Mode: ModeSummary}
}

// ---------- 模板装配(引用字段组,不重抄) ----------

func promptTemplateSeeds() []PromptTemplate {
	// 有机房电梯 = 头部组 + 机房组 + 轿厢层站组 + 汇总
	elevatorMachineRoom := PromptTemplate{
		ID:    "elevator_machine_room",
		Name:  "电梯巡检（有机房）",
		Scene: "有机房电梯:有独立机房,查机房环境/设备 + 轿厢层站现场。",
		ExpectedPhotos: []string{
			"电梯机房门(挂「电梯机房 / ELEVATOR MACHINE ROOM」标识、防火门)",
			"机房内部(曳引机、控制柜、限速器、盘车手轮/松闸扳手/救援说明牌、温控器、火警电话)",
			"机房地面与环境(是否干净、有无杂物)",
			"轿厢操作面板(楼层按钮、警铃/对讲、消防返回开关)",
			"层站/厅门、特种设备使用标志/登记证、灭火器(压力表、检查记录卡、合格证)",
		},
	}
	elevatorMachineRoom.Fields = concatFields(
		fieldGroupHeader(),
		fieldGroupElevatorMachineRoom(),
		fieldGroupElevatorCar(),
		[]PromptField{fieldSummaryNonconformity()},
	)

	// 无机房电梯 = 头部组 + 轿厢层站组 + 汇总(复用轿厢组,几乎"免费")
	elevatorNoRoom := PromptTemplate{
		ID:    "elevator_no_room",
		Name:  "电梯巡检（无机房）",
		Scene: "无机房电梯:没有独立机房,只查轿厢与层站现场。",
		ExpectedPhotos: []string{
			"轿厢操作面板(楼层按钮/显示屏、警铃/对讲、消防返回开关)",
			"轿厢与候梯厅照明、层门",
			"《特种设备使用标志》/登记证、轿厢编号牌",
		},
	}
	elevatorNoRoom.Fields = concatFields(
		fieldGroupHeader(),
		fieldGroupElevatorCar(),
		[]PromptField{fieldSummaryNonconformity()},
	)

	return []PromptTemplate{elevatorMachineRoom, elevatorNoRoom}
}

func concatFields(groups ...[]PromptField) []PromptField {
	out := []PromptField{}
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

func promptTemplateByID(id string) (PromptTemplate, bool) {
	for _, t := range promptTemplateSeeds() {
		if t.ID == id {
			return t, true
		}
	}
	return PromptTemplate{}, false
}

// ---------- 渲染器:结构化数据 → 提示词文本 ----------

// renderFieldCriteria — 按判定模式把一个字段渲染成"判断依据"话术
func renderFieldCriteria(f PromptField) string {
	yes := f.YesWhen
	no := f.NoWhen
	skip := f.SkipWhen
	note := ""
	if f.Note != "" {
		note = "（注:" + f.Note + "）"
	}
	switch f.Mode {
	case ModeSystem:
		return "系统注入,不返回"
	case ModeReadText:
		return fmt.Sprintf("仅当%s时返回;模糊或推断则不返回", yes)
	case ModeVisual:
		parts := []string{}
		if yes != "" {
			parts = append(parts, yes+" → 是")
		}
		if no != "" {
			parts = append(parts, no+" → 否")
		}
		if skip != "" {
			parts = append(parts, skip+" → 不返回")
		}
		return strings.Join(parts, ";") + note
	case ModeVisualLenient:
		s := fmt.Sprintf("**拍到就判,别犹豫**:%s → 是;只有%s才 → 否;%s → 不返回", yes, no, skip)
		return s + note
	case ModeFunctionalTest:
		return fmt.Sprintf("**判定从宽**:%s → 是;%s → 否;%s → 不返回", yes, no, skip) + note
	case ModeSensory:
		return fmt.Sprintf("照片不能感知声音/气味;仅当%s才返回\"否\",否则不返回,留人工", no)
	case ModeObjectiveDate:
		return fmt.Sprintf("用注入的 `current_date` 比对:%s → 是;%s → 否;%s → 不返回", yes, no, skip) + note
	case ModeSummary:
		return "只要有任何字段判为「否」,必须在此逐条写明问题(哪一项+什么问题+依据);全部正常则不返回"
	}
	return ""
}

// renderPromptFromSeed — 用内存种子渲染(测试/回退用)
func renderPromptFromSeed(id string) (string, bool) {
	t, ok := promptTemplateByID(id)
	if !ok {
		return "", false
	}
	return renderPromptText(t), true
}

// renderPromptViaStore — 优先从 DB 取模板渲染;DB 没有则回退内存种子。
// 后台改了 DB,下一次识别立刻用新规则(即时生效)。
func renderPromptViaStore(store Store, id string) (string, bool) {
	if store != nil {
		if t, ok, err := store.GetPromptTemplate(id); err == nil && ok {
			return renderPromptText(t), true
		}
	}
	return renderPromptFromSeed(id)
}

// ensurePromptTemplateSeeds — 首次启动时把内存种子灌进 DB(已有数据则不动)
func ensurePromptTemplateSeeds(store Store) error {
	existing, err := store.ListPromptTemplates()
	if err != nil {
		return err
	}
	if len(existing) > 0 {
		return nil
	}
	for _, t := range promptTemplateSeeds() {
		if err := store.UpsertPromptTemplate(t); err != nil {
			return err
		}
	}
	return nil
}

// renderPromptText — 把一个模板渲染成完整提示词文本(等价原 .md)
func renderPromptText(t PromptTemplate) string {
	c := promptCommons()
	var b strings.Builder

	b.WriteString(fmt.Sprintf("# %s（视觉合规检查）\n\n", t.Name))
	b.WriteString(fmt.Sprintf("> 适用模板:`%s`(%s)\n", t.ID, t.Name))
	b.WriteString("> " + t.Scene + "\n")
	if len(t.ExpectedPhotos) > 0 {
		b.WriteString("> 期望照片:" + strings.Join(t.ExpectedPhotos, "、") + "\n")
	}
	b.WriteString("> 任务定位:视觉合规检查,只把照片里**能确认的事实**变成字段,不做维修结论,不默认现场正常。\n\n")

	b.WriteString("## 总则\n")
	b.WriteString("- " + c.YesNoSemantics + "\n")
	for _, r := range c.GeneralRules {
		b.WriteString("- " + r + "\n")
	}
	b.WriteString("\n")

	b.WriteString("## 字段映射\n")
	b.WriteString("| code | label | 判断依据 |\n| --- | --- | --- |\n")
	for _, f := range t.Fields {
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s |\n", f.Code, f.Label, renderFieldCriteria(f)))
	}
	b.WriteString("\n")

	b.WriteString("## 输出\n" + c.OutputSchema + "\n\n")
	b.WriteString("## 置信度\n")
	for _, line := range c.Confidence {
		b.WriteString("- " + line + "\n")
	}
	return b.String()
}

// ---------- 判定模式选项(给后台下拉用) ----------

func promptModeOptions() []map[string]string {
	return []map[string]string{
		{"value": ModeSystem, "label": "系统注入(不识别)"},
		{"value": ModeReadText, "label": "读取文本(清晰才返回)"},
		{"value": ModeVisual, "label": "看外观/状态"},
		{"value": ModeVisualLenient, "label": "主观项(宽松判定)"},
		{"value": ModeFunctionalTest, "label": "现场测试照"},
		{"value": ModeSensory, "label": "靠听/闻(留人工)"},
		{"value": ModeObjectiveDate, "label": "查日期(比当前日期)"},
		{"value": ModeSummary, "label": "汇总(不符合项)"},
	}
}

// ---------- 后台接口 ----------

// GET /api/prompt/templates —— 模板列表(给后台选择)
func (s *Server) handleListPromptTemplates(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	tpls, err := s.store.ListPromptTemplates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	sort.Slice(tpls, func(i, j int) bool { return tpls[i].ID < tpls[j].ID })
	list := make([]map[string]any, 0, len(tpls))
	for _, t := range tpls {
		list = append(list, map[string]any{"id": t.ID, "name": t.Name, "fieldCount": len(t.Fields)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"templates": list, "modes": promptModeOptions()})
}

// /api/prompt/templates/{id}        GET 取详情 / PUT 保存
// /api/prompt/templates/{id}/render GET 预览渲染
func (s *Server) handlePromptTemplateRoutes(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/prompt/templates/")
	if rest == "" {
		writeError(w, http.StatusBadRequest, "bad_id", "缺少模板 id")
		return
	}
	if strings.HasSuffix(rest, "/render") {
		id := strings.TrimSuffix(rest, "/render")
		text, ok := renderPromptViaStore(s.store, id)
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "模板不存在")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": id, "prompt": text})
		return
	}
	id := rest
	switch r.Method {
	case http.MethodGet:
		t, ok, err := s.store.GetPromptTemplate(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "get_failed", err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "not_found", "模板不存在")
			return
		}
		writeJSON(w, http.StatusOK, t)
	case http.MethodPut:
		var t PromptTemplate
		if err := decodeJSON(r, &t); err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
		t.ID = id // 以路径 id 为准,防改错
		if strings.TrimSpace(t.Name) == "" {
			writeError(w, http.StatusBadRequest, "bad_name", "模板名不能为空")
			return
		}
		if err := s.store.UpsertPromptTemplate(t); err != nil {
			writeError(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
	default:
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的方法")
	}
}
