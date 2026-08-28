package main

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

// ===== 今日应巡 =====
//
// 每日计划说"这些设备今天要巡",系统自动判断哪些巡过了 ——
// 不给现场加"打勾"这个动作。判定依据是 asset_snapshots:
// 那张表就是"这台设备在什么时候被巡过",一条记录成单就写一条。
//
// 【为什么先做看板、后做推送】口径不准的自动推送比不推送更糟:
// 群里天天收到错的数字,很快就没人看了,而且很难挽回。
// 先让这个数字在页面上跑准,再让它自己发出去。

// DailyAssetStatus 一台设备今天的状态。
type DailyAssetStatus struct {
	AssetID   string `json:"assetId"`
	AssetName string `json:"assetName"`
	Project   string `json:"project"`
	Done      bool   `json:"done"`
	// DoneAt 今天巡的时间。没巡就是空。
	DoneAt string `json:"doneAt,omitempty"`
	// Missing 这台设备在台账里已经查不到了(计划录入后被删)。
	//
	// 【要显式标出来】不标的话它会永远算作"未完成",完成率永远到不了 100%,
	// 而没人知道是因为一台不存在的设备。
	Missing bool `json:"missing,omitempty"`

	// 下面三样是给移动端"点一下就去巡这台"用的 —— 和扫码锁定设备是同一套
	// 上下文(templateId 跳过 AI 分类、assetKey 让后端认归属)。
	//
	// 【必须在这里一起给】列表里已经查过台账了,让手机再逐台去问一遍
	// 就是几十次往返;而且中间那一刻台账被改了,两边还会对不上。
	TemplateID string `json:"templateId,omitempty"`
	AssetKey   string `json:"assetKey,omitempty"`
	PointID    string `json:"pointId,omitempty"`
}

// DailyPlanStatus 一条每日计划今天的执行情况。
type DailyPlanStatus struct {
	PlanID    string             `json:"planId"`
	Title     string             `json:"title"`
	Project   string             `json:"project"`
	OwnerName string             `json:"ownerName,omitempty"`
	Total     int                `json:"total"`
	Done      int                `json:"done"`
	Assets    []DailyAssetStatus `json:"assets"`
	// NoAssets 这条计划没指定设备 —— 算不出完成率。
	//
	// 建计划时要求填,但存量数据和"先建个壳回头再补"的情况一定存在。
	// 静默当成 0/0 会让它看起来像"已完成",所以单独标出来。
	NoAssets bool `json:"noAssets,omitempty"`
}

// TodayInspectionBoard 今日应巡看板。
type TodayInspectionBoard struct {
	Date    string            `json:"date"`
	Weekday int               `json:"weekday"` // 1=周一 … 7=周日
	Total   int               `json:"total"`
	Done    int               `json:"done"`
	Plans   []DailyPlanStatus `json:"plans"`
}

// buildTodayBoard 算出今天该巡什么、巡了多少。
//
// 【按设备去重】两条计划可能都点了同一台设备(比如"每日例检"和"重点关注"),
// 顶部的总数要按设备去重 —— 否则一台设备巡一次,总数却涨了 2,
// 完成率看着永远差一截。而各条计划自己的进度仍然分别算,互不影响。
func (s *Server) buildTodayBoard(r *http.Request, now time.Time) (*TodayInspectionBoard, error) {
	return s.buildTodayBoardFor(s.tenantForRequest(r), s.visibilityFor(r), now)
}

