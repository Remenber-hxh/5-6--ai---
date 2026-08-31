package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

// ===== 一台设备身上还没了结的事 =====
//
// 【这是设备档案页最要紧的一块】一台设备挂着未销账的异常,却在它自己的
// 档案页上看不见 —— 那这一页就只能用来"查看",不能用来"判断"。
// 打开这一页的人要的从来是同一件事:这台设备现在要不要我管。

// openTaskBrief 一条还没了结的任务。只给判断需要的字段,不搬整个任务对象。
type openTaskBrief struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	AssigneeName string `json:"assigneeName,omitempty"`
	DueAt        string `json:"dueAt,omitempty"`
	// Overdue 已过截止日期。空的截止日期不算逾期 —— 没定期限的任务
	// 说它"逾期"是冤枉,人也不知道该怎么办。
	Overdue bool `json:"overdue,omitempty"`
}

// assetOpenItems 这台设备身上未了结的事。
type assetOpenItems struct {
	AssetID string          `json:"assetId"`
	Tasks   []openTaskBrief `json:"tasks"`
	// AbnormalWithoutTask 最近一次巡检判为异常/待整改,却没有任何在办任务。
	//
	// 【这是最危险的一种组合,也是最不容易被发现的】出了问题、没人接手,
	// 而两边各自看都很正常:台账里它只是一个红标签,任务列表里它根本不存在。
	// 只有把两者放在一起才看得出来 —— 所以由后端算,不留给前端拼。
	AbnormalWithoutTask bool `json:"abnormalWithoutTask,omitempty"`
	// LastStatus 最近一次巡检的结论,给上面那个判断作依据
	LastStatus string `json:"lastStatus,omitempty"`
}

// taskClosed 这条任务算不算了结了。
//
// 【定义只放一处】"已完成 / 已取消 之外都算在办"这条规则原本散在
// 计划页和移动端各写一遍。散着写的后果不是报错,是两个地方对"还剩几件事"
// 给出不同的数字,而没人知道该信哪个。
func taskClosed(status string) bool {
	switch strings.TrimSpace(status) {
	case engTaskStatusDone, "已取消":
		return true
	}
	return false
}

// assetStatusNeedsFollowUp 这个状态算不算"要跟进"。
func assetStatusNeedsFollowUp(status, level string) bool {
	s := strings.TrimSpace(status)
	if s == "异常" || s == "待整改" || s == "待维修" {
		return true
	}
	return level == "danger" || level == "repair"
}

// handleAssetOpenItems —— GET /api/assets/{id}/open-items
func (s *Server) handleAssetOpenItems(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}

	tasks, err := s.store.ListEngineeringTasks(EngineeringTaskFilter{TenantID: s.tenantForRequest(r)})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	// 【按东八区算今天】开发机是太平洋时区,用本地时间的话逾期判断会差一整天 ——
	// 而"差一天"正好是这类判断最容易错、也最难被发现的地方。
	today := dayStamp(time.Now().In(pushTZ))
	out := assetOpenItems{AssetID: id, Tasks: []openTaskBrief{}, LastStatus: asset.LastStatus}
	for _, t := range tasks {
		if t == nil || t.AssetID != id || taskClosed(t.Status) {
			continue
		}
		b := openTaskBrief{
			ID: t.ID, Title: firstNonEmpty(t.Title, "巡检任务"),
			Status: t.Status, AssigneeName: t.AssigneeName, DueAt: t.DueAt,
		}
		// 截止日期存的是 YYYY-MM-DD,直接按字符串比即可
		if d := strings.TrimSpace(t.DueAt); d != "" && d < today {
			b.Overdue = true
		}
		out.Tasks = append(out.Tasks, b)
	}
	// 逾期的排前面,其次按截止日期
	sort.SliceStable(out.Tasks, func(i, j int) bool {
		if out.Tasks[i].Overdue != out.Tasks[j].Overdue {
			return out.Tasks[i].Overdue
		}
		return out.Tasks[i].DueAt < out.Tasks[j].DueAt
	})

	out.AbnormalWithoutTask = len(out.Tasks) == 0 &&
		assetStatusNeedsFollowUp(asset.LastStatus, asset.StatusLevel)

	writeJSON(w, http.StatusOK, out)
}
