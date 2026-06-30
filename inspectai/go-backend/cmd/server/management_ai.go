package main

// 管理 AI(DeepSeek)的「后端工具层」实现:
//   - risk_score 公式可解释,即便 DeepSeek 挂了首页和看板仍能展示规则版重点关注
//   - 8 个白名单只读工具,后端聚合后由模型按需调用
// 阶段一只用 risk_score 排重点关注 + 给数据看板提供聚合数字,DeepSeek 工具调用阶段二接

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ===== 时间范围 =====

func parseRangeDays(key string) int {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "1d", "today", "day":
		return 1
	case "7d", "week":
		return 7
	case "90d", "season":
		return 90
	case "180d":
		return 180
	case "365d", "year":
		return 365
	case "30d", "month", "":
		return 30
	}
	if n, err := strconv.Atoi(strings.TrimSuffix(strings.ToLower(key), "d")); err == nil && n > 0 {
		return n
	}
	return 30
}

func rangeWindow(key string, now time.Time) (start, end time.Time, days int) {
	days = parseRangeDays(key)
	end = now
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "1d", "today", "day":
		// 日报口径:今日 00:00 ~ 现在(prev 自动落到昨天整日)
		y, m, d := now.Date()
		start = time.Date(y, m, d, 0, 0, 0, 0, now.Location())
		days = 1
		return
	}
	start = end.AddDate(0, 0, -days)
	return
}

// ===== 上下文:一次性把跨资产共享的数据捞出来,避免每个资产重复查表 =====

type insightsContext struct {
	now            time.Time
	rangeKey       string
	rangeStart     time.Time
	rangeEnd       time.Time
	prevStart      time.Time
	prevEnd        time.Time
	project        string
	assets         []*AssetEntry
	records        []*Record
	recordsByID    map[string]*Record
	confirmRecent  []*FieldConfirmLog
	pendingApprovals int
}

func (s *Server) buildInsightsContext(project, rangeKey string) (*insightsContext, error) {
	now := time.Now()
	start, end, days := rangeWindow(rangeKey, now)
	assets, err := s.store.ListAssets()
	if err != nil {
		return nil, err
	}
	records, err := s.store.ListRecords(2000)
	if err != nil {
		return nil, err
	}
	if project != "" {
		assets = filterAssetsByProject(assets, project)
		records = filterRecordsByProject(records, project)
	}
	recBy := make(map[string]*Record, len(records))
	for _, r := range records {
		if r != nil {
			recBy[r.ID] = r
		}
	}
	confirmLogs, _ := s.store.ListRecentFieldConfirmLogs(2000)
	if project != "" {
		confirmLogs = filterConfirmLogsByRecords(confirmLogs, recBy)
	}
	requests, _ := s.store.ListChangeRequests(ChangeRequestFilter{Status: "pending", Limit: 500})
	return &insightsContext{
		now: now, rangeKey: rangeKey,
		rangeStart: start, rangeEnd: end,
		prevStart: start.AddDate(0, 0, -days),
		prevEnd:   start,
		project:   project,
		assets:    assets,
		records:   records,
		recordsByID: recBy,
		confirmRecent: confirmLogs,
		pendingApprovals: len(requests),
	}, nil
}

func filterAssetsByProject(in []*AssetEntry, project string) []*AssetEntry {
	out := make([]*AssetEntry, 0, len(in))
	for _, a := range in {
		if a != nil && a.Project == project {
			out = append(out, a)
		}
	}
	return out
}

func filterRecordsByProject(in []*Record, project string) []*Record {
	out := make([]*Record, 0, len(in))
	for _, r := range in {
		if r != nil && r.Project == project {
			out = append(out, r)
		}
	}
	return out
}

func filterConfirmLogsByRecords(in []*FieldConfirmLog, recBy map[string]*Record) []*FieldConfirmLog {
	out := make([]*FieldConfirmLog, 0, len(in))
	for _, e := range in {
		if _, ok := recBy[e.RecordID]; ok {
			out = append(out, e)
		}
	}
	return out
}

// ===== Tool 1: get_overview =====

func (s *Server) toolGetOverview(project, rangeKey string) (*OverviewSummary, error) {
	ctx, err := s.buildInsightsContext(project, rangeKey)
	if err != nil {
		return nil, err
	}
	out := &OverviewSummary{
		AssetTotal:       len(ctx.assets),
		RecordTotal:      len(ctx.records),
		RangeKey:         ctx.rangeKey,
		RangeStart:       ctx.rangeStart.Format(time.RFC3339),
		RangeEnd:         ctx.rangeEnd.Format(time.RFC3339),
		PendingApprovals: ctx.pendingApprovals,
	}
	for _, a := range ctx.assets {
		switch normalizeAssetStatusLevel(a) {
		case "normal":
			out.AssetNormal++
		case "warning":
			out.AssetWarning++
		case "danger":
			out.AssetDanger++
		}
	}
	for _, r := range ctx.records {
		t := recordTimestamp(r)
		if !t.Before(ctx.rangeStart) && !t.After(ctx.rangeEnd) {
			out.RecordRecent++
			if recordIsAbnormal(r) {
				out.AbnormalRecent++
			}
			if r.RecognitionStatus == "manual_required" || hasNeedsReview(r) {
				out.PendingReviews++
			}
		} else if !t.Before(ctx.prevStart) && t.Before(ctx.prevEnd) {
			out.RecordPrev++
			if recordIsAbnormal(r) {
				out.AbnormalPrev++
			}
		}
	}
	// 字段漂移项数:跨所有资产、所有数值字段,看变化率绝对值是否 > 10%
	out.DriftFieldCount = s.countDriftFields(ctx)
	// 复核惰性:未看图就 confirm/correct 的占比
	out.LazyConfirmRate = lazyConfirmRate(ctx.confirmRecent)
	return out, nil
}

func normalizeAssetStatusLevel(a *AssetEntry) string {
	switch a.StatusLevel {
	case "normal", "warning", "danger", "repair":
		return a.StatusLevel
	}
	switch a.LastStatus {
	case "正常":
		return "normal"
	case "异常":
		return "danger"
	case "待复核":
		return "warning"
	case "待维修":
		return "repair"
	}
	return "unknown"
}

func recordTimestamp(r *Record) time.Time {
	if r.SubmittedAt != nil && !r.SubmittedAt.IsZero() {
		return *r.SubmittedAt
	}
	if !r.UpdatedAt.IsZero() {
		return r.UpdatedAt
	}
	return r.CreatedAt
}

var abnormalValueRE = stringMatcher{
	"异常", "告警", "故障", "离线", "不合格", "超标", "漏水", "渗漏",
	"报警", "破损", "损坏", "缺失", "跳闸", "烧毁",
}

type stringMatcher []string