// buildTodayBoardFor 不依赖 *http.Request 的内核。
//
// 【为什么要劈开】定时推送没有 request —— 它是后台 goroutine 里跑的。
// 而如果为它另写一份"算今天谁没巡"的代码,两份口径迟早分叉:
// 页面上说还差 3 台、群里发出去说还差 5 台,而两边看起来都对,
// 没人会想到去比对。所以算法只有这一份,入口有两个。
//
// vis 由调用方给:HTTP 那边传这次请求的可见范围,调度器传"全部数据"
// (它代表系统本身,不代表某个人)。
func (s *Server) buildTodayBoardFor(tenant string, vis dataVisibility, now time.Time) (*TodayInspectionBoard, error) {
	plans, err := s.store.ListEngineeringPlans(EngineeringPlanFilter{})
	if err != nil {
		return nil, err
	}
	wd := isoWeekday(int(now.Weekday()))
	today := dayStamp(now)

	// 今天有巡检快照的设备。一次查全,不逐台查 —— 逐台查在几十台时就是几十次往返。
	doneAt, err := s.inspectedToday(tenant, today)
	if err != nil {
		return nil, err
	}

	// 台账里现有的设备,用来识别"计划里点了但已经删掉"的
	assets, err := s.store.ListAssets(tenant)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*AssetEntry, len(assets))
	for _, a := range assets {
		byID[a.ID] = a
	}

	board := &TodayInspectionBoard{Date: today, Weekday: wd, Plans: []DailyPlanStatus{}}
	seen := map[string]bool{} // 顶部总数按设备去重

	for _, p := range plans {
		if p == nil || p.PlanType != planTypeDaily {
			continue
		}
		if !runsOnWeekday(p.Weekdays, wd) {
			continue
		}
		// 数据范围:看不到这个项目的人,不该看到它的计划
		if !vis.allowsProject(p.Project) {
			continue
		}
		st := DailyPlanStatus{
			PlanID: p.ID, Title: firstNonEmpty(p.WorkContent, p.SubType, "每日巡检"),
			Project: p.Project, OwnerName: p.OwnerName,
			Assets: []DailyAssetStatus{},
		}
		if len(p.AssetIDs) == 0 {
			st.NoAssets = true
			board.Plans = append(board.Plans, st)
			continue
		}
		for _, id := range p.AssetIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			a := byID[id]
			item := DailyAssetStatus{AssetID: id}
			if a == nil {
				item.Missing = true
				item.AssetName = id
			} else {
				item.AssetName = firstNonEmpty(a.AssetName, a.AssetKey, id)
				item.Project = a.Project
				item.TemplateID = a.TemplateID
				item.AssetKey = a.AssetKey
				item.PointID = a.PointID
			}
			if t, ok := doneAt[id]; ok {
				item.Done = true
				item.DoneAt = t
			}
			st.Assets = append(st.Assets, item)
			st.Total++
			if item.Done {
				st.Done++
			}
			if !seen[id] {
				seen[id] = true
				board.Total++
				if item.Done {
					board.Done++
				}
			}
		}
		board.Plans = append(board.Plans, st)
	}
	sort.Slice(board.Plans, func(i, j int) bool {
		// 没做完的排前面 —— 这一屏是给人"还差什么"用的
		li := board.Plans[i].Total - board.Plans[i].Done
		lj := board.Plans[j].Total - board.Plans[j].Done
		if li != lj {
			return li > lj
		}
		return board.Plans[i].Title < board.Plans[j].Title
	})
	return board, nil
}

// handleTodayBoard —— GET /api/engineering/plans/today
func (s *Server) handleTodayBoard(w http.ResponseWriter, r *http.Request) {
	board, err := s.buildTodayBoard(r, time.Now())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "build_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, board)
}

// inspectedToday 今天有巡检快照的设备 → 最近一次的时间。
//
// 【一次查全,不逐台查】几十台设备逐台查就是几十次数据库往返,
// 而这个看板是每次打开计划页都要算的。
//
// 【按日期前缀比,不用时间区间】created_at 存的是带时区的字符串
// (见 fmtStamp,统一成 +08:00),前缀就是当天日期。用区间要先构造
// 当天零点/次日零点两个带时区的字符串,反而更容易和存的格式对不上。
func (s *Server) inspectedToday(tenantID, day string) (map[string]string, error) {
	out := map[string]string{}
	store, ok := s.store.(*SQLiteStore)
	if !ok {
		// MemStore(测试/回落):没有"按日期查快照"的接口,逐台翻。
		// 测试数据量小,不值得为它加一个只有测试用得到的 Store 方法。
		assets, err := s.store.ListAssets(tenantID)
		if err != nil {
			return out, err
		}
		for _, a := range assets {
			if a == nil {
				continue
			}
			snaps, sErr := s.store.ListAssetSnapshots(a.ID, 50, 0)
			if sErr != nil {
				continue
			}
			for _, sn := range snaps {
				if sn != nil && strings.HasPrefix(fmtStamp(sn.CreatedAt), day) {
					out[a.ID] = fmtStamp(sn.CreatedAt)
					break
				}
			}
		}
		return out, nil
	}
	rows, err := store.db.Query(`
		SELECT s.asset_id, MAX(s.created_at)
		FROM asset_snapshots s
		JOIN assets a ON a.id = s.asset_id
		WHERE a.tenant_id = ? AND s.created_at LIKE ?
		GROUP BY s.asset_id`, tenantOrDefault(tenantID), day+"%")
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, at string
		if err := rows.Scan(&id, &at); err != nil {
			return out, err
		}
		out[id] = at
	}
	return out, rows.Err()
}
