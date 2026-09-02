package main

import (
	"net/http"
	"strconv"
	"strings"
)

// ===== 没提交完的记录 =====
//
// 【为什么必须能看见】提交被打断(退出微信、信号断、手机被杀后台)之后,
// 那条记录就留在库里没提交。而它在手机上【一个入口都没有】:
// 首页、任务、待处理、台账都不列它。
//
// 更要命的是照片跟着一起消失:建记录时照片已经从「待处理」里被认领走了,
// 所以人既回不到那条记录,也拿不回照片重做一次 —— 现场白跑一趟。
//
// 这一屏给的是两条出路,都由本人决定:接着提交,或者直接删掉。

const draftListLimit = 30

type draftBrief struct {
	ID           string `json:"id"`
	TemplateName string `json:"templateName"`
	Project      string `json:"project"`
	PointName    string `json:"pointName"`
	AssetNo      string `json:"assetNo"`
	CreatedAt    string `json:"createdAt"`
	ImageCount   int    `json:"imageCount"`
	// FieldsFilled / FieldsTotal 让人一眼看出这条草稿离交完还差多少 ——
	// 只给一个时间戳的话,人分不清"刚建的空壳"和"填完就差点提交"。
	FieldsFilled int `json:"fieldsFilled"`
	FieldsTotal  int `json:"fieldsTotal"`
}

// handleListDrafts —— GET /api/inspection/drafts
//
// 只列【自己的】未提交记录。不按数据范围放宽:草稿是没做完的活,
// 不是给主管看的数据,别人看见了也做不了什么。
func (s *Server) handleListDrafts(w http.ResponseWriter, r *http.Request) {
	limit := draftListLimit
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = min(n, draftListLimit)
		}
	}

	tenantID := s.tenantForRequest(r)
	var records []*Record
	var err error
	if user, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
		records, err = s.store.ListDraftsByOwner(tenantID, user.ID, user.DisplayName, user.Username, limit)
	} else if s.localNoAuthAllowed(r) {
		records, err = s.store.ListDraftsByOwner(tenantID, "", userName(r), "", limit)
	} else {
		writeError(w, http.StatusForbidden, "forbidden", "请使用巡检员账号登录")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	out := make([]draftBrief, 0, len(records))
	for _, rec := range records {
		// 用当前模板过一遍:模板改过之后,草稿里可能留着已经删掉的字段,
		// 拿它算"填了几项"会算出一个比总数还大的数。
		clean := sanitizeRecordForCurrentTemplate(rec)
		filled := 0
		for _, f := range clean.Fields {
			if strings.TrimSpace(f.Value) != "" {
				filled++
			}
		}
		out = append(out, draftBrief{
			ID:           clean.ID,
			TemplateName: clean.TemplateName,
			Project:      clean.Project,
			PointName:    clean.PointName,
			AssetNo:      draftAssetNo(clean),
			CreatedAt:    clean.CreatedAt.Format("2006-01-02 15:04"),
			ImageCount:   len(clean.Images),
			FieldsFilled: filled,
			FieldsTotal:  len(clean.Fields),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"drafts": out})
}

// draftAssetNo 取设备编号,用来在列表里认出"这是哪一台"。
// 取不到就空着 —— 编空一个编号出来比不显示更糟。
func draftAssetNo(rec *Record) string {
	for _, f := range rec.Fields {
		if f.Code == "asset_no" {
			return strings.TrimSpace(f.Value)
		}
	}
	return ""
}
