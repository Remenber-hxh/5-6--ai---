package main

import (
	"net/http"
	"strings"
)

// ===== 两格读数对调 =====
//
// 【为什么单有一个"对调",不能用两次 move 凑】move 的规矩是"目标格有读数就拒绝"
// (见 reading_move.go,那条规矩本身是对的:不拒绝就会把人刚抄的数冲掉)。
// 于是 A、B 两格都有数时,move 一步都走不了 —— 得先把 B 挪到某个空格寄存、
// 再把 A 挪进 B、再把寄存的挪回 A,三次请求。中间断一次,读数就停在一个
// 谁也说不清的中间态上,而每次请求都返回 200。
//
// 【为什么现场一定会需要对调】抄表按拍照顺序一行一张图,哪张图是哪台表由人来认。
// AI 的初始归属经常是错位的:第 3 张的数落在了「消防水表」那一格上。
// 人要做的就是"把这两行换过来"—— 这是个对调,不是搬家。
//
// 2026-09-22 线上卡死的就是这件事:我先前把"已经归了别行的设备"在下拉里
// 一律禁掉,而两个水表的格子里正装着电表的读数。要腾出水表得先改那两格,
// 可所有设备都是灰的 —— 一步都动不了。禁用解决不了错位,对调才能。

// handleSwapReading —— POST /api/inspection/records/{id}/fields/swap
//
//	{"aCode":"z1_reading","bCode":"fire_water_reading"}
//
// 两格的【读数、照片、置信度、AI 原值、理由】整组互换。
// AssetName 不换 —— 它是"这一格属于哪台设备"的配置,不随读数走
// (和 move 同一个道理:日报上的栏位是固定的,搬值不搬名)。
func (s *Server) handleSwapReading(w http.ResponseWriter, r *http.Request, recordID string) {
	var req struct {
		ACode string `json:"aCode"`
		BCode string `json:"bCode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.ACode = strings.TrimSpace(req.ACode)
	req.BCode = strings.TrimSpace(req.BCode)
	if req.ACode == "" || req.BCode == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少字段标识")
		return
	}
	if req.ACode == req.BCode {
		writeError(w, http.StatusBadRequest, "same_field", "换的是同一格,不用换")
		return
	}

	// 权限在前缀路由那层已经查过(requireRecordAccess,写操作 write=true)。
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil || rec == nil {
		writeError(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	if rec.Submitted {
		writeError(w, http.StatusConflict, "already_submitted", "这条已提交,改动要走修改申请")
		return
	}

	a, ai := fieldByCode(rec.Fields, req.ACode)
	b, bi := fieldByCode(rec.Fields, req.BCode)
	if a == nil || b == nil {
		writeError(w, http.StatusBadRequest, "bad_field", "字段不存在")
		return
	}

	swapFieldPayload(&rec.Fields[ai], &rec.Fields[bi])

	rec.Report = buildDailyPreview(rec)
	if err := s.store.UpdateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	// 【两格各留一条】对调是一次操作、两格都变了。只记一条的话,
	// 事后查"这个数怎么跑到这一格来的"只能查到一半。
	operator := rec.Inspector
	if u, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		operator = firstNonEmpty(u.DisplayName, u.Username)
	}
	for _, idx := range []int{ai, bi} {
		f := rec.Fields[idx]
		_ = s.store.CreateFieldConfirmLog(&FieldConfirmLog{
			RecordID:   recordID,
			FieldKey:   f.Code,
			FieldLabel: f.Label,
			AIValue:    f.AIValue,
			FinalValue: f.Value,
			Action:     "swap",
			Operator:   operator,
		})
	}

	s.fillReadingAssetOptions(rec)
	writeJSON(w, http.StatusOK, rec)
}

// swapFieldPayload 两格互换"这一次抄到的东西",不换这一格是谁。
//
// 【换哪些必须和 move 搬的那一组一字不差】少换一样就会出现
// "读数换过来了、照片还是原来那张"——人看着 A 的照片核 B 的数,
// 而两边都不报错。
func swapFieldPayload(a, b *FieldValue) {
	a.Value, b.Value = b.Value, a.Value
	a.AIValue, b.AIValue = b.AIValue, a.AIValue
	a.Confidence, b.Confidence = b.Confidence, a.Confidence
	a.Reason, b.Reason = b.Reason, a.Reason
	a.NeedsReview, b.NeedsReview = b.NeedsReview, a.NeedsReview
	a.SourceImageID, b.SourceImageID = b.SourceImageID, a.SourceImageID
	a.Bbox, b.Bbox = b.Bbox, a.Bbox
	// 换这一下是人做的判断,不是 AI 填的
	a.Source, b.Source = "human-edited", "human-edited"
	a.Version++
	b.Version++
}
