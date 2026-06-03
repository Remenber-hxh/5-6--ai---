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
	payload := map[string]any{
		"message": req.Message,
		"history": req.History,
		"context": map[string]any{
			"overview":  overview,
			"attention": attention,
			"rangeKey":  rangeKey,
			"project":   req.Project,
		},
	}
	resp, err := s.analyticsClient.Chat(payload)
	if err != nil {
		writeError(w, http.StatusBadGateway, "ai_call_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}
