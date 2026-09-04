package main

import (
	"net/http"
	"strconv"
	"strings"
)

// ===== 需求文字 → 字段表 =====
//
// 后台写一段"我要检查什么",AI 拆成检查项。人确认之后才落库。
//
// 【为什么生成的是字段表而不是一整段提示词】
// "字段表 → 标准提示词"的渲染器早就有(renderPromptText,就是那份 skill
// 的代码化:总则/字段映射/输出/置信度)。让模型直接写整段提示词的话,
// 要再解析回字段表才能编辑,而自然语言解析回结构化数据是【有损】的 ——
// 解析不出来的部分会静默丢掉。反过来先出字段表、再渲染成提示词,两边无损,
// 而且人在界面上看到的提示词和模型将来收到的一字不差。
//
// 【AI 提的 code 会变成永久的键,所以必须过一道】字段 code 是记录里
// 字段值的键,有记录之后就再也改不了;而且读数趋势是按 code 归集的 ——
// 同一个"温度"这次叫 temp、下次叫 temperature,趋势就断成两截,还不报错。
// 所以模型只负责"提哪些检查项",code 的合法性和唯一性由这一层兜住。

// draftFieldLimit 一次最多接受多少条。
//
// 模型偶尔会失控吐出几十条。全盘接受的话,现场要拍的照片和要点的选项
// 会多到没人拍得全,最后变成随便点 —— 那比没有这些检查项更糟。
const draftFieldLimit = 30

// normalizeDraftFields 把模型给的原始字段整理成能直接用的样子。
//
// 每一条都做:code 合法化 + 去重、判定模式白名单、单选补上是/否选项。
func normalizeDraftFields(raw []map[string]any) []TemplateField {
	out := make([]TemplateField, 0, len(raw))
	seen := map[string]bool{}
	for i, m := range raw {
		if len(out) >= draftFieldLimit {
			break
		}
		label := strings.TrimSpace(str(m["label"]))
		if label == "" {
			continue // 没有中文名的检查项在表单上是一行空白,留着没意义
		}
		code := strings.ToLower(strings.TrimSpace(str(m["code"])))
		if !tplIDPattern.MatchString(code) || seen[code] {
			// 【生成不出合法 code 也不能丢掉这一条】给一个兜底的,
			// 人在界面上还能改;直接跳过的话,他要的检查项凭空少了一条,
			// 而界面上不会说少了什么。
			code = "field_" + strconv.Itoa(i+1)
			for seen[code] {
				code += "_x"
			}
		}
		seen[code] = true

		kind := strings.TrimSpace(str(m["kind"]))
		if !validFieldKinds[kind] {
			kind = "choice" // 绝大多数检查项是是/否判断
		}
		f := TemplateField{
			Code: code, Label: label, Kind: kind,
			Source:     "ai",
			JudgeMode:  normalizeJudgeMode(str(m["judgeMode"])),
			JudgeGroup: strings.TrimSpace(str(m["group"])),
			YesWhen:    strings.TrimSpace(str(m["yesWhen"])),
			NoWhen:     strings.TrimSpace(str(m["noWhen"])),
			SkipWhen:   strings.TrimSpace(str(m["skipWhen"])),
			JudgeNote:  strings.TrimSpace(str(m["note"])),
		}
		if f.Kind == "choice" {
			// 【选项统一成是/否】全系统的判定语义就是这两个值
			// (是=符合要求,否=不符合),模型自创选项会让识别结果对不上。
			f.Options = []string{"是", "否"}
		}
		if f.Code == assetNoFieldCode {
			// 设备编号是台账认归属的字段:必填、且不让 AI 代填
			f.Required = true
			f.ManualOnly = true
			f.Kind = "text"
		}
		out = append(out, f)
	}
	return ensureDraftSkeleton(out)
}

// normalizeJudgeMode 判定模式只认白名单里的。
//
// 【自创的模式渲染不出话术】渲染器按模式展开成标准句子,认不出的模式
// 会渲染成一条空规则 —— 提示词里那一行只有字段名没有判断依据,
// 模型照跑,结果随机。
func normalizeJudgeMode(v string) string {
	m := strings.TrimSpace(strings.ToLower(v))
	switch m {
	case ModeSystem, ModeReadText, ModeVisual, ModeVisualLenient,
		ModeFunctionalTest, ModeSensory, ModeObjectiveDate, ModeSummary:
		return m
	case "number":
		// 模型常按 kind 填这个。读数类按"看外观/读数"处理最接近。
		return ModeVisual
	default:
		return ModeVisual
	}
}

// ensureDraftSkeleton 补上两条骨架字段。
//
// 【这两条不是可选项】
//   - 没有设备编号,提交的记录挂不到任何设备上
//   - 没有不符合项汇总,判"否"的时候没地方写清楚问题是什么
//
// 提示词里已经要求模型带上它们,但要求 ≠ 保证 —— 模型漏了的话,
// 人要到第一次提交失败时才发现,而那时候他已经把整个模板配完了。
func ensureDraftSkeleton(fields []TemplateField) []TemplateField {
	has := map[string]bool{}
	for _, f := range fields {
		has[f.Code] = true
	}
	if !has[assetNoFieldCode] {
		fields = append([]TemplateField{{
			Code: assetNoFieldCode, Label: "设备编号", Kind: "text",
			Required: true, Source: "manual", ManualOnly: true,
			JudgeMode: ModeReadText, YesWhen: "编号牌/铭牌上的编号清晰可读",
		}}, fields...)
	}
	if !has["nonconformity"] {
		fields = append(fields, TemplateField{
			Code: "nonconformity", Label: "不符合项处理情况记录", Kind: "text",
			Source: "ai", JudgeMode: ModeSummary,
		})
	}
	return fields
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// handleDraftTemplateFields —— POST /api/report/templates/draft
//
// 只生成、不保存。人看过之后再决定采不采用 ——
// 直接落库的话,一次手滑的需求描述会把整份字段表冲掉。
func (s *Server) handleDraftTemplateFields(w http.ResponseWriter, r *http.Request) {
	if !s.requirePermission(w, r, "template_manage") {
		return
	}
	var req struct {
		Requirement  string `json:"requirement"`
		TemplateName string `json:"templateName"`
		AssetType    string `json:"assetType"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Requirement) == "" {
		writeError(w, http.StatusBadRequest, "empty_requirement", "请先写一段需求描述")
		return
	}
	if s.aiClient == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "AI 服务未配置")
		return
	}

	raw, model, err := s.aiClient.DraftFields(
		req.Requirement, req.TemplateName, req.AssetType)
	if err != nil {
		// ai-service 那边的理由已经是人话(没配密钥 / 需求太含糊 / 账户欠费),
		// 直接给出来 —— 换成"生成失败"人不知道该改什么。
		writeError(w, http.StatusBadGateway, "draft_failed", err.Error())
		return
	}
	fields := normalizeDraftFields(raw)
	if len(fields) <= 2 {
		// 只剩骨架那两条 = 模型什么也没提出来。说实话,别让人以为生成成功了。
		writeError(w, http.StatusBadGateway, "draft_empty",
			"没能从这段描述里拆出检查项,把要检查什么写得具体些再试")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"fields": fields, "model": model})
}
