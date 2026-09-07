package main

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ===== 按天聚合 =====
//
// 【为什么这件事必须在后端做】看板那两张图(近 30 天趋势、状态热力图)
// 原来是这么画的:前端把记录列表拉下来,自己一天一天数。而那个列表是
// 有上限的 —— 记录一超过上限,较早的那些天就全数成 0。
//
// 图照样画得出来,而且很好看:一条从高到低的平滑曲线,像是巡检量在下滑。
// 【它不会报错,也没有任何地方提示数据不全】。列表少几条人一眼看得见,
// 图错了看不见 —— 还会被拿去汇报。
//
// 现在数由后端来数:数据库里有多少就数多少,回给前端的只是 30 个数字,
// 比原来传几百 KB 明细还快。

// statsMaxDays 最多看多少天。
//
// 上限的理由和记录列表那个不一样:这里回给前端的量很小(每天一行),
// 大头是后端要扫的记录数。180 天足够覆盖季度/半年复盘,再长的话
// 该走离线报表,不该让一个页面请求去扫。
const statsMaxDays = 180

const statsDefaultDays = 30

// dailyStat 某一天的巡检量与状态分布。
type dailyStat struct {
	Date     string         `json:"date"` // 本地日期 YYYY-MM-DD
	Total    int            `json:"total"`
	ByStatus map[string]int `json:"byStatus"`
}

// handleRecordDailyStats —— GET /api/inspection/stats/daily?days=30&project=xxx
func (s *Server) handleRecordDailyStats(w http.ResponseWriter, r *http.Request) {
	days := statsDefaultDays
	if raw := strings.TrimSpace(r.URL.Query().Get("days")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			days = min(n, statsMaxDays)
		}
	}

	// 【按东八区的"天"来分桶,不是 UTC】前端原来用 toISOString() 取日期,
	// 那是 UTC —— 北京时间早上 7:30 的巡检在 UTC 里还是前一天,于是
	// 【早班巡检一直被记在前一天】。现场是按本地日历上班的,日界线就该按本地算。
	now := time.Now().In(cnLoc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, cnLoc)
	since := today.AddDate(0, 0, -(days - 1))

	// 先把 30 个桶摆好。
	// 【没有记录的那天也要出现在结果里】只回有数据的天的话,前端得自己
	// 补齐缺的日期 —— 补漏了就是图上少一段,而不是显示成 0。
	buckets := make([]*dailyStat, 0, days)
	index := make(map[string]*dailyStat, days)
	for i := 0; i < days; i++ {
		d := since.AddDate(0, 0, i).Format("2006-01-02")
		b := &dailyStat{Date: d, ByStatus: map[string]int{}}
		buckets = append(buckets, b)
		index[d] = b
	}

	vis := s.visibilityFor(r)
	if vis.Blocked {
		// 配了项目范围却一个项目都没分到 —— 给空桶,不给全部。
		// (和记录列表同一条规矩,见 dataVisibility.Blocked)
		writeJSON(w, http.StatusOK, map[string]any{
			"days": buckets, "from": buckets[0].Date, "to": buckets[len(buckets)-1].Date,
		})
		return
	}

	records, err := s.store.ListRecordsSince(s.tenantForRequest(r), since)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "stats_failed", err.Error())
		return
	}

	// 页面上那个项目下拉。空 = 不筛。
	wantProject := strings.TrimSpace(r.URL.Query().Get("project"))

	// 数据范围过滤。
	//
	// 【这里在内存里筛是安全的】记录列表那边有一条铁律:不能先取 limit 条
	// 再在内存里筛(会变成"从最新 100 条里挑本项目的")。这里没有条数上限,
	// 取的是整个时间窗口,所以内存过滤不会漏。
	var owner *User
	if vis.OwnOnly {
		if u, ok := s.userFromSessionToken(s.tokenFromRequest(r)); ok {
			owner = u
		} else {
			// 只能看自己、又认不出是谁 —— 【给空,不给全部】。
			// 这个分支上放行等于把全租户的量交出去。
			writeJSON(w, http.StatusOK, map[string]any{
				"days": buckets, "from": buckets[0].Date, "to": buckets[len(buckets)-1].Date,
			})
			return
		}
	}
	allowedProjects := map[string]bool{}
	for _, p := range vis.Projects {
		allowedProjects[p] = true
	}

	for _, rec := range records {
		if rec == nil {
			continue
		}
		if !vis.AllData && len(allowedProjects) > 0 && !allowedProjects[rec.Project] {
			continue
		}
		if owner != nil && !recordOwnedBy(rec, owner.ID, owner.DisplayName, owner.Username) {
			continue
		}
		if wantProject != "" && rec.Project != wantProject {
			continue
		}
		// 【按 CreatedAt 分桶,不是提交时间】图的名字是"巡检量趋势",
		// 问的是"哪天巡的",不是"哪天审完的"。用提交时间的话,一批
		// 补审的历史记录会全堆在审批那天,看上去像那天巡了几十次。
		day := rec.CreatedAt.In(cnLoc).Format("2006-01-02")
		b, ok := index[day]
		if !ok {
			continue // 窗口外(边界上的时区误差),不计
		}
		b.Total++
		// 用界面口径的状态 —— 看板和记录页是同一批人对着看的,
		// 两处对同一条记录说法不一样,人只会觉得数据错了。
		b.ByStatus[recordBusinessStatus(rec)]++
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"days": buckets,
		"from": buckets[0].Date,
		"to":   buckets[len(buckets)-1].Date,
	})
}

// 归属判断用的是 handlers.go 里那个 recordOwnedBy —— 【不另写一份】。
// 这一趟已经栽过两次:业务状态和这个函数,都是动手写完才发现早就有了。
// 归属判错的后果是静默越权(在看板上数到别人的巡检量),不会报错。