func (m stringMatcher) Match(s string) bool {
	for _, kw := range m {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func recordIsAbnormal(r *Record) bool {
	for _, f := range r.Fields {
		if abnormalValueRE.Match(f.Value) {
			return true
		}
	}
	return false
}

func hasNeedsReview(r *Record) bool {
	for _, f := range r.Fields {
		if f.NeedsReview {
			return true
		}
	}
	return false
}

func lazyConfirmRate(logs []*FieldConfirmLog) float64 {
	if len(logs) == 0 {
		return 0
	}
	confirms := 0
	noPhoto := 0
	for _, e := range logs {
		if e.Action == "confirm" || e.Action == "correct" {
			confirms++
			if !e.ViewedPhoto {
				noPhoto++
			}
		}
	}
	if confirms == 0 {
		return 0
	}
	return float64(noPhoto) / float64(confirms)
}

// countDriftFields — 数值字段近 30d 最后两次的变化率 |Δ| > 10% 计一次。
func (s *Server) countDriftFields(ctx *insightsContext) int {
	cnt := 0
	for _, a := range ctx.assets {
		obs, err := s.store.ListFieldObservations(a.ID, "", 200)
		if err != nil || len(obs) == 0 {
			continue
		}
		// 按 fieldKey 分组,只看数值
		byKey := map[string][]*FieldObservation{}
		for _, o := range obs {
			if o.ValueNumber == nil {
				continue
			}
			if o.CreatedAt.Before(ctx.rangeStart) {
				continue
			}
			byKey[o.FieldKey] = append(byKey[o.FieldKey], o)
		}
		for _, list := range byKey {
			if len(list) < 2 {
				continue
			}
			sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
			prev := *list[len(list)-2].ValueNumber
			cur := *list[len(list)-1].ValueNumber
			if prev == 0 {
				continue
			}
			rate := (cur - prev) / prev
			if rate < 0 {
				rate = -rate
			}
			if rate > 0.10 {
				cnt++
			}
		}
	}
	return cnt
}

// ===== Tool 1b: 全资产数值字段漂移明细(看板 ⑤ 字段漂移看板用)=====

type NumericDriftEntry struct {
	AssetID       string  `json:"assetId"`
	AssetName     string  `json:"assetName"`
	FieldKey      string  `json:"fieldKey"`
	FieldLabel    string  `json:"fieldLabel"`
	Current       float64 `json:"current"`
	Previous      float64 `json:"previous"`
	ChangeRate    float64 `json:"changeRate"`
	OverThreshold bool    `json:"overThreshold"`
}

func (s *Server) toolListNumericDrift(project string) ([]*NumericDriftEntry, error) {
	ctx, err := s.buildInsightsContext(project, "30d")
	if err != nil {
		return nil, err
	}
	out := []*NumericDriftEntry{}
	for _, a := range ctx.assets {
		obs, err := s.store.ListFieldObservations(a.ID, "", 200)
		if err != nil || len(obs) == 0 {
			continue
		}
		byKey := map[string][]*FieldObservation{}
		for _, o := range obs {
			if o.ValueNumber == nil || o.CreatedAt.Before(ctx.rangeStart) {
				continue
			}
			byKey[o.FieldKey] = append(byKey[o.FieldKey], o)
		}
		for key, list := range byKey {
			if len(list) < 2 {
				continue
			}
			sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
			prev := *list[len(list)-2].ValueNumber
			cur := *list[len(list)-1].ValueNumber
			if prev == 0 {
				continue
			}
			rate := (cur - prev) / prev
			abs := rate
			if abs < 0 {
				abs = -abs
			}
			out = append(out, &NumericDriftEntry{
				AssetID: a.ID, AssetName: a.AssetName,
				FieldKey: key, FieldLabel: list[0].FieldLabel,
				Current: cur, Previous: prev, ChangeRate: rate,
				OverThreshold: abs > 0.10,
			})
		}
	}
	// 按变化率绝对值降序
	sort.SliceStable(out, func(i, j int) bool {
		ai := out[i].ChangeRate
		aj := out[j].ChangeRate
		if ai < 0 {
			ai = -ai
		}
		if aj < 0 {
			aj = -aj
		}
		return ai > aj
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out, nil
}

// ===== Tool 2: list_attention_assets (按 risk_score 排序的 Top N) =====

const (
	pointStateDanger      = 60
	pointStateWarning     = 35
	pointRepeatAbnormal   = 12
	pointSameFieldRepeat  = 15
	pointRetake           = 8
	pointUncertain        = 8
	pointAIOverwrite      = 5
	pointDrift            = 20
	pointMissedSchedule   = 25
)

func (s *Server) toolListAttention(project string, limit int) ([]*AttentionItem, error) {
	if limit <= 0 {
		limit = 5
	}
	ctx, err := s.buildInsightsContext(project, "30d")
	if err != nil {
		return nil, err
	}
	items := make([]*AttentionItem, 0, len(ctx.assets))
	for _, a := range ctx.assets {
		it := s.computeAttentionForAsset(a, ctx)
		if it == nil {
			continue
		}
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].RiskScore > items[j].RiskScore })
	if len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func (s *Server) computeAttentionForAsset(a *AssetEntry, ctx *insightsContext) *AttentionItem {
	score := 0
	reasons := []string{}
	evidence := []ItemRef{}

	level := normalizeAssetStatusLevel(a)
	switch level {
	case "danger":
		score += pointStateDanger
		reasons = append(reasons, "当前资产状态:异常")
	case "warning":
		score += pointStateWarning
		reasons = append(reasons, "当前资产状态:待复核")
	}

	// 历史快照:近 30d 异常次数 / 重复字段
	snaps, _ := s.store.ListAssetSnapshots(a.ID, 100, 0)
	abnormalCnt := 0
	fieldAbnormal := map[string]int{}
	for _, sn := range snaps {
		if sn.CreatedAt.Before(ctx.rangeStart) {
			continue
		}
		if sn.StatusLevel == "danger" || sn.StatusLevel == "warning" {
			abnormalCnt++
			// 翻 record 找当时哪些字段异常,做"同字段重复"加分
			if rec, ok := ctx.recordsByID[sn.RecordID]; ok {
				for _, f := range rec.Fields {
					if abnormalValueRE.Match(f.Value) {
						fieldAbnormal[f.Code]++
					}
				}
			}
		}
	}
	if abnormalCnt > 0 {
		score += abnormalCnt * pointRepeatAbnormal
		reasons = append(reasons, "近 30 天异常 "+strconv.Itoa(abnormalCnt)+" 次")
	}
	for fk, c := range fieldAbnormal {
		if c >= 2 {
			score += c * pointSameFieldRepeat
			reasons = append(reasons, "同一字段 "+fk+" 重复异常 "+strconv.Itoa(c)+" 次")
		}
	}

	// 字段漂移
	driftCount := s.countAssetDrift(a.ID, ctx.rangeStart)
	if driftCount > 0 {
		score += driftCount * pointDrift
		reasons = append(reasons, "数值字段超阈漂移 "+strconv.Itoa(driftCount)+" 项")
	}

	// 复核留痕:补拍/无判/AI 覆盖
	retake, uncertain, overwritten := 0, 0, 0
	for _, e := range ctx.confirmRecent {
		rec, ok := ctx.recordsByID[e.RecordID]
		if !ok || !recordTouchesAsset(rec, a) {
			continue
		}
		switch e.Action {
		case "uncertain":
			uncertain++
		case "correct":
			overwritten++
		}
	}
	for _, r := range ctx.records {
		if !recordTouchesAsset(r, a) {
			continue
		}
		if r.RecognitionStatus == "retake_required" {
			retake++
		}
	}
	if retake > 0 {
		score += retake * pointRetake
		reasons = append(reasons, "近 30 天补拍 "+strconv.Itoa(retake)+" 次")
	}
	if uncertain > 0 {
		score += uncertain * pointUncertain
		reasons = append(reasons, "人工无法判定 "+strconv.Itoa(uncertain)+" 次")
	}
	if overwritten > 0 {
		score += overwritten * pointAIOverwrite
		reasons = append(reasons, "人工修正 AI 识别 "+strconv.Itoa(overwritten)+" 次")
	}

	// 超过 7d 未巡
	if !a.LastInspectedAt.IsZero() && ctx.now.Sub(a.LastInspectedAt) > 7*24*time.Hour {
		score += pointMissedSchedule
		reasons = append(reasons, "超过 7 天未巡检")
	}

	if score < 5 {
		return nil
	}
	if len(snaps) > 0 {
		evidence = append(evidence, ItemRef{
			Type: "record", ID: snaps[0].RecordID,
			Label: "最近一次巡检", Time: snaps[0].CreatedAt.Format("2006-01-02 15:04"),
		})
	}
	riskLevel := "normal"
	if score >= 60 {
		riskLevel = "danger"
	} else if score >= 25 {
		riskLevel = "warning"
	}
	title := a.AssetName
	if a.LastStatus != "" && a.LastStatus != "未巡检" {
		title = a.AssetName + " · " + a.LastStatus
	}
	return &AttentionItem{
		AssetID:     a.ID,
		AssetName:   a.AssetName,
		AssetType:   a.AssetType,
		Project:     a.Project,
		RiskScore:   score,
		RiskLevel:   riskLevel,
		Title:       title,
		Reasons:     reasons,
		Action:      suggestActionFor(a, reasons),
		LastRecordID: a.LastRecordID,
		LastInspectedAt: a.LastInspectedAt.Format("2006-01-02 15:04"),
		Evidence:    evidence,
	}
}

func (s *Server) countAssetDrift(assetID string, since time.Time) int {
	obs, err := s.store.ListFieldObservations(assetID, "", 200)
	if err != nil || len(obs) == 0 {
		return 0
	}
	byKey := map[string][]*FieldObservation{}
	for _, o := range obs {
		if o.ValueNumber == nil || o.CreatedAt.Before(since) {
			continue
		}
		byKey[o.FieldKey] = append(byKey[o.FieldKey], o)
	}
	cnt := 0
	for _, list := range byKey {
		if len(list) < 2 {
			continue
		}
		sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
		prev := *list[len(list)-2].ValueNumber
		cur := *list[len(list)-1].ValueNumber
		if prev == 0 {
			continue
		}
		rate := (cur - prev) / prev
		if rate < 0 {
			rate = -rate
		}
		if rate > 0.10 {
			cnt++
		}
	}
	return cnt
}

func suggestActionFor(a *AssetEntry, reasons []string) string {
	for _, r := range reasons {
		if strings.Contains(r, "未巡") {
			return "尽快安排下一次巡检"
		}
		if strings.Contains(r, "异常") || strings.Contains(r, "待复核") {
			return "排查异常字段并补拍现场照片"
		}
		if strings.Contains(r, "漂移") {
			return "复核数值字段,确认设备运行参数"
		}
	}
	return "下次巡检重点关注"
}

// ===== Tool 3: get_asset_history (复用 §3 ListAssetSnapshots) =====

func (s *Server) toolGetAssetHistory(assetID string, limit int) ([]*AssetSnapshot, error) {
	if limit <= 0 {
		limit = 20
	}
	return s.store.ListAssetSnapshots(assetID, limit, 0)
}

// ===== Tool 4: compare_asset_periods =====

type PeriodComparison struct {
	AssetID         string `json:"assetId"`
	CurrentRange    string `json:"currentRange"`
	PreviousRange   string `json:"previousRange"`
	CurrentTotal    int    `json:"currentTotal"`
	PreviousTotal   int    `json:"previousTotal"`
	CurrentAbnormal int    `json:"currentAbnormal"`
	PreviousAbnormal int   `json:"previousAbnormal"`
	Trend           string `json:"trend"` // up / down / flat
}

func (s *Server) toolCompareAssetPeriods(assetID, current, previous string) (*PeriodComparison, error) {
	now := time.Now()
	curDays := parseRangeDays(current)
	prevDays := parseRangeDays(previous)
	if prevDays == 0 {
		prevDays = curDays
	}
	curStart := now.AddDate(0, 0, -curDays)
	prevStart := curStart.AddDate(0, 0, -prevDays)
	snaps, err := s.store.ListAssetSnapshots(assetID, 200, 0)
	if err != nil {
		return nil, err
	}
	out := &PeriodComparison{AssetID: assetID, CurrentRange: current, PreviousRange: previous}
	for _, sn := range snaps {
		switch {
		case !sn.CreatedAt.Before(curStart):
			out.CurrentTotal++
			if sn.StatusLevel == "danger" || sn.StatusLevel == "warning" {
				out.CurrentAbnormal++
			}
		case !sn.CreatedAt.Before(prevStart) && sn.CreatedAt.Before(curStart):
			out.PreviousTotal++
			if sn.StatusLevel == "danger" || sn.StatusLevel == "warning" {
				out.PreviousAbnormal++
			}
		}
	}
	switch {
	case out.CurrentAbnormal > out.PreviousAbnormal:
		out.Trend = "up"
	case out.CurrentAbnormal < out.PreviousAbnormal:
		out.Trend = "down"
	default:
		out.Trend = "flat"
	}
	return out, nil
}

// ===== Tool 5: list_repeated_issues =====

func (s *Server) toolListRepeatedIssues(project string, limit int) ([]*RepeatedIssue, error) {
	if limit <= 0 {
		limit = 10
	}
	ctx, err := s.buildInsightsContext(project, "30d")
	if err != nil {
		return nil, err
	}
	type key struct {
		AssetID, AssetName, FieldKey, FieldLabel string
	}
	bucket := map[key]*RepeatedIssue{}
	for _, a := range ctx.assets {
		snaps, _ := s.store.ListAssetSnapshots(a.ID, 100, 0)
		for _, sn := range snaps {
			if sn.CreatedAt.Before(ctx.rangeStart) {
				continue
			}
			rec, ok := ctx.recordsByID[sn.RecordID]
			if !ok {
				continue
			}
			for _, f := range rec.Fields {
				if !abnormalValueRE.Match(f.Value) {
					continue
				}
				k := key{a.ID, a.AssetName, f.Code, f.Label}
				item, exists := bucket[k]
				if !exists {
					item = &RepeatedIssue{
						AssetID: a.ID, AssetName: a.AssetName,
						FieldKey: f.Code, FieldLabel: f.Label,
						Issue: f.Label + " 异常",
					}
					bucket[k] = item
				}
				item.Count++
				t := sn.CreatedAt.Format("2006-01-02 15:04")
				if t > item.LastTime {
					item.LastTime = t
				}
			}
		}
	}
	out := make([]*RepeatedIssue, 0, len(bucket))
	for _, it := range bucket {
		if it.Count >= 2 { // 至少 2 次才算重复
			out = append(out, it)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Count > out[j].Count })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ===== Tool 6: list_pending_reviews =====

type PendingReviews struct {
	NeedsReview      []map[string]any `json:"needsReview"`
	PendingApprovals []map[string]any `json:"pendingApprovals"`
}

func (s *Server) toolListPendingReviews(project string, limit int) (*PendingReviews, error) {
	if limit <= 0 {
		limit = 20
	}
	ctx, err := s.buildInsightsContext(project, "30d")
	if err != nil {
		return nil, err
	}
	out := &PendingReviews{}
	for _, r := range ctx.records {
		if r.Submitted {
			continue
		}
		if r.RecognitionStatus == "manual_required" || hasNeedsReview(r) {
			out.NeedsReview = append(out.NeedsReview, map[string]any{
				"recordId":  r.ID,
				"pointName": r.PointName,
				"inspector": r.Inspector,
				"status":    r.RecognitionStatus,
				"createdAt": r.CreatedAt.Format("2006-01-02 15:04"),
			})
		}
		if len(out.NeedsReview) >= limit {
			break
		}
	}
	requests, _ := s.store.ListChangeRequests(ChangeRequestFilter{Status: "pending", Limit: limit})
	for _, cr := range requests {
		out.PendingApprovals = append(out.PendingApprovals, map[string]any{
			"id":          cr.ID,
			"targetType":  cr.TargetType,
			"targetId":    cr.TargetID,
			"reason":      cr.Reason,
			"requestedBy": cr.RequestedBy,
			"requestedAt": cr.RequestedAt.Format("2006-01-02 15:04"),
		})
	}
	return out, nil
}

// ===== Tool 7: get_inspector_quality =====

func (s *Server) toolGetInspectorQuality(rangeKey string) ([]*InspectorQualityRow, error) {
	ctx, err := s.buildInsightsContext("", rangeKey)
	if err != nil {
		return nil, err
	}
	type acc struct {
		row *InspectorQualityRow
		durSum int
	}
	by := map[string]*acc{}
	get := func(op string) *acc {
		a, ok := by[op]
		if !ok {
			a = &acc{row: &InspectorQualityRow{Operator: op}}
			by[op] = a
		}
		return a
	}
	for _, e := range ctx.confirmRecent {
		if e.CreatedAt.Before(ctx.rangeStart) {
			continue
		}
		op := e.Operator
		if op == "" {
			op = "未知"
		}
		a := get(op)
		a.row.Total++
		switch e.Action {
		case "confirm", "correct":
			if !e.ViewedPhoto {
				a.row.NoPhotoConfirm++
			}
			if e.DurationMs > 0 && e.DurationMs < 2000 {
				a.row.FastConfirmCount++
			}
		case "uncertain":
			a.row.UncertainCount++
		}
		if e.DurationMs > 0 {
			a.durSum += e.DurationMs
		}
	}
	// 补拍次数:从记录里数
	for _, r := range ctx.records {
		if r.RecognitionStatus != "retake_required" {
			continue
		}
		op := r.Inspector
		if op == "" {
			op = "未知"
		}
		a := get(op)
		a.row.RetakeCount++
	}
	out := make([]*InspectorQualityRow, 0, len(by))
	for _, a := range by {
		if a.row.Total > 0 {
			a.row.AvgDurationMs = a.durSum / a.row.Total
		}
		out = append(out, a.row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].NoPhotoConfirm+out[i].FastConfirmCount >
			out[j].NoPhotoConfirm+out[j].FastConfirmCount
	})
	return out, nil
}

// ===== Tool 8+: get_status_events (电梯类资产专用,字段都是 choice 时数值趋势空白)=====

type StatusEventStat struct {
	AssetID         string             `json:"assetId"`
	RangeKey        string             `json:"rangeKey"`
	Inspections     int                `json:"inspections"`
	Normal          int                `json:"normal"`
	Warning         int                `json:"warning"`
	Danger          int                `json:"danger"`
	RetakeCount     int                `json:"retakeCount"`
	UncertainCount  int                `json:"uncertainCount"`
	NoPhotoConfirm  int                `json:"noPhotoConfirm"`
	RepeatFields    []FieldFreqEntry   `json:"repeatFields"`    // 重复异常字段 Top
	LastInspection  string             `json:"lastInspection,omitempty"`
}

type FieldFreqEntry struct {
	FieldKey   string `json:"fieldKey"`
	FieldLabel string `json:"fieldLabel"`
	Count      int    `json:"count"`
}

func (s *Server) toolGetStatusEvents(assetID, rangeKey string) (*StatusEventStat, error) {
	asset, err := s.store.GetAsset(assetID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	start, _, _ := rangeWindow(rangeKey, now)
	out := &StatusEventStat{AssetID: assetID, RangeKey: rangeKey}

	snaps, _ := s.store.ListAssetSnapshots(assetID, 200, 0)
	fieldFreq := map[string]*FieldFreqEntry{}
	for _, sn := range snaps {
		if sn.CreatedAt.Before(start) {
			continue
		}
		out.Inspections++
		switch sn.StatusLevel {
		case "danger":
			out.Danger++
		case "warning":
			out.Warning++
		case "normal":
			out.Normal++
		}
		if out.LastInspection == "" || sn.CreatedAt.Format("2006-01-02 15:04") > out.LastInspection {
			out.LastInspection = sn.CreatedAt.Format("2006-01-02 15:04")
		}
		// 翻 record 找异常字段做"重复异常"计数
		rec, err := s.store.GetRecord(sn.RecordID)
		if err != nil || rec == nil {
			continue
		}
		for _, f := range rec.Fields {
			if !abnormalValueRE.Match(f.Value) {
				continue
			}
			ent, ok := fieldFreq[f.Code]
			if !ok {
				ent = &FieldFreqEntry{FieldKey: f.Code, FieldLabel: f.Label}
				fieldFreq[f.Code] = ent
			}
			ent.Count++
		}
	}

	// 跨记录数补拍/无法判定/未看图次数
	all, _ := s.store.ListRecords(2000)
	for _, r := range all {
		if r == nil || !recordTouchesAsset(r, asset) {
			continue
		}
		if recordTimestamp(r).Before(start) {
			continue
		}
		if r.RecognitionStatus == "retake_required" {
			out.RetakeCount++
		}
	}
	confirmLogs, _ := s.store.ListRecentFieldConfirmLogs(2000)
	recBy := map[string]*Record{}
	for _, r := range all {
		if r != nil {
			recBy[r.ID] = r
		}
	}
	for _, e := range confirmLogs {
		if e.CreatedAt.Before(start) {
			continue
		}
		rec, ok := recBy[e.RecordID]
		if !ok || !recordTouchesAsset(rec, asset) {
			continue
		}
		switch e.Action {
		case "uncertain":
			out.UncertainCount++
		case "confirm", "correct", "confirm-batch":
			if !e.ViewedPhoto {
				out.NoPhotoConfirm++
			}
		}
	}

	// Top 5 重复异常字段
	rep := make([]FieldFreqEntry, 0, len(fieldFreq))
	for _, e := range fieldFreq {
		if e.Count >= 1 {
			rep = append(rep, *e)
		}
	}
	sort.SliceStable(rep, func(i, j int) bool { return rep[i].Count > rep[j].Count })
	if len(rep) > 5 {
		rep = rep[:5]
	}
	out.RepeatFields = rep
	return out, nil
}

// ===== Tool 8: get_record_detail =====

func (s *Server) toolGetRecordDetail(recordID string) (map[string]any, error) {
	rec, err := s.store.GetRecord(recordID)
	if err != nil {
		return nil, err
	}
	clean := sanitizeRecordForCurrentTemplate(rec)
	logs, _ := s.store.ListFieldConfirmLogs(recordID)
	return map[string]any{
		"record":      clean,
		"confirmLogs": logs,
	}, nil
}

// ===== AnalyticsClient — 转发到 ai-service /management/* =====

type AnalyticsClient struct {
	baseURL string
	http    *http.Client
}

func NewAnalyticsClient(baseURL string) *AnalyticsClient {
	return &AnalyticsClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *AnalyticsClient) Chat(payload map[string]any) (map[string]any, error) {
	return c.post("/management/chat", payload)
}

func (c *AnalyticsClient) Analyze(payload map[string]any) (map[string]any, error) {
	return c.post("/management/analyze", payload)
}

func (c *AnalyticsClient) post(path string, payload map[string]any) (map[string]any, error) {
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("analytics %s status %d: %s", path, resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode analytics %s: %w", path, err)
	}
	return out, nil
}

// ===== HTTP handlers =====

// GET /api/management-ai/snapshot?project=&range=30d
// 看板用的聚合数字,不调 AI,纯后端规则。AI 挂了首页和看板照样能展示。
func (s *Server) handleManagementSnapshot(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	project := r.URL.Query().Get("project")
	rangeKey := firstNonEmpty(r.URL.Query().Get("range"), "30d")
	overview, err := s.toolGetOverview(project, rangeKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "overview_failed", err.Error())
		return
	}
	repeated, _ := s.toolListRepeatedIssues(project, 10)
	quality, _ := s.toolGetInspectorQuality(rangeKey)
	pending, _ := s.toolListPendingReviews(project, 20)
	numericDrifts, _ := s.toolListNumericDrift(project)
	writeJSON(w, http.StatusOK, map[string]any{
		"overview":         overview,
		"repeatedIssues":   repeated,
		"inspectorQuality": quality,
		"pendingReviews":   pending,
		"numericDrifts":    numericDrifts,
		"generatedAt":      time.Now().Format(time.RFC3339),
	})
}

// GET /api/management-ai/attention?project=&limit=5&refresh=1
// risk_score 后端算重点关注,顺便把缓存的 AI 摘要塞进去。AI 摘要可空(降级)。
func (s *Server) handleManagementAttention(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	project := r.URL.Query().Get("project")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	refresh := r.URL.Query().Get("refresh") == "1"
	rangeKey := firstNonEmpty(r.URL.Query().Get("range"), "30d")
	items, err := s.toolListAttention(project, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "attention_failed", err.Error())
		return
	}
	// 缓存读 / 写
	var summary string
	var model string
	var promptVersion string
	var isMock bool
	var generatedAt time.Time
	if !refresh {
		if cached, err := s.store.GetLatestManagementAIReport("attention", project, rangeKey); err == nil {
			if time.Now().Before(cached.ExpiresAt) {
				summary = cached.Summary
				model = cached.Model
				promptVersion = cached.PromptVersion
				generatedAt = cached.GeneratedAt
				isMock = strings.HasPrefix(cached.PromptVersion, "v1-mock")
			}
		}
	}
	overview, _ := s.toolGetOverview(project, rangeKey)
	if summary == "" {
		// 调 ai-service(阶段一返回 mock,阶段二接真 DeepSeek)
		if s.analyticsClient != nil {
			payload := map[string]any{
				"overview":  overview,
				"attention": items,
				"rangeKey":  rangeKey,
				"project":   project,
			}
			resp, err := s.analyticsClient.Analyze(payload)
			if err != nil {
				log.Printf("WARN: management analyze failed (降级用规则版): %v", err)
			} else {
				if v, ok := resp["summary"].(string); ok {
					summary = v
				}
				if v, ok := resp["model"].(string); ok {
					model = v
				}
				if v, ok := resp["isMock"].(bool); ok {
					isMock = v
				}
				promptVersion = "v1-mock"
				if !isMock {
					promptVersion = "v1"
				}
				generatedAt = time.Now()
				_ = s.store.SaveManagementAIReport(&ManagementAIReport{
					ReportType: "attention", Project: project, RangeKey: rangeKey,
					Summary: summary, Attention: items, Model: model,
					PromptVersion: promptVersion,
					GeneratedAt:   generatedAt,
					ExpiresAt:     generatedAt.Add(30 * time.Minute),
				})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"items":         items,
		"summary":       summary,
		"model":         model,
		"isMock":        isMock,
		"promptVersion": promptVersion,
		"overview":      overview,
		"rangeKey":      rangeKey,
		"project":       project,
		"generatedAt":   generatedAt,
	})
}

// POST /api/management-ai/chat
// 转发到 ai-service /management/chat,带 snapshot+attention 上下文。
func (s *Server) handleManagementChat(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	var req struct {
		Message string           `json:"message"`
		History []map[string]any `json:"history,omitempty"`
		Project string           `json:"project,omitempty"`
		Range   string           `json:"range,omitempty"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		writeError(w, http.StatusBadRequest, "empty_message", "请输入要询问的问题")
		return
	}
	if s.analyticsClient == nil {
		writeError(w, http.StatusServiceUnavailable, "ai_unavailable", "管理 AI 服务未配置")
		return
	}
	rangeKey := firstNonEmpty(req.Range, "30d")
	overview, _ := s.toolGetOverview(req.Project, rangeKey)
	attention, _ := s.toolListAttention(req.Project, 5)
	// 多类聚合一起喂给 AI,让它能按问题挑对应数据,而不是每次都答最高风险资产
	repeated, _ := s.toolListRepeatedIssues(req.Project, 6)
	pending, _ := s.toolListPendingReviews(req.Project, 10)
	quality, _ := s.toolGetInspectorQuality(rangeKey)
	drift, _ := s.toolListNumericDrift(req.Project)
	if len(drift) > 8 {
		drift = drift[:8]
	}
	payload := map[string]any{
		"message": req.Message,
		"history": req.History,
		"context": map[string]any{
			"overview":         overview,
			"topRiskAssets":    attention,
			"repeatedIssues":   repeated,
			"pendingReviews":   pending,
			"inspectorQuality": quality,
			"numericDrift":     drift,
			"rangeKey":         rangeKey,
			"project":          req.Project,
		},
	}
	resp, err := s.analyticsClient.Chat(payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, "ai_call_failed", err.Error())
		return
	}
	// 溯源:按问句精准匹配本地依据(标准模块/记录/资产),附在回复后
	resp["sources"] = s.buildChatSources(req.Message, attention)
	writeJSON(w, http.StatusOK, resp)
}

// GET /api/management-ai/report?type=weekly&project=
// 周报:滚动近 7 天聚合(规则表格)+ AI 写一段态势综述。AI 挂了仍返回表格(综述降级为规则版)。
func (s *Server) handleManagementReport(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	reportType := firstNonEmpty(r.URL.Query().Get("type"), "weekly")
	project := r.URL.Query().Get("project")

	if reportType == "daily" {
		s.handleDailyReport(w, project)
		return
	}

	s.handleWeeklyReport(w, project)
}

// 周报:7 模块管理型(结论/指标/重点资产/异常闭环/质量协同/下周安排/溯源)
// 复用日报的记录-状态-任务聚合,搬到「近 7 天 vs 上 7 天」口径。
func (s *Server) handleWeeklyReport(w http.ResponseWriter, project string) {
	rangeKey := "7d"
	ctx, err := s.buildInsightsContext(project, rangeKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "weekly_failed", err.Error())
		return
	}
	overview, _ := s.toolGetOverview(project, rangeKey)
	attention, _ := s.toolListAttention(project, 8)
	repeated, _ := s.toolListRepeatedIssues(project, 8)
	quality, _ := s.toolGetInspectorQuality(rangeKey)
	pending, _ := s.toolListPendingReviews(project, 15)
	drift, _ := s.toolListNumericDrift(project)

	// 点位 → 资产 id 映射(记录只带 pointId,溯源跳转要资产 id)
	pointToAsset := map[string]string{}
	for _, a := range ctx.assets {
		if a.PointID != "" {
			pointToAsset[a.PointID] = a.ID
		}
	}

	// ---- 一次遍历:本周/上周分桶 + 异常闭环清单 + 质量计数 ----
	assetRecent := map[string]bool{}
	assetPrev := map[string]bool{}
	statusCount := map[string]int{}
	aiSuccess, retakes := 0, 0
	issueClosure := []map[string]any{}
	for _, r := range ctx.records {
		t := recordTimestamp(r)
		inRecent := !t.Before(ctx.rangeStart) && !t.After(ctx.rangeEnd)
		inPrev := !t.Before(ctx.prevStart) && t.Before(ctx.prevEnd)
		key := firstNonEmpty(r.PointID, r.ID)
		if inRecent {
			assetRecent[key] = true
		}
		if inPrev {
			assetPrev[key] = true
		}
		if !inRecent {
			continue
		}
		st := recordBusinessStatus(r)
		statusCount[st]++
		if r.RecognitionStatus == "recognized" {
			aiSuccess++
		}
		if r.CaptureAttempts > 1 || r.RecognitionStatus == "retake_required" {
			retakes++
		}
		if st == "异常" || st == "待复核" || st == "需补图" {
			field, value := primaryAbnormalField(r)
			assignee := r.Inspector
			dueAt := ""
			if r.EngineeringTaskID != "" {
				if tk, e := s.store.GetEngineeringTask(r.EngineeringTaskID); e == nil && tk != nil {
					assignee = firstNonEmpty(tk.AssigneeName, assignee)
					dueAt = tk.DueAt
				}
			}
			if assignee == "" {
				assignee = "待指派"
			}
			issueClosure = append(issueClosure, map[string]any{
				"issueName":  firstNonEmpty(field, "现场异常"),
				"value":      value,
				"foundAt":    t.Format("01/02"),
				"recordNo":   firstNonEmpty(r.RecordNo, r.ID),
				"recordId":   r.ID,
				"assetId":    pointToAsset[r.PointID],
				"assetName":  firstNonEmpty(r.PointName, r.TemplateName),
				"status":     st,
				"assignee":   assignee,
				"dueAt":      dueAt,
				"suggestion": weeklyIssueSuggestion(st, field),
			})
		}
	}
	if len(issueClosure) > 12 {
		issueClosure = issueClosure[:12]
	}

	// 质量:复核留痕(本周窗口)
	manualEdits, lowConf, noPhotoConfirm := 0, 0, 0
	for _, e := range ctx.confirmRecent {
		if e.CreatedAt.Before(ctx.rangeStart) || e.CreatedAt.After(ctx.rangeEnd) {
			continue
		}
		if e.Action == "correct" {
			manualEdits++
		}
		if e.AIConfidence > 0 && e.AIConfidence < 0.6 {
			lowConf++
		}
		if (e.Action == "confirm" || e.Action == "correct") && !e.ViewedPhoto {
			noPhotoConfirm++
		}
	}

	// 闭环:复查任务完成数(本周 vs 上周)
	closedRecent, closedPrev := 0, 0
	tasks, _ := s.store.ListEngineeringTasks(EngineeringTaskFilter{Project: project})
	for _, tk := range tasks {
		if tk.Status != engTaskStatusDone || tk.CompletedAt == "" {
			continue
		}
		ct := parseFlexTime(tk.CompletedAt)
		if ct.IsZero() {
			continue
		}
		if !ct.Before(ctx.rangeStart) && !ct.After(ctx.rangeEnd) {
			closedRecent++
		} else if !ct.Before(ctx.prevStart) && ct.Before(ctx.prevEnd) {
			closedPrev++
		}
	}

	metrics := map[string]any{
		"recordRecent": overview.RecordRecent, "recordPrev": overview.RecordPrev,
		"assetInspectedRecent": len(assetRecent), "assetInspectedPrev": len(assetPrev),
		"abnormalRecent": overview.AbnormalRecent, "abnormalPrev": overview.AbnormalPrev,
		"closedRecent": closedRecent, "closedPrev": closedPrev,
		"pendingReviews": overview.PendingReviews, "pendingApprovals": overview.PendingApprovals,
		"needRetake": statusCount["需补图"], "lazyConfirmRate": overview.LazyConfirmRate,
	}

	// 重点资产增强
	topRisk := make([]map[string]any, 0, len(attention))
	for _, a := range attention {
		topRisk = append(topRisk, map[string]any{
			"assetId": a.AssetID, "assetName": a.AssetName,
			"riskLevel": a.RiskLevel, "riskScore": a.RiskScore,
			"mainIssue":       firstNonEmpty(strings.Join(a.Reasons, "；"), a.Title),
			"aiBasis":         "异常频次+人工修正率+巡检间隔综合评分",
			"suggestedAction": weeklySuggestAction(a),
		})
	}

	// 下周工作安排(规则挑对象,纯展示)
	nextActions := s.buildWeeklyNextActions(attention, repeated, drift, pending)

	qualitySummary := map[string]any{
		"aiSuccess": aiSuccess, "manualEdits": manualEdits,
		"lowConfidenceFields": lowConf, "retakes": retakes,
		"noPhotoConfirm": noPhotoConfirm, "repeatedFieldIssues": len(repeated),
	}

	// AI 综述(只写本周结论;降级走规则版)
	summary, model, isMock := "", "", true
	if s.analyticsClient != nil {
		payload := map[string]any{
			"kind": "weekly", "period": "近 7 天",
			"overview": overview, "attention": attention,
			"repeatedIssues": repeated, "inspectorQuality": quality,
		}
		if resp, err := s.analyticsClient.Analyze(payload); err != nil {
			log.Printf("WARN: weekly report analyze failed (降级规则版): %v", err)
		} else {
			if v, ok := resp["summary"].(string); ok {
				summary = v
			}
			if v, ok := resp["model"].(string); ok {
				model = v
			}
			if v, ok := resp["isMock"].(bool); ok {
				isMock = v
			}
		}
	}
	if strings.TrimSpace(summary) == "" {
		summary = weeklySummaryFallback(overview, attention)
		isMock = true
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"type":             "weekly",
		"rangeKey":         rangeKey,
		"rangeStart":       overview.RangeStart,
		"rangeEnd":         overview.RangeEnd,
		"generatedAt":      time.Now().Format(time.RFC3339),
		"summary":          summary,
		"model":            model,
		"isMock":           isMock,
		"overview":         overview,
		"metrics":          metrics,
		"topRisk":          topRisk,
		"repeatedIssues":   repeated,
		"inspectorQuality": quality,
		"pending":          pending,
		"issueClosure":     issueClosure,
		"qualitySummary":   qualitySummary,
		"nextActions":      nextActions,
		"traceability": map[string]any{
			"sources":         []string{"巡检记录", "资产台账", "审批记录", "AI识别结果", "现场图片"},
			"recordTraceable": true,
			"assetTraceable":  true,
		},
	})
}

// 解析任务完成时间:兼容 RFC3339 与 "2006-01-02" 等
func parseFlexTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	if len(s) >= 10 {
		if t, err := time.Parse("2006-01-02", s[:10]); err == nil {
			return t
		}
	}
	return time.Time{}
}

// 异常项的处理建议(按业务状态,大白话)
func weeklyIssueSuggestion(status, field string) string {
	switch status {
	case "需补图":
		return "补拍清晰照片后复核"
	case "待复核":
		return "主管确认该项判定"
	case "异常":
		if field != "" {
			return "现场核实「" + field + "」并整改"
		}
		return "现场核实并整改"
	}
	return "跟进处理"
}

// 重点资产的建议动作
func weeklySuggestAction(a *AttentionItem) string {
	if a.RiskLevel == "danger" {
		return "安排专项现场复核"
	}
	return "下周复查跟进"
}

// 下周工作安排:规则挑对象 → 管理型工作项(纯展示,不落库)
func (s *Server) buildWeeklyNextActions(attention []*AttentionItem, repeated []*RepeatedIssue, drift []*NumericDriftEntry, pending *PendingReviews) []map[string]any {
	out := []map[string]any{}
	seen := map[string]bool{}
	add := func(item, target, trigger string) {
		k := item + "|" + target
		if target == "" || seen[k] || len(out) >= 6 {
			return
		}
		seen[k] = true
		out = append(out, map[string]any{
			"workItem": item, "target": target,
			"assignee": "待指派", "time": "下周", "trigger": trigger,
		})
	}
	// 规则1:高风险/重点资产 → 专项复核
	for _, a := range attention {
		if a.RiskLevel == "danger" {
			add("专项现场复核", a.AssetName, firstNonEmpty(strings.Join(a.Reasons, "；"), a.Title))
		}
	}
	// 规则2:重复异常字段 → 字段复查
	for _, ri := range repeated {
		add("复查「"+firstNonEmpty(ri.FieldLabel, ri.FieldKey)+"」", ri.AssetName, fmt.Sprintf("近期该字段异常 %d 次", ri.Count))
	}
	// 规则3:数值漂移大 → 读数复核
	for _, d := range drift {
		add("读数复核", d.AssetName, fmt.Sprintf("「%s」较历史波动 %.0f%%", firstNonEmpty(d.FieldLabel, d.FieldKey), d.ChangeRate*100))
	}
	return out
}

// 规则版周报综述(AI 不可用时降级用),不编造、只用聚合数字。
func weeklySummaryFallback(o *OverviewSummary, attention []*AttentionItem) string {
	if o == nil {
		return "近 7 天暂无足够数据生成综述。"
	}
	delta := o.RecordRecent - o.RecordPrev
	trend := "持平"
	if delta > 0 {
		trend = fmt.Sprintf("较上周增加 %d 条", delta)
	} else if delta < 0 {
		trend = fmt.Sprintf("较上周减少 %d 条", -delta)
	}
	parts := []string{
		fmt.Sprintf("近 7 天完成巡检 %d 条(%s),发现异常 %d 条,待复核 %d 条、待审批 %d 条",
			o.RecordRecent, trend, o.AbnormalRecent, o.PendingReviews, o.PendingApprovals),
	}
	if o.LazyConfirmRate > 0 {
		parts = append(parts, fmt.Sprintf("未看图即确认占比 %.1f%%,质量管控需关注", o.LazyConfirmRate*100))
	}
	if len(attention) > 0 {
		names := make([]string, 0, 3)
		for i, a := range attention {
			if i >= 3 {
				break
			}
			names = append(names, a.AssetName)
		}
		parts = append(parts, "重点关注 "+strings.Join(names, "、"))
	}
	return strings.Join(parts, ";") + "。"
}

// 记录的业务状态(日报口径):异常 > 需补图 > 待复核 > 人工填写 > 正常
func recordBusinessStatus(r *Record) string {
	if recordIsAbnormal(r) {
		return "异常"
	}
	if r.RecognitionStatus == "retake_required" {
		return "需补图"
	}
	if r.RecognitionStatus == "manual_required" || hasNeedsReview(r) {
		return "待复核"
	}
	if r.ManualRequired {
		return "人工填写"
	}
	return "正常"
}

func statusRisk(st string) string {
	switch st {
	case "异常":
		return "danger"
	case "待复核", "需补图":
		return "warning"
	}
	return "normal"
}

// 取记录里最能代表问题的字段(优先异常值字段,其次待复核字段)
func primaryAbnormalField(r *Record) (string, string) {
	for _, f := range r.Fields {
		if abnormalValueRE.Match(f.Value) {
			return firstNonEmpty(f.Label, f.Code), f.Value
		}
	}
	for _, f := range r.Fields {
		if f.NeedsReview {
			return firstNonEmpty(f.Label, f.Code), f.Value
		}
	}
	return "", ""
}

func truncateRunes(s string, n int) string {
	rs := []rune(strings.TrimSpace(s))
	if len(rs) <= n {
		return string(rs)
	}
	return string(rs[:n]) + "…"
}

func dailySummaryFallback(o *OverviewSummary, abnormal, done, plan int) string {
	if o == nil {
		return "今日暂无巡检数据。"
	}
	head := "暂未发现异常"
	if abnormal > 0 {
		head = fmt.Sprintf("发现 %d 项异常待处理", abnormal)
	}
	return fmt.Sprintf("今日完成巡检 %d 条,%s;待复核 %d 条、待审批 %d 条;任务完成 %d/%d。",
		o.RecordRecent, head, o.PendingReviews, o.PendingApprovals, done, plan)
}

// GET /api/management-ai/report?type=daily —— 管理日报:今日 00:00~现在,业务状态口径,管理摘要 + 巡检明细两层。
func (s *Server) handleDailyReport(w http.ResponseWriter, project string) {
	ctx, err := s.buildInsightsContext(project, "today")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "daily_failed", err.Error())
		return
	}
	overview, _ := s.toolGetOverview(project, "today")
	attention, _ := s.toolListAttention(project, 5)
	repeated, _ := s.toolListRepeatedIssues(project, 5)
	drift, _ := s.toolListNumericDrift(project)
	if len(drift) > 5 {
		drift = drift[:5]
	}

	statusCount := map[string]int{}
	abnormalList := []map[string]any{}
	normalList := []map[string]any{}
	aiSuccess, retakes, manualFill := 0, 0, 0
	for _, r := range ctx.records {
		t := recordTimestamp(r)
		if t.Before(ctx.rangeStart) || t.After(ctx.rangeEnd) {
			continue
		}
		st := recordBusinessStatus(r)
		statusCount[st]++
		if r.RecognitionStatus == "recognized" {
			aiSuccess++
		}
		if r.CaptureAttempts > 1 || r.RecognitionStatus == "retake_required" {
			retakes++
		}
		if r.ManualRequired {
			manualFill++
		}
		if st == "异常" || st == "待复核" || st == "需补图" {
			field, value := primaryAbnormalField(r)
			assignee := r.Inspector
			dueAt := ""
			if r.EngineeringTaskID != "" {
				if tk, e := s.store.GetEngineeringTask(r.EngineeringTaskID); e == nil && tk != nil {
					assignee = firstNonEmpty(tk.AssigneeName, assignee)
					dueAt = tk.DueAt
				}
			}
			abnormalList = append(abnormalList, map[string]any{
				"point": r.PointName, "template": r.TemplateName,
				"field": field, "value": value,
				"risk": statusRisk(st), "status": st,
				"basis":    truncateRunes(r.AISummary, 40),
				"assignee": assignee, "dueAt": dueAt,
				"recordNo": firstNonEmpty(r.RecordNo, r.ID),
			})
		} else if len(normalList) < 20 {
			sub := ""
			if r.SubmittedAt != nil {
				sub = r.SubmittedAt.Format("15:04")
			}
			normalList = append(normalList, map[string]any{
				"point": r.PointName, "template": r.TemplateName,
				"inspector": r.Inspector, "submittedAt": sub,
			})
		}
	}

	manualEdits, lowConf, noPhotoConfirm := 0, 0, 0
	for _, e := range ctx.confirmRecent {
		if e.CreatedAt.Before(ctx.rangeStart) || e.CreatedAt.After(ctx.rangeEnd) {
			continue
		}
		if e.Action == "correct" {
			manualEdits++
		}
		if e.AIConfidence > 0 && e.AIConfidence < 0.6 {
			lowConf++
		}
		if (e.Action == "confirm" || e.Action == "correct") && !e.ViewedPhoto {
			noPhotoConfirm++
		}
	}

	tasks, _ := s.store.ListEngineeringTasks(EngineeringTaskFilter{Project: project})
	planN, doneN, procN, todoN, overdueN, closedToday := len(tasks), 0, 0, 0, 0, 0
	todayStr := ctx.rangeStart.Format("2006-01-02")
	for _, tk := range tasks {
		switch tk.Status {
		case engTaskStatusDone:
			doneN++
			if strings.HasPrefix(tk.CompletedAt, todayStr) {
				closedToday++
			}
		case engTaskStatusProcessing:
			procN++
		case engTaskStatusOverdue:
			overdueN++
		default:
			todoN++
			if tk.DueAt != "" && tk.DueAt < todayStr {
				overdueN++
			}
		}
	}
	completeRate := 0.0
	if planN > 0 {
		completeRate = float64(doneN) / float64(planN)
	}

	abnormalToday := statusCount["异常"]
	focus := make([]string, 0, 3)
	for _, a := range attention {
		focus = append(focus, a.AssetName)
		if len(focus) >= 3 {
			break
		}
	}

	resp := map[string]any{
		"type":        "daily",
		"date":        todayStr,
		"project":     project,
		"generatedAt": time.Now().Format(time.RFC3339),
		"rangeStart":  ctx.rangeStart.Format(time.RFC3339),
		"rangeEnd":    ctx.rangeEnd.Format(time.RFC3339),
		"conclusion": map[string]any{
			"hasAbnormal":   abnormalToday > 0 || overview.AbnormalRecent > 0,
			"abnormalCount": abnormalToday,
			"pendingCount":  overview.PendingReviews + overview.PendingApprovals,
			"closedCount":   closedToday,
		},
		"execution": map[string]any{
			"plan": planN, "done": doneN, "processing": procN,
			"notStarted": todoN, "overdue": overdueN, "completeRate": completeRate,
		},
		"assetStatus": map[string]any{
			"inspected":     statusCount["正常"] + statusCount["异常"] + statusCount["待复核"] + statusCount["需补图"] + statusCount["人工填写"],
			"normal":        statusCount["正常"],
			"abnormal":      statusCount["异常"],
			"pendingReview": statusCount["待复核"],
			"needRetake":    statusCount["需补图"],
			"manualFill":    manualFill,
		},
		"abnormalList":  abnormalList,
		"normalSummary": map[string]any{"count": statusCount["正常"], "items": normalList},
		"reviewQuality": map[string]any{
			"aiSuccess": aiSuccess, "manualEdits": manualEdits, "lowConf": lowConf,
			"retakes": retakes, "noPhotoConfirm": noPhotoConfirm, "needSupervisor": statusCount["待复核"],
		},
		"compare": map[string]any{
			"recordDelta":    overview.RecordRecent - overview.RecordPrev,
			"abnormalDelta":  overview.AbnormalRecent - overview.AbnormalPrev,
			"repeatedIssues": repeated,
			"drift":          drift,
		},
		"nextStep": map[string]any{
			"carryOver": procN + todoN + overdueN, "focusAssets": focus,
			"approvals": overview.PendingApprovals, "recheckSuggest": focus,
		},
		"overview": overview,
	}

	summary, model, isMock := "", "", true
	if s.analyticsClient != nil {
		payload := map[string]any{
			"kind": "daily", "period": "今日",
			"overview": overview, "conclusion": resp["conclusion"],
			"execution": resp["execution"], "assetStatus": resp["assetStatus"],
			"abnormalList": abnormalList, "attention": attention,
		}
		if r2, e := s.analyticsClient.Analyze(payload); e != nil {
			log.Printf("WARN: daily report analyze failed (降级规则版): %v", e)
		} else {
			if v, ok := r2["summary"].(string); ok {
				summary = v
			}
			if v, ok := r2["model"].(string); ok {
				model = v
			}
			if v, ok := r2["isMock"].(bool); ok {
				isMock = v
			}
		}
	}
	if strings.TrimSpace(summary) == "" {
		summary = dailySummaryFallback(overview, abnormalToday, doneN, planN)
		isMock = true
	}
	resp["summary"], resp["model"], resp["isMock"] = summary, model, isMock
	writeJSON(w, http.StatusOK, resp)
}

// 检查项关键词 → 字段 code(问句命中即给该字段的标准依据)
// 给主管看的大白话判定说明(溯源展示用;AI 用的技术判定提示词另算,不混用)
var fieldPlain = map[string]string{
	"extinguisher_valid": "看灭火器有没有过期。瓶身检验/维修标签上的有效期还没到、压力表指针在绿区,算合格;过了有效期、指针在红区(没气或超压)、或检查记录卡很久没人填,算不合格。注意:出厂/生产日期不等于有效期,只有生产日期且已超过 5 年还没维修的,也要判不合格。照片看不清日期或压力表时先不下结论,留人工现场核。",
	"anti_clip":          "测电梯门的防夹。关门时在门口挡一下,门能停住或回弹重开,就算正常;只有明显夹人、不回弹才算不合格。判定从宽,有回弹动作即视为正常。",
	"door_smooth":        "看开关门顺不顺。门开合到位、不卡顿不异响,算合格;明显卡阻、关不严、反复弹跳,算不合格。",
	"floor_buttons":      "看楼层按钮和显示屏。按钮齐全、按下有反馈、楼层显示正常,算合格;按钮缺失或失灵、显示黑屏或乱码,算不合格。",
	"car_lighting":       "看轿厢和候梯厅的照明。灯亮、够亮,算合格;不亮、明显昏暗、灯具坏了,算不合格。",
	"reg_mark":           "看《特种设备使用标志》/登记证。在位、信息清晰、没破损褪色、没过下次检验日期,算合格;缺失、模糊、已过期,算不合格。",
	"fire_switch_glass":  "看消防返回开关的玻璃。完好无破损,算合格;玻璃破碎或缺失,算不合格。",
	"door_window_sign":   "看机房门和警示标识。门完好关好、'电梯机房'和'危险/禁止入内'等标识齐全完好,算合格;标识缺失破损、门坏了,算不合格。",
	"room_clean":         "看机房地面。基本整洁、没明显杂物堆放/积水/油污,算合格;堆了杂物纸箱、积水漏油、大面积脏乱,算不合格。",
	"lighting_ac":        "看机房照明和空调。灯亮、空调或温控在运行(显示温度、有制冷),算合格;没照明、空调黑屏不工作、室温明显偏高,算不合格。",
	"rescue_device":      "看机房应急救援三件套。盘车手轮、松闸扳手、救援说明牌都在且就位,算合格;缺其中一项,算不合格。",
	"noise_smell":        "电梯运行有没有异响、异味。照片判断不了声音和气味,只有检查记录里明确写了异响异味才记不合格,否则留人工现场判断。",
	"alarm_device":       "测紧急报警/对讲。按警铃或对讲能接通、有响应,算合格;按了没反应、打不通,算不合格。",
}

// 权威来源:检查项 → {精确标准名, 官方标准系统链接}。链到稳定官方入口,标准名是准确引用。
type stdSource struct{ Name, URL string }

const (
	officialSysGB  = "https://openstd.samr.gov.cn/bzgk/gb/" // 国家标准全文公开系统(GB 标准)
	officialSysTSG = "https://www.samr.gov.cn/"             // 市场监管总局(特种设备安全技术规范 TSG 发布机关)
)

var fieldStandard = map[string]stdSource{
	"extinguisher_valid": {"GB 50444《建筑灭火器配置验收及检查规范》", officialSysGB},
	"anti_clip":          {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"door_smooth":        {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"floor_buttons":      {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"car_lighting":       {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"reg_mark":           {"TSG 08《特种设备使用管理规则》", officialSysTSG},
	"fire_switch_glass":  {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"door_window_sign":   {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"room_clean":         {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"lighting_ac":        {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"rescue_device":      {"TSG T5002《电梯维护保养规则》", officialSysTSG},
	"noise_smell":        {"GB/T 18775《电梯、自动扶梯和自动人行道维修规范》", officialSysGB},
	"alarm_device":       {"TSG T5002《电梯维护保养规则》", officialSysTSG},
}

var fieldAliases = map[string][]string{
	"extinguisher_valid": {"灭火器", "灭火", "过期", "压力表"},
	"anti_clip":          {"防夹", "夹人", "光幕"},
	"door_smooth":        {"开关门", "卡阻", "门运行"},
	"floor_buttons":      {"选层", "楼层显示", "按钮面板"},
	"car_lighting":       {"照明", "灯光", "候梯厅"},
	"reg_mark":           {"登记标志", "使用标志", "登记证"},
	"fire_switch_glass":  {"消防开关", "消防返回", "开关玻璃"},
	"door_window_sign":   {"机房门", "警示标识", "门窗"},
	"room_clean":         {"机房干净", "杂物", "整洁", "清洁"},
	"lighting_ac":        {"机房照明", "空调", "温控"},
	"rescue_device":      {"救援", "盘车", "松闸"},
	"noise_smell":        {"异响", "异味", "声音", "气味"},
	"alarm_device":       {"报警", "警铃", "对讲"},
}

func containsAny(s string, kws []string) bool {
	for _, k := range kws {
		if k != "" && strings.Contains(s, k) {
			return true
		}
	}
	return false
}

// buildChatSources — 按问句精准匹配本地依据:命中检查项→标准模块;提到设备/带异常意图→记录+异常史
func (s *Server) buildChatSources(question string, attention []*AttentionItem) []map[string]any {
	out := []map[string]any{}
	seen := map[string]bool{}

	// 维保/标准类问句,额外附权威官方来源链接
	wantStandard := containsAny(question, []string{"维保", "维护", "保养", "标准", "规范", "如何", "怎么", "依据", "要求", "年限", "报废", "规程"})

	// 1. 标准模块:问句命中某检查项关键词 → 该字段判定标准(+ 权威来源)
	tpls, _ := s.store.ListPromptTemplates()
	for _, t := range tpls {
		for _, f := range t.Fields {
			if seen["s:"+f.Code] {
				continue
			}
			if containsAny(question, fieldAliases[f.Code]) {
				seen["s:"+f.Code] = true
				detail := fieldPlain[f.Code] // 主管看大白话
				if detail == "" {
					detail = renderFieldCriteria(f) // 没写大白话的回退到判定标准
				}
				out = append(out, map[string]any{
					"type": "standard", "title": "标准 · " + f.Label,
					"summary": t.Name, "detail": detail,
				})
				// 维保/标准类:附权威官方来源(精确标准名 + 官方系统链接)
				if wantStandard {
					if st, ok := fieldStandard[f.Code]; ok {
						out = append(out, map[string]any{
							"type": "official", "title": st.Name, "url": st.URL,
							"summary": "权威标准 · 点击查看官方来源",
						})
					}
				}
			}
		}
	}

	// 2. 记录/资产:问句提到具体设备名,或带"异常类"意图(才给设备依据,否则不给)
	anomalyIntent := containsAny(question, []string{"异常", "故障", "问题", "重点", "关注", "趋势", "风险", "复查", "隐患", "处理"})
	added := 0
	for _, a := range attention {
		hit := (a.AssetName != "" && strings.Contains(question, a.AssetName)) || (anomalyIntent && added < 2)
		if !hit {
			continue
		}
		if a.LastRecordID != "" && !seen["r:"+a.LastRecordID] {
			seen["r:"+a.LastRecordID] = true
			out = append(out, map[string]any{
				"type": "record", "title": a.AssetName + " · 最近巡检记录",
				"summary": strings.Join(a.Reasons, "；"), "recordId": a.LastRecordID,
			})
		}
		if !seen["a:"+a.AssetID] {
			seen["a:"+a.AssetID] = true
			out = append(out, map[string]any{
				"type": "asset", "title": a.AssetName + " · 异常史",
				"summary": a.Title, "assetId": a.AssetID,
			})
		}
		added++
	}
	return out
}

// ===== Agent 执行入口 /api/management-ai/act =====
//
// L2「一键执行」的落地端:管理 AI 只能产出「动作提议」,真正的写操作全部走这里——
// 由后端按 type 校验、调现有内部逻辑、权限门控 + 审计。模型永远碰不到真实 API。

func (s *Server) handleManagementAct(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	var req struct {
		Type     string         `json:"type"`
		TargetID string         `json:"targetId"`
		Params   map[string]any `json:"params"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	switch req.Type {
	case "create_recheck_task":
		s.actCreateRecheckTask(w, r, req.TargetID, req.Params)
	default:
		writeError(w, http.StatusBadRequest, "unknown_action", "未知或暂不支持的动作类型")
	}
}

// actCreateRecheckTask 给某资产派一条「异常复查」工程任务。
// 资产已有在途任务则复用并更新责任人/截止(再派发),否则新建;
// 走的是和自动兜底同一条 TaskType=异常复查 + 待整改 路径,完成后复用既有的复检自动销账。
func (s *Server) actCreateRecheckTask(w http.ResponseWriter, r *http.Request, assetID string, params map[string]any) {
	assetID = strings.TrimSpace(assetID)
	if assetID == "" {
		writeError(w, http.StatusBadRequest, "missing_target", "缺少目标资产")
		return
	}
	asset, err := s.store.GetAsset(assetID)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "目标资产不存在")
		return
	}
	assignee := strings.TrimSpace(actParamString(params, "assignee"))
	if assignee == "" {
		assignee = asset.LastInspector
	}
	dueAt := strings.TrimSpace(actParamString(params, "dueAt"))
	if dueAt == "" {
		dueAt = time.Now().AddDate(0, 0, 3).Format("2006-01-02")
	}
	reason := strings.TrimSpace(actParamString(params, "reason"))

	// 去重:该资产已有在途任务时,改为更新责任人/截止(再派发),不重复建
	if existing, err := s.store.ListEngineeringTasks(EngineeringTaskFilter{}); err == nil {
		for _, t := range existing {
			if t == nil || t.AssetID != assetID {
				continue
			}
			switch t.Status {
			case engTaskStatusDraft, engTaskStatusPending, engTaskStatusProcessing, engTaskStatusRectify, engTaskStatusOverdue:
				_ = s.store.UpdateEngineeringTask(t.ID, func(task *EngineeringTask) {
					if assignee != "" {
						task.AssigneeName = assignee
					}
					if dueAt != "" {
						task.DueAt = dueAt
					}
				})
				updated, _ := s.store.GetEngineeringTask(t.ID)
				s.recordOperation(r, "agent_recheck_redispatch", "engineering_task", t.ID, map[string]any{
					"asset": asset.AssetName, "assignee": assignee, "dueAt": dueAt,
				})
				writeJSON(w, http.StatusOK, map[string]any{
					"task":    updated,
					"reused":  true,
					"message": "该资产已有在途复查任务,已更新责任人/截止",
				})
				return
			}
		}
	}

	task := &EngineeringTask{
		ID:             newID("eng_task"),
		Source:         "agent-recheck",
		TaskType:       "异常复查",
		Title:          firstNonEmpty(asset.AssetName, "设备") + " 异常复查",
		Project:        asset.Project,
		Category:       asset.AssetType,
		AssetID:        asset.ID,
		AssigneeName:   assignee,
		DueAt:          dueAt,
		Status:         engTaskStatusRectify,
		EvidenceStatus: "待复查",
		AIStatus:       "待复查",
		CloseResult:    asset.LastStatus,
		CloseNote:      firstNonEmpty(reason, "管理 AI 派发复查"),
	}
	normalizeEngineeringTask(task)
	task.Status = engTaskStatusRectify
	if err := s.store.CreateEngineeringTask(task); err != nil {
		writeError(w, http.StatusInternalServerError, "create_task_failed", err.Error())
		return
	}
	s.recordOperation(r, "agent_recheck_create", "engineering_task", task.ID, map[string]any{
		"asset": asset.AssetName, "assignee": task.AssigneeName, "dueAt": dueAt,
	})
	writeJSON(w, http.StatusCreated, map[string]any{
		"task":    task,
		"message": "复查任务已派发",
	})
}

func actParamString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
