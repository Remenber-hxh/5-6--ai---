package main

import (
	"net/http"
	"strings"
)

// ===== 把一个读数整体挪到另一台设备名下 =====
//
// 【为什么需要这个动作】抄表的确认页按拍照顺序排:一张照片一行,行上选
// 「这是哪台表」。选的那一下,人表达的是"这个读数是 Z3 的,不是 Z1 的" ——
// 那就得让读数、照片、置信度、AI 原值一起搬到 z3_reading 上去,
// 而不是给 z1_reading 改个名字。
//
// 【为什么不能让前端发两次 PATCH】搬家要么整个成、要么整个不成。
// 拆成"清掉这边"+"写到那边"两次请求,中间断一次就变成:读数在两边都有,
// 或者两边都没有 —— 而两次请求各自都返回 200,现场看不出发生过什么。
//
// 【为什么不是给日报改成按 assetName 出】Z1~Z4 是日报和纸质表上的固定栏位。
// 让栏位跟着人选的设备名走,就会出现日报上没有 Z1 这一栏、却有两栏叫 Z3。
// 问题从"名字对不上"变成"栏位对不上",更难查。所以搬值,不搬名。

// handleMoveReading —— POST /api/inspection/records/{id}/fields/move
//
//	{"fromCode":"z1_reading","toCode":"z3_reading"}
func (s *Server) handleMoveReading(w http.ResponseWriter, r *http.Request, recordID string) {
	var req struct {
		FromCode string `json:"fromCode"`
		ToCode   string `json:"toCode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	req.FromCode = strings.TrimSpace(req.FromCode)
	req.ToCode = strings.TrimSpace(req.ToCode)
	if req.FromCode == "" || req.ToCode == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "缺少字段标识")
		return
	}
	if req.FromCode == req.ToCode {
		writeError(w, http.StatusBadRequest, "same_field", "挪到的是同一格,不用挪")
		return
	}

	// 权限在前缀路由那层已经查过了(requireRecordAccess,写操作走 write=true),
	// 这里不再查一遍 —— 同一件事查两处,迟早有一处和另一处的口径漂开。
	rec, err := s.store.GetRecord(s.tenantForRequest(r), recordID)
	if err != nil || rec == nil {
		writeError(w, http.StatusNotFound, "not_found", "记录不存在")
		return
	}
	if rec.Submitted {
		// 提交过的记录要改,走「修改申请」那条路 —— 那里有审批留痕。
		writeError(w, http.StatusConflict, "already_submitted", "这条已提交,改动要走修改申请")
		return
	}

	from, fromIdx := fieldByCode(rec.Fields, req.FromCode)
	to, toIdx := fieldByCode(rec.Fields, req.ToCode)
	if from == nil || to == nil {
		writeError(w, http.StatusBadRequest, "bad_field", "字段不存在")
		return
	}
	// 【目标格已经有读数就拦住】不拦的话会把那台表刚抄的数冲掉,
	// 而且日报上会出现两个一样的读数、少一台表 —— 现场看不出来。
	if strings.TrimSpace(to.Value) != "" {
		writeError(w, http.StatusConflict, "target_occupied",
			"「"+orNone(to.Label)+"」已经有读数了,先把那一格清掉再挪")
		return
	}

	moved := *from
	// 【搬的是整组:读数 + 照片 + 置信度 + AI 原值 + 理由】
	// 只搬读数的话,那一行下面还摆着原来那张照片 —— 人看着照片核另一台表的数。
	rec.Fields[toIdx].Value = moved.Value
	rec.Fields[toIdx].AIValue = moved.AIValue
	rec.Fields[toIdx].Confidence = moved.Confidence
	rec.Fields[toIdx].Reason = moved.Reason
	rec.Fields[toIdx].NeedsReview = moved.NeedsReview
	rec.Fields[toIdx].SourceImageID = moved.SourceImageID
	rec.Fields[toIdx].Bbox = moved.Bbox
	// 挪过来这一下是人做的判断,不是 AI 填的 —— source 要说实话
	rec.Fields[toIdx].Source = "human-edited"
	rec.Fields[toIdx].Version++

	// 原来那格清空。【AssetName 不动】它是"这一格属于哪台设备"的配置,
	// 不随某一次读数搬家 —— 清掉的话这一格下次就不知道自己是谁了。
	rec.Fields[fromIdx].Value = ""
	rec.Fields[fromIdx].AIValue = ""
	rec.Fields[fromIdx].Confidence = 0
	rec.Fields[fromIdx].Reason = ""
	rec.Fields[fromIdx].NeedsReview = false
	rec.Fields[fromIdx].SourceImageID = ""
	rec.Fields[fromIdx].Bbox = nil
	rec.Fields[fromIdx].Source = "human-edited"
	rec.Fields[fromIdx].Version++

	rec.Report = buildDailyPreview(rec)
	if err := s.store.UpdateRecord(rec); err != nil {
		writeError(w, http.StatusInternalServerError, "update_failed", err.Error())
		return
	}

	// 留痕:谁把哪个读数从哪一格挪到了哪一格。和改数值同等重要 ——
	// 挪错了,日报上两台表的数就对调了,而数字本身都是对的。
	operator := rec.Inspector
	if u, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		operator = firstNonEmpty(u.DisplayName, u.Username)
	}
	_ = s.store.CreateFieldConfirmLog(&FieldConfirmLog{
		RecordID:      recordID,
		FieldKey:      req.ToCode,
		FieldLabel:    to.Label,
		AIValue:       moved.AIValue,
		OriginalValue: "(空)",
		FinalValue:    moved.Value + "(从「" + orNone(from.Label) + "」挪来)",
		Action:        "move",
		Operator:      operator,
	})

	// 【和 GET 同一个形状】原来这里只裁了字段,没填设备候选 —— 候选是现算的、
	// 不存库,于是前端拿这个返回值刷新之后,读数行的设备下拉里一台都没有,
	// 要等下一次 GET 才回来。接口返回 200,界面看着"就是选不了"。
	s.respondRecord(w, rec)
}
