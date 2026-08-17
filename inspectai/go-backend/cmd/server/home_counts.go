package main

import (
	"net/http"
	"strings"
	"time"
)

// ===== 首页「今日快照」的四个数字 =====
//
// 【为什么单开一个接口】原来后台首页是这么算的:
//
//	listAssets()          整份资产
//	listRecords()         最新 100 条,每条带 fields_json + images_json —— 实测 654 KB
//	listTasks()           整张工程任务表
//	listChangeRequests()  最多 200 条
//
// 四份全量列表下载下来,在浏览器里 filter().length 出四个整数。而这是【首页】,
// 登录后第一个加载的页面。底栏角标那次踩过同样的坑(见 badge_counts.go),
// 结论一样:要数字就查数字。
//
// 另一个更要命的问题是:那四个请求的错误全被 .catch(() => void 0) 吞掉。
// 记录拉失败 → 首页显示"今日巡检 0 次",不报错、不提示。领导看到这个数字
// 会去问巡检员为什么没干活,而实际上是接口挂了。这个接口任何一路查失败都
// 直接返回错误,让前端能显示"取不到"而不是显示一个错的 0。

type HomeCounts struct {
	/** 待审批的修改申请 */
	Approvals int `json:"approvals"`
	/** 异常 + 待复核的设备台账 */
	AbnormalAssets int `json:"abnormalAssets"`
	/** 今日创建的巡检记录 */
	TodayRecords int `json:"todayRecords"`
	/** 待整改的工程任务 */
	RectifyTasks int `json:"rectifyTasks"`
}

func (s *SQLiteStore) HomeCounts(tenantID, dayPrefix string) (HomeCounts, error) {
	var c HomeCounts
	q := func(dst *int, sql string, args ...any) error {
		return s.db.QueryRow(sql, args...).Scan(dst)
	}
	if err := q(&c.Approvals,
		`SELECT COUNT(*) FROM change_requests WHERE tenant_id=? AND status='pending'`,
		tenantID); err != nil {
		return c, err
	}
	// 口径和台账列表的状态标一致:异常和待复核都算"要盯的"
	if err := q(&c.AbnormalAssets,
		`SELECT COUNT(*) FROM assets WHERE tenant_id=? AND last_status IN ('异常','待复核')`,
		tenantID); err != nil {
		return c, err
	}
	// 按天用字符串前缀比。dayPrefix 必须由 dayStamp 算(东八区)——
	// 库里的时间戳都是 fmtStamp 写的东八区字符串,拿本地时区的日期去比,
	// 在非东八区的开发机上整体错一天。
	if err := q(&c.TodayRecords,
		`SELECT COUNT(*) FROM records WHERE tenant_id=? AND created_at LIKE ?`,
		tenantID, dayPrefix+"%"); err != nil {
		return c, err
	}
	if err := q(&c.RectifyTasks,
		`SELECT COUNT(*) FROM engineering_tasks WHERE tenant_id=? AND status=?`,
		tenantID, engTaskStatusRectify); err != nil {
		return c, err
	}
	return c, nil
}

func (m *MemStore) HomeCounts(tenantID, dayPrefix string) (HomeCounts, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var c HomeCounts
	for _, cr := range m.changeRequests {
		if cr.Status == "pending" {
			c.Approvals++
		}
	}
	for _, a := range m.assets {
		if a.TenantID == tenantID && (a.LastStatus == "异常" || a.LastStatus == "待复核") {
			c.AbnormalAssets++
		}
	}
	for _, rec := range m.records {
		if rec.TenantID == tenantID && strings.HasPrefix(fmtStamp(rec.CreatedAt), dayPrefix) {
			c.TodayRecords++
		}
	}
	for _, t := range m.engTasks {
		if t.Status == engTaskStatusRectify {
			c.RectifyTasks++
		}
	}
	return c, nil
}

// handleHomeCounts — GET /api/management-ai/today
func (s *Server) handleHomeCounts(w http.ResponseWriter, r *http.Request) {
	if !s.requireSupervisorAccess(w, r) {
		return
	}
	c, err := s.store.HomeCounts(s.tenantForRequest(r), dayStamp(time.Now()))
	if err != nil {
		// 【不吞错】返回错误,让前端显示"取不到"。返回一堆 0 的话,
		// 首页会理直气壮地显示"今日巡检 0 次",没人知道那是假的。
		writeError(w, http.StatusInternalServerError, "counts_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, c)
}
