package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// ===== AI 服务真实健康状态 =====
//
// 【为什么单开一个接口,不塞进 /health】/health 是不登录就能打的
// (给探针和运维用)。密钥有没有、账户是不是欠费,这些属于运营信息,
// 不该对着公网摆出来。
//
// 【为什么非做不可】在这之前,系统页那张「AI 视觉 / 问答服务」卡只判断
// aiServiceUrl 非空就显示绿色「已配置」—— 跟能不能用毫无关系。
// 于是出现过这种局面:DeepSeek 账户欠费,管理问答每一句都是兜底文案,
// 而系统页一片绿、聊天窗口也不吭声,只能靠"回答看着不太对"去猜。
// 一个永远显示健康的健康指示灯,比没有指示灯更糟。

type aiHealthResp struct {
	// Reachable ai-service 进程通不通
	Reachable bool `json:"reachable"`
	// Vision / Chat 两条链路分开报 —— 它们用的是两个不同账户,
	// 合成一个"AI 正常吗"会把"拍照识别还能用"这条关键信息抹掉。
	Vision bool `json:"vision"`
	Chat   bool `json:"chat"`
	// Reason 给人看的一句话,空 = 一切正常
	Reason string `json:"reason"`
	URL    string `json:"url"`
}

// aiHealthReason 把上游的英文错误码翻成人话。
//
// 【不直接把原文摆到界面上】"invalid_request_error: Insufficient Balance"
// 对着甲方演示时是减分项,而且看的人未必知道该找谁。
// errCode 取出某个故障对象的错误码。没有故障时返回空串。
//
// 【视觉和问答分开取】accountError 是 DashScope(拍照识别),
// chatError 是 DeepSeek(管理问答)—— 两个不同账户,一个欠费不代表
// 另一个也停了。合成一个的话,DeepSeek 欠费会把"拍照还能用"抹掉。
func errCode(h map[string]any, key string) string {
	e, ok := h[key].(map[string]any)
	if !ok || e == nil {
		return ""
	}
	code, _ := e["code"].(string)
	if code == "" {
		return "error"
	}
	return code
}

// codeToText 错误码 → 人话。
//
// 【不把上游原文摆到界面上】"invalid_request_error: Insufficient Balance"
// 对着甲方演示是减分项,而且看的人未必知道该找谁。
func codeToText(code string) string {
	l := strings.ToLower(code)
	switch {
	case strings.Contains(l, "balance"), strings.Contains(l, "quota"),
		strings.Contains(l, "arrearage"), strings.Contains(l, "allocation"):
		return "账户余额或额度已用尽"
	case strings.Contains(l, "apikey"), strings.Contains(l, "api_key"):
		return "密钥失效"
	case strings.Contains(l, "unpurchased"):
		return "模型未开通"
	case strings.Contains(l, "ratelimit"), strings.Contains(l, "throttling"):
		return "被限流"
	}
	return "账户异常"
}

func aiHealthReason(h map[string]any) string {
	get := func(k string) bool { v, _ := h[k].(bool); return v }
	visionErr := errCode(h, "accountError")
	chatErr := errCode(h, "chatError")

	// 账户级故障优先说 —— key 配了不代表能用,而这一条才是
	// "为什么每次回答都不对"的答案。
	switch {
	case visionErr != "" && chatErr != "":
		return "识别与问答账户均异常(" + codeToText(visionErr) + "),两边都会退化为人工/摘要模式"
	case visionErr != "":
		return "视觉识别" + codeToText(visionErr) + ",拍照只能人工填写"
	case chatErr != "":
		return "管理问答" + codeToText(chatErr) + ",回答会退化为台账摘要"
	}
	switch {
	case !get("hasDashscopeKey") && !get("hasDeepSeekKey"):
		return "视觉识别与管理问答均未配置密钥"
	case !get("hasDashscopeKey"):
		return "视觉识别未配置密钥,拍照只能人工填写"
	case !get("hasDeepSeekKey"):
		return "管理问答未配置密钥,回答会退化为台账摘要"
	}
	return ""
}

// handleAIHealth —— GET /api/system/ai-health
func (s *Server) handleAIHealth(w http.ResponseWriter, r *http.Request) {
	out := aiHealthResp{}
	if s.aiClient != nil {
		out.URL = s.aiClient.baseURL
	}
	if out.URL == "" {
		out.Reason = "未配置 AI 服务地址"
		writeJSON(w, http.StatusOK, out)
		return
	}

	// 【超时压到 5 秒】这是系统页每次打开都要打的一次调用;
	// ai-service 卡住的时候,不能连带把管理后台也拖住。
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(out.URL + "/health")
	if err != nil {
		out.Reason = "AI 服务连不上,识别与问答都不可用"
		writeJSON(w, http.StatusOK, out)
		return
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var h map[string]any
	if resp.StatusCode >= 300 || json.Unmarshal(raw, &h) != nil {
		out.Reason = "AI 服务返回异常,识别与问答可能不可用"
		writeJSON(w, http.StatusOK, out)
		return
	}

	out.Reachable = true
	out.Reason = aiHealthReason(h)
	hasKey := func(k string) bool { v, _ := h[k].(bool); return v }
	// 【两条链路各判各的】视觉走 DashScope、问答走 DeepSeek,是两个账户。
	// 一起判的话,DeepSeek 欠费会把"拍照识别还能用"这条关键信息一起抹掉 ——
	// 而现场最需要知道的恰恰是这个。
	out.Vision = hasKey("hasDashscopeKey") && errCode(h, "accountError") == ""
	out.Chat = hasKey("hasDeepSeekKey") && errCode(h, "chatError") == ""

	writeJSON(w, http.StatusOK, out)
}
