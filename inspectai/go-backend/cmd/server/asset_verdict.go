package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===== 一句话结论:这台设备现在要不要管 =====
//
// 【为什么要有这一层】设备档案页上摆着一堆事实 —— 状态、最近巡检时间、
// 未了结任务、读数曲线。全是真的,但没有一个是判断。人得自己把它们
// 在脑子里合起来,而这正是他打开这一页想让系统替他做的事。
//
// 【只给结论不给依据 = 没人会信】"健康分 72" 这种东西,人既无法认同
// 也无法反驳,最后只会忽略它。所以结论后面必须跟着那几条具体依据 ——
// 说得出"为什么",这个判断才站得住,人才有机会说"这条不算,我知道原因"。

const (
	verdictAct      = "act"      // 需要处理
	verdictSchedule = "schedule" // 建议安排
	verdictOK       = "ok"       // 暂时不用管
)

// 多久没巡就该安排一次。
//
// 和「巡检覆盖」那一屏的分档口径一致(超过 30 天算掉队)——
// 两处对"多久算久"给出不同答案的话,同一台设备在台账里是橙的、
// 在档案页里是绿的,而没人知道该信哪个。
const verdictStaleDays = 30

type assetVerdict struct {
	AssetID string `json:"assetId"`
	// Level: act / schedule / ok
	Level    string `json:"level"`
	Headline string `json:"headline"`
	// Reasons 依据。【结论可以被反驳,分数不能】——
	// 摆出来人才有机会说"这条不算",而不是默默不信。
	Reasons []string `json:"reasons"`
}

// buildAssetVerdict 由三样输入合成一句结论。
//
// 【纯函数】三样输入都由调用方查好传进来 —— 判断规则才测得了,
// 而"什么情况该报警"恰恰是最需要能测的部分。
func buildAssetVerdict(
	asset *AssetEntry,
	items assetOpenItems,
	trend assetTrendResp,
	daysSinceInspect int, // <0 表示从未巡检
	maintenanceOverdue int, // 距上次维保超期天数;0 = 不超期或算不出来
) assetVerdict {
	v := assetVerdict{AssetID: asset.ID, Level: verdictOK, Reasons: []string{}}

	overdue := 0
	for _, t := range items.Tasks {
		if t.Overdue {
			overdue++
		}
	}
	var drifting []string
	for _, s := range trend.Series {
		if s.Drifting {
			d := ""
			if s.Deviation != nil {
				dir := "高"
				if *s.Deviation < 0 {
					dir = "低"
				}
				d = "较平时" + dir + strconv.Itoa(int(absFloat(*s.Deviation))) + "%"
			}
			drifting = append(drifting, s.FieldLabel+d)
		}
	}

	// —— 依据先收齐,再定级。【顺序:先说要动手的,再说该留意的】——
	if items.AbnormalWithoutTask {
		v.Reasons = append(v.Reasons,
			"最近一次判为「"+firstNonEmpty(items.LastStatus, "异常")+"」,但没有任何在办任务")
	}
	if overdue > 0 {
		v.Reasons = append(v.Reasons, strconv.Itoa(overdue)+" 项整改已逾期")
	}
	if n := len(items.Tasks) - overdue; n > 0 {
		v.Reasons = append(v.Reasons, strconv.Itoa(n)+" 项在办未了结")
	}
	if len(drifting) > 0 {
		v.Reasons = append(v.Reasons, "读数偏离:"+strings.Join(drifting, "、"))
	}
	// 【维保超期归"需要处理"】它和"久没巡"不是一回事:久没巡是没人去看,
	// 而维保超期是该做的保养没做 —— 后者是合同和安全上的实质缺失。
	if maintenanceOverdue > 0 {
		v.Reasons = append(v.Reasons, maintenanceReason(maintenanceOverdue))
	}
	switch {
	case daysSinceInspect < 0:
		v.Reasons = append(v.Reasons, "从未巡检")
	case daysSinceInspect > verdictStaleDays:
		v.Reasons = append(v.Reasons, "已 "+strconv.Itoa(daysSinceInspect)+" 天没巡")
	}

	// —— 定级 ——
	switch {
	case items.AbnormalWithoutTask || overdue > 0 || len(drifting) > 0 ||
		len(items.Tasks) > 0 || maintenanceOverdue > 0:
		v.Level = verdictAct
		v.Headline = "需要处理"
	case daysSinceInspect < 0 || daysSinceInspect > verdictStaleDays:
		// 【"没人管"和"有问题"要分开】久没巡不代表它坏了,只代表没人看过 ——
		// 混成同一档会让真正出问题的设备淹没在"很久没巡"里面。
		v.Level = verdictSchedule
		v.Headline = "建议安排巡检"
	default:
		v.Level = verdictOK
		v.Headline = "暂时不用管"
	}
	return v
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// handleAssetVerdict —— GET /api/assets/{id}/verdict
func (s *Server) handleAssetVerdict(w http.ResponseWriter, r *http.Request, id string) {
	asset, err := s.store.GetAsset(s.tenantForRequest(r), id)
	if err != nil || asset == nil {
		writeError(w, http.StatusNotFound, "asset_not_found", "资产台账不存在")
		return
	}
	items, err := s.assetOpenItemsFor(r, asset)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}
	trend, err := s.assetTrendFor(asset, 0)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "list_failed", err.Error())
		return
	}

	// 【零值时间戳 = 从未巡检】Go 把没巡过的设备序列化成 0001-01-01,
	// 直接拿它算天数会得到七十多万天 —— 前端已经踩过一次(界面上显示
	// "739855 天前"),后端这里同样要防。
	days := -1
	if t := asset.LastInspectedAt; !t.IsZero() && t.Year() > 2000 {
		days = int(time.Since(t).Hours() / 24)
		if days < 0 {
			days = 0
		}
	}

	writeJSON(w, http.StatusOK,
		buildAssetVerdict(asset, items, trend, days, maintenanceOverdueDays(asset, time.Now())))
}
