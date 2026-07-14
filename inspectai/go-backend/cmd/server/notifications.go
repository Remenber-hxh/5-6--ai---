package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"
)

func (s *Server) notifyInspectionSubmitted(rec *Record, assets []*AssetEntry) {
	if rec == nil || s.weworkBot == nil || !s.weworkBot.Enabled() {
		return
	}
	attention := assetsNeedingAttention(assets)
	if len(attention) == 0 {
		return
	}
	status := worstAssetStatus(attention)
	assetLine := attentionAssetLine(attention)
	advice := firstRecommendationText(rec.AIRecommendations)
	if advice == "" {
		advice = "请主管查看后台记录并完成复核。"
	}
	content := strings.Join([]string{
		"### 智巡异常提醒",
		fmt.Sprintf("> 设备：%s", assetLine),
		fmt.Sprintf("> 点位：%s", firstNonEmpty(rec.PointName, rec.TemplateName, "未填写")),
		fmt.Sprintf("> 巡检人：%s", firstNonEmpty(rec.Inspector, "未填写")),
		fmt.Sprintf("> 状态：%s", status),
		fmt.Sprintf("> AI建议：%s", truncate(advice, 90)),
		fmt.Sprintf("> 处理入口：%s", markdownLink("查看巡检记录", s.adminRecordURL(rec.ID))),
	}, "\n")
	s.sendWeWorkBotMarkdownAsync("inspection.submitted", content)
}

func (s *Server) notifyChangeRequestCreated(cr *ChangeRequest) {
	if cr == nil || s.weworkBot == nil || !s.weworkBot.Enabled() {
		return
	}
	content := strings.Join([]string{
		"### 智巡修改申请",
		fmt.Sprintf("> 申请人：%s", firstNonEmpty(cr.RequestedBy, "未填写")),
		fmt.Sprintf("> 对象：%s", changeTargetLabel(cr.TargetType, cr.TargetID)),
		fmt.Sprintf("> 原因：%s", truncate(cr.Reason, 100)),
		fmt.Sprintf("> 审批入口：%s", markdownLink("进入审批详情", s.adminChangeRequestURL(cr.ID))),
	}, "\n")
	s.sendWeWorkBotMarkdownAsync("change_request.created", content)
}

func (s *Server) notifyChangeRequestReviewed(cr *ChangeRequest, resultText string) {
	if cr == nil || s.weworkBot == nil || !s.weworkBot.Enabled() {
		return
	}
	content := strings.Join([]string{
		"### 智巡处理结果",
		fmt.Sprintf("> 申请人：%s", firstNonEmpty(cr.RequestedBy, "未填写")),
		fmt.Sprintf("> 对象：%s", changeTargetLabel(cr.TargetType, cr.TargetID)),
		fmt.Sprintf("> 结果：%s", resultText),
		fmt.Sprintf("> 处理人：%s", firstNonEmpty(cr.ReviewedBy, "未填写")),
		fmt.Sprintf("> 说明：%s", truncate(firstNonEmpty(cr.ReviewNote, "无"), 100)),
		fmt.Sprintf("> 查看结果：%s", markdownLink("打开审批记录", s.adminChangeRequestURL(cr.ID))),
	}, "\n")
	s.sendWeWorkBotMarkdownAsync("change_request.reviewed", content)
}

func (s *Server) sendWeWorkBotMarkdownAsync(event, content string) {
	if s.weworkBot == nil || !s.weworkBot.Enabled() || strings.TrimSpace(content) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if _, err := s.weworkBot.SendMarkdown(ctx, content); err != nil {
			log.Printf("WARN: wework bot notification failed event=%s err=%v", event, err)
		}
	}()
}

func (s *Server) adminURL() string {
	return s.adminPageURL("", nil)
}

func (s *Server) adminChangeRequestURL(id string) string {
	return s.adminPageURL("approval", map[string]string{"focus": id})
}

func (s *Server) adminRecordURL(id string) string {
	return s.adminPageURL("record", map[string]string{"focus": id})
}

func (s *Server) adminAssetURL(id string) string {
	return s.adminPageURL("ledger", map[string]string{"focus": id})
}

// adminPageURL 生成新版管理后台(admin-web / v2)的深链。
// v2 用 HashRouter,路由形如 https://<host>/v2/#/approval?focus=xxx。
// 旧版 /admin/?page=xxx 保留但不再作为入口。
func (s *Server) adminPageURL(route string, params map[string]string) string {
	base := strings.TrimRight(firstNonEmpty(s.publicBaseURL, "https://jadeast.cloud"), "/")
	route = strings.Trim(strings.TrimSpace(route), "/")
	values := url.Values{}
	for key, value := range params {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key != "" && value != "" {
			values.Set(key, value)
		}
	}
	encoded := values.Encode()
	if route == "" && encoded == "" {
		return base + "/v2/"
	}
	hash := "/" + route
	if encoded != "" {
		hash += "?" + encoded
	}
	return base + "/v2/#" + hash
}

func markdownLink(text, href string) string {
	text = strings.TrimSpace(text)
	href = strings.TrimSpace(href)
	if text == "" {
		text = href
	}
	if href == "" {
		return text
	}
	return fmt.Sprintf("[%s](%s)", strings.ReplaceAll(text, "]", "］"), href)
}

func assetsNeedingAttention(assets []*AssetEntry) []*AssetEntry {
	out := make([]*AssetEntry, 0, len(assets))
	for _, a := range assets {
		if a == nil {
			continue
		}
		level := firstNonEmpty(a.StatusLevel, statusLevel(a.LastStatus))
		if level == "warning" || level == "danger" || level == "repair" {
			out = append(out, a)
		}
	}
	return out
}

func worstAssetStatus(assets []*AssetEntry) string {
	worst := "待复核"
	for _, a := range assets {
		if a == nil {
			continue
		}
		switch firstNonEmpty(a.StatusLevel, statusLevel(a.LastStatus)) {
		case "danger":
			return "异常"
		case "repair":
			worst = "待维修"
		case "warning":
			if worst == "" {
				worst = "待复核"
			}
		}
	}
	return firstNonEmpty(worst, "待复核")
}

func attentionAssetLine(assets []*AssetEntry) string {
	names := make([]string, 0, len(assets))
	for _, a := range assets {
		if a == nil {
			continue
		}
		names = append(names, firstNonEmpty(a.AssetName, a.AssetKey, a.ID))
		if len(names) >= 3 {
			break
		}
	}
	if len(assets) > 3 {
		names = append(names, fmt.Sprintf("等 %d 项", len(assets)))
	}
	return firstNonEmpty(strings.Join(names, "、"), "未识别资产")
}

func firstRecommendationText(items []Recommendation) string {
	for _, item := range items {
		if strings.EqualFold(item.Priority, "high") && strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text)
		}
	}
	for _, item := range items {
		if strings.TrimSpace(item.Text) != "" {
			return strings.TrimSpace(item.Text)
		}
	}
	return ""
}

func changeTargetLabel(targetType, targetID string) string {
	switch targetType {
	case "asset":
		return "资产台账 " + targetID
	case "record":
		return "巡检记录 " + targetID
	default:
		return firstNonEmpty(targetID, "未知对象")
	}
}
